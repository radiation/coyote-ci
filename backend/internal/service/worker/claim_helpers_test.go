package worker

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestCloneRemoteImageBuildSpecPreservesPlatforms(t *testing.T) {
	if cloned := cloneRemoteImageBuildSpec(nil); cloned != nil {
		t.Fatalf("nil spec became %+v", cloned)
	}
	for _, platforms := range [][]string{nil, {"linux/amd64", "linux/arm64"}} {
		spec := &domain.RemoteImageBuildSpec{Platforms: platforms}
		cloned := cloneRemoteImageBuildSpec(spec)
		if cloned == nil || cloned == spec || !slices.Equal(cloned.Platforms, platforms) {
			t.Fatalf("clone=%+v, source=%+v", cloned, spec)
		}
		if platforms == nil && cloned.Platforms != nil {
			t.Fatal("omitted platforms must remain nil")
		}
		if len(platforms) > 0 {
			cloned.Platforms[0] = "changed"
			if platforms[0] != "linux/amd64" {
				t.Fatalf("source platforms were mutated: %v", platforms)
			}
		}
	}
}

func TestExecutionWorkerService_EnsureBuildRunning(t *testing.T) {
	tests := []struct {
		name      string
		status    domain.BuildStatus
		wantErr   error
		wantStart int
	}{
		{name: "queued starts build", status: domain.BuildStatusQueued, wantStart: 1},
		{name: "running is already ready", status: domain.BuildStatusRunning},
		{name: "terminal build rejected", status: domain.BuildStatusFailed, wantErr: buildsvc.ErrInvalidBuildStatusTransition},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			boundary := &fakeExecutionWorkerBoundary{listBuildsResp: []domain.Build{{ID: "build-1", Status: tc.status}}}
			worker := NewExecutionWorkerServiceWithLease(boundary, "worker-1", 30*time.Second)

			err := worker.ensureBuildRunning(context.Background(), "build-1")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if boundary.startCalls != tc.wantStart {
				t.Fatalf("expected %d start calls, got %d", tc.wantStart, boundary.startCalls)
			}
		})
	}
}

func TestExecutionWorkerService_MirrorJobClaimToStepIgnoresJobsWithoutStepID(t *testing.T) {
	boundary := &fakeExecutionWorkerBoundary{}
	worker := NewExecutionWorkerServiceWithLease(boundary, "worker-1", 30*time.Second)
	claim := worker.newStepClaim()

	claimedStep, claimErr := worker.mirrorJobClaimToStep(context.Background(), domain.ExecutionJob{ID: "job-1", BuildID: "build-1"}, claim)
	if claimErr != nil || claimedStep.ID != "" {
		t.Fatalf("expected missing step id to be ignored, got step=%#v err=%v", claimedStep, claimErr)
	}
	if boundary.claimCalls != 0 || boundary.reclaimCalls != 0 {
		t.Fatalf("expected no step claim attempts, got claim=%d reclaim=%d", boundary.claimCalls, boundary.reclaimCalls)
	}
}
