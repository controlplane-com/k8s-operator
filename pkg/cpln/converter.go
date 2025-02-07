package cpln

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

type Converter interface {
	K8sFormat(ctx Context, cr *unstructured.Unstructured, cplnObj map[string]any) (*unstructured.Unstructured, error)

	CplnFormat(cr *unstructured.Unstructured) (map[string]any, error)
}
