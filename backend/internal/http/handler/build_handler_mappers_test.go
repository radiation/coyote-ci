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

func TestToExecutionJobResponse_MapsOptionalTiming(t *testing.T) {
	startedAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Minute)
	response := toExecutionJobResponse(&domain.ExecutionJob{
		ID:        "job-1",
		BuildID:   "build-1",
		StepID:    "step-1",
		CreatedAt: startedAt,
		Timing: &domain.ExecutionTiming{Phases: []domain.ExecutionPhaseTiming{
			{Name: "scheduling", StartedAt: &startedAt, FinishedAt: &finishedAt},
			{Name: "workspace_prepare"},
		}},
	}, nil)
	if response == nil || response.Timing == nil || len(response.Timing.Phases) != 2 {
		t.Fatalf("response=%+v", response)
	}
	phase := response.Timing.Phases[0]
	if phase.Name != "scheduling" || phase.StartedAt == nil || phase.FinishedAt == nil || *phase.StartedAt != startedAt.Format(time.RFC3339) || *phase.FinishedAt != finishedAt.Format(time.RFC3339) {
		t.Fatalf("phase=%+v", phase)
	}
	if response.Timing.Phases[1].StartedAt != nil || response.Timing.Phases[1].FinishedAt != nil {
		t.Fatalf("expected missing timestamps to remain omitted, phase=%+v", response.Timing.Phases[1])
	}
	if toExecutionJobResponse(nil, nil) != nil || toExecutionTimingResponse(nil) != nil {
		t.Fatal("expected nil job and timing responses")
	}
}
