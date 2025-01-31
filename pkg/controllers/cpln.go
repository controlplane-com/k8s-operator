package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/controlplane-com/types-go/pkg/base"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"github.com/go-logr/logr"
	"io"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net/http"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

var dependentResourceErr = errors.New("resource deletion failed. Another resource depends on this one. Deletion will be retried later")

func (r *CplnCRDController) getCplnContext(ctx context.Context, req ctrl.Request, cr *unstructured.Unstructured) (string, string, error) {
	l := log.FromContext(ctx)
	org, _ := cr.Object["org"].(string)
	if org == "" {
		msg := fmt.Sprintf("CRD resource %s/%s has no org field", req.Namespace, req.Name)
		if err := updateStatusWithSyncFailure(ctx, r.Client, cr, msg); err != nil {
			l.Error(err, "failed to update status with sync failure")
		}
		return "", "", errors.New(msg)
	}

	gvc, _ := cr.Object["gvc"].(string)
	if isGvcScoped(cr.GetKind()) && gvc == "" {
		msg := fmt.Sprintf("CRD resource %s/%s is of a gvc-scoped kind (%s), but has no gvc field", req.Namespace, req.Name, cr.GetKind())
		if err := updateStatusWithSyncFailure(ctx, r.Client, cr, msg); err != nil {
			l.Error(err, "failed to update status with sync failure")
		}
		return "", "", errors.New(msg)
	}
	return org, gvc, nil
}

var notFoundError = fmt.Errorf("cpln resource not found")

func (r *CplnCRDController) getCplnResourceUrl(org string, gvc string, crdObj *unstructured.Unstructured) (string, error) {
	kind := crdObj.GetKind()
	name := strings.ReplaceAll(cplnName(crdObj), ".", "")
	url := fmt.Sprintf("%s/org/%s", r.apiUrl, org)
	if gvc != "" {
		url = fmt.Sprintf("%s/gvc/%s", url, gvc)
	}
	return fmt.Sprintf("%s/%s/%s", url, strings.ToLower(kind), name), nil
}

func (r *CplnCRDController) putToCpln(ctx context.Context, l logr.Logger, token string, url string, cr *unstructured.Unstructured, cplnObj map[string]any, dryRun bool) (string, error) {
	payload, err := json.Marshal(cplnObj)
	if err != nil {
		l.Error(err, "Error marshalling payload")
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
		if err = updateStatusWithSyncFailure(ctx, r.Client, cr, string(responseJson)); err != nil {
			return "", err
		}
		return "", fmt.Errorf("PUT %s -> status %d: %s", url, resp.StatusCode, string(responseJson))
	}
	return string(responseJson), nil
}

func (r *CplnCRDController) getWorkloadDeploymentsFromCpln(ctx context.Context, org, gvc, token string, crdObj *unstructured.Unstructured) ([]deployment.Deployment, error) {
	url, err := r.getCplnResourceUrl(org, gvc, crdObj)
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

func (r *CplnCRDController) getCplnResource(ctx context.Context, org, gvc, token string, crdObj *unstructured.Unstructured) ([]byte, error) {
	url, err := r.getCplnResourceUrl(org, gvc, crdObj)
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

func (r *CplnCRDController) deleteFromCpln(ctx context.Context, org, gvc, token string, cr *unstructured.Unstructured) error {
	_, err := r.getCplnResource(ctx, org, gvc, token, cr)
	if errors.Is(err, notFoundError) {
		//Gone. Good!
		return nil
	}
	url, err := r.getCplnResourceUrl(org, gvc, cr)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	c := r.HttpClient
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusBadRequest {
		if err = updateStatusWithSyncFailure(ctx, r.Client, cr, "resource deletion failed. Another resource depends on this one. Deletion will be retried later"); err != nil {
			return err
		}
		return dependentResourceErr
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	return nil
}
