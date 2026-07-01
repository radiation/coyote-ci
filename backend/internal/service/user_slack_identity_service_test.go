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
	if !errors.Is(err, ErrUserSlackIdentityMissingScope) {
		t.Fatalf("expected missing-scope sentinel, got %v", err)
	}
	if err == nil || err.Error() != "slack member lookup requires the users:read.email scope. Ask an administrator to add it and reinstall or reauthorize the Slack app" {
		t.Fatalf("expected actionable missing-scope error, got %v", err)
	}
}

func TestUserSlackIdentityService_GetStatesAndValidation(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{})

	_, err := svc.Get(context.Background(), domain.User{})
	if !errors.Is(err, ErrUserSlackIdentityUserIDRequired) {
		t.Fatalf("expected missing user id error, got %v", err)
	}

	state, err := svc.Get(context.Background(), domain.User{ID: "user-1"})
	if err != nil {
		t.Fatalf("get not configured state: %v", err)
	}
	if state.WorkspaceStatus != SlackIdentityWorkspaceStatusNotConfigured {
		t.Fatalf("expected not configured status, got %q", state.WorkspaceStatus)
	}
	if state.Workspace != nil || state.Identity != nil {
		t.Fatalf("expected empty state when workspace and identity are absent, got %+v", state)
	}
}

func TestUserSlackIdentityService_GetSuppressesOrphanedIdentityWhenWorkspaceMissing(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{})
	now := time.Date(2026, 7, 1, 14, 6, 0, 0, time.UTC)

	_, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
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
		t.Fatalf("seed orphan identity: %v", err)
	}

	state, err := svc.Get(context.Background(), domain.User{ID: "user-1"})
	if err != nil {
		t.Fatalf("get state with orphan identity: %v", err)
	}
	if state.WorkspaceStatus != SlackIdentityWorkspaceStatusNotConfigured {
		t.Fatalf("expected not configured status, got %q", state.WorkspaceStatus)
	}
	if state.Workspace != nil {
		t.Fatalf("expected no workspace, got %+v", state.Workspace)
	}
	if state.Identity != nil {
		t.Fatalf("expected orphan identity to be suppressed, got %+v", state.Identity)
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

func TestUserSlackIdentityService_ResolveLookupErrorMappings(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 9, 30, 0, time.UTC)
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

	testCases := []struct {
		name        string
		err         error
		wantErr     error
		wantMatched bool
	}{
		{name: "no match", err: platformslack.ErrUsersNotFound, wantMatched: false},
		{name: "deleted user", err: platformslack.ErrDeletedUser, wantErr: ErrUserSlackIdentityMemberUnavailable},
		{name: "bot user", err: platformslack.ErrBotUser, wantErr: ErrUserSlackIdentityMemberUnavailable},
		{name: "app user", err: platformslack.ErrAppUser, wantErr: ErrUserSlackIdentityMemberUnavailable},
		{name: "token revoked", err: platformslack.ErrTokenRevoked, wantErr: ErrSlackWorkspaceTokenRevoked},
		{name: "account inactive", err: platformslack.ErrAccountInactive, wantErr: ErrSlackWorkspaceAccountInactive},
		{name: "rate limited", err: platformslack.ErrRateLimited, wantErr: ErrSlackWorkspaceRateLimited},
		{name: "malformed", err: platformslack.ErrMalformedResponse, wantErr: ErrSlackWorkspaceMalformedResponse},
		{name: "upstream", err: platformslack.ErrUpstreamFailure, wantErr: ErrSlackWorkspaceUpstream},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{err: tc.err})
			candidate, matched, resolveErr := svc.ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
			if tc.wantErr == nil {
				if resolveErr != nil {
					t.Fatalf("expected no error, got %v", resolveErr)
				}
				if matched != tc.wantMatched || candidate != nil {
					t.Fatalf("expected matched=%t and nil candidate, got matched=%t candidate=%v", tc.wantMatched, matched, candidate)
				}
				return
			}
			if !errors.Is(resolveErr, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, resolveErr)
			}
		})
	}
}

func TestUserSlackIdentityService_LinkValidationAndCandidateMismatch(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 11, 0, 0, time.UTC)
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

	validationCases := []struct {
		name  string
		user  domain.User
		input LinkUserSlackIdentityInput
		want  error
	}{
		{name: "missing user id", user: domain.User{Email: "user@example.com"}, input: LinkUserSlackIdentityInput{ResolutionMethod: SlackIdentityResolutionMethodAuthenticatedEmail, WorkspaceIntegrationID: "workspace-1", SlackWorkspaceID: "T123", SlackUserID: "U123"}, want: ErrUserSlackIdentityUserIDRequired},
		{name: "invalid method", user: user, input: LinkUserSlackIdentityInput{ResolutionMethod: "email", WorkspaceIntegrationID: "workspace-1", SlackWorkspaceID: "T123", SlackUserID: "U123"}, want: ErrUserSlackIdentityResolutionMethodInvalid},
		{name: "missing workspace integration id", user: user, input: LinkUserSlackIdentityInput{ResolutionMethod: SlackIdentityResolutionMethodAuthenticatedEmail, SlackWorkspaceID: "T123", SlackUserID: "U123"}, want: ErrUserSlackIdentityWorkspaceIntegrationIDRequired},
		{name: "missing slack workspace id", user: user, input: LinkUserSlackIdentityInput{ResolutionMethod: SlackIdentityResolutionMethodAuthenticatedEmail, WorkspaceIntegrationID: "workspace-1", SlackUserID: "U123"}, want: ErrUserSlackIdentitySlackWorkspaceIDRequired},
		{name: "missing slack user id", user: user, input: LinkUserSlackIdentityInput{ResolutionMethod: SlackIdentityResolutionMethodAuthenticatedEmail, WorkspaceIntegrationID: "workspace-1", SlackWorkspaceID: "T123"}, want: ErrUserSlackIdentitySlackUserIDRequired},
	}

	for _, tc := range validationCases {
		t.Run(tc.name, func(t *testing.T) {
			_, linkErr := svc.Link(context.Background(), tc.user, tc.input)
			if !errors.Is(linkErr, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, linkErr)
			}
		})
	}

	_, err = svc.Link(context.Background(), user, LinkUserSlackIdentityInput{
		ResolutionMethod:       SlackIdentityResolutionMethodAuthenticatedEmail,
		WorkspaceIntegrationID: "workspace-1",
		SlackWorkspaceID:       "T123",
		SlackUserID:            "U999",
	})
	if !errors.Is(err, ErrUserSlackIdentityCandidateChanged) {
		t.Fatalf("expected candidate changed for mismatched slack user, got %v", err)
	}

	_, _, err = NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{}).ResolveByAuthenticatedEmail(context.Background(), domain.User{ID: "user-1"})
	if !errors.Is(err, ErrUserSlackIdentityEmailRequired) {
		t.Fatalf("expected missing email error, got %v", err)
	}
}

func TestUserSlackIdentityService_SetEnabledValidationAndGetIdentity(t *testing.T) {
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)
	now := time.Date(2026, 7, 1, 14, 21, 0, 0, time.UTC)
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

	identity, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
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

	svc := NewUserSlackIdentityService(identityRepo, workspaceRepo, fakeSlackDirectoryClient{})
	state, err := svc.Get(context.Background(), domain.User{ID: "user-1", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("get state with identity: %v", err)
	}
	if state.Identity == nil || state.Identity.ID != identity.ID {
		t.Fatalf("expected linked identity in state, got %+v", state.Identity)
	}

	_, err = svc.SetEnabled(context.Background(), domain.User{}, testBoolPtr(true))
	if !errors.Is(err, ErrUserSlackIdentityUserIDRequired) {
		t.Fatalf("expected missing user id error, got %v", err)
	}
	_, err = svc.SetEnabled(context.Background(), domain.User{ID: "user-1"}, nil)
	if !errors.Is(err, ErrUserSlackIdentityEnabledRequired) {
		t.Fatalf("expected enabled required error, got %v", err)
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
