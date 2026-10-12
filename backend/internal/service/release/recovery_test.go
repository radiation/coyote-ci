package release

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
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
