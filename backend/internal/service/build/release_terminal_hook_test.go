package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
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

func TestBuildService_ReconcileArtifactVersions(t *testing.T) {
	for _, status := range []domain.BuildStatus{domain.BuildStatusRunning, domain.BuildStatusFailed, domain.BuildStatusCanceled, domain.BuildStatusSuccess} {
		t.Run(string(status), func(t *testing.T) {
			jobID := "job-1"
			pipelineYAML := "version: 1\nsteps:\n  - name: package\n    run: true\nartifacts:\n  - path: dist/app.tgz\n    version:\n      template: '1.2.3'\n      channel: latest\n"
			build := domain.Build{ID: "build-1", JobID: &jobID, Status: status, PipelineConfigYAML: &pipelineYAML}
			tagger := &fakeBuildVersionTagger{}
			service := NewBuildServiceFromConfig(&fakeBuildRepository{build: build}, nil, nil, BuildServiceConfig{
				ArtifactRepo:  &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{build.ID: {{ID: "artifact-1", BuildID: build.ID, LogicalPath: "dist/app.tgz"}}}},
				VersionTagger: tagger,
			})
			if reconcileErr := service.ReconcileArtifactVersions(context.Background(), build.ID); reconcileErr != nil {
				t.Fatal(reconcileErr)
			}
			wantCalls := 0
			if status == domain.BuildStatusSuccess {
				wantCalls = 1
				if tagger.input.Kind != string(domain.VersionTagKindVersion) || tagger.input.Version != "1.2.3" {
					t.Fatalf("recovery moved configured channel: %+v", tagger.input)
				}
			}
			if tagger.calls != wantCalls {
				t.Fatalf("version calls=%d, want %d", tagger.calls, wantCalls)
			}
		})
	}
	service := NewBuildService(&fakeBuildRepository{getErr: repository.ErrBuildNotFound}, nil, nil)
	if reconcileErr := service.ReconcileArtifactVersions(context.Background(), "missing"); !errors.Is(reconcileErr, repository.ErrBuildNotFound) {
		t.Fatalf("build lookup failure not propagated: %v", reconcileErr)
	}
}

func (*recordingReleaseSnapshotter) EnsureSnapshot(context.Context, domain.Build) error {
	return nil
}

func (s *recordingReleaseSnapshotter) FailBuildRelease(_ context.Context, build domain.Build) error {
	s.failedBuilds = append(s.failedBuilds, build)
	return nil
}
