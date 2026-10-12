package build

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestBuildService_ReleaseFailureHookRunsAfterFailedStepCompletion(t *testing.T) {
	claimToken := "claim-1"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning},
		steps: []domain.BuildStep{{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	snapshotter := &recordingReleaseSnapshotter{}
	service := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), BuildServiceConfig{ReleaseSnapshotter: snapshotter})

	_, completionErr := service.HandleStepResult(context.Background(), runner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "test", ClaimToken: claimToken}, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: 1, Stderr: "test failed", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if completionErr != nil {
		t.Fatalf("complete failed step: %v", completionErr)
	}
	if len(snapshotter.failedBuilds) != 1 || snapshotter.failedBuilds[0].Status != domain.BuildStatusFailed {
		t.Fatalf("release failure calls=%+v, want failed build", snapshotter.failedBuilds)
	}
}

func TestBuildService_ReleaseFailureHookRunsAfterAtomicCancel(t *testing.T) {
	base := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning}}
	repo := &fakeAtomicCancelBuildRepository{fakeBuildRepository: base}
	snapshotter := &recordingReleaseSnapshotter{}
	service := NewBuildServiceFromConfig(repo, nil, logs.NewMemorySink(), BuildServiceConfig{ReleaseSnapshotter: snapshotter})

	_, cancelErr := service.CancelBuild(context.Background(), "build-1")
	if cancelErr != nil {
		t.Fatalf("cancel build: %v", cancelErr)
	}
	if len(snapshotter.failedBuilds) != 1 || snapshotter.failedBuilds[0].Status != domain.BuildStatusCanceled {
		t.Fatalf("release failure calls=%+v, want canceled build", snapshotter.failedBuilds)
	}
}

type recordingReleaseSnapshotter struct {
	failedBuilds []domain.Build
}

func (*recordingReleaseSnapshotter) EnsureSnapshot(context.Context, domain.Build) error {
	return nil
}

func (s *recordingReleaseSnapshotter) FailBuildRelease(_ context.Context, build domain.Build) error {
	s.failedBuilds = append(s.failedBuilds, build)
	return nil
}
