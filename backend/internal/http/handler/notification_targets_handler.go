package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func (h *NotificationHandler) ListTargets(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	targets, err := h.admin.ListTargets(r.Context())
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	responses := make([]api.NotificationTargetResponse, 0, len(targets))
	for _, target := range targets {
		responses = append(responses, toNotificationTargetResponse(target))
	}
	writeDataJSON(w, http.StatusOK, api.NotificationTargetListResponse{Targets: responses})
}

func (h *NotificationHandler) GetMyEmailTarget(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	target, err := h.admin.GetOwnedEmailTarget(r.Context(), user)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			writeDataJSON(w, http.StatusOK, api.MyEmailNotificationTargetResponse{Target: nil})
			return
		}
		h.writeNotificationError(w, err)
		return
	}

	response := toNotificationTargetResponse(target)
	writeDataJSON(w, http.StatusOK, api.MyEmailNotificationTargetResponse{Target: &response})
}

func (h *NotificationHandler) EnsureMyEmailTarget(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	target, err := h.admin.EnsureOwnedEmailTarget(r.Context(), user)
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toNotificationTargetResponse(target))
}

// SetMyEmailTarget godoc
// @Summary Update my email notification target
// @Description Enables or disables the authenticated user's owned personal email notification target without changing its address or preferences.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.PutMyEmailNotificationTargetRequest true "Owned personal email target state"
// @Success 200 {object} api.NotificationTargetEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/notification-targets/email [put]
func (h *NotificationHandler) SetMyEmailTarget(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.PutMyEmailNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	target, err := h.admin.SetOwnedEmailTargetEnabled(r.Context(), user, req.Enabled)
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toNotificationTargetResponse(target))
}

func (h *NotificationHandler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	target, err := h.admin.CreateTarget(r.Context(), service.CreateNotificationTargetInput{
		Type:       req.Type,
		Name:       req.Name,
		Address:    req.Address,
		WebhookURL: req.WebhookURL,
		Enabled:    req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toNotificationTargetResponse(target))
}

func (h *NotificationHandler) CreateEmailTarget(w http.ResponseWriter, r *http.Request) {
	h.CreateTarget(w, r)
}

func (h *NotificationHandler) UpdateTarget(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.UpdateNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	target, err := h.admin.UpdateTarget(r.Context(), strings.TrimSpace(chi.URLParam(r, "targetID")), service.UpdateNotificationTargetInput{
		Name:       req.Name,
		Address:    req.Address,
		WebhookURL: req.WebhookURL,
		Enabled:    req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toNotificationTargetResponse(target))
}

func (h *NotificationHandler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	if err := h.admin.DeleteTarget(r.Context(), strings.TrimSpace(chi.URLParam(r, "targetID"))); err != nil {
		h.writeNotificationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toNotificationTargetResponse(target domain.NotificationTarget) api.NotificationTargetResponse {
	response := api.NotificationTargetResponse{
		ID:          target.ID,
		OwnerUserID: target.OwnerUserID,
		Type:        string(target.Type),
		Name:        target.Name,
		Enabled:     target.Enabled,
		CreatedAt:   target.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   target.UpdatedAt.Format(time.RFC3339),
	}
	if target.Type == domain.NotificationTargetTypeEmail {
		response.Address = target.Recipient
	} else {
		response.WebhookConfigured = strings.TrimSpace(target.Recipient) != ""
	}
	return response
}
