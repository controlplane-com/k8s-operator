package controllers

import (
	"context"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type genericConnector struct {
	gvk schema.GroupVersionKind
	client.Client
}

func NewGenericConnector(gvk schema.GroupVersionKind, client client.Client) Connector {
	return &genericConnector{
		gvk:    gvk,
		Client: client,
	}
}

func (g *genericConnector) Read(ctx context.Context, name types.NamespacedName) (*unstructured.Unstructured, error) {
	l := log.FromContext(ctx)
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(g.gvk)

	if err := g.Get(ctx, name, cr); err != nil {
		if metav1.IsNotFound(err) {
			l.Info("CRD resource not found; must have been deleted")
			return nil, nil
		}
		return nil, err
	}
	return cr, nil
}

func (g *genericConnector) Write(ctx context.Context, cr *unstructured.Unstructured) error {
	return g.Client.Update(ctx, cr)
}

func (s *genericConnector) WriteStatus(ctx context.Context, cr *unstructured.Unstructured) error {
	return s.Status().Update(ctx, cr)
}

func (s *genericConnector) Cleanup(ctx context.Context, cr *unstructured.Unstructured) error {
	if err := s.Delete(ctx, cr); err != nil {
		return err
	}
	if err := s.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		return err
	}
	finalizers := cr.GetFinalizers()
	if len(finalizers) == 0 {
		return nil
	}

	var updatedFinalizers []string
	for _, finalizer := range finalizers {
		if finalizer != common.FINALIZER {
			updatedFinalizers = append(updatedFinalizers, finalizer)
		}
	}

	cr.SetFinalizers(updatedFinalizers)

	return s.Client.Update(ctx, cr)
}
