package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

type PublicHandler struct {
	projects *service.ProjectService
	builds   *buildsvc.BuildService
	jobs     *service.JobService
}

func NewPublicHandler(projects *service.ProjectService, builds *buildsvc.BuildService, jobs *service.JobService) *PublicHandler {
	return &PublicHandler{projects: projects, builds: builds, jobs: jobs}
}

// ListProjects godoc
// @Summary List public projects
// @Description Lists projects explicitly marked public.
// @Tags public
// @Produce json
// @Success 200 {object} api.PublicProjectListEnvelope
// @Router /public/projects [get]
func (h *PublicHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.ListProjects(r.Context())
	if err != nil {
		h.writePublicError(w, err)
		return
	}

	responses := make([]api.PublicProjectResponse, 0, len(projects))
	for _, project := range projects {
		if project.IsPublic {
			responses = append(responses, toPublicProjectResponse(project))
		}
	}
	writeDataJSON(w, http.StatusOK, api.PublicProjectListResponse{Projects: responses})
}

// GetProject godoc
// @Summary Get a public project
// @Tags public
// @Produce json
// @Param slug path string true "Project slug"
// @Success 200 {object} api.PublicProjectEnvelope
// @Failure 404 {object} api.ErrorResponse
// @Router /public/projects/{slug} [get]
func (h *PublicHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	project, ok := h.publicProjectBySlug(w, r)
	if !ok {
		return
	}
	writeDataJSON(w, http.StatusOK, toPublicProjectResponse(project))
}

// ListProjectBuilds godoc
// @Summary List builds for a public project
// @Tags public
// @Produce json
// @Param slug path string true "Project slug"
// @Param limit query int false "Max results (default 50, max 200)"
// @Param offset query int false "Number of results to skip"
// @Success 200 {object} api.PublicBuildListEnvelope
// @Failure 404 {object} api.ErrorResponse
// @Router /public/projects/{slug}/builds [get]
func (h *PublicHandler) ListProjectBuilds(w http.ResponseWriter, r *http.Request) {
	project, ok := h.publicProjectBySlug(w, r)
	if !ok {
		return
	}
	builds, err := h.builds.ListBuildsPaged(r.Context(), repository.ListParams{
		Limit:     parseQueryInt(r, "limit", 0),
		Offset:    parseQueryInt(r, "offset", 0),
		ProjectID: project.ID,
	})
	if err != nil {
		h.writePublicError(w, err)
		return
	}
	jobNames, err := h.jobNames(r, builds)
	if err != nil {
		h.writePublicError(w, err)
		return
	}

	responses := make([]api.PublicBuildResponse, 0, len(builds))
	for _, build := range builds {
		responses = append(responses, toPublicBuildResponse(build, jobNames[publicJobID(build.JobID)]))
	}
	writeDataJSON(w, http.StatusOK, api.PublicBuildListResponse{Builds: responses})
}

// GetProjectBuild godoc
// @Summary Get a public project build
// @Tags public
// @Produce json
// @Param slug path string true "Project slug"
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.PublicBuildEnvelope
// @Failure 404 {object} api.ErrorResponse
// @Router /public/projects/{slug}/builds/{buildID} [get]
func (h *PublicHandler) GetProjectBuild(w http.ResponseWriter, r *http.Request) {
	project, ok := h.publicProjectBySlug(w, r)
	if !ok {
		return
	}
	buildID := strings.TrimSpace(chi.URLParam(r, "buildID"))
	if buildID == "" {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	build, err := h.builds.GetBuild(r.Context(), buildID)
	if err != nil {
		h.writePublicError(w, err)
		return
	}
	if build.ProjectID != project.ID {
		h.writeNotFound(w)
		return
	}
	jobNames, err := h.jobNames(r, []domain.Build{build})
	if err != nil {
		h.writePublicError(w, err)
		return
	}
	steps, err := h.builds.GetBuildSteps(r.Context(), build.ID)
	if err != nil {
		h.writePublicError(w, err)
		return
	}
	response := toPublicBuildResponse(build, jobNames[publicJobID(build.JobID)])
	response.Steps = toPublicBuildStepResponses(steps)
	writeDataJSON(w, http.StatusOK, response)
}

func (h *PublicHandler) publicProjectBySlug(w http.ResponseWriter, r *http.Request) (domain.Project, bool) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	project, err := h.projects.GetProjectBySlug(r.Context(), slug)
	if err != nil {
		h.writePublicError(w, err)
		return domain.Project{}, false
	}
	if !project.IsPublic {
		h.writeNotFound(w)
		return domain.Project{}, false
	}
	return project, true
}

func (h *PublicHandler) jobNames(r *http.Request, builds []domain.Build) (map[string]*string, error) {
	names := make(map[string]*string)
	if h.jobs == nil {
		return names, nil
	}
	jobIDs := make([]string, 0, len(builds))
	for _, build := range builds {
		if jobID := publicJobID(build.JobID); jobID != "" {
			jobIDs = append(jobIDs, jobID)
		}
	}
	jobs, err := h.jobs.GetJobsByIDs(r.Context(), jobIDs)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		name := strings.TrimSpace(job.Name)
		if name == "" {
			continue
		}
		nameCopy := name
		names[job.ID] = &nameCopy
	}
	return names, nil
}

func (h *PublicHandler) writePublicError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrProjectNotFound) || errors.Is(err, buildsvc.ErrBuildNotFound) {
		h.writeNotFound(w)
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *PublicHandler) writeNotFound(w http.ResponseWriter) {
	writeErrorJSON(w, http.StatusNotFound, "not_found", "not found")
}

func toPublicProjectResponse(project domain.Project) api.PublicProjectResponse {
	return api.PublicProjectResponse{
		ID:          project.ID,
		Slug:        project.Slug,
		Name:        project.Name,
		Description: project.Description,
	}
}

func toPublicBuildResponse(build domain.Build, jobName *string) api.PublicBuildResponse {
	return api.PublicBuildResponse{
		ID:          build.ID,
		Number:      build.BuildNumber,
		Status:      string(build.Status),
		JobName:     jobName,
		Attempt:     build.AttemptNumber,
		CreatedAt:   build.CreatedAt.Format(time.RFC3339),
		StartedAt:   formatPublicTime(build.StartedAt),
		CompletedAt: formatPublicTime(build.FinishedAt),
	}
}

func toPublicBuildStepResponses(steps []domain.BuildStep) []api.PublicBuildStepResponse {
	responses := make([]api.PublicBuildStepResponse, 0, len(steps))
	for _, step := range steps {
		responses = append(responses, api.PublicBuildStepResponse{
			Index:       step.StepIndex,
			Name:        step.Name,
			Status:      string(step.Status),
			StartedAt:   formatPublicTime(step.StartedAt),
			CompletedAt: formatPublicTime(step.FinishedAt),
		})
	}
	return responses
}

func formatPublicTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func publicJobID(jobID *string) string {
	if jobID == nil {
		return ""
	}
	return strings.TrimSpace(*jobID)
}
