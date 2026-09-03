package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestWorkspaceHelperHandlerExchangeCapability(t *testing.T) {
	exchanger := &workspaceHelperExchangerStub{token: "capability-token", capability: domain.WorkspaceHelperCapability{ExecutionJobID: "job-1", PodUID: "pod-1", Role: domain.WorkspaceHelperRolePrepare, ExpiresAt: time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)}}
	handler := NewWorkspaceHelperHandler(exchanger)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1","role":"prepare"}`))
	request.Header.Set("Authorization", "Bearer projected-token")
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusCreated || exchanger.projectedToken != "projected-token" {
		t.Fatalf("status=%d token=%q", response.Code, exchanger.projectedToken)
	}
	var body struct {
		Data struct {
			Capability string `json:"capability"`
		} `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&body); decodeErr != nil || body.Data.Capability != "capability-token" {
		t.Fatalf("body=%q err=%v", response.Body.String(), decodeErr)
	}
}

func TestWorkspaceHelperHandlerRejectsUnauthorizedWithoutTokenLeakage(t *testing.T) {
	rawToken := "projected-token-secret"
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{err: service.ErrWorkspaceHelperUnauthorized})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1","role":"prepare"}`))
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), rawToken) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWorkspaceHelperHandlerRejectsMissingIdentity(t *testing.T) {
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWorkspaceHelperHandlerRejectsInvalidRequestBody(t *testing.T) {
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer projected-token")
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWorkspaceHelperHandlerHandlesUnavailableAndOperationalFailures(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer projected-token")

	unavailableResponse := httptest.NewRecorder()
	NewWorkspaceHelperHandler(nil).ExchangeCapability(unavailableResponse, request)
	if unavailableResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", unavailableResponse.Code)
	}

	internalResponse := httptest.NewRecorder()
	NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{err: errors.New("upstream failure")}).ExchangeCapability(internalResponse, request)
	if internalResponse.Code != http.StatusInternalServerError {
		t.Fatalf("internal status=%d", internalResponse.Code)
	}
}

type workspaceHelperExchangerStub struct {
	token          string
	capability     domain.WorkspaceHelperCapability
	err            error
	projectedToken string
}

func (s *workspaceHelperExchangerStub) Exchange(_ context.Context, projectedToken string, _ domain.WorkspaceHelperCapability) (string, domain.WorkspaceHelperCapability, error) {
	s.projectedToken = projectedToken
	return s.token, s.capability, s.err
}

var _ workspaceHelperCapabilityExchanger = (*workspaceHelperExchangerStub)(nil)
