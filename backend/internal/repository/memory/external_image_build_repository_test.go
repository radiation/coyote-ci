package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestExternalImageBuildRepositoryCreatesIdempotentIntentAndCopiesSubmissionTime(t *testing.T) {
	repo := NewExternalImageBuildRepository()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	ctx := context.Background()

	created, createErr := repo.CreateIntent(ctx, domain.ExternalImageBuild{ExecutionJobID: "execution-1", Provider: domain.ImageBuildProviderCloudBuild, TargetImageReference: "coyote-ci/backend"})
	if createErr != nil || created.SubmissionState != domain.ExternalImageBuildSubmissionIntent || !created.CreatedAt.Equal(now) {
		t.Fatalf("created=%+v err=%v", created, createErr)
	}
	duplicate, duplicateErr := repo.CreateIntent(ctx, domain.ExternalImageBuild{ExecutionJobID: "execution-1", Provider: domain.ImageBuildProviderCloudBuild, TargetImageReference: "other"})
	if duplicateErr != nil || duplicate.TargetImageReference != "coyote-ci/backend" {
		t.Fatalf("duplicate=%+v err=%v", duplicate, duplicateErr)
	}

	submittedAt := now.Add(time.Minute)
	created.SubmissionState = domain.ExternalImageBuildSubmissionSubmitted
	created.SubmittedAt = &submittedAt
	updated, updateErr := repo.Update(ctx, created)
	if updateErr != nil || updated.SubmittedAt == nil || !updated.SubmittedAt.Equal(submittedAt) {
		t.Fatalf("updated=%+v err=%v", updated, updateErr)
	}
	*updated.SubmittedAt = time.Time{}
	stored, getErr := repo.GetByExecutionJobID(ctx, "execution-1")
	if getErr != nil || stored.SubmittedAt == nil || !stored.SubmittedAt.Equal(submittedAt) {
		t.Fatalf("stored=%+v err=%v", stored, getErr)
	}
}

func TestExternalImageBuildRepositoryRejectsUnknownReadsAndUpdates(t *testing.T) {
	repo := NewExternalImageBuildRepository()
	if _, getErr := repo.GetByExecutionJobID(context.Background(), "missing"); !errors.Is(getErr, repository.ErrExternalImageBuildNotFound) {
		t.Fatalf("get error=%v", getErr)
	}
	if _, updateErr := repo.Update(context.Background(), domain.ExternalImageBuild{ExecutionJobID: "missing"}); !errors.Is(updateErr, repository.ErrExternalImageBuildNotFound) {
		t.Fatalf("update error=%v", updateErr)
	}
}
