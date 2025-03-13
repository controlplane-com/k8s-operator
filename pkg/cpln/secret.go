package cpln

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type secretUrlProvider struct {
	UrlProvider
}

func NewSecretConnector(client client.Client, apiUrl string) Connector {
	s := &secretConnector{
		Connector: NewGenericConnector(client, apiUrl),
	}
	s.InjectUrlProvider(&secretUrlProvider{
		UrlProvider: NewGenericUrlProvider(apiUrl),
	})
	s.InjectConverter(NewSecretConverter())
	return s
}

type secretConnector struct {
	Connector
}

func (s *secretConnector) Context(ctx context.Context, cr *unstructured.Unstructured) (Context, error) {
	copied := cr.DeepCopy()
	a := copied.GetAnnotations()
	if a == nil {
		a = make(map[string]string)
	}
	copied.Object["org"] = a["cpln.io/org"]
	return s.Connector.Context(ctx, copied)
}

func (s *secretUrlProvider) ReadUrl(ctx Context, crdObj *unstructured.Unstructured) string {
	return fmt.Sprintf("%s/-reveal", s.UrlProvider.ReadUrl(ctx, crdObj))
}

type secretConverter struct {
	genericConverter Converter
}

func (s secretConverter) K8sFormat(ctx Context, cr *unstructured.Unstructured, cplnObj map[string]any) (*unstructured.Unstructured, error) {
	cr, err := s.genericConverter.K8sFormat(ctx, cr, cplnObj)
	if err != nil {
		return nil, err
	}
	l := cr.GetLabels()
	if l == nil {
		l = make(map[string]string)
	}
	l["app.kubernetes.io/managed-by"] = "cpln-operator"
	cr.SetLabels(l)
	a := cr.GetAnnotations()
	if a == nil {
		a = make(map[string]string)
	}
	a["cpln.io/org"] = ctx.Org()
	cr.Object["type"] = strings.ToLower(cr.Object["type"].(string))
	cr.SetKind(common.KIND_NATIVE_SECRET)

	//Argo doesn't track changes in labels as drift, so we store the tags as annotations instead
	StoreTagsAsAnnotations(cr, cplnObj)

	cr.SetAnnotations(a)
	data := cr.Object["data"].(map[string]any)
	for k, v := range data {
		var encoded = make([]byte, base64.StdEncoding.EncodedLen(len(v.(string))))
		base64.StdEncoding.Encode(encoded, []byte(v.(string)))
		data[k] = string(encoded)
	}
	return cr, nil
}

func (s secretConverter) CplnFormat(cr *unstructured.Unstructured) (map[string]any, error) {
	cplnResource, err := s.genericConverter.CplnFormat(cr)
	if err != nil {
		return nil, err
	}
	cplnResource["kind"] = "secret"
	cplnResource["type"] = strings.ToLower(cplnResource["type"].(string))

	ReadTagsFromAnnotations(cr, cplnResource)

	data := cplnResource["data"].(map[string]any)
	for k, v := range data {
		var decoded = make([]byte, base64.StdEncoding.DecodedLen(len(v.(string))))
		n, err := base64.StdEncoding.Decode(decoded, []byte(v.(string)))
		if err != nil {
			return nil, err
		}
		data[k] = string(decoded[:n])
	}
	return cplnResource, nil
}

func NewSecretConverter() Converter {
	return &secretConverter{
		//We convert to/from core/v1 Secrets
		genericConverter: NewGenericConverter("v1"),
	}
}
