package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func unstructuredCR(gvk schema.GroupVersionKind, namespace, name string, body any, parent *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}

	j, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}
	err = json.Unmarshal(j, &spec)
	if name == "" && spec["name"] != nil {
		name = spec["name"].(string)
	}
	if err != nil {
		return nil, err
	}
	obj.Object = spec
	obj.SetGroupVersionKind(gvk)
	obj.Object["org"] = parent.Object["org"]
	if common.IsGvcScoped(gvk.Kind) {
		obj.Object["gvc"] = parent.Object["gvc"]
	}
	obj.SetName(fmt.Sprintf("%s.%s", name, parent.GetName()))
	obj.SetNamespace(namespace)
	obj.SetOwnerReferences([]v1.OwnerReference{
		{
			APIVersion: parent.GetAPIVersion(),
			Kind:       parent.GetKind(),
			Name:       parent.GetName(),
			UID:        parent.GetUID(),
		},
	})
	addLabel(obj, common.UID_LABEL, string(parent.GetUID()))
	return obj, nil
}

func syncCRs(ctx *syncContext, desiredChildren []*unstructured.Unstructured, childGvk schema.GroupVersionKind) ([]string, error) {
	var deletedNames []string
	existingChildren := &unstructured.UnstructuredList{}
	existingChildren.SetGroupVersionKind(childGvk)
	parentUID := ctx.parent.GetUID()
	err := ctx.c.List(context.Background(), existingChildren, &client.ListOptions{Namespace: ctx.namespace, LabelSelector: labels.SelectorFromSet(labels.Set{
		common.UID_LABEL: string(parentUID),
	})})
	if err != nil {
		return nil, err
	}

	//Build maps for efficiency
	desiredMap := map[string]*unstructured.Unstructured{}
	existingMap := map[string]*unstructured.Unstructured{}
	for _, d := range desiredChildren {
		desiredMap[d.GetName()] = d
	}
	for _, e := range existingChildren.Items {
		existingMap[e.GetName()] = &e
	}

	//Delete orphaned CRs
	for name, e := range existingMap {
		if _, ok := desiredMap[name]; ok {
			continue
		}
		if err = ctx.c.Delete(ctx, e); err != nil {
			return deletedNames, err
		}
		deletedNames = append(deletedNames, name)
	}

	for name, d := range desiredMap {
		if e, ok := existingMap[name]; ok {
			//Update existing CRs, preserving the uid label
			addLabel(d, common.UID_LABEL, e.GetLabels()[common.UID_LABEL])
			d.SetResourceVersion(e.GetResourceVersion())
			ds := buildStatusUpdate(d)
			if err = ctx.c.Update(ctx, d); err != nil {
				return deletedNames, err
			}
			ds.SetResourceVersion(d.GetResourceVersion())
			if err = ctx.c.Status().Update(ctx, ds); err != nil {
				return deletedNames, err
			}
			continue
		}
		//Create missing CRs
		if err = ctx.c.Create(ctx, d); err != nil {
			return deletedNames, err
		}
		if err = ctx.c.Status().Update(ctx, d); err != nil {
			return deletedNames, err
		}
	}

	return deletedNames, nil
}

func buildStatusUpdate(d *unstructured.Unstructured) *unstructured.Unstructured {
	statusUpdate := &unstructured.Unstructured{}
	statusUpdate.SetGroupVersionKind(d.GroupVersionKind())
	statusUpdate.SetName(d.GetName())
	statusUpdate.SetNamespace(d.GetNamespace())
	statusUpdate.SetResourceVersion(d.GetResourceVersion())
	statusUpdate.SetUID(d.GetUID())
	statusUpdate.SetOwnerReferences(d.GetOwnerReferences())
	statusUpdate.SetLabels(d.GetLabels())
	statusUpdate.SetAnnotations(d.GetAnnotations())
	statusUpdate.SetFinalizers(d.GetFinalizers())
	statusUpdate.Object["status"] = d.Object["status"]
	return statusUpdate
}

func addLabel(d *unstructured.Unstructured, key, value string) {
	l := d.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[key] = value
	d.SetLabels(l)
}
