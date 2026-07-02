package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type fakeSampleNotificationSender struct {
	recipients []string
	err        error
}

func (f *fakeSampleNotificationSender) SendSampleBuildFailure(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.recipients...), nil
}

type fakeNotificationAdminService struct {
	listTargetsResult             []domain.NotificationTarget
	listTargetsErr                error
	getOwnedEmailTargetResult     domain.NotificationTarget
	getOwnedEmailTargetErr        error
	ensureOwnedEmailTargetResult  domain.NotificationTarget
	ensureOwnedEmailTargetErr     error
	setOwnedEmailTargetResult     domain.NotificationTarget
	setOwnedEmailTargetErr        error
	getNotificationDefaultsResult service.NotificationDefaultsState
	getNotificationDefaultsErr    error
	setNotificationDefaultsResult service.NotificationDefaultsState
	setNotificationDefaultsErr    error
	getPreferenceResult           service.CommitAuthorFailureNotificationPreferenceState
	getPreferenceErr              error
	setPreferenceResult           service.CommitAuthorFailureNotificationPreferenceState
	setPreferenceErr              error
	getSuccessPreferenceResult    service.CommitAuthorSuccessNotificationPreferenceState
	getSuccessPreferenceErr       error
	setSuccessPreferenceResult    service.CommitAuthorSuccessNotificationPreferenceState
	setSuccessPreferenceErr       error
	createTargetResult            domain.NotificationTarget
	createTargetErr               error
	updateTargetResult            domain.NotificationTarget
	updateTargetErr               error
	listSubscriptionsResult       []domain.NotificationSubscription
	listSubscriptionsErr          error
	createSubscriptionResult      domain.NotificationSubscription
	createSubscriptionErr         error
	updateSubscriptionResult      domain.NotificationSubscription
	updateSubscriptionErr         error
	deleteSubscriptionErr         error
}

type fakePersonalSlackIdentityService struct {
	getState      service.UserSlackIdentityState
	getErr        error
	resolveResult *service.ResolvedUserSlackIdentityCandidate
	resolveMatch  bool
	resolveErr    error
	linkResult    domain.UserSlackIdentity
	linkErr       error
	setResult     domain.UserSlackIdentity
	setErr        error
	unlinkErr     error
	linkInput     service.LinkUserSlackIdentityInput
}

type fakeWrappedError struct {
	message string
	cause   error
}

func (e fakeWrappedError) Error() string {
	return e.message
}

func (e fakeWrappedError) Unwrap() error {
	return e.cause
}

func (f *fakePersonalSlackIdentityService) Get(_ context.Context, _ domain.User) (service.UserSlackIdentityState, error) {
	return f.getState, f.getErr
}

func (f *fakePersonalSlackIdentityService) ResolveByAuthenticatedEmail(_ context.Context, _ domain.User) (*service.ResolvedUserSlackIdentityCandidate, bool, error) {
	return f.resolveResult, f.resolveMatch, f.resolveErr
}

func (f *fakePersonalSlackIdentityService) Link(_ context.Context, _ domain.User, input service.LinkUserSlackIdentityInput) (domain.UserSlackIdentity, error) {
	f.linkInput = input
	return f.linkResult, f.linkErr
}

func (f *fakePersonalSlackIdentityService) SetEnabled(_ context.Context, _ domain.User, _ *bool) (domain.UserSlackIdentity, error) {
	return f.setResult, f.setErr
}

func (f *fakePersonalSlackIdentityService) Unlink(_ context.Context, _ domain.User) error {
	return f.unlinkErr
}

func (f *fakeNotificationAdminService) ListTargets(_ context.Context) ([]domain.NotificationTarget, error) {
	return f.listTargetsResult, f.listTargetsErr
}

func (f *fakeNotificationAdminService) GetOwnedEmailTarget(_ context.Context, _ domain.User) (domain.NotificationTarget, error) {
	return f.getOwnedEmailTargetResult, f.getOwnedEmailTargetErr
}

func (f *fakeNotificationAdminService) EnsureOwnedEmailTarget(_ context.Context, _ domain.User) (domain.NotificationTarget, error) {
	return f.ensureOwnedEmailTargetResult, f.ensureOwnedEmailTargetErr
}

func (f *fakeNotificationAdminService) SetOwnedEmailTargetEnabled(_ context.Context, _ domain.User, _ *bool) (domain.NotificationTarget, error) {
	return f.setOwnedEmailTargetResult, f.setOwnedEmailTargetErr
}

func (f *fakeNotificationAdminService) GetNotificationDefaults(_ context.Context) (service.NotificationDefaultsState, error) {
	return f.getNotificationDefaultsResult, f.getNotificationDefaultsErr
}

func (f *fakeNotificationAdminService) SetNotificationDefaults(_ context.Context, _ *bool, _ *bool) (service.NotificationDefaultsState, error) {
	return f.setNotificationDefaultsResult, f.setNotificationDefaultsErr
}

func (f *fakeNotificationAdminService) GetCommitAuthorFailureNotificationPreference(_ context.Context, _ domain.User) (service.CommitAuthorFailureNotificationPreferenceState, error) {
	return f.getPreferenceResult, f.getPreferenceErr
}

func (f *fakeNotificationAdminService) SetCommitAuthorFailureNotificationPreference(_ context.Context, _ domain.User, _ service.UpdateCommitAuthorNotificationPreferenceInput) (service.CommitAuthorFailureNotificationPreferenceState, error) {
	return f.setPreferenceResult, f.setPreferenceErr
}

func (f *fakeNotificationAdminService) GetCommitAuthorSuccessNotificationPreference(_ context.Context, _ domain.User) (service.CommitAuthorSuccessNotificationPreferenceState, error) {
	return f.getSuccessPreferenceResult, f.getSuccessPreferenceErr
}

func (f *fakeNotificationAdminService) SetCommitAuthorSuccessNotificationPreference(_ context.Context, _ domain.User, _ service.UpdateCommitAuthorNotificationPreferenceInput) (service.CommitAuthorSuccessNotificationPreferenceState, error) {
	return f.setSuccessPreferenceResult, f.setSuccessPreferenceErr
}

func (f *fakeNotificationAdminService) CreateTarget(_ context.Context, _ service.CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.createTargetResult, f.createTargetErr
}

func (f *fakeNotificationAdminService) CreateEmailTarget(_ context.Context, _ service.CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.createTargetResult, f.createTargetErr
}

func (f *fakeNotificationAdminService) UpdateTarget(_ context.Context, _ string, _ service.UpdateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.updateTargetResult, f.updateTargetErr
}

func (f *fakeNotificationAdminService) ListSubscriptions(_ context.Context, _ service.ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error) {
	return f.listSubscriptionsResult, f.listSubscriptionsErr
}

func (f *fakeNotificationAdminService) CreateSubscription(_ context.Context, _ service.CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	return f.createSubscriptionResult, f.createSubscriptionErr
}

func (f *fakeNotificationAdminService) UpdateSubscription(_ context.Context, _ string, _ service.UpdateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	return f.updateSubscriptionResult, f.updateSubscriptionErr
}

func (f *fakeNotificationAdminService) DeleteTarget(_ context.Context, _ string) error {
	return nil
}

func (f *fakeNotificationAdminService) DeleteSubscription(_ context.Context, _ string) error {
	return f.deleteSubscriptionErr
}

func TestNotificationHandler_SendSampleBuildFailure_NotFoundWhenUnavailable(t *testing.T) {
	h := NewNotificationHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, res.Code, res.Body.String())
	}
}

func TestNotificationHandler_HasSampleSender(t *testing.T) {
	var nilHandler *NotificationHandler
	if nilHandler.HasSampleSender() {
		t.Fatal("expected nil handler to report no sample sender")
	}

	h := NewNotificationHandler(nil)
	if h.HasSampleSender() {
		t.Fatal("expected handler without sender to report false")
	}

	h = NewNotificationHandler(&fakeSampleNotificationSender{})
	if !h.HasSampleSender() {
		t.Fatal("expected handler with sender to report true")
	}
}

func TestNotificationHandler_CurrentRequestUserAndTrimQueryString(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeDisabled)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications?project_id=%20project-1%20", nil)
	user, ok := h.currentRequestUser(req)
	if !ok {
		t.Fatal("expected disabled auth mode to provide a request user")
	}
	if user.ID == "" {
		t.Fatalf("expected disabled mode user, got %+v", user)
	}

	trimmed := trimQueryString(req, "project_id")
	if trimmed == nil || *trimmed != "project-1" {
		t.Fatalf("expected trimmed project_id, got %+v", trimmed)
	}

	missing := trimQueryString(req, "job_id")
	if missing != nil {
		t.Fatalf("expected nil missing query string, got %+v", missing)
	}
}

func TestNotificationHandler_SendSampleBuildFailure_ConflictForDisabledOrMissingRecipients(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "disabled", err: buildsvc.ErrEmailNotificationsDisabled},
		{name: "no recipients", err: buildsvc.ErrEmailNotificationRecipientsNotConfigured},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(&fakeSampleNotificationSender{err: tc.err})
			req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
			res := httptest.NewRecorder()

			h.SendSampleBuildFailure(res, req)

			if res.Code != http.StatusConflict {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusConflict, res.Code, res.Body.String())
			}
		})
	}

	t.Run("success put target required returns neutral conflict message", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{setSuccessPreferenceErr: service.ErrNotificationPreferencePersonalTargetRequired})
		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

		req := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
		req = req.WithContext(auth.WithUser(req.Context(), user))
		res := httptest.NewRecorder()

		h.SetMyCommitAuthorSuccessNotificationPreference(res, req)

		if res.Code != http.StatusConflict {
			t.Fatalf("expected status %d, got %d body=%s", http.StatusConflict, res.Code, res.Body.String())
		}
		if !strings.Contains(res.Body.String(), "commit-author notifications") {
			t.Fatalf("expected neutral conflict message, got body=%s", res.Body.String())
		}
		if strings.Contains(res.Body.String(), "failure notifications") {
			t.Fatalf("expected success conflict body to avoid failure-specific wording, got body=%s", res.Body.String())
		}
	})
}

func TestNotificationHandler_SendSampleBuildFailure_InternalError(t *testing.T) {
	h := NewNotificationHandler(&fakeSampleNotificationSender{err: errors.New("smtp unavailable")})
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusInternalServerError, res.Code, res.Body.String())
	}
}

func TestNotificationHandler_SendSampleBuildFailure_Success(t *testing.T) {
	h := NewNotificationHandler(&fakeSampleNotificationSender{recipients: []string{"<dev@example.com>", "<qa@example.com>"}})
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	var payload struct {
		Data struct {
			OK         bool     `json:"ok"`
			Recipients []string `json:"recipients"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Data.OK {
		t.Fatal("expected ok=true")
	}
	if len(payload.Data.Recipients) != 2 {
		t.Fatalf("expected two recipients, got %v", payload.Data.Recipients)
	}
}

func TestNotificationHandler_MyEmailTargetEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		getOwnedEmailTargetErr: repository.ErrNotificationTargetNotFound,
		ensureOwnedEmailTargetResult: domain.NotificationTarget{
			ID:          "target-1",
			OwnerUserID: stringPtr("user-1"),
			Type:        domain.NotificationTargetTypeEmail,
			Name:        "User One",
			Recipient:   "<user@example.com>",
			Enabled:     true,
		},
	})
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
	getRes := httptest.NewRecorder()
	h.GetMyEmailTarget(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get my target status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	if targetValue, exists := getData["target"]; !exists || targetValue != nil {
		t.Fatalf("expected null target payload, got %v", getData["target"])
	}

	ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
	ensureReq = ensureReq.WithContext(auth.WithUser(ensureReq.Context(), user))
	ensureRes := httptest.NewRecorder()
	h.EnsureMyEmailTarget(ensureRes, ensureReq)
	if ensureRes.Code != http.StatusOK {
		t.Fatalf("expected ensure my target status %d, got %d body=%s", http.StatusOK, ensureRes.Code, ensureRes.Body.String())
	}
	ensureData := decodeDataMap(t, ensureRes)
	if ensureData["id"] != "target-1" {
		t.Fatalf("expected ensured target id target-1, got %v", ensureData["id"])
	}
}

func TestNotificationHandler_MySlackIdentityEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	serviceDouble := &fakePersonalSlackIdentityService{
		getState: service.UserSlackIdentityState{
			WorkspaceStatus: service.SlackIdentityWorkspaceStatusReady,
			Workspace: &service.SlackWorkspaceReference{
				ID:               "workspace-1",
				SlackWorkspaceID: "T123",
				Name:             strPtr("Coyote"),
			},
			Identity: &domain.UserSlackIdentity{
				ID:                          "identity-1",
				UserID:                      "user-1",
				SlackWorkspaceIntegrationID: "workspace-1",
				SlackUserID:                 "U123",
				SlackDisplayName:            strPtr("Bryan"),
				Enabled:                     true,
				LinkedAt:                    time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
			},
		},
		resolveResult: &service.ResolvedUserSlackIdentityCandidate{
			ResolutionMethod: service.SlackIdentityResolutionMethodAuthenticatedEmail,
			Workspace: service.SlackWorkspaceReference{
				ID:               "workspace-1",
				SlackWorkspaceID: "T123",
				Name:             strPtr("Coyote"),
			},
			SlackUserID: "U123",
			DisplayName: strPtr("Bryan"),
		},
		resolveMatch: true,
		linkResult: domain.UserSlackIdentity{
			ID:                          "identity-1",
			UserID:                      "user-1",
			SlackWorkspaceIntegrationID: "workspace-1",
			SlackUserID:                 "U123",
			SlackDisplayName:            strPtr("Bryan"),
			Enabled:                     true,
			LinkedAt:                    time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		},
		setResult: domain.UserSlackIdentity{
			ID:                          "identity-1",
			UserID:                      "user-1",
			SlackWorkspaceIntegrationID: "workspace-1",
			SlackUserID:                 "U123",
			SlackDisplayName:            strPtr("Bryan"),
			Enabled:                     false,
			LinkedAt:                    time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		},
	}
	h.SetPersonalSlackIdentityService(serviceDouble)

	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/slack-identity", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
	getRes := httptest.NewRecorder()
	h.GetMySlackIdentity(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	if getData["workspace_status"] != service.SlackIdentityWorkspaceStatusReady {
		t.Fatalf("expected ready workspace status, got %v", getData["workspace_status"])
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`))
	resolveReq = resolveReq.WithContext(auth.WithUser(resolveReq.Context(), user))
	resolveRes := httptest.NewRecorder()
	h.ResolveMySlackIdentity(resolveRes, resolveReq)
	if resolveRes.Code != http.StatusOK {
		t.Fatalf("expected resolve status %d, got %d body=%s", http.StatusOK, resolveRes.Code, resolveRes.Body.String())
	}
	resolveData := decodeDataMap(t, resolveRes)
	if resolveData["matched"] != true {
		t.Fatalf("expected matched=true, got %v", resolveData["matched"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity", bytes.NewBufferString(`{"resolution_method":"authenticated_email","workspace_integration_id":"workspace-1","slack_workspace_id":"T123","slack_user_id":"U123"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), user))
	createRes := httptest.NewRecorder()
	h.CreateMySlackIdentity(createRes, createReq)
	if createRes.Code != http.StatusOK {
		t.Fatalf("expected create status %d, got %d body=%s", http.StatusOK, createRes.Code, createRes.Body.String())
	}
	if serviceDouble.linkInput.SlackUserID != "U123" {
		t.Fatalf("expected link input slack_user_id U123, got %+v", serviceDouble.linkInput)
	}
	if serviceDouble.linkInput.SlackWorkspaceID != "T123" {
		t.Fatalf("expected link input slack_workspace_id T123, got %+v", serviceDouble.linkInput)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/me/slack-identity", bytes.NewBufferString(`{"enabled":false}`))
	patchReq = patchReq.WithContext(auth.WithUser(patchReq.Context(), user))
	patchRes := httptest.NewRecorder()
	h.PatchMySlackIdentity(patchRes, patchReq)
	if patchRes.Code != http.StatusOK {
		t.Fatalf("expected patch status %d, got %d body=%s", http.StatusOK, patchRes.Code, patchRes.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/me/slack-identity", nil)
	deleteReq = deleteReq.WithContext(auth.WithUser(deleteReq.Context(), user))
	deleteRes := httptest.NewRecorder()
	h.DeleteMySlackIdentity(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete status %d, got %d body=%s", http.StatusNoContent, deleteRes.Code, deleteRes.Body.String())
	}
}

func TestNotificationHandler_MySlackIdentityRequiresAuthentication(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/me/slack-identity", nil),
		httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`)),
		httptest.NewRequest(http.MethodPost, "/api/me/slack-identity", bytes.NewBufferString(`{"resolution_method":"authenticated_email","workspace_integration_id":"workspace-1","slack_workspace_id":"T123","slack_user_id":"U123"}`)),
		httptest.NewRequest(http.MethodPatch, "/api/me/slack-identity", bytes.NewBufferString(`{"enabled":false}`)),
		httptest.NewRequest(http.MethodDelete, "/api/me/slack-identity", nil),
	} {
		res := httptest.NewRecorder()
		switch req.Method {
		case http.MethodGet:
			h.GetMySlackIdentity(res, req)
		case http.MethodPost:
			if strings.HasSuffix(req.URL.Path, "/resolve") {
				h.ResolveMySlackIdentity(res, req)
			} else {
				h.CreateMySlackIdentity(res, req)
			}
		case http.MethodPatch:
			h.PatchMySlackIdentity(res, req)
		case http.MethodDelete:
			h.DeleteMySlackIdentity(res, req)
		}
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthorized for %s %s, got %d body=%s", req.Method, req.URL.Path, res.Code, res.Body.String())
		}
	}
}

func TestNotificationHandler_MySlackIdentityConflictIsPrivacySafe(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{linkErr: service.ErrUserSlackIdentityConflict})

	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	req := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity", bytes.NewBufferString(`{"resolution_method":"authenticated_email","workspace_integration_id":"workspace-1","slack_workspace_id":"T123","slack_user_id":"U123"}`))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	res := httptest.NewRecorder()

	h.CreateMySlackIdentity(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected conflict status %d, got %d body=%s", http.StatusConflict, res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "user@example.com") || strings.Contains(res.Body.String(), "user-2") {
		t.Fatalf("expected privacy-safe conflict, got body=%s", res.Body.String())
	}
}

func TestNotificationHandler_MySlackIdentityResolveInvalidAuthReturnsBadRequest(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{resolveErr: service.ErrSlackWorkspaceInvalidAuth})

	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	req := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	res := httptest.NewRecorder()

	h.ResolveMySlackIdentity(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request status %d, got %d body=%s", http.StatusBadRequest, res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), service.ErrSlackWorkspaceInvalidAuth.Error()) {
		t.Fatalf("expected invalid auth details in body, got %s", res.Body.String())
	}
}

func TestNotificationHandler_MySlackIdentityResolveMissingScopeReturnsBadRequest(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{resolveErr: fakeWrappedError{
		message: "slack member lookup requires the users:read.email scope. Ask an administrator to add it and reinstall or reauthorize the Slack app",
		cause:   service.ErrUserSlackIdentityMissingScope,
	}})

	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	req := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	res := httptest.NewRecorder()

	h.ResolveMySlackIdentity(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request status %d, got %d body=%s", http.StatusBadRequest, res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "slack member lookup requires the users:read.email scope") {
		t.Fatalf("expected missing scope details in body, got %s", res.Body.String())
	}
}

func TestNotificationHandler_MySlackIdentityEndpointValidationAndErrorMappings(t *testing.T) {
	t.Run("resolve invalid body and invalid method", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeOIDC)
		h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{})
		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

		invalidBodyReq := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":`))
		invalidBodyReq = invalidBodyReq.WithContext(auth.WithUser(invalidBodyReq.Context(), user))
		invalidBodyRes := httptest.NewRecorder()
		h.ResolveMySlackIdentity(invalidBodyRes, invalidBodyReq)
		if invalidBodyRes.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid body status %d, got %d body=%s", http.StatusBadRequest, invalidBodyRes.Code, invalidBodyRes.Body.String())
		}

		invalidMethodReq := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"email"}`))
		invalidMethodReq = invalidMethodReq.WithContext(auth.WithUser(invalidMethodReq.Context(), user))
		invalidMethodRes := httptest.NewRecorder()
		h.ResolveMySlackIdentity(invalidMethodRes, invalidMethodReq)
		if invalidMethodRes.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid method status %d, got %d body=%s", http.StatusBadRequest, invalidMethodRes.Code, invalidMethodRes.Body.String())
		}
	})

	t.Run("resolve, create, patch, delete error mappings", func(t *testing.T) {
		testCases := []struct {
			name       string
			serviceErr error
			statusCode int
			invoke     func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder)
			request    *http.Request
		}{
			{
				name:       "resolve rate limited",
				serviceErr: service.ErrSlackWorkspaceRateLimited,
				statusCode: http.StatusTooManyRequests,
				request:    httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`)),
				invoke: func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder) {
					h.ResolveMySlackIdentity(res, req)
				},
			},
			{
				name:       "resolve upstream",
				serviceErr: service.ErrSlackWorkspaceUpstream,
				statusCode: http.StatusBadGateway,
				request:    httptest.NewRequest(http.MethodPost, "/api/me/slack-identity/resolve", bytes.NewBufferString(`{"method":"authenticated_email"}`)),
				invoke: func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder) {
					h.ResolveMySlackIdentity(res, req)
				},
			},
			{
				name:       "create member unavailable",
				serviceErr: service.ErrUserSlackIdentityMemberUnavailable,
				statusCode: http.StatusNotFound,
				request:    httptest.NewRequest(http.MethodPost, "/api/me/slack-identity", bytes.NewBufferString(`{"resolution_method":"authenticated_email","workspace_integration_id":"workspace-1","slack_workspace_id":"T123","slack_user_id":"U123"}`)),
				invoke: func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder) {
					h.CreateMySlackIdentity(res, req)
				},
			},
			{
				name:       "patch enabled required",
				serviceErr: service.ErrUserSlackIdentityEnabledRequired,
				statusCode: http.StatusBadRequest,
				request:    httptest.NewRequest(http.MethodPatch, "/api/me/slack-identity", bytes.NewBufferString(`{"enabled":true}`)),
				invoke: func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder) {
					h.PatchMySlackIdentity(res, req)
				},
			},
			{
				name:       "delete unexpected internal",
				serviceErr: errors.New("boom"),
				statusCode: http.StatusInternalServerError,
				request:    httptest.NewRequest(http.MethodDelete, "/api/me/slack-identity", nil),
				invoke: func(h *NotificationHandler, req *http.Request, res *httptest.ResponseRecorder) {
					h.DeleteMySlackIdentity(res, req)
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				serviceDouble := &fakePersonalSlackIdentityService{}
				switch tc.request.Method {
				case http.MethodPost:
					if strings.HasSuffix(tc.request.URL.Path, "/resolve") {
						serviceDouble.resolveErr = tc.serviceErr
					} else {
						serviceDouble.linkErr = tc.serviceErr
					}
				case http.MethodPatch:
					serviceDouble.setErr = tc.serviceErr
				case http.MethodDelete:
					serviceDouble.unlinkErr = tc.serviceErr
				}

				h := NewNotificationHandler(nil)
				h.SetAuthorization(auth.ModeOIDC)
				h.SetPersonalSlackIdentityService(serviceDouble)
				user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
				req := tc.request.WithContext(auth.WithUser(tc.request.Context(), user))
				res := httptest.NewRecorder()

				tc.invoke(h, req, res)
				if res.Code != tc.statusCode {
					t.Fatalf("expected status %d, got %d body=%s", tc.statusCode, res.Code, res.Body.String())
				}
			})
		}
	})

	t.Run("create and patch state reload failures map through helper", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeOIDC)
		h.SetPersonalSlackIdentityService(&fakePersonalSlackIdentityService{
			linkResult: domain.UserSlackIdentity{ID: "identity-1", UserID: "user-1", SlackWorkspaceIntegrationID: "workspace-1", SlackUserID: "U123", Enabled: true, LinkedAt: time.Now().UTC()},
			setResult:  domain.UserSlackIdentity{ID: "identity-1", UserID: "user-1", SlackWorkspaceIntegrationID: "workspace-1", SlackUserID: "U123", Enabled: false, LinkedAt: time.Now().UTC()},
			getErr:     repository.ErrUserSlackIdentityNotFound,
		})

		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
		createReq := httptest.NewRequest(http.MethodPost, "/api/me/slack-identity", bytes.NewBufferString(`{"resolution_method":"authenticated_email","workspace_integration_id":"workspace-1","slack_workspace_id":"T123","slack_user_id":"U123"}`))
		createReq = createReq.WithContext(auth.WithUser(createReq.Context(), user))
		createRes := httptest.NewRecorder()
		h.CreateMySlackIdentity(createRes, createReq)
		if createRes.Code != http.StatusNotFound {
			t.Fatalf("expected state reload not found on create, got %d body=%s", createRes.Code, createRes.Body.String())
		}

		patchReq := httptest.NewRequest(http.MethodPatch, "/api/me/slack-identity", bytes.NewBufferString(`{"enabled":false}`))
		patchReq = patchReq.WithContext(auth.WithUser(patchReq.Context(), user))
		patchRes := httptest.NewRecorder()
		h.PatchMySlackIdentity(patchRes, patchReq)
		if patchRes.Code != http.StatusNotFound {
			t.Fatalf("expected state reload not found on patch, got %d body=%s", patchRes.Code, patchRes.Body.String())
		}
	})
}

func TestNotificationHandler_MyEmailTargetEndpointsRequireAuthentication(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
	getRes := httptest.NewRecorder()
	h.GetMyEmailTarget(getRes, getReq)
	if getRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected get my target unauthorized status %d, got %d body=%s", http.StatusUnauthorized, getRes.Code, getRes.Body.String())
	}

	ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
	ensureRes := httptest.NewRecorder()
	h.EnsureMyEmailTarget(ensureRes, ensureReq)
	if ensureRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected ensure my target unauthorized status %d, got %d body=%s", http.StatusUnauthorized, ensureRes.Code, ensureRes.Body.String())
	}

	setReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-targets/email", bytes.NewBufferString(`{"enabled":false}`))
	setRes := httptest.NewRecorder()
	h.SetMyEmailTarget(setRes, setReq)
	if setRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected set my target unauthorized status %d, got %d body=%s", http.StatusUnauthorized, setRes.Code, setRes.Body.String())
	}
}

func TestNotificationHandler_MyEmailTargetEndpointErrors(t *testing.T) {
	t.Run("endpoint unavailable", func(t *testing.T) {
		h := NewNotificationHandler(nil)

		getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
		getRes := httptest.NewRecorder()
		h.GetMyEmailTarget(getRes, getReq)
		if getRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable get status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
		}

		ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
		ensureRes := httptest.NewRecorder()
		h.EnsureMyEmailTarget(ensureRes, ensureReq)
		if ensureRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable ensure status %d, got %d body=%s", http.StatusNotFound, ensureRes.Code, ensureRes.Body.String())
		}

		setReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-targets/email", bytes.NewBufferString(`{"enabled":false}`))
		setRes := httptest.NewRecorder()
		h.SetMyEmailTarget(setRes, setReq)
		if setRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable set status %d, got %d body=%s", http.StatusNotFound, setRes.Code, setRes.Body.String())
		}
	})

	tests := []struct {
		name       string
		method     string
		serviceErr error
		wantStatus int
	}{
		{name: "get internal error", method: http.MethodGet, serviceErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
		{name: "ensure ownership conflict", method: http.MethodPost, serviceErr: repository.ErrNotificationTargetOwnershipConflict, wantStatus: http.StatusConflict},
		{name: "ensure invalid request", method: http.MethodPost, serviceErr: service.ErrNotificationPersonalEmailRequired, wantStatus: http.StatusBadRequest},
		{name: "set missing target", method: http.MethodPut, serviceErr: repository.ErrNotificationTargetNotFound, wantStatus: http.StatusNotFound},
		{name: "set invalid request", method: http.MethodPut, serviceErr: service.ErrNotificationTargetEnabledRequired, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(nil)
			h.SetAuthorization(auth.ModeHeader)
			fakeSvc := &fakeNotificationAdminService{}
			switch tc.method {
			case http.MethodGet:
				fakeSvc.getOwnedEmailTargetErr = tc.serviceErr
			case http.MethodPost:
				fakeSvc.ensureOwnedEmailTargetErr = tc.serviceErr
			default:
				fakeSvc.setOwnedEmailTargetErr = tc.serviceErr
			}
			h.SetAdminService(fakeSvc)
			user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

			var req *http.Request
			switch tc.method {
			case http.MethodPut:
				req = httptest.NewRequest(tc.method, "/api/me/notification-targets/email", bytes.NewBufferString(`{"enabled":true}`))
			default:
				req = httptest.NewRequest(tc.method, "/api/me/notification-targets/email", nil)
			}
			req = req.WithContext(auth.WithUser(req.Context(), user))
			res := httptest.NewRecorder()

			switch tc.method {
			case http.MethodGet:
				h.GetMyEmailTarget(res, req)
			case http.MethodPost:
				h.EnsureMyEmailTarget(res, req)
			default:
				h.SetMyEmailTarget(res, req)
			}

			if res.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, res.Code, res.Body.String())
			}
		})
	}
}

func TestNotificationHandler_SetMyEmailTarget(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		setOwnedEmailTargetResult: domain.NotificationTarget{
			ID:          "target-1",
			OwnerUserID: stringPtr("user-1"),
			Type:        domain.NotificationTargetTypeEmail,
			Name:        "User One",
			Recipient:   "<user@example.com>",
			Enabled:     false,
		},
	})
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	req := httptest.NewRequest(http.MethodPut, "/api/me/notification-targets/email", bytes.NewBufferString(`{"enabled":false}`))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	res := httptest.NewRecorder()

	h.SetMyEmailTarget(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected set my target status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	data := decodeDataMap(t, res)
	if data["enabled"] != false {
		t.Fatalf("expected disabled target payload, got %v", data["enabled"])
	}
}

func TestNotificationHandler_MyCommitAuthorFailurePreferenceEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	reason := service.NotificationPreferenceUnavailableReasonPersonalTargetRequired
	h.SetAdminService(&fakeNotificationAdminService{
		getPreferenceResult: service.CommitAuthorFailureNotificationPreferenceState{
			Email: service.CommitAuthorEmailNotificationPreferenceState{
				Enabled:           false,
				DeliveryActive:    false,
				UnavailableReason: &reason,
			},
			Slack: service.CommitAuthorSlackNotificationPreferenceState{
				Enabled:           false,
				DeliveryActive:    false,
				UnavailableReason: stringPtr(service.NotificationPreferenceUnavailableReasonSlackIdentityRequired),
			},
		},
		setPreferenceResult: service.CommitAuthorFailureNotificationPreferenceState{
			Email: service.CommitAuthorEmailNotificationPreferenceState{
				Enabled:        true,
				DeliveryActive: true,
				Target: &domain.NotificationTarget{
					ID:          "target-1",
					OwnerUserID: stringPtr("user-1"),
					Type:        domain.NotificationTargetTypeEmail,
					Name:        "User One",
					Recipient:   "<user@example.com>",
					Enabled:     true,
				},
			},
			Slack: service.CommitAuthorSlackNotificationPreferenceState{
				Enabled:        false,
				DeliveryActive: false,
			},
		},
	})
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-failures", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
	getRes := httptest.NewRecorder()
	h.GetMyCommitAuthorFailureNotificationPreference(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get preference status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	emailData, ok := getData["email"].(map[string]any)
	if !ok {
		t.Fatalf("expected email preference payload map, got %T", getData["email"])
	}
	slackData, ok := getData["slack"].(map[string]any)
	if !ok {
		t.Fatalf("expected slack preference payload map, got %T", getData["slack"])
	}
	if emailData["enabled"] != false || emailData["delivery_active"] != false {
		t.Fatalf("unexpected get preference payload: %+v", getData)
	}
	if emailData["unavailable_reason"] != reason {
		t.Fatalf("expected email unavailable reason %q, got %v", reason, emailData["unavailable_reason"])
	}
	if slackData["enabled"] != false || slackData["delivery_active"] != false {
		t.Fatalf("unexpected slack get preference payload: %+v", slackData)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-failures", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
	putReq = putReq.WithContext(auth.WithUser(putReq.Context(), user))
	putRes := httptest.NewRecorder()
	h.SetMyCommitAuthorFailureNotificationPreference(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("expected put preference status %d, got %d body=%s", http.StatusOK, putRes.Code, putRes.Body.String())
	}
	putData := decodeDataMap(t, putRes)
	putEmailData, ok := putData["email"].(map[string]any)
	if !ok {
		t.Fatalf("expected email put payload map, got %T", putData["email"])
	}
	putSlackData, ok := putData["slack"].(map[string]any)
	if !ok {
		t.Fatalf("expected slack put payload map, got %T", putData["slack"])
	}
	if putEmailData["enabled"] != true || putEmailData["delivery_active"] != true {
		t.Fatalf("unexpected put preference payload: %+v", putData)
	}
	if putSlackData["enabled"] != false || putSlackData["delivery_active"] != false {
		t.Fatalf("unexpected put slack preference payload: %+v", putData)
	}
}

func TestNotificationHandler_MyCommitAuthorFailurePreferenceEndpointsRequireAuthentication(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-failures", nil)
	getRes := httptest.NewRecorder()
	h.GetMyCommitAuthorFailureNotificationPreference(getRes, getReq)
	if getRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected get preference unauthorized status %d, got %d body=%s", http.StatusUnauthorized, getRes.Code, getRes.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-failures", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
	putRes := httptest.NewRecorder()
	h.SetMyCommitAuthorFailureNotificationPreference(putRes, putReq)
	if putRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected put preference unauthorized status %d, got %d body=%s", http.StatusUnauthorized, putRes.Code, putRes.Body.String())
	}
}

func TestNotificationHandler_MyCommitAuthorFailurePreferenceEndpointErrors(t *testing.T) {
	t.Run("endpoint unavailable", func(t *testing.T) {
		h := NewNotificationHandler(nil)

		getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-failures", nil)
		getRes := httptest.NewRecorder()
		h.GetMyCommitAuthorFailureNotificationPreference(getRes, getReq)
		if getRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable get status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
		}

		putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-failures", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
		putRes := httptest.NewRecorder()
		h.SetMyCommitAuthorFailureNotificationPreference(putRes, putReq)
		if putRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable put status %d, got %d body=%s", http.StatusNotFound, putRes.Code, putRes.Body.String())
		}
	})

	t.Run("invalid request body", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{})
		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

		putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-failures", bytes.NewBufferString(`{"enabled":`))
		putReq = putReq.WithContext(auth.WithUser(putReq.Context(), user))
		putRes := httptest.NewRecorder()
		h.SetMyCommitAuthorFailureNotificationPreference(putRes, putReq)
		if putRes.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid request status %d, got %d body=%s", http.StatusBadRequest, putRes.Code, putRes.Body.String())
		}
	})

	tests := []struct {
		name       string
		method     string
		serviceErr error
		wantStatus int
	}{
		{name: "get internal error", method: http.MethodGet, serviceErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
		{name: "put target required", method: http.MethodPut, serviceErr: service.ErrNotificationPreferencePersonalTargetRequired, wantStatus: http.StatusConflict},
		{name: "put invalid request", method: http.MethodPut, serviceErr: service.ErrNotificationPreferenceChannelEnabledRequired, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(nil)
			h.SetAuthorization(auth.ModeHeader)
			fakeSvc := &fakeNotificationAdminService{}
			if tc.method == http.MethodGet {
				fakeSvc.getPreferenceErr = tc.serviceErr
			} else {
				fakeSvc.setPreferenceErr = tc.serviceErr
			}
			h.SetAdminService(fakeSvc)
			user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

			req := httptest.NewRequest(tc.method, "/api/me/notification-preferences/commit-author-failures", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
			req = req.WithContext(auth.WithUser(req.Context(), user))
			res := httptest.NewRecorder()

			if tc.method == http.MethodGet {
				h.GetMyCommitAuthorFailureNotificationPreference(res, req)
			} else {
				h.SetMyCommitAuthorFailureNotificationPreference(res, req)
			}

			if res.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, res.Code, res.Body.String())
			}
		})
	}
}

func TestNotificationHandler_MyCommitAuthorSuccessPreferenceEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	reason := service.NotificationPreferenceUnavailableReasonPersonalTargetRequired
	h.SetAdminService(&fakeNotificationAdminService{
		getSuccessPreferenceResult: service.CommitAuthorSuccessNotificationPreferenceState{
			Email: service.CommitAuthorEmailNotificationPreferenceState{
				Enabled:           false,
				DeliveryActive:    false,
				UnavailableReason: &reason,
			},
			Slack: service.CommitAuthorSlackNotificationPreferenceState{
				Enabled:           false,
				DeliveryActive:    false,
				UnavailableReason: stringPtr(service.NotificationPreferenceUnavailableReasonSlackIdentityRequired),
			},
		},
		setSuccessPreferenceResult: service.CommitAuthorSuccessNotificationPreferenceState{
			Email: service.CommitAuthorEmailNotificationPreferenceState{
				Enabled:        true,
				DeliveryActive: true,
				Target: &domain.NotificationTarget{
					ID:          "target-1",
					OwnerUserID: stringPtr("user-1"),
					Type:        domain.NotificationTargetTypeEmail,
					Name:        "User One",
					Recipient:   "<user@example.com>",
					Enabled:     true,
				},
			},
			Slack: service.CommitAuthorSlackNotificationPreferenceState{
				Enabled:        false,
				DeliveryActive: false,
			},
		},
	})
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-successes", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
	getRes := httptest.NewRecorder()
	h.GetMyCommitAuthorSuccessNotificationPreference(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get preference status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	emailData, ok := getData["email"].(map[string]any)
	if !ok {
		t.Fatalf("expected email preference payload map, got %T", getData["email"])
	}
	slackData, ok := getData["slack"].(map[string]any)
	if !ok {
		t.Fatalf("expected slack preference payload map, got %T", getData["slack"])
	}
	if emailData["enabled"] != false || emailData["delivery_active"] != false {
		t.Fatalf("unexpected get preference payload: %+v", getData)
	}
	if emailData["unavailable_reason"] != reason {
		t.Fatalf("expected email unavailable reason %q, got %v", reason, emailData["unavailable_reason"])
	}
	if slackData["enabled"] != false || slackData["delivery_active"] != false {
		t.Fatalf("unexpected slack get preference payload: %+v", slackData)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
	putReq = putReq.WithContext(auth.WithUser(putReq.Context(), user))
	putRes := httptest.NewRecorder()
	h.SetMyCommitAuthorSuccessNotificationPreference(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("expected put preference status %d, got %d body=%s", http.StatusOK, putRes.Code, putRes.Body.String())
	}
	putData := decodeDataMap(t, putRes)
	putEmailData, ok := putData["email"].(map[string]any)
	if !ok {
		t.Fatalf("expected email put payload map, got %T", putData["email"])
	}
	putSlackData, ok := putData["slack"].(map[string]any)
	if !ok {
		t.Fatalf("expected slack put payload map, got %T", putData["slack"])
	}
	if putEmailData["enabled"] != true || putEmailData["delivery_active"] != true {
		t.Fatalf("unexpected put preference payload: %+v", putData)
	}
	if putSlackData["enabled"] != false || putSlackData["delivery_active"] != false {
		t.Fatalf("unexpected put slack preference payload: %+v", putData)
	}
}

func TestNotificationHandler_MyCommitAuthorSuccessPreferenceEndpointsRequireAuthentication(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-successes", nil)
	getRes := httptest.NewRecorder()
	h.GetMyCommitAuthorSuccessNotificationPreference(getRes, getReq)
	if getRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected get preference unauthorized status %d, got %d body=%s", http.StatusUnauthorized, getRes.Code, getRes.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
	putRes := httptest.NewRecorder()
	h.SetMyCommitAuthorSuccessNotificationPreference(putRes, putReq)
	if putRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected put preference unauthorized status %d, got %d body=%s", http.StatusUnauthorized, putRes.Code, putRes.Body.String())
	}
}

func TestNotificationHandler_MyCommitAuthorSuccessPreferenceEndpointErrors(t *testing.T) {
	t.Run("endpoint unavailable", func(t *testing.T) {
		h := NewNotificationHandler(nil)

		getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-preferences/commit-author-successes", nil)
		getRes := httptest.NewRecorder()
		h.GetMyCommitAuthorSuccessNotificationPreference(getRes, getReq)
		if getRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable get status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
		}

		putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
		putRes := httptest.NewRecorder()
		h.SetMyCommitAuthorSuccessNotificationPreference(putRes, putReq)
		if putRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable put status %d, got %d body=%s", http.StatusNotFound, putRes.Code, putRes.Body.String())
		}
	})

	t.Run("invalid request body", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{})
		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

		putReq := httptest.NewRequest(http.MethodPut, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"enabled":`))
		putReq = putReq.WithContext(auth.WithUser(putReq.Context(), user))
		putRes := httptest.NewRecorder()
		h.SetMyCommitAuthorSuccessNotificationPreference(putRes, putReq)
		if putRes.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid request status %d, got %d body=%s", http.StatusBadRequest, putRes.Code, putRes.Body.String())
		}
	})

	tests := []struct {
		name       string
		method     string
		serviceErr error
		wantStatus int
	}{
		{name: "get internal error", method: http.MethodGet, serviceErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
		{name: "put target required", method: http.MethodPut, serviceErr: service.ErrNotificationPreferencePersonalTargetRequired, wantStatus: http.StatusConflict},
		{name: "put invalid request", method: http.MethodPut, serviceErr: service.ErrNotificationPreferenceChannelEnabledRequired, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(nil)
			h.SetAuthorization(auth.ModeHeader)
			fakeSvc := &fakeNotificationAdminService{}
			if tc.method == http.MethodGet {
				fakeSvc.getSuccessPreferenceErr = tc.serviceErr
			} else {
				fakeSvc.setSuccessPreferenceErr = tc.serviceErr
			}
			h.SetAdminService(fakeSvc)
			user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

			req := httptest.NewRequest(tc.method, "/api/me/notification-preferences/commit-author-successes", bytes.NewBufferString(`{"email_enabled":true,"slack_enabled":false}`))
			req = req.WithContext(auth.WithUser(req.Context(), user))
			res := httptest.NewRecorder()

			if tc.method == http.MethodGet {
				h.GetMyCommitAuthorSuccessNotificationPreference(res, req)
			} else {
				h.SetMyCommitAuthorSuccessNotificationPreference(res, req)
			}

			if res.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, res.Code, res.Body.String())
			}
		})
	}
}

func TestNotificationHandler_AdminEndpoints(t *testing.T) {
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	otherJobID := uuid.NewString()
	otherProjectID := uuid.NewString()

	repo := memoryrepo.NewNotificationSubscriptionRepository()
	notificationService := service.NewNotificationService(repo)
	h := NewNotificationHandler(nil)
	h.SetAdminService(notificationService)
	h.SetAuthorization(auth.ModeHeader)
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	listReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), admin))
	listRes := httptest.NewRecorder()
	h.ListTargets(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected empty list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	if targets, ok := listData["targets"].([]any); !ok || len(targets) != 0 {
		t.Fatalf("expected empty targets list, got %v", listData["targets"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Build Alerts","address":"dev@example.com"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), admin))
	createRes := httptest.NewRecorder()
	h.CreateEmailTarget(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create target status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	targetData := decodeDataMap(t, createRes)
	targetID, _ := targetData["id"].(string)
	if targetID == "" {
		t.Fatalf("expected target id, got %v", targetData)
	}

	invalidTargetReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Bad","address":"nope"}`))
	invalidTargetReq = invalidTargetReq.WithContext(auth.WithUser(invalidTargetReq.Context(), admin))
	invalidTargetRes := httptest.NewRecorder()
	h.CreateEmailTarget(invalidTargetRes, invalidTargetReq)
	if invalidTargetRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid target status %d, got %d body=%s", http.StatusBadRequest, invalidTargetRes.Code, invalidTargetRes.Body.String())
	}

	disableBody := `{"enabled":false}`
	updateTargetReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/"+targetID, bytes.NewBufferString(disableBody)), "targetID", targetID)
	updateTargetReq = updateTargetReq.WithContext(auth.WithUser(updateTargetReq.Context(), admin))
	updateTargetRes := httptest.NewRecorder()
	h.UpdateTarget(updateTargetRes, updateTargetReq)
	if updateTargetRes.Code != http.StatusOK {
		t.Fatalf("expected update target status %d, got %d body=%s", http.StatusOK, updateTargetRes.Code, updateTargetRes.Body.String())
	}
	updatedTargetData := decodeDataMap(t, updateTargetRes)
	if enabled, ok := updatedTargetData["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected disabled target, got %v", updatedTargetData["enabled"])
	}

	enableBody := `{"enabled":true}`
	enableTargetReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/"+targetID, bytes.NewBufferString(enableBody)), "targetID", targetID)
	enableTargetReq = enableTargetReq.WithContext(auth.WithUser(enableTargetReq.Context(), admin))
	enableTargetRes := httptest.NewRecorder()
	h.UpdateTarget(enableTargetRes, enableTargetReq)
	if enableTargetRes.Code != http.StatusOK {
		t.Fatalf("expected re-enable target status %d, got %d body=%s", http.StatusOK, enableTargetRes.Code, enableTargetRes.Body.String())
	}

	projectReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+projectID+`","event_type":"build_failed"}`))
	projectReq = projectReq.WithContext(auth.WithUser(projectReq.Context(), admin))
	projectRes := httptest.NewRecorder()
	h.CreateSubscription(projectRes, projectReq)
	if projectRes.Code != http.StatusCreated {
		t.Fatalf("expected project subscription status %d, got %d body=%s", http.StatusCreated, projectRes.Code, projectRes.Body.String())
	}
	projectData := decodeDataMap(t, projectRes)
	projectSubID, _ := projectData["id"].(string)

	jobReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","job_id":"`+jobID+`","event_type":"build_succeeded"}`))
	jobReq = jobReq.WithContext(auth.WithUser(jobReq.Context(), admin))
	jobRes := httptest.NewRecorder()
	h.CreateSubscription(jobRes, jobReq)
	if jobRes.Code != http.StatusCreated {
		t.Fatalf("expected job subscription status %d, got %d body=%s", http.StatusCreated, jobRes.Code, jobRes.Body.String())
	}
	jobData := decodeDataMap(t, jobRes)
	jobSubID, _ := jobData["id"].(string)
	if jobSubID == "" {
		t.Fatalf("expected job subscription id, got %v", jobData)
	}

	bothReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+otherProjectID+`","job_id":"`+otherJobID+`","event_type":"build_failed"}`))
	bothReq = bothReq.WithContext(auth.WithUser(bothReq.Context(), admin))
	bothRes := httptest.NewRecorder()
	h.CreateSubscription(bothRes, bothReq)
	if bothRes.Code != http.StatusBadRequest {
		t.Fatalf("expected both-scope status %d, got %d body=%s", http.StatusBadRequest, bothRes.Code, bothRes.Body.String())
	}

	neitherReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","event_type":"build_failed"}`))
	neitherReq = neitherReq.WithContext(auth.WithUser(neitherReq.Context(), admin))
	neitherRes := httptest.NewRecorder()
	h.CreateSubscription(neitherRes, neitherReq)
	if neitherRes.Code != http.StatusBadRequest {
		t.Fatalf("expected neither-scope status %d, got %d body=%s", http.StatusBadRequest, neitherRes.Code, neitherRes.Body.String())
	}

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+projectID+`","event_type":"build_failed"}`))
	duplicateReq = duplicateReq.WithContext(auth.WithUser(duplicateReq.Context(), admin))
	duplicateRes := httptest.NewRecorder()
	h.CreateSubscription(duplicateRes, duplicateReq)
	if duplicateRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate subscription status %d, got %d body=%s", http.StatusConflict, duplicateRes.Code, duplicateRes.Body.String())
	}

	projectListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id="+projectID, nil)
	projectListReq = projectListReq.WithContext(auth.WithUser(projectListReq.Context(), admin))
	projectListRes := httptest.NewRecorder()
	h.ListSubscriptions(projectListRes, projectListReq)
	if projectListRes.Code != http.StatusOK {
		t.Fatalf("expected project filter status %d, got %d body=%s", http.StatusOK, projectListRes.Code, projectListRes.Body.String())
	}
	projectListData := decodeDataMap(t, projectListRes)
	projectSubs, ok := projectListData["subscriptions"].([]any)
	if !ok || len(projectSubs) != 1 {
		t.Fatalf("expected one project subscription, got %v", projectListData["subscriptions"])
	}

	jobListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?job_id="+jobID, nil)
	jobListReq = jobListReq.WithContext(auth.WithUser(jobListReq.Context(), admin))
	jobListRes := httptest.NewRecorder()
	h.ListSubscriptions(jobListRes, jobListReq)
	if jobListRes.Code != http.StatusOK {
		t.Fatalf("expected job filter status %d, got %d body=%s", http.StatusOK, jobListRes.Code, jobListRes.Body.String())
	}
	jobListData := decodeDataMap(t, jobListRes)
	jobSubs, ok := jobListData["subscriptions"].([]any)
	if !ok || len(jobSubs) != 1 {
		t.Fatalf("expected one job subscription, got %v", jobListData["subscriptions"])
	}

	updateSubReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-subscriptions/"+projectSubID, bytes.NewBufferString(`{"enabled":false}`)), "subscriptionID", projectSubID)
	updateSubReq = updateSubReq.WithContext(auth.WithUser(updateSubReq.Context(), admin))
	updateSubRes := httptest.NewRecorder()
	h.UpdateSubscription(updateSubRes, updateSubReq)
	if updateSubRes.Code != http.StatusOK {
		t.Fatalf("expected update subscription status %d, got %d body=%s", http.StatusOK, updateSubRes.Code, updateSubRes.Body.String())
	}
	updatedSubData := decodeDataMap(t, updateSubRes)
	if enabled, ok := updatedSubData["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected disabled subscription, got %v", updatedSubData["enabled"])
	}

	deleteSubReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/"+jobSubID, nil), "subscriptionID", jobSubID)
	deleteSubReq = deleteSubReq.WithContext(auth.WithUser(deleteSubReq.Context(), admin))
	deleteSubRes := httptest.NewRecorder()
	h.DeleteSubscription(deleteSubRes, deleteSubReq)
	if deleteSubRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete subscription status %d, got %d body=%s", http.StatusNoContent, deleteSubRes.Code, deleteSubRes.Body.String())
	}

	invalidProjectReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"not-a-uuid","event_type":"build_failed"}`))
	invalidProjectReq = invalidProjectReq.WithContext(auth.WithUser(invalidProjectReq.Context(), admin))
	invalidProjectRes := httptest.NewRecorder()
	h.CreateSubscription(invalidProjectRes, invalidProjectReq)
	if invalidProjectRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid project id status %d, got %d body=%s", http.StatusBadRequest, invalidProjectRes.Code, invalidProjectRes.Body.String())
	}

	invalidListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id=not-a-uuid", nil)
	invalidListReq = invalidListReq.WithContext(auth.WithUser(invalidListReq.Context(), admin))
	invalidListRes := httptest.NewRecorder()
	h.ListSubscriptions(invalidListRes, invalidListReq)
	if invalidListRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid list filter status %d, got %d body=%s", http.StatusBadRequest, invalidListRes.Code, invalidListRes.Body.String())
	}
}

func TestNotificationHandler_NotificationDefaultsEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		getNotificationDefaultsResult: service.NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: true, DefaultCommitAuthorSuccessEmailEnabled: false},
		setNotificationDefaultsResult: service.NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: false, DefaultCommitAuthorSuccessEmailEnabled: true},
	})
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/notifications/defaults", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), admin))
	getRes := httptest.NewRecorder()
	h.GetNotificationDefaults(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get defaults status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	if enabled, ok := getData["default_commit_author_failure_email_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected enabled defaults payload, got %v", getData)
	}
	if enabled, ok := getData["default_commit_author_success_email_enabled"].(bool); !ok || enabled {
		t.Fatalf("expected success defaults to start disabled, got %v", getData)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", bytes.NewBufferString(`{"default_commit_author_failure_email_enabled":false,"default_commit_author_success_email_enabled":true}`))
	putReq = putReq.WithContext(auth.WithUser(putReq.Context(), admin))
	putRes := httptest.NewRecorder()
	h.SetNotificationDefaults(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("expected put defaults status %d, got %d body=%s", http.StatusOK, putRes.Code, putRes.Body.String())
	}
	putData := decodeDataMap(t, putRes)
	if enabled, ok := putData["default_commit_author_failure_email_enabled"].(bool); !ok || enabled {
		t.Fatalf("expected disabled defaults payload, got %v", putData)
	}
	if enabled, ok := putData["default_commit_author_success_email_enabled"].(bool); !ok || !enabled {
		t.Fatalf("expected success defaults payload to be enabled, got %v", putData)
	}

	invalidReq := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", bytes.NewBufferString(`{}`))
	invalidReq = invalidReq.WithContext(auth.WithUser(invalidReq.Context(), admin))
	invalidRes := httptest.NewRecorder()
	h.SetNotificationDefaults(invalidRes, invalidReq)
	if invalidRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid defaults status %d, got %d body=%s", http.StatusBadRequest, invalidRes.Code, invalidRes.Body.String())
	}
}

func TestNotificationHandler_NotificationDefaultsEndpointErrors(t *testing.T) {
	t.Run("admin endpoints unavailable", func(t *testing.T) {
		h := NewNotificationHandler(nil)

		getReq := httptest.NewRequest(http.MethodGet, "/api/settings/notifications/defaults", nil)
		getRes := httptest.NewRecorder()
		h.GetNotificationDefaults(getRes, getReq)
		if getRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable get defaults status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
		}

		putReq := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", nil)
		putRes := httptest.NewRecorder()
		h.SetNotificationDefaults(putRes, putReq)
		if putRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable put defaults status %d, got %d body=%s", http.StatusNotFound, putRes.Code, putRes.Body.String())
		}
	})

	t.Run("non admin forbidden", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{})
		user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

		getReq := httptest.NewRequest(http.MethodGet, "/api/settings/notifications/defaults", nil)
		getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
		getRes := httptest.NewRecorder()
		h.GetNotificationDefaults(getRes, getReq)
		if getRes.Code != http.StatusForbidden {
			t.Fatalf("expected forbidden get defaults status %d, got %d body=%s", http.StatusForbidden, getRes.Code, getRes.Body.String())
		}
	})

	t.Run("current defaults read failure blocks update", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{getNotificationDefaultsErr: errors.New("boom")})
		admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

		req := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", bytes.NewBufferString(`{"default_commit_author_failure_email_enabled":true,"default_commit_author_success_email_enabled":false}`))
		req = req.WithContext(auth.WithUser(req.Context(), admin))
		res := httptest.NewRecorder()
		h.SetNotificationDefaults(res, req)
		if res.Code != http.StatusInternalServerError {
			t.Fatalf("expected current defaults failure status %d, got %d body=%s", http.StatusInternalServerError, res.Code, res.Body.String())
		}
	})

	t.Run("get defaults service error", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{getNotificationDefaultsErr: errors.New("boom")})
		admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

		req := httptest.NewRequest(http.MethodGet, "/api/settings/notifications/defaults", nil)
		req = req.WithContext(auth.WithUser(req.Context(), admin))
		res := httptest.NewRecorder()
		h.GetNotificationDefaults(res, req)
		if res.Code != http.StatusInternalServerError {
			t.Fatalf("expected get defaults error status %d, got %d body=%s", http.StatusInternalServerError, res.Code, res.Body.String())
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{getNotificationDefaultsResult: service.NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: true, DefaultCommitAuthorSuccessEmailEnabled: false}})
		admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

		req := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", bytes.NewBufferString(`{"default_commit_author_failure_email_enabled":`))
		req = req.WithContext(auth.WithUser(req.Context(), admin))
		res := httptest.NewRecorder()
		h.SetNotificationDefaults(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid json status %d, got %d body=%s", http.StatusBadRequest, res.Code, res.Body.String())
		}
	})

	t.Run("set defaults service error", func(t *testing.T) {
		h := NewNotificationHandler(nil)
		h.SetAuthorization(auth.ModeHeader)
		h.SetAdminService(&fakeNotificationAdminService{
			getNotificationDefaultsResult: service.NotificationDefaultsState{DefaultCommitAuthorFailureEmailEnabled: true, DefaultCommitAuthorSuccessEmailEnabled: false},
			setNotificationDefaultsErr:    service.ErrNotificationDefaultEnabledRequired,
		})
		admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

		req := httptest.NewRequest(http.MethodPut, "/api/settings/notifications/defaults", bytes.NewBufferString(`{"default_commit_author_failure_email_enabled":true,"default_commit_author_success_email_enabled":false}`))
		req = req.WithContext(auth.WithUser(req.Context(), admin))
		res := httptest.NewRecorder()
		h.SetNotificationDefaults(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("expected service set defaults error status %d, got %d body=%s", http.StatusBadRequest, res.Code, res.Body.String())
		}
	})
}

func TestNotificationHandler_TargetResponsesMaskSlackWebhookSecrets(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		listTargetsResult: []domain.NotificationTarget{
			{ID: "target-email", Type: domain.NotificationTargetTypeEmail, Name: "Email", Recipient: "<dev@example.com>", Enabled: true},
			{ID: "target-slack", Type: domain.NotificationTargetTypeSlackWebhook, Name: "Slack", Recipient: "https://hooks.slack.example/services/T/B/X", Enabled: true},
		},
	})
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	req := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	res := httptest.NewRecorder()
	h.ListTargets(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	data := decodeDataMap(t, res)
	targets, ok := data["targets"].([]any)
	if !ok || len(targets) != 2 {
		t.Fatalf("expected two targets, got %v", data["targets"])
	}
	slackTarget, ok := targets[1].(map[string]any)
	if !ok {
		t.Fatalf("expected slack target map, got %T", targets[1])
	}
	if _, exists := slackTarget["address"]; exists {
		t.Fatalf("expected slack target secret to be omitted, got %v", slackTarget)
	}
	configured, ok := slackTarget["webhook_configured"].(bool)
	if !ok || !configured {
		t.Fatalf("expected webhook_configured=true, got %v", slackTarget["webhook_configured"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"type":"slack_webhook","name":"Slack","webhook_url":"https://hooks.slack.example/services/T/B/X"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), admin))
	createRes := httptest.NewRecorder()
	h.SetAdminService(&fakeNotificationAdminService{
		createTargetResult: domain.NotificationTarget{ID: "target-slack", Type: domain.NotificationTargetTypeSlackWebhook, Name: "Slack", Recipient: "https://hooks.slack.example/services/T/B/X", Enabled: true},
	})
	h.CreateTarget(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create target status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	created := decodeDataMap(t, createRes)
	if _, exists := created["address"]; exists {
		t.Fatalf("expected created slack target to omit address, got %v", created)
	}
}

func TestNotificationHandler_AdminAuthorizationAndErrors(t *testing.T) {
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	nonAdmin := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	missingHandler := NewNotificationHandler(nil)
	missingReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	missingRes := httptest.NewRecorder()
	missingHandler.ListTargets(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing admin service status %d, got %d body=%s", http.StatusNotFound, missingRes.Code, missingRes.Body.String())
	}

	authHandler := NewNotificationHandler(nil)
	authHandler.SetAdminService(&fakeNotificationAdminService{})
	authHandler.SetAuthorization(auth.ModeHeader)

	unauthReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	unauthRes := httptest.NewRecorder()
	authHandler.ListTargets(unauthRes, unauthReq)
	if unauthRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status %d, got %d body=%s", http.StatusUnauthorized, unauthRes.Code, unauthRes.Body.String())
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	forbiddenReq = forbiddenReq.WithContext(auth.WithUser(forbiddenReq.Context(), nonAdmin))
	forbiddenRes := httptest.NewRecorder()
	authHandler.ListTargets(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status %d, got %d body=%s", http.StatusForbidden, forbiddenRes.Code, forbiddenRes.Body.String())
	}

	invalidJSONReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString("{"))
	invalidJSONReq = invalidJSONReq.WithContext(auth.WithUser(invalidJSONReq.Context(), admin))
	invalidJSONRes := httptest.NewRecorder()
	authHandler.CreateEmailTarget(invalidJSONRes, invalidJSONReq)
	if invalidJSONRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid JSON status %d, got %d body=%s", http.StatusBadRequest, invalidJSONRes.Code, invalidJSONRes.Body.String())
	}

	errorCases := []struct {
		name       string
		call       func(*NotificationHandler, *httptest.ResponseRecorder, *http.Request)
		err        error
		statusCode int
	}{
		{name: "list targets internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListTargets(res, req)
		}},
		{name: "create target duplicate", err: repository.ErrNotificationTargetDuplicate, statusCode: http.StatusConflict, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.CreateEmailTarget(res, req)
		}},
		{name: "update target not found", err: repository.ErrNotificationTargetNotFound, statusCode: http.StatusNotFound, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateTarget(res, req)
		}},
		{name: "list subscriptions internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListSubscriptions(res, req)
		}},
		{name: "create subscription invalid", err: service.ErrNotificationSubscriptionEventTypeInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.CreateSubscription(res, req)
		}},
		{name: "update target invalid id", err: service.ErrNotificationTargetIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateTarget(res, req)
		}},
		{name: "list subscriptions invalid filter", err: service.ErrNotificationSubscriptionProjectIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListSubscriptions(res, req)
		}},
		{name: "update subscription not found", err: repository.ErrNotificationSubscriptionNotFound, statusCode: http.StatusNotFound, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateSubscription(res, req)
		}},
		{name: "delete subscription invalid id", err: service.ErrNotificationSubscriptionIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.DeleteSubscription(res, req)
		}},
		{name: "delete subscription internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.DeleteSubscription(res, req)
		}},
	}

	for _, testCase := range errorCases {
		t.Run(testCase.name, func(t *testing.T) {
			adminService := &fakeNotificationAdminService{}
			switch testCase.name {
			case "list targets internal":
				adminService.listTargetsErr = testCase.err
			case "create target duplicate":
				adminService.createTargetErr = testCase.err
			case "update target not found":
				adminService.updateTargetErr = testCase.err
			case "list subscriptions internal":
				adminService.listSubscriptionsErr = testCase.err
			case "create subscription invalid":
				adminService.createSubscriptionErr = testCase.err
			case "update target invalid id":
				adminService.updateTargetErr = testCase.err
			case "list subscriptions invalid filter":
				adminService.listSubscriptionsErr = testCase.err
			case "update subscription not found":
				adminService.updateSubscriptionErr = testCase.err
			case "delete subscription invalid id":
				adminService.deleteSubscriptionErr = testCase.err
			case "delete subscription internal":
				adminService.deleteSubscriptionErr = testCase.err
			}

			h := NewNotificationHandler(nil)
			h.SetAdminService(adminService)
			h.SetAuthorization(auth.ModeHeader)

			var req *http.Request
			switch testCase.name {
			case "list targets internal":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
			case "create target duplicate":
				req = httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Alerts","address":"dev@example.com"}`))
			case "update target not found":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/missing", bytes.NewBufferString(`{"enabled":true}`)), "targetID", "missing")
			case "list subscriptions internal":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions", nil)
			case "create subscription invalid":
				req = httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"target-1","project_id":"project-1","event_type":"invalid"}`))
			case "update target invalid id":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/not-a-uuid", bytes.NewBufferString(`{"enabled":true}`)), "targetID", "not-a-uuid")
			case "list subscriptions invalid filter":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id=not-a-uuid", nil)
			case "update subscription not found":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-subscriptions/missing", bytes.NewBufferString(`{"enabled":true}`)), "subscriptionID", "missing")
			case "delete subscription invalid id":
				req = addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/not-a-uuid", nil), "subscriptionID", "not-a-uuid")
			case "delete subscription internal":
				req = addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/missing", nil), "subscriptionID", "missing")
			}
			req = req.WithContext(auth.WithUser(req.Context(), admin))
			res := httptest.NewRecorder()
			testCase.call(h, res, req)
			if res.Code != testCase.statusCode {
				t.Fatalf("expected status %d, got %d body=%s", testCase.statusCode, res.Code, res.Body.String())
			}
		})
	}
}
