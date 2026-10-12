package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	releasesvc "github.com/radiation/coyote-ci/backend/internal/service/release"
)

type ApplicationReleaseHandler struct {
	releases     *releasesvc.Service
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

func NewApplicationReleaseHandler(releases *releasesvc.Service) *ApplicationReleaseHandler {
	return &ApplicationReleaseHandler{releases: releases}
}

func (h *ApplicationReleaseHandler) SetAuthorization(mode auth.Mode, roles auth.ProjectRoleLookup) {
	h.authMode, h.projectRoles = mode, roles
}

// Get godoc
// @Summary Get application release
// @Description Returns a generic application release and its expected output publication state.
// @Tags application-releases
// @Produce json
// @Param releaseID path string true "Application release ID"
// @Success 200 {object} api.ApplicationReleaseEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /application-releases/{releaseID} [get]
func (h *ApplicationReleaseHandler) Get(w http.ResponseWriter, r *http.Request) {
	release, ok := h.loadAuthorized(w, r, false)
	if !ok {
		return
	}
	writeDataJSON(w, http.StatusOK, applicationReleaseResponse(release))
}

// PromoteStable godoc
// @Summary Promote application release to stable
// @Description Moves the generic stable channel to a complete job-backed application release.
// @Tags application-releases
// @Produce json
// @Param releaseID path string true "Application release ID"
// @Success 200 {object} api.ApplicationReleaseEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /application-releases/{releaseID}/channels/stable [post]
func (h *ApplicationReleaseHandler) PromoteStable(w http.ResponseWriter, r *http.Request) {
	release, ok := h.loadAuthorized(w, r, true)
	if !ok {
		return
	}
	var actorID *string
	if user, found := auth.CurrentUser(r.Context()); found {
		value := user.ID
		actorID = &value
	}
	promoted, err := h.releases.PromoteStable(r.Context(), release.ID, actorID)
	if errors.Is(err, repository.ErrApplicationReleaseIncomplete) {
		writeErrorJSON(w, http.StatusConflict, "release_incomplete", "only complete releases can be promoted")
		return
	}
	if errors.Is(err, repository.ErrApplicationReleaseChannelScope) {
		writeErrorJSON(w, http.StatusConflict, "release_channel_scope", "generic channels require a job-backed release")
		return
	}
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeDataJSON(w, http.StatusOK, applicationReleaseResponse(promoted))
}

func (h *ApplicationReleaseHandler) loadAuthorized(w http.ResponseWriter, r *http.Request, manage bool) (domain.ApplicationRelease, bool) {
	if h.releases == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "application release support is not configured")
		return domain.ApplicationRelease{}, false
	}
	releaseID := strings.TrimSpace(chi.URLParam(r, "releaseID"))
	if releaseID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "release id is required")
		return domain.ApplicationRelease{}, false
	}
	release, err := h.releases.Get(r.Context(), releaseID)
	if errors.Is(err, repository.ErrApplicationReleaseNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "application release not found")
		return domain.ApplicationRelease{}, false
	}
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return domain.ApplicationRelease{}, false
	}
	check := auth.CanReadProjectResources
	message := "project membership is required"
	scope := domain.APITokenScopeBuildRead
	if manage {
		check, message, scope = auth.CanManageProjectJobs, "project owner or maintainer is required", domain.APITokenScopeBuildRun
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, release.ProjectID, check, message) || !requireAPITokenScope(w, r, scope) {
		return domain.ApplicationRelease{}, false
	}
	return release, true
}

func applicationReleaseResponse(release domain.ApplicationRelease) api.ApplicationReleaseResponse {
	outputs := make([]api.ApplicationReleaseOutputResponse, 0, len(release.Outputs))
	for _, output := range release.Outputs {
		outputs = append(outputs, api.ApplicationReleaseOutputResponse{ID: output.ID, Kind: string(output.Kind), Key: output.OutputKey, Name: output.DisplayName, Status: string(output.Status), ImmutableReference: output.ImmutableReference, FailureReason: output.FailureReason, ArtifactID: output.ArtifactID, ExecutionJobID: output.ExecutionJobID})
	}
	return api.ApplicationReleaseResponse{ID: release.ID, BuildID: release.BuildID, ProjectID: release.ProjectID, JobID: release.JobID, ApplicationVersion: release.ApplicationVersion, Status: string(release.Status), AutoChannel: release.AutoChannelName, FailureReason: release.FailureReason, Outputs: outputs}
}
