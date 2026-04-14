package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/service"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
	githubwebhook "github.com/radiation/coyote-ci/backend/internal/webhook/github"
)

type EventHandler struct {
	jobService          *service.JobService
	webhookService      *webhooksvc.DeliveryIngressService
	metrics             observability.WebhookIngressMetrics
	githubWebhookSecret string
}

func NewEventHandler(jobService *service.JobService, webhookService *webhooksvc.DeliveryIngressService, metrics observability.WebhookIngressMetrics, githubWebhookSecret string) *EventHandler {
	if metrics == nil {
		metrics = observability.NewNoopWebhookIngressMetrics()
	}
	return &EventHandler{jobService: jobService, webhookService: webhookService, metrics: metrics, githubWebhookSecret: githubWebhookSecret}
}

// IngestPushEvent godoc
// @Summary Ingest push event
// @Description Triggers builds for matching enabled jobs configured for push events.
// @Tags events
// @Accept json
// @Produce json
// @Param request body api.PushEventRequest true "Push event payload"
// @Success 200 {object} api.PushEventEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /events/push [post]
func (h *EventHandler) IngestPushEvent(w http.ResponseWriter, r *http.Request) {
	var req api.PushEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	result, err := h.jobService.TriggerPushEvent(r.Context(), service.PushEventInput{
		RepositoryURL: req.RepositoryURL,
		Ref:           req.Ref,
		CommitSHA:     req.CommitSHA,
	})
	if err != nil {
		if isBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	builds := make([]api.PushEventMatchedJob, 0, len(result.Builds))
	for _, item := range result.Builds {
		builds = append(builds, api.PushEventMatchedJob{
			JobID:       item.Job.ID,
			JobName:     item.Job.Name,
			BuildID:     item.Build.ID,
			BuildStatus: string(item.Build.Status),
		})
	}

	writeDataJSON(w, http.StatusOK, api.PushEventResponse{
		RepositoryURL: result.RepositoryURL,
		Ref:           result.Ref,
		CommitSHA:     result.CommitSHA,
		MatchedJobs:   result.MatchedJobs,
		CreatedBuilds: len(result.Builds),
		Builds:        builds,
	})
}

// IngestGitHubWebhook godoc
// @Summary Ingest GitHub webhook
// @Description Verifies a GitHub webhook signature and triggers builds for matching jobs on push events.
// @Tags webhooks
// @Produce json
// @Success 200 {object} api.PushEventEnvelope
// @Success 202 {object} api.PushEventEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 503 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /webhooks/github [post]
func (h *EventHandler) IngestGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	provider := "github"
	eventType := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GitHub-Event")))
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	logCtx := webhooksvc.NewWebhookLogContext(provider, deliveryID, eventType)
	ctx := webhooksvc.WithWebhookLogContext(r.Context(), logCtx)
	outcome := observability.WebhookOutcomeFailedProcessing
	defer func() {
		h.metrics.ObserveIngressDuration(provider, eventType, outcome, time.Since(startedAt))
	}()

	h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeDeliveriesReceived)
	log.Printf("INFO webhook received %s", webhooksvc.WebhookLogFields(ctx))

	if h.webhookService == nil {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "webhook service not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	delivery, duplicate, deliveryErr := h.webhookService.RegisterReceived(ctx, provider, deliveryID, eventType)
	if deliveryErr != nil {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		log.Printf("WARN webhook register failed %s err=%v", webhooksvc.WebhookLogFields(ctx), deliveryErr)
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", deliveryErr.Error())
		return
	}
	if duplicate {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeDuplicate)
		outcome = observability.WebhookOutcomeDuplicate
		log.Printf("INFO webhook duplicate detected %s", webhooksvc.WebhookLogFields(ctx))
		writeDataJSON(w, http.StatusOK, api.PushEventResponse{MatchedJobs: 0, CreatedBuilds: 0, Builds: []api.PushEventMatchedJob{}, Duplicate: true})
		return
	}

	if h.githubWebhookSecret == "" {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		_, _ = h.webhookService.MarkFailed(ctx, delivery, "github webhook secret not configured")
		log.Printf("ERROR webhook secret missing %s", webhooksvc.WebhookLogFields(ctx))
		writeErrorJSON(w, http.StatusServiceUnavailable, "misconfigured", "github webhook secret is not configured")
		return
	}

	if !githubwebhook.VerifySignature(h.githubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeInvalidSignature)
		outcome = observability.WebhookOutcomeInvalidSignature
		log.Printf("WARN webhook signature validation failed %s", webhooksvc.WebhookLogFields(ctx))
		_, _ = h.webhookService.MarkFailed(ctx, delivery, "signature validation failed")
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "invalid signature")
		return
	}

	pushEvent, parseErr := githubwebhook.ParsePushEvent(r.Header, body)
	if parseErr != nil {
		if errors.Is(parseErr, githubwebhook.ErrUnsupportedEvent) {
			h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeUnsupportedEvent)
			outcome = observability.WebhookOutcomeUnsupportedEvent
			log.Printf("INFO webhook unsupported event %s", webhooksvc.WebhookLogFields(ctx))
			_, _ = h.webhookService.MarkUnsupported(ctx, delivery, "unsupported event", webhooksvc.WebhookTriggerInput{SCMProvider: provider, EventType: eventType})
			writeDataJSON(w, http.StatusAccepted, api.PushEventResponse{MatchedJobs: 0, CreatedBuilds: 0, Builds: []api.PushEventMatchedJob{}})
			return
		}
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		_, _ = h.webhookService.MarkFailed(ctx, delivery, "invalid github webhook payload")
		log.Printf("WARN webhook payload parse failed %s err=%v", webhooksvc.WebhookLogFields(ctx), parseErr)
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid github webhook payload")
		return
	}

	eventType = strings.ToLower(strings.TrimSpace(pushEvent.EventType))
	ctx = webhooksvc.WithWebhookLogContext(ctx, webhooksvc.WebhookLogContext{
		CorrelationID: logCtx.CorrelationID,
		Provider:      provider,
		DeliveryID:    deliveryID,
		EventType:     eventType,
	})

	ingressResult, triggerErr := h.webhookService.ProcessVerifiedEvent(ctx, delivery, webhooksvc.WebhookTriggerInput{
		SCMProvider:     provider,
		EventType:       pushEvent.EventType,
		RepositoryOwner: pushEvent.RepositoryOwner,
		RepositoryName:  pushEvent.RepositoryName,
		RepositoryURL:   pushEvent.RepositoryURL,
		RawRef:          pushEvent.RawRef,
		Ref:             pushEvent.Ref,
		RefType:         pushEvent.RefType,
		RefName:         pushEvent.RefName,
		Deleted:         pushEvent.Deleted,
		CommitSHA:       pushEvent.CommitSHA,
		DeliveryID:      pushEvent.DeliveryID,
		Actor:           pushEvent.Actor,
	})
	if triggerErr != nil {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeFailedProcessing)
		log.Printf("ERROR webhook delivery failed %s err=%v", webhooksvc.WebhookLogFields(ctx), triggerErr)
		if isBadRequestError(triggerErr) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", triggerErr.Error())
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if ingressResult.Trigger.MatchedJobs == 0 {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeNoMatchingJob)
		outcome = observability.WebhookOutcomeNoMatchingJob
		log.Printf("INFO webhook processed no match %s", webhooksvc.WebhookLogFields(ctx))
	} else {
		h.metrics.IncOutcome(provider, eventType, observability.WebhookOutcomeBuildQueued)
		outcome = observability.WebhookOutcomeBuildQueued
		log.Printf("INFO webhook processed build queued %s matched_jobs=%d created_builds=%d", webhooksvc.WebhookLogFields(ctx), ingressResult.Trigger.MatchedJobs, len(ingressResult.Trigger.Builds))
	}

	builds := make([]api.PushEventMatchedJob, 0, len(ingressResult.Trigger.Builds))
	for _, item := range ingressResult.Trigger.Builds {
		builds = append(builds, api.PushEventMatchedJob{
			JobID:       item.Job.ID,
			JobName:     item.Job.Name,
			BuildID:     item.Build.ID,
			BuildStatus: string(item.Build.Status),
		})
	}

	writeDataJSON(w, http.StatusOK, api.PushEventResponse{
		RepositoryURL: ingressResult.Trigger.RepositoryURL,
		Ref:           ingressResult.Trigger.Ref,
		CommitSHA:     ingressResult.Trigger.CommitSHA,
		MatchedJobs:   ingressResult.Trigger.MatchedJobs,
		CreatedBuilds: len(ingressResult.Trigger.Builds),
		Builds:        builds,
	})
}
