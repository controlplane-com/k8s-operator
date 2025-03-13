package main

import (
	"fmt"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	crdDir := "chart/templates/crd"
	outputFile := "chart/templates/08-argocd-cm.yaml"

	// 1) Read all YAML files in the crdDir
	files, err := os.ReadDir(crdDir)
	if err != nil {
		log.Fatalf("Failed to read CRD directory: %v", err)
	}

	// 2) Build up resource customizations text
	var customizationsBuilder strings.Builder

	healthCheckScript, err := os.ReadFile("scripts/healthCheck.lua")

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}

		var crd v1.CustomResourceDefinition
		b, err := os.ReadFile(filepath.Join(crdDir, f.Name()))
		if err != nil {
			log.Fatalf("Failed to read CRD file: %v", err)
		}
		if err = yaml.Unmarshal(b, &crd); err != nil {
			log.Fatalf("Failed to unmarshal CRD file: %v", err)
		}

		groupKind := fmt.Sprintf("      cpln.io/%s", crd.Spec.Names.Kind)

		customizationsBuilder.WriteString(fmt.Sprintf("  %s:\n", groupKind))
		customizationsBuilder.WriteString("          health.lua: |\n")

		for _, line := range strings.Split(strings.TrimSuffix(string(healthCheckScript), "\n"), "\n") {
			customizationsBuilder.WriteString("            " + line + "\n")
		}
	}

	configMapYaml := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm-patch
  namespace: controlplane
data:
  argocd-cm-patch.yaml: |-
    data:
      resource.customizations: |-
%s
`, customizationsBuilder.String())

	if err := os.WriteFile(outputFile, []byte(configMapYaml), 0644); err != nil {
		log.Fatalf("Failed to write patch file: %v", err)
	}

	log.Printf("Generated patch file: %s\n", outputFile)
}
