package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

// GetNotificationDefaults godoc
// @Summary Get notification defaults
// @Description Returns the effective instance defaults used when a newly eligible user first gets a personal email target for commit-author failure and success notifications.
// @Tags notifications
// @Produce json
// @Success 200 {object} api.NotificationDefaultsEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/notifications/defaults [get]
func (h *NotificationHandler) GetNotificationDefaults(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}

	state, err := h.admin.GetNotificationDefaults(r.Context())
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toNotificationDefaultsResponse(state))
}

// SetNotificationDefaults godoc
// @Summary Update notification defaults
// @Description Updates the instance defaults used only when a newly eligible user first gets a personal email target for commit-author failure and success notifications.
// @Tags notifications
// @Accept json
// @Produce json
// @Param request body api.PutNotificationDefaultsRequest true "Notification defaults"
// @Success 200 {object} api.NotificationDefaultsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/notifications/defaults [put]
func (h *NotificationHandler) SetNotificationDefaults(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}

	actor, _ := h.currentRequestUser(r)
	current, currentErr := h.admin.GetNotificationDefaults(r.Context())
	if currentErr != nil {
		h.writeNotificationError(w, currentErr)
		return
	}

	var req api.PutNotificationDefaultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.DefaultCommitAuthorFailureEmailEnabled == nil && req.DefaultCommitAuthorSuccessEmailEnabled == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", service.ErrNotificationDefaultsUpdateRequired.Error())
		return
	}

	state, err := h.admin.SetNotificationDefaults(r.Context(), req.DefaultCommitAuthorFailureEmailEnabled, req.DefaultCommitAuthorSuccessEmailEnabled)
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	log.Printf(
		"notification defaults updated: actor_user_id=%s old_default_commit_author_failure_email_enabled=%t new_default_commit_author_failure_email_enabled=%t old_default_commit_author_success_email_enabled=%t new_default_commit_author_success_email_enabled=%t",
		strings.TrimSpace(actor.ID),
		current.DefaultCommitAuthorFailureEmailEnabled,
		state.DefaultCommitAuthorFailureEmailEnabled,
		current.DefaultCommitAuthorSuccessEmailEnabled,
		state.DefaultCommitAuthorSuccessEmailEnabled,
	)

	writeDataJSON(w, http.StatusOK, toNotificationDefaultsResponse(state))
}

// GetMyCommitAuthorFailureNotificationPreference godoc
// @Summary Get my commit failure notification preference
// @Description Returns the authenticated user's opt-in state for commit-author failure notifications and whether delivery is currently active.
// @Tags users
// @Produce json
// @Success 200 {object} api.CommitAuthorFailureNotificationPreferenceEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/notification-preferences/commit-author-failures [get]
func (h *NotificationHandler) GetMyCommitAuthorFailureNotificationPreference(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	state, err := h.admin.GetCommitAuthorFailureNotificationPreference(r.Context(), user)
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toCommitAuthorFailureNotificationPreferenceResponse(state))
}

// GetMyCommitAuthorSuccessNotificationPreference godoc
// @Summary Get my commit success notification preference
// @Description Returns the authenticated user's opt-in state for commit-author success notifications and whether delivery is currently active.
// @Tags users
// @Produce json
// @Success 200 {object} api.CommitAuthorSuccessNotificationPreferenceEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/notification-preferences/commit-author-successes [get]
func (h *NotificationHandler) GetMyCommitAuthorSuccessNotificationPreference(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	state, err := h.admin.GetCommitAuthorSuccessNotificationPreference(r.Context(), user)
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toCommitAuthorSuccessNotificationPreferenceResponse(state))
}

// SetMyCommitAuthorFailureNotificationPreference godoc
// @Summary Update my commit failure notification preference
// @Description Sets the authenticated user's explicit opt-in state for commit-author failure notifications.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.PutCommitAuthorFailureNotificationPreferenceRequest true "Commit failure notification preference"
// @Success 200 {object} api.CommitAuthorFailureNotificationPreferenceEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/notification-preferences/commit-author-failures [put]
func (h *NotificationHandler) SetMyCommitAuthorFailureNotificationPreference(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.PutCommitAuthorFailureNotificationPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	state, err := h.admin.SetCommitAuthorFailureNotificationPreference(r.Context(), user, service.UpdateCommitAuthorNotificationPreferenceInput{
		EmailEnabled: req.EmailEnabled,
		SlackEnabled: req.SlackEnabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toCommitAuthorFailureNotificationPreferenceResponse(state))
}

// SetMyCommitAuthorSuccessNotificationPreference godoc
// @Summary Update my commit success notification preference
// @Description Sets the authenticated user's explicit opt-in state for commit-author success notifications.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.PutCommitAuthorSuccessNotificationPreferenceRequest true "Commit success notification preference"
// @Success 200 {object} api.CommitAuthorSuccessNotificationPreferenceEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/notification-preferences/commit-author-successes [put]
func (h *NotificationHandler) SetMyCommitAuthorSuccessNotificationPreference(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.PutCommitAuthorSuccessNotificationPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	state, err := h.admin.SetCommitAuthorSuccessNotificationPreference(r.Context(), user, service.UpdateCommitAuthorNotificationPreferenceInput{
		EmailEnabled: req.EmailEnabled,
		SlackEnabled: req.SlackEnabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toCommitAuthorSuccessNotificationPreferenceResponse(state))
}

func toNotificationDefaultsResponse(state service.NotificationDefaultsState) api.NotificationDefaultsResponse {
	return api.NotificationDefaultsResponse{
		DefaultCommitAuthorFailureEmailEnabled: state.DefaultCommitAuthorFailureEmailEnabled,
		DefaultCommitAuthorSuccessEmailEnabled: state.DefaultCommitAuthorSuccessEmailEnabled,
	}
}

func toCommitAuthorFailureNotificationPreferenceResponse(state service.CommitAuthorNotificationPreferenceState) api.CommitAuthorFailureNotificationPreferenceResponse {
	return api.CommitAuthorFailureNotificationPreferenceResponse{
		Email: toCommitAuthorNotificationPreferenceChannelResponse(state.Email),
		Slack: toCommitAuthorSlackNotificationPreferenceChannelResponse(state.Slack),
	}
}

func toCommitAuthorSuccessNotificationPreferenceResponse(state service.CommitAuthorNotificationPreferenceState) api.CommitAuthorSuccessNotificationPreferenceResponse {
	return api.CommitAuthorSuccessNotificationPreferenceResponse{
		Email: toCommitAuthorNotificationPreferenceChannelResponse(state.Email),
		Slack: toCommitAuthorSlackNotificationPreferenceChannelResponse(state.Slack),
	}
}

func toCommitAuthorNotificationPreferenceChannelResponse(state service.CommitAuthorEmailNotificationPreferenceState) api.CommitAuthorNotificationPreferenceChannelResponse {
	response := api.CommitAuthorNotificationPreferenceChannelResponse{
		Enabled:           state.Enabled,
		DeliveryActive:    state.DeliveryActive,
		UnavailableReason: state.UnavailableReason,
	}
	if state.Target != nil {
		targetResponse := toNotificationTargetResponse(*state.Target)
		response.Target = &targetResponse
	}
	return response
}

func toCommitAuthorSlackNotificationPreferenceChannelResponse(state service.CommitAuthorSlackNotificationPreferenceState) api.CommitAuthorNotificationPreferenceChannelResponse {
	return api.CommitAuthorNotificationPreferenceChannelResponse{
		Enabled:           state.Enabled,
		DeliveryActive:    state.DeliveryActive,
		UnavailableReason: state.UnavailableReason,
	}
}
