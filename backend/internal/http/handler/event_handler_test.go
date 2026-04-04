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

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestEventHandler_IngestPushEvent(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := service.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	webhookSvc := service.NewWebhookIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc)
	h := NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "")

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
	jobSvc := service.NewJobService(jobRepo, service.NewBuildService(buildRepo, nil, nil))
	h := NewEventHandler(jobSvc, service.NewWebhookIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")

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
	buildSvc := service.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	webhookSvc := service.NewWebhookIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := NewEventHandler(jobSvc, webhookSvc, metrics, "secret")

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
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)
	sig := githubTestSignature("secret", body)

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", "delivery-1")
		req.Header.Set("X-Hub-Signature-256", sig)

		res := httptest.NewRecorder()
		h.IngestGitHubWebhook(res, req)
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
	if len(builds) != 1 {
		t.Fatalf("expected exactly one queued build for duplicate deliveries, got %d", len(builds))
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
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := service.NewWebhookIngressService(deliveryRepo, jobSvc)
	h := NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "secret")

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-unsupported")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	h.IngestGitHubWebhook(res, req)
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

func TestEventHandler_IngestGitHubWebhook_NoMatchRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := service.NewWebhookIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := NewEventHandler(jobSvc, webhookSvc, metrics, "secret")

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-no-match")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	h.IngestGitHubWebhook(res, req)
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

func TestEventHandler_IngestGitHubWebhook_FailedProcessingRecorded(t *testing.T) {
	deliveryRepo := repositorymemory.NewWebhookDeliveryRepository()
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), nil)
	webhookSvc := service.NewWebhookIngressService(deliveryRepo, jobSvc)
	h := NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "secret")

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"octocat"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-failed")
	req.Header.Set("X-Hub-Signature-256", githubTestSignature("secret", body))

	res := httptest.NewRecorder()
	h.IngestGitHubWebhook(res, req)
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
	jobSvc := service.NewJobService(repositorymemory.NewJobRepository(), service.NewBuildService(repositorymemory.NewBuildRepository(), nil, nil))
	webhookSvc := service.NewWebhookIngressService(deliveryRepo, jobSvc)
	metrics := observability.NewInMemoryWebhookIngressMetrics()
	webhookSvc.SetMetrics(metrics)
	h := NewEventHandler(jobSvc, webhookSvc, metrics, "secret")

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewBufferString(`{"ref":"refs/heads/main"}`))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-invalid-signature")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")

	res := httptest.NewRecorder()
	h.IngestGitHubWebhook(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}

	delivery, err := deliveryRepo.GetByProviderDeliveryID(context.Background(), "github", "delivery-invalid-signature")
	if err != nil {
		t.Fatalf("expected ledger record, got %v", err)
	}
	if delivery.Status != domain.WebhookDeliveryStatusFailed {
		t.Fatalf("expected failed status, got %q", delivery.Status)
	}
	if got := metrics.OutcomeCount("github", "push", observability.WebhookOutcomeInvalidSignature); got != 1 {
		t.Fatalf("expected invalid_signature metric count 1, got %d", got)
	}
	if got := metrics.DurationSampleCount("github", "push", observability.WebhookOutcomeInvalidSignature); got != 1 {
		t.Fatalf("expected one invalid_signature duration sample, got %d", got)
	}
}

func githubTestSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }
