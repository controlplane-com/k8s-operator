package main

import (
	"fmt"
	"log"
	"os"
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

		fileName := f.Name()
		base := strings.TrimSuffix(fileName, ".yaml")
		lowerKind := strings.ToLower(base)

		groupKind := fmt.Sprintf("      cpln.io/%s", lowerKind)

		customizationsBuilder.WriteString(fmt.Sprintf("  %s:\n", groupKind))
		customizationsBuilder.WriteString("          health.lua: |\n")

		for _, line := range strings.Split(strings.TrimSuffix(string(healthCheckScript), "\n"), "\n") {
			customizationsBuilder.WriteString("            " + line + "\n")
		}

		// Add ignoreDifferences so ArgoCD ignores .metadata.ownerReferences
		customizationsBuilder.WriteString("\n          ignoreDifferences: |-\n")
		customizationsBuilder.WriteString("            jsonPointers:\n")
		customizationsBuilder.WriteString("              - /metadata/ownerReferences\n\n")
	}

	yaml := fmt.Sprintf(`apiVersion: v1
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

	if err := os.WriteFile(outputFile, []byte(yaml), 0644); err != nil {
		log.Fatalf("Failed to write patch file: %v", err)
	}

	log.Printf("Generated patch file: %s\n", outputFile)
}
