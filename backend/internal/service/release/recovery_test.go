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
	buildID          string
	err              error
	beforeVersioning func()
}

func (f *artifactVersionerFake) ReconcileArtifactVersions(_ context.Context, buildID string) error {
	f.buildID = buildID
	if f.beforeVersioning != nil {
		f.beforeVersioning()
	}
	return f.err
}

func TestService_ReconcileArtifactVersionRecovery(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         domain.ApplicationReleaseStatus
		preflightErr   error
		versionErr     error
		finalErr       error
		wantVersion    bool
		wantReconciles int
	}{
		{name: "preflight then versions then finalization", status: domain.ApplicationReleaseStatusPending, wantVersion: true, wantReconciles: 2},
		{name: "complete not retagged", status: domain.ApplicationReleaseStatusComplete, wantReconciles: 1},
		{name: "failed not retagged", status: domain.ApplicationReleaseStatusFailed, wantReconciles: 1},
		{name: "missing release", preflightErr: repository.ErrApplicationReleaseNotFound, wantReconciles: 1},
		{name: "ambiguous release", preflightErr: repository.ErrApplicationReleaseConflict, wantReconciles: 1},
		{name: "preflight database error", preflightErr: errors.New("database unavailable"), wantReconciles: 1},
		{name: "version conflict", status: domain.ApplicationReleaseStatusPending, versionErr: repository.ErrVersionTagConflict, wantVersion: true, wantReconciles: 1},
		{name: "finalization error", status: domain.ApplicationReleaseStatusPending, finalErr: errors.New("finalization failed"), wantVersion: true, wantReconciles: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &releaseRepositoryFake{reconciled: domain.ApplicationRelease{ID: "release-1", BuildID: "build-1", Status: test.status}, reconcileErr: test.preflightErr}
			versioner := &artifactVersionerFake{err: test.versionErr, beforeVersioning: func() {
				if len(repo.reconciledIDs) != 1 || repo.reconciledIDs[0] != "release-1" {
					t.Fatalf("version creation preceded ambiguity validation: reconciled=%v", repo.reconciledIDs)
				}
				repo.reconciled.Status = domain.ApplicationReleaseStatusComplete
				repo.reconcileErr = test.finalErr
			}}
			reconciled, reconcileErr := NewService(repo).WithArtifactVersioner(versioner).Reconcile(context.Background(), "release-1")
			wantErr := errors.Join(test.preflightErr, test.versionErr, test.finalErr)
			if wantErr == nil && reconcileErr != nil || test.preflightErr != nil && !errors.Is(reconcileErr, test.preflightErr) || test.versionErr != nil && !errors.Is(reconcileErr, test.versionErr) || test.finalErr != nil && !errors.Is(reconcileErr, test.finalErr) {
				t.Fatalf("error=%v, want %v", reconcileErr, wantErr)
			}
			if (versioner.buildID == "build-1") != test.wantVersion || len(repo.reconciledIDs) != test.wantReconciles {
				t.Fatalf("version build=%s reconciled=%v", versioner.buildID, repo.reconciledIDs)
			}
			if wantErr == nil && reconciled.Status != repo.reconciled.Status {
				t.Fatalf("returned release=%+v, want status=%s", reconciled, repo.reconciled.Status)
			}
		})
	}
}
