package imagebuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

func TestControllerStagesSubmitsRenewsAndCompletesImageBuild(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
	stager := &stagerFake{source: domain.ImageBuildSource{Bucket: "sources", Object: "job-1.tar.gz", Generation: "1"}}
	controller, newErr := NewController(execution, records, builder, stager, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || !active {
		t.Fatalf("first reconcile active=%t err=%v", active, reconcileErr)
	}
	if stager.calls != 1 || builder.submitCalls != 1 || execution.renewCalls != 1 {
		t.Fatalf("stage=%d submit=%d renew=%d", stager.calls, builder.submitCalls, execution.renewCalls)
	}
	record, getErr := records.GetByExecutionJobID(context.Background(), "job-1")
	if getErr != nil || record.SubmissionState != domain.ExternalImageBuildSubmissionSubmitted || record.ExternalBuildID != "provider-1" {
		t.Fatalf("record=%+v err=%v", record, getErr)
	}

	builder.result = domain.ImageBuildResult{Status: domain.ImageBuildStatusSuccess, ImageDigest: "sha256:abc"}
	active, reconcileErr = controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("terminal reconcile active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
	record, getErr = records.GetByExecutionJobID(context.Background(), "job-1")
	if getErr != nil || record.SubmissionState != domain.ExternalImageBuildSubmissionTerminal || record.ImageDigest != "sha256:abc" {
		t.Fatalf("terminal record=%+v err=%v", record, getErr)
	}
}

func TestControllerCompletesWhenTimingPersistenceFails(t *testing.T) {
	execution := newExecutionFake(t)
	execution.timingErr = errors.New("timing store unavailable")
	createdAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	finishedAt := startedAt.Add(time.Minute)
	builder := &builderFake{result: domain.ImageBuildResult{
		Status:      domain.ImageBuildStatusSuccess,
		ImageDigest: "sha256:abc",
		Timing: &domain.ExecutionTiming{Phases: []domain.ExecutionPhaseTiming{
			{Name: "queue", StartedAt: &createdAt, FinishedAt: &startedAt},
			{Name: "command", StartedAt: &startedAt, FinishedAt: &finishedAt},
		}},
	}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
}
func TestControllerAdoptsExistingProviderBuildAndCancelsCanceledJob(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{found: true, handle: domain.ImageBuildHandle{ID: "adopted"}, result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
	controller, newErr := NewController(execution, records, builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}
	if active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step); reconcileErr != nil || !active {
		t.Fatalf("reconcile active=%t err=%v", active, reconcileErr)
	}
	if builder.submitCalls != 0 {
		t.Fatalf("submit calls=%d, want adoption only", builder.submitCalls)
	}
	execution.job.Status = domain.ExecutionJobStatusCanceled
	if active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step); reconcileErr != nil || active || builder.cancelCalls != 1 {
		t.Fatalf("cancel reconcile active=%t err=%v cancel=%d", active, reconcileErr, builder.cancelCalls)
	}
}

func TestControllerRejectsInvalidOrUnsafeTerminalStates(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*executionFake, *builderFake, *stagerFake)
		wantActive bool
		wantStatus runner.RunStepStatus
	}{
		{name: "invalid spec", configure: func(execution *executionFake, _ *builderFake, _ *stagerFake) { execution.job.ResolvedSpecJSON = "{}" }, wantStatus: runner.RunStepStatusFailed},
		{name: "stage error", configure: func(_ *executionFake, _ *builderFake, stager *stagerFake) { stager.err = errors.New("stage failed") }, wantActive: true},
		{name: "successful provider result without digest", configure: func(_ *executionFake, builder *builderFake, _ *stagerFake) {
			builder.result = domain.ImageBuildResult{Status: domain.ImageBuildStatusSuccess}
		}, wantStatus: runner.RunStepStatusFailed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			execution := newExecutionFake(t)
			builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
			stager := &stagerFake{}
			testCase.configure(execution, builder, stager)
			controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, stager, &sourceFake{})
			if newErr != nil {
				t.Fatalf("new controller: %v", newErr)
			}
			active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
			if testCase.name == "stage error" {
				if !active || !errors.Is(reconcileErr, stager.err) {
					t.Fatalf("active=%t err=%v", active, reconcileErr)
				}
				return
			}
			if reconcileErr != nil || active != testCase.wantActive || execution.result.Status != testCase.wantStatus {
				t.Fatalf("active=%t err=%v result=%+v", active, reconcileErr, execution.result)
			}
		})
	}
}

func TestControllerCompletesPermanentSubmissionError(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("invalid timeout format")}}
	controller, newErr := NewController(execution, records, builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusFailed || !strings.Contains(execution.result.Stderr, "invalid timeout format") {
		t.Fatalf("active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
	if builder.submitCalls != 1 {
		t.Fatalf("submit calls=%d", builder.submitCalls)
	}
	execution.job.Status = domain.ExecutionJobStatusFailed
	active, reconcileErr = controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || builder.submitCalls != 1 {
		t.Fatalf("retry active=%t err=%v submit calls=%d", active, reconcileErr, builder.submitCalls)
	}
}

func TestControllerKeepsRetryableSubmissionErrorActive(t *testing.T) {
	execution := newExecutionFake(t)
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if !active || !errors.Is(reconcileErr, builder.submitErr) || execution.completions != 0 || execution.renewCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d", active, reconcileErr, execution.completions, execution.renewCalls)
	}
}

func TestControllerStopsRetryableSubmissionWhenLeaseIsLost(t *testing.T) {
	execution := newExecutionFake(t)
	execution.renewLost = true
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if active || reconcileErr != nil || execution.completions != 0 || execution.renewCalls != 1 || builder.submitCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d submissions=%d", active, reconcileErr, execution.completions, execution.renewCalls, builder.submitCalls)
	}
}

func TestControllerReturnsRetryableSubmissionAndLeaseRenewalErrors(t *testing.T) {
	execution := newExecutionFake(t)
	execution.renewErr = errors.New("lease renewal unavailable")
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if !active || !errors.Is(reconcileErr, builder.submitErr) || !errors.Is(reconcileErr, execution.renewErr) || execution.completions != 0 || execution.renewCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d", active, reconcileErr, execution.completions, execution.renewCalls)
	}
}

func TestNewControllerRequiresAllDependencies(t *testing.T) {
	if _, err := NewController(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

type executionFake struct {
	job         domain.ExecutionJob
	step        workersvc.WorkerRunnableStep
	renewCalls  int
	renewLost   bool
	renewErr    error
	completions int
	result      runner.RunStepResult
	timing      domain.ExecutionTiming
	timingErr   error
}

func newExecutionFake(t *testing.T) *executionFake {
	t.Helper()
	spec, marshalErr := json.Marshal(domain.ExecutionJobSpec{ExecutionKind: domain.ExecutionKindImageBuild, RemoteImageBuild: &domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "coyote-ci/backend"}, TimeoutSeconds: 60})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return &executionFake{job: domain.ExecutionJob{ID: "job-1", BuildID: "build-1", Status: domain.ExecutionJobStatusRunning, ResolvedSpecJSON: string(spec)}, step: workersvc.WorkerRunnableStep{BuildID: "build-1", JobID: "job-1", ClaimToken: "claim"}}
}

func (f *executionFake) GetExecutionJob(context.Context, string) (domain.ExecutionJob, error) {
	return f.job, nil
}
func (f *executionFake) GetBuild(context.Context, string) (domain.Build, error) {
	return domain.Build{ID: "build-1"}, nil
}
func (f *executionFake) RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error) {
	f.renewCalls++
	if f.renewErr != nil {
		return false, f.renewErr
	}
	return !f.renewLost, nil
}
func (f *executionFake) UpdateRunnableStepTiming(_ context.Context, _ workersvc.WorkerRunnableStep, timing domain.ExecutionTiming) (bool, error) {
	f.timing = timing
	return true, f.timingErr
}
func (f *executionFake) CompleteKubernetesRunnableStep(_ context.Context, _ workersvc.WorkerRunnableStep, result runner.RunStepResult) (repository.StepCompletionOutcome, error) {
	f.completions++
	f.result = result
	return repository.StepCompletionCompleted, nil
}

type builderFake struct {
	found                    bool
	handle                   domain.ImageBuildHandle
	result                   domain.ImageBuildResult
	submitErr                error
	submitCalls, cancelCalls int
}

type retryableBuilderError struct {
	err       error
	retryable bool
}

func (e retryableBuilderError) Error() string   { return e.err.Error() }
func (e retryableBuilderError) Unwrap() error   { return e.err }
func (e retryableBuilderError) Retryable() bool { return e.retryable }

func (f *builderFake) Submit(_ context.Context, _ domain.ImageBuildRequest) (domain.ImageBuildHandle, error) {
	f.submitCalls++
	if f.submitErr != nil {
		return domain.ImageBuildHandle{}, f.submitErr
	}
	if f.handle.ID == "" {
		f.handle = domain.ImageBuildHandle{ID: "provider-1"}
	}
	return f.handle, nil
}
func (f *builderFake) Get(context.Context, domain.ImageBuildHandle) (domain.ImageBuildResult, error) {
	return f.result, nil
}
func (f *builderFake) Cancel(context.Context, domain.ImageBuildHandle) error {
	f.cancelCalls++
	return nil
}
func (f *builderFake) FindByExecutionJobID(context.Context, string) (domain.ImageBuildHandle, bool, error) {
	return f.handle, f.found, nil
}

type stagerFake struct {
	calls  int
	source domain.ImageBuildSource
	err    error
}

func (f *stagerFake) Stage(context.Context, string, io.Reader) (domain.ImageBuildSource, error) {
	f.calls++
	if f.err != nil {
		return domain.ImageBuildSource{}, f.err
	}
	if f.source.Bucket == "" {
		return domain.ImageBuildSource{Bucket: "sources", Object: "job-1", Generation: "1"}, nil
	}
	return f.source, nil
}

type sourceFake struct{}

func (*sourceFake) OpenSourceArchive(context.Context, domain.Build, domain.ExecutionJob, domain.ExecutionJobSpec) (service.WorkspacePreparePayload, error) {
	return service.WorkspacePreparePayload{Archive: io.NopCloser(bytes.NewReader([]byte("archive")))}, nil
}

var _ service.ImageBuilder = (*builderFake)(nil)
var _ service.ImageBuildSourceStager = (*stagerFake)(nil)
var _ service.WorkspaceSourceArchivePreparer = (*sourceFake)(nil)
