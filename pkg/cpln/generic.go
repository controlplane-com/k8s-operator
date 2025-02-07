package cpln

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/types-go/pkg/base"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
	"sync"
)

type genericUrlProvider struct {
	apiUrl string
}

func NewGenericUrlProvider(apiUrl string) UrlProvider {
	return &genericUrlProvider{apiUrl: apiUrl}
}

var secrets = map[string]*corev1.Secret{}
var m = &sync.Mutex{}

func (g *genericUrlProvider) ReadUrl(ctx Context, cr *unstructured.Unstructured) string {
	org := ctx.Org()
	gvc := ctx.Gvc()
	kind := cr.GetKind()
	name := Name(cr)
	url := fmt.Sprintf("%s/org/%s", g.apiUrl, org)
	if gvc != "" {
		url = fmt.Sprintf("%s/gvc/%s", url, gvc)
	}
	return fmt.Sprintf("%s/%s/%s", url, strings.ToLower(kind), name)
}

func (g *genericUrlProvider) WriteUrl(ctx Context, crdObj *unstructured.Unstructured) string {
	return g.ReadUrl(ctx, crdObj)
}

type genericConnector struct {
	*http.Client
	k8sClient client.Client
	UrlProvider
	Converter
}

func NewGenericConnector(client client.Client, apiUrl string) Connector {
	g := &genericConnector{
		k8sClient: client,
		Client:    &http.Client{},
	}
	g.InjectUrlProvider(&genericUrlProvider{
		apiUrl: apiUrl,
	})
	g.InjectConverter(NewGenericConverter(common.API_VERSION))
	return g
}

func (g *genericConnector) InjectUrlProvider(u UrlProvider) {
	g.UrlProvider = u
}

func (g *genericConnector) InjectConverter(c Converter) {
	g.Converter = c
}

func (g *genericConnector) Context(ctx context.Context, cr *unstructured.Unstructured) (Context, error) {
	org, _ := cr.Object["org"].(string)
	if org == "" {
		return nil, errors.New(fmt.Sprintf("CRD resource has no org field"))
	}

	gvc, _ := cr.Object["gvc"].(string)
	if common.IsGvcScoped(cr.GetKind()) && gvc == "" {
		return nil, errors.New(fmt.Sprintf("CRD resource %s/%s is of a gvc-scoped kind (%s), but has no gvc field", cr.GetKind()))
	}
	token, err := g.getSecret(ctx, org)
	if err != nil {
		return nil, err
	}
	return NewContext(ctx, org, gvc, token), nil
}

func (g *genericConnector) Put(ctx Context, cr *unstructured.Unstructured, dryRun bool) (string, error) {
	url := g.WriteUrl(ctx, cr)
	l := log.FromContext(ctx)
	cplnObj, err := g.CplnFormat(cr)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(cplnObj)
	if err != nil {
		l.Error(err, "Error marshalling payload")
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Token())
	req.Header.Set("Content-Type", "application/json")
	if dryRun {
		query := req.URL.Query()
		query.Set("dryRun", "true")
		req.URL.RawQuery = query.Encode()
	}
	c := g.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	responseJson, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("PUT %s -> status %d: %s", url, resp.StatusCode, string(responseJson))
	}
	return string(responseJson), nil
}

func (g *genericConnector) Get(ctx Context, crdObj *unstructured.Unstructured) ([]byte, error) {
	url := g.ReadUrl(ctx, crdObj)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Token())

	c := g.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, common.NotFoundError
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

func (g *genericConnector) Delete(ctx Context, cr *unstructured.Unstructured) error {
	_, err := g.Get(ctx, cr)
	if errors.Is(err, common.NotFoundError) {
		//Gone. Good!
		return nil
	}
	url := g.WriteUrl(ctx, cr)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Token())
	c := g.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusBadRequest {
		return common.DependentResourceErr
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	return nil
}

func (g *genericConnector) getWorkloadDeploymentsFromCpln(ctx Context, crdObj *unstructured.Unstructured) ([]deployment.Deployment, error) {
	url := g.UrlProvider.ReadUrl(ctx, crdObj)
	url = fmt.Sprintf("%s/%s", url, "deployment")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Token())

	c := g.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, common.NotFoundError
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var deployments base.GenericList[deployment.Deployment]
	return deployments.Items, json.Unmarshal(body, &deployments)
}

func (g *genericConnector) getSecret(ctx context.Context, org string) (string, error) {
	m.Lock()
	defer m.Unlock()
	l := log.FromContext(ctx)
	secret, _ := secrets[org]
	if secret == nil {
		secret = &corev1.Secret{}
		if err := g.k8sClient.Get(ctx, types.NamespacedName{
			Namespace: common.CONTROLLER_NAMESPACE,
			Name:      fmt.Sprintf("%s", org),
		}, secret); err != nil {
			return "", fmt.Errorf("unable to sync resources because the secret %s could not be found. Details: %v", org, err)
		}
		secrets[org] = secret
	}

	token := string(secret.Data["token"])
	if token == "" {
		// If missing, we can't do anything
		msg := "secret missing required field: token'"
		l.Error(nil, msg)
		return "", fmt.Errorf(msg)
	}
	return token, nil
}

type genericConverter struct {
	apiVersion string
}

func NewGenericConverter(apiVersion string) Converter {
	return &genericConverter{
		apiVersion: apiVersion,
	}
}

func (g *genericConverter) K8sFormat(ctx Context, template *unstructured.Unstructured, cpln map[string]any) (*unstructured.Unstructured, error) {
	//convert name/gvc into metadata name and namespace
	//add k8s boilerplate
	cr := &unstructured.Unstructured{
		Object: map[string]any{},
	}
	for key, val := range cpln {
		cr.Object[key] = val
	}
	cr.Object["org"] = ctx.Org()
	gvc := ctx.Gvc()
	if gvc != "" {
		cr.Object["gvc"] = gvc
	}
	cr.Object["metadata"] = template.Object["metadata"]
	cr.SetAPIVersion(g.apiVersion)
	cr.SetResourceVersion(template.GetResourceVersion())
	cr.SetKind(template.GetKind())
	cr.SetNamespace(template.GetNamespace())
	return cr, nil
}

func (g *genericConverter) CplnFormat(cr *unstructured.Unstructured) (map[string]any, error) {
	crCopy := cr.DeepCopy()
	c := map[string]any{}
	c["name"] = Name(crCopy)
	for key, prop := range crCopy.Object {
		if keep, ok := k8sBoilerplate[key]; ok && !keep {
			continue
		}
		//Status can't be touched by users
		if key == "status" {
			continue
		}
		//Org can't be sent to the cpln API
		if key == "org" {
			continue
		}
		c[key] = prop
	}
	return c, nil
}
