package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func TestStepExecutionContextBuilder_BindsPersistedJobAsExecutionPlan(t *testing.T) {
	ctx := context.Background()
	buildRepo := memoryrepo.NewBuildRepository()
	executionJobRepo := memoryrepo.NewExecutionJobRepository()

	build, createErr := buildRepo.CreateQueuedBuild(ctx, domain.Build{ID: "build-1"}, []domain.BuildStep{{
		ID:        "step-1",
		StepIndex: 0,
		Name:      "test",
		Command:   "go",
		Args:      []string{"test", "./..."},
	}})
	if createErr != nil {
		t.Fatalf("create queued build: %v", createErr)
	}

	claimToken := "claim-token"
	timeoutSeconds := 90
	refName := " main "
	_, createJobsErr := executionJobRepo.CreateJobsForBuild(ctx, []domain.ExecutionJob{{
		ID:             "job-1",
		BuildID:        build.ID,
		StepID:         "step-1",
		Name:           "verify",
		StepIndex:      0,
		Status:         domain.ExecutionJobStatusRunning,
		Image:          "golang:1.26",
		WorkingDir:     "backend",
		Command:        []string{"sh", "-c", "go test ./..."},
		Environment:    map[string]string{"GOFLAGS": "-mod=readonly"},
		TimeoutSeconds: &timeoutSeconds,
		ClaimToken:     &claimToken,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			RefName:       &refName,
			CommitSHA:     "abc123",
		},
	}})
	if createJobsErr != nil {
		t.Fatalf("create execution jobs: %v", createJobsErr)
	}

	logSink := &recordingLogSink{}
	builder := NewStepExecutionContextBuilder(StepExecutionContextBuilderDeps{
		BuildRepo:        buildRepo,
		ExecutionJobRepo: executionJobRepo,
		ResolveExecutionImage: func(domain.Build) string {
			return "fallback:latest"
		},
		LogSink: logSink,
	})

	executionContext, buildErr := builder.Build(ctx, runner.RunStepRequest{
		BuildID:    build.ID,
		StepID:     "step-1",
		Command:    "echo",
		Args:       []string{"fallback"},
		ClaimToken: claimToken,
	})
	if buildErr != nil {
		t.Fatalf("build execution context: %v", buildErr)
	}

	if executionContext.ExecutionRequest.JobID != "job-1" {
		t.Fatalf("expected job ID from persisted job, got %q", executionContext.ExecutionRequest.JobID)
	}
	if executionContext.ExecutionRequest.Command != "sh" || strings.Join(executionContext.ExecutionRequest.Args, " ") != "-c go test ./..." {
		t.Fatalf("expected command from persisted job, got command=%q args=%v", executionContext.ExecutionRequest.Command, executionContext.ExecutionRequest.Args)
	}
	if executionContext.ExecutionRequest.Env["GOFLAGS"] != "-mod=readonly" {
		t.Fatalf("expected environment from persisted job, got %#v", executionContext.ExecutionRequest.Env)
	}
	if executionContext.ExecutionRequest.TimeoutSeconds != timeoutSeconds {
		t.Fatalf("expected timeout %d, got %d", timeoutSeconds, executionContext.ExecutionRequest.TimeoutSeconds)
	}
	if executionContext.ExecutionImage != "golang:1.26" || executionContext.ExecutionRequest.Image != "golang:1.26" {
		t.Fatalf("expected persisted image, got context=%q request=%q", executionContext.ExecutionImage, executionContext.ExecutionRequest.Image)
	}
	if executionContext.BuildSource.Ref != "main" || executionContext.BuildSource.CommitSHA != "abc123" || !executionContext.BuildSource.HasSource {
		t.Fatalf("expected source from persisted job, got %+v", executionContext.BuildSource)
	}
	if executionContext.Step == nil || executionContext.Step.ID != "step-1" {
		t.Fatalf("expected selected step, got %+v", executionContext.Step)
	}
	if !executionContext.HasChunkAppender || executionContext.ChunkAppender == nil {
		t.Fatal("expected chunk appender from log sink")
	}
}

func TestStepExecutionContextBuilder_MissingAuthoritativeJobReturnsDomainError(t *testing.T) {
	ctx := context.Background()
	buildRepo := memoryrepo.NewBuildRepository()
	_, createErr := buildRepo.CreateQueuedBuild(ctx, domain.Build{ID: "build-1"}, []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "test"}})
	if createErr != nil {
		t.Fatalf("create queued build: %v", createErr)
	}

	builder := NewStepExecutionContextBuilder(StepExecutionContextBuilderDeps{
		BuildRepo:        buildRepo,
		ExecutionJobRepo: memoryrepo.NewExecutionJobRepository(),
		LogSink:          &recordingLogSink{},
	})

	_, buildErr := builder.Build(ctx, runner.RunStepRequest{BuildID: "build-1", JobID: "missing-job", StepID: "step-1"})
	if !errors.Is(buildErr, ErrExecutionJobNotFound) {
		t.Fatalf("expected ErrExecutionJobNotFound, got %v", buildErr)
	}
}

func TestStepRunner_StreamingPersistsNonEmptyChunks(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	logSink := &recordingLogSink{}
	executionContext := testExecutionContext(logSink)
	logManager := NewExecutionLogManager(logSink, executionContext)
	stepRunner := NewStepRunner(&recordingStreamingRunner{
		chunks: []runner.StepOutputChunk{
			{Stream: runner.StepOutputStreamStdout, ChunkText: "hello\n", EmittedAt: now},
			{Stream: runner.StepOutputStreamStderr, ChunkText: "warning\n", EmittedAt: now.Add(time.Second)},
			{Stream: runner.StepOutputStreamSystem, ChunkText: "   \n", EmittedAt: now.Add(2 * time.Second)},
		},
		result: runner.RunStepResult{Status: runner.RunStepStatusSuccess, ExitCode: 0, StartedAt: now, FinishedAt: now.Add(time.Second)},
	})

	outcome := stepRunner.Run(ctx, executionContext, logManager)
	if outcome.ExecutionErr != nil {
		t.Fatalf("expected no execution error, got %v", outcome.ExecutionErr)
	}
	if len(logSink.chunks) != 2 {
		t.Fatalf("expected two persisted chunks, got %#v", logSink.chunks)
	}
	if logSink.chunks[0].Stream != logs.StepLogStreamStdout || logSink.chunks[0].ChunkText != "hello" {
		t.Fatalf("unexpected stdout chunk: %+v", logSink.chunks[0])
	}
	if logSink.chunks[1].Stream != logs.StepLogStreamStderr || logSink.chunks[1].ChunkText != "warning" {
		t.Fatalf("unexpected stderr chunk: %+v", logSink.chunks[1])
	}
}

func TestStepRunner_NonStreamingBackfillsOutputAndConvertsRunError(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	logSink := &recordingLogSink{}
	executionContext := testExecutionContext(logSink)
	logManager := NewExecutionLogManager(logSink, executionContext)
	stepRunner := NewStepRunner(&recordingRunner{result: runner.RunStepResult{
		Status:     runner.RunStepStatusSuccess,
		ExitCode:   0,
		Stdout:     "line one\nline two\n",
		Stderr:     "warn\n",
		StartedAt:  now,
		FinishedAt: now.Add(time.Second),
	}})

	outcome := stepRunner.Run(ctx, executionContext, logManager)
	if outcome.ExecutionErr != nil {
		t.Fatalf("expected no execution error, got %v", outcome.ExecutionErr)
	}
	if len(logSink.chunks) != 3 {
		t.Fatalf("expected three backfilled chunks, got %#v", logSink.chunks)
	}
	if logSink.chunks[0].ChunkText != "line one" || logSink.chunks[1].ChunkText != "line two" || logSink.chunks[2].ChunkText != "warn" {
		t.Fatalf("unexpected backfilled chunks: %#v", logSink.chunks)
	}

	runErr := errors.New("runner unavailable")
	failingRunner := NewStepRunner(&recordingRunner{err: runErr})
	failureOutcome := failingRunner.Run(ctx, executionContext, logManager)
	if !errors.Is(failureOutcome.ExecutionErr, runErr) {
		t.Fatalf("expected execution error %v, got %v", runErr, failureOutcome.ExecutionErr)
	}
	if failureOutcome.Result.Status != runner.RunStepStatusFailed || failureOutcome.Result.ExitCode != -1 {
		t.Fatalf("expected failed synthetic result, got %+v", failureOutcome.Result)
	}
	if failureOutcome.Result.Stderr != runErr.Error() {
		t.Fatalf("expected run error in stderr, got %q", failureOutcome.Result.Stderr)
	}
}

func TestWorkspacePreparer_ReturnsEarlyFailureForBuildScopedPrepareError(t *testing.T) {
	ctx := context.Background()
	prepareErr := errors.New("docker create failed")
	logSink := &recordingLogSink{}
	executionContext := testExecutionContext(logSink)
	preparer := NewWorkspacePreparer(WorkspacePreparerDeps{Runner: &recordingBuildScopedRunner{prepareErr: prepareErr}})
	logManager := NewExecutionLogManager(logSink, executionContext)

	earlyResult, earlyErr, prepareHardErr := preparer.Prepare(ctx, executionContext, logManager)
	if prepareHardErr != nil {
		t.Fatalf("expected no hard prepare error, got %v", prepareHardErr)
	}
	if !errors.Is(earlyErr, prepareErr) {
		t.Fatalf("expected early error %v, got %v", prepareErr, earlyErr)
	}
	if earlyResult == nil || earlyResult.Status != runner.RunStepStatusFailed || earlyResult.ExitCode != -1 {
		t.Fatalf("expected failed early result, got %+v", earlyResult)
	}
	if earlyResult.Stderr != "docker create failed" {
		t.Fatalf("expected classified prepare reason, got %q", earlyResult.Stderr)
	}
}

func TestStepCompletionManager_JoinsSideEffectAndBufferedLogErrors(t *testing.T) {
	ctx := context.Background()
	chunkErr := errors.New("chunk append failed")
	postCompletionErr := errors.New("artifact publish failed")
	logSink := &recordingLogSink{appendErr: chunkErr}
	executionContext := testExecutionContext(logSink)
	logManager := NewExecutionLogManager(logSink, executionContext)
	_ = logManager.PersistRunnerChunk(ctx, runner.StepOutputChunk{Stream: runner.StepOutputStreamStdout, ChunkText: "output"})

	manager := NewStepCompletionManager(StepCompletionManagerDeps{
		HandleStepResult: func(context.Context, runner.RunStepRequest, runner.RunStepResult, bool) (StepCompletionReport, error) {
			return StepCompletionReport{Step: domain.BuildStep{ID: "step-1"}}, nil
		},
		RunPostCompletionSideEffects: func(context.Context, runner.RunStepRequest, logs.StepLogChunkAppender) error {
			return postCompletionErr
		},
	})

	report, completeErr := manager.CompleteExecution(ctx, executionContext, runner.RunStepResult{Status: runner.RunStepStatusSuccess}, logManager)
	if completeErr != nil {
		t.Fatalf("expected no completion error, got %v", completeErr)
	}
	if !errors.Is(report.SideEffectErr, postCompletionErr) {
		t.Fatalf("expected post-completion error in side effects, got %v", report.SideEffectErr)
	}
	if !errors.Is(report.SideEffectErr, chunkErr) {
		t.Fatalf("expected buffered chunk error in side effects, got %v", report.SideEffectErr)
	}
}

func TestExecutionLogOutput_ClassifiesFailuresAndWritesOutput(t *testing.T) {
	timeoutKind, timeoutReason := classifyExecutionStepFailure(runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "step timed out after 1m"})
	if timeoutKind != stepFailureKindTimeout || timeoutReason != "step timed out after 1m" {
		t.Fatalf("expected timeout classification, got kind=%q reason=%q", timeoutKind, timeoutReason)
	}

	exitKind, exitReason := classifyExecutionStepFailure(runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: 2})
	if exitKind != stepFailureKindExitCode || exitReason != "command exited with code 2" {
		t.Fatalf("expected exit-code classification, got kind=%q reason=%q", exitKind, exitReason)
	}

	internalKind, internalReason := classifyExecutionStepFailure(runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1})
	if internalKind != stepFailureKindInternal || internalReason != "internal execution error" {
		t.Fatalf("expected internal classification, got kind=%q reason=%q", internalKind, internalReason)
	}

	noneKind, noneReason := classifyExecutionStepFailure(runner.RunStepResult{Status: runner.RunStepStatusSuccess})
	if noneKind != stepFailureKindNone || noneReason != "" {
		t.Fatalf("expected no failure classification, got kind=%q reason=%q", noneKind, noneReason)
	}

	if line := formatExecutionFailureStepEndLine(1, 2, "test", time.Minute, -1, stepFailureKindTimeout); !strings.Contains(line, "timed out") {
		t.Fatalf("expected timeout end line, got %q", line)
	}

	logSink := &recordingLogSink{}
	writeErr := WriteExecutionOutputLogs(context.Background(), logSink, "build-1", "test", " one\r\ntwo\n\n")
	if writeErr != nil {
		t.Fatalf("write output logs: %v", writeErr)
	}
	if len(logSink.lines) != 2 || logSink.lines[0] != "one" || logSink.lines[1] != "two" {
		t.Fatalf("unexpected output log lines: %#v", logSink.lines)
	}

	blankErr := WriteExecutionSystemLogLine(context.Background(), logSink, runner.RunStepRequest{BuildID: "build-1", StepName: "test"}, nil, "   \n")
	if blankErr != nil {
		t.Fatalf("blank system line should not error: %v", blankErr)
	}
	if len(logSink.lines) != 2 {
		t.Fatalf("expected blank system line to be skipped, got %#v", logSink.lines)
	}

	systemErr := WriteExecutionSystemLogLine(context.Background(), logSink, runner.RunStepRequest{BuildID: "build-1", StepName: "test"}, nil, "system line\n")
	if systemErr != nil {
		t.Fatalf("write system line: %v", systemErr)
	}
	if logSink.lines[2] != "system line" {
		t.Fatalf("expected trimmed system line, got %#v", logSink.lines)
	}
}

func TestExecutionLogManager_EmitExecutionStart_UsesWorkspaceEnvAndFallback(t *testing.T) {
	logSink := &recordingLogSink{}
	executionContext := testExecutionContext(nil)
	executionContext.Build.StartedAt = nil
	executionContext.ExecutionRequest.Env = map[string]string{runner.EnvWorkspace: "/tmp/build-1"}
	logManager := NewExecutionLogManager(logSink, executionContext)
	logManager.EmitExecutionStart(context.Background())
	if len(logSink.lines) == 0 || logSink.lines[2] != "Workspace: /tmp/build-1" {
		t.Fatalf("expected workspace line from env, got %#v", logSink.lines)
	}

	fallbackSink := &recordingLogSink{}
	fallbackContext := testExecutionContext(nil)
	fallbackContext.Build.StartedAt = nil
	fallbackContext.ExecutionRequest.Env = map[string]string{}
	NewExecutionLogManager(fallbackSink, fallbackContext).EmitExecutionStart(context.Background())
	if len(fallbackSink.lines) == 0 || fallbackSink.lines[2] != "Workspace: /workspace" {
		t.Fatalf("expected fallback workspace line, got %#v", fallbackSink.lines)
	}
}

func TestExecutionLogManager_PersistRunnerChunk_NoAppenderAndApplyBufferedErrors(t *testing.T) {
	ctx := context.Background()
	logSink := &recordingLogSink{writeErr: errors.New("write failed")}
	executionContext := testExecutionContext(nil)
	executionContext.ExecutionRequest.Env = map[string]string{}
	manager := NewExecutionLogManager(logSink, executionContext)
	if err := manager.PersistRunnerChunk(ctx, runner.StepOutputChunk{Stream: runner.StepOutputStreamStdout, ChunkText: "output"}); err != nil {
		t.Fatalf("expected no error when no appender is present, got %v", err)
	}
	manager.EmitSystemLine(ctx, "system line")
	report := &StepCompletionReport{}
	manager.ApplyBufferedErrors(report)
	if !errors.Is(report.SideEffectErr, logSink.writeErr) {
		t.Fatalf("expected buffered visibility error in report, got %v", report.SideEffectErr)
	}
}

func TestSourceSpecFromBuild_PrefersSnapshotAndFallsBackToLegacyFields(t *testing.T) {
	sourceRef := " main "
	sourceCommit := " abc123 "
	fromSource := sourceSpecFromBuild(domain.Build{Source: &domain.SourceSpec{
		RepositoryURL: " https://github.com/acme/repo.git ",
		Ref:           &sourceRef,
		CommitSHA:     &sourceCommit,
	}})
	if !fromSource.HasSource || fromSource.RepositoryURL != "https://github.com/acme/repo.git" || fromSource.Ref != "main" || fromSource.CommitSHA != "abc123" {
		t.Fatalf("unexpected source snapshot result: %+v", fromSource)
	}

	legacyRepo := " https://github.com/acme/legacy.git "
	legacyRef := " release "
	legacyCommit := " def456 "
	fromLegacy := sourceSpecFromBuild(domain.Build{RepoURL: &legacyRepo, Ref: &legacyRef, CommitSHA: &legacyCommit})
	if !fromLegacy.HasSource || fromLegacy.RepositoryURL != "https://github.com/acme/legacy.git" || fromLegacy.Ref != "release" || fromLegacy.CommitSHA != "def456" {
		t.Fatalf("unexpected legacy source result: %+v", fromLegacy)
	}

	withoutSource := sourceSpecFromBuild(domain.Build{})
	if withoutSource.HasSource || withoutSource.RepositoryURL != "" || withoutSource.Ref != "" || withoutSource.CommitSHA != "" {
		t.Fatalf("unexpected empty source result: %+v", withoutSource)
	}
}

func testExecutionContext(logSink logs.StepLogChunkAppender) StepExecutionContext {
	now := time.Now().UTC()
	return StepExecutionContext{
		Build:          domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, StartedAt: &now},
		ExecutionImage: "alpine:3.20",
		BuildSource:    ResolvedBuildSourceSpec{RepositoryURL: "https://github.com/acme/repo.git", Ref: "main", HasSource: true},
		ExecutionRequest: runner.RunStepRequest{
			BuildID:    "build-1",
			StepID:     "step-1",
			StepIndex:  0,
			StepName:   "test",
			Command:    "sh",
			Args:       []string{"-c", "echo ok"},
			WorkingDir: ".",
		},
		StepNumber:       1,
		TotalSteps:       1,
		ChunkAppender:    logSink,
		HasChunkAppender: logSink != nil,
	}
}

type recordingLogSink struct {
	lines     []string
	chunks    []logs.StepLogChunk
	writeErr  error
	appendErr error
}

func (s *recordingLogSink) WriteStepLog(_ context.Context, _ string, _ string, line string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.lines = append(s.lines, line)
	return nil
}

func (s *recordingLogSink) AppendStepLogChunk(_ context.Context, chunk logs.StepLogChunk) (logs.StepLogChunk, error) {
	if s.appendErr != nil {
		return logs.StepLogChunk{}, s.appendErr
	}
	chunk.SequenceNo = int64(len(s.chunks) + 1)
	s.chunks = append(s.chunks, chunk)
	return chunk, nil
}

type recordingRunner struct {
	result runner.RunStepResult
	err    error
}

func (r *recordingRunner) RunStep(context.Context, runner.RunStepRequest) (runner.RunStepResult, error) {
	if r.err != nil {
		return runner.RunStepResult{}, r.err
	}
	return r.result, nil
}

type recordingStreamingRunner struct {
	recordingRunner
	chunks []runner.StepOutputChunk
	result runner.RunStepResult
	err    error
}

func (r *recordingStreamingRunner) RunStep(context.Context, runner.RunStepRequest) (runner.RunStepResult, error) {
	if r.err != nil {
		return runner.RunStepResult{}, r.err
	}
	return r.result, nil
}

func (r *recordingStreamingRunner) RunStepStream(_ context.Context, _ runner.RunStepRequest, onOutput runner.StepOutputCallback) (runner.RunStepResult, error) {
	for _, chunk := range r.chunks {
		callbackErr := onOutput(chunk)
		if callbackErr != nil {
			return runner.RunStepResult{}, callbackErr
		}
	}
	if r.err != nil {
		return runner.RunStepResult{}, r.err
	}
	return r.result, nil
}

type recordingBuildScopedRunner struct {
	recordingStreamingRunner
	prepareErr error
}

func (r *recordingBuildScopedRunner) PrepareBuild(context.Context, runner.PrepareBuildRequest) error {
	return r.prepareErr
}

func (r *recordingBuildScopedRunner) CleanupBuild(context.Context, string) error {
	return nil
}
