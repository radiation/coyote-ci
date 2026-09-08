package domain

import "time"

type WorkspaceHelperRole string

const (
	WorkspaceHelperRolePrepare      WorkspaceHelperRole = "prepare"
	WorkspaceHelperRolePublish      WorkspaceHelperRole = "publish"
	WorkspaceHelperRoleCacheRestore WorkspaceHelperRole = "cache-restore"
	WorkspaceHelperRoleCacheSave    WorkspaceHelperRole = "cache-save"
)

func (r WorkspaceHelperRole) Valid() bool {
	return r == WorkspaceHelperRolePrepare || r == WorkspaceHelperRolePublish || r == WorkspaceHelperRoleCacheRestore || r == WorkspaceHelperRoleCacheSave
}

// WorkspaceHelperCapability is the provider-neutral, execution-scoped authority
// granted after a trusted workload identity exchange.
type WorkspaceHelperCapability struct {
	ExecutionJobID string
	PodUID         string
	Role           WorkspaceHelperRole
	ClaimDigest    string
	ExpiresAt      time.Time
}
