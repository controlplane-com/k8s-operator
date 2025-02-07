package cpln

import (
	"encoding/json"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/types-go/pkg/base"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"io"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net/http"
)

var k8sBoilerplate = map[string]bool{
	"apiVersion": false,
	"metadata":   false,
}

func Name(cr *unstructured.Unstructured) string {
	m, ok := cr.Object["metadata"].(map[string]any)
	if !ok {
		return cr.GetName()
	}
	annotations, ok := m["annotations"].(map[string]any)
	if !ok {
		return cr.GetName()
	}
	replacement, ok := annotations["cpln.io/name-replacement"].(string)
	if !ok {
		return cr.GetName()
	}
	return replacement
}

func GetWorkloadDeploymentsFromCpln(ctx Context, connector Connector, cr *unstructured.Unstructured) ([]deployment.Deployment, error) {
	url := connector.ReadUrl(ctx, cr)
	url = fmt.Sprintf("%s/%s", url, "deployment")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ctx.Token())
	c := http.DefaultClient
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, common.NotFoundError
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s -> status %d: %s", url, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var deployments base.GenericList[deployment.Deployment]
	return deployments.Items, json.Unmarshal(body, &deployments)
}
