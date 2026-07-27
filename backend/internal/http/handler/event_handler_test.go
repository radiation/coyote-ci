package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
)

func TestEventHandler_IngestPushEvent(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	webhookSvc := webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc)
	h := NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), testGitHubWebhookResolver{})

	_, err := jobSvc.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/events/push", bytes.NewBufferString(`{"repository_url":"https://github.com/example/backend.git","ref":"refs/heads/main","commit_sha":"abc123"}`))
	res := httptest.NewRecorder()
	h.IngestPushEvent(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	data := decodeDataMap(t, res)
	if data["matched_jobs"] != float64(1) {
		t.Fatalf("expected matched_jobs=1, got %v", data["matched_jobs"])
	}
	if data["created_builds"] != float64(1) {
		t.Fatalf("expected created_builds=1, got %v", data["created_builds"])
	}
	if data["ref"] != "main" {
		t.Fatalf("expected normalized ref main, got %v", data["ref"])
	}
}

func TestEventHandler_IngestPushEvent_BadRequest(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	jobSvc := service.NewJobService(jobRepo, buildsvc.NewBuildService(buildRepo, nil, nil))
	h := NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), testGitHubWebhookResolver{})

	req := httptest.NewRequest(http.MethodPost, "/events/push", bytes.NewBufferString(`{"repository_url":"","ref":"","commit_sha":""}`))
	res := httptest.NewRecorder()
	h.IngestPushEvent(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}

func TestEventHandler_IngestGitHubWebhook_IdempotentDuplicateNoSecondBuild(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, metrics, "secret")

	_, err := jobSvc.CreateJob(context.Background(), service.CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"installation":{"id":999},
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)
	sig := githubTestSignature("secret", body)

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", "delivery-1")
		req.Header.Set("X-Hub-Signature-256", sig)

		res := httptest.NewRecorder()
		ingestTestGitHubWebhook(h, res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected status %d on attempt %d, got %d body=%s", http.StatusOK, i+1, res.Code, res.Body.String())
		}
		if i == 1 {
			data := decodeDataMap(t, res)
			if duplicate, _ := data["duplicate"].(bool); !duplicate {
				t.Fatalf("expected duplicate=true on second delivery, got %v", data["duplicate"])
			}
		}
	}

	builds, err := buildRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list builds failed: %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("expected one initial build plus one queued webhook build for duplicate deliveries, got %d", len(builds))
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-1")
	if err != nil {
		t.Fatalf("expected delivery ledger record, got err=%v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusDuplicate {
		t.Fatalf("expected duplicate status after replay, got %q", delivery.Status)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeDuplicate); got != 1 {
		t.Fatalf("expected duplicate metric count 1, got %d", got)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeDeliveriesVerified); got != 1 {
		t.Fatalf("expected deliveries_verified metric count 1, got %d", got)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeBuildQueued); got != 1 {
		t.Fatalf("expected build_queued metric count 1, got %d", got)
	}
	if got := metrics.DurationSampleCount("github", "push", observability.WebhookOutcomeDuplicate); got != 1 {
		t.Fatalf("expected one duplicate duration sample, got %d", got)
	}
}

func TestEventHandler_IngestGitHubWebhook_UnsupportedEventRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "secret")

	body := []byte(`{"installation":{"id":999}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-unsupported")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, res.Code)
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-unsupported")
	if err != nil {
		t.Fatalf("expected ledger record, got %v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusUnsupported {
		t.Fatalf("expected unsupported status, got %q", delivery.Status)
	}
}

func TestEventHandler_IngestGitHubWebhook_UnsupportedEventRequiresConnectionIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		body         []byte
		resolution   webhooksvc.GitHubWebhookConnectionResolution
		wantStatus   int
		wantDelivery bool
	}{
		{name: "missing installation", body: []byte(`{}`), wantStatus: http.StatusBadRequest},
		{name: "fractional installation", body: []byte(`{"installation":{"id":1.5}}`), wantStatus: http.StatusBadRequest},
		{name: "unknown installation", body: []byte(`{"installation":{"id":999}}`), wantStatus: http.StatusOK},
		{name: "disabled connection", body: []byte(`{"installation":{"id":999}}`), resolution: webhooksvc.GitHubWebhookConnectionResolution{ConnectionID: "connection-1", Found: true}, wantStatus: http.StatusOK},
		{name: "enabled connection", body: []byte(`{"installation":{"id":999}}`), resolution: webhooksvc.GitHubWebhookConnectionResolution{ConnectionID: "connection-1", Found: true, Enabled: true}, wantStatus: http.StatusAccepted, wantDelivery: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
			jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
			h := NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc), observability.NewNoopWebhookIngressMetrics(), testGitHubWebhookResolver{secret: "secret", resolution: testCase.resolution})
			req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(testCase.body))
			req.Header.Set("X-GitHub-Event", "pull_request")
			req.Header.Set("X-GitHub-Delivery", "delivery-unsupported-identity")
			req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", testCase.body))
			res := httptest.NewRecorder()
			ingestTestGitHubWebhook(h, res, req)
			if res.Code != testCase.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", testCase.wantStatus, res.Code, res.Body.String())
			}
			_, deliveryErr := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-unsupported-identity")
			if testCase.wantDelivery && deliveryErr != nil {
				t.Fatalf("expected delivery to be recorded, got %v", deliveryErr)
			}
			if !testCase.wantDelivery && deliveryErr == nil {
				t.Fatal("invalid or unresolved connection identity must not claim a delivery")
			}
		})
	}
}

func TestEventHandler_IngestGitHubWebhook_UnsupportedDuplicateReturnsOK(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	h := newTestGitHubEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc), observability.NewNoopWebhookIngressMetrics(), "secret")
	body := []byte(`{"installation":{"id":999}}`)
	for attempt := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-GitHub-Delivery", "delivery-unsupported-duplicate")
		req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
		res := httptest.NewRecorder()
		ingestTestGitHubWebhook(h, res, req)
		wantStatus := http.StatusAccepted
		if attempt == 1 {
			wantStatus = http.StatusOK
		}
		if res.Code != wantStatus {
			t.Fatalf("attempt %d: expected status %d, got %d body=%s", attempt+1, wantStatus, res.Code, res.Body.String())
		}
	}
}

func TestEventHandler_IngestGitHubWebhook_NoMatchRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, metrics, "secret")

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"installation":{"id":999},
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-no-match")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-no-match")
	if err != nil {
		t.Fatalf("expected ledger record, got %v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusIgnoredNoMatch {
		t.Fatalf("expected ignored_no_match status, got %q", delivery.Status)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeNoMatchingJob); got != 1 {
		t.Fatalf("expected no_matching_job metric count 1, got %d", got)
	}
	if got := metrics.DurationSampleCount("github", "push", observability.WebhookOutcomeNoMatchingJob); got != 1 {
		t.Fatalf("expected one no_matching_job duration sample, got %d", got)
	}
}

func TestEventHandler_IngestGitHubWebhook_DeletePushIgnored(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "secret")

	body := []byte(`{
		"ref":"refs/heads/main",
		"deleted":true,
		"after":"",
		"installation":{"id":999},
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-delete")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-delete")
	if err != nil {
		t.Fatalf("expected ledger record, got %v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusIgnoredNoMatch {
		t.Fatalf("expected ignored_no_match status, got %q", delivery.Status)
	}
	if delivery.Reason == nil || *delivery.Reason != string(webhooksvc.WebhookFilterDecisionDeletedRef) {
		t.Fatalf("expected reason deleted_ref, got %v", delivery.Reason)
	}
}

func TestEventHandler_IngestGitHubWebhook_FailedProcessingRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), nil)
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "secret")

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"installation":{"id":999},
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-failed")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, res.Code)
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-failed")
	if err != nil {
		t.Fatalf("expected ledger record, got %v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusFailed {
		t.Fatalf("expected failed status, got %q", delivery.Status)
	}
}

func TestEventHandler_IngestGitHubWebhook_InvalidSignatureRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := newTestGitHubEventHandler(jobSvc, webhookSvc, metrics, "secret")

	body := validTestGitHubPushBody()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-invalid-signature")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")

	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}

	_, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-invalid-signature")
	if err == nil {
		t.Fatal("invalid signature must not reserve a delivery ID")
	}

	validReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	validReq.Header.Set("X-GitHub-Event", "push")
	validReq.Header.Set("X-GitHub-Delivery", "delivery-invalid-signature")
	validReq.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
	validRes := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, validRes, validReq)
	if validRes.Code != http.StatusOK {
		t.Fatalf("expected later valid delivery status %d, got %d body=%s", http.StatusOK, validRes.Code, validRes.Body.String())
	}
	if _, validErr := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-invalid-signature"); validErr != nil {
		t.Fatalf("expected later valid delivery to be recorded, got %v", validErr)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeInvalidSignature); got != 1 {
		t.Fatalf("expected invalid_signature metric count 1, got %d", got)
	}
	if got := metrics.DurationSampleCount("github", "push", observability.WebhookOutcomeInvalidSignature); got != 1 {
		t.Fatalf("expected one invalid_signature duration sample, got %d", got)
	}
}

func TestEventHandler_IngestGitHubWebhook_RegistrationFailuresDoNotClaimDelivery(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		resolver   testGitHubWebhookResolver
		wantStatus int
	}{
		{name: "unknown registration", resolver: testGitHubWebhookResolver{registrationErr: webhooksvc.ErrGitHubWebhookRegistrationNotFound}, wantStatus: http.StatusNotFound},
		{name: "secret unavailable", resolver: testGitHubWebhookResolver{registrationErr: webhooksvc.ErrGitHubWebhookSecretUnavailable}, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
			jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
			h := NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc), observability.NewNoopWebhookIngressMetrics(), testCase.resolver)
			body := validTestGitHubPushBody()
			req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
			req.Header.Set("X-GitHub-Event", "push")
			req.Header.Set("X-GitHub-Delivery", "delivery-registration-failure")
			req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
			res := httptest.NewRecorder()
			ingestTestGitHubWebhook(h, res, req)
			if res.Code != testCase.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", testCase.wantStatus, res.Code, res.Body.String())
			}
			if _, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-registration-failure"); err == nil {
				t.Fatal("registration failure must not claim a delivery")
			}
		})
	}
}

func TestEventHandler_IngestGitHubWebhook_InvalidIdentityDoesNotClaimDelivery(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	h := newTestGitHubEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc), observability.NewNoopWebhookIngressMetrics(), "secret")
	body := []byte(`{"ref":"refs/heads/main","after":"abc123","repository":{"name":"backend","html_url":"https://github.com/example/backend","owner":{"login":"example"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-invalid-installation")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusBadRequest, res.Code, res.Body.String())
	}
	if _, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-invalid-installation"); err == nil {
		t.Fatal("malformed identity must not claim a delivery")
	}
}

func TestEventHandler_IngestGitHubWebhook_UnknownAndDisabledConnectionsAreNoOps(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		resolution webhooksvc.GitHubWebhookConnectionResolution
	}{
		{name: "unknown installation", resolution: webhooksvc.GitHubWebhookConnectionResolution{}},
		{name: "disabled connection", resolution: webhooksvc.GitHubWebhookConnectionResolution{ConnectionID: "connection-1", Found: true, Enabled: false}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
			jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), buildsvc.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
			h := NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(deliveryRepo, jobSvc), observability.NewNoopWebhookIngressMetrics(), testGitHubWebhookResolver{secret: "secret", resolution: testCase.resolution})
			body := validTestGitHubPushBody()
			req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
			req.Header.Set("X-GitHub-Event", "push")
			req.Header.Set("X-GitHub-Delivery", "delivery-noop-"+testCase.name)
			req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
			res := httptest.NewRecorder()
			ingestTestGitHubWebhook(h, res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
			}
		})
	}
}

func TestEventHandler_IngestGitHubWebhook_PropagatesConnectionID(t *testing.T) {
	triggerer := &recordingHandlerWebhookTriggerer{}
	deliverySvc := webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), triggerer)
	h := NewEventHandler(nil, deliverySvc, observability.NewNoopWebhookIngressMetrics(), testGitHubWebhookResolver{secret: "secret", resolution: webhooksvc.GitHubWebhookConnectionResolution{ConnectionID: "connection-1", Found: true, Enabled: true}})
	body := validTestGitHubPushBody()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/apps/registration-1", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-connection-id")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))
	res := httptest.NewRecorder()
	ingestTestGitHubWebhook(h, res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	if triggerer.input.ConnectionID != "connection-1" || triggerer.input.InstallationID != "999" {
		t.Fatalf("expected propagated connection identity, got %+v", triggerer.input)
	}
}

func githubTestSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestGitHubEventHandler(jobSvc *service.JobService, deliverySvc *webhooksvc.DeliveryIngressService, metrics observability.WebhookIngressMetrics, secret string) *EventHandler {
	return NewEventHandler(jobSvc, deliverySvc, metrics, testGitHubWebhookResolver{secret: secret, resolution: webhooksvc.GitHubWebhookConnectionResolution{ConnectionID: "connection-1", Found: true, Enabled: true}})
}

func ingestTestGitHubWebhook(handler *EventHandler, recorder *httptest.ResponseRecorder, request *http.Request) {
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("registrationID", "registration-1")
	handler.IngestGitHubWebhook(recorder, request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)))
}

type testGitHubWebhookResolver struct {
	secret          string
	resolution      webhooksvc.GitHubWebhookConnectionResolution
	registrationErr error
	connectionErr   error
}

func (r testGitHubWebhookResolver) ResolveRegistrationSecret(_ context.Context, registrationID string) (string, error) {
	if r.registrationErr != nil {
		return "", r.registrationErr
	}
	if registrationID != "registration-1" {
		return "", webhooksvc.ErrGitHubWebhookRegistrationNotFound
	}
	return r.secret, nil
}

func (r testGitHubWebhookResolver) ResolveConnection(_ context.Context, registrationID string, installationID string) (webhooksvc.GitHubWebhookConnectionResolution, error) {
	if r.connectionErr != nil {
		return webhooksvc.GitHubWebhookConnectionResolution{}, r.connectionErr
	}
	if registrationID != "registration-1" || installationID != "999" {
		return webhooksvc.GitHubWebhookConnectionResolution{}, nil
	}
	return r.resolution, nil
}

func validTestGitHubPushBody() []byte {
	return []byte(`{"ref":"refs/heads/main","after":"abc123","installation":{"id":999},"repository":{"name":"backend","html_url":"https://github.com/example/backend","owner":{"login":"example"}},"sender":{"login":"octocat"}}`)
}

type recordingHandlerWebhookTriggerer struct {
	input webhooksvc.WebhookTriggerInput
}

func (t *recordingHandlerWebhookTriggerer) TriggerWebhookEvent(_ context.Context, input webhooksvc.WebhookTriggerInput) (webhooksvc.WebhookTriggerResult, error) {
	t.input = input
	return webhooksvc.WebhookTriggerResult{}, nil
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }
