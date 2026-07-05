package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

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
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
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
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
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
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
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
	if !requireAPITokenScope(w, r, domain.APITokenScopeArtifactRead) {
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
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRun) {
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
