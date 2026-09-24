package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type WorkspaceRevisionStatus string

const (
	WorkspaceRevisionStatusPublishing WorkspaceRevisionStatus = "publishing"
	WorkspaceRevisionStatusPublished  WorkspaceRevisionStatus = "published"
	WorkspaceRevisionStatusDeleted    WorkspaceRevisionStatus = "deleted"
)

var ErrInvalidWorkspaceRevision = errors.New("invalid workspace revision")

var workspaceRevisionNamespace = uuid.MustParse("80c528d8-d286-5fdd-98ce-5d5239616c2a")

func WorkspaceRevisionIDForExecutionJob(executionJobID string) string {
	return uuid.NewSHA1(workspaceRevisionNamespace, []byte(executionJobID)).String()
}

// WorkspaceRevision records the durable publication state for one execution
// job's logical workspace result. It does not describe the writable workspace.
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
	StorageProvider         *StorageProvider
	SizeBytes               *int64
	CreatedAt               time.Time
	PublishedAt             *time.Time
	DeletedAt               *time.Time
}

type WorkspaceRevisionPublication struct {
	ContentDigest   string
	StorageKey      string
	StorageProvider StorageProvider
	SizeBytes       *int64
}

func (r WorkspaceRevision) ValidateForCreate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ProducingExecutionJobID) == "" || strings.TrimSpace(r.BuildID) == "" || strings.TrimSpace(r.NodeID) == "" || r.AttemptNumber < 1 || r.CreatedAt.IsZero() {
		return ErrInvalidWorkspaceRevision
	}
	if r.Status != WorkspaceRevisionStatusPublishing || r.ContentDigest != nil || r.StorageKey != nil || r.StorageProvider != nil || r.SizeBytes != nil || r.PublishedAt != nil || r.DeletedAt != nil {
		return ErrInvalidWorkspaceRevision
	}
	return nil
}

func (p WorkspaceRevisionPublication) Validate() error {
	if strings.TrimSpace(p.ContentDigest) == "" || strings.TrimSpace(p.StorageKey) == "" {
		return ErrInvalidWorkspaceRevision
	}
	if p.StorageProvider != StorageProviderFilesystem && p.StorageProvider != StorageProviderGCS {
		return ErrInvalidWorkspaceRevision
	}
	if p.SizeBytes != nil && *p.SizeBytes < 0 {
		return ErrInvalidWorkspaceRevision
	}
	return nil
}
