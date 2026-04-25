package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type VersionTagHandler struct {
	service *versiontagsvc.Service
}

func NewVersionTagHandler(service *versiontagsvc.Service) *VersionTagHandler {
	return &VersionTagHandler{service: service}
}

// ListArtifactVersionTags godoc
// @Summary List artifact version tags
// @Description Returns immutable version tags attached to one artifact.
// @Tags version-tags
// @Produce json
// @Param artifactID path string true "Artifact ID"
// @Success 200 {object} api.DataResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /artifacts/{artifactID}/version-tags [get]
func (h *VersionTagHandler) ListArtifactVersionTags(w http.ResponseWriter, r *http.Request) {
	artifactID := strings.TrimSpace(chi.URLParam(r, "artifactID"))
	if artifactID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "artifact id is required")
		return
	}
	tags, err := h.service.ListArtifactTags(r.Context(), artifactID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, api.ArtifactVersionTagsResponse{ArtifactID: artifactID, Tags: toVersionTagResponses(tags)})
}

// ListManagedImageVersionTags godoc
// @Summary List managed image version tags
// @Description Returns immutable version tags attached to one managed image version.
// @Tags version-tags
// @Produce json
// @Param managedImageVersionID path string true "Managed image version ID"
// @Success 200 {object} api.DataResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /managed-image-versions/{managedImageVersionID}/version-tags [get]
func (h *VersionTagHandler) ListManagedImageVersionTags(w http.ResponseWriter, r *http.Request) {
	managedImageVersionID := strings.TrimSpace(chi.URLParam(r, "managedImageVersionID"))
	if managedImageVersionID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "managed image version id is required")
		return
	}
	tags, err := h.service.ListManagedImageVersionTags(r.Context(), managedImageVersionID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, api.ManagedImageVersionTagsResponse{ManagedImageVersionID: managedImageVersionID, Tags: toVersionTagResponses(tags)})
}

// CreateJobVersionTags godoc
// @Summary Create immutable version tags for job targets
// @Description Creates immutable version tags for one or more artifacts and managed image versions scoped to a job.
// @Tags version-tags
// @Accept json
// @Produce json
// @Param jobID path string true "Job ID"
// @Param request body api.VersionTagCreateRequest true "Version tag create request"
// @Success 201 {object} api.DataResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID}/version-tags [post]
func (h *VersionTagHandler) CreateJobVersionTags(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}
	var req api.VersionTagCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	tags, err := h.service.CreateVersionTags(r.Context(), jobID, versiontagsvc.CreateVersionTagsInput{
		Version:                req.Version,
		ArtifactIDs:            req.ArtifactIDs,
		ManagedImageVersionIDs: req.ManagedImageVersionIDs,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, api.JobVersionTagsResponse{JobID: jobID, Version: strings.TrimSpace(req.Version), Tags: toVersionTagResponses(tags)})
}

// ListJobVersionTags godoc
// @Summary Query job version tags by version
// @Description Returns all targets in a job carrying the requested immutable version.
// @Tags version-tags
// @Produce json
// @Param jobID path string true "Job ID"
// @Param version query string true "Version"
// @Success 200 {object} api.DataResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /jobs/{jobID}/version-tags [get]
func (h *VersionTagHandler) ListJobVersionTags(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "job id is required")
		return
	}
	version := r.URL.Query().Get("version")
	tags, err := h.service.ListJobVersionTags(r.Context(), jobID, version)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, api.JobVersionTagsResponse{JobID: jobID, Version: strings.TrimSpace(version), Tags: toVersionTagResponses(tags)})
}

func (h *VersionTagHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, versiontagsvc.ErrJobIDRequired),
		errors.Is(err, versiontagsvc.ErrVersionRequired),
		errors.Is(err, versiontagsvc.ErrTargetRequired),
		errors.Is(err, versiontagsvc.ErrVersionTooLong),
		errors.Is(err, versiontagsvc.ErrVersionContainsControlChars):
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, versiontagsvc.ErrVersionTagRepositoryNotConfigured):
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
	case errors.Is(err, repository.ErrVersionTagTargetNotFound):
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, repository.ErrVersionTagTargetJobMismatch), errors.Is(err, repository.ErrVersionTagConflict):
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
