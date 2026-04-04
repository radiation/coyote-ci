package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/service"
	githubwebhook "github.com/radiation/coyote-ci/backend/internal/webhook/github"
)

type EventHandler struct {
	jobService          *service.JobService
	githubWebhookSecret string
}

func NewEventHandler(jobService *service.JobService, githubWebhookSecret string) *EventHandler {
	return &EventHandler{jobService: jobService, githubWebhookSecret: githubWebhookSecret}
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	log.Printf("INFO webhook received provider=github event_type=%s delivery_id=%s", eventType, deliveryID)

	if h.githubWebhookSecret == "" {
		writeErrorJSON(w, http.StatusServiceUnavailable, "misconfigured", "github webhook secret is not configured")
		return
	}

	if !githubwebhook.VerifySignature(h.githubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		log.Printf("WARN webhook signature validation failed provider=github event_type=%s delivery_id=%s", eventType, deliveryID)
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "invalid signature")
		return
	}

	pushEvent, parseErr := githubwebhook.ParsePushEvent(r.Header, body)
	if parseErr != nil {
		if errors.Is(parseErr, githubwebhook.ErrUnsupportedEvent) {
			log.Printf("INFO webhook unsupported event provider=github event_type=%s delivery_id=%s", eventType, deliveryID)
			writeDataJSON(w, http.StatusAccepted, api.PushEventResponse{MatchedJobs: 0, CreatedBuilds: 0, Builds: []api.PushEventMatchedJob{}})
			return
		}
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid github webhook payload")
		return
	}

	result, triggerErr := h.jobService.TriggerWebhookEvent(r.Context(), service.WebhookTriggerInput{
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
		if isBadRequestError(triggerErr) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", triggerErr.Error())
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
