package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type JobHandler struct {
	jobService   *service.JobService
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

func NewJobHandler(jobService *service.JobService) *JobHandler {
	return &JobHandler{jobService: jobService}
}

func (h *JobHandler) SetAuthorization(mode auth.Mode, projectRoles auth.ProjectRoleLookup) {
	h.authMode = mode
	h.projectRoles = projectRoles
}

// CreateJob godoc
// @Summary Create job
// @Description Creates a new job and queues an initial build by default.
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
	projectID, projectErr := h.jobService.ResolveProjectID(r.Context(), req.ProjectID, req.ProjectSlug)
	if projectErr != nil {
		h.writeJobServiceError(w, projectErr)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanManageProjectJobs, "project owner or maintainer is required") {
		return
	}

	job, err := h.jobService.CreateJob(r.Context(), service.CreateJobInput{
		ProjectID:        projectID,
		Name:             req.Name,
		Priority:         req.Priority,
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
		ManagedImage:     toCreateManagedImageConfigInput(req.ManagedImage),
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
// @Description Lists jobs with optional pagination.
// @Tags jobs
// @Produce json
// @Param limit query int false "Max results (default 50, max 200)"
// @Param offset query int false "Number of results to skip"
// @Success 200 {object} api.JobListEnvelope
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs [get]
func (h *JobHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
		return
	}
	limit := parseQueryInt(r, "limit", 0)
	offset := parseQueryInt(r, "offset", 0)

	jobs, err := h.jobService.ListJobsPaged(r.Context(), repository.ListParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	jobs, err = h.filterJobsForRead(r.Context(), jobs)
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
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
		return
	}
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
	if !authorizeProject(w, r, h.authMode, h.projectRoles, job.ProjectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}

	writeDataJSON(w, http.StatusOK, toJobResponse(job))
}

// ResolveJob godoc
// @Summary Resolve job by project and name
// @Description Returns job details by resolving an exact job name within a project.
// @Tags jobs
// @Produce json
// @Param project query string true "Project ID or slug"
// @Param name query string true "Exact job name"
// @Success 200 {object} api.JobEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/resolve [get]
func (h *JobHandler) ResolveJob(w http.ResponseWriter, r *http.Request) {
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
		return
	}
	projectSelector := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectSelector == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "project query parameter is required")
		return
	}
	jobName := strings.TrimSpace(r.URL.Query().Get("name"))
	if jobName == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "name query parameter is required")
		return
	}

	projectID, err := h.jobService.ResolveProjectID(r.Context(), projectSelector, "")
	if err != nil {
		if errors.Is(err, repository.ErrProjectNotFound) {
			writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		h.writeJobServiceError(w, err)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}

	job, err := h.resolveJobSelectorWithinProject(r.Context(), jobName, projectID)
	if err != nil {
		if errors.Is(err, service.ErrJobNameAmbiguous) {
			writeErrorJSON(w, http.StatusConflict, "ambiguous_selector", err.Error())
			return
		}
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
	current, currentErr := h.jobService.GetJob(r.Context(), jobID)
	if currentErr != nil {
		h.writeJobServiceError(w, currentErr)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, current.ProjectID, auth.CanManageProjectJobs, "project owner or maintainer is required") {
		return
	}

	updated, err := h.jobService.UpdateJob(r.Context(), jobID, service.UpdateJobInput{
		Name:             req.Name,
		Priority:         req.Priority,
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
		ManagedImageSet:  req.ManagedImagePresent(),
		ManagedImage:     toUpdateManagedImageConfigInput(req.ManagedImage),
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
// @Accept json
// @Produce json
// @Param jobID path string true "Job ID"
// @Param request body api.RunJobRequest false "Optional run request"
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
	var req api.RunJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	job, jobErr := h.jobService.GetJob(r.Context(), jobID)
	if jobErr != nil {
		h.writeJobServiceError(w, jobErr)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, job.ProjectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRun) {
		return
	}

	build, err := h.jobService.RunJobNow(r.Context(), jobID, req.Ref)
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
	job, jobErr := h.jobService.GetJob(r.Context(), jobID)
	if jobErr != nil {
		h.writeJobServiceError(w, jobErr)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, job.ProjectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
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

func (h *JobHandler) resolveJobSelectorWithinProject(ctx context.Context, selector string, projectID string) (domain.Job, error) {
	trimmedSelector := strings.TrimSpace(selector)
	if _, err := uuid.Parse(trimmedSelector); err == nil {
		job, getErr := h.jobService.GetJob(ctx, trimmedSelector)
		if getErr == nil {
			if job.ProjectID != projectID {
				return domain.Job{}, service.ErrJobNotFound
			}
			return job, nil
		}
		if !errors.Is(getErr, service.ErrJobNotFound) {
			return domain.Job{}, getErr
		}
	}
	return h.jobService.ResolveJobByProjectAndName(ctx, projectID, trimmedSelector)
}

func (h *JobHandler) filterJobsForRead(ctx context.Context, jobs []domain.Job) ([]domain.Job, error) {
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return jobs, nil
	}
	projectIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		projectIDs = append(projectIDs, job.ProjectID)
	}
	allowedProjects, err := allowedProjectsForUser(ctx, h.authMode, h.projectRoles, projectIDs, auth.CanReadProjectResources)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.Job, 0, len(jobs))
	for _, job := range jobs {
		if _, ok := allowedProjects[job.ProjectID]; ok {
			filtered = append(filtered, job)
		}
	}
	return filtered, nil
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
	if errors.Is(err, service.ErrJobBuildServiceNotConfigured) {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "build service not configured")
		return
	}
	if errors.Is(err, buildsvc.ErrRepoFetcherNotConfigured) {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "repo fetcher not configured")
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

	log.Printf("ERROR job handler request failed: %v", err)
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isBadRequestError(err error) bool {
	return errors.Is(err, service.ErrJobIDRequired) ||
		errors.Is(err, service.ErrJobNameRequired) ||
		errors.Is(err, service.ErrJobRunRefRequired) ||
		errors.Is(err, service.ErrJobPriorityOutOfRange) ||
		errors.Is(err, service.ErrJobProjectIDRequired) ||
		errors.Is(err, repository.ErrProjectNotFound) ||
		errors.Is(err, service.ErrJobManagedImageConfigNotConfigured) ||
		errors.Is(err, service.ErrJobManagedImageNameRequired) ||
		errors.Is(err, service.ErrJobManagedImagePipelinePathRequired) ||
		errors.Is(err, service.ErrJobManagedImageWriteCredentialIDRequired) ||
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

	var latestBuild *api.JobBuildSummaryResponse
	if job.LatestBuild != nil {
		latestBuild = &api.JobBuildSummaryResponse{
			ID:           job.LatestBuild.ID,
			BuildNumber:  job.LatestBuild.BuildNumber,
			Status:       string(job.LatestBuild.Status),
			CreatedAt:    job.LatestBuild.CreatedAt.Format(time.RFC3339),
			ErrorMessage: job.LatestBuild.ErrorMessage,
		}
		if job.LatestBuild.FinishedAt != nil {
			formatted := job.LatestBuild.FinishedAt.Format(time.RFC3339)
			latestBuild.FinishedAt = &formatted
		}
	}

	return api.JobResponse{
		ID:               job.ID,
		ProjectID:        job.ProjectID,
		Name:             job.Name,
		Priority:         domain.NormalizePriority(job.Priority),
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
		ManagedImage:     toManagedImageConfigResponse(job.ManagedImageConfig),
		LatestBuild:      latestBuild,
		Enabled:          job.Enabled,
		CreatedAt:        job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        job.UpdatedAt.Format(time.RFC3339),
	}
}

func toCreateManagedImageConfigInput(req *api.CreateJobManagedImageConfigRequest) *service.ManagedImageConfigInput {
	if req == nil {
		return nil
	}
	return &service.ManagedImageConfigInput{
		Enabled:           req.Enabled,
		ManagedImageName:  req.ManagedImageName,
		PipelinePath:      req.PipelinePath,
		WriteCredentialID: req.WriteCredentialID,
		BotBranchPrefix:   req.BotBranchPrefix,
		CommitAuthorName:  req.CommitAuthorName,
		CommitAuthorEmail: req.CommitAuthorEmail,
	}
}

func toUpdateManagedImageConfigInput(req *api.UpdateJobManagedImageConfigRequest) *service.ManagedImageConfigPatch {
	if req == nil {
		return nil
	}
	return &service.ManagedImageConfigPatch{
		Enabled:           req.Enabled,
		ManagedImageName:  req.ManagedImageName,
		PipelinePath:      req.PipelinePath,
		WriteCredentialID: req.WriteCredentialID,
		BotBranchPrefix:   req.BotBranchPrefix,
		CommitAuthorName:  req.CommitAuthorName,
		CommitAuthorEmail: req.CommitAuthorEmail,
	}
}

func toManagedImageConfigResponse(config *domain.JobManagedImageConfig) *api.JobManagedImageConfigResponse {
	if config == nil {
		return nil
	}
	return &api.JobManagedImageConfigResponse{
		Enabled:           config.Enabled,
		ManagedImageName:  config.ManagedImageName,
		PipelinePath:      config.PipelinePath,
		WriteCredentialID: config.WriteCredentialID,
		BotBranchPrefix:   config.BotBranchPrefix,
		CommitAuthorName:  config.CommitAuthorName,
		CommitAuthorEmail: config.CommitAuthorEmail,
		CreatedAt:         config.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         config.UpdatedAt.Format(time.RFC3339),
	}
}
