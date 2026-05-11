package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type BuildHandler struct {
	buildService *buildsvc.BuildService
	versionTags  *versiontagsvc.Service
	projects     *service.ProjectService
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

// GetBuildStepLogs godoc
// @Summary Get build step logs
// @Description Returns persisted log chunks for a build step.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Param stepIndex path int true "Step index"
// @Param after query int false "Replay cursor (exclusive sequence number)"
// @Param limit query int false "Maximum chunks to return"
// @Success 200 {object} api.StepLogsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/steps/{stepIndex}/logs [get]
func (h *BuildHandler) GetBuildStepLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, buildID); !ok {
		return
	}

	stepIndex, ok := parseStepIndex(w, r)
	if !ok {
		return
	}

	var after int64
	afterStr := r.URL.Query().Get("after")
	if afterStr != "" {
		parsedAfter, err := strconv.ParseInt(afterStr, 10, 64)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'after' query parameter")
			return
		}
		if parsedAfter < 0 {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'after' query parameter")
			return
		}
		after = parsedAfter
	}

	limit := 200
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'limit' query parameter")
			return
		}
		if parsedLimit < 1 {
			parsedLimit = 1
		}
		limit = parsedLimit
	}

	chunks, err := h.buildService.GetStepLogChunks(r.Context(), buildID, stepIndex, after, limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	respChunks := make([]api.StepLogChunkResponse, 0, len(chunks))
	next := after
	for _, chunk := range chunks {
		respChunks = append(respChunks, toStepLogChunkResponse(chunk))
		if chunk.SequenceNo > next {
			next = chunk.SequenceNo
		}
	}

	writeDataJSON(w, http.StatusOK, api.StepLogsResponse{
		BuildID:      buildID,
		StepIndex:    stepIndex,
		After:        after,
		NextSequence: next,
		Chunks:       respChunks,
	})
}

// StreamBuildStepLogs godoc
// @Summary Stream build step logs
// @Description Streams build step log chunks over SSE with cursor resume support.
// @Tags builds
// @Produce text/event-stream
// @Param buildID path string true "Build ID"
// @Param stepIndex path int true "Step index"
// @Param after query int false "Replay cursor (exclusive sequence number)"
// @Success 200 {string} string "SSE stream"
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/steps/{stepIndex}/logs/stream [get]
func (h *BuildHandler) StreamBuildStepLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, buildID); !ok {
		return
	}

	stepIndex, ok := parseStepIndex(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}

	after := parseQueryInt64(r, "after", 0)
	if lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); lastEventID != "" {
		if parsed, err := strconv.ParseInt(lastEventID, 10, 64); err == nil && parsed > after {
			after = parsed
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSEComment(w, "connected")
	flusher.Flush()

	const maxSSEDuration = 30 * time.Minute
	ctx := r.Context()
	deadlineTimer := time.NewTimer(maxSSEDuration)
	defer deadlineTimer.Stop()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		chunks, err := h.buildService.GetStepLogChunks(ctx, buildID, stepIndex, after, 500)
		if err != nil {
			if errors.Is(err, buildsvc.ErrBuildNotFound) {
				return
			}
			writeSSEEvent(w, "error", 0, map[string]string{"message": err.Error()})
			flusher.Flush()
			return
		}

		for _, chunk := range chunks {
			resp := toStepLogChunkResponse(chunk)
			writeSSEEvent(w, "chunk", chunk.SequenceNo, resp)
			after = chunk.SequenceNo
		}
		flusher.Flush()

		select {
		case <-ctx.Done():
			return
		case <-deadlineTimer.C:
			writeSSEEvent(w, "timeout", 0, map[string]string{"message": "maximum stream duration exceeded"})
			flusher.Flush()
			return
		case <-ticker.C:
		}
	}
}

func NewBuildHandler(buildService *buildsvc.BuildService) *BuildHandler {
	return &BuildHandler{
		buildService: buildService,
	}
}

func (h *BuildHandler) SetVersionTagService(service *versiontagsvc.Service) {
	h.versionTags = service
}

func (h *BuildHandler) SetProjectService(projects *service.ProjectService) {
	h.projects = projects
}

func (h *BuildHandler) SetAuthorization(mode auth.Mode, projectRoles auth.ProjectRoleLookup) {
	h.authMode = mode
	h.projectRoles = projectRoles
}

// CreateBuild godoc
// @Summary Create build
// @Description Creates a new build in pending status.
// @Tags builds
// @Accept json
// @Produce json
// @Param request body api.CreateBuildRequest true "Build create request"
// @Success 201 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds [post]
func (h *BuildHandler) CreateBuild(w http.ResponseWriter, r *http.Request) {
	var req api.CreateBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, req.ProjectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuild(r.Context(), buildsvc.CreateBuildInput{
		ProjectID: req.ProjectID,
		Steps:     toCreateBuildStepInputs(req.Steps),
		Source:    toCreateBuildSourceInput(req.Source),
	})
	if err != nil {
		if isCreateBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeDataJSON(w, http.StatusCreated, toBuildResponse(build))
}

func toCreateBuildStepInputs(steps []api.CreateBuildStepInput) []buildsvc.CreateBuildStepInput {
	out := make([]buildsvc.CreateBuildStepInput, 0, len(steps))
	for _, step := range steps {
		out = append(out, buildsvc.CreateBuildStepInput{
			Name:           step.Name,
			Command:        step.Command,
			Args:           step.Args,
			Env:            step.Env,
			WorkingDir:     step.WorkingDir,
			TimeoutSeconds: step.TimeoutSeconds,
		})
	}

	return out
}

func toCreateBuildSourceInput(sourceInput *api.BuildSourceInput) *buildsvc.CreateBuildSourceInput {
	if sourceInput == nil {
		return nil
	}

	result := &buildsvc.CreateBuildSourceInput{
		RepositoryURL: sourceInput.RepositoryURL,
	}
	if sourceInput.Ref != nil {
		result.Ref = *sourceInput.Ref
	}
	if sourceInput.CommitSHA != nil {
		result.CommitSHA = *sourceInput.CommitSHA
	}

	return result
}

// CreatePipelineBuild godoc
// @Summary Create build from pipeline YAML
// @Description Parses and validates pipeline YAML, then creates a queued build with resolved steps.
// @Tags builds
// @Accept json
// @Produce json
// @Param request body api.CreatePipelineBuildRequest true "Pipeline build create request"
// @Success 201 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/pipeline [post]
func (h *BuildHandler) CreatePipelineBuild(w http.ResponseWriter, r *http.Request) {
	var req api.CreatePipelineBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, req.ProjectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuildFromPipeline(r.Context(), buildsvc.CreatePipelineBuildInput{
		ProjectID:    req.ProjectID,
		PipelineYAML: req.PipelineYAML,
		Source:       toCreateBuildSourceInput(req.Source),
	})
	if err != nil {
		if isCreatePipelineBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		// Pipeline parse/validation errors are user-facing.
		if _, ok := err.(pipeline.ValidationErrors); ok {
			writeErrorJSON(w, http.StatusBadRequest, "pipeline_validation", err.Error())
			return
		}
		if pe, ok := err.(*pipeline.ParseError); ok {
			writeErrorJSON(w, http.StatusBadRequest, "pipeline_parse", pe.Error())
			return
		}
		log.Printf("CreatePipelineBuild unexpected error: %v", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeDataJSON(w, http.StatusCreated, toBuildResponse(build))
}

// CreateRepoBuild godoc
// @Summary Create build from repository
// @Description Clones a repository, loads .coyote/pipeline.yml, then creates a queued build.
// @Tags builds
// @Accept json
// @Produce json
// @Param request body api.CreateRepoBuildRequest true "Repo build create request"
// @Success 201 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/repo [post]
func (h *BuildHandler) CreateRepoBuild(w http.ResponseWriter, r *http.Request) {
	var req api.CreateRepoBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, req.ProjectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuildFromRepo(r.Context(), buildsvc.CreateRepoBuildInput{
		ProjectID:    req.ProjectID,
		RepoURL:      req.RepoURL,
		Ref:          req.Ref,
		CommitSHA:    req.CommitSHA,
		PipelinePath: req.PipelinePath,
	})
	if err != nil {
		if isCreateRepoBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, buildsvc.ErrPipelineFileNotFound) {
			writeErrorJSON(w, http.StatusBadRequest, "pipeline_not_found", err.Error())
			return
		}
		if errors.Is(err, buildsvc.ErrRepoFetcherNotConfigured) {
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "repo fetcher not configured")
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
		log.Printf("CreateRepoBuild unexpected error: %v", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeDataJSON(w, http.StatusCreated, toBuildResponse(build))
}

// ListBuilds godoc
// @Summary List builds
// @Description Lists builds sorted by newest first with optional pagination and project filters.
// @Tags builds
// @Produce json
// @Param limit query int false "Max results (default 50, max 200)"
// @Param offset query int false "Number of results to skip"
// @Param project_id query string false "Filter builds by project id"
// @Param project_slug query string false "Filter builds by project slug"
// @Success 200 {object} api.BuildListEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds [get]
func (h *BuildHandler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	limit := parseQueryInt(r, "limit", 0)
	offset := parseQueryInt(r, "offset", 0)
	projectID, ok := h.resolveProjectFilter(w, r)
	if !ok {
		return
	}
	if projectID != "" && !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}

	builds, err := h.buildService.ListBuildsPaged(r.Context(), repository.ListParams{
		Limit:     limit,
		Offset:    offset,
		ProjectID: projectID,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	builds, err = h.filterBuildsForRead(r.Context(), builds)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	projectLookup, err := h.projectLookup(r.Context(), builds)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.BuildResponse, 0, len(builds))
	for _, build := range builds {
		responses = append(responses, toBuildResponse(build, projectLookup[build.ProjectID]))
	}

	writeDataJSON(w, http.StatusOK, api.BuildListResponse{Builds: responses})
}

// ListQueue godoc
// @Summary List queue
// @Description Returns queued and running builds with project and job context.
// @Tags queue
// @Produce json
// @Param project_id query string false "Project ID filter"
// @Param project_slug query string false "Project slug filter"
// @Param status query string false "Status filter (queued or running)"
// @Success 200 {object} api.QueueEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /queue [get]
func (h *BuildHandler) ListQueue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProjectFilter(w, r)
	if !ok {
		return
	}
	if projectID != "" && !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != string(domain.BuildStatusQueued) && status != string(domain.BuildStatusRunning) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "status must be queued or running")
		return
	}

	entries, err := h.buildService.ListQueue(r.Context(), repository.QueueListParams{
		ProjectID: projectID,
		Status:    status,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	entries, err = h.filterQueueEntriesForRead(r.Context(), entries)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.QueueEntryResponse, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, toQueueEntryResponse(entry))
	}

	writeDataJSON(w, http.StatusOK, api.QueueListResponse{Entries: responses})
}

// GetBuild godoc
// @Summary Get build
// @Description Returns build details by id.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID} [get]
func (h *BuildHandler) GetBuild(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}

	build, err := h.buildService.GetBuild(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, build.ProjectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}

	projectLookup, err := h.projectLookup(r.Context(), []domain.Build{build})
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	resp := toBuildResponse(build, projectLookup[build.ProjectID])
	if h.versionTags != nil && build.JobID != nil && build.ManagedImageVersionID != nil {
		tags, listErr := h.versionTags.ListManagedImageVersionTags(r.Context(), *build.ManagedImageVersionID)
		if listErr != nil {
			h.writeServiceError(w, listErr)
			return
		}
		resp.Image.VersionTags = toVersionTagResponses(filterVersionTagsForJob(tags, *build.JobID))
	}

	writeDataJSON(w, http.StatusOK, resp)
}

func (h *BuildHandler) resolveProjectFilter(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	projectSlug := strings.TrimSpace(r.URL.Query().Get("project_slug"))
	if projectSlug == "" {
		return projectID, true
	}
	if h.projects == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "project service not configured")
		return "", false
	}
	project, err := h.projects.GetProjectBySlug(r.Context(), projectSlug)
	if err != nil {
		h.writeProjectLookupError(w, err)
		return "", false
	}
	if projectID != "" && projectID != project.ID {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "project_id and project_slug must refer to the same project")
		return "", false
	}
	return project.ID, true
}

func (h *BuildHandler) projectLookup(ctx context.Context, builds []domain.Build) (map[string]*domain.Project, error) {
	lookup := make(map[string]*domain.Project)
	if h.projects == nil || len(builds) == 0 {
		return lookup, nil
	}
	projectIDs := make([]string, 0, len(builds))
	for _, build := range builds {
		if strings.TrimSpace(build.ProjectID) != "" {
			projectIDs = append(projectIDs, build.ProjectID)
		}
	}
	if len(projectIDs) == 0 {
		return lookup, nil
	}
	projects, err := h.projects.GetProjectsByIDs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	for idx := range projects {
		project := projects[idx]
		copyProject := project
		lookup[project.ID] = &copyProject
	}
	return lookup, nil
}

func (h *BuildHandler) writeProjectLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrProjectNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

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

// GetBuildArtifacts godoc
// @Summary List build artifacts
// @Description Returns persisted artifact metadata for a build.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildArtifactsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/artifacts [get]
func (h *BuildHandler) GetBuildArtifacts(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, buildID); !ok {
		return
	}

	artifacts, err := h.buildService.GetBuildArtifacts(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if h.versionTags != nil && len(artifacts) > 0 {
		artifactIDs := make([]string, 0, len(artifacts))
		for _, item := range artifacts {
			artifactIDs = append(artifactIDs, item.ID)
		}
		tagsByArtifactID, listErr := h.versionTags.ListArtifactTagsByIDs(r.Context(), artifactIDs)
		if listErr != nil {
			h.writeServiceError(w, listErr)
			return
		}
		for index := range artifacts {
			artifacts[index].VersionTags = tagsByArtifactID[artifacts[index].ID]
		}
	}

	resp := make([]api.BuildArtifactResponse, 0, len(artifacts))
	for _, item := range artifacts {
		resp = append(resp, toBuildArtifactResponse(item))
	}

	writeDataJSON(w, http.StatusOK, api.BuildArtifactsResponse{
		BuildID:   buildID,
		Artifacts: resp,
	})
}

// DownloadBuildArtifact godoc
// @Summary Download build artifact
// @Description Streams stored artifact content for a build artifact.
// @Tags builds
// @Produce application/octet-stream
// @Param buildID path string true "Build ID"
// @Param artifactID path string true "Artifact ID"
// @Success 200 {string} string "binary payload"
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/artifacts/{artifactID}/download [get]
func (h *BuildHandler) DownloadBuildArtifact(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	artifactID := strings.TrimSpace(chi.URLParam(r, "artifactID"))
	if artifactID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "artifact id is required")
		return
	}
	if _, ok := h.authorizeBuildDownload(w, r, buildID); !ok {
		return
	}

	meta, reader, err := h.buildService.OpenBuildArtifact(r.Context(), buildID, artifactID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	defer func() {
		_ = reader.Close()
	}()

	contentType := "application/octet-stream"
	if meta.ContentType != nil && strings.TrimSpace(*meta.ContentType) != "" {
		contentType = *meta.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(meta.LogicalPath)))
	if meta.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
	}

	if _, err := io.Copy(w, reader); err != nil {
		log.Printf("artifact download stream error: build_id=%s artifact_id=%s err=%v", buildID, artifactID, err)
	}
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
// @Description Marks a non-terminal build as failed and terminalizes non-terminal steps.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
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

// RerunBuildFromStep godoc
// @Summary Rerun build from step
// @Description Creates a new build attempt rerunning from a selected step index.
// @Tags builds
// @Accept json
// @Produce json
// @Param buildID path string true "Build ID"
// @Param request body api.RerunBuildFromStepRequest true "Rerun request"
// @Success 200 {object} api.BuildEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/rerun [post]
func (h *BuildHandler) RerunBuildFromStep(w http.ResponseWriter, r *http.Request) {
	buildID := strings.TrimSpace(chi.URLParam(r, "buildID"))
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}

	var req api.RerunBuildFromStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if _, ok := h.authorizeBuildAction(w, r, buildID, auth.CanTriggerBuild); !ok {
		return
	}

	build, err := h.buildService.RerunBuildFromStep(r.Context(), buildID, req.StepIndex)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toBuildResponse(build))
}

func (h *BuildHandler) authorizeBuildRead(w http.ResponseWriter, r *http.Request, buildID string) (domain.Build, bool) {
	build, err := h.buildService.GetBuild(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return domain.Build{}, false
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, build.ProjectID, auth.CanReadProjectResources, "project membership is required") {
		return domain.Build{}, false
	}
	return build, true
}

func (h *BuildHandler) authorizeBuildDownload(w http.ResponseWriter, r *http.Request, buildID string) (domain.Build, bool) {
	build, err := h.buildService.GetBuild(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return domain.Build{}, false
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, build.ProjectID, auth.CanDownloadArtifact, "project membership is required") {
		return domain.Build{}, false
	}
	return build, true
}

func (h *BuildHandler) authorizeBuildAction(w http.ResponseWriter, r *http.Request, buildID string, check projectAuthorizer) (domain.Build, bool) {
	build, err := h.buildService.GetBuild(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return domain.Build{}, false
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, build.ProjectID, check, "project owner or maintainer is required") {
		return domain.Build{}, false
	}
	return build, true
}

func (h *BuildHandler) filterBuildsForRead(ctx context.Context, builds []domain.Build) ([]domain.Build, error) {
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return builds, nil
	}
	projectIDs := make([]string, 0, len(builds))
	for _, build := range builds {
		projectIDs = append(projectIDs, build.ProjectID)
	}
	allowedProjects, err := allowedProjectsForUser(ctx, h.authMode, h.projectRoles, projectIDs, auth.CanReadProjectResources)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.Build, 0, len(builds))
	for _, build := range builds {
		if _, ok := allowedProjects[build.ProjectID]; ok {
			filtered = append(filtered, build)
		}
	}
	return filtered, nil
}

func (h *BuildHandler) filterQueueEntriesForRead(ctx context.Context, entries []domain.QueueEntry) ([]domain.QueueEntry, error) {
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return entries, nil
	}
	projectIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		projectIDs = append(projectIDs, entry.Build.ProjectID)
	}
	allowedProjects, err := allowedProjectsForUser(ctx, h.authMode, h.projectRoles, projectIDs, auth.CanReadProjectResources)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.QueueEntry, 0, len(entries))
	for _, entry := range entries {
		if _, ok := allowedProjects[entry.Build.ProjectID]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

func (h *BuildHandler) writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, buildsvc.ErrBuildNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "build_not_found", "build not found")
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "execution_job_not_found", "execution job not found")
		return
	}

	if errors.Is(err, buildsvc.ErrArtifactNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "artifact_not_found", "artifact not found")
		return
	}

	if errors.Is(err, buildsvc.ErrInvalidBuildStatusTransition) {
		writeErrorJSON(w, http.StatusConflict, "invalid_transition", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobNotRetryable) {
		writeErrorJSON(w, http.StatusConflict, "job_not_retryable", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrInvalidRerunStepIndex) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_step_index", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobRepoNotConfigured) {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrCustomTemplateStepsRequired) || errors.Is(err, buildsvc.ErrCustomTemplateStepCommandRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isCreateBuildBadRequestError(err error) bool {
	return errors.Is(err, buildsvc.ErrProjectIDRequired) ||
		errors.Is(err, buildsvc.ErrRepoURLRequired) ||
		errors.Is(err, buildsvc.ErrSourceTargetRequired)
}

func isCreatePipelineBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err) ||
		errors.Is(err, buildsvc.ErrPipelineYAMLRequired)
}

func isCreateRepoBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err) ||
		errors.Is(err, buildsvc.ErrInvalidPipelinePath)
}

func parseStepIndex(w http.ResponseWriter, r *http.Request) (int, bool) {
	stepIndexRaw := chi.URLParam(r, "stepIndex")
	if strings.TrimSpace(stepIndexRaw) == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "step index is required")
		return 0, false
	}

	stepIndex, err := strconv.Atoi(stepIndexRaw)
	if err != nil || stepIndex < 0 {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "step index must be a non-negative integer")
		return 0, false
	}

	return stepIndex, true
}

func parseQueryInt64(r *http.Request, key string, fallback int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
