package common

const (
	SECRET_NAME   = "controlplane-operator"
	UID_LABEL     = "ownerUID"
	KIND_WORKLOAD = "workload"
	API_GROUP     = "cpln.io"
	API_REVISION  = "v1"
	API_VERSION   = API_GROUP + "/" + API_REVISION
	FINALIZER     = "cpln.io/sync-protection"
)
