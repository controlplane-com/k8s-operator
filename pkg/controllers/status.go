package controllers

import (
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strconv"
	"strings"
	"time"
)

func resourcePolicy(cr *unstructured.Unstructured) string {
	m, ok := cr.Object["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	annotations, ok := m["annotations"].(map[string]any)
	if !ok {
		return ""
	}
	p, ok := annotations[common.RESOURCE_POLICY_ANNOTATION]
	if !ok {
		return ""
	}
	return p.(string)
}

func timeUntilNextSync(cr *unstructured.Unstructured) (time.Time, time.Duration) {
	st := operatorStatus(cr)
	_, ok := st["validationError"]
	if !ok {
		return time.Now().UTC(), time.Duration(0)
	}
	//If we haven't attempted to sync the current generation yet, we try immediately
	if st["lastProcessedGeneration"].(int64) != generation(cr) {
		return time.Now().UTC(), time.Duration(0)
	}
	nextRetry := getNextRetry(cr)
	return nextRetry, nextRetry.Sub(time.Now().UTC())
}

func getNextRetry(cr *unstructured.Unstructured) time.Time {
	st := operatorStatus(cr)
	lastRetryStr, _ := st["lastSyncTime"].(string)
	if lastRetryStr == "" {
		return time.Now().UTC()
	}
	lastRetry, err := time.Parse(time.RFC3339, lastRetryStr)
	if err != nil {
		return time.Now().UTC()
	}
	retries, _ := st["syncRetries"].(int64)
	nextRetry := lastRetry.Add(common.GetRetryDuration(int(retries)))
	return nextRetry
}

func getSyncFailureMessage(nextRetry time.Time) string {
	return fmt.Sprintf("resource status is in sync failure state. Sync will be retried after %s", nextRetry.Format(time.RFC3339))
}

func generation(cr *unstructured.Unstructured) int64 {
	metadata, ok := cr.Object["metadata"].(map[string]any)
	if !ok {
		return 1
	}
	g, ok := metadata["generation"]
	if !ok {
		return 1
	}
	switch s := g.(type) {
	case string:
		i, err := strconv.Atoi(s)
		if err != nil {
			return 1
		}
		return int64(i)
	case int64:
		return s
	case float64:
		return int64(s)
	default:
		return 1
	}
}

func operatorStatus(cr *unstructured.Unstructured) map[string]any {
	st, ok := cr.Object["status"].(map[string]any)
	if !ok {
		cr.Object["status"] = make(map[string]any)
		st = cr.Object["status"].(map[string]any)
	}
	_, ok = st["operator"].(map[string]any)
	if !ok {
		st["operator"] = map[string]any{}
	}
	cr.Object["status"] = st
	return cr.Object["status"].(map[string]any)["operator"].(map[string]any)
}

func isReady(cr *unstructured.Unstructured) bool {
	if _, ok := cr.Object["status"]; !ok {
		return false
	}
	status := cr.Object["status"].(map[string]any)
	if status["phase"] == "Ready" {
		return true
	}
	return false
}

func ready(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"]; !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Ready"
	status["conditions"] = []map[string]any{
		{
			"status": "True",
			"type":   "Ready",
		},
	}
	op := operatorStatus(cr)
	op["healthStatusMessage"] = ""
}

func isUnhealthy(cr *unstructured.Unstructured) bool {
	if _, ok := cr.Object["status"]; !ok {
		return false
	}
	status := cr.Object["status"].(map[string]any)
	if status["phase"] == "Unhealthy" {
		return true
	}
	return false
}

func addErrorMessages(cr *unstructured.Unstructured, errorMessages ...string) {
	status := operatorStatus(cr)
	status["healthStatusMessage"] = strings.Join(errorMessages, "\n")
}

func unhealthy(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"].(map[string]any); !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Unhealthy"
	status["conditions"] = []map[string]any{
		{
			"status": "False",
			"type":   "Ready",
		},
	}
}

func isProgressing(cr *unstructured.Unstructured) bool {
	if _, ok := cr.Object["status"]; !ok {
		return false
	}
	status := cr.Object["status"].(map[string]any)
	if status["phase"] == "Pending" {
		return true
	}
	return false
}

func progressing(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"]; !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Pending"
	status["conditions"] = []map[string]any{
		{
			"status": "False",
			"type":   "Ready",
		},
	}
}

func isSuspended(cr *unstructured.Unstructured) bool {
	if _, ok := cr.Object["status"]; !ok {
		return false
	}
	status := cr.Object["status"].(map[string]any)
	if status["phase"] == "Suspended" {
		return true
	}
	return false
}

func suspended(cr *unstructured.Unstructured) {
	if _, ok := cr.Object["status"]; !ok {
		cr.Object["status"] = map[string]any{}
	}
	status := cr.Object["status"].(map[string]any)
	status["phase"] = "Suspended"
	status["conditions"] = []map[string]any{
		{
			"status": "False",
			"type":   "Ready",
		},
	}
}

func synced(cr *unstructured.Unstructured, downstreamOnly bool, newStatus any) {
	o := operatorStatus(cr)
	g := generation(cr)
	o["lastSyncedGeneration"] = g
	o["lastProcessedGeneration"] = g
	o["downstreamOnly"] = downstreamOnly
	delete(o, "lastSyncTime")
	delete(o, "syncRetries")
	delete(o, "validationError")

	st := cr.Object["status"].(map[string]any)
	if m, ok := newStatus.(map[string]any); ok {
		for k, v := range m {
			if k == "operator" {
				continue
			}
			st[k] = v
		}
	}
}

func syncFailed(cr *unstructured.Unstructured, errorMessage string) {
	if errorMessage == "" {
		errorMessage = "sync failed, but no error message provided"
	}
	o := operatorStatus(cr)
	o["validationError"] = errorMessage
	o["lastProcessedGeneration"] = generation(cr)
	o["lastSyncTime"] = time.Now().UTC().Format(time.RFC3339)
	r, ok := o["syncRetries"].(int64)
	if !ok {
		o["syncRetries"] = int64(0)
	} else {
		o["syncRetries"] = r + 1
	}
}
