package handler

import (
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestToBuildResponse_MapsOptionalPullRequestSnapshot(t *testing.T) {
	build := domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusQueued,
		CreatedAt: time.Now().UTC(),
		Trigger: domain.BuildTrigger{PullRequest: &domain.PullRequestSnapshot{
			Number:     42,
			Action:     "opened",
			URL:        "https://github.example.com/acme/repo/pull/42",
			BaseRef:    "main",
			BaseSHA:    "base-sha",
			HeadRef:    "feature/pr-42",
			HeadSHA:    "head-sha",
			SourceMode: domain.PullRequestSourceModeHead,
		}},
	}
	response := toBuildResponse(build)
	if response.PullRequest == nil || response.PullRequest.Number != 42 || response.PullRequest.HeadSHA != "head-sha" || response.PullRequest.SourceMode != "head" {
		t.Fatalf("expected mapped pull-request snapshot, got %+v", response.PullRequest)
	}

	response = toBuildResponse(domain.Build{ID: "build-2", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: time.Now().UTC()})
	if response.PullRequest != nil {
		t.Fatalf("expected no pull-request snapshot for non-PR build, got %+v", response.PullRequest)
	}
}
