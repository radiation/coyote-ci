package build

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func newMemoryCompletionBuildRepository(t *testing.T, build domain.Build, steps []domain.BuildStep) *memoryrepo.BuildRepository {
	t.Helper()
	repo := memoryrepo.NewBuildRepository()
	if _, createErr := repo.CreateQueuedBuild(context.Background(), build, steps); createErr != nil {
		t.Fatal(createErr)
	}
	if _, updateErr := repo.UpdateStatus(context.Background(), build.ID, domain.BuildStatusRunning, nil); updateErr != nil {
		t.Fatal(updateErr)
	}
	return repo
}

func TestExecutionFailureKind(t *testing.T) {
	tests := []struct {
		name   string
		result runner.RunStepResult
		want   domain.ExecutionFailureKind
	}{
		{
			name: "workspace publication failure",
			result: runner.RunStepResult{
				ExitCode: -1,
				Stderr:   "workspace revision: store unavailable",
			},
			want: domain.ExecutionFailureKindWorkspace,
		},
		{
			name: "timeout",
			result: runner.RunStepResult{
				ExitCode: -1,
				TimedOut: true,
			},
			want: domain.ExecutionFailureKindTimeout,
		},
		{
			name: "timeout-like stderr without typed timeout",
			result: runner.RunStepResult{
				ExitCode: -1,
				Stderr:   "step timed out after 30s",
			},
			want: domain.ExecutionFailureKindExecution,
		},
		{
			name: "ordinary execution failure",
			result: runner.RunStepResult{
				ExitCode: 1,
				Stderr:   "command exited with code 1",
			},
			want: domain.ExecutionFailureKindExecution,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := executionFailureKind(test.result); got != test.want {
				t.Fatalf("executionFailureKind() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildService_HandleStepResult_RequiresAtomicRepositoryForRevisionSuccess(t *testing.T) {
	claimToken := "claim-active"
	service := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	service.SetWorkspaceRevisionPublication(failingWorkspaceRevisionRepository{}, &recordingWorkspaceRevisionStore{})

	report, handleErr := service.HandleStepResult(context.Background(), runner.RunStepRequest{BuildID: "build-1", JobID: "job-1", StepIndex: 0, ClaimToken: claimToken}, runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if handleErr == nil || handleErr.Error() != "execution job repository does not support atomic successful completion" {
		t.Fatalf("expected atomic repository requirement error, got %v", handleErr)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatalf("expected invalid transition report, got %q", report.CompletionOutcome)
	}
}

func TestBuildService_RunStep_AtomicSynchronousCompletion(t *testing.T) {
	for _, configured := range []bool{true, false} {
		for _, publish := range []bool{true, false} {
			for _, status := range []runner.RunStepStatus{runner.RunStepStatusSuccess, runner.RunStepStatusFailed} {
				t.Run(fmt.Sprintf("config=%t/publication=%t/%s", configured, publish, status), func(t *testing.T) {
					service, builds, jobs, request := newAtomicSynchronousFixture(t, configured, publish)
					now := time.Now().UTC()
					service.runner = &fakeRunner{result: runner.RunStepResult{Status: status, StartedAt: now, FinishedAt: now, Stderr: "runner output"}}
					_, report, runErr := service.RunStep(context.Background(), request)
					if runErr != nil || report.CompletionOutcome != repository.StepCompletionCompleted || report.SideEffectErr != nil {
						t.Fatalf("completion=%+v err=%v", report, runErr)
					}
					build, buildErr := builds.GetByID(context.Background(), request.BuildID)
					steps, stepsErr := builds.GetStepsByBuildID(context.Background(), request.BuildID)
					job, jobErr := jobs.GetJobByID(context.Background(), request.JobID)
					if buildErr != nil || stepsErr != nil || jobErr != nil || len(steps) != 1 {
						t.Fatalf("read state: build=%v steps=%v job=%v", buildErr, stepsErr, jobErr)
					}
					if string(build.Status) != string(status) || string(steps[0].Status) != string(status) || string(job.Status) != string(status) {
						t.Fatalf("expected %s: build=%s step=%s job=%s", status, build.Status, steps[0].Status, job.Status)
					}
					if job.ClaimToken != nil || steps[0].ClaimToken != nil || report.BuildBecameSuccessful != (status == runner.RunStepStatusSuccess) {
						t.Fatalf("terminal claims or success ownership incorrect: job=%+v report=%+v", job, report)
					}
					if publish {
						revision, revisionErr := service.workspaceRevisionRepo.GetByProducingExecutionJob(context.Background(), job.ID)
						if status == runner.RunStepStatusSuccess {
							if revisionErr != nil || revision.Status != domain.WorkspaceRevisionStatusPublished {
								t.Fatalf("published workspace: %+v err=%v", revision, revisionErr)
							}
						} else if !errors.Is(revisionErr, repository.ErrWorkspaceRevisionNotFound) {
							t.Fatalf("failed execution must not publish: %+v err=%v", revision, revisionErr)
						}
					}
				})
			}
		}
	}
}

func TestBuildService_HandleStepResult_AtomicSynchronousOutcomes(t *testing.T) {
	for _, status := range []runner.RunStepStatus{runner.RunStepStatusSuccess, runner.RunStepStatusFailed} {
		for _, outcomeCase := range []string{"duplicate", "wrong job token", "wrong step token", "expired", "pending step"} {
			t.Run(fmt.Sprintf("%s/%s", status, outcomeCase), func(t *testing.T) {
				service, builds, jobs, request := newAtomicSynchronousFixture(t, true, false)
				now := time.Now().UTC()
				result := runner.RunStepResult{Status: status, StartedAt: now, FinishedAt: now}
				wantOutcome := repository.StepCompletionStaleClaim
				switch outcomeCase {
				case "duplicate":
					first, firstErr := service.HandleStepResult(context.Background(), request, result)
					if firstErr != nil || first.CompletionOutcome != repository.StepCompletionCompleted {
						t.Fatalf("first completion=%+v err=%v", first, firstErr)
					}
					request.ClaimToken = "old-token"
					wantOutcome = repository.StepCompletionDuplicateTerminal
				case "wrong job token":
					request.ClaimToken = "old-token"
				case "wrong step token":
					_, claimed, claimErr := jobs.ClaimJobByStepID(context.Background(), request.StepID, repository.StepClaim{
						ClaimToken: "replacement", ClaimedAt: now, LeaseExpiresAt: now.Add(time.Hour),
					})
					if claimErr != nil || !claimed {
						t.Fatalf("replace job claim: claimed=%t err=%v", claimed, claimErr)
					}
					request.ClaimToken = "replacement"
				case "expired":
					if _, _, renewErr := jobs.RenewJobLease(context.Background(), request.JobID, request.ClaimToken, now.Add(-time.Minute)); renewErr != nil {
						t.Fatal(renewErr)
					}
				case "pending step":
					if _, updateErr := builds.UpdateStepByIndex(context.Background(), request.BuildID, 0, repository.StepUpdate{Status: domain.BuildStepStatusPending}); updateErr != nil {
						t.Fatal(updateErr)
					}
					wantOutcome = repository.StepCompletionInvalidTransition
				}
				beforeBuild, _ := builds.GetByID(context.Background(), request.BuildID)
				beforeSteps, _ := builds.GetStepsByBuildID(context.Background(), request.BuildID)
				beforeJob, _ := jobs.GetJobByID(context.Background(), request.JobID)
				report, completeErr := service.HandleStepResult(context.Background(), request, result)
				if completeErr != nil || report.CompletionOutcome != wantOutcome || report.BuildBecameSuccessful {
					t.Fatalf("completion=%+v err=%v, want %s", report, completeErr, wantOutcome)
				}
				afterBuild, _ := builds.GetByID(context.Background(), request.BuildID)
				afterSteps, _ := builds.GetStepsByBuildID(context.Background(), request.BuildID)
				afterJob, _ := jobs.GetJobByID(context.Background(), request.JobID)
				if !reflect.DeepEqual(beforeBuild, afterBuild) || !reflect.DeepEqual(beforeSteps, afterSteps) || !reflect.DeepEqual(beforeJob, afterJob) {
					t.Fatal("non-completed report changed lifecycle state")
				}
			})
		}
	}
}

func TestBuildService_HandleStepResult_AtomicSynchronousPersistenceErrors(t *testing.T) {
	for _, publish := range []bool{true, false} {
		for _, status := range []runner.RunStepStatus{runner.RunStepStatusSuccess, runner.RunStepStatusFailed} {
			for _, unavailable := range []bool{true, false} {
				t.Run(fmt.Sprintf("publication=%t/%s/unavailable=%t", publish, status, unavailable), func(t *testing.T) {
					service, builds, jobs, request := newAtomicSynchronousFixture(t, true, publish)
					persistErr := errors.New("completion transaction failed")
					service.SetExecutionJobRepository(&failingAtomicCompletionRepository{ExecutionJobRepository: jobs, err: persistErr})
					if unavailable {
						service.SetExecutionJobRepository(struct {
							repository.ExecutionJobRepository
						}{jobs})
					}
					report, completeErr := service.HandleStepResult(context.Background(), request, runner.RunStepResult{Status: status})
					if report.CompletionOutcome != repository.StepCompletionInvalidTransition || report.SideEffectErr != nil || completeErr == nil {
						t.Fatalf("persistence error reported as successful lifecycle/side effect: %+v err=%v", report, completeErr)
					}
					if unavailable {
						if !strings.Contains(completeErr.Error(), "does not support atomic") {
							t.Fatal(completeErr)
						}
					} else if !errors.Is(completeErr, persistErr) {
						t.Fatalf("lost persistence error: %v", completeErr)
					}
					service.runner = &fakeRunner{result: runner.RunStepResult{Status: status}}
					_, runReport, runErr := service.RunStep(context.Background(), request)
					if runErr == nil || runErr.Error() != completeErr.Error() || runReport.SideEffectErr != nil || runReport.CompletionOutcome != repository.StepCompletionInvalidTransition {
						t.Fatalf("synchronous manager lost persistence error: report=%+v err=%v", runReport, runErr)
					}
					build, _ := builds.GetByID(context.Background(), request.BuildID)
					steps, _ := builds.GetStepsByBuildID(context.Background(), request.BuildID)
					job, _ := jobs.GetJobByID(context.Background(), request.JobID)
					if build.Status != domain.BuildStatusRunning || steps[0].Status != domain.BuildStepStatusRunning || job.Status != domain.ExecutionJobStatusRunning {
						t.Fatalf("partial completion: build=%s step=%s job=%s", build.Status, steps[0].Status, job.Status)
					}
				})
			}
		}
	}
}

type failingAtomicCompletionRepository struct {
	repository.ExecutionJobRepository
	err error
}

func (repo *failingAtomicCompletionRepository) CompleteSuccessfulStepAndJob(context.Context, repository.CompleteSuccessfulStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repo.err
}

func (repo *failingAtomicCompletionRepository) CompleteFailedStepAndJob(context.Context, repository.CompleteFailedStepAndJobRequest) (repository.CompleteStepResult, domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return repository.CompleteStepResult{}, domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, repo.err
}

func newAtomicSynchronousFixture(t *testing.T, configured, publish bool) (*BuildService, *memoryrepo.BuildRepository, *memoryrepo.ExecutionJobRepository, runner.RunStepRequest) {
	t.Helper()
	ctx := context.Background()
	claimToken := "active-claim"
	now := time.Now().UTC()
	builds := newMemoryCompletionBuildRepository(t, scmMappedBuild(domain.Build{ID: "build-atomic", JobID: strPtr("job-definition"), CommitSHA: strPtr("deadbeef"), CreatedAt: now}),
		[]domain.BuildStep{{ID: "step-atomic", StepIndex: 0, Name: "test", NodeID: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}})
	jobs := memoryrepo.NewExecutionJobRepository()
	if _, createErr := jobs.CreateJobsForBuild(ctx, []domain.ExecutionJob{{
		ID: "job-atomic", BuildID: "build-atomic", StepID: "step-atomic", NodeID: "test", Name: "test", StepIndex: 0,
		Status: domain.ExecutionJobStatusRunning, ClaimToken: &claimToken, ClaimExpiresAt: executionTestTimePointer(now.Add(time.Hour)), CreatedAt: now,
	}}); createErr != nil {
		t.Fatal(createErr)
	}
	service := NewBuildService(builds, &fakeRunner{}, &fakeLogSink{})
	if configured {
		service = NewBuildServiceFromConfig(builds, &fakeRunner{}, &fakeLogSink{}, BuildServiceConfig{ExecutionJobRepo: jobs})
	} else {
		service.SetExecutionJobRepository(jobs)
	}
	if publish {
		service.SetWorkspaceRevisionPublication(memoryrepo.NewWorkspaceRevisionRepository(jobs), &recordingWorkspaceRevisionStore{})
		service.workspaceMaterializer = &recordingExecutionWorkspaceMaterializer{}
	}
	return service, builds, jobs, runner.RunStepRequest{
		BuildID: "build-atomic", JobID: "job-atomic", StepID: "step-atomic", StepIndex: 0, StepName: "test", ClaimToken: claimToken,
	}
}
