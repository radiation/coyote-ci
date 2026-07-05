package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

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
	if !requireAPITokenScope(w, r, domain.APITokenScopeArtifactRead) {
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
