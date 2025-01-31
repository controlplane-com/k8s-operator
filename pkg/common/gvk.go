package common

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var DeploymentGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_DEPLOYMENT,
}
var DeploymentVersionGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_DEPLOYMENT_VERSION,
}
var ContainerStatusGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_CONTAINER_STATUS,
}
var JobExecutionStatusGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_JOB_EXECUTION_STATUS,
}
var VolumesetStatusLocationGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_VOLUME_SET_STATUS_LOCATION,
}
var PersistentVolumeStatusGVK = schema.GroupVersionKind{
	Group:   API_GROUP,
	Version: API_REVISION,
	Kind:    KIND_PERSISTENT_VOLUME_STATUS,
}
