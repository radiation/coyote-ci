package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type slackWorkspaceIntegrationAdminService interface {
	Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error)
	Connect(ctx context.Context, input service.ConnectSlackWorkspaceIntegrationInput) (domain.SlackWorkspaceIntegration, error)
	SetEnabled(ctx context.Context, enabled *bool) (domain.SlackWorkspaceIntegration, error)
	TestConnection(ctx context.Context) (domain.SlackWorkspaceIntegration, error)
	Disconnect(ctx context.Context) error
}

// GetSlackWorkspaceIntegration godoc
// @Summary Get Slack workspace integration
// @Description Returns safe Slack workspace integration metadata for the instance.
// @Tags notifications
// @Produce json
// @Success 200 {object} api.SlackWorkspaceIntegrationStatusEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/integrations/slack [get]
func (h *NotificationHandler) GetSlackWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSlackAdmin(w, r) {
		return
	}

	integration, err := h.slackAdmin.Get(r.Context())
	if err != nil {
		if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			writeDataJSON(w, http.StatusOK, api.SlackWorkspaceIntegrationStatusResponse{Configured: false})
			return
		}
		h.writeSlackWorkspaceIntegrationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSlackWorkspaceIntegrationStatusResponse(&integration))
}

// PutSlackWorkspaceIntegration godoc
// @Summary Connect or replace Slack workspace integration
// @Description Validates a Slack bot token via auth.test and stores safe workspace metadata plus credentials for the instance.
// @Tags notifications
// @Accept json
// @Produce json
// @Param request body api.PutSlackWorkspaceIntegrationRequest true "Slack workspace integration request"
// @Success 200 {object} api.SlackWorkspaceIntegrationStatusEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/integrations/slack [put]
func (h *NotificationHandler) PutSlackWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSlackAdmin(w, r) {
		return
	}
	actor, _ := h.currentRequestUser(r)

	var req api.PutSlackWorkspaceIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	replace := false
	if req.ReplaceExisting != nil {
		replace = *req.ReplaceExisting
	}
	integration, err := h.slackAdmin.Connect(r.Context(), service.ConnectSlackWorkspaceIntegrationInput{
		BotToken:        req.BotToken,
		ReplaceExisting: replace,
	})
	if err != nil {
		h.writeSlackWorkspaceIntegrationError(w, err)
		return
	}

	log.Printf("slack workspace integration connected: actor_user_id=%s workspace_id=%s", strings.TrimSpace(actor.ID), integration.WorkspaceID)
	writeDataJSON(w, http.StatusOK, toSlackWorkspaceIntegrationStatusResponse(&integration))
}

// PatchSlackWorkspaceIntegration godoc
// @Summary Enable or disable Slack workspace integration
// @Description Enables or disables the connected Slack workspace integration without deleting credentials.
// @Tags notifications
// @Accept json
// @Produce json
// @Param request body api.PatchSlackWorkspaceIntegrationRequest true "Slack workspace integration patch"
// @Success 200 {object} api.SlackWorkspaceIntegrationStatusEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/integrations/slack [patch]
func (h *NotificationHandler) PatchSlackWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSlackAdmin(w, r) {
		return
	}

	var req api.PatchSlackWorkspaceIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	integration, err := h.slackAdmin.SetEnabled(r.Context(), req.Enabled)
	if err != nil {
		h.writeSlackWorkspaceIntegrationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSlackWorkspaceIntegrationStatusResponse(&integration))
}

// TestSlackWorkspaceIntegration godoc
// @Summary Test Slack workspace integration
// @Description Runs Slack auth.test using stored credentials and updates last test status.
// @Tags notifications
// @Produce json
// @Success 200 {object} api.SlackWorkspaceIntegrationStatusEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/integrations/slack/test [post]
func (h *NotificationHandler) TestSlackWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSlackAdmin(w, r) {
		return
	}

	integration, err := h.slackAdmin.TestConnection(r.Context())
	if err != nil {
		h.writeSlackWorkspaceIntegrationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSlackWorkspaceIntegrationStatusResponse(&integration))
}

// DeleteSlackWorkspaceIntegration godoc
// @Summary Disconnect Slack workspace integration
// @Description Disconnects the instance-level Slack workspace integration and removes stored credentials.
// @Tags notifications
// @Produce json
// @Success 204 {object} nil
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/integrations/slack [delete]
func (h *NotificationHandler) DeleteSlackWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeSlackAdmin(w, r) {
		return
	}

	if err := h.slackAdmin.Disconnect(r.Context()); err != nil {
		h.writeSlackWorkspaceIntegrationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *NotificationHandler) authorizeSlackAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.slackAdmin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return false
	}
	return authorizeGlobalAdmin(w, r, h.authMode, "global admin is required")
}

func (h *NotificationHandler) writeSlackWorkspaceIntegrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrSlackWorkspaceIntegrationConflict) || errors.Is(err, service.ErrSlackWorkspaceReplaceRequired) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrSlackWorkspaceBotTokenRequired) || errors.Is(err, service.ErrSlackWorkspaceEnabledRequired) ||
		errors.Is(err, service.ErrSlackWorkspaceInvalidAuth) || errors.Is(err, service.ErrSlackWorkspaceTokenRevoked) ||
		errors.Is(err, service.ErrSlackWorkspaceAccountInactive) || errors.Is(err, service.ErrSlackWorkspaceMalformedResponse) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if errors.Is(err, service.ErrSlackWorkspaceRateLimited) {
		writeErrorJSON(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	}
	if errors.Is(err, service.ErrSlackWorkspaceUpstream) || errors.Is(err, context.DeadlineExceeded) {
		writeErrorJSON(w, http.StatusBadGateway, "upstream_failure", "slack request failed")
		return
	}
	if errors.Is(err, context.Canceled) {
		writeErrorJSON(w, http.StatusBadGateway, "upstream_failure", "slack request canceled")
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toSlackWorkspaceIntegrationStatusResponse(integration *domain.SlackWorkspaceIntegration) api.SlackWorkspaceIntegrationStatusResponse {
	if integration == nil {
		return api.SlackWorkspaceIntegrationStatusResponse{Configured: false}
	}
	return api.SlackWorkspaceIntegrationStatusResponse{
		Configured:  true,
		Integration: toSlackWorkspaceIntegrationResponse(*integration),
	}
}

func toSlackWorkspaceIntegrationResponse(integration domain.SlackWorkspaceIntegration) *api.SlackWorkspaceIntegrationResponse {
	response := &api.SlackWorkspaceIntegrationResponse{
		ID:            integration.ID,
		WorkspaceID:   integration.WorkspaceID,
		WorkspaceName: integration.WorkspaceName,
		WorkspaceURL:  integration.WorkspaceURL,
		BotID:         integration.BotID,
		AuthedUserID:  integration.AuthedUserID,
		AppID:         integration.AppID,
		Enabled:       integration.Enabled,
		ConnectedAt:   integration.ConnectedAt.Format(time.RFC3339),
		UpdatedAt:     integration.UpdatedAt.Format(time.RFC3339),
	}
	if integration.LastTestedAt != nil {
		value := integration.LastTestedAt.Format(time.RFC3339)
		response.LastTestedAt = &value
	}
	if integration.LastTestSucceeded != nil {
		response.LastTestSucceeded = integration.LastTestSucceeded
	}
	return response
}
