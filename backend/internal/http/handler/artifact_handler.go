package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type ArtifactHandler struct {
	service      *artifactsvc.Service
	versionTags  *versiontagsvc.Service
	projects     *service.ProjectService
	jobs         *service.JobService
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

func NewArtifactHandler(service *artifactsvc.Service) *ArtifactHandler {
	return &ArtifactHandler{service: service}
}

func (h *ArtifactHandler) SetVersionTagService(service *versiontagsvc.Service) {
	h.versionTags = service
}

func (h *ArtifactHandler) SetProjectService(projects *service.ProjectService) {
	h.projects = projects
}

func (h *ArtifactHandler) SetJobService(jobs *service.JobService) {
	h.jobs = jobs
}

func (h *ArtifactHandler) SetAuthorization(mode auth.Mode, projectRoles auth.ProjectRoleLookup) {
	h.authMode = mode
	h.projectRoles = projectRoles
}

// ListArtifacts godoc
// @Summary List logical artifacts
// @Description Returns logical artifacts grouped with their available versions for artifact repository browsing.
// @Tags artifacts
// @Produce json
// @Param q query string false "Search artifacts by path, project, job, or version tag"
// @Param type query string false "Artifact type filter" Enums(docker_image,npm_package,generic,unknown)
// @Param project_id query string false "Filter artifacts by project id"
// @Param project_slug query string false "Filter artifacts by project slug"
// @Param limit query int false "Max logical artifacts to return"
// @Param offset query int false "Number of logical artifacts to skip"
// @Success 200 {object} api.ArtifactBrowseEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /artifacts [get]
func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "artifact service not configured")
		return
	}
	projectID, ok := h.resolveProjectFilter(w, r)
	if !ok {
		return
	}
	if projectID != "" && !authorizeProject(w, r, h.authMode, h.projectRoles, projectID, auth.CanReadProjectResources, "project membership is required") {
		return
	}

	items, err := h.service.ListArtifacts(r.Context(), artifactsvc.ListArtifactsInput{
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Type:      strings.TrimSpace(r.URL.Query().Get("type")),
		ProjectID: projectID,
		Limit:     parseQueryInt(r, "limit", 0),
		Offset:    parseQueryInt(r, "offset", 0),
	})
	if err != nil {
		switch {
		case errors.Is(err, artifactsvc.ErrInvalidArtifactTypeFilter):
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, artifactsvc.ErrArtifactRepositoryNotConfigured):
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "artifact repository not configured")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	items, err = h.filterArtifactsForRead(r.Context(), items)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if h.versionTags != nil {
		artifactIDs := collectArtifactBrowseArtifactIDs(items)
		if len(artifactIDs) > 0 {
			tagsByArtifactID, listErr := h.versionTags.ListArtifactTagsByIDs(r.Context(), artifactIDs)
			if listErr != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}
			for itemIndex := range items {
				for versionIndex := range items[itemIndex].Versions {
					artifactID := items[itemIndex].Versions[versionIndex].Artifact.ID
					items[itemIndex].Versions[versionIndex].Artifact.VersionTags = tagsByArtifactID[artifactID]
				}
			}
		}
	}
	projectLookup, jobLookup, err := h.contextLookup(r.Context(), items)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	response := make([]api.ArtifactBrowseItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toArtifactBrowseItemResponse(item, projectLookup, jobLookup))
	}

	writeDataJSON(w, http.StatusOK, api.ArtifactBrowseResponse{Artifacts: response})
}

func (h *ArtifactHandler) filterArtifactsForRead(ctx context.Context, items []domain.ArtifactBrowseItem) ([]domain.ArtifactBrowseItem, error) {
	if normalizedAuthMode(h.authMode) == auth.ModeDisabled {
		return items, nil
	}
	projectIDs := make([]string, 0, len(items))
	for _, item := range items {
		projectIDs = append(projectIDs, item.ProjectID)
	}
	allowedProjects, err := allowedProjectsForUser(ctx, h.authMode, h.projectRoles, projectIDs, auth.CanReadProjectResources)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.ArtifactBrowseItem, 0, len(items))
	for _, item := range items {
		if _, ok := allowedProjects[item.ProjectID]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func collectArtifactBrowseArtifactIDs(items []domain.ArtifactBrowseItem) []string {
	artifactIDs := make([]string, 0)
	for _, item := range items {
		for _, version := range item.Versions {
			artifactIDs = append(artifactIDs, version.Artifact.ID)
		}
	}
	return artifactIDs
}

func toArtifactBrowseItemResponse(item domain.ArtifactBrowseItem, projects map[string]domain.Project, jobs map[string]domain.Job) api.ArtifactBrowseItemResponse {
	versions := make([]api.ArtifactBrowseVersionResponse, 0, len(item.Versions))
	for _, version := range item.Versions {
		versions = append(versions, toArtifactBrowseVersionResponse(version, projects, jobs))
	}
	projectName, projectSlug := projectContext(projects, item.ProjectID)
	jobName := jobContext(jobs, item.JobID)
	return api.ArtifactBrowseItemResponse{
		Key:             item.Key,
		Name:            item.Name,
		Path:            item.Path,
		ProjectID:       item.ProjectID,
		ProjectName:     projectName,
		ProjectSlug:     projectSlug,
		JobID:           item.JobID,
		JobName:         jobName,
		ArtifactType:    string(item.ArtifactType),
		LatestCreatedAt: item.LatestCreatedAt.Format(time.RFC3339),
		Versions:        versions,
	}
}

func toArtifactBrowseVersionResponse(version domain.ArtifactBrowseVersion, projects map[string]domain.Project, jobs map[string]domain.Job) api.ArtifactBrowseVersionResponse {
	provider := string(version.Artifact.StorageProvider)
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}
	var stepIndex *int
	var stepName *string
	if version.Step != nil {
		stepIndex = &version.Step.StepIndex
		stepName = &version.Step.Name
	}
	projectName, projectSlug := projectContext(projects, version.Build.ProjectID)
	jobName := jobContext(jobs, version.Build.JobID)
	return api.ArtifactBrowseVersionResponse{
		ArtifactID:      version.Artifact.ID,
		Name:            version.Artifact.Name,
		BuildID:         version.Build.ID,
		BuildNumber:     version.Build.BuildNumber,
		BuildStatus:     string(version.Build.Status),
		ProjectID:       version.Build.ProjectID,
		ProjectName:     projectName,
		ProjectSlug:     projectSlug,
		JobID:           version.Build.JobID,
		JobName:         jobName,
		StepID:          version.Artifact.StepID,
		StepIndex:       stepIndex,
		StepName:        stepName,
		Path:            version.Artifact.LogicalPath,
		SizeBytes:       version.Artifact.SizeBytes,
		ContentType:     version.Artifact.ContentType,
		ChecksumSHA256:  version.Artifact.ChecksumSHA256,
		StorageProvider: provider,
		DownloadURLPath: "/api/builds/" + version.Build.ID + "/artifacts/" + version.Artifact.ID + "/download",
		VersionTags:     toVersionTagResponses(version.Artifact.VersionTags),
		CreatedAt:       version.Artifact.CreatedAt.Format(time.RFC3339),
	}
}

func (h *ArtifactHandler) resolveProjectFilter(w http.ResponseWriter, r *http.Request) (string, bool) {
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

func (h *ArtifactHandler) contextLookup(ctx context.Context, items []domain.ArtifactBrowseItem) (map[string]domain.Project, map[string]domain.Job, error) {
	projectLookup := make(map[string]domain.Project)
	jobLookup := make(map[string]domain.Job)
	if len(items) == 0 {
		return projectLookup, jobLookup, nil
	}
	if h.projects != nil {
		projects, err := h.projects.ListProjects(ctx)
		if err != nil {
			return nil, nil, err
		}
		neededProjects := make(map[string]struct{}, len(items))
		for _, item := range items {
			if strings.TrimSpace(item.ProjectID) != "" {
				neededProjects[item.ProjectID] = struct{}{}
			}
		}
		for _, item := range items {
			for _, version := range item.Versions {
				if strings.TrimSpace(version.Build.ProjectID) != "" {
					neededProjects[version.Build.ProjectID] = struct{}{}
				}
			}
		}
		for _, project := range projects {
			if _, ok := neededProjects[project.ID]; ok {
				projectLookup[project.ID] = project
			}
		}
	}
	if h.jobs != nil {
		neededJobs := make(map[string]struct{})
		for _, item := range items {
			if item.JobID != nil && strings.TrimSpace(*item.JobID) != "" {
				neededJobs[*item.JobID] = struct{}{}
			}
			for _, version := range item.Versions {
				if version.Build.JobID != nil && strings.TrimSpace(*version.Build.JobID) != "" {
					neededJobs[*version.Build.JobID] = struct{}{}
				}
			}
		}
		jobIDs := make([]string, 0, len(neededJobs))
		for id := range neededJobs {
			jobIDs = append(jobIDs, id)
		}
		jobs, err := h.jobs.GetJobsByIDs(ctx, jobIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, job := range jobs {
			jobLookup[job.ID] = job
		}
	}
	return projectLookup, jobLookup, nil
}

func projectContext(projects map[string]domain.Project, projectID string) (*string, *string) {
	project, ok := projects[projectID]
	if !ok {
		return nil, nil
	}
	return &project.Name, &project.Slug
}

func jobContext(jobs map[string]domain.Job, jobID *string) *string {
	if jobID == nil {
		return nil
	}
	job, ok := jobs[*jobID]
	if !ok {
		return nil
	}
	return &job.Name
}

func (h *ArtifactHandler) writeProjectLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrProjectNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
