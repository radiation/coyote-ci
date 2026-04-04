package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/service"
	githubwebhook "github.com/radiation/coyote-ci/backend/internal/webhook/github"
)

type EventHandler struct {
	jobService          *service.JobService
	webhookService      *service.WebhookIngressService
	githubWebhookSecret string
}

func NewEventHandler(jobService *service.JobService, webhookService *service.WebhookIngressService, githubWebhookSecret string) *EventHandler {
	return &EventHandler{jobService: jobService, webhookService: webhookService, githubWebhookSecret: githubWebhookSecret}
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
// @Router /api/webhooks/github [post]
func (h *EventHandler) IngestGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookService == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "webhook service not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	eventType := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GitHub-Event")))
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	log.Printf("INFO webhook received provider=github event_type=%s delivery_id=%s", eventType, deliveryID)

	delivery, duplicate, deliveryErr := h.webhookService.RegisterReceived(r.Context(), "github", deliveryID, eventType)
	if deliveryErr != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", deliveryErr.Error())
		return
	}
	if duplicate {
		log.Printf("INFO webhook duplicate detected provider=github delivery_id=%s", deliveryID)
		writeDataJSON(w, http.StatusOK, api.PushEventResponse{MatchedJobs: 0, CreatedBuilds: 0, Builds: []api.PushEventMatchedJob{}, Duplicate: true})
		return
	}

	if h.githubWebhookSecret == "" {
		_, _ = h.webhookService.MarkFailed(r.Context(), delivery, "github webhook secret not configured")
		writeErrorJSON(w, http.StatusServiceUnavailable, "misconfigured", "github webhook secret is not configured")
		return
	}

	if !githubwebhook.VerifySignature(h.githubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		log.Printf("WARN webhook signature validation failed provider=github event_type=%s delivery_id=%s", eventType, deliveryID)
		_, _ = h.webhookService.MarkFailed(r.Context(), delivery, "signature validation failed")
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "invalid signature")
		return
	}

	pushEvent, parseErr := githubwebhook.ParsePushEvent(r.Header, body)
	if parseErr != nil {
		if errors.Is(parseErr, githubwebhook.ErrUnsupportedEvent) {
			log.Printf("INFO webhook unsupported event provider=github event_type=%s delivery_id=%s", eventType, deliveryID)
			_, _ = h.webhookService.MarkUnsupported(r.Context(), delivery, "unsupported event", service.WebhookTriggerInput{SCMProvider: "github", EventType: eventType})
			writeDataJSON(w, http.StatusAccepted, api.PushEventResponse{MatchedJobs: 0, CreatedBuilds: 0, Builds: []api.PushEventMatchedJob{}})
			return
		}
		_, _ = h.webhookService.MarkFailed(r.Context(), delivery, "invalid github webhook payload")
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid github webhook payload")
		return
	}

	ingressResult, triggerErr := h.webhookService.ProcessVerifiedEvent(r.Context(), delivery, service.WebhookTriggerInput{
		SCMProvider:     "github",
		EventType:       pushEvent.EventType,
		RepositoryOwner: pushEvent.RepositoryOwner,
		RepositoryName:  pushEvent.RepositoryName,
		RepositoryURL:   pushEvent.RepositoryURL,
		Ref:             pushEvent.Ref,
		RefType:         pushEvent.RefType,
		CommitSHA:       pushEvent.CommitSHA,
		DeliveryID:      pushEvent.DeliveryID,
		Actor:           pushEvent.Actor,
	})
	if triggerErr != nil {
		log.Printf("ERROR webhook delivery failed provider=github delivery_id=%s err=%v", deliveryID, triggerErr)
		if isBadRequestError(triggerErr) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", triggerErr.Error())
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
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
