package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

// GetBuildSteps godoc
// @Summary Get build steps
// @Description Returns steps for a build.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildStepsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/steps [get]
func (h *BuildHandler) GetBuildSteps(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, id); !ok {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
		return
	}

	steps, err := h.buildService.GetBuildSteps(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	jobs, err := h.buildService.GetJobsByBuildID(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	outputs, err := h.buildService.GetJobOutputsByBuildID(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	jobByStepID := map[string]domain.ExecutionJob{}
	for _, job := range jobs {
		jobByStepID[job.StepID] = job
	}
	outputsByJobID := map[string][]domain.ExecutionJobOutput{}
	for _, output := range outputs {
		outputsByJobID[output.JobID] = append(outputsByJobID[output.JobID], output)
	}

	respSteps := make([]api.BuildStepResponse, 0, len(steps))
	for _, step := range steps {
		linkedJob, hasJob := jobByStepID[step.ID]
		if hasJob {
			respSteps = append(respSteps, toBuildStepResponse(step, &linkedJob, outputsByJobID[linkedJob.ID]))
			continue
		}
		respSteps = append(respSteps, toBuildStepResponse(step, nil, nil))
	}

	sort.Slice(respSteps, func(i, j int) bool {
		return respSteps[i].StepIndex < respSteps[j].StepIndex
	})

	writeDataJSON(w, http.StatusOK, api.BuildStepsResponse{
		BuildID: id,
		Steps:   respSteps,
	})
}

// GetBuildLogs godoc
// @Summary Get build logs
// @Description Returns log lines for a build.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildLogsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/logs [get]
func (h *BuildHandler) GetBuildLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, id); !ok {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildLogs) {
		return
	}

	logs, err := h.buildService.GetBuildLogs(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	respLogs := make([]api.BuildLogResponse, 0, len(logs))
	for _, logLine := range logs {
		respLogs = append(respLogs, api.BuildLogResponse{
			StepName:  logLine.StepName,
			Timestamp: logLine.Timestamp.Format(time.RFC3339),
			Message:   logLine.Message,
		})
	}

	writeDataJSON(w, http.StatusOK, api.BuildLogsResponse{
		BuildID: id,
		Logs:    respLogs,
	})
}

// QueueBuild godoc
// @Summary Queue build
// @Description Transitions build status from pending to queued.
// @Tags builds
// @Accept json
// @Produce json
// @Param buildID path string true "Build ID"
// @Param request body api.QueueBuildRequest false "Queue build request"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/queue [post]
func (h *BuildHandler) QueueBuild(w http.ResponseWriter, r *http.Request) {
	var req api.QueueBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	customSteps := make([]buildsvc.QueueBuildCustomStepInput, 0, len(req.Steps))
	for _, step := range req.Steps {
		customSteps = append(customSteps, buildsvc.QueueBuildCustomStepInput{
			Name:    step.Name,
			Command: step.Command,
		})
	}

	h.transitionBuild(w, r, auth.CanTriggerBuild, func(ctx context.Context, id string) (domain.Build, error) {
		return h.buildService.QueueBuildWithTemplateAndCustomSteps(ctx, id, req.Template, customSteps)
	})
}

// StartBuild godoc
// @Summary Start build
// @Description Transitions build status from queued to running.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/start [post]
func (h *BuildHandler) StartBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, auth.CanTriggerBuild, h.buildService.StartBuild)
}

// CompleteBuild godoc
// @Summary Complete build
// @Description Transitions build status from running to success.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/complete [post]
func (h *BuildHandler) CompleteBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, auth.CanTriggerBuild, h.buildService.CompleteBuild)
}

// FailBuild godoc
// @Summary Fail build
// @Description Transitions build status from running to failed.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/fail [post]
func (h *BuildHandler) FailBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, auth.CanTriggerBuild, h.buildService.FailBuild)
}

// CancelBuild godoc
// @Summary Cancel build
// @Description Marks a queued or running build as canceled and terminalizes cancellable steps/jobs.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/cancel [post]
func (h *BuildHandler) CancelBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, auth.CanCancelBuild, h.buildService.CancelBuild)
}

func (h *BuildHandler) transitionBuild(w http.ResponseWriter, r *http.Request, check projectAuthorizer, transition func(ctx context.Context, id string) (domain.Build, error)) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildAction(w, r, id, check); !ok {
		return
	}

	build, err := transition(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toBuildResponse(build))
}

// RetryJob godoc
// @Summary Retry failed execution job
// @Description Creates a new build attempt containing a retry of the failed execution job.
// @Tags builds
// @Produce json
// @Param jobID path string true "Execution Job ID"
// @Success 200 {object} api.RetryJobEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/jobs/{jobID}/retry [post]
func (h *BuildHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}

	job, err := h.buildService.GetJobByID(r.Context(), jobID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if _, ok := h.authorizeBuildAction(w, r, job.BuildID, auth.CanTriggerBuild); !ok {
		return
	}

	retryResult, err := h.buildService.RetryJob(r.Context(), jobID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	outputs, err := h.buildService.GetJobOutputsByJobID(r.Context(), retryResult.Job.ID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	jobResponse := toExecutionJobResponse(&retryResult.Job, outputs)
	if jobResponse == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeDataJSON(w, http.StatusOK, api.RetryJobResponse{
		Build: toBuildResponse(retryResult.Build),
		Job:   *jobResponse,
	})
}

// RerunBuild godoc
// @Summary Rerun build
// @Description Creates a new queued build using the source, trigger, job, and step context from an existing build.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/rerun [post]
func (h *BuildHandler) RerunBuild(w http.ResponseWriter, r *http.Request) {
	buildID := strings.TrimSpace(chi.URLParam(r, "buildID"))
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}

	if _, ok := h.authorizeBuildAction(w, r, buildID, auth.CanTriggerBuild); !ok {
		return
	}

	build, err := h.buildService.RerunBuild(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toBuildResponse(build))
}
