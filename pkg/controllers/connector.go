package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

type Connector interface {
	Read(ctx context.Context, name types.NamespacedName) (*unstructured.Unstructured, error)
	Write(ctx context.Context, cr *unstructured.Unstructured) error
	WriteStatus(ctx context.Context, cr *unstructured.Unstructured) error
	Cleanup(ctx context.Context, cr *unstructured.Unstructured) error
}
