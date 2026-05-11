package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type ProjectHandler struct {
	projects     *service.ProjectService
	jobs         *service.JobService
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

func NewProjectHandler(projects *service.ProjectService, jobs *service.JobService) *ProjectHandler {
	return &ProjectHandler{projects: projects, jobs: jobs}
}

func (h *ProjectHandler) SetAuthorization(mode auth.Mode, projectRoles auth.ProjectRoleLookup) {
	h.authMode = mode
	h.projectRoles = projectRoles
}

// ListProjects godoc
// @Summary List projects
// @Description Lists projects.
// @Tags projects
// @Produce json
// @Success 200 {object} api.ProjectListEnvelope
// @Failure 500 {object} api.ErrorResponse
// @Router /projects [get]
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.ListProjects(r.Context())
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	projects, err = h.filterProjectsForRead(r.Context(), projects)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.ProjectResponse, 0, len(projects))
	for _, project := range projects {
		responses = append(responses, toProjectResponse(project))
	}
	writeDataJSON(w, http.StatusOK, api.ProjectListResponse{Projects: responses})
}

// CreateProject godoc
// @Summary Create project
// @Description Creates a new project.
// @Tags projects
// @Accept json
// @Produce json
// @Param request body api.CreateProjectRequest true "Project create request"
// @Success 201 {object} api.ProjectEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects [post]
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	if !authorizeGlobalAdmin(w, r, h.authMode, "global admin is required") {
		return
	}

	var req api.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	project, err := h.projects.CreateProject(r.Context(), service.CreateProjectInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toProjectResponse(project))
}

// GetProject godoc
// @Summary Get project
// @Description Returns one project.
// @Tags projects
// @Produce json
// @Param id path string true "Project ID"
// @Success 200 {object} api.ProjectEnvelope
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id} [get]
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	project, err := h.projects.GetProject(r.Context(), id)
	if err != nil {
		h.writeProjectError(w, err)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, project.ID, auth.CanReadProject, "project membership is required") {
		return
	}
	writeDataJSON(w, http.StatusOK, toProjectResponse(project))
}

// UpdateProject godoc
// @Summary Update project
// @Description Updates project metadata.
// @Tags projects
// @Accept json
// @Produce json
// @Param id path string true "Project ID"
// @Param request body api.UpdateProjectRequest true "Project update request"
// @Success 200 {object} api.ProjectEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id} [patch]
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !authorizeProject(w, r, h.authMode, h.projectRoles, id, auth.CanUpdateProject, "project owner is required") {
		return
	}
	var req api.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	updated, err := h.projects.UpdateProject(r.Context(), id, service.UpdateProjectInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: service.OptionalStringPatch{Set: req.Description.Set, Value: req.Description.Value},
	})
	if err != nil {
		h.writeProjectError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toProjectResponse(updated))
}

// DeleteProject godoc
// @Summary Delete project
// @Description Deletes a project when it has no jobs.
// @Tags projects
// @Param id path string true "Project ID"
// @Success 204
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id} [delete]
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if !authorizeGlobalAdmin(w, r, h.authMode, "global admin is required") {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.projects.DeleteProject(r.Context(), id); err != nil {
		h.writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListProjectJobs godoc
// @Summary List project jobs
// @Description Lists jobs for one project.
// @Tags projects
// @Produce json
// @Param id path string true "Project ID"
// @Success 200 {object} api.JobListEnvelope
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id}/jobs [get]
func (h *ProjectHandler) ListProjectJobs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !authorizeProject(w, r, h.authMode, h.projectRoles, id, auth.CanReadProjectResources, "project membership is required") {
		return
	}
	jobs, err := h.jobs.ListJobsByProject(r.Context(), id)
	if err != nil {
		h.writeProjectError(w, err)
		return
	}

	responses := make([]api.JobResponse, 0, len(jobs))
	for _, job := range jobs {
		responses = append(responses, toJobResponse(job))
	}
	writeDataJSON(w, http.StatusOK, api.JobListResponse{Jobs: responses})
}

func (h *ProjectHandler) filterProjectsForRead(ctx context.Context, projects []domain.Project) ([]domain.Project, error) {
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return projects, nil
	}
	projectIDs := make([]string, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID)
	}
	allowedProjects, err := allowedProjectsForUser(ctx, h.authMode, h.projectRoles, projectIDs, auth.CanReadProject)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.Project, 0, len(projects))
	for _, project := range projects {
		if _, ok := allowedProjects[project.ID]; ok {
			filtered = append(filtered, project)
		}
	}
	return filtered, nil
}

func (h *ProjectHandler) writeProjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrProjectNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrProjectHasJobs) ||
		errors.Is(err, repository.ErrProjectSlugConflict) ||
		errors.Is(err, service.ErrDefaultProjectDeleteForbidden) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrProjectIDRequired) ||
		errors.Is(err, service.ErrProjectNameRequired) ||
		errors.Is(err, service.ErrProjectSlugRequired) ||
		errors.Is(err, service.ErrProjectSlugInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toProjectResponse(project domain.Project) api.ProjectResponse {
	return api.ProjectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Slug:        project.Slug,
		Description: project.Description,
		CreatedAt:   project.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   project.UpdatedAt.Format(time.RFC3339),
	}
}
