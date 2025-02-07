package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/k8s-operator/pkg/cpln"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"slices"
	"strings"
	"time"
)

var ignoredKinds = []string{
	common.KIND_DEPLOYMENT,
	common.KIND_DEPLOYMENT_VERSION,
	common.KIND_CONTAINER_STATUS,
	common.KIND_JOB_EXECUTION_STATUS,
	common.KIND_VOLUME_SET_STATUS_LOCATION,
	common.KIND_PERSISTENT_VOLUME_STATUS,
	common.KIND_IMAGE,
	common.KIND_USER,
	common.KIND_CPLN_SECRET,
}

type controller struct {
	client.Client
	Scheme        *runtime.Scheme
	HttpClient    *http.Client
	gvk           schema.GroupVersionKind
	cplnConnector cpln.Connector
	k8sConnector  Connector
}

var zeroResult = ctrl.Result{}

var defaultResult = ctrl.Result{
	RequeueAfter: time.Second * time.Duration(common.GetEnvInt("RECONCILE_INTERVAL_SECONDS", 30)),
}

var ignoredFields = []string{"version", "gvc", "lastModified"}

func BuildControllers(mgr ctrl.Manager) error {
	if !common.GetEnvBool("CONTROLLER_ENABLED", true) {
		return nil
	}
	url := common.GetEnvStr("CPLN_API_URL", "https://api.cpln.io")
	if err := buildGenericControllers(mgr, url); err != nil {
		return err
	}
	return buildSpecializedControllers(mgr, url)
}

func buildGenericControllers(mgr ctrl.Manager, url string) error {
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
		r := &controller{
			cplnConnector: cpln.NewGenericConnector(mgr.GetClient(), url),
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			gvk:           gvk,
			k8sConnector:  NewGenericConnector(gvk, mgr.GetClient()),
		}
		err = ctrl.NewControllerManagedBy(mgr).Named(fmt.Sprintf("%s_controller", gvk.Kind)).For(obj).Complete(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func buildSpecializedControllers(mgr ctrl.Manager, url string) error {
	//TODO: add more specialized controllers here as needed
	return buildSecretController(mgr, url)
}

func buildSecretController(mgr ctrl.Manager, url string) error {
	secret := &corev1.Secret{}
	secret.SetGroupVersionKind(common.NativeSecretGVK)
	r := &controller{
		cplnConnector: cpln.NewSecretConnector(mgr.GetClient(), url),
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		gvk:           common.NativeSecretGVK,
		k8sConnector:  NewSecretConnector(mgr.GetClient()),
	}
	return ctrl.NewControllerManagedBy(mgr).Named("secret_controller").For(secret, builder.WithPredicates(syncPredicate())).Complete(r)
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

func (r *controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	cr, err := r.k8sConnector.Read(ctx, req.NamespacedName)
	if err != nil || cr == nil {
		return zeroResult, err
	}
	cplnContext, err := r.cplnConnector.Context(ctx, cr)
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
		return r.handleResourceDeletion(cplnContext, cr, l)
	}

	if err = r.syncChildren(cplnContext, req, cr); err != nil {
		return zeroResult, err
	}

	g := generation(cr)
	st := operatorStatus(cr)
	cplnLastSynced, _ := st["lastSyncedGeneration"].(int64)
	var result ctrl.Result
	if cplnLastSynced == g {
		result, err = r.syncFromCplnToK8s(cplnContext, l, cr)
	} else {
		result, err = r.syncFromK8sToCpln(cplnContext, l, cr)
	}
	if err != nil {
		syncFailed(cr, err.Error())
		if err := r.k8sConnector.WriteStatus(ctx, cr); err != nil {
			l.Error(err, "Error updating status with sync failure")
		}
	}
	return result, err
}

func (r *controller) handleResourceDeletion(ctx cpln.Context, cr *unstructured.Unstructured, l logr.Logger) (ctrl.Result, error) {
	if err := r.cleanupSync(ctx, cr); err != nil {
		return zeroResult, err
	}
	if resourcePolicy(cr) != common.RESOURCE_POLICY_KEEP {
		err := r.cplnConnector.Delete(ctx, cr)
		if err != nil {
			l.Error(err, "Failed to delete from Cpln")
			if errors.Is(err, common.DependentResourceErr) {
				return defaultResult, nil
			}
			return zeroResult, err
		}
	}
	if err := r.k8sConnector.Cleanup(ctx, cr); err != nil {
		return zeroResult, err
	}
	return zeroResult, nil
}

func (r *controller) cleanupSync(ctx cpln.Context, cr *unstructured.Unstructured) error {
	switch r.gvk.Kind {
	case common.KIND_WORKLOAD:
		return realtime.DeregisterSync(fmt.Sprintf("%s.%s.%s", ctx.Org(), ctx.Gvc(), cr.GetName()))
	default:
		return nil
	}
}

func (r *controller) syncChildren(ctx cpln.Context, req ctrl.Request, cr *unstructured.Unstructured) error {
	syncCtx := newSyncContext(ctx, r.Client)
	syncCtx.namespace = req.Namespace
	syncCtx.parent = cr

	switch r.gvk.Kind {
	case common.KIND_WORKLOAD:
		if err := r.websocketDeploymentSync(ctx, cr); err != nil {
			return err
		}
	case common.KIND_VOLUME_SET:
		if err := syncVolumeSetStatusLocations(syncCtx, cr); err != nil {
			return err
		}
		break
	}
	return nil
}

func (r *controller) websocketDeploymentSync(ctx cpln.Context, cr *unstructured.Unstructured) error {
	org := ctx.Org()
	gvc := ctx.Gvc()
	token := ctx.Token()
	fullName := fmt.Sprintf("%s.%s.%s", org, gvc, cr.GetName())
	s := realtime.GetSync(fullName)
	if s != nil {
		return nil
	}
	url := common.GetEnvStr("CPLN_WORKLOAD_STATUS_URL", "wss://workload-status.cpln.io/register")
	parent := cr.DeepCopy()
	background := context.Background()
	l := log.FromContext(background)

	messageHandler := func(message []byte) error {
		return r.handleWorkloadStatusMessage(background, ctx, parent, message)
	}
	connectHandler := func(w websocket.Client) error {
		return registerInterest(w, token, Interest{
			Org:      org,
			Gvc:      gvc,
			Workload: cr.GetName(),
		})
	}

	w, err := websocket.NewClient(background, l, url, token, time.Second*5, messageHandler, connectHandler)
	if err != nil {
		return err
	}
	realtime.RegisterSync(fullName, w)
	return nil
}

func (r *controller) syncWorkloadDeployments(ctx *syncContext, deployments []deployment.Deployment) error {
	var deploymentCRs []*unstructured.Unstructured
	for _, d := range deployments {
		cr, err := unstructuredCR(common.DeploymentGVK, ctx.namespace, d.Name, d, ctx.parent)
		if err != nil {
			return err
		}
		setDeploymentHealth(cr, d.Status.Versions)
		synced(cr, true, nil)
		delete(cr.Object["status"].(map[string]any), "internal")
		deploymentCRs = append(deploymentCRs, cr)
	}
	deletedDeployments, err := syncCRs(ctx.copy(), deploymentCRs, common.DeploymentGVK)
	if err != nil {
		return err
	}

	currentParent := ctx.parent.DeepCopy()
	err = r.Get(ctx, types.NamespacedName{
		Name:      ctx.parent.GetName(),
		Namespace: ctx.parent.GetNamespace(),
	}, currentParent)
	if err != nil {
		return err
	}
	ctx.parent = currentParent

	if err = r.setWorkloadHealth(ctx, ctx.parent, deploymentCRs); err != nil {
		return err
	}

	for i, d := range deployments {
		if slices.Contains(deletedDeployments, d.Name) {
			continue
		}
		ctx.parent = deploymentCRs[i]
		if err = syncDeploymentVersions(ctx.copy(), d.Status.Versions); err != nil {
			return err
		}
		if err = syncJobExecutions(ctx.copy(), d.Status.JobExecutions); err != nil {
			return err
		}
	}

	return nil
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

func (r *controller) verifyParent(ctx context.Context, parent *unstructured.Unstructured) error {
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
		*parent = currentParent
	}
	return nil
}

func (r *controller) syncFromCplnToK8s(ctx cpln.Context, log logr.Logger, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration == generation, pulling from Control Plane")

	cplnResource, err := r.cplnConnector.Get(ctx, cr)
	if err != nil && !errors.Is(err, common.NotFoundError) {
		log.Error(err, "Error fetching from Control Plane")
		return zeroResult, err
	}
	if errors.Is(err, common.NotFoundError) {
		log.Info("Resource not found on Control Plane, deleting from Kubernetes")
		if err := r.k8sConnector.Cleanup(ctx, cr); err != nil {
			log.Error(err, "Error deleting from Kubernetes")
			return zeroResult, nil
		}
		return zeroResult, err
	}

	var cplnResourceMap map[string]any
	err = json.Unmarshal(cplnResource, &cplnResourceMap)
	if err != nil {
		log.Info(fmt.Sprintf("Got non-JSON response from Control Plane: %s", cplnResource))
		return defaultResult, nil
	}

	cplnResourceAfterDryRun, err := r.cplnConnector.Put(ctx, cr, true)
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
		synced(cr, false, cplnResourceMap["status"])
		if err := r.k8sConnector.WriteStatus(ctx, cr); err != nil {
			log.Error(err, "Failed to update resource status after pulling from Control Plane")
			return zeroResult, err
		}
		return defaultResult, nil
	}

	cplnObj, err := r.cplnConnector.CplnFormat(cr)
	if err != nil {
		log.Error(err, "Error converting custom resource to cpln format")
		return zeroResult, err
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
	cr, err = r.cplnConnector.K8sFormat(ctx, cr, patchedSpec)
	if err != nil {
		return zeroResult, err
	}

	if err := r.k8sConnector.Write(ctx, cr); err != nil {
		log.Error(err, "Failed to patch k8s resource(s) with the updates from Control Plane")
		return zeroResult, err
	}

	for {
		synced(cr, false, cplnResourceMap["status"])
		if err := r.k8sConnector.WriteStatus(ctx, cr); err == nil {
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

func (r *controller) syncFromK8sToCpln(ctx cpln.Context, log logr.Logger, cr *unstructured.Unstructured) (ctrl.Result, error) {
	log.Info("lastSyncedGeneration != generation, pushing to Control Plane")
	cplnResourceAfterUpdate, err := r.cplnConnector.Put(ctx, cr, false)
	if err != nil {
		log.Error(err, "Failed to PUT resource to Control Plane")
		syncFailed(cr, err.Error())
		if err := r.k8sConnector.WriteStatus(ctx, cr); err != nil {
			log.Error(err, "Error updating status with sync failure")
		}
		return zeroResult, err
	}

	if strings.TrimSpace(cplnResourceAfterUpdate) == "" {
		b, err := r.cplnConnector.Get(ctx, cr)
		if err != nil {
			log.Error(err, "Failed to GET resource from Control Plane")
			syncFailed(cr, err.Error())
			if err := r.k8sConnector.WriteStatus(ctx, cr); err != nil {
				log.Error(err, "Error updating status with sync failure")
			}
			return zeroResult, err
		}
		cplnResourceAfterUpdate = string(b)
	}

	responseMap := map[string]any{}
	if err := json.Unmarshal([]byte(cplnResourceAfterUpdate), &responseMap); err != nil {
		log.Error(err, fmt.Sprintf("could not unmarshal response from Control Plane: %s", cplnResourceAfterUpdate))
		return zeroResult, err
	}

	synced(cr, false, responseMap["status"])
	if err := r.k8sConnector.WriteStatus(ctx, cr); err != nil {
		log.Error(err, "Failed to update resource status after pulling from Control Plane")
		return zeroResult, err
	}
	return defaultResult, nil
}

func setDeploymentHealth(deploy *unstructured.Unstructured, versions []deployment.DeploymentVersion) {
	//Unhealthy?
	if !anyVersionReady(versions) {
		unhealthy(deploy)
		var m []string
		m = append(m, "All versions of this deployment are unhealthy.")
		m = append(m, collectDeploymentMessages([]*unstructured.Unstructured{deploy})...)
		addErrorMessages(deploy, m...)
		return
	}

	//Progressing?
	if anyVersionUnready(versions) {
		progressing(deploy)
		return
	}

	//Ready!
	ready(deploy)
}

func (r *controller) setWorkloadHealth(ctx context.Context, workload *unstructured.Unstructured, deployments []*unstructured.Unstructured) error {
	setStatus := func() {
		for _, d := range deployments {
			if isUnhealthy(d) {
				unhealthy(workload)
				var m []string
				m = append(m, "At least one deployment of this workload is unhealthy.")
				m = append(m, collectDeploymentMessages(deployments)...)
				addErrorMessages(workload, m...)
				return
			}
		}
		for _, d := range deployments {
			if isProgressing(d) {
				progressing(workload)
				return
			}
		}
		for _, d := range deployments {
			if !isSuspended(d) {
				ready(workload)
				return
			}
		}
		suspended(workload)
	}

	setStatus()
	return r.Status().Update(ctx, workload)
}

func (r *controller) handleWorkloadStatusMessage(background context.Context, ctx cpln.Context, parent *unstructured.Unstructured, _ []byte) error {
	l := log.FromContext(background)
	syncCtx := newSyncContext(background, r.Client)
	if err := r.verifyParent(background, parent); err != nil {
		return nil
	}
	syncCtx.parent = parent
	syncCtx.namespace = parent.GetNamespace()
	deployments, err := cpln.GetWorkloadDeploymentsFromCpln(ctx, r.cplnConnector, parent)
	if err != nil {
		l.Error(err, "Failed to get deployments from Cpln")
		return nil
	}
	return r.syncWorkloadDeployments(syncCtx, deployments)
}
