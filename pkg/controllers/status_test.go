package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Reproduces the production panic: some CR source structs (e.g.
// cronjob.JobExecutionStatus) have their own "Status" field that serializes
// into the same top-level "status" key as a plain string. ready/unhealthy/
// progressing/suspended and their is* predicates used to only check that the
// key existed, not that it was a map, and panicked with "interface
// conversion: interface {} is string, not map[string]interface {}".
func TestReady_StringStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{"status": "failed"}}

	ready(cr) // must not panic

	status, ok := cr.Object["status"].(map[string]any)
	if !ok {
		t.Fatalf("expected status to become a map, got %#v", cr.Object["status"])
	}
	if status["phase"] != "Ready" {
		t.Fatalf("expected phase Ready, got %v", status["phase"])
	}
}

func TestIsUnhealthy_StringStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{"status": "failed"}}

	if isUnhealthy(cr) {
		t.Fatal("expected isUnhealthy to return false for a non-map status, not panic")
	}
}

func TestProgressing_StringStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{"status": "pending"}}

	progressing(cr) // must not panic

	status := cr.Object["status"].(map[string]any)
	if status["phase"] != "Pending" {
		t.Fatalf("expected phase Pending, got %v", status["phase"])
	}
}

func TestSuspended_StringStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{"status": "suspended"}}

	suspended(cr) // must not panic

	status := cr.Object["status"].(map[string]any)
	if status["phase"] != "Suspended" {
		t.Fatalf("expected phase Suspended, got %v", status["phase"])
	}
}

func TestUnhealthy_StringStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{"status": "bad"}}

	unhealthy(cr) // must not panic

	status := cr.Object["status"].(map[string]any)
	if status["phase"] != "Unhealthy" {
		t.Fatalf("expected phase Unhealthy, got %v", status["phase"])
	}
}

func TestOperatorStatus_MissingStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{}}

	op := operatorStatus(cr) // must not panic
	op["lastSyncedGeneration"] = int64(1)

	status := cr.Object["status"].(map[string]any)
	if status["operator"].(map[string]any)["lastSyncedGeneration"] != int64(1) {
		t.Fatalf("expected operator status to be persisted, got %#v", status)
	}
}

func TestReady_NormalMapStatus(t *testing.T) {
	cr := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"phase": "Pending"},
	}}

	ready(cr)

	status := cr.Object["status"].(map[string]any)
	if status["phase"] != "Ready" {
		t.Fatalf("expected phase Ready, got %v", status["phase"])
	}
}
