package controllers

import (
	"context"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/types-go/pkg/containerstatus"
	"github.com/controlplane-com/types-go/pkg/cronjob"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
	"time"
)

var deploymentGVK = schema.GroupVersionKind{
	Group:   common.API_GROUP,
	Version: common.API_REVISION,
	Kind:    "deployment",
}
var deploymentVersionGVK = schema.GroupVersionKind{
	Group:   common.API_GROUP,
	Version: common.API_REVISION,
	Kind:    "deploymentversion",
}
var containerStatusGVK = schema.GroupVersionKind{
	Group:   common.API_GROUP,
	Version: common.API_REVISION,
	Kind:    "containerstatus",
}
var jobExecutionStatusGVK = schema.GroupVersionKind{
	Group:   common.API_GROUP,
	Version: common.API_REVISION,
	Kind:    "jobexecutionstatus",
}

type syncContext struct {
	ctx       context.Context
	namespace string
	c         client.Client
	parent    *unstructured.Unstructured
}

func (ctx *syncContext) Deadline() (deadline time.Time, ok bool) {
	return ctx.ctx.Deadline()
}

func (ctx *syncContext) Done() <-chan struct{} {
	return ctx.ctx.Done()
}

func (ctx *syncContext) Err() error {
	return ctx.ctx.Err()
}

func (ctx *syncContext) Value(key any) any {
	return ctx.ctx.Value(key)
}

func newSyncContext(ctx context.Context, c client.Client) *syncContext {
	return &syncContext{
		ctx: ctx,
		c:   c,
	}
}

func (ctx *syncContext) copy() *syncContext {
	c := newSyncContext(ctx.ctx, ctx.c)
	c.c = ctx.c
	u := *ctx.parent
	c.parent = &u
	c.namespace = ctx.namespace
	return c
}

func ready(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"]; !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Ready"
	status["conditions"] = []map[string]any{
		{
			"status": "True",
			"type":   "Ready",
		},
	}
}

func unready(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"]; !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Pending"
	status["conditions"] = []map[string]any{
		{
			"status": "False",
			"type":   "Ready",
		},
	}
}

func syncWorkloadDeployments(ctx *syncContext, deployments []deployment.Deployment) error {
	var deploymentCRs []*unstructured.Unstructured
	for _, d := range deployments {
		cr, err := unstructuredCR(deploymentGVK, ctx.namespace, d.Name, d, ctx.parent)
		if err != nil {
			return err
		}
		if d.Status.Ready {
			ready(cr)
		} else {
			unready(cr)
		}
		delete(cr.Object["status"].(map[string]any), "internal")
		deploymentCRs = append(deploymentCRs, cr)
	}
	deletedDeployments, err := syncCRs(ctx.copy(), deploymentCRs, deploymentGVK)
	if err != nil {
		return err
	}

	for i, d := range deployments {
		if slices.Contains(deletedDeployments, d.Name) {
			continue
		}
		ctx.parent = deploymentCRs[i]
		if err = syncDeploymentVersions(ctx.copy(), d.Status.Versions); err != nil {
			return err
		}
		if err = syncJobExecutions(ctx.copy(), d.Status.JobExecutions); err != nil {
			return err
		}
	}
	return nil
}

func syncDeploymentVersions(ctx *syncContext, versions []deployment.DeploymentVersion) error {
	var versionCRs []*unstructured.Unstructured
	var filteredVersions []deployment.DeploymentVersion
	for _, v := range versions {
		if v.Name == "" {
			continue
		}
		filteredVersions = append(filteredVersions, v)
	}
	for _, v := range filteredVersions {
		cr, err := unstructuredCR(deploymentVersionGVK, ctx.namespace, v.Name, v, ctx.parent)
		if err != nil {
			return err
		}
		if v.Ready {
			ready(cr)
		} else {
			unready(cr)
		}
		versionCRs = append(versionCRs, cr)
	}
	deletedVersions, err := syncCRs(ctx, versionCRs, deploymentVersionGVK)
	if err != nil {
		return err
	}
	for i, v := range filteredVersions {
		if slices.Contains(deletedVersions, v.Name) {
			continue
		}
		ctx.parent = versionCRs[i]
		if err = syncContainerStatuses(ctx.copy(), common.MapValues(v.Containers)); err != nil {
			return err
		}
	}
	return nil
}

func syncJobExecutions(ctx *syncContext, jobExecutions []cronjob.JobExecutionStatus) error {
	var jobExecutionCRs []*unstructured.Unstructured
	for _, j := range jobExecutions {
		cr, err := unstructuredCR(jobExecutionStatusGVK, ctx.namespace, j.Name, j, ctx.parent)
		if err != nil {
			return err
		}
		if j.Status != "failed" && j.Status != "invalid" && j.Status != "removed" {
			ready(cr)
		} else {
			unready(cr)
		}
		jobExecutionCRs = append(jobExecutionCRs, cr)
	}
	deletedExecutions, err := syncCRs(ctx, jobExecutionCRs, jobExecutionStatusGVK)
	if err != nil {
		return err
	}
	for i, j := range jobExecutions {
		if slices.Contains(deletedExecutions, j.Name) {
			continue
		}
		ctx.parent = jobExecutionCRs[i]
		if err = syncContainerStatuses(ctx.copy(), common.MapValues(j.Containers)); err != nil {
			return err
		}
	}
	return nil
}

func syncContainerStatuses(ctx *syncContext, containers []containerstatus.ContainerStatus) error {
	var containerCRs []*unstructured.Unstructured
	for _, container := range containers {
		cr, err := unstructuredCR(containerStatusGVK, ctx.namespace, container.Name, container, ctx.parent)
		if err != nil {
			return err
		}
		if container.Ready {
			ready(cr)
		} else {
			unready(cr)
		}
		containerCRs = append(containerCRs, cr)
	}
	_, err := syncCRs(ctx, containerCRs, containerStatusGVK)
	return err
}
