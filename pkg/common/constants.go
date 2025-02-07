package common

const (
	UID_LABEL            = "ownerUID"
	API_GROUP            = "cpln.io"
	API_REVISION         = "v1"
	API_VERSION          = API_GROUP + "/" + API_REVISION
	FINALIZER            = "cpln.io/sync-protection"
	CONTROLLER_NAMESPACE = "controlplane"

	KIND_WORKLOAD                   = "workload"
	KIND_VOLUME_SET                 = "volumeset"
	KIND_VOLUME_SET_STATUS_LOCATION = "volumesetstatuslocation"
	KIND_PERSISTENT_VOLUME_STATUS   = "persistentvolumestatus"
	KIND_JOB_EXECUTION_STATUS       = "jobexecutionstatus"
	KIND_DEPLOYMENT_VERSION         = "deploymentversion"
	KIND_DEPLOYMENT                 = "deployment"
	KIND_CONTAINER_STATUS           = "containerstatus"
	KIND_IMAGE                      = "image"
	KIND_USER                       = "user"
	KIND_NATIVE_SECRET              = "Secret"
	KIND_CPLN_SECRET                = "secret"

	RESOURCE_POLICY_ANNOTATION = "cpln.io/resource-policy"
	RESOURCE_POLICY_KEEP       = "keep"
)
