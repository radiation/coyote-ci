package handler

import (
	"context"
	"errors"
	"net/http"

	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type sampleBuildEmailSender interface {
	SendSampleBuildFailure(ctx context.Context) ([]string, error)
}

type NotificationHandler struct {
	notifications sampleBuildEmailSender
}

func NewNotificationHandler(notifications sampleBuildEmailSender) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
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
