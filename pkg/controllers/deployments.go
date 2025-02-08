package controllers

import (
	"context"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/types-go/pkg/containerstatus"
	"github.com/controlplane-com/types-go/pkg/cronjob"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
	"time"
)

type Interest struct {
	Org      string `json:"org"`
	Gvc      string `json:"gvc"`
	Workload string `json:"workload"`
}

type RegisterInterestRequest struct {
	Token     string     `json:"token"`
	Interests []Interest `json:"interests"`
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

func syncDeploymentVersions(ctx *syncContext, versions []deployment.DeploymentVersion) error {
	var versionCRs []*unstructured.Unstructured
	for i, v := range versions {
		if v.Name == "" {
			versions[i].Name = fmt.Sprintf("version-%d", int(v.Workload))
		}
	}
	for _, v := range versions {
		cr, err := unstructuredCR(common.DeploymentVersionGVK, ctx.namespace, v.Name, v, ctx.parent)
		if err != nil {
			return err
		}
		if v.Ready {
			ready(cr)
		} else {
			progressing(cr)
		}
		synced(cr, true, nil)
		versionCRs = append(versionCRs, cr)
	}
	deletedVersions, err := syncCRs(ctx.copy(), versionCRs, common.DeploymentVersionGVK)
	if err != nil {
		return err
	}
	for i, v := range versions {
		if slices.Contains(deletedVersions, v.Name) {
			continue
		}
		versionCtx := ctx.copy()
		versionCtx.parent = versionCRs[i]
		if err = syncContainerStatuses(versionCtx, common.MapValues(v.Containers)); err != nil {
			return err
		}
	}
	return nil
}

func anyVersionReady(versions []deployment.DeploymentVersion) bool {
	for _, v := range versions {
		if v.Ready {
			return true
		}
	}
	return false
}

func anyVersionUnready(versions []deployment.DeploymentVersion) bool {
	for _, v := range versions {
		if !v.Ready {
			return true
		}
	}
	return false
}

func syncJobExecutions(ctx *syncContext, jobExecutions []cronjob.JobExecutionStatus) error {
	var jobExecutionCRs []*unstructured.Unstructured
	for _, j := range jobExecutions {
		cr, err := unstructuredCR(common.JobExecutionStatusGVK, ctx.namespace, j.Name, j, ctx.parent)
		if err != nil {
			return err
		}
		if j.Status != "failed" && j.Status != "invalid" && j.Status != "removed" {
			ready(cr)
		} else {
			progressing(cr)
		}
		synced(cr, true, nil)
		jobExecutionCRs = append(jobExecutionCRs, cr)
	}
	deletedExecutions, err := syncCRs(ctx, jobExecutionCRs, common.JobExecutionStatusGVK)
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
		cr, err := unstructuredCR(common.ContainerStatusGVK, ctx.namespace, container.Name, container, ctx.parent)
		if err != nil {
			return err
		}
		if container.Ready {
			ready(cr)
		} else {
			progressing(cr)
		}
		synced(cr, true, nil)
		containerCRs = append(containerCRs, cr)
	}
	_, err := syncCRs(ctx, containerCRs, common.ContainerStatusGVK)
	return err
}

func getMessage(m map[string]any) string {
	if m == nil {
		return ""
	}
	message, ok := m["message"].(string)
	if !ok {
		return ""
	}
	return message
}

func collectDeploymentMessages(deployments []*unstructured.Unstructured) []string {
	var messages []string
	for _, d := range deployments {
		status, ok := d.Object["status"].(map[string]any)
		if ok {
			messages = append(messages, getMessage(status))
		}
		for _, v := range status["versions"].([]any) {
			v := v.(map[string]any)
			m := getMessage(v)
			if m != "" {
				messages = append(messages, m)
			}
			for _, c := range v["containers"].(map[string]any) {
				c := c.(map[string]any)
				m := getMessage(c)
				if m != "" {
					messages = append(messages, m)
				}
			}
		}
	}
	return messages
}
