package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/internal/service"
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
		CreatedAt: build.CreatedAt.Format(http.TimeFormat),
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
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "build not found",
		})
		return
	}

	resp := buildResponse{
		ID:        build.ID,
		ProjectID: build.ProjectID,
		Status:    string(build.Status),
		CreatedAt: build.CreatedAt.Format(http.TimeFormat),
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}
