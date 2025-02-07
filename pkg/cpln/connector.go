package cpln

import (
	"context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Connector interface {
	InjectUrlProvider(urlProvider UrlProvider)
	InjectConverter(converter Converter)

	Context(ctx context.Context, cr *unstructured.Unstructured) (Context, error)

	//Put works for create and update operations
	Put(ctx Context, cr *unstructured.Unstructured, dryRun bool) (string, error)

	Get(ctx Context, cr *unstructured.Unstructured) ([]byte, error)

	Delete(ctx Context, cr *unstructured.Unstructured) error

	UrlProvider
	Converter
}
