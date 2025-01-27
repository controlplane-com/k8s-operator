// scripts/generate_rbac.go
package main

import (
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"os"
	"path/filepath"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	sigYaml "sigs.k8s.io/yaml" // separate package "sigs.k8s.io/yaml" for marshalling
)

// GenerateRBAC reads all CRDs in chart/crd, then generates a single
// ClusterRole that grants get, list, watch on each CRD resource.
func main() {
	crdDir := "chart/crd"
	outputFile := "chart/templates/00-dynamic-rbac.yaml"

	// 1. Read all YAML files in chart/crd
	files, err := os.ReadDir(crdDir)
	if err != nil {
		log.Fatalf("Failed to read CRD directory: %v", err)
	}

	// We will collect all resource rules here
	var rules []rbacv1.PolicyRule

	for _, file := range files {
		// Skip dirs and non-yaml
		if file.IsDir() {
			continue
		}
		if filepath.Ext(file.Name()) != ".yaml" && filepath.Ext(file.Name()) != ".yml" {
			continue
		}

		filePath := filepath.Join(crdDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", filePath, err)
		}

		// 2. Parse as a CRD
		crd := &apiextensionsv1.CustomResourceDefinition{}

		// We must decode YAML -> JSON -> object
		jsonBytes, err := yaml.ToJSON(data)
		if err != nil {
			log.Fatalf("Failed to convert YAML to JSON for %s: %v", filePath, err)
		}

		if err := yaml.Unmarshal(jsonBytes, crd); err != nil {
			log.Fatalf("Failed to unmarshal CRD from file %s: %v", filePath, err)
		}

		// 3. Extract group and resource (plural) from the CRD
		apiGroup := crd.Spec.Group
		resource := crd.Spec.Names.Plural

		// 4. Create a PolicyRule for this CRD that allows get, list, watch
		rule := rbacv1.PolicyRule{
			APIGroups: []string{apiGroup},
			Resources: []string{resource, resource + "/status"},
			Verbs:     []string{"*"},
		}

		rules = append(rules, rule)
	}

	// 5. Construct a single ClusterRole. You could do multiple if needed.
	clusterRole := rbacv1.ClusterRole{
		TypeMeta: v1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: v1.ObjectMeta{
			Name: "controlplane-operator-crds",
		},
		Rules: rules,
	}

	// 6. Marshal to YAML
	out, err := sigYaml.Marshal(clusterRole)
	if err != nil {
		log.Fatalf("Failed to marshal ClusterRole to YAML: %v", err)
	}

	// Ensure the output directory exists
	err = os.MkdirAll(filepath.Dir(outputFile), 0755)
	if err != nil {
		log.Fatalf("Failed to create directories for %s: %v", outputFile, err)
	}

	// 7. Write to chart/manifests/rbac/rbac.yaml
	err = os.WriteFile(outputFile, out, 0644)
	if err != nil {
		log.Fatalf("Failed to write RBAC file: %v", err)
	}

	fmt.Printf("Wrote RBAC file at %s\n", outputFile)
}
