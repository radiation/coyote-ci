package domain

import (
	"errors"
	"strings"
	"time"
)

type WorkspaceRevisionStatus string

const (
	WorkspaceRevisionStatusPublishing WorkspaceRevisionStatus = "publishing"
	WorkspaceRevisionStatusPublished  WorkspaceRevisionStatus = "published"
	WorkspaceRevisionStatusDeleted    WorkspaceRevisionStatus = "deleted"
)

var ErrInvalidWorkspaceRevision = errors.New("invalid workspace revision")

// WorkspaceRevision records the durable publication state for one execution
// job's logical workspace result. It does not describe the writable workspace
// or the storage provider that holds future revision bytes.
type WorkspaceRevision struct {
	ID                      string
	ProducingExecutionJobID string
	BuildID                 string
	NodeID                  string
	AttemptNumber           int
	ParentRevisionID        *string
	Status                  WorkspaceRevisionStatus
	ContentDigest           *string
	StorageKey              *string
	SizeBytes               *int64
	CreatedAt               time.Time
	PublishedAt             *time.Time
	DeletedAt               *time.Time
}

type WorkspaceRevisionPublication struct {
	ContentDigest string
	StorageKey    string
	SizeBytes     *int64
}

func (r WorkspaceRevision) ValidateForCreate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ProducingExecutionJobID) == "" || strings.TrimSpace(r.BuildID) == "" || strings.TrimSpace(r.NodeID) == "" || r.AttemptNumber < 1 || r.CreatedAt.IsZero() {
		return ErrInvalidWorkspaceRevision
	}
	if r.Status != WorkspaceRevisionStatusPublishing || r.ContentDigest != nil || r.StorageKey != nil || r.SizeBytes != nil || r.PublishedAt != nil || r.DeletedAt != nil {
		return ErrInvalidWorkspaceRevision
	}
	return nil
}

func (p WorkspaceRevisionPublication) Validate() error {
	if strings.TrimSpace(p.ContentDigest) == "" || strings.TrimSpace(p.StorageKey) == "" {
		return ErrInvalidWorkspaceRevision
	}
	if p.SizeBytes != nil && *p.SizeBytes < 0 {
		return ErrInvalidWorkspaceRevision
	}
	return nil
}
