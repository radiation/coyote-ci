package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func (h *NotificationHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	projectID := trimQueryString(r, "project_id")
	jobID := trimQueryString(r, "job_id")
	subscriptions, err := h.admin.ListSubscriptions(r.Context(), service.ListNotificationSubscriptionsInput{
		ProjectID: projectID,
		JobID:     jobID,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	responses := make([]api.NotificationSubscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		responses = append(responses, toNotificationSubscriptionResponse(subscription))
	}
	writeDataJSON(w, http.StatusOK, api.NotificationSubscriptionListResponse{Subscriptions: responses})
}

func (h *NotificationHandler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateNotificationSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	subscription, err := h.admin.CreateSubscription(r.Context(), service.CreateNotificationSubscriptionInput{
		TargetID:  req.TargetID,
		ProjectID: req.ProjectID,
		JobID:     req.JobID,
		EventType: req.EventType,
		Enabled:   req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toNotificationSubscriptionResponse(subscription))
}

func (h *NotificationHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.UpdateNotificationSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	subscription, err := h.admin.UpdateSubscription(r.Context(), strings.TrimSpace(chi.URLParam(r, "subscriptionID")), service.UpdateNotificationSubscriptionInput{
		Enabled: req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toNotificationSubscriptionResponse(subscription))
}

func (h *NotificationHandler) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	if err := h.admin.DeleteSubscription(r.Context(), strings.TrimSpace(chi.URLParam(r, "subscriptionID"))); err != nil {
		h.writeNotificationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toNotificationSubscriptionResponse(subscription domain.NotificationSubscription) api.NotificationSubscriptionResponse {
	return api.NotificationSubscriptionResponse{
		ID:        subscription.ID,
		TargetID:  subscription.TargetID,
		ProjectID: subscription.ProjectID,
		JobID:     subscription.JobID,
		EventType: string(subscription.EventType),
		Enabled:   subscription.Enabled,
		CreatedAt: subscription.CreatedAt.Format(time.RFC3339),
		UpdatedAt: subscription.UpdatedAt.Format(time.RFC3339),
	}
}

func trimQueryString(r *http.Request, key string) *string {
	trimmed := strings.TrimSpace(r.URL.Query().Get(key))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
