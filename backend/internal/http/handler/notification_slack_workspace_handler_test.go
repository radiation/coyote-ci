package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type fakeSlackWorkspaceAdminService struct {
	integration domain.SlackWorkspaceIntegration
	has         bool
	getErr      error
	connectErr  error
	setErr      error
	testErr     error
	deleteErr   error
}

func (f *fakeSlackWorkspaceAdminService) Get(_ context.Context) (domain.SlackWorkspaceIntegration, error) {
	if f.getErr != nil {
		return domain.SlackWorkspaceIntegration{}, f.getErr
	}
	if !f.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	return f.integration, nil
}

func (f *fakeSlackWorkspaceAdminService) Connect(_ context.Context, input service.ConnectSlackWorkspaceIntegrationInput) (domain.SlackWorkspaceIntegration, error) {
	if f.connectErr != nil {
		return domain.SlackWorkspaceIntegration{}, f.connectErr
	}
	if input.BotToken == "" {
		return domain.SlackWorkspaceIntegration{}, service.ErrSlackWorkspaceBotTokenRequired
	}
	now := time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)
	f.integration = domain.SlackWorkspaceIntegration{
		ID:             "int-1",
		WorkspaceID:    "T123",
		WorkspaceName:  strPtrHandler("Coyote"),
		BotID:          strPtrHandler("B123"),
		BotTokenSecret: input.BotToken,
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.has = true
	return f.integration, nil
}

func (f *fakeSlackWorkspaceAdminService) SetEnabled(_ context.Context, enabled *bool) (domain.SlackWorkspaceIntegration, error) {
	if f.setErr != nil {
		return domain.SlackWorkspaceIntegration{}, f.setErr
	}
	if !f.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	if enabled == nil {
		return domain.SlackWorkspaceIntegration{}, service.ErrSlackWorkspaceEnabledRequired
	}
	f.integration.Enabled = *enabled
	return f.integration, nil
}

func (f *fakeSlackWorkspaceAdminService) TestConnection(_ context.Context) (domain.SlackWorkspaceIntegration, error) {
	if f.testErr != nil {
		return domain.SlackWorkspaceIntegration{}, f.testErr
	}
	if !f.has {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
	}
	success := true
	now := time.Date(2026, 7, 1, 13, 1, 0, 0, time.UTC)
	f.integration.LastTestedAt = &now
	f.integration.LastTestSucceeded = &success
	f.integration.UpdatedAt = now
	return f.integration, nil
}

func (f *fakeSlackWorkspaceAdminService) Disconnect(_ context.Context) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if !f.has {
		return repository.ErrSlackWorkspaceIntegrationNotFound
	}
	f.has = false
	return nil
}

func TestNotificationHandler_SlackWorkspaceIntegration_AdminAndAuthorization(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetSlackWorkspaceIntegrationService(&fakeSlackWorkspaceAdminService{})

	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	member := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/settings/integrations/slack", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), admin))
	getRes := httptest.NewRecorder()
	h.GetSlackWorkspaceIntegration(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, getRes.Code)
	}
	getData := decodeDataMap(t, getRes)
	if getData["configured"] != false {
		t.Fatalf("expected configured false, got %v", getData["configured"])
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/settings/integrations/slack", nil)
	forbiddenReq = forbiddenReq.WithContext(auth.WithUser(forbiddenReq.Context(), member))
	forbiddenRes := httptest.NewRecorder()
	h.GetSlackWorkspaceIntegration(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status %d, got %d", http.StatusForbidden, forbiddenRes.Code)
	}
}

func TestNotificationHandler_SlackWorkspaceIntegration_ConnectAndNoTokenInResponse(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetSlackWorkspaceIntegrationService(&fakeSlackWorkspaceAdminService{})

	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	req := httptest.NewRequest(http.MethodPut, "/settings/integrations/slack", bytes.NewBufferString(`{"bot_token":"xoxb-secret"}`))
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	res := httptest.NewRecorder()

	h.PutSlackWorkspaceIntegration(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	data := decodeDataMap(t, res)
	if data["configured"] != true {
		t.Fatalf("expected configured true, got %v", data["configured"])
	}
	integration, ok := data["integration"].(map[string]any)
	if !ok {
		t.Fatalf("expected integration payload, got %T", data["integration"])
	}
	if _, has := integration["bot_token"]; has {
		t.Fatalf("did not expect bot token field in response")
	}
	if got := integration["bot_id"]; got != "B123" {
		t.Fatalf("expected bot_id B123, got %v", got)
	}
	if _, has := integration["bot_user_id"]; has {
		t.Fatalf("did not expect legacy bot_user_id field in response")
	}
}

func TestNotificationHandler_SlackWorkspaceIntegration_ServiceUnavailableAndInvalidBodies(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	getReq := httptest.NewRequest(http.MethodGet, "/settings/integrations/slack", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), admin))
	getRes := httptest.NewRecorder()
	h.GetSlackWorkspaceIntegration(getRes, getReq)
	if getRes.Code != http.StatusNotFound {
		t.Fatalf("expected not found when service unavailable, got %d", getRes.Code)
	}

	h.SetSlackWorkspaceIntegrationService(&fakeSlackWorkspaceAdminService{})
	putReq := httptest.NewRequest(http.MethodPut, "/settings/integrations/slack", bytes.NewBufferString("{"))
	putReq = putReq.WithContext(auth.WithUser(putReq.Context(), admin))
	putRes := httptest.NewRecorder()
	h.PutSlackWorkspaceIntegration(putRes, putReq)
	if putRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid put body, got %d", putRes.Code)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/settings/integrations/slack", bytes.NewBufferString("{"))
	patchReq = patchReq.WithContext(auth.WithUser(patchReq.Context(), admin))
	patchRes := httptest.NewRecorder()
	h.PatchSlackWorkspaceIntegration(patchRes, patchReq)
	if patchRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid patch body, got %d", patchRes.Code)
	}
}

func TestNotificationHandler_SlackWorkspaceIntegration_PatchTestAndDelete(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeOIDC)
	h.SetSlackWorkspaceIntegrationService(&fakeSlackWorkspaceAdminService{
		has: true,
		integration: domain.SlackWorkspaceIntegration{
			ID:          "int-1",
			WorkspaceID: "T123",
			Enabled:     true,
			ConnectedAt: time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
			CreatedAt:   time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
		},
	})

	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	patchReq := httptest.NewRequest(http.MethodPatch, "/settings/integrations/slack", bytes.NewBufferString(`{"enabled":false}`))
	patchReq = patchReq.WithContext(auth.WithUser(patchReq.Context(), admin))
	patchRes := httptest.NewRecorder()
	h.PatchSlackWorkspaceIntegration(patchRes, patchReq)
	if patchRes.Code != http.StatusOK {
		t.Fatalf("expected patch status 200, got %d", patchRes.Code)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/settings/integrations/slack/test", nil)
	testReq = testReq.WithContext(auth.WithUser(testReq.Context(), admin))
	testRes := httptest.NewRecorder()
	h.TestSlackWorkspaceIntegration(testRes, testReq)
	if testRes.Code != http.StatusOK {
		t.Fatalf("expected test status 200, got %d", testRes.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/settings/integrations/slack", nil)
	deleteReq = deleteReq.WithContext(auth.WithUser(deleteReq.Context(), admin))
	deleteRes := httptest.NewRecorder()
	h.DeleteSlackWorkspaceIntegration(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete status 204, got %d", deleteRes.Code)
	}
}

func TestNotificationHandler_SlackWorkspaceIntegration_ErrorMappings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "not found", err: repository.ErrSlackWorkspaceIntegrationNotFound, code: http.StatusNotFound},
		{name: "conflict", err: service.ErrSlackWorkspaceReplaceRequired, code: http.StatusConflict},
		{name: "invalid", err: service.ErrSlackWorkspaceTokenRevoked, code: http.StatusBadRequest},
		{name: "rate limited", err: service.ErrSlackWorkspaceRateLimited, code: http.StatusTooManyRequests},
		{name: "upstream", err: service.ErrSlackWorkspaceUpstream, code: http.StatusBadGateway},
		{name: "deadline", err: context.DeadlineExceeded, code: http.StatusBadGateway},
		{name: "canceled", err: context.Canceled, code: http.StatusBadGateway},
		{name: "internal", err: errors.New("boom"), code: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(nil)
			res := httptest.NewRecorder()
			h.writeSlackWorkspaceIntegrationError(res, tc.err)
			if res.Code != tc.code {
				t.Fatalf("expected status %d, got %d", tc.code, res.Code)
			}
		})
	}
}

func strPtrHandler(value string) *string {
	v := value
	return &v
}
