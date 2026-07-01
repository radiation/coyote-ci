package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type fakeSlackDirectoryClient struct {
	user  platformslack.User
	err   error
	calls *int
}

func (f fakeSlackDirectoryClient) LookupUserByEmail(_ context.Context, _ string, _ string) (platformslack.User, error) {
	if f.calls != nil {
		*f.calls = *f.calls + 1
	}
	if f.err != nil {
		return platformslack.User{}, f.err
	}
	return f.user, nil
}

func TestUserSlackIdentityService_ResolveAndLinkByAuthenticatedEmail(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)

	now := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		WorkspaceName:  testStringPtr("Coyote"),
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{user: platformslack.User{
		ID:              "U123",
		DisplayName:     testStringPtr("Bryan"),
		RealName:        testStringPtr("Bryan Choate"),
		Handle:          testStringPtr("bryan"),
		Email:           testStringPtr("bryan@example.com"),
		ProfileImageURL: testStringPtr("https://images.example/avatar.png"),
	}})
	svc.now = func() time.Time { return now }

	user := domain.User{ID: "user-1", Email: "Bryan@example.com"}
	candidate, matched, err := svc.ResolveByAuthenticatedEmail(context.Background(), user)
	if err != nil {
		t.Fatalf("resolve by authenticated email: %v", err)
	}
	if !matched || candidate == nil {
		t.Fatalf("expected matched candidate, got matched=%t candidate=%v", matched, candidate)
	}
	if _, lookupErr := identityRepo.GetByUserID(context.Background(), user.ID); lookupErr == nil {
		t.Fatalf("expected resolve to persist nothing")
	}

	linked, err := svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: candidate.Workspace.ID,
		SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
		SlackUserID:            candidate.SlackUserID,
	})
	if err != nil {
		t.Fatalf("link user slack identity: %v", err)
	}
	if linked.SlackUserID != "U123" {
		t.Fatalf("expected slack user id U123, got %q", linked.SlackUserID)
	}
}

func TestUserSlackIdentityService_MissingScopeIsActionable(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 5, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{err: &platformslack.MissingScopeError{Needed: "users:read.email", Provided: "chat:write"}})
	_, _, err = svc.ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if err == nil || err.Error() != "slack member lookup requires the users:read.email scope. Ask an administrator to add it and reinstall or reauthorize the Slack app" {
		t.Fatalf("expected actionable missing-scope error, got %v", err)
	}
}

func TestUserSlackIdentityService_PreviousFailedHealthTestStillAllowsLookup(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 7, 0, 0, time.UTC)
	failed := false
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:                "workspace-1",
		WorkspaceID:       "T123",
		WorkspaceName:     testStringPtr("Coyote"),
		BotTokenSecret:    "xoxb-secret",
		Enabled:           true,
		ConnectedAt:       now,
		LastTestedAt:      &now,
		LastTestSucceeded: &failed,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	lookupCalls := 0
	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{
		calls: &lookupCalls,
		user:  platformslack.User{ID: "U123"},
	})

	state, err := svc.Get(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("get slack identity state: %v", err)
	}
	if state.WorkspaceStatus != SlackIdentityWorkspaceStatusReady {
		t.Fatalf("expected ready workspace status, got %q", state.WorkspaceStatus)
	}
	if state.Workspace == nil || state.Workspace.LastTestSucceeded == nil || *state.Workspace.LastTestSucceeded {
		t.Fatalf("expected failed historical test to be preserved in workspace metadata, got %+v", state.Workspace)
	}

	candidate, matched, err := svc.ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if err != nil || !matched || candidate == nil {
		t.Fatalf("expected successful live lookup despite failed historical test, got matched=%t candidate=%v err=%v", matched, candidate, err)
	}
	if lookupCalls != 1 {
		t.Fatalf("expected exactly one live lookup, got %d", lookupCalls)
	}
}

func TestUserSlackIdentityService_DisabledWorkspaceDoesNotAttemptLookup(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 8, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        false,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	lookupCalls := 0
	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{
		calls: &lookupCalls,
		user:  platformslack.User{ID: "U123"},
	})

	_, _, err = svc.ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if !errors.Is(err, ErrUserSlackIdentityWorkspaceDisabled) {
		t.Fatalf("expected disabled workspace error, got %v", err)
	}
	if lookupCalls != 0 {
		t.Fatalf("expected disabled workspace to skip lookup, got %d calls", lookupCalls)
	}
}

func TestUserSlackIdentityService_LiveLookupInvalidCredentialsStillFails(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 9, 0, 0, time.UTC)
	failed := false
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:                "workspace-1",
		WorkspaceID:       "T123",
		BotTokenSecret:    "xoxb-secret",
		Enabled:           true,
		ConnectedAt:       now,
		LastTestedAt:      &now,
		LastTestSucceeded: &failed,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{err: platformslack.ErrInvalidAuth})
	_, _, err = svc.ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if !errors.Is(err, ErrSlackWorkspaceInvalidAuth) {
		t.Fatalf("expected live lookup invalid auth error, got %v", err)
	}
}

func TestUserSlackIdentityService_WorkspaceChangeBetweenResolveAndConfirmIsRejected(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 10, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{user: platformslack.User{ID: "U123"}})
	user := domain.User{ID: "user-1", Email: "user@example.com"}
	candidate, matched, err := svc.ResolveByAuthenticatedEmail(context.Background(), user)
	if err != nil || !matched || candidate == nil {
		t.Fatalf("expected candidate, got matched=%t candidate=%v err=%v", matched, candidate, err)
	}

	_, err = workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-other",
		Enabled:        true,
		ConnectedAt:    now.Add(time.Minute),
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}, true)
	if err != nil {
		t.Fatalf("replace workspace integration: %v", err)
	}

	_, err = svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: candidate.Workspace.ID,
		SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
		SlackUserID:            candidate.SlackUserID,
	})
	if !errors.Is(err, ErrUserSlackIdentityCandidateChanged) {
		t.Fatalf("expected candidate changed error, got %v", err)
	}
}

func TestUserSlackIdentityService_DuplicateSlackMemberIsPrivacySafeConflict(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 15, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{user: platformslack.User{ID: "U123"}})
	svc.now = func() time.Time { return now }

	for _, user := range []domain.User{{ID: "user-1", Email: "user1@example.com"}, {ID: "user-2", Email: "user2@example.com"}} {
		candidate, matched, resolveErr := svc.ResolveByAuthenticatedEmail(context.Background(), user)
		if resolveErr != nil || !matched || candidate == nil {
			t.Fatalf("resolve candidate for %s: matched=%t candidate=%v err=%v", user.ID, matched, candidate, resolveErr)
		}
		_, linkErr := svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
			ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
			WorkspaceIntegrationID: candidate.Workspace.ID,
			SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
			SlackUserID:            candidate.SlackUserID,
		})
		if user.ID == "user-1" && linkErr != nil {
			t.Fatalf("first link should succeed, got %v", linkErr)
		}
		if user.ID == "user-2" && !errors.Is(linkErr, ErrUserSlackIdentityConflict) {
			t.Fatalf("expected privacy-safe conflict, got %v", linkErr)
		}
	}
}

func TestUserSlackIdentityService_EnableDisableAndUnlinkAreSelfScoped(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 20, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{user: platformslack.User{ID: "U123"}})
	svc.now = func() time.Time { return now }
	user := domain.User{ID: "user-1", Email: "user@example.com"}
	candidate, matched, err := svc.ResolveByAuthenticatedEmail(context.Background(), user)
	if err != nil || !matched || candidate == nil {
		t.Fatalf("resolve candidate: matched=%t candidate=%v err=%v", matched, candidate, err)
	}
	_, err = svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: candidate.Workspace.ID,
		SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
		SlackUserID:            candidate.SlackUserID,
	})
	if err != nil {
		t.Fatalf("link identity: %v", err)
	}

	updated, err := svc.SetEnabled(context.Background(), user, testBoolPtr(false))
	if err != nil {
		t.Fatalf("disable identity: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected identity to be disabled")
	}

	if err := svc.Unlink(context.Background(), user); err != nil {
		t.Fatalf("unlink identity: %v", err)
	}
	if err := svc.Unlink(context.Background(), user); err != nil {
		t.Fatalf("expected repeated unlink to be idempotent, got %v", err)
	}
}

func TestUserSlackIdentityService_RepeatedConfirmationIsIdempotentForSameUser(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 22, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{user: platformslack.User{ID: "U123"}})
	svc.now = func() time.Time { return now }
	user := domain.User{ID: "user-1", Email: "user@example.com"}
	candidate, matched, err := svc.ResolveByAuthenticatedEmail(context.Background(), user)
	if err != nil || !matched || candidate == nil {
		t.Fatalf("resolve candidate: matched=%t candidate=%v err=%v", matched, candidate, err)
	}

	first, err := svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: candidate.Workspace.ID,
		SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
		SlackUserID:            candidate.SlackUserID,
	})
	if err != nil {
		t.Fatalf("first link: %v", err)
	}

	svc.now = func() time.Time { return now.Add(5 * time.Minute) }
	second, err := svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: candidate.Workspace.ID,
		SlackWorkspaceID:       candidate.Workspace.SlackWorkspaceID,
		SlackUserID:            candidate.SlackUserID,
	})
	if err != nil {
		t.Fatalf("second link: %v", err)
	}
	if !second.LinkedAt.Equal(first.LinkedAt) {
		t.Fatalf("expected repeated confirmation to preserve linked_at, got first=%s second=%s", first.LinkedAt, second.LinkedAt)
	}
}

func TestUserSlackIdentityService_DisconnectAndWorkspaceSwitchBlockedWhenLinkedIdentitiesExist(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 25, 0, 0, time.UTC)
	_, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if err != nil {
		t.Fatalf("seed workspace integration: %v", err)
	}

	_, err = identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      "user-1",
		SlackWorkspaceIntegrationID: "workspace-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	deleteErr := workspaceRepo.Delete(context.Background())
	if !errors.Is(deleteErr, repository.ErrSlackWorkspaceIntegrationLinkedIdentitiesExist) {
		t.Fatalf("expected linked-identities disconnect conflict, got %v", deleteErr)
	}
	_, err = workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-other",
		Enabled:        true,
		ConnectedAt:    now.Add(time.Minute),
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}, true)
	if !errors.Is(err, repository.ErrSlackWorkspaceIntegrationLinkedIdentitiesExist) {
		t.Fatalf("expected linked-identities replace conflict, got %v", err)
	}

	if err := identityRepo.DeleteByUserID(context.Background(), "user-1"); err != nil {
		t.Fatalf("unlink identity: %v", err)
	}
	if err := workspaceRepo.Delete(context.Background()); err != nil {
		t.Fatalf("expected disconnect to succeed after unlinking last identity, got %v", err)
	}
}

func testStringPtr(value string) *string {
	v := value
	return &v
}

func testBoolPtr(value bool) *bool {
	v := value
	return &v
}
