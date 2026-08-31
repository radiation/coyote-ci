package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type WorkspaceRevisionRepository struct {
	mu                    sync.RWMutex
	revisionsByID         map[string]domain.WorkspaceRevision
	revisionIDByExecution map[string]string
	executionJobs         repository.ExecutionJobRepository
}

func NewWorkspaceRevisionRepository(executionJobs repository.ExecutionJobRepository) *WorkspaceRevisionRepository {
	return &WorkspaceRevisionRepository{
		revisionsByID:         map[string]domain.WorkspaceRevision{},
		revisionIDByExecution: map[string]string{},
		executionJobs:         executionJobs,
	}
}

func (r *WorkspaceRevisionRepository) CreatePublishing(ctx context.Context, revision domain.WorkspaceRevision) (domain.WorkspaceRevision, error) {
	if err := revision.ValidateForCreate(); err != nil {
		return domain.WorkspaceRevision{}, err
	}
	if r.executionJobs == nil {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}
	job, err := r.executionJobs.GetJobByID(ctx, revision.ProducingExecutionJobID)
	if err != nil {
		return domain.WorkspaceRevision{}, err
	}
	if job.BuildID != revision.BuildID || job.NodeID != revision.NodeID || job.AttemptNumber != revision.AttemptNumber {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existingID, found := r.revisionIDByExecution[revision.ProducingExecutionJobID]; found {
		existing := r.revisionsByID[existingID]
		if existing.ID == revision.ID && sameRevisionCreate(existing, revision) {
			return cloneWorkspaceRevision(existing), nil
		}
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}
	if _, found := r.revisionsByID[revision.ID]; found {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}

	r.revisionsByID[revision.ID] = cloneWorkspaceRevision(revision)
	r.revisionIDByExecution[revision.ProducingExecutionJobID] = revision.ID
	return cloneWorkspaceRevision(revision), nil
}

func (r *WorkspaceRevisionRepository) MarkPublishedIfClaimed(ctx context.Context, revisionID string, claimToken string, publication domain.WorkspaceRevisionPublication, publishedAt time.Time) (domain.WorkspaceRevision, error) {
	if err := publication.Validate(); err != nil || strings.TrimSpace(revisionID) == "" || strings.TrimSpace(claimToken) == "" || publishedAt.IsZero() {
		return domain.WorkspaceRevision{}, domain.ErrInvalidWorkspaceRevision
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	revision, found := r.revisionsByID[revisionID]
	if !found {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	if revision.Status == domain.WorkspaceRevisionStatusDeleted {
		return cloneWorkspaceRevision(revision), repository.ErrWorkspaceRevisionConflict
	}
	if revision.Status == domain.WorkspaceRevisionStatusPublished {
		if samePublication(revision, publication) {
			return cloneWorkspaceRevision(revision), nil
		}
		return cloneWorkspaceRevision(revision), repository.ErrWorkspaceRevisionConflict
	}

	if r.executionJobs == nil {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionStaleClaim
	}
	job, err := r.executionJobs.GetJobByID(ctx, revision.ProducingExecutionJobID)
	if err != nil {
		return domain.WorkspaceRevision{}, err
	}
	if job.Status != domain.ExecutionJobStatusRunning || job.ClaimToken == nil || *job.ClaimToken != claimToken || job.ClaimExpiresAt == nil || !job.ClaimExpiresAt.After(time.Now().UTC()) {
		return cloneWorkspaceRevision(revision), repository.ErrWorkspaceRevisionStaleClaim
	}

	revision.Status = domain.WorkspaceRevisionStatusPublished
	revision.ContentDigest = workspaceStringPointer(publication.ContentDigest)
	revision.StorageKey = workspaceStringPointer(publication.StorageKey)
	revision.SizeBytes = cloneInt64Pointer(publication.SizeBytes)
	published := publishedAt.UTC()
	revision.PublishedAt = &published
	r.revisionsByID[revision.ID] = revision
	return cloneWorkspaceRevision(revision), nil
}

func (r *WorkspaceRevisionRepository) GetByProducingExecutionJob(_ context.Context, executionJobID string) (domain.WorkspaceRevision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	revisionID, found := r.revisionIDByExecution[executionJobID]
	if !found {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	return cloneWorkspaceRevision(r.revisionsByID[revisionID]), nil
}

func (r *WorkspaceRevisionRepository) GetPublishedByBuildNode(ctx context.Context, buildID string, nodeID string) (domain.WorkspaceRevision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var selected domain.WorkspaceRevision
	found := false
	for _, revision := range r.revisionsByID {
		if revision.BuildID != buildID || revision.NodeID != nodeID || revision.Status != domain.WorkspaceRevisionStatusPublished {
			continue
		}
		if r.executionJobs == nil {
			return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
		}
		job, err := r.executionJobs.GetJobByID(ctx, revision.ProducingExecutionJobID)
		if err != nil {
			return domain.WorkspaceRevision{}, err
		}
		if job.Status != domain.ExecutionJobStatusSuccess {
			continue
		}
		if !found || revision.AttemptNumber > selected.AttemptNumber || (revision.AttemptNumber == selected.AttemptNumber && revision.CreatedAt.After(selected.CreatedAt)) {
			selected = revision
			found = true
		}
	}
	if !found {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	return cloneWorkspaceRevision(selected), nil
}

func (r *WorkspaceRevisionRepository) MarkDeleted(_ context.Context, revisionID string, deletedAt time.Time) (domain.WorkspaceRevision, error) {
	if strings.TrimSpace(revisionID) == "" || deletedAt.IsZero() {
		return domain.WorkspaceRevision{}, domain.ErrInvalidWorkspaceRevision
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	revision, found := r.revisionsByID[revisionID]
	if !found {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	if revision.Status == domain.WorkspaceRevisionStatusDeleted {
		return cloneWorkspaceRevision(revision), nil
	}
	if revision.Status != domain.WorkspaceRevisionStatusPublished {
		return cloneWorkspaceRevision(revision), repository.ErrWorkspaceRevisionConflict
	}
	deleted := deletedAt.UTC()
	revision.Status = domain.WorkspaceRevisionStatusDeleted
	revision.DeletedAt = &deleted
	r.revisionsByID[revisionID] = revision
	return cloneWorkspaceRevision(revision), nil
}

func sameRevisionCreate(left domain.WorkspaceRevision, right domain.WorkspaceRevision) bool {
	return left.ID == right.ID && left.ProducingExecutionJobID == right.ProducingExecutionJobID && left.BuildID == right.BuildID && left.NodeID == right.NodeID && left.AttemptNumber == right.AttemptNumber && sameStringPointer(left.ParentRevisionID, right.ParentRevisionID) && left.Status == right.Status
}

func samePublication(revision domain.WorkspaceRevision, publication domain.WorkspaceRevisionPublication) bool {
	return sameStringPointer(revision.ContentDigest, workspaceStringPointer(publication.ContentDigest)) && sameStringPointer(revision.StorageKey, workspaceStringPointer(publication.StorageKey)) && sameInt64Pointer(revision.SizeBytes, publication.SizeBytes)
}

func cloneWorkspaceRevision(revision domain.WorkspaceRevision) domain.WorkspaceRevision {
	revision.ParentRevisionID = cloneWorkspaceStringPointer(revision.ParentRevisionID)
	revision.ContentDigest = cloneWorkspaceStringPointer(revision.ContentDigest)
	revision.StorageKey = cloneWorkspaceStringPointer(revision.StorageKey)
	revision.SizeBytes = cloneInt64Pointer(revision.SizeBytes)
	revision.PublishedAt = cloneTimePointer(revision.PublishedAt)
	revision.DeletedAt = cloneTimePointer(revision.DeletedAt)
	return revision
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sameStringPointer(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameInt64Pointer(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func workspaceStringPointer(value string) *string {
	cloned := value
	return &cloned
}

func cloneWorkspaceStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return workspaceStringPointer(*value)
}
