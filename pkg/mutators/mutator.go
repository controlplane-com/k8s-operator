package mutators

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	admissionv1 "k8s.io/api/admission/v1"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"slices"
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
	var unstructuredObj map[string]interface{}
	err := json.Unmarshal(req.Object.Raw, &unstructuredObj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("could not unmarshal raw object: %v", err))
	} // We navigate to metadata.finalizers in the unstructured object
	kind := unstructuredObj["kind"].(string)
	if slices.Contains(ignoredKinds, kind) {
		return admission.Allowed("kind is ignored - ignoring")
	}
	metadata, ok := unstructuredObj["metadata"].(map[string]interface{})
	if !ok {
		return admission.Allowed("no metadata field - ignoring")
	}

	if metadata["deletionTimestamp"] != nil {
		return admission.Allowed("resource has been deleted - ignoring")
	}

	finalizers, ok := metadata["finalizers"].([]interface{})
	if !ok {
		// if finalizers is nil or not an array, we create an empty array
		finalizers = make([]interface{}, 0)
	}

	// Check if our required finalizer is present
	found := false
	for _, fz := range finalizers {
		if str, ok := fz.(string); ok && str == common.FINALIZER {
			found = true
			break
		}
	}

	if !found {
		// add finalizer
		finalizers = append(finalizers, common.FINALIZER)
		metadata["finalizers"] = finalizers
	}

	// Re-marshal to JSON
	marshaledObj, err := json.Marshal(unstructuredObj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to marshal mutated object: %v", err))
	}

	// Return a PatchResponse which modifies the request object
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)
}
