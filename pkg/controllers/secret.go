package controllers

import (
	"context"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type secretConnector struct {
	Connector
	client.Client
}

func NewSecretConnector(client client.Client) Connector {
	return &secretConnector{
		Connector: NewGenericConnector(common.NativeSecretGVK, client),
		Client:    client,
	}
}

func (s *secretConnector) Read(ctx context.Context, name types.NamespacedName) (*unstructured.Unstructured, error) {
	nativeSecret, err := s.Connector.Read(ctx, name)
	if err != nil || nativeSecret == nil {
		return nativeSecret, err
	}

	cpSecret := &unstructured.Unstructured{}
	cpSecret.SetNamespace(nativeSecret.GetNamespace())
	cpSecret.SetName(nativeSecret.GetName())
	cpSecret.SetGroupVersionKind(common.CplnSecretGVK)

	err = s.Client.Get(ctx, name, cpSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nativeSecret, nil
		}
		return nil, err // Actual error
	}

	if status, ok, _ := unstructured.NestedFieldCopy(cpSecret.Object, "status"); ok {
		_ = unstructured.SetNestedField(nativeSecret.Object, status, "status")
	}

	m := nativeSecret.Object["metadata"].(map[string]any)
	v, err := strconv.Atoi(m["resourceVersion"].(string))
	if err != nil {
		m["generation"] = int64(1)
	} else {
		m["generation"] = int64(v)
	}

	return nativeSecret, nil
}

func (s *secretConnector) WriteStatus(ctx context.Context, cr *unstructured.Unstructured) error {
	existingChild, err := s.getStatusChild(ctx, cr)
	if err != nil {
		return err
	}
	child, err := s.updateStatusChild(cr, existingChild)
	if err != nil {
		return err
	}
	if existingChild != nil {
		child.SetResourceVersion(child.GetResourceVersion())
		if err = s.Client.Status().Update(ctx, child); err != nil {
			return err
		}
		return s.Client.Update(ctx, child)
	}
	if err := s.Client.Create(ctx, child); err != nil {
		return err
	}
	return s.Client.Status().Update(ctx, child)
}

func (s *secretConnector) Cleanup(ctx context.Context, parent *unstructured.Unstructured) error {
	existingChild, err := s.getStatusChild(ctx, parent)
	child, err := s.updateStatusChild(parent, existingChild)
	if err != nil {
		return err
	}
	//Delete the child
	if err = s.Connector.Cleanup(ctx, child); err != nil {
		return err
	}
	//Delete the parent
	return s.Connector.Cleanup(ctx, parent)
}

func (s *secretConnector) getStatusChild(ctx context.Context, cr *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	child := &unstructured.Unstructured{}
	child.SetGroupVersionKind(common.CplnSecretGVK)
	err := s.Client.Get(ctx, types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.GetName(),
	}, child)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return child, nil
}

func (s *secretConnector) updateStatusChild(cr *unstructured.Unstructured, cplnSecret *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if cplnSecret == nil {
		cplnSecret = &unstructured.Unstructured{}
		cplnSecret.SetGroupVersionKind(common.CplnSecretGVK)
	}
	op := operatorStatus(cr)
	validationError, ok := op["validationError"].(string)
	childAnnotations := cplnSecret.GetAnnotations()
	if childAnnotations == nil {
		childAnnotations = map[string]string{}
	}
	if ok && validationError != "" {
		childAnnotations["cpln.io/sync-health-message"] = validationError
		childAnnotations["cpln.io/sync-health-status"] = "Degraded"
	} else {
		childAnnotations["cpln.io/sync-health-status"] = "Healthy"
		delete(childAnnotations, "cpln.io/sync-health-message")
	}

	parentAnnotations := cr.GetAnnotations()
	if parentAnnotations == nil {
		parentAnnotations = map[string]string{}
	}

	cplnSecret.SetAnnotations(childAnnotations)
	cplnSecret.SetNamespace(cr.GetNamespace())
	cplnSecret.SetName(cr.GetName())
	cplnSecret.SetOwnerReferences([]v1.OwnerReference{
		{
			APIVersion: cr.GetAPIVersion(),
			Kind:       cr.GetKind(),
			Name:       cr.GetName(),
			UID:        cr.GetUID(),
		},
	})
	cplnSecret.Object["org"] = parentAnnotations["cpln.io/org"]

	status, _, _ := unstructured.NestedFieldCopy(cr.Object, "status")
	if err := unstructured.SetNestedField(cplnSecret.Object, status, "status"); err != nil {
		return nil, err
	}
	return cplnSecret, nil
}
