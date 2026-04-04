package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type JobHandler struct {
	jobService *service.JobService
}

func NewJobHandler(jobService *service.JobService) *JobHandler {
	return &JobHandler{jobService: jobService}
}

// CreateJob godoc
// @Summary Create job
// @Description Creates a new job.
// @Tags jobs
// @Accept json
// @Produce json
// @Param request body api.CreateJobRequest true "Job create request"
// @Success 201 {object} api.JobEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs [post]
func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req api.CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	job, err := h.jobService.CreateJob(r.Context(), service.CreateJobInput{
		ProjectID:        req.ProjectID,
		Name:             req.Name,
		RepositoryURL:    req.RepositoryURL,
		DefaultRef:       req.DefaultRef,
		DefaultCommitSHA: req.DefaultCommitSHA,
		PushEnabled:      req.PushEnabled,
		PushBranch:       req.PushBranch,
		TriggerMode:      req.TriggerMode,
		BranchAllowlist:  req.BranchAllowlist,
		TagAllowlist:     req.TagAllowlist,
		PipelineYAML:     req.PipelineYAML,
		PipelinePath:     req.PipelinePath,
		Enabled:          req.Enabled,
	})
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toJobResponse(job))
}

// ListJobs godoc
// @Summary List jobs
// @Description Lists all jobs.
// @Tags jobs
// @Produce json
// @Success 200 {object} api.JobListEnvelope
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs [get]
func (h *JobHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.jobService.ListJobs(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.JobResponse, 0, len(jobs))
	for _, job := range jobs {
		responses = append(responses, toJobResponse(job))
	}

	writeDataJSON(w, http.StatusOK, api.JobListResponse{Jobs: responses})
}

// GetJob godoc
// @Summary Get job
// @Description Returns job details by id.
// @Tags jobs
// @Produce json
// @Param jobID path string true "Job ID"
// @Success 200 {object} api.JobEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID} [get]
func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}

	job, err := h.jobService.GetJob(r.Context(), jobID)
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toJobResponse(job))
}

// UpdateJob godoc
// @Summary Update job
// @Description Updates an existing job.
// @Tags jobs
// @Accept json
// @Produce json
// @Param jobID path string true "Job ID"
// @Param request body api.UpdateJobRequest true "Job update request"
// @Success 200 {object} api.JobEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID} [put]
func (h *JobHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}

	var req api.UpdateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	updated, err := h.jobService.UpdateJob(r.Context(), jobID, service.UpdateJobInput{
		Name:             req.Name,
		RepositoryURL:    req.RepositoryURL,
		DefaultRef:       req.DefaultRef,
		DefaultCommitSHA: req.DefaultCommitSHA,
		PushEnabled:      req.PushEnabled,
		PushBranch:       req.PushBranch,
		TriggerMode:      req.TriggerMode,
		BranchAllowlist:  req.BranchAllowlist,
		TagAllowlist:     req.TagAllowlist,
		PipelineYAML:     req.PipelineYAML,
		PipelinePath:     req.PipelinePath,
		Enabled:          req.Enabled,
	})
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toJobResponse(updated))
}

// RunNow godoc
// @Summary Run job now
// @Description Triggers an immediate build for a job.
// @Tags jobs
// @Produce json
// @Param jobID path string true "Job ID"
// @Success 201 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID}/run [post]
func (h *JobHandler) RunNow(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}

	build, err := h.jobService.RunJobNow(r.Context(), jobID)
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toBuildResponse(build))
}

// ListJobBuilds godoc
// @Summary List builds for a job
// @Description Returns builds triggered by a specific job, sorted newest first.
// @Tags jobs
// @Produce json
// @Param jobID path string true "Job ID"
// @Success 200 {object} api.BuildListEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID}/builds [get]
func (h *JobHandler) ListJobBuilds(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}

	builds, err := h.jobService.ListBuildsByJobID(r.Context(), jobID)
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	responses := make([]api.BuildResponse, 0, len(builds))
	for _, build := range builds {
		responses = append(responses, toBuildResponse(build))
	}

	writeDataJSON(w, http.StatusOK, api.BuildListResponse{Builds: responses})
}

func (h *JobHandler) writeJobServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrJobNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "job_not_found", "job not found")
		return
	}
	if errors.Is(err, service.ErrJobDisabled) {
		writeErrorJSON(w, http.StatusConflict, "job_disabled", err.Error())
		return
	}
	if isBadRequestError(err) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, ok := err.(pipeline.ValidationErrors); ok {
		writeErrorJSON(w, http.StatusBadRequest, "pipeline_validation", err.Error())
		return
	}
	if pe, ok := err.(*pipeline.ParseError); ok {
		writeErrorJSON(w, http.StatusBadRequest, "pipeline_parse", pe.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isBadRequestError(err error) bool {
	return errors.Is(err, service.ErrJobIDRequired) ||
		errors.Is(err, service.ErrJobNameRequired) ||
		errors.Is(err, service.ErrJobProjectIDRequired) ||
		errors.Is(err, service.ErrJobRepositoryURLRequired) ||
		errors.Is(err, service.ErrJobSourceTargetRequired) ||
		errors.Is(err, service.ErrJobInvalidTriggerMode) ||
		errors.Is(err, service.ErrJobPipelineDefinitionRequired) ||
		errors.Is(err, service.ErrPushEventRepositoryURLRequired) ||
		errors.Is(err, service.ErrPushEventRefRequired) ||
		errors.Is(err, service.ErrPushEventCommitSHARequired)
}

func toJobResponse(job domain.Job) api.JobResponse {
	triggerMode := string(job.TriggerMode)
	if strings.TrimSpace(triggerMode) == "" {
		triggerMode = string(domain.JobTriggerModeBranches)
	}

	return api.JobResponse{
		ID:               job.ID,
		ProjectID:        job.ProjectID,
		Name:             job.Name,
		RepositoryURL:    job.RepositoryURL,
		DefaultRef:       job.DefaultRef,
		DefaultCommitSHA: job.DefaultCommitSHA,
		PushEnabled:      job.PushEnabled,
		PushBranch:       job.PushBranch,
		TriggerMode:      triggerMode,
		BranchAllowlist:  job.BranchAllowlist,
		TagAllowlist:     job.TagAllowlist,
		PipelineYAML:     job.PipelineYAML,
		PipelinePath:     job.PipelinePath,
		Enabled:          job.Enabled,
		CreatedAt:        job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        job.UpdatedAt.Format(time.RFC3339),
	}
}
