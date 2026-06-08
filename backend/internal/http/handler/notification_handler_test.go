package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
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

func TestNotificationHandler_AdminEndpoints(t *testing.T) {
	repo := memoryrepo.NewNotificationSubscriptionRepository()
	notificationService := service.NewNotificationService(repo)
	h := NewNotificationHandler(nil)
	h.SetAdminService(notificationService)
	h.SetAuthorization(auth.ModeHeader)
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	listReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	listReq = listReq.WithContext(auth.WithUser(listReq.Context(), admin))
	listRes := httptest.NewRecorder()
	h.ListTargets(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected empty list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	listData := decodeDataMap(t, listRes)
	if targets, ok := listData["targets"].([]any); !ok || len(targets) != 0 {
		t.Fatalf("expected empty targets list, got %v", listData["targets"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Build Alerts","address":"dev@example.com"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), admin))
	createRes := httptest.NewRecorder()
	h.CreateEmailTarget(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create target status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	targetData := decodeDataMap(t, createRes)
	targetID, _ := targetData["id"].(string)
	if targetID == "" {
		t.Fatalf("expected target id, got %v", targetData)
	}

	invalidTargetReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Bad","address":"nope"}`))
	invalidTargetReq = invalidTargetReq.WithContext(auth.WithUser(invalidTargetReq.Context(), admin))
	invalidTargetRes := httptest.NewRecorder()
	h.CreateEmailTarget(invalidTargetRes, invalidTargetReq)
	if invalidTargetRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid target status %d, got %d body=%s", http.StatusBadRequest, invalidTargetRes.Code, invalidTargetRes.Body.String())
	}

	disableBody := `{"enabled":false}`
	updateTargetReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/"+targetID, bytes.NewBufferString(disableBody)), "targetID", targetID)
	updateTargetReq = updateTargetReq.WithContext(auth.WithUser(updateTargetReq.Context(), admin))
	updateTargetRes := httptest.NewRecorder()
	h.UpdateTarget(updateTargetRes, updateTargetReq)
	if updateTargetRes.Code != http.StatusOK {
		t.Fatalf("expected update target status %d, got %d body=%s", http.StatusOK, updateTargetRes.Code, updateTargetRes.Body.String())
	}
	updatedTargetData := decodeDataMap(t, updateTargetRes)
	if enabled, ok := updatedTargetData["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected disabled target, got %v", updatedTargetData["enabled"])
	}

	enableBody := `{"enabled":true}`
	enableTargetReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/"+targetID, bytes.NewBufferString(enableBody)), "targetID", targetID)
	enableTargetReq = enableTargetReq.WithContext(auth.WithUser(enableTargetReq.Context(), admin))
	enableTargetRes := httptest.NewRecorder()
	h.UpdateTarget(enableTargetRes, enableTargetReq)
	if enableTargetRes.Code != http.StatusOK {
		t.Fatalf("expected re-enable target status %d, got %d body=%s", http.StatusOK, enableTargetRes.Code, enableTargetRes.Body.String())
	}

	projectReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"project-1","event_type":"build_failed"}`))
	projectReq = projectReq.WithContext(auth.WithUser(projectReq.Context(), admin))
	projectRes := httptest.NewRecorder()
	h.CreateSubscription(projectRes, projectReq)
	if projectRes.Code != http.StatusCreated {
		t.Fatalf("expected project subscription status %d, got %d body=%s", http.StatusCreated, projectRes.Code, projectRes.Body.String())
	}
	projectData := decodeDataMap(t, projectRes)
	projectSubID, _ := projectData["id"].(string)

	jobReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","job_id":"job-1","event_type":"build_succeeded"}`))
	jobReq = jobReq.WithContext(auth.WithUser(jobReq.Context(), admin))
	jobRes := httptest.NewRecorder()
	h.CreateSubscription(jobRes, jobReq)
	if jobRes.Code != http.StatusCreated {
		t.Fatalf("expected job subscription status %d, got %d body=%s", http.StatusCreated, jobRes.Code, jobRes.Body.String())
	}
	jobData := decodeDataMap(t, jobRes)
	jobSubID, _ := jobData["id"].(string)
	if jobSubID == "" {
		t.Fatalf("expected job subscription id, got %v", jobData)
	}

	bothReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"project-1","job_id":"job-1","event_type":"build_failed"}`))
	bothReq = bothReq.WithContext(auth.WithUser(bothReq.Context(), admin))
	bothRes := httptest.NewRecorder()
	h.CreateSubscription(bothRes, bothReq)
	if bothRes.Code != http.StatusBadRequest {
		t.Fatalf("expected both-scope status %d, got %d body=%s", http.StatusBadRequest, bothRes.Code, bothRes.Body.String())
	}

	neitherReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","event_type":"build_failed"}`))
	neitherReq = neitherReq.WithContext(auth.WithUser(neitherReq.Context(), admin))
	neitherRes := httptest.NewRecorder()
	h.CreateSubscription(neitherRes, neitherReq)
	if neitherRes.Code != http.StatusBadRequest {
		t.Fatalf("expected neither-scope status %d, got %d body=%s", http.StatusBadRequest, neitherRes.Code, neitherRes.Body.String())
	}

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"project-1","event_type":"build_failed"}`))
	duplicateReq = duplicateReq.WithContext(auth.WithUser(duplicateReq.Context(), admin))
	duplicateRes := httptest.NewRecorder()
	h.CreateSubscription(duplicateRes, duplicateReq)
	if duplicateRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate subscription status %d, got %d body=%s", http.StatusConflict, duplicateRes.Code, duplicateRes.Body.String())
	}

	projectListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id=project-1", nil)
	projectListReq = projectListReq.WithContext(auth.WithUser(projectListReq.Context(), admin))
	projectListRes := httptest.NewRecorder()
	h.ListSubscriptions(projectListRes, projectListReq)
	if projectListRes.Code != http.StatusOK {
		t.Fatalf("expected project filter status %d, got %d body=%s", http.StatusOK, projectListRes.Code, projectListRes.Body.String())
	}
	projectListData := decodeDataMap(t, projectListRes)
	projectSubs, ok := projectListData["subscriptions"].([]any)
	if !ok || len(projectSubs) != 1 {
		t.Fatalf("expected one project subscription, got %v", projectListData["subscriptions"])
	}

	jobListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?job_id=job-1", nil)
	jobListReq = jobListReq.WithContext(auth.WithUser(jobListReq.Context(), admin))
	jobListRes := httptest.NewRecorder()
	h.ListSubscriptions(jobListRes, jobListReq)
	if jobListRes.Code != http.StatusOK {
		t.Fatalf("expected job filter status %d, got %d body=%s", http.StatusOK, jobListRes.Code, jobListRes.Body.String())
	}
	jobListData := decodeDataMap(t, jobListRes)
	jobSubs, ok := jobListData["subscriptions"].([]any)
	if !ok || len(jobSubs) != 1 {
		t.Fatalf("expected one job subscription, got %v", jobListData["subscriptions"])
	}

	updateSubReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-subscriptions/"+projectSubID, bytes.NewBufferString(`{"enabled":false}`)), "subscriptionID", projectSubID)
	updateSubReq = updateSubReq.WithContext(auth.WithUser(updateSubReq.Context(), admin))
	updateSubRes := httptest.NewRecorder()
	h.UpdateSubscription(updateSubRes, updateSubReq)
	if updateSubRes.Code != http.StatusOK {
		t.Fatalf("expected update subscription status %d, got %d body=%s", http.StatusOK, updateSubRes.Code, updateSubRes.Body.String())
	}
	updatedSubData := decodeDataMap(t, updateSubRes)
	if enabled, ok := updatedSubData["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected disabled subscription, got %v", updatedSubData["enabled"])
	}

	deleteSubReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/"+jobSubID, nil), "subscriptionID", jobSubID)
	deleteSubReq = deleteSubReq.WithContext(auth.WithUser(deleteSubReq.Context(), admin))
	deleteSubRes := httptest.NewRecorder()
	h.DeleteSubscription(deleteSubRes, deleteSubReq)
	if deleteSubRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete subscription status %d, got %d body=%s", http.StatusNoContent, deleteSubRes.Code, deleteSubRes.Body.String())
	}
}
