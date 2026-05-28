package handler

import (
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type BuildHandler struct {
	buildService *buildsvc.BuildService
	versionTags  *versiontagsvc.Service
	projects     *service.ProjectService
	authMode     auth.Mode
	projectRoles auth.ProjectRoleLookup
}

func NewBuildHandler(buildService *buildsvc.BuildService) *BuildHandler {
	return &BuildHandler{
		buildService: buildService,
	}
}

func (h *BuildHandler) SetVersionTagService(service *versiontagsvc.Service) {
	h.versionTags = service
}

func (h *BuildHandler) SetProjectService(projects *service.ProjectService) {
	h.projects = projects
}

func (h *BuildHandler) SetAuthorization(mode auth.Mode, projectRoles auth.ProjectRoleLookup) {
	h.authMode = mode
	h.projectRoles = projectRoles
}
