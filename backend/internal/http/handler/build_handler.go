package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type BuildHandler struct {
	buildService *service.BuildService
}

func NewBuildHandler(buildService *service.BuildService) *BuildHandler {
	return &BuildHandler{
		buildService: buildService,
	}
}

type createBuildRequest struct {
	ProjectID string `json:"project_id"`
}

func (h *BuildHandler) CreateBuild(w http.ResponseWriter, r *http.Request) {
	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	build, err := h.buildService.CreateBuild(r.Context(), service.CreateBuildInput{
		ProjectID: req.ProjectID,
	})
	if err != nil {
		if errors.Is(err, service.ErrProjectIDRequired) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeDataJSON(w, http.StatusCreated, toBuildResponse(build))
}

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

	writeDataJSON(w, http.StatusOK, api.BuildStepsResponse{
		BuildID: id,
		Steps:   respSteps,
	})
}

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
			Timestamp: logLine.Timestamp.Format(time.RFC3339),
			Message:   logLine.Message,
		})
	}

	writeDataJSON(w, http.StatusOK, api.BuildLogsResponse{
		BuildID: id,
		Logs:    respLogs,
	})
}

func (h *BuildHandler) QueueBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, h.buildService.QueueBuild)
}

func (h *BuildHandler) StartBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, h.buildService.StartBuild)
}

func (h *BuildHandler) CompleteBuild(w http.ResponseWriter, r *http.Request) {
	h.transitionBuild(w, r, h.buildService.CompleteBuild)
}

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
	if errors.Is(err, repository.ErrBuildNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "build_not_found", "build not found")
		return
	}

	if errors.Is(err, service.ErrInvalidBuildStatusTransition) {
		writeErrorJSON(w, http.StatusConflict, "invalid_transition", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toBuildResponse(build domain.Build) api.BuildResponse {
	return api.BuildResponse{
		ID:        build.ID,
		ProjectID: build.ProjectID,
		Status:    string(build.Status),
		CreatedAt: build.CreatedAt.Format(time.RFC3339),
	}
}

func toBuildStepResponse(step contracts.BuildStep) api.BuildStepResponse {
	resp := api.BuildStepResponse{
		Name:   step.Name,
		Status: string(step.Status),
	}

	if step.StartedAt != nil {
		startedAt := step.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &startedAt
	}

	if step.EndedAt != nil {
		endedAt := step.EndedAt.Format(time.RFC3339)
		resp.EndedAt = &endedAt
	}

	return resp
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
