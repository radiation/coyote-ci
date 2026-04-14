package execution

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type ResolvedBuildSourceSpec struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
	HasSource     bool
}

func sourceSpecFromBuild(build domain.Build) ResolvedBuildSourceSpec {
	if build.Source != nil {
		result := ResolvedBuildSourceSpec{
			RepositoryURL: strings.TrimSpace(build.Source.RepositoryURL),
			Ref:           readOptionalString(build.Source.Ref),
			CommitSHA:     readOptionalString(build.Source.CommitSHA),
		}
		result.HasSource = result.RepositoryURL != ""
		return result
	}

	result := ResolvedBuildSourceSpec{
		RepositoryURL: readOptionalString(build.RepoURL),
		Ref:           readOptionalString(build.Ref),
		CommitSHA:     readOptionalString(build.CommitSHA),
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
