package execution

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type ResolvedBuildSourceSpec struct {
	RepositoryURL      string
	Ref                string
	CommitSHA          string
	RepositoryIdentity *domain.RepositoryIdentitySnapshot
	HasSource          bool
}

func sourceSpecFromBuild(build domain.Build) ResolvedBuildSourceSpec {
	if build.Source != nil {
		result := ResolvedBuildSourceSpec{
			RepositoryURL: strings.TrimSpace(build.Source.RepositoryURL),
			Ref:           readOptionalString(build.Source.Ref),
			CommitSHA:     readOptionalString(build.Source.CommitSHA),
		}
		if build.RegisteredRepositoryID != nil && build.SCMConnectionID != nil && build.ProviderRepositoryID != nil {
			result.RepositoryIdentity = &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: strings.TrimSpace(*build.RegisteredRepositoryID), SCMConnectionID: strings.TrimSpace(*build.SCMConnectionID), ProviderRepositoryID: strings.TrimSpace(*build.ProviderRepositoryID)}
		}
		result.HasSource = result.RepositoryURL != ""
		return result
	}

	result := ResolvedBuildSourceSpec{
		RepositoryURL: readOptionalString(build.RepoURL),
		Ref:           readOptionalString(build.Ref),
		CommitSHA:     readOptionalString(build.CommitSHA),
	}
	if build.RegisteredRepositoryID != nil && build.SCMConnectionID != nil && build.ProviderRepositoryID != nil {
		result.RepositoryIdentity = &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: strings.TrimSpace(*build.RegisteredRepositoryID), SCMConnectionID: strings.TrimSpace(*build.SCMConnectionID), ProviderRepositoryID: strings.TrimSpace(*build.ProviderRepositoryID)}
	}
	result.HasSource = result.RepositoryURL != ""
	return result
}

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
