package build

import (
	"context"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type AuthorizedReader struct {
	builds     *BuildService
	authorizer authorization.Authorizer
}

func NewAuthorizedReader(builds *BuildService, authorizer authorization.Authorizer) *AuthorizedReader {
	return &AuthorizedReader{builds: builds, authorizer: authorizer}
}

func (r *AuthorizedReader) GetBuild(ctx context.Context, principal authorization.Principal, buildID string) (domain.Build, error) {
	build, err := r.builds.GetBuild(ctx, buildID)
	if err != nil {
		return domain.Build{}, err
	}
	if err := r.authorizer.RequireProjectRole(ctx, principal, build.ProjectID, domain.ProjectMemberRoleViewer); err != nil {
		return domain.Build{}, err
	}
	if err := r.authorizer.RequireAPITokenScope(principal, domain.APITokenScopeBuildRead); err != nil {
		return domain.Build{}, err
	}
	return build, nil
}
