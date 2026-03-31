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
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type BuildHandler struct {
	buildService *service.BuildService
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

	ctx := r.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		chunks, err := h.buildService.GetStepLogChunks(ctx, buildID, stepIndex, after, 500)
		if err != nil {
			if errors.Is(err, service.ErrBuildNotFound) {
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
		case <-ticker.C:
		}
	}
}

func NewBuildHandler(buildService *service.BuildService) *BuildHandler {
	return &BuildHandler{
		buildService: buildService,
	}
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

	build, err := h.buildService.CreateBuild(r.Context(), service.CreateBuildInput{
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

func toCreateBuildStepInputs(steps []api.CreateBuildStepInput) []service.CreateBuildStepInput {
	out := make([]service.CreateBuildStepInput, 0, len(steps))
	for _, step := range steps {
		out = append(out, service.CreateBuildStepInput{
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

func toCreateBuildSourceInput(sourceInput *api.BuildSourceInput) *service.CreateBuildSourceInput {
	if sourceInput == nil {
		return nil
	}

	result := &service.CreateBuildSourceInput{
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

	build, err := h.buildService.CreateBuildFromPipeline(r.Context(), service.CreatePipelineBuildInput{
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

	build, err := h.buildService.CreateBuildFromRepo(r.Context(), service.CreateRepoBuildInput{
		ProjectID: req.ProjectID,
		RepoURL:   req.RepoURL,
		Ref:       req.Ref,
		CommitSHA: req.CommitSHA,
	})
	if err != nil {
		if isCreateRepoBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, service.ErrPipelineFileNotFound) {
			writeErrorJSON(w, http.StatusBadRequest, "pipeline_not_found", err.Error())
			return
		}
		if errors.Is(err, service.ErrRepoFetcherNotConfigured) {
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
// @Description Lists all builds sorted by newest first.
// @Tags builds
// @Produce json
// @Success 200 {object} api.BuildListEnvelope
// @Failure 500 {object} api.ErrorResponse
// @Router /builds [get]
func (h *BuildHandler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	builds, err := h.buildService.ListBuilds(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.BuildResponse, 0, len(builds))
	for _, build := range builds {
		responses = append(responses, toBuildResponse(build))
	}

	writeDataJSON(w, http.StatusOK, api.BuildListResponse{Builds: responses})
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

	writeDataJSON(w, http.StatusOK, toBuildResponse(build))
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

	steps, err := h.buildService.GetBuildSteps(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	respSteps := make([]api.BuildStepResponse, 0, len(steps))
	for _, step := range steps {
		respSteps = append(respSteps, toBuildStepResponse(step))
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

	artifacts, err := h.buildService.GetBuildArtifacts(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return
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

	customSteps := make([]service.QueueBuildCustomStepInput, 0, len(req.Steps))
	for _, step := range req.Steps {
		customSteps = append(customSteps, service.QueueBuildCustomStepInput{
			Name:    step.Name,
			Command: step.Command,
		})
	}

	h.transitionBuild(w, r, func(ctx context.Context, id string) (domain.Build, error) {
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
	h.transitionBuild(w, r, h.buildService.StartBuild)
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
	h.transitionBuild(w, r, h.buildService.CompleteBuild)
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
	h.transitionBuild(w, r, h.buildService.FailBuild)
}

func (h *BuildHandler) transitionBuild(w http.ResponseWriter, r *http.Request, transition func(ctx context.Context, id string) (domain.Build, error)) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}

	build, err := transition(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toBuildResponse(build))
}

func (h *BuildHandler) writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrBuildNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "build_not_found", "build not found")
		return
	}

	if errors.Is(err, service.ErrArtifactNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "artifact_not_found", "artifact not found")
		return
	}

	if errors.Is(err, service.ErrInvalidBuildStatusTransition) {
		writeErrorJSON(w, http.StatusConflict, "invalid_transition", err.Error())
		return
	}

	if errors.Is(err, service.ErrCustomTemplateStepsRequired) || errors.Is(err, service.ErrCustomTemplateStepCommandRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isCreateBuildBadRequestError(err error) bool {
	return errors.Is(err, service.ErrProjectIDRequired) ||
		errors.Is(err, service.ErrRepoURLRequired) ||
		errors.Is(err, service.ErrSourceTargetRequired)
}

func isCreatePipelineBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err) ||
		errors.Is(err, service.ErrPipelineYAMLRequired)
}

func isCreateRepoBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err)
}

func toBuildResponse(build domain.Build) api.BuildResponse {
	return api.BuildResponse{
		ID:                 build.ID,
		ProjectID:          build.ProjectID,
		Status:             string(build.Status),
		CreatedAt:          build.CreatedAt.Format(time.RFC3339),
		QueuedAt:           formatOptionalTime(build.QueuedAt),
		StartedAt:          formatOptionalTime(build.StartedAt),
		FinishedAt:         formatOptionalTime(build.FinishedAt),
		CurrentStepIndex:   build.CurrentStepIndex,
		ErrorMessage:       build.ErrorMessage,
		PipelineConfigYAML: build.PipelineConfigYAML,
		PipelineName:       build.PipelineName,
		PipelineSource:     build.PipelineSource,
		RepoURL:            build.RepoURL,
		Ref:                build.Ref,
		CommitSHA:          build.CommitSHA,
		Source:             toBuildSourceResponse(build),
	}
}

func toBuildSourceResponse(build domain.Build) *api.BuildSourceResponse {
	if build.Source != nil {
		return &api.BuildSourceResponse{
			RepositoryURL: strings.TrimSpace(build.Source.RepositoryURL),
			Ref:           build.Source.Ref,
			CommitSHA:     build.Source.CommitSHA,
		}
	}

	if build.RepoURL == nil {
		return nil
	}

	return &api.BuildSourceResponse{
		RepositoryURL: strings.TrimSpace(*build.RepoURL),
		Ref:           build.Ref,
		CommitSHA:     build.CommitSHA,
	}
}

func toBuildStepResponse(step domain.BuildStep) api.BuildStepResponse {
	resp := api.BuildStepResponse{
		ID:           step.ID,
		BuildID:      step.BuildID,
		StepIndex:    step.StepIndex,
		Name:         step.Name,
		Command:      displayCommand(step),
		Status:       string(step.Status),
		WorkerID:     step.WorkerID,
		ExitCode:     step.ExitCode,
		Stdout:       step.Stdout,
		Stderr:       step.Stderr,
		ErrorMessage: step.ErrorMessage,
	}

	if step.StartedAt != nil {
		startedAt := step.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &startedAt
	}

	if step.FinishedAt != nil {
		finishedAt := step.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &finishedAt
	}

	return resp
}

func toBuildArtifactResponse(item domain.BuildArtifact) api.BuildArtifactResponse {
	return api.BuildArtifactResponse{
		ID:              item.ID,
		BuildID:         item.BuildID,
		Path:            item.LogicalPath,
		SizeBytes:       item.SizeBytes,
		ContentType:     item.ContentType,
		ChecksumSHA256:  item.ChecksumSHA256,
		DownloadURLPath: "/builds/" + item.BuildID + "/artifacts/" + item.ID + "/download",
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
	}
}

func displayCommand(step domain.BuildStep) string {
	command := strings.TrimSpace(step.Command)
	if command == "" {
		return ""
	}

	if isShellCommand(command) && len(step.Args) >= 2 && strings.TrimSpace(step.Args[0]) == "-c" {
		script := strings.TrimSpace(step.Args[1])
		if script != "" {
			return script
		}
	}

	if len(step.Args) == 0 {
		return command
	}

	parts := make([]string, 0, len(step.Args)+1)
	parts = append(parts, command)
	for _, arg := range step.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}

	return strings.Join(parts, " ")
}

func isShellCommand(command string) bool {
	switch command {
	case "sh", "bash", "zsh", "/bin/sh", "/bin/bash", "/bin/zsh":
		return true
	default:
		return false
	}
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.Format(time.RFC3339)
	return &formatted
}

func toStepLogChunkResponse(chunk logs.StepLogChunk) api.StepLogChunkResponse {
	return api.StepLogChunkResponse{
		SequenceNo: chunk.SequenceNo,
		BuildID:    chunk.BuildID,
		StepID:     chunk.StepID,
		StepIndex:  chunk.StepIndex,
		StepName:   chunk.StepName,
		Stream:     string(chunk.Stream),
		ChunkText:  chunk.ChunkText,
		CreatedAt:  chunk.CreatedAt.Format(time.RFC3339),
	}
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

func writeSSEComment(w http.ResponseWriter, comment string) {
	_, _ = fmt.Fprintf(w, ": %s\n\n", comment)
}

func writeSSEEvent(w http.ResponseWriter, event string, id int64, payload any) {
	if id > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", id)
	}
	if strings.TrimSpace(event) != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintf(w, "data: {\"message\":\"marshal error\"}\n\n")
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}

func writeDataJSON(w http.ResponseWriter, status int, payload any) {
	writeJSON(w, status, api.DataResponse{Data: payload})
}

func writeErrorJSON(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: api.ErrorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
