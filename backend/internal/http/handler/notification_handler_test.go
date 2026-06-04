package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type fakeSampleNotificationSender struct {
	recipients []string
	err        error
}

func (f *fakeSampleNotificationSender) SendSampleBuildFailure(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.recipients...), nil
}

func TestNotificationHandler_SendSampleBuildFailure_NotFoundWhenUnavailable(t *testing.T) {
	h := NewNotificationHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, res.Code, res.Body.String())
	}
}

func TestNotificationHandler_SendSampleBuildFailure_ConflictForDisabledOrMissingRecipients(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "disabled", err: buildsvc.ErrEmailNotificationsDisabled},
		{name: "no recipients", err: buildsvc.ErrEmailNotificationRecipientsNotConfigured},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(&fakeSampleNotificationSender{err: tc.err})
			req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
			res := httptest.NewRecorder()

			h.SendSampleBuildFailure(res, req)

			if res.Code != http.StatusConflict {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusConflict, res.Code, res.Body.String())
			}
		})
	}
}

func TestNotificationHandler_SendSampleBuildFailure_InternalError(t *testing.T) {
	h := NewNotificationHandler(&fakeSampleNotificationSender{err: errors.New("smtp unavailable")})
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusInternalServerError, res.Code, res.Body.String())
	}
}

func TestNotificationHandler_SendSampleBuildFailure_Success(t *testing.T) {
	h := NewNotificationHandler(&fakeSampleNotificationSender{recipients: []string{"<dev@example.com>", "<qa@example.com>"}})
	req := httptest.NewRequest(http.MethodPost, "/api/dev/notifications/sample-build", nil)
	res := httptest.NewRecorder()

	h.SendSampleBuildFailure(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	var payload struct {
		Data struct {
			OK         bool     `json:"ok"`
			Recipients []string `json:"recipients"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Data.OK {
		t.Fatal("expected ok=true")
	}
	if len(payload.Data.Recipients) != 2 {
		t.Fatalf("expected two recipients, got %v", payload.Data.Recipients)
	}
}
