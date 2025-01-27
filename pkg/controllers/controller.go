package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/types-go/pkg/base"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"github.com/evanphx/json-patch/v5"
	"github.com/go-logr/logr"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"slices"
	"strings"
	"time"
)

type CplnCRDController struct {
	client.Client
	Scheme     *runtime.Scheme
	HttpClient *http.Client
	gvk        schema.GroupVersionKind
	apiUrl     string
}

var zeroResult = ctrl.Result{}

var defaultResult = ctrl.Result{
	RequeueAfter: time.Second * 5,
}

var ignoredFields = []string{"version", "gvc", "lastModified"}
var ignoredKinds = []string{"deployment", "deploymentversion", "containerstatus", "jobexecutionstatus"}
var secret *corev1.Secret

func BuildControllers(mgr ctrl.Manager) error {

	gvks, err := listGVKForCRDs()
	if err != nil {
		return err
	}
	for _, gvk := range gvks {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		url := os.Getenv("CPLN_API_URL")
		if url == "" {
			url = "https://api.cpln.io"
		}
		r := &CplnCRDController{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			gvk:    gvk,
			apiUrl: url,
		}
		err = ctrl.NewControllerManagedBy(mgr).Named(fmt.Sprintf("%s_controller", gvk.Kind)).For(obj).Complete(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func listGVKForCRDs() ([]schema.GroupVersionKind, error) {
	var gvks []schema.GroupVersionKind
	entries, err := os.ReadDir("chart/crd")
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		kind := strings.ToLower(strings.Split(e.Name(), ".")[0])
		if slices.Contains(ignoredKinds, kind) {
			continue
		}
		gvks = append(gvks, schema.GroupVersionKind{
			Group:   common.API_GROUP,
			Version: common.API_REVISION,
			Kind:    kind,
		})
	}
	return gvks, nil
}

func (r *CplnCRDController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(r.gvk)

	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if metav1.IsNotFound(err) {
			l.Info("CRD resource not found; must have been deleted")
			return zeroResult, nil
		}
		return zeroResult, err
	}

	if secret == nil {
		secret = &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: req.Namespace,
			Name:      common.SECRET_NAME,
		}, secret); err != nil {
			l.Error(err, "unable to sync resources because the secret %s could not be found", common.SECRET_NAME)
			os.Exit(1)
		}
	}

	b, orgOK := secret.Data["org"]
	token, tokenOK := secret.Data["token"]
	if !orgOK || !tokenOK {
		// If missing, we can't do anything
		msg := "secret missing required fields 'org' or 'token'"
		l.Error(nil, msg)
		return zeroResult, fmt.Errorf(msg)
	}
	org := string(b)

	md, ok := cr.Object["metadata"].(map[string]any)
	if !ok {
		l.Info("CR has no metadata? skipping")
		return zeroResult, nil
	}
	generation, _ := md["generation"].(int64)

	st := getOperatorStatus(cr)
	cplnLastSynced, _ := st["lastSyncedGeneration"].(int64)

	if r.gvk.Kind == common.KIND_WORKLOAD {
		syncCtx := newSyncContext(ctx, r.Client)
		syncCtx.namespace = req.Namespace
		syncCtx.parent = cr
		deployments, err := r.getWorkloadDeploymentsFromCpln(ctx, org, string(token), cr)
		if err != nil && !errors.Is(err, notFoundError) {
			return zeroResult, err
		}
		if err = syncWorkloadDeployments(syncCtx, deployments); err != nil {
			return zeroResult, err
		}
	}

	if cplnLastSynced == generation {
		return r.syncFromCplnToK8s(ctx, l, org, string(token), cr)
	} else {
		return r.syncFromK8sToCpln(ctx, l, org, string(token), cr)
	}
}

func getOperatorStatus(cr *unstructured.Unstructured) map[string]any {
	st, ok := cr.Object["status"].(map[string]any)
	if !ok {
		st = make(map[string]any)
	}
	_, ok = st["operator"].(map[string]any)
	if !ok {
		st["operator"] = map[string]any{}
	}
	cr.Object["status"] = st
	return cr.Object["status"].(map[string]any)["operator"].(map[string]any)
}

// updateCRDSpec is a simple method to update the CRD's spec (unstructured).
func (r *CplnCRDController) updateCRDSpec(ctx context.Context, crdObj *unstructured.Unstructured) error {
	return r.Client.Update(ctx, crdObj)
}

func (r *CplnCRDController) updateCrStatus(ctx context.Context, cr *unstructured.Unstructured, newStatus any) error {
	md, _ := cr.Object["metadata"].(map[string]any)
	g, _ := md["generation"].(int64)

	operatorStatus := getOperatorStatus(cr)
	operatorStatus["lastSyncedGeneration"] = g
	st := cr.Object["status"].(map[string]any)
	if m, ok := newStatus.(map[string]any); ok {
		for k, v := range m {
			if k == "operator" {
				continue
			}
			st[k] = v
		}
	}
	return r.Client.Status().Update(ctx, cr)
}

var notFoundError = fmt.Errorf("cpln resource not found")

func (r *CplnCRDController) getWorkloadDeploymentsFromCpln(ctx context.Context, org, token string, crdObj *unstructured.Unstructured) ([]deployment.Deployment, error) {
	url, err := r.getCplnResourceUrl(org, crdObj)
	if err != nil {
		return nil, err
	}
	url = fmt.Sprintf("%s/%s", url, "deployment")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	c := r.HttpClient
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, notFoundError
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

func (r *CplnCRDController) fetchCplnResource(ctx context.Context, org, token string, crdObj *unstructured.Unstructured) ([]byte, error) {
	url, err := r.getCplnResourceUrl(org, crdObj)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	c := r.HttpClient
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, notFoundError
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

func (r *CplnCRDController) crdGvc(crdObj *unstructured.Unstructured) (string, error) {
	_, gvc, err := nameToCplnFormat(crdObj.GetKind(), crdObj.GetName())
	if err != nil {
		return "", err
	}
	return gvc, nil
}

func (r *CplnCRDController) getCplnResourceUrl(org string, crdObj *unstructured.Unstructured) (string, error) {
	kind := crdObj.GetKind()
	name, gvc, err := nameToCplnFormat(kind, crdObj.GetName())
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/org/%s", r.apiUrl, org)
	if gvc != "" {
		url = fmt.Sprintf("%s/gvc/%s", url, gvc)
	}
	url = fmt.Sprintf("%s/%s/%s", url, strings.ToLower(kind), name)
	return url, nil
}

func (r *CplnCRDController) putToCpln(ctx context.Context, token string, url string, cplnObj map[string]any, dryRun bool) (string, error) {
	payload, err := json.Marshal(cplnObj)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if dryRun {
		query := req.URL.Query()
		query.Set("dryRun", "true")
		req.URL.RawQuery = query.Encode()
	}
	c := r.HttpClient
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

func (r *CplnCRDController) syncFromCplnToK8s(ctx context.Context, log logr.Logger, org string, token string, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration == generation, pulling from Control Plane")

	cplnResource, err := r.fetchCplnResource(ctx, org, token, cr)
	if err != nil && !errors.Is(err, notFoundError) {
		log.Error(err, "Error fetching from Control Plane")
		return zeroResult, err
	}
	if errors.Is(err, notFoundError) {
		log.Info("Resource not found on Control Plane, deleting from Kubernetes")
		if err := r.Client.Delete(ctx, cr); err != nil {
			log.Error(err, "Error deleting from Kubernetes")
		}
	}

	var cplnResourceMap map[string]any
	err = json.Unmarshal(cplnResource, &cplnResourceMap)
	if err != nil {
		return zeroResult, err
	}

	url, err := r.getCplnResourceUrl(org, cr)
	if err != nil {
		return zeroResult, err
	}

	cplnObj, err := toCplnFormat(cr)
	if err != nil {
		return zeroResult, err
	}

	cplnResourceAfterDryRun, err := r.putToCpln(ctx, token, url, cplnObj, true)
	if err != nil {
		log.Error(err, "Error during cpln dry run")
		return zeroResult, err
	}

	patch, err := jsonpatch.CreateMergePatch([]byte(cplnResourceAfterDryRun), cplnResource)
	if err != nil {
		log.Error(err, "Error creating merge patch")
		return zeroResult, err
	}

	patchMap := map[string]any{}
	if err = json.Unmarshal(patch, &patchMap); err != nil {
		log.Error(err, "Error unmarshalling patch")
		return zeroResult, err
	}
	for _, field := range ignoredFields {
		delete(patchMap, field)
	}
	patch, err = json.Marshal(patchMap)
	if err != nil {
		log.Error(err, "Error marshalling patch")
		return zeroResult, err
	}

	//No changes
	if len(patch) == 0 || string(patch) == "{}" {
		if err := r.updateCrStatus(ctx, cr, cplnResourceMap["status"]); err != nil {
			log.Error(err, "Failed to update lastSyncedGeneration after pulling from Control Plane")
			return zeroResult, err
		}
		return defaultResult, nil
	}

	localSpec, err := json.Marshal(cplnObj)
	if err != nil {
		log.Error(err, "Could not marshal crd as JSON")
		return zeroResult, err
	}

	patchedSpecJson, err := jsonpatch.MergePatch(localSpec, patch)
	if err != nil {
		log.Error(err, "Could not merge patch")
		return zeroResult, err
	}

	var patchedSpec map[string]any
	err = json.Unmarshal(patchedSpecJson, &patchedSpec)
	if err != nil {
		log.Error(err, "Could not unmarshal patched spec as JSON")
		return zeroResult, err
	}
	cr, err = toK8sFormat(cr, patchedSpec)
	if err != nil {
		return zeroResult, err
	}

	if err := r.updateCRDSpec(ctx, cr); err != nil {
		log.Error(err, "Failed to patch CRD with new .spec from Control Plane")
		return zeroResult, err
	}

	for {
		err := r.updateCrStatus(ctx, cr, cplnResourceMap["status"])
		if err == nil {
			break
		}
		var apiErr *metav1.StatusError
		if errors.As(err, &apiErr) && apiErr.Status().Code == http.StatusConflict {
			log.Info("Conflict detected (HTTP 409), retrying updateCrStatus")
			err := r.Client.Get(ctx, types.NamespacedName{
				Namespace: cr.GetNamespace(),
				Name:      cr.GetName(),
			}, cr)
			if err != nil {
				return zeroResult, err
			}
			continue
		}
		log.Error(err, "Failed to update lastSyncedGeneration after pulling from Control Plane")
		return zeroResult, err
	}

	return defaultResult, nil
}

func (r *CplnCRDController) syncFromK8sToCpln(ctx context.Context, log logr.Logger, org string, token string, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration != generation, pushing to Control Plane")

	url, err := r.getCplnResourceUrl(org, cr)
	if err != nil {
		return zeroResult, err
	}

	cplnObj, err := toCplnFormat(cr)
	if err != nil {
		return zeroResult, err
	}
	response, err := r.putToCpln(ctx, token, url, cplnObj, false)
	if err != nil {
		log.Error(err, "Failed to PUT resource to Control Plane")
		return zeroResult, err
	}

	responseMap := map[string]any{}
	if err := json.Unmarshal([]byte(response), &responseMap); err != nil {
		return zeroResult, err
	}

	if err := r.updateCrStatus(ctx, cr, responseMap["status"]); err != nil {
		log.Error(err, "Failed to update lastSyncedGeneration after pushing to Control Plane")
		return zeroResult, err
	}
	return defaultResult, nil
}
