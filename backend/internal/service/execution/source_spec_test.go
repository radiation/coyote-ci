package execution

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestSourceSpecFromBuild_PreservesCompleteRepositoryIdentity(t *testing.T) {
	registeredRepositoryID := " repository-a "
	connectionID := " connection-a "
	providerRepositoryID := " 100 "
	repositoryURL := " https://github.com/acme/repository.git "
	ref := " main "
	build := domain.Build{
		Source:                 domain.NewSourceSpec(repositoryURL, ref, ""),
		RegisteredRepositoryID: &registeredRepositoryID,
		SCMConnectionID:        &connectionID,
		ProviderRepositoryID:   &providerRepositoryID,
	}

	spec := sourceSpecFromBuild(build)
	if !spec.HasSource || spec.RepositoryURL != "https://github.com/acme/repository.git" || spec.Ref != "main" {
		t.Fatalf("unexpected source spec: %+v", spec)
	}
	if spec.RepositoryIdentity == nil || spec.RepositoryIdentity.RegisteredRepositoryID != "repository-a" || spec.RepositoryIdentity.SCMConnectionID != "connection-a" || spec.RepositoryIdentity.ProviderRepositoryID != "100" {
		t.Fatalf("expected trimmed repository identity, got %+v", spec.RepositoryIdentity)
	}
}

func TestSourceSpecFromBuild_OmitsPartialRepositoryIdentity(t *testing.T) {
	repositoryURL := "https://github.com/acme/repository.git"
	registeredRepositoryID := "repository-a"
	build := domain.Build{RepoURL: &repositoryURL, RegisteredRepositoryID: &registeredRepositoryID}

	spec := sourceSpecFromBuild(build)
	if !spec.HasSource || spec.RepositoryIdentity != nil {
		t.Fatalf("expected source without incomplete identity, got %+v", spec)
	}
}
