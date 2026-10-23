package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Reproduces the panic reported in production: a deployment whose "status"
// field is absent (or not a map) used to crash collectDeploymentMessages with
// "interface conversion: interface {} is nil, not []interface {}", because
// the loop over status["versions"] ran unconditionally instead of only when
// status was successfully asserted to a map.
func TestCollectDeploymentMessages_MissingStatus(t *testing.T) {
	deployments := []*unstructured.Unstructured{
		{Object: map[string]any{}}, // no "status" key at all
	}

	messages := collectDeploymentMessages(deployments)

	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %v", messages)
	}
}

func TestCollectDeploymentMessages_StatusWithoutVersions(t *testing.T) {
	deployments := []*unstructured.Unstructured{
		{Object: map[string]any{
			"status": map[string]any{"message": "some error"},
		}},
	}

	messages := collectDeploymentMessages(deployments)

	if len(messages) != 1 || messages[0] != "some error" {
		t.Fatalf("expected [\"some error\"], got %v", messages)
	}
}

func TestCollectDeploymentMessages_FullShape(t *testing.T) {
	deployments := []*unstructured.Unstructured{
		{Object: map[string]any{
			"status": map[string]any{
				"message": "deployment error",
				"versions": []any{
					map[string]any{
						"message": "version error",
						"containers": map[string]any{
							"app": map[string]any{"message": "container error"},
						},
					},
				},
			},
		}},
	}

	messages := collectDeploymentMessages(deployments)

	want := map[string]bool{"deployment error": true, "version error": true, "container error": true}
	if len(messages) != len(want) {
		t.Fatalf("expected %d messages, got %v", len(want), messages)
	}
	for _, m := range messages {
		if !want[m] {
			t.Fatalf("unexpected message %q in %v", m, messages)
		}
	}
}
