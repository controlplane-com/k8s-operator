package controllers

import (
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"slices"
)

func syncVolumeSetStatusLocations(ctx *syncContext, volumeSetStatus *unstructured.Unstructured) error {
	locations := getVolumeSetStatusLocations(volumeSetStatus)
	deletedLocations, err := syncCRs(ctx, locations, common.VolumesetStatusLocationGVK)
	if err != nil {
		return err
	}
	for _, l := range locations {
		if slices.Contains(deletedLocations, l.GetName()) {
			continue
		}
		ctx.parent = l
		if err := syncPersistentVolumeStatuses(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

func syncPersistentVolumeStatuses(ctx *syncContext, location *unstructured.Unstructured) error {
	statuses := getPersistentVolumeStatuses(location)
	_, err := syncCRs(ctx, statuses, common.PersistentVolumeStatusGVK)
	return err
}

func getPersistentVolumeStatuses(location *unstructured.Unstructured) []*unstructured.Unstructured {
	volumes, ok := location.Object["volumes"].([]any)
	if !ok || volumes == nil {
		return nil
	}
	var children []*unstructured.Unstructured
	for _, v := range volumes {
		volume, ok := v.(map[string]any)
		if !ok || volume == nil {
			continue
		}
		index, ok := volume["index"].(int64)
		if !ok {
			continue
		}
		lifecycle, ok := volume["lifecycle"].(string)
		if !ok {
			continue
		}
		child, err := unstructuredCR(common.PersistentVolumeStatusGVK, location.GetNamespace(), fmt.Sprintf("volume-%d", index), v, location)
		if err != nil {
			continue
		}
		switch lifecycle {
		case "creating":
			progressing(child)
			break
		case "unused":
			suspended(child)
			break
		case "unbound":
			progressing(child)
		case "bound":
			ready(child)
		}
		synced(child, true, nil)
		children = append(children, child)
	}
	return children
}

func getVolumeSetStatusLocations(cr *unstructured.Unstructured) []*unstructured.Unstructured {
	status, ok := cr.Object["status"].(map[string]any)
	if !ok || status == nil {
		return nil
	}
	locations, ok := status["locations"].([]any)
	if !ok || locations == nil {
		return nil
	}
	var locationResources []*unstructured.Unstructured
	for _, l := range locations {
		child, err := unstructuredCR(common.VolumesetStatusLocationGVK, cr.GetNamespace(), "", l, cr)
		synced(child, true, nil)
		if err != nil {
			continue
		}
		locationResources = append(locationResources, child)
	}
	return locationResources
}
