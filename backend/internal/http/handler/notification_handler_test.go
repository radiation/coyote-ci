package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
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

type fakeNotificationAdminService struct {
	listTargetsResult            []domain.NotificationTarget
	listTargetsErr               error
	getOwnedEmailTargetResult    domain.NotificationTarget
	getOwnedEmailTargetErr       error
	ensureOwnedEmailTargetResult domain.NotificationTarget
	ensureOwnedEmailTargetErr    error
	createTargetResult           domain.NotificationTarget
	createTargetErr              error
	updateTargetResult           domain.NotificationTarget
	updateTargetErr              error
	listSubscriptionsResult      []domain.NotificationSubscription
	listSubscriptionsErr         error
	createSubscriptionResult     domain.NotificationSubscription
	createSubscriptionErr        error
	updateSubscriptionResult     domain.NotificationSubscription
	updateSubscriptionErr        error
	deleteSubscriptionErr        error
}

func (f *fakeNotificationAdminService) ListTargets(_ context.Context) ([]domain.NotificationTarget, error) {
	return f.listTargetsResult, f.listTargetsErr
}

func (f *fakeNotificationAdminService) GetOwnedEmailTarget(_ context.Context, _ domain.User) (domain.NotificationTarget, error) {
	return f.getOwnedEmailTargetResult, f.getOwnedEmailTargetErr
}

func (f *fakeNotificationAdminService) EnsureOwnedEmailTarget(_ context.Context, _ domain.User) (domain.NotificationTarget, error) {
	return f.ensureOwnedEmailTargetResult, f.ensureOwnedEmailTargetErr
}

func (f *fakeNotificationAdminService) CreateTarget(_ context.Context, _ service.CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.createTargetResult, f.createTargetErr
}

func (f *fakeNotificationAdminService) CreateEmailTarget(_ context.Context, _ service.CreateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.createTargetResult, f.createTargetErr
}

func (f *fakeNotificationAdminService) UpdateTarget(_ context.Context, _ string, _ service.UpdateNotificationTargetInput) (domain.NotificationTarget, error) {
	return f.updateTargetResult, f.updateTargetErr
}

func (f *fakeNotificationAdminService) ListSubscriptions(_ context.Context, _ service.ListNotificationSubscriptionsInput) ([]domain.NotificationSubscription, error) {
	return f.listSubscriptionsResult, f.listSubscriptionsErr
}

func (f *fakeNotificationAdminService) CreateSubscription(_ context.Context, _ service.CreateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	return f.createSubscriptionResult, f.createSubscriptionErr
}

func (f *fakeNotificationAdminService) UpdateSubscription(_ context.Context, _ string, _ service.UpdateNotificationSubscriptionInput) (domain.NotificationSubscription, error) {
	return f.updateSubscriptionResult, f.updateSubscriptionErr
}

func (f *fakeNotificationAdminService) DeleteTarget(_ context.Context, _ string) error {
	return nil
}

func (f *fakeNotificationAdminService) DeleteSubscription(_ context.Context, _ string) error {
	return f.deleteSubscriptionErr
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

func TestNotificationHandler_MyEmailTargetEndpoints(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		getOwnedEmailTargetErr: repository.ErrNotificationTargetNotFound,
		ensureOwnedEmailTargetResult: domain.NotificationTarget{
			ID:          "target-1",
			OwnerUserID: stringPtr("user-1"),
			Type:        domain.NotificationTargetTypeEmail,
			Name:        "User One",
			Recipient:   "<user@example.com>",
			Enabled:     true,
		},
	})
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
	getReq = getReq.WithContext(auth.WithUser(getReq.Context(), user))
	getRes := httptest.NewRecorder()
	h.GetMyEmailTarget(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get my target status %d, got %d body=%s", http.StatusOK, getRes.Code, getRes.Body.String())
	}
	getData := decodeDataMap(t, getRes)
	if targetValue, exists := getData["target"]; !exists || targetValue != nil {
		t.Fatalf("expected null target payload, got %v", getData["target"])
	}

	ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
	ensureReq = ensureReq.WithContext(auth.WithUser(ensureReq.Context(), user))
	ensureRes := httptest.NewRecorder()
	h.EnsureMyEmailTarget(ensureRes, ensureReq)
	if ensureRes.Code != http.StatusOK {
		t.Fatalf("expected ensure my target status %d, got %d body=%s", http.StatusOK, ensureRes.Code, ensureRes.Body.String())
	}
	ensureData := decodeDataMap(t, ensureRes)
	if ensureData["id"] != "target-1" {
		t.Fatalf("expected ensured target id target-1, got %v", ensureData["id"])
	}
}

func TestNotificationHandler_MyEmailTargetEndpointsRequireAuthentication(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
	getRes := httptest.NewRecorder()
	h.GetMyEmailTarget(getRes, getReq)
	if getRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected get my target unauthorized status %d, got %d body=%s", http.StatusUnauthorized, getRes.Code, getRes.Body.String())
	}

	ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
	ensureRes := httptest.NewRecorder()
	h.EnsureMyEmailTarget(ensureRes, ensureReq)
	if ensureRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected ensure my target unauthorized status %d, got %d body=%s", http.StatusUnauthorized, ensureRes.Code, ensureRes.Body.String())
	}
}

func TestNotificationHandler_MyEmailTargetEndpointErrors(t *testing.T) {
	t.Run("endpoint unavailable", func(t *testing.T) {
		h := NewNotificationHandler(nil)

		getReq := httptest.NewRequest(http.MethodGet, "/api/me/notification-targets/email", nil)
		getRes := httptest.NewRecorder()
		h.GetMyEmailTarget(getRes, getReq)
		if getRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable get status %d, got %d body=%s", http.StatusNotFound, getRes.Code, getRes.Body.String())
		}

		ensureReq := httptest.NewRequest(http.MethodPost, "/api/me/notification-targets/email", nil)
		ensureRes := httptest.NewRecorder()
		h.EnsureMyEmailTarget(ensureRes, ensureReq)
		if ensureRes.Code != http.StatusNotFound {
			t.Fatalf("expected unavailable ensure status %d, got %d body=%s", http.StatusNotFound, ensureRes.Code, ensureRes.Body.String())
		}
	})

	tests := []struct {
		name       string
		method     string
		serviceErr error
		wantStatus int
	}{
		{name: "get internal error", method: http.MethodGet, serviceErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
		{name: "ensure ownership conflict", method: http.MethodPost, serviceErr: repository.ErrNotificationTargetOwnershipConflict, wantStatus: http.StatusConflict},
		{name: "ensure invalid request", method: http.MethodPost, serviceErr: service.ErrNotificationPersonalEmailRequired, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewNotificationHandler(nil)
			h.SetAuthorization(auth.ModeHeader)
			fakeSvc := &fakeNotificationAdminService{}
			if tc.method == http.MethodGet {
				fakeSvc.getOwnedEmailTargetErr = tc.serviceErr
			} else {
				fakeSvc.ensureOwnedEmailTargetErr = tc.serviceErr
			}
			h.SetAdminService(fakeSvc)
			user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

			req := httptest.NewRequest(tc.method, "/api/me/notification-targets/email", nil)
			req = req.WithContext(auth.WithUser(req.Context(), user))
			res := httptest.NewRecorder()

			if tc.method == http.MethodGet {
				h.GetMyEmailTarget(res, req)
			} else {
				h.EnsureMyEmailTarget(res, req)
			}

			if res.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, res.Code, res.Body.String())
			}
		})
	}
}

func TestNotificationHandler_AdminEndpoints(t *testing.T) {
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	otherJobID := uuid.NewString()
	otherProjectID := uuid.NewString()

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

	projectReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+projectID+`","event_type":"build_failed"}`))
	projectReq = projectReq.WithContext(auth.WithUser(projectReq.Context(), admin))
	projectRes := httptest.NewRecorder()
	h.CreateSubscription(projectRes, projectReq)
	if projectRes.Code != http.StatusCreated {
		t.Fatalf("expected project subscription status %d, got %d body=%s", http.StatusCreated, projectRes.Code, projectRes.Body.String())
	}
	projectData := decodeDataMap(t, projectRes)
	projectSubID, _ := projectData["id"].(string)

	jobReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","job_id":"`+jobID+`","event_type":"build_succeeded"}`))
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

	bothReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+otherProjectID+`","job_id":"`+otherJobID+`","event_type":"build_failed"}`))
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

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"`+projectID+`","event_type":"build_failed"}`))
	duplicateReq = duplicateReq.WithContext(auth.WithUser(duplicateReq.Context(), admin))
	duplicateRes := httptest.NewRecorder()
	h.CreateSubscription(duplicateRes, duplicateReq)
	if duplicateRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate subscription status %d, got %d body=%s", http.StatusConflict, duplicateRes.Code, duplicateRes.Body.String())
	}

	projectListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id="+projectID, nil)
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

	jobListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?job_id="+jobID, nil)
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

	invalidProjectReq := httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"`+targetID+`","project_id":"not-a-uuid","event_type":"build_failed"}`))
	invalidProjectReq = invalidProjectReq.WithContext(auth.WithUser(invalidProjectReq.Context(), admin))
	invalidProjectRes := httptest.NewRecorder()
	h.CreateSubscription(invalidProjectRes, invalidProjectReq)
	if invalidProjectRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid project id status %d, got %d body=%s", http.StatusBadRequest, invalidProjectRes.Code, invalidProjectRes.Body.String())
	}

	invalidListReq := httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id=not-a-uuid", nil)
	invalidListReq = invalidListReq.WithContext(auth.WithUser(invalidListReq.Context(), admin))
	invalidListRes := httptest.NewRecorder()
	h.ListSubscriptions(invalidListRes, invalidListReq)
	if invalidListRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid list filter status %d, got %d body=%s", http.StatusBadRequest, invalidListRes.Code, invalidListRes.Body.String())
	}
}

func TestNotificationHandler_TargetResponsesMaskSlackWebhookSecrets(t *testing.T) {
	h := NewNotificationHandler(nil)
	h.SetAuthorization(auth.ModeHeader)
	h.SetAdminService(&fakeNotificationAdminService{
		listTargetsResult: []domain.NotificationTarget{
			{ID: "target-email", Type: domain.NotificationTargetTypeEmail, Name: "Email", Recipient: "<dev@example.com>", Enabled: true},
			{ID: "target-slack", Type: domain.NotificationTargetTypeSlackWebhook, Name: "Slack", Recipient: "https://hooks.slack.example/services/T/B/X", Enabled: true},
		},
	})
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}

	req := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	res := httptest.NewRecorder()
	h.ListTargets(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	data := decodeDataMap(t, res)
	targets, ok := data["targets"].([]any)
	if !ok || len(targets) != 2 {
		t.Fatalf("expected two targets, got %v", data["targets"])
	}
	slackTarget, ok := targets[1].(map[string]any)
	if !ok {
		t.Fatalf("expected slack target map, got %T", targets[1])
	}
	if _, exists := slackTarget["address"]; exists {
		t.Fatalf("expected slack target secret to be omitted, got %v", slackTarget)
	}
	configured, ok := slackTarget["webhook_configured"].(bool)
	if !ok || !configured {
		t.Fatalf("expected webhook_configured=true, got %v", slackTarget["webhook_configured"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"type":"slack_webhook","name":"Slack","webhook_url":"https://hooks.slack.example/services/T/B/X"}`))
	createReq = createReq.WithContext(auth.WithUser(createReq.Context(), admin))
	createRes := httptest.NewRecorder()
	h.SetAdminService(&fakeNotificationAdminService{
		createTargetResult: domain.NotificationTarget{ID: "target-slack", Type: domain.NotificationTargetTypeSlackWebhook, Name: "Slack", Recipient: "https://hooks.slack.example/services/T/B/X", Enabled: true},
	})
	h.CreateTarget(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create target status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	created := decodeDataMap(t, createRes)
	if _, exists := created["address"]; exists {
		t.Fatalf("expected created slack target to omit address, got %v", created)
	}
}

func TestNotificationHandler_AdminAuthorizationAndErrors(t *testing.T) {
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	nonAdmin := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	missingHandler := NewNotificationHandler(nil)
	missingReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	missingRes := httptest.NewRecorder()
	missingHandler.ListTargets(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing admin service status %d, got %d body=%s", http.StatusNotFound, missingRes.Code, missingRes.Body.String())
	}

	authHandler := NewNotificationHandler(nil)
	authHandler.SetAdminService(&fakeNotificationAdminService{})
	authHandler.SetAuthorization(auth.ModeHeader)

	unauthReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	unauthRes := httptest.NewRecorder()
	authHandler.ListTargets(unauthRes, unauthReq)
	if unauthRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status %d, got %d body=%s", http.StatusUnauthorized, unauthRes.Code, unauthRes.Body.String())
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
	forbiddenReq = forbiddenReq.WithContext(auth.WithUser(forbiddenReq.Context(), nonAdmin))
	forbiddenRes := httptest.NewRecorder()
	authHandler.ListTargets(forbiddenRes, forbiddenReq)
	if forbiddenRes.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status %d, got %d body=%s", http.StatusForbidden, forbiddenRes.Code, forbiddenRes.Body.String())
	}

	invalidJSONReq := httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString("{"))
	invalidJSONReq = invalidJSONReq.WithContext(auth.WithUser(invalidJSONReq.Context(), admin))
	invalidJSONRes := httptest.NewRecorder()
	authHandler.CreateEmailTarget(invalidJSONRes, invalidJSONReq)
	if invalidJSONRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid JSON status %d, got %d body=%s", http.StatusBadRequest, invalidJSONRes.Code, invalidJSONRes.Body.String())
	}

	errorCases := []struct {
		name       string
		call       func(*NotificationHandler, *httptest.ResponseRecorder, *http.Request)
		err        error
		statusCode int
	}{
		{name: "list targets internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListTargets(res, req)
		}},
		{name: "create target duplicate", err: repository.ErrNotificationTargetDuplicate, statusCode: http.StatusConflict, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.CreateEmailTarget(res, req)
		}},
		{name: "update target not found", err: repository.ErrNotificationTargetNotFound, statusCode: http.StatusNotFound, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateTarget(res, req)
		}},
		{name: "list subscriptions internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListSubscriptions(res, req)
		}},
		{name: "create subscription invalid", err: service.ErrNotificationSubscriptionEventTypeInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.CreateSubscription(res, req)
		}},
		{name: "update target invalid id", err: service.ErrNotificationTargetIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateTarget(res, req)
		}},
		{name: "list subscriptions invalid filter", err: service.ErrNotificationSubscriptionProjectIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.ListSubscriptions(res, req)
		}},
		{name: "update subscription not found", err: repository.ErrNotificationSubscriptionNotFound, statusCode: http.StatusNotFound, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.UpdateSubscription(res, req)
		}},
		{name: "delete subscription invalid id", err: service.ErrNotificationSubscriptionIDInvalid, statusCode: http.StatusBadRequest, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.DeleteSubscription(res, req)
		}},
		{name: "delete subscription internal", err: errors.New("boom"), statusCode: http.StatusInternalServerError, call: func(h *NotificationHandler, res *httptest.ResponseRecorder, req *http.Request) {
			h.DeleteSubscription(res, req)
		}},
	}

	for _, testCase := range errorCases {
		t.Run(testCase.name, func(t *testing.T) {
			adminService := &fakeNotificationAdminService{}
			switch testCase.name {
			case "list targets internal":
				adminService.listTargetsErr = testCase.err
			case "create target duplicate":
				adminService.createTargetErr = testCase.err
			case "update target not found":
				adminService.updateTargetErr = testCase.err
			case "list subscriptions internal":
				adminService.listSubscriptionsErr = testCase.err
			case "create subscription invalid":
				adminService.createSubscriptionErr = testCase.err
			case "update target invalid id":
				adminService.updateTargetErr = testCase.err
			case "list subscriptions invalid filter":
				adminService.listSubscriptionsErr = testCase.err
			case "update subscription not found":
				adminService.updateSubscriptionErr = testCase.err
			case "delete subscription invalid id":
				adminService.deleteSubscriptionErr = testCase.err
			case "delete subscription internal":
				adminService.deleteSubscriptionErr = testCase.err
			}

			h := NewNotificationHandler(nil)
			h.SetAdminService(adminService)
			h.SetAuthorization(auth.ModeHeader)

			var req *http.Request
			switch testCase.name {
			case "list targets internal":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-targets", nil)
			case "create target duplicate":
				req = httptest.NewRequest(http.MethodPost, "/api/notification-targets", bytes.NewBufferString(`{"name":"Alerts","address":"dev@example.com"}`))
			case "update target not found":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/missing", bytes.NewBufferString(`{"enabled":true}`)), "targetID", "missing")
			case "list subscriptions internal":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions", nil)
			case "create subscription invalid":
				req = httptest.NewRequest(http.MethodPost, "/api/notification-subscriptions", bytes.NewBufferString(`{"target_id":"target-1","project_id":"project-1","event_type":"invalid"}`))
			case "update target invalid id":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-targets/not-a-uuid", bytes.NewBufferString(`{"enabled":true}`)), "targetID", "not-a-uuid")
			case "list subscriptions invalid filter":
				req = httptest.NewRequest(http.MethodGet, "/api/notification-subscriptions?project_id=not-a-uuid", nil)
			case "update subscription not found":
				req = addURLParam(httptest.NewRequest(http.MethodPatch, "/api/notification-subscriptions/missing", bytes.NewBufferString(`{"enabled":true}`)), "subscriptionID", "missing")
			case "delete subscription invalid id":
				req = addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/not-a-uuid", nil), "subscriptionID", "not-a-uuid")
			case "delete subscription internal":
				req = addURLParam(httptest.NewRequest(http.MethodDelete, "/api/notification-subscriptions/missing", nil), "subscriptionID", "missing")
			}
			req = req.WithContext(auth.WithUser(req.Context(), admin))
			res := httptest.NewRecorder()
			testCase.call(h, res, req)
			if res.Code != testCase.statusCode {
				t.Fatalf("expected status %d, got %d body=%s", testCase.statusCode, res.Code, res.Body.String())
			}
		})
	}
}
