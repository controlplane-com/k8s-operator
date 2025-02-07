package cpln

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

type UrlProvider interface {
	WriteUrl(ctx Context, cr *unstructured.Unstructured) string
	ReadUrl(ctx Context, cr *unstructured.Unstructured) string
}
