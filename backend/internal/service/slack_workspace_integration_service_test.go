package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type fakeSlackAuthClient struct {
	result platformslack.AuthTestResult
	err    error
}

func (f fakeSlackAuthClient) TestAuthentication(_ context.Context, _ string) (platformslack.AuthTestResult, error) {
	if f.err != nil {
		return platformslack.AuthTestResult{}, f.err
	}
	return f.result, nil
}

type fakeSlackWorkspaceIntegrationRepository struct {
	mu                sync.Mutex
	integration       domain.SlackWorkspaceIntegration
	has               bool
	connectErr        error
	updateLastTestErr error
}

func (r *fakeSlackWorkspaceIntegrationRepository) Get(_ context.Context) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	return r.integration, nil
}

func (r *fakeSlackWorkspaceIntegrationRepository) ConnectOrReplace(_ context.Context, integration domain.SlackWorkspaceIntegration, replaceDifferentWorkspace bool) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connectErr != nil {
		return domain.SlackWorkspaceIntegration{}, r.connectErr
	}
	if !r.has {
		r.integration = integration
		r.has = true
		return integration, nil
	}
	if r.integration.WorkspaceID != integration.WorkspaceID && !replaceDifferentWorkspace {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationReplaceRequired
	}
	integration.ID = r.integration.ID
	integration.Enabled = r.integration.Enabled
	integration.CreatedAt = r.integration.CreatedAt
	r.integration = integration
	return integration, nil
}

func (r *fakeSlackWorkspaceIntegrationRepository) SetEnabled(_ context.Context, enabled bool, updatedAt time.Time) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	r.integration.Enabled = enabled
	r.integration.UpdatedAt = updatedAt
	return r.integration, nil
}

func (r *fakeSlackWorkspaceIntegrationRepository) UpdateLastTestResult(_ context.Context, testedAt time.Time, succeeded bool) (domain.SlackWorkspaceIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	if r.updateLastTestErr != nil {
		return domain.SlackWorkspaceIntegration{}, r.updateLastTestErr
	}
	r.integration.LastTestedAt = &testedAt
	r.integration.LastTestSucceeded = boolPtrSvc(succeeded)
	r.integration.UpdatedAt = testedAt
	return r.integration, nil
}

func (r *fakeSlackWorkspaceIntegrationRepository) Delete(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.has {
		return repository.ErrSlackWorkspaceIntegrationNotFound
	}
	r.has = false
	return nil
}

func TestSlackWorkspaceIntegrationService_ConnectAndReplace(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T123", WorkspaceName: strPtrSvc("Coyote"), BotID: strPtrSvc("B123")}})
	svc.now = func() time.Time { return now }

	created, err := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{BotToken: "xoxb-1"})
	if err != nil {
		t.Fatalf("connect integration: %v", err)
	}
	if created.WorkspaceID != "T123" {
		t.Fatalf("unexpected workspace id %q", created.WorkspaceID)
	}
	if created.BotID == nil || *created.BotID != "B123" {
		t.Fatalf("expected bot id B123, got %v", created.BotID)
	}

	svc.client = fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T999"}}
	_, replaceErr := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{BotToken: "xoxb-2"})
	if !errors.Is(replaceErr, ErrSlackWorkspaceReplaceRequired) {
		t.Fatalf("expected replacement required error, got %v", replaceErr)
	}
	if repo.integration.WorkspaceID != "T123" {
		t.Fatalf("expected existing workspace to remain unchanged, got %q", repo.integration.WorkspaceID)
	}

	replaced, err := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{BotToken: "xoxb-2", ReplaceExisting: true})
	if err != nil {
		t.Fatalf("replace integration: %v", err)
	}
	if replaced.WorkspaceID != "T999" {
		t.Fatalf("expected replaced workspace, got %q", replaced.WorkspaceID)
	}
}

func TestSlackWorkspaceIntegrationService_ConnectMapsRepositoryReplacementConflict(t *testing.T) {
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-old",
			Enabled:        true,
			ConnectedAt:    time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			CreatedAt:      time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T999"}})

	_, err := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{BotToken: "xoxb-new"})
	if !errors.Is(err, ErrSlackWorkspaceReplaceRequired) {
		t.Fatalf("expected replace required error, got %v", err)
	}
}

func TestSlackWorkspaceIntegrationService_InvalidTokenPersistsNothing(t *testing.T) {
	repo := &fakeSlackWorkspaceIntegrationRepository{}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{err: platformslack.ErrInvalidAuth})

	_, err := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{BotToken: "xoxb-invalid"})
	if !errors.Is(err, ErrSlackWorkspaceInvalidAuth) {
		t.Fatalf("expected invalid auth error, got %v", err)
	}
	if repo.has {
		t.Fatalf("expected no persisted integration on invalid token")
	}
}

func TestSlackWorkspaceIntegrationService_FailedReplacementPreservesPriorIntegration(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 35, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-old",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		connectErr: errors.New("write failed"),
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T999"}})

	_, err := svc.Connect(context.Background(), ConnectSlackWorkspaceIntegrationInput{
		BotToken:        "xoxb-new",
		ReplaceExisting: true,
	})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("expected write failure, got %v", err)
	}
	if repo.integration.WorkspaceID != "T123" || repo.integration.BotTokenSecret != "xoxb-old" {
		t.Fatalf("expected previous integration to remain unchanged, got %+v", repo.integration)
	}
}

func TestSlackWorkspaceIntegrationService_TestConnectionMarksResult(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 40, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T123"}})
	svc.now = func() time.Time { return now.Add(time.Minute) }

	integration, err := svc.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if integration.LastTestSucceeded == nil || !*integration.LastTestSucceeded {
		t.Fatalf("expected successful test result")
	}

	svc.client = fakeSlackAuthClient{err: platformslack.ErrInvalidAuth}
	_, err = svc.TestConnection(context.Background())
	if !errors.Is(err, ErrSlackWorkspaceInvalidAuth) {
		t.Fatalf("expected invalid auth error, got %v", err)
	}
	if repo.integration.LastTestSucceeded == nil || *repo.integration.LastTestSucceeded {
		t.Fatalf("expected failed test result to be persisted")
	}
}

func TestSlackWorkspaceIntegrationService_TestConnectionFailureLogsPersistenceError(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 45, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		updateLastTestErr: errors.New("persist failed"),
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{err: platformslack.ErrInvalidAuth})
	svc.now = func() time.Time { return now.Add(time.Minute) }

	var logs bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalWriter)

	_, err := svc.TestConnection(context.Background())
	if !errors.Is(err, ErrSlackWorkspaceInvalidAuth) {
		t.Fatalf("expected slack auth error, got %v", err)
	}
	logged := logs.String()
	if !strings.Contains(logged, "failed test status persistence") {
		t.Fatalf("expected persistence failure log, got %q", logged)
	}
	if strings.Contains(logged, "xoxb-secret") {
		t.Fatalf("did not expect token in logs: %q", logged)
	}
	if strings.Contains(err.Error(), "xoxb-secret") {
		t.Fatalf("did not expect token in returned error: %q", err.Error())
	}
}

func TestSlackWorkspaceIntegrationService_TestConnectionSuccessReturnsPersistenceFailure(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 50, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		updateLastTestErr: errors.New("persist success state failed"),
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T123"}})
	svc.now = func() time.Time { return now.Add(time.Minute) }

	_, err := svc.TestConnection(context.Background())
	if err == nil || err.Error() != "persist success state failed" {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if strings.Contains(err.Error(), "xoxb-secret") {
		t.Fatalf("did not expect token in returned error: %q", err.Error())
	}
}

func TestSlackWorkspaceIntegrationService_SetEnabledAndDisconnect(t *testing.T) {
	now := time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{})
	svc.now = func() time.Time { return now.Add(time.Minute) }

	updated, err := svc.SetEnabled(context.Background(), boolPtrSvc(false))
	if err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected integration to be disabled")
	}

	if err := svc.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect integration: %v", err)
	}
	if repo.has {
		t.Fatalf("expected integration to be removed after disconnect")
	}
}

func TestSlackWorkspaceIntegrationService_SetEnabledRequiresValue(t *testing.T) {
	svc := NewSlackWorkspaceIntegrationService(&fakeSlackWorkspaceIntegrationRepository{}, fakeSlackAuthClient{})

	_, err := svc.SetEnabled(context.Background(), nil)
	if !errors.Is(err, ErrSlackWorkspaceEnabledRequired) {
		t.Fatalf("expected enabled required error, got %v", err)
	}
}

func TestSlackWorkspaceIntegrationService_TestConnectionWorkspaceMismatchMarksFailure(t *testing.T) {
	now := time.Date(2026, 7, 1, 13, 10, 0, 0, time.UTC)
	repo := &fakeSlackWorkspaceIntegrationRepository{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:             "int-1",
			WorkspaceID:    "T123",
			BotTokenSecret: "xoxb-secret",
			Enabled:        true,
			ConnectedAt:    now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	svc := NewSlackWorkspaceIntegrationService(repo, fakeSlackAuthClient{result: platformslack.AuthTestResult{WorkspaceID: "T999"}})
	svc.now = func() time.Time { return now.Add(time.Minute) }

	_, err := svc.TestConnection(context.Background())
	if !errors.Is(err, ErrSlackWorkspaceInvalidAuth) {
		t.Fatalf("expected invalid auth for workspace mismatch, got %v", err)
	}
	if repo.integration.LastTestSucceeded == nil || *repo.integration.LastTestSucceeded {
		t.Fatalf("expected failed test result to be persisted on mismatch")
	}
}

func TestMapSlackClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid auth", err: platformslack.ErrInvalidAuth, want: ErrSlackWorkspaceInvalidAuth},
		{name: "token revoked", err: platformslack.ErrTokenRevoked, want: ErrSlackWorkspaceTokenRevoked},
		{name: "inactive", err: platformslack.ErrAccountInactive, want: ErrSlackWorkspaceAccountInactive},
		{name: "rate limited", err: platformslack.ErrRateLimited, want: ErrSlackWorkspaceRateLimited},
		{name: "malformed", err: platformslack.ErrMalformedResponse, want: ErrSlackWorkspaceMalformedResponse},
		{name: "upstream", err: platformslack.ErrUpstreamFailure, want: ErrSlackWorkspaceUpstream},
		{name: "auth failed fallback", err: platformslack.ErrAuthTestFailed, want: ErrSlackWorkspaceInvalidAuth},
		{name: "context canceled passthrough", err: context.Canceled, want: context.Canceled},
		{name: "context deadline passthrough", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "unknown fallback", err: errors.New("boom"), want: ErrSlackWorkspaceUpstream},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapSlackClientError(tc.err)
			if !errors.Is(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func strPtrSvc(value string) *string {
	v := value
	return &v
}

func boolPtrSvc(value bool) *bool {
	v := value
	return &v
}
