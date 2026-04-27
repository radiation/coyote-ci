package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type ArtifactHandler struct {
	service     *artifactsvc.Service
	versionTags *versiontagsvc.Service
}

func NewArtifactHandler(service *artifactsvc.Service) *ArtifactHandler {
	return &ArtifactHandler{service: service}
}

func (h *ArtifactHandler) SetVersionTagService(service *versiontagsvc.Service) {
	h.versionTags = service
}

// ListArtifacts godoc
// @Summary List logical artifacts
// @Description Returns logical artifacts grouped with their available versions for artifact repository browsing.
// @Tags artifacts
// @Produce json
// @Param q query string false "Search artifacts by path, project, job, or version tag"
// @Param type query string false "Artifact type filter" Enums(docker_image,npm_package,generic,unknown)
// @Success 200 {object} api.ArtifactBrowseEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /artifacts [get]
func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "artifact service not configured")
		return
	}

	items, err := h.service.ListArtifacts(r.Context(), artifactsvc.ListArtifactsInput{
		Query: strings.TrimSpace(r.URL.Query().Get("q")),
		Type:  strings.TrimSpace(r.URL.Query().Get("type")),
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

	response := make([]api.ArtifactBrowseItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toArtifactBrowseItemResponse(item))
	}

	writeDataJSON(w, http.StatusOK, api.ArtifactBrowseResponse{Artifacts: response})
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

func toArtifactBrowseItemResponse(item domain.ArtifactBrowseItem) api.ArtifactBrowseItemResponse {
	versions := make([]api.ArtifactBrowseVersionResponse, 0, len(item.Versions))
	for _, version := range item.Versions {
		versions = append(versions, toArtifactBrowseVersionResponse(version))
	}
	return api.ArtifactBrowseItemResponse{
		Key:             item.GroupKey,
		Name:            item.Name,
		Path:            item.Path,
		ProjectID:       item.ProjectID,
		JobID:           item.JobID,
		ArtifactType:    string(item.ArtifactType),
		LatestCreatedAt: item.LatestCreatedAt.Format(time.RFC3339),
		Versions:        versions,
	}
}

func toArtifactBrowseVersionResponse(version domain.ArtifactBrowseVersion) api.ArtifactBrowseVersionResponse {
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
	return api.ArtifactBrowseVersionResponse{
		ArtifactID:      version.Artifact.ID,
		Name:            version.Artifact.Name,
		BuildID:         version.Build.ID,
		BuildNumber:     version.Build.BuildNumber,
		BuildStatus:     string(version.Build.Status),
		ProjectID:       version.Build.ProjectID,
		JobID:           version.Build.JobID,
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
