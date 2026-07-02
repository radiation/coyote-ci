package handler

import (
	"context"
	"errors"
	"net/http"

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
	slackAdmin    slackWorkspaceIntegrationAdminService
	personalSlack personalSlackIdentityService
	authMode      auth.Mode
}

type personalSlackIdentityService interface {
	Get(ctx context.Context, user domain.User) (service.UserSlackIdentityState, error)
	ResolveByAuthenticatedEmail(ctx context.Context, user domain.User) (*service.ResolvedUserSlackIdentityCandidate, bool, error)
	Link(ctx context.Context, user domain.User, input service.LinkUserSlackIdentityInput) (domain.UserSlackIdentity, error)
	SetEnabled(ctx context.Context, user domain.User, enabled *bool) (domain.UserSlackIdentity, error)
	Unlink(ctx context.Context, user domain.User) error
}

type notificationAdminService interface {
	ListTargets(ctx context.Context) ([]domain.NotificationTarget, error)
	GetOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error)
	EnsureOwnedEmailTarget(ctx context.Context, user domain.User) (domain.NotificationTarget, error)
	SetOwnedEmailTargetEnabled(ctx context.Context, user domain.User, enabled *bool) (domain.NotificationTarget, error)
	GetNotificationDefaults(ctx context.Context) (service.NotificationDefaultsState, error)
	SetNotificationDefaults(ctx context.Context, failureEnabled *bool, successEnabled *bool) (service.NotificationDefaultsState, error)
	GetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User) (service.CommitAuthorNotificationPreferenceState, error)
	SetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User, input service.UpdateCommitAuthorNotificationPreferenceInput) (service.CommitAuthorNotificationPreferenceState, error)
	GetCommitAuthorSuccessNotificationPreference(ctx context.Context, user domain.User) (service.CommitAuthorNotificationPreferenceState, error)
	SetCommitAuthorSuccessNotificationPreference(ctx context.Context, user domain.User, input service.UpdateCommitAuthorNotificationPreferenceInput) (service.CommitAuthorNotificationPreferenceState, error)
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

func (h *NotificationHandler) SetSlackWorkspaceIntegrationService(admin slackWorkspaceIntegrationAdminService) {
	h.slackAdmin = admin
}

func (h *NotificationHandler) SetPersonalSlackIdentityService(personalSlack personalSlackIdentityService) {
	h.personalSlack = personalSlack
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
	if errors.Is(err, service.ErrNotificationPreferencePersonalTargetRequired) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrNotificationPreferencePersonalSlackRequired) {
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
		errors.Is(err, service.ErrNotificationTargetEnabledRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionTargetIDRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionTargetIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionProjectIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionJobIDInvalid) ||
		errors.Is(err, service.ErrNotificationSubscriptionScopeRequired) ||
		errors.Is(err, service.ErrNotificationSubscriptionEventTypeInvalid) ||
		errors.Is(err, service.ErrNotificationPersonalEmailRequired) ||
		errors.Is(err, service.ErrNotificationPersonalUserIDRequired) ||
		errors.Is(err, service.ErrNotificationPreferenceChannelEnabledRequired) ||
		errors.Is(err, service.ErrNotificationDefaultsUpdateRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
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
