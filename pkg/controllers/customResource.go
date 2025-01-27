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
	"strings"
)

var k8sBoilerplate = map[string]bool{
	"apiVersion": false,
	"metadata":   false,
}

func isGvcScoped(kind string) bool {
	switch kind {
	case "workload":
		return true
	case "volumeset":
		return true
	case "identity":
		return true
	default:
		return false
	}
}

func nameToCplnFormat(kind, name string) (string, string, error) {
	p := strings.Split(name, ".")
	if !isGvcScoped(kind) {
		return name, "", nil
	}
	if len(p) != 2 {
		return "", "", fmt.Errorf("invalid name: %s. Name must be of the form {name}.{gvc} for gvc-scoped kinds", name)
	}
	return p[0], p[1], nil
}

func nameToK8sFormat(kind, name, gvc string) (string, error) {
	if !isGvcScoped(kind) {
		return name, nil
	}
	if gvc == "" {
		return "", fmt.Errorf("gvc is required for kind %s", kind)
	}
	return fmt.Sprintf("%s.%s", name, gvc), nil
}

func toCplnFormat(cr *unstructured.Unstructured) (map[string]any, error) {
	//convert metadata into name/gvc
	//remove all k8s boilerplate
	cpln := map[string]any{}
	name, gvc, err := nameToCplnFormat(cr.GetKind(), cr.GetName())
	if err != nil {
		return nil, err
	}
	cpln["name"] = name
	cpln["gvc"] = gvc
	for key, prop := range cr.Object {
		if keep, ok := k8sBoilerplate[key]; ok && !keep {
			continue
		}
		cpln[key] = prop
	}
	return cpln, nil
}

func toK8sFormat(template *unstructured.Unstructured, cpln map[string]any) (*unstructured.Unstructured, error) {
	//convert name/gvc into metadata name and namespace
	//add k8s boilerplate

	name, ok := cpln["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name is required, and must be a string. Given name: %v", cpln["name"])
	}
	kind, ok := cpln["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("kind is required, and must be a string. Given name: %v", cpln["name"])
	}
	gvc, ok := cpln["gvc"].(string)
	if !ok {
		gvc = ""
	}
	k8sName, err := nameToK8sFormat(kind, name, gvc)
	if err != nil {
		return nil, err
	}

	cr := &unstructured.Unstructured{
		Object: map[string]any{},
	}
	for key, val := range cpln {
		cr.Object[key] = val
	}
	cr.SetAPIVersion(common.API_VERSION)
	cr.SetResourceVersion(template.GetResourceVersion())
	cr.SetName(k8sName)
	cr.SetNamespace(template.GetNamespace())
	return cr, nil
}

func unstructuredCR(gvk schema.GroupVersionKind, namespace, name string, body any, parent *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}

	j, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}
	err = json.Unmarshal(j, &spec)
	if err != nil {
		return nil, err
	}
	obj.Object = spec
	obj.SetGroupVersionKind(gvk)
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
			statusUpdate := buildStatusUpdate(d)
			if err = ctx.c.Status().Update(ctx, statusUpdate); err != nil {
				return deletedNames, err
			}
			d.SetResourceVersion(statusUpdate.GetResourceVersion())
			if err = ctx.c.Update(ctx, d); err != nil {
				return deletedNames, err
			}
			continue
		}
		//Create missing CRs
		if err = ctx.c.Create(ctx, d); err != nil {
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
