package domain

import "strings"

type SourceSpec struct {
	RepositoryURL string
	Ref           *string
	CommitSHA     *string
}

func NewSourceSpec(repositoryURL string, ref string, commitSHA string) *SourceSpec {
	repo := strings.TrimSpace(repositoryURL)
	if repo == "" {
		return nil
	}

	result := &SourceSpec{RepositoryURL: repo}
	if trimmedRef := strings.TrimSpace(ref); trimmedRef != "" {
		result.Ref = &trimmedRef
	}
	if trimmedCommit := strings.TrimSpace(commitSHA); trimmedCommit != "" {
		result.CommitSHA = &trimmedCommit
	}

	return result
}
