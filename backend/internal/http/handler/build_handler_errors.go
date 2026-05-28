package handler

import (
	"errors"
	"net/http"

	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func (h *BuildHandler) writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, buildsvc.ErrBuildNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "build_not_found", "build not found")
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "execution_job_not_found", "execution job not found")
		return
	}

	if errors.Is(err, buildsvc.ErrArtifactNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "artifact_not_found", "artifact not found")
		return
	}

	if errors.Is(err, buildsvc.ErrInvalidBuildStatusTransition) {
		writeErrorJSON(w, http.StatusConflict, "invalid_transition", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobNotRetryable) {
		writeErrorJSON(w, http.StatusConflict, "job_not_retryable", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrInvalidRerunStepIndex) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_step_index", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrBuildRerunUnavailable) {
		writeErrorJSON(w, http.StatusBadRequest, "rerun_unavailable", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrExecutionJobRepoNotConfigured) {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if errors.Is(err, buildsvc.ErrCustomTemplateStepsRequired) || errors.Is(err, buildsvc.ErrCustomTemplateStepCommandRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isCreateBuildBadRequestError(err error) bool {
	return errors.Is(err, buildsvc.ErrProjectIDRequired) ||
		errors.Is(err, buildsvc.ErrRepoURLRequired) ||
		errors.Is(err, buildsvc.ErrSourceTargetRequired)
}

func isCreatePipelineBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err) ||
		errors.Is(err, buildsvc.ErrPipelineYAMLRequired)
}

func isCreateRepoBuildBadRequestError(err error) bool {
	return isCreateBuildBadRequestError(err) ||
		errors.Is(err, buildsvc.ErrInvalidPipelinePath)
}
