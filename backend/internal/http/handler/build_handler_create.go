package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

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
	projectID, ok := h.resolveRequestedProjectID(w, r, req.ProjectID)
	if !ok {
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuild(r.Context(), buildsvc.CreateBuildInput{
		ProjectID: projectID,
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

func toCreateBuildStepInputs(steps []api.CreateBuildStepInput) []buildsvc.CreateBuildStepInput {
	out := make([]buildsvc.CreateBuildStepInput, 0, len(steps))
	for _, step := range steps {
		out = append(out, buildsvc.CreateBuildStepInput{
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

func toCreateBuildSourceInput(sourceInput *api.BuildSourceInput) *buildsvc.CreateBuildSourceInput {
	if sourceInput == nil {
		return nil
	}

	result := &buildsvc.CreateBuildSourceInput{
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
	projectID, ok := h.resolveRequestedProjectID(w, r, req.ProjectID)
	if !ok {
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuildFromPipeline(r.Context(), buildsvc.CreatePipelineBuildInput{
		ProjectID:    projectID,
		PipelineYAML: req.PipelineYAML,
		Source:       toCreateBuildSourceInput(req.Source),
	})
	if err != nil {
		if isCreatePipelineBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
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
	projectID, ok := h.resolveRequestedProjectID(w, r, req.ProjectID)
	if !ok {
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}

	build, err := h.buildService.CreateBuildFromRepo(r.Context(), buildsvc.CreateRepoBuildInput{
		ProjectID:    projectID,
		RepoURL:      req.RepoURL,
		Ref:          req.Ref,
		CommitSHA:    req.CommitSHA,
		PipelinePath: req.PipelinePath,
	})
	if err != nil {
		if isCreateRepoBuildBadRequestError(err) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, buildsvc.ErrPipelineFileNotFound) {
			writeErrorJSON(w, http.StatusBadRequest, "pipeline_not_found", err.Error())
			return
		}
		if errors.Is(err, buildsvc.ErrRepoFetcherNotConfigured) {
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

func (h *BuildHandler) resolveRequestedProjectID(w http.ResponseWriter, r *http.Request, requestedProjectID string) (string, bool) {
	trimmedProjectID := strings.TrimSpace(requestedProjectID)
	if trimmedProjectID == "" {
		return "", true
	}
	if _, err := uuid.Parse(trimmedProjectID); err == nil {
		return trimmedProjectID, true
	}
	if h.projects == nil {
		h.writeProjectLookupError(w, errors.New("project service not configured"))
		return "", false
	}
	project, err := h.projects.GetProjectBySlug(r.Context(), trimmedProjectID)
	if err != nil {
		h.writeProjectLookupError(w, err)
		return "", false
	}
	return project.ID, true
}
