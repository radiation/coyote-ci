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

func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req api.CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	job, err := h.jobService.CreateJob(r.Context(), service.CreateJobInput{
		ProjectID:     req.ProjectID,
		Name:          req.Name,
		RepositoryURL: req.RepositoryURL,
		DefaultRef:    req.DefaultRef,
		PipelineYAML:  req.PipelineYAML,
		Enabled:       req.Enabled,
	})
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toJobResponse(job))
}

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
		Name:          req.Name,
		RepositoryURL: req.RepositoryURL,
		DefaultRef:    req.DefaultRef,
		PipelineYAML:  req.PipelineYAML,
		Enabled:       req.Enabled,
	})
	if err != nil {
		h.writeJobServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toJobResponse(updated))
}

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

func (h *JobHandler) writeJobServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrJobNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "job_not_found", "job not found")
		return
	}
	if errors.Is(err, service.ErrJobDisabled) {
		writeErrorJSON(w, http.StatusConflict, "job_disabled", err.Error())
		return
	}
	if errors.Is(err, service.ErrJobIDRequired) || errors.Is(err, service.ErrJobNameRequired) || errors.Is(err, service.ErrJobProjectIDRequired) || errors.Is(err, service.ErrJobRepositoryURLRequired) || errors.Is(err, service.ErrJobDefaultRefRequired) || errors.Is(err, service.ErrJobPipelineYAMLRequired) {
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

func toJobResponse(job domain.Job) api.JobResponse {
	return api.JobResponse{
		ID:            job.ID,
		ProjectID:     job.ProjectID,
		Name:          job.Name,
		RepositoryURL: job.RepositoryURL,
		DefaultRef:    job.DefaultRef,
		PipelineYAML:  job.PipelineYAML,
		Enabled:       job.Enabled,
		CreatedAt:     job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     job.UpdatedAt.Format(time.RFC3339),
	}
}
