package controllers

import (
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func syncPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return shouldSyncObject(e.ObjectNew)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return shouldSyncObject(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return shouldSyncObject(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return shouldSyncObject(e.Object)
		},
	}
}

func shouldSyncObject(obj client.Object) bool {
	return obj.GetNamespace() != common.CONTROLLER_NAMESPACE
}
