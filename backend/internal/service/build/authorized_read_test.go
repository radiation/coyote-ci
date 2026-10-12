package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestAuthorizedReaderGetBuild(t *testing.T) {
	ctx := context.Background()
	buildRepo := repositorymemory.NewBuildRepository()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: time.Now().UTC()}
	if _, createErr := buildRepo.Create(ctx, build); createErr != nil {
		t.Fatalf("create build failed: %v", createErr)
	}

	reader := NewAuthorizedReader(
		NewBuildService(buildRepo, nil, nil),
		authorization.NewAuthorizer(false, buildMembershipLookup{memberships: map[string]domain.ProjectMembership{
			"project-1": {ProjectID: "project-1", UserID: "viewer-1", Role: domain.ProjectMemberRoleViewer},
		}}),
	)
	viewer := authorization.Principal{User: domain.User{ID: "viewer-1"}}
	if got, getErr := reader.GetBuild(ctx, viewer, build.ID); getErr != nil || got.ID != build.ID {
		t.Fatalf("expected viewer build access, got build=%+v err=%v", got, getErr)
	}

	outsider := authorization.Principal{User: domain.User{ID: "outsider-1"}}
	if _, getErr := reader.GetBuild(ctx, outsider, build.ID); !errors.Is(getErr, authorization.ErrForbidden) {
		t.Fatalf("expected outsider denial, got %v", getErr)
	}

	missingScope := authorization.Principal{
		User: domain.User{ID: "viewer-1"},
		APIToken: &authorization.APITokenIdentity{
			ID:     "token-1",
			Scopes: []domain.APITokenScope{domain.APITokenScopeBuildLogs},
		},
	}
	var scopeErr *authorization.MissingAPITokenScopeError
	if _, getErr := reader.GetBuild(ctx, missingScope, build.ID); !errors.As(getErr, &scopeErr) {
		t.Fatalf("expected missing scope error, got %v", getErr)
	}

	scopedPrincipal := missingScope
	scopedPrincipal.APIToken.Scopes = []domain.APITokenScope{domain.APITokenScopeBuildRead}
	if _, getErr := reader.GetBuild(ctx, scopedPrincipal, build.ID); getErr != nil {
		t.Fatalf("expected scoped token access, got %v", getErr)
	}

	if _, getErr := reader.GetBuild(ctx, viewer, "missing"); !errors.Is(getErr, ErrBuildNotFound) {
		t.Fatalf("expected build not found, got %v", getErr)
	}
}

func TestAuthorizedReaderGetBuildAllowsDisabledAuth(t *testing.T) {
	ctx := context.Background()
	buildRepo := repositorymemory.NewBuildRepository()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: time.Now().UTC()}
	if _, createErr := buildRepo.Create(ctx, build); createErr != nil {
		t.Fatalf("create build failed: %v", createErr)
	}

	reader := NewAuthorizedReader(NewBuildService(buildRepo, nil, nil), authorization.NewAuthorizer(true, nil))
	if _, getErr := reader.GetBuild(ctx, authorization.Principal{}, build.ID); getErr != nil {
		t.Fatalf("expected disabled auth build access, got %v", getErr)
	}
}

type buildMembershipLookup struct {
	memberships map[string]domain.ProjectMembership
}

func (s buildMembershipLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	membership, ok := s.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}
