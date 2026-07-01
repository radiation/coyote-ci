package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type countErrorIdentityRepo struct {
	err     error
	count   int
	lastCtx context.Context
}

func (r *countErrorIdentityRepo) GetByUserID(context.Context, string) (domain.UserSlackIdentity, error) {
	return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
}

func (r *countErrorIdentityRepo) Upsert(context.Context, domain.UserSlackIdentity) (domain.UserSlackIdentity, error) {
	return domain.UserSlackIdentity{}, nil
}

func (r *countErrorIdentityRepo) SetEnabled(context.Context, string, bool, time.Time) (domain.UserSlackIdentity, error) {
	return domain.UserSlackIdentity{}, nil
}

func (r *countErrorIdentityRepo) DeleteByUserID(context.Context, string) error {
	return nil
}

func (r *countErrorIdentityRepo) CountByWorkspaceIntegrationID(ctx context.Context, _ string) (int, error) {
	r.lastCtx = ctx
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.err != nil {
		return 0, r.err
	}
	return r.count, nil
}

func TestSlackWorkspaceIntegrationRepository_GetPropagatesLinkedIdentityCountErrors(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	repo.SetUserSlackIdentityRepository(&countErrorIdentityRepo{err: errors.New("count failed")})
	now := time.Date(2026, 7, 1, 16, 15, 0, 0, time.UTC)
	_, err := repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	_, err = repo.Get(context.Background())
	if err == nil || err.Error() != "count failed" {
		t.Fatalf("expected count error to surface, got %v", err)
	}
}

func TestSlackWorkspaceIntegrationRepository_CountErrorsBlockDisconnectAndSwitch(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	identityRepo := &countErrorIdentityRepo{err: errors.New("count failed")}
	repo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 16, 20, 0, 0, time.UTC)
	_, err := repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	err = repo.Delete(context.Background())
	if err == nil || err.Error() != "count failed" {
		t.Fatalf("expected disconnect to fail safely on count error, got %v", err)
	}

	_, err = repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-other",
		Enabled:        true,
		ConnectedAt:    now.Add(time.Minute),
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}, true)
	if err == nil || err.Error() != "count failed" {
		t.Fatalf("expected workspace switch to fail safely on count error, got %v", err)
	}
}

func TestSlackWorkspaceIntegrationRepository_PropagatesCallerCancellationToLinkedIdentityChecks(t *testing.T) {
	repo := NewSlackWorkspaceIntegrationRepository()
	identityRepo := &countErrorIdentityRepo{}
	repo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 16, 25, 0, 0, time.UTC)
	_, err := repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repo.Get(canceledCtx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
	if identityRepo.lastCtx != canceledCtx {
		t.Fatal("expected linked-identity count to use caller context")
	}
}
