package release

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestRecoveryService_RunReconcilesPendingBeforeCancellation(t *testing.T) {
	repo := &releaseRepositoryFake{pending: []domain.ApplicationRelease{{ID: "release-1"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewRecoveryService(NewService(repo), time.Millisecond).Run(ctx)
	if len(repo.reconciledIDs) != 1 || repo.reconciledIDs[0] != "release-1" {
		t.Fatalf("reconciled IDs = %#v, want release-1", repo.reconciledIDs)
	}
}

type artifactVersionerFake struct {
	buildID string
	err     error
}

func (f *artifactVersionerFake) ReconcileArtifactVersions(_ context.Context, buildID string) error {
	f.buildID = buildID
	return f.err
}

func TestService_ReconcileArtifactVersionRecovery(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        domain.ApplicationReleaseStatus
		lookupErr     error
		versionErr    error
		wantVersion   bool
		wantReconcile bool
	}{
		{name: "pending versions first", status: domain.ApplicationReleaseStatusPending, wantVersion: true, wantReconcile: true},
		{name: "complete not retagged", status: domain.ApplicationReleaseStatusComplete, wantReconcile: true},
		{name: "failed not retagged", status: domain.ApplicationReleaseStatusFailed, wantReconcile: true},
		{name: "lookup error", lookupErr: repository.ErrApplicationReleaseNotFound},
		{name: "version conflict", status: domain.ApplicationReleaseStatusPending, versionErr: repository.ErrVersionTagConflict, wantVersion: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &releaseRepositoryFake{getByID: domain.ApplicationRelease{ID: "release-1", BuildID: "build-1", Status: test.status}, getByIDErr: test.lookupErr}
			versioner := &artifactVersionerFake{err: test.versionErr}
			_, reconcileErr := NewService(repo).WithArtifactVersioner(versioner).Reconcile(context.Background(), "release-1")
			wantErr := errors.Join(test.lookupErr, test.versionErr)
			if wantErr == nil && reconcileErr != nil || test.lookupErr != nil && !errors.Is(reconcileErr, test.lookupErr) || test.versionErr != nil && !errors.Is(reconcileErr, test.versionErr) {
				t.Fatalf("error=%v, want %v", reconcileErr, wantErr)
			}
			if (versioner.buildID == "build-1") != test.wantVersion || (len(repo.reconciledIDs) == 1) != test.wantReconcile {
				t.Fatalf("version build=%s reconciled=%v", versioner.buildID, repo.reconciledIDs)
			}
		})
	}
}
