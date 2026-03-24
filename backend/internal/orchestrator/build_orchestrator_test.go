package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type fakeBuildStore struct {
	build         domain.Build
	steps         []domain.BuildStep
	createErr     error
	getErr        error
	updateErr     error
	updateCalls   int
	updatedID     string
	updatedStatus domain.BuildStatus
}

func (s *fakeBuildStore) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	if s.createErr != nil {
		return domain.Build{}, s.createErr
	}

	s.build = build
	return build, nil
}

func (s *fakeBuildStore) CreateQueuedBuild(_ context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	if s.createErr != nil {
		return domain.Build{}, s.createErr
	}

	build.Status = domain.BuildStatusQueued
	s.build = build
	s.steps = append([]domain.BuildStep(nil), steps...)

	return build, nil
}

func (s *fakeBuildStore) List(_ context.Context) ([]domain.Build, error) {
	if s.build.ID == "" {
		return []domain.Build{}, nil
	}

	return []domain.Build{s.build}, nil
}

func (s *fakeBuildStore) GetByID(_ context.Context, _ string) (domain.Build, error) {
	if s.getErr != nil {
		return domain.Build{}, s.getErr
	}

	return s.build, nil
}

func (s *fakeBuildStore) UpdateStatus(_ context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	s.updateCalls++
	s.updatedID = id
	s.updatedStatus = status

	if s.updateErr != nil {
		return domain.Build{}, s.updateErr
	}

	s.build.Status = status
	s.build.ErrorMessage = errorMessage
	return s.build, nil
}

func (s *fakeBuildStore) QueueBuild(_ context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	s.updateCalls++
	s.updatedID = id
	s.updatedStatus = domain.BuildStatusQueued

	if s.updateErr != nil {
		return domain.Build{}, s.updateErr
	}

	s.build.Status = domain.BuildStatusQueued
	s.steps = append([]domain.BuildStep(nil), steps...)

	return s.build, nil
}

func (s *fakeBuildStore) GetStepsByBuildID(_ context.Context, _ string) ([]domain.BuildStep, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}

	steps := make([]domain.BuildStep, len(s.steps))
	copy(steps, s.steps)
	return steps, nil
}

func (s *fakeBuildStore) ClaimStepIfPending(_ context.Context, _ string, stepIndex int, _ *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	if s.updateErr != nil {
		return domain.BuildStep{}, false, s.updateErr
	}

	for i := range s.steps {
		if s.steps[i].StepIndex != stepIndex {
			continue
		}
		if s.steps[i].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		s.steps[i].Status = domain.BuildStepStatusRunning
		s.steps[i].StartedAt = &startedAt
		return s.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (s *fakeBuildStore) UpdateStepByIndex(_ context.Context, _ string, stepIndex int, status domain.BuildStepStatus, _ *string, _ *int, _ *string, startedAt *time.Time, finishedAt *time.Time) (domain.BuildStep, error) {
	if s.updateErr != nil {
		return domain.BuildStep{}, s.updateErr
	}

	for i := range s.steps {
		if s.steps[i].StepIndex != stepIndex {
			continue
		}

		s.steps[i].Status = status
		if startedAt != nil {
			s.steps[i].StartedAt = startedAt
		}
		if finishedAt != nil {
			s.steps[i].FinishedAt = finishedAt
		}
		return s.steps[i], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (s *fakeBuildStore) UpdateCurrentStepIndex(_ context.Context, _ string, currentStepIndex int) (domain.Build, error) {
	if s.updateErr != nil {
		return domain.Build{}, s.updateErr
	}

	s.build.CurrentStepIndex = currentStepIndex
	return s.build, nil
}

type fakeRunner struct {
	result      contracts.RunStepResult
	err         error
	called      bool
	lastRequest contracts.RunStepRequest
}

func (r *fakeRunner) RunStep(_ context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error) {
	r.called = true
	r.lastRequest = request
	if r.err != nil {
		return contracts.RunStepResult{}, r.err
	}
	return r.result, nil
}

type fakeLogSink struct {
	err    error
	calls  int
	lines  []string
	builds []string
	steps  []string
}

func (s *fakeLogSink) WriteStepLog(_ context.Context, buildID string, stepName string, line string) error {
	if s.err != nil {
		return s.err
	}
	s.calls++
	s.builds = append(s.builds, buildID)
	s.steps = append(s.steps, stepName)
	s.lines = append(s.lines, line)
	return nil
}

func TestBuildOrchestrator_CreateBuild(t *testing.T) {
	tests := []struct {
		name      string
		input     CreateBuildInput
		store     *fakeBuildStore
		expectErr error
	}{
		{name: "missing project id", input: CreateBuildInput{}, store: &fakeBuildStore{}, expectErr: ErrProjectIDRequired},
		{name: "create error", input: CreateBuildInput{ProjectID: "project-1"}, store: &fakeBuildStore{createErr: errors.New("create failed")}},
		{name: "success", input: CreateBuildInput{ProjectID: "project-1"}, store: &fakeBuildStore{}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			o := NewBuildOrchestrator(tc.store, nil, nil)
			build, err := o.CreateBuild(context.Background(), tc.input)

			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}

			if tc.store.createErr != nil {
				if err == nil || err.Error() != "create failed" {
					t.Fatalf("expected create error, got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if build.ID == "" {
				t.Fatal("expected generated build id")
			}
			if build.ProjectID != tc.input.ProjectID {
				t.Fatalf("expected project id %q, got %q", tc.input.ProjectID, build.ProjectID)
			}
			if build.Status != domain.BuildStatusPending {
				t.Fatalf("expected status pending, got %q", build.Status)
			}
			if build.CreatedAt.Location() != time.UTC {
				t.Fatalf("expected UTC timestamp, got %v", build.CreatedAt.Location())
			}
		})
	}
}

func TestBuildOrchestrator_Transitions(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeBuildStore{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}}
	o := NewBuildOrchestrator(store, nil, nil)

	if _, err := o.QueueBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("queue build returned error: %v", err)
	}
	if store.build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", store.build.Status)
	}

	if _, err := o.StartBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("start build returned error: %v", err)
	}
	if store.build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected running status, got %q", store.build.Status)
	}

	if _, err := o.CompleteBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if store.build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected success status, got %q", store.build.Status)
	}

	if _, err := o.FailBuild(context.Background(), "build-1"); !errors.Is(err, ErrInvalidBuildStatusTransition) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
}

func TestBuildOrchestrator_QueueBuildWithTemplate(t *testing.T) {
	tests := []struct {
		name          string
		template      string
		expectedNames []string
	}{
		{name: "default when omitted", template: "", expectedNames: []string{"default"}},
		{name: "default explicit", template: BuildTemplateDefault, expectedNames: []string{"default"}},
		{name: "test template", template: BuildTemplateTest, expectedNames: []string{"setup", "test", "teardown"}},
		{name: "build template", template: BuildTemplateBuild, expectedNames: []string{"install", "compile"}},
		{name: "unknown falls back", template: "unknown", expectedNames: []string{"default"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeBuildStore{
				build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
			}
			o := NewBuildOrchestrator(store, nil, nil)

			if _, err := o.QueueBuildWithTemplate(context.Background(), "build-1", tc.template); err != nil {
				t.Fatalf("queue with template returned error: %v", err)
			}

			if len(store.steps) != len(tc.expectedNames) {
				t.Fatalf("expected %d steps, got %d", len(tc.expectedNames), len(store.steps))
			}

			for idx, expectedName := range tc.expectedNames {
				if store.steps[idx].StepIndex != idx {
					t.Fatalf("expected step index %d, got %d", idx, store.steps[idx].StepIndex)
				}
				if store.steps[idx].Name != expectedName {
					t.Fatalf("expected step name %q at index %d, got %q", expectedName, idx, store.steps[idx].Name)
				}
			}
		})
	}
}

func TestBuildOrchestrator_TransitionNotFound(t *testing.T) {
	store := &fakeBuildStore{getErr: repository.ErrBuildNotFound}
	o := NewBuildOrchestrator(store, nil, nil)

	_, err := o.StartBuild(context.Background(), "missing")
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	if store.updateCalls != 0 {
		t.Fatalf("expected no update call, got %d", store.updateCalls)
	}
}

func TestBuildOrchestrator_ListBuilds(t *testing.T) {
	store := &fakeBuildStore{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending}}
	o := NewBuildOrchestrator(store, nil, nil)

	builds, err := o.ListBuilds(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one build, got %d", len(builds))
	}
	if builds[0].ID != "build-1" {
		t.Fatalf("expected build-1 id, got %q", builds[0].ID)
	}
}

func TestBuildOrchestrator_GetBuildSteps_NotFound(t *testing.T) {
	store := &fakeBuildStore{getErr: repository.ErrBuildNotFound}
	o := NewBuildOrchestrator(store, nil, nil)

	_, err := o.GetBuildSteps(context.Background(), "missing")
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildOrchestrator_GetBuildLogs_NotFound(t *testing.T) {
	store := &fakeBuildStore{getErr: repository.ErrBuildNotFound}
	o := NewBuildOrchestrator(store, nil, nil)

	_, err := o.GetBuildLogs(context.Background(), "missing")
	if !errors.Is(err, repository.ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildOrchestrator_RunStep_DelegatesToRunner(t *testing.T) {
	runner := &fakeRunner{result: contracts.RunStepResult{Status: contracts.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}
	logs := &fakeLogSink{}
	orchestrator := NewBuildOrchestrator(&fakeBuildStore{build: domain.Build{ID: "build-1", CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending}}}, runner, logs)

	request := contracts.RunStepRequest{BuildID: "build-1", StepName: "test", Command: "echo", Args: []string{"ok"}}
	result, err := orchestrator.RunStep(context.Background(), request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !runner.called {
		t.Fatal("expected runner to be called")
	}
	if runner.lastRequest.Command != "echo" {
		t.Fatalf("expected command echo, got %q", runner.lastRequest.Command)
	}
	if result.Status != contracts.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if logs.calls != 1 {
		t.Fatalf("expected one log write, got %d", logs.calls)
	}
	if logs.lines[0] != "ok" {
		t.Fatalf("expected trimmed log line, got %q", logs.lines[0])
	}
}

func TestBuildOrchestrator_RunStep_RunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("runner failed")}
	orchestrator := NewBuildOrchestrator(&fakeBuildStore{build: domain.Build{ID: "build-1", CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusPending}}}, runner, &fakeLogSink{})

	_, err := orchestrator.RunStep(context.Background(), contracts.RunStepRequest{BuildID: "build-1", StepName: "echo", Command: "echo"})
	if err == nil || err.Error() != "runner failed" {
		t.Fatalf("expected runner error, got %v", err)
	}
}

func TestBuildOrchestrator_RunStep_PersistsLogsForSuccessAndFailedResults(t *testing.T) {
	tests := []struct {
		name          string
		runnerResult  contracts.RunStepResult
		expectedLines []string
	}{
		{
			name: "success output logs",
			runnerResult: contracts.RunStepResult{
				Status: contracts.RunStepStatusSuccess,
				Stdout: "line-1\nline-2\n",
				Stderr: "",
			},
			expectedLines: []string{"line-1", "line-2"},
		},
		{
			name: "failed output logs",
			runnerResult: contracts.RunStepResult{
				Status: contracts.RunStepStatusFailed,
				Stdout: "",
				Stderr: "err-1\nerr-2\n",
			},
			expectedLines: []string{"err-1", "err-2"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeBuildStore{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}, steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusPending}}}
			runner := &fakeRunner{result: tc.runnerResult}
			logStore := logs.NewMemorySink()
			o := NewBuildOrchestrator(store, runner, logStore)

			if _, err := o.RunStep(context.Background(), contracts.RunStepRequest{BuildID: "build-1", StepName: "step-1", Command: "echo"}); err != nil {
				t.Fatalf("run step failed: %v", err)
			}

			buildLogs, err := o.GetBuildLogs(context.Background(), "build-1")
			if err != nil {
				t.Fatalf("get build logs failed: %v", err)
			}
			if len(buildLogs) != len(tc.expectedLines) {
				t.Fatalf("expected %d logs, got %d", len(tc.expectedLines), len(buildLogs))
			}

			for i := range tc.expectedLines {
				if buildLogs[i].Message != tc.expectedLines[i] {
					t.Fatalf("expected log line %q at index %d, got %q", tc.expectedLines[i], i, buildLogs[i].Message)
				}
				if buildLogs[i].StepName != "step-1" {
					t.Fatalf("expected step name step-1, got %q", buildLogs[i].StepName)
				}
			}
		})
	}
}
