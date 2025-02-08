package mutators

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"slices"
	"strings"
)

type CrMutator struct {
}

var ignoredKinds = []string{
	common.KIND_DEPLOYMENT,
	common.KIND_DEPLOYMENT_VERSION,
	common.KIND_CONTAINER_STATUS,
	common.KIND_JOB_EXECUTION_STATUS,
	common.KIND_VOLUME_SET_STATUS_LOCATION,
	common.KIND_PERSISTENT_VOLUME_STATUS,
	common.KIND_IMAGE,
	common.KIND_USER,
}

func (c CrMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("No mutation for non-create/update")
	}
	u := &unstructured.Unstructured{
		Object: make(map[string]any),
	}
	err := json.Unmarshal(req.Object.Raw, &u.Object)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("could not unmarshal raw object: %v", err))
	} // We navigate to metadata.finalizers in the unstructured object
	kind := u.GetKind()
	if slices.Contains(ignoredKinds, kind) {
		return admission.Allowed("kind is ignored - ignoring")
	}
	labels := u.GetLabels()
	deletionTimestamp := u.GetDeletionTimestamp()
	if strings.ToLower(kind) == "secret" && u.GetAPIVersion() == "v1" {
		if len(labels) == 0 {
			return admission.Allowed("no labels field - ignoring")
		}
		if labels["app.kubernetes.io/managed-by"] != "cpln-operator" {
			return admission.Allowed("resource is not managed by cpln-operator - ignoring")
		}
	}

	if deletionTimestamp != nil {
		return admission.Allowed("resource has been deleted - ignoring")
	}

	finalizers := u.GetFinalizers()
	if finalizers == nil {
		finalizers = []string{}
	}

	// Check if our required finalizer is present
	found := false
	for _, f := range finalizers {
		if f == common.FINALIZER {
			found = true
			break
		}
	}

	if !found {
		// add finalizer
		finalizers = append(finalizers, common.FINALIZER)
		u.SetFinalizers(finalizers)
	}

	// Re-marshal to JSON
	marshaledObj, err := json.Marshal(u.Object)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to marshal mutated object: %v", err))
	}

	// Return a PatchResponse which modifies the request object
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
}
