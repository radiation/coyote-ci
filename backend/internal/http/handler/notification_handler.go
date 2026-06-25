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
	GetOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error)
	EnsureOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error)
	CreateTarget(ctx context.Context, input service.CreateNotificationTargetInput) (domain.NotificationTarget, error)
	CreateEmailTarget(ctx context.Context, input service.CreateNotificationTargetInput) (domain.NotificationTarget, error)
	UpdateTarget(ctx context.Context, id string, input service.UpdateNotificationTargetInput) (domain.NotificationTarget, error)
	DeleteTarget(ctx context.Context, id string) error
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
	if errors.Is(err, repository.ErrNotificationTargetDuplicate) || errors.Is(err, repository.ErrNotificationSubscriptionDuplicate) || errors.Is(err, repository.ErrNotificationTargetOwnershipConflict) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrNotificationTargetNameRequired) ||
		errors.Is(err, service.ErrNotificationTargetTypeInvalid) ||
		errors.Is(err, service.ErrNotificationTargetAddressRequired) ||
		errors.Is(err, service.ErrNotificationTargetAddressInvalid) ||
		errors.Is(err, service.ErrNotificationTargetWebhookURLRequired) ||
		errors.Is(err, service.ErrNotificationTargetWebhookURLInvalid) ||
		errors.Is(err, service.ErrNotificationTargetIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionTargetIDRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionTargetIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionProjectIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionJobIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionScopeRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionEventTypeInvalid) ||
		errors.Is(err, service.ErrNotificationPersonalEmailRequired) ||
		errors.Is(err, service.ErrNotificationPersonalUserIDRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
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

func (h *NotificationHandler) currentRequestUser(r *http.Request) (domain.User, bool) {
	if user, ok := auth.CurrentUser(r.Context()); ok {
		return user, true
	}
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return auth.DisabledModeUser(), true
	}
	return domain.User{}, false
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
