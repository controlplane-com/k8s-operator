package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/k8s-operator/pkg/realtime"
	"github.com/controlplane-com/k8s-operator/pkg/websocket"
	"github.com/controlplane-com/types-go/pkg/deployment"
	"github.com/evanphx/json-patch/v5"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"net/http"
	"os"
	"path"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"slices"
	"strings"
	"sync"
	"time"
)

type CplnCRDController struct {
	client.Client
	Scheme     *runtime.Scheme
	HttpClient *http.Client
	gvk        schema.GroupVersionKind
	apiUrl     string
}

var zeroResult = ctrl.Result{}

var defaultResult = ctrl.Result{
	RequeueAfter: time.Second * time.Duration(common.GetEnvInt("RECONCILE_INTERVAL_SECONDS", 30)),
}

var ignoredFields = []string{"version", "gvc", "lastModified"}
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
var secrets = map[string]*corev1.Secret{}
var m = &sync.Mutex{}

func BuildControllers(mgr ctrl.Manager) error {
	if !common.GetEnvBool("CONTROLLER_ENABLED", true) {
		return nil
	}
	configuredKinds := common.GetEnvSlice[string]("MANAGE_KINDS", nil)
	gvks, err := listGVKForCRDs()
	if err != nil {
		return err
	}
	for _, gvk := range gvks {
		if configuredKinds != nil && !slices.Contains(configuredKinds, gvk.Kind) {
			continue
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		url := os.Getenv("CPLN_API_URL")
		if url == "" {
			url = "https://api.cpln.io"
		}
		r := &CplnCRDController{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			gvk:    gvk,
			apiUrl: url,
		}
		err = ctrl.NewControllerManagedBy(mgr).Named(fmt.Sprintf("%s_controller", gvk.Kind)).For(obj).Complete(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func listGVKForCRDs() ([]schema.GroupVersionKind, error) {
	var gvks []schema.GroupVersionKind
	entries, err := os.ReadDir("chart/templates/crd")
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		kind := strings.ToLower(strings.Split(e.Name(), ".")[0])
		if slices.Contains(ignoredKinds, kind) {
			continue
		}
		b, err := os.ReadFile(path.Join("chart/templates/crd", e.Name()))
		if err != nil {
			return nil, err
		}
		var crd v1.CustomResourceDefinition
		if err = yaml.Unmarshal(b, &crd); err != nil {
			return nil, err
		}
		gvks = append(gvks, schema.GroupVersionKind{
			Group:   common.API_GROUP,
			Version: common.API_REVISION,
			Kind:    crd.Spec.Names.Kind,
		})
	}
	return gvks, nil
}

func (r *CplnCRDController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	cr, err := r.getCustomResource(ctx, req)
	if err != nil || cr == nil {
		return zeroResult, err
	}
	org, gvc, err := r.getCplnContext(ctx, req, cr)
	if err != nil {
		return zeroResult, err
	}
	token, err := r.getSecret(ctx, org)
	if err != nil {
		return zeroResult, err
	}

	md, ok := cr.Object["metadata"].(map[string]any)
	if !ok {
		l.Info("CR has no metadata? skipping")
		return zeroResult, nil
	}

	nextSync, timeUntilSync := timeUntilNextSync(cr)
	if timeUntilSync > 0 {
		l.Info(getSyncFailureMessage(nextSync))
		return ctrl.Result{
			RequeueAfter: timeUntilSync,
		}, nil
	}

	deletedTimestamp := md["deletionTimestamp"]
	if deletedTimestamp != nil {
		return r.handleResourceDeletion(ctx, org, gvc, cr, token, l)
	}

	if err = r.syncChildren(ctx, req, org, gvc, token, cr); err != nil {
		return zeroResult, err
	}

	g := generation(cr)
	st := operatorStatus(cr)
	cplnLastSynced, _ := st["lastSyncedGeneration"].(int64)
	if cplnLastSynced == g {
		return r.syncFromCplnToK8s(ctx, l, org, gvc, token, cr)
	} else {
		return r.syncFromK8sToCpln(ctx, l, org, gvc, token, cr)
	}
}

func (r *CplnCRDController) handleResourceDeletion(ctx context.Context, org string, gvc string, cr *unstructured.Unstructured, token string, l logr.Logger) (ctrl.Result, error) {
	if err := r.cleanupSync(org, gvc, cr); err != nil {
		return zeroResult, err
	}
	if resourcePolicy(cr) != common.RESOURCE_POLICY_KEEP {
		err := r.deleteFromCpln(ctx, org, gvc, token, cr)
		if err != nil {
			l.Error(err, "Failed to delete from Cpln")
			if errors.Is(err, dependentResourceErr) {
				return defaultResult, nil
			}
			return zeroResult, err
		}
	}
	if err := r.removeFinalizer(ctx, cr); err != nil {
		return zeroResult, err
	}
	return zeroResult, nil
}

func (r *CplnCRDController) getCustomResource(ctx context.Context, req ctrl.Request) (*unstructured.Unstructured, error) {
	l := log.FromContext(ctx)
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(r.gvk)

	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if metav1.IsNotFound(err) {
			l.Info("CRD resource not found; must have been deleted")
			return nil, nil
		}
		return nil, err
	}
	return cr, nil
}

func (r *CplnCRDController) cleanupSync(org, gvc string, cr *unstructured.Unstructured) error {
	switch r.gvk.Kind {
	case common.KIND_WORKLOAD:
		return realtime.DeregisterSync(fmt.Sprintf("%s.%s.%s", org, gvc, cr.GetName()))
	default:
		return nil
	}
}

func (r *CplnCRDController) syncChildren(ctx context.Context, req ctrl.Request, org, gvc, token string, cr *unstructured.Unstructured) error {
	syncCtx := newSyncContext(ctx, r.Client)
	syncCtx.namespace = req.Namespace
	syncCtx.parent = cr

	switch r.gvk.Kind {
	case common.KIND_WORKLOAD:
		if err := r.websocketDeploymentSync(org, gvc, token, cr); err != nil {
			return err
		}
		/*
			deployments, err := r.getWorkloadDeploymentsFromCpln(ctx, org, gvc, token, cr)
			if err != nil && !errors.Is(err, notFoundError) {
				return err
			}
			if err = syncWorkloadDeployments(syncCtx, deployments); err != nil {
				return err
			}
		*/
		break
	case common.KIND_VOLUME_SET:
		if err := syncVolumeSetStatusLocations(syncCtx, cr); err != nil {
			return err
		}
		break
	}
	return nil
}

func (r *CplnCRDController) websocketDeploymentSync(org string, gvc string, token string, cr *unstructured.Unstructured) error {
	fullName := fmt.Sprintf("%s.%s.%s", org, gvc, cr.GetName())
	s := realtime.GetSync(fullName)
	if s != nil {
		return nil
	}
	ctx := context.Background()
	url := common.GetEnvStr("CPLN_WORKLOAD_STATUS_URL", "wss://workload-status.cpln.io/register")
	parent := cr.DeepCopy()
	l := log.FromContext(ctx)
	w, err := websocket.NewClient(ctx, l, url, token, time.Second*5, func(message []byte) error {
		syncCtx := newSyncContext(ctx, r.Client)
		if err := r.verifyParent(ctx, parent); err != nil {
			return nil
		}
		syncCtx.parent = parent
		syncCtx.namespace = parent.GetNamespace()
		deployments, err := r.getWorkloadDeploymentsFromCpln(ctx, org, gvc, token, parent)
		if err != nil {
			l.Error(err, "Failed to get deployments from Cpln")
			return nil
		}
		return syncWorkloadDeployments(syncCtx, deployments)
	})
	if err != nil {
		return err
	}
	realtime.RegisterSync(fullName, w)
	return registerInterest(w, token, Interest{
		Org:      org,
		Gvc:      gvc,
		Workload: cr.GetName(),
	})
}

func registerInterest(w websocket.Client, token string, interest Interest) error {
	b, err := json.Marshal(RegisterInterestRequest{
		Token:     token,
		Interests: []Interest{interest},
	})
	if err != nil {
		return err
	}
	return w.Send(b)
}

func (r *CplnCRDController) verifyParent(ctx context.Context, parent *unstructured.Unstructured) error {
	l := log.FromContext(ctx)
	var currentParent unstructured.Unstructured
	currentParent.SetGroupVersionKind(parent.GroupVersionKind())
	//For safety, verify that the parent the websocket client was configured with still matches the one in the cluster
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: parent.GetNamespace(), Name: parent.GetName()}, &currentParent); err != nil {
		//Do nothing. Can't verify parent identity
		l.Error(err, "Failed to get parent")
		return err
	}
	if parent.GetUID() != currentParent.GetUID() {
		err := errors.New("parent UID has changed. Ignoring deployments")
		l.Error(err, "")
		return err
	}
	return nil
}

func parseDeployment(message []byte) (*deployment.Deployment, error) {
	event := &realtime.Message[deployment.Deployment]{}
	err := json.Unmarshal(message, event)
	var d deployment.Deployment
	if err != nil || event.Id == "" {
		err = json.Unmarshal(message, &d)
		if err != nil {
			return nil, err
		}
	} else {
		d = event.Data
	}
	return &d, nil
}

func (r *CplnCRDController) getSecret(ctx context.Context, org string) (string, error) {
	m.Lock()
	defer m.Unlock()
	l := log.FromContext(ctx)
	secret, _ := secrets[org]
	if secret == nil {
		secret = &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: "controlplane",
			Name:      fmt.Sprintf("%s", org),
		}, secret); err != nil {
			return "", fmt.Errorf("unable to sync resources because the secret %s could not be found. Details: %v", org, err)
		}
		secrets[org] = secret
	}

	token := string(secret.Data["token"])
	if token == "" {
		// If missing, we can't do anything
		msg := "secret missing required field: token'"
		l.Error(nil, msg)
		return "", fmt.Errorf(msg)
	}
	return token, nil
}

// updateCRDSpec is a simple method to update the CRD's spec (unstructured).
func (r *CplnCRDController) updateCRDSpec(ctx context.Context, crdObj *unstructured.Unstructured) error {
	return r.Client.Update(ctx, crdObj)
}

func (r *CplnCRDController) syncFromCplnToK8s(ctx context.Context, log logr.Logger, org string, gvc string, token string, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration == generation, pulling from Control Plane")

	cplnResource, err := r.getCplnResource(ctx, org, gvc, token, cr)
	if err != nil && !errors.Is(err, notFoundError) {
		log.Error(err, "Error fetching from Control Plane")
		return zeroResult, err
	}
	if errors.Is(err, notFoundError) {
		log.Info("Resource not found on Control Plane, deleting from Kubernetes")
		if err := r.Client.Delete(ctx, cr); err != nil {
			log.Error(err, "Error deleting from Kubernetes")
		}
	}

	var cplnResourceMap map[string]any
	err = json.Unmarshal(cplnResource, &cplnResourceMap)
	if err != nil {
		log.Info(fmt.Sprintf("Got non-JSON response from Control Plane: %s", cplnResource))
		return defaultResult, nil
	}

	url, err := r.getCplnResourceUrl(org, gvc, cr)
	if err != nil {
		return zeroResult, err
	}

	cplnObj, err := toCplnFormat(cr)
	if err != nil {
		return zeroResult, err
	}

	cplnResourceAfterDryRun, err := r.putToCpln(ctx, log, token, url, cr, cplnObj, true)
	if err != nil {
		log.Error(err, "Error during cpln dry run")
		return zeroResult, err
	}

	patch, err := jsonpatch.CreateMergePatch([]byte(cplnResourceAfterDryRun), cplnResource)
	if err != nil {
		log.Error(err, "Error creating merge patch")
		return zeroResult, err
	}

	patchMap := map[string]any{}
	if err = json.Unmarshal(patch, &patchMap); err != nil {
		log.Error(err, "Error unmarshalling patch")
		return zeroResult, err
	}
	for _, field := range ignoredFields {
		delete(patchMap, field)
	}
	patch, err = json.Marshal(patchMap)
	if err != nil {
		log.Error(err, "Error marshalling patch")
		return zeroResult, err
	}

	//No changes
	if len(patch) == 0 || string(patch) == "{}" {
		if err := updateStatusWithSyncSuccess(ctx, r.Client, cr, cplnResourceMap["status"]); err != nil {
			log.Error(err, "Failed to update lastSyncedGeneration after pulling from Control Plane")
			return zeroResult, err
		}
		return defaultResult, nil
	}

	localSpec, err := json.Marshal(cplnObj)
	if err != nil {
		log.Error(err, "Could not marshal crd as JSON")
		return zeroResult, err
	}

	patchedSpecJson, err := jsonpatch.MergePatch(localSpec, patch)
	if err != nil {
		log.Error(err, "Could not merge patch")
		return zeroResult, err
	}

	var patchedSpec map[string]any
	err = json.Unmarshal(patchedSpecJson, &patchedSpec)
	if err != nil {
		log.Error(err, "Could not unmarshal patched spec as JSON")
		return zeroResult, err
	}
	cr, err = toK8sFormat(cr, org, gvc, patchedSpec)
	if err != nil {
		return zeroResult, err
	}

	if err := r.updateCRDSpec(ctx, cr); err != nil {
		log.Error(err, "Failed to patch CRD with new .spec from Control Plane")
		return zeroResult, err
	}

	for {
		err := updateStatusWithSyncSuccess(ctx, r.Client, cr, cplnResourceMap["status"])
		if err == nil {
			break
		}
		var apiErr *metav1.StatusError
		if errors.As(err, &apiErr) && apiErr.Status().Code == http.StatusConflict {
			log.Info("Conflict detected during status update, retrying")
			err := r.Client.Get(ctx, types.NamespacedName{
				Namespace: cr.GetNamespace(),
				Name:      cr.GetName(),
			}, cr)
			if err != nil {
				return zeroResult, err
			}
			continue
		}
		log.Error(err, "Failed to update lastSyncedGeneration after pulling from Control Plane")
		return zeroResult, err
	}

	return defaultResult, nil
}

func (r *CplnCRDController) syncFromK8sToCpln(ctx context.Context, log logr.Logger, org string, gvc string, token string, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration != generation, pushing to Control Plane")

	url, err := r.getCplnResourceUrl(org, gvc, cr)
	if err != nil {
		return zeroResult, err
	}

	cplnObj, err := toCplnFormat(cr)
	if err != nil {
		return zeroResult, err
	}
	cplnResourceAfterUpdate, err := r.putToCpln(ctx, log, token, url, cr, cplnObj, false)
	if err != nil {
		log.Error(err, "Failed to PUT resource to Control Plane")
		return zeroResult, err
	}

	if strings.TrimSpace(cplnResourceAfterUpdate) == "" {
		b, err := r.getCplnResource(ctx, org, gvc, token, cr)
		if err != nil {
			log.Error(err, "Failed to GET resource from Control Plane")
			return zeroResult, err
		}
		cplnResourceAfterUpdate = string(b)
	}

	responseMap := map[string]any{}
	if err := json.Unmarshal([]byte(cplnResourceAfterUpdate), &responseMap); err != nil {
		log.Error(err, fmt.Sprintf("could not unmarshal response from Control Plane: %s", cplnResourceAfterUpdate))
		return zeroResult, err
	}

	if err := updateStatusWithSyncSuccess(ctx, r.Client, cr, responseMap["status"]); err != nil {
		log.Error(err, "Failed to update lastSyncedGeneration after pushing to Control Plane")
		return zeroResult, err
	}
	return defaultResult, nil
}

func (r *CplnCRDController) removeFinalizer(ctx context.Context, cr *unstructured.Unstructured) error {
	finalizers := cr.GetFinalizers()
	if len(finalizers) == 0 {
		return nil
	}

	updatedFinalizers := []string{}
	for _, finalizer := range finalizers {
		if finalizer != common.FINALIZER {
			updatedFinalizers = append(updatedFinalizers, finalizer)
		}
	}

	cr.SetFinalizers(updatedFinalizers)

	if err := r.Client.Update(ctx, cr); err != nil {
		return err
	}

	return nil
}
