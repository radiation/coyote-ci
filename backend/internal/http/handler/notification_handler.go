package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type sampleBuildEmailSender interface {
	SendSampleBuildFailure(ctx context.Context) ([]string, error)
}

type NotificationHandler struct {
	notifications sampleBuildEmailSender
	admin         notificationAdminService
	authMode      auth.Mode
}

type notificationAdminService interface {
	ListTargets(ctx context.Context) ([]domain.NotificationTarget, error)
	CreateEmailTarget(ctx context.Context, input service.CreateNotificationTargetInput) (domain.NotificationTarget, error)
	UpdateTarget(ctx context.Context, id string, input service.UpdateNotificationTargetInput) (domain.NotificationTarget, error)
	ListSubscriptions(ctx context.Context, input service.ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error)
	CreateSubscription(ctx context.Context, input service.CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error)
	UpdateSubscription(ctx context.Context, id string, input service.UpdateNotificationSubscriptionInput) (domain.NotificationSubscription, error)
	DeleteSubscription(ctx context.Context, id string) error
}

func NewNotificationHandler(notifications sampleBuildEmailSender) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
}

func (h *NotificationHandler) SetAuthorization(mode auth.Mode) {
	h.authMode = mode
}

func (h *NotificationHandler) SetAdminService(admin notificationAdminService) {
	h.admin = admin
}

func (h *NotificationHandler) HasSampleSender() bool {
	return h != nil && h.notifications != nil
}

func (h *NotificationHandler) SendSampleBuildFailure(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.notifications == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	recipients, err := h.notifications.SendSampleBuildFailure(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, buildsvc.ErrEmailNotificationsDisabled), errors.Is(err, buildsvc.ErrEmailNotificationRecipientsNotConfigured):
			writeErrorJSON(w, http.StatusConflict, "invalid_state", err.Error())
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "failed to send sample build email")
		}
		return
	}

	writeDataJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"recipients": recipients,
	})
}

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

func (h *NotificationHandler) CreateEmailTarget(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	target, err := h.admin.CreateEmailTarget(r.Context(), service.CreateNotificationTargetInput{
		Name:    req.Name,
		Address: req.Address,
		Enabled: req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toNotificationTargetResponse(target))
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
		Name:    req.Name,
		Address: req.Address,
		Enabled: req.Enabled,
	})
	if err != nil {
		h.writeNotificationError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toNotificationTargetResponse(target))
}

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

func (h *NotificationHandler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return false
	}
	return authorizeGlobalAdmin(w, r, h.authMode, "global admin is required")
}

func (h *NotificationHandler) writeNotificationError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotificationTargetNotFound) || errors.Is(err, repository.ErrNotificationSubscriptionNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrNotificationTargetDuplicate) || errors.Is(err, repository.ErrNotificationSubscriptionDuplicate) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrNotificationTargetNameRequired) ||
		errors.Is(err, service.ErrNotificationTargetAddressRequired) ||
		errors.Is(err, service.ErrNotificationTargetAddressInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionTargetIDRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionScopeRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionEventTypeInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toNotificationTargetResponse(target domain.NotificationTarget) api.NotificationTargetResponse {
	return api.NotificationTargetResponse{
		ID:        target.ID,
		Type:      string(target.Type),
		Name:      target.Name,
		Address:   target.Recipient,
		Enabled:   target.Enabled,
		CreatedAt: target.CreatedAt.Format(time.RFC3339),
		UpdatedAt: target.UpdatedAt.Format(time.RFC3339),
	}
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
