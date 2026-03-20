package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
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

type buildResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func (h *BuildHandler) CreateBuild(w http.ResponseWriter, r *http.Request) {
	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	build, err := h.buildService.CreateBuild(r.Context(), service.CreateBuildInput{
		ProjectID: req.ProjectID,
	})
	if err != nil {
		status := http.StatusInternalServerError
		msg := "internal server error"

		if errors.Is(err, service.ErrProjectIDRequired) {
			status = http.StatusBadRequest
			msg = err.Error()
		}

		writeJSON(w, status, map[string]string{
			"error": msg,
		})
		return
	}

	resp := buildResponse{
		ID:        build.ID,
		ProjectID: build.ProjectID,
		Status:    string(build.Status),
		CreatedAt: build.CreatedAt.Format(time.RFC3339),
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *BuildHandler) GetBuild(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "buildID")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "build id is required",
		})
		return
	}

	build, err := h.buildService.GetBuild(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toBuildResponse(build))
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
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "build id is required",
		})
		return
	}

	build, err := transition(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toBuildResponse(build))
}

func (h *BuildHandler) writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := "internal server error"

	if errors.Is(err, repository.ErrBuildNotFound) {
		status = http.StatusNotFound
		msg = "build not found"
	}

	if errors.Is(err, service.ErrInvalidBuildStatusTransition) {
		status = http.StatusConflict
		msg = err.Error()
	}

	writeJSON(w, status, map[string]string{
		"error": msg,
	})
}

func toBuildResponse(build domain.Build) buildResponse {
	return buildResponse{
		ID:        build.ID,
		ProjectID: build.ProjectID,
		Status:    string(build.Status),
		CreatedAt: build.CreatedAt.Format(time.RFC3339),
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
