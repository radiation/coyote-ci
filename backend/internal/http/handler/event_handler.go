package handler

import (
	"encoding/json"
	"net/http"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type EventHandler struct {
	jobService *service.JobService
}

func NewEventHandler(jobService *service.JobService) *EventHandler {
	return &EventHandler{jobService: jobService}
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
