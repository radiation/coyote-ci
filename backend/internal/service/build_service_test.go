package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
	inprocessrunner "github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
)

type fakeBuildRepository struct {
	build         domain.Build
	steps         []domain.BuildStep
	createErr     error
	getErr        error
	updateErr     error
	updateCalls   int
	updatedID     string
	updatedStatus domain.BuildStatus
}

func (r *fakeBuildRepository) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}

	r.build = build
	return build, nil
}

func (r *fakeBuildRepository) CreateQueuedBuild(_ context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}

	build.Status = domain.BuildStatusQueued
	r.build = build
	r.steps = append([]domain.BuildStep(nil), steps...)

	return build, nil
}

func (r *fakeBuildRepository) List(_ context.Context) ([]domain.Build, error) {
	if r.build.ID == "" {
		return []domain.Build{}, nil
	}

	return []domain.Build{r.build}, nil
}

func (r *fakeBuildRepository) GetByID(_ context.Context, _ string) (domain.Build, error) {
	if r.getErr != nil {
		return domain.Build{}, r.getErr
	}

	return r.build, nil
}

func (r *fakeBuildRepository) UpdateStatus(_ context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	r.updateCalls++
	r.updatedID = id
	r.updatedStatus = status

	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.Status = status
	r.build.ErrorMessage = errorMessage
	return r.build, nil
}

func (r *fakeBuildRepository) QueueBuild(_ context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	r.updateCalls++
	r.updatedID = id
	r.updatedStatus = domain.BuildStatusQueued

	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.Status = domain.BuildStatusQueued
	r.steps = append([]domain.BuildStep(nil), steps...)

	return r.build, nil
}

func (r *fakeBuildRepository) GetStepsByBuildID(_ context.Context, _ string) ([]domain.BuildStep, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}

	steps := make([]domain.BuildStep, len(r.steps))
	copy(steps, r.steps)
	return steps, nil
}

func (r *fakeBuildRepository) ClaimStepIfPending(_ context.Context, _ string, stepIndex int, _ *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].Status = domain.BuildStepStatusRunning
		r.steps[i].StartedAt = &startedAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) ClaimPendingStep(_ context.Context, _ string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].Status = domain.BuildStepStatusRunning
		r.steps[i].WorkerID = &claim.WorkerID
		r.steps[i].ClaimToken = &claim.ClaimToken
		r.steps[i].ClaimedAt = &claim.ClaimedAt
		r.steps[i].LeaseExpiresAt = &claim.LeaseExpiresAt
		r.steps[i].StartedAt = &claim.ClaimedAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) ReclaimExpiredStep(_ context.Context, _ string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if r.steps[i].LeaseExpiresAt == nil || r.steps[i].LeaseExpiresAt.After(reclaimBefore) {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].WorkerID = &claim.WorkerID
		r.steps[i].ClaimToken = &claim.ClaimToken
		r.steps[i].ClaimedAt = &claim.ClaimedAt
		r.steps[i].LeaseExpiresAt = &claim.LeaseExpiresAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) RenewStepLease(_ context.Context, _ string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status == domain.BuildStepStatusSuccess || r.steps[i].Status == domain.BuildStepStatusFailed {
			return r.steps[i], repository.StepCompletionDuplicateTerminal, nil
		}
		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		if r.steps[i].ClaimToken == nil || *r.steps[i].ClaimToken != claimToken {
			return r.steps[i], repository.StepCompletionStaleClaim, nil
		}
		r.steps[i].LeaseExpiresAt = &leaseExpiresAt
		return r.steps[i], repository.StepCompletionCompleted, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) UpdateStepByIndex(_ context.Context, _ string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}

		r.steps[i].Status = update.Status
		if update.ExitCode != nil {
			r.steps[i].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			r.steps[i].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			r.steps[i].Stderr = update.Stderr
		}
		if update.StartedAt != nil {
			r.steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			r.steps[i].FinishedAt = update.FinishedAt
		}
		return r.steps[i], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) CompleteStepIfRunning(_ context.Context, _ string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}

		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return r.steps[i], false, nil
		}

		r.steps[i].Status = update.Status
		if update.ExitCode != nil {
			r.steps[i].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			r.steps[i].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			r.steps[i].Stderr = update.Stderr
		}
		if update.ErrorMessage != nil {
			r.steps[i].ErrorMessage = update.ErrorMessage
		} else if update.Status == domain.BuildStepStatusSuccess {
			r.steps[i].ErrorMessage = nil
		}
		if update.StartedAt != nil {
			r.steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			r.steps[i].FinishedAt = update.FinishedAt
		}

		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) CompleteStep(_ context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	buildID := request.BuildID
	stepIndex := request.StepIndex
	update := request.Update

	if request.RequireClaim {
		for i := range r.steps {
			if r.steps[i].StepIndex != stepIndex {
				continue
			}

			if r.steps[i].Status == domain.BuildStepStatusSuccess || r.steps[i].Status == domain.BuildStepStatusFailed {
				return repository.CompleteStepResult{Step: r.steps[i], Outcome: repository.StepCompletionDuplicateTerminal}, nil
			}
			if r.steps[i].Status != domain.BuildStepStatusRunning {
				return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
			}
			if r.steps[i].ClaimToken == nil || *r.steps[i].ClaimToken != request.ClaimToken {
				return repository.CompleteStepResult{Step: r.steps[i], Outcome: repository.StepCompletionStaleClaim}, nil
			}

			break
		}
	}

	step, completed, err := r.CompleteStepIfRunning(context.Background(), buildID, stepIndex, update)
	if err != nil {
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, err
	}
	if !completed {
		if step.Status == domain.BuildStepStatusSuccess || step.Status == domain.BuildStepStatusFailed {
			return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionDuplicateTerminal}, nil
		}
		if step.ID == "" && step.Name == "" {
			return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, repository.ErrBuildNotFound
		}
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
	}

	if update.Status == domain.BuildStepStatusFailed {
		r.build.Status = domain.BuildStatusFailed
		r.build.ErrorMessage = step.ErrorMessage
		return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionCompleted}, nil
	}

	nextIndex := stepIndex + 1
	if nextIndex > r.build.CurrentStepIndex {
		r.build.CurrentStepIndex = nextIndex
	}

	hasNext := false
	for idx := range r.steps {
		if r.steps[idx].StepIndex > stepIndex {
			hasNext = true
			break
		}
	}

	if !hasNext {
		r.build.Status = domain.BuildStatusSuccess
		r.build.ErrorMessage = nil
	}

	return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionCompleted}, nil
}

func (r *fakeBuildRepository) UpdateCurrentStepIndex(_ context.Context, _ string, currentStepIndex int) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.CurrentStepIndex = currentStepIndex
	return r.build, nil
}

type fakeRunner struct {
	result      steprunner.RunStepResult
	err         error
	called      bool
	lastRequest steprunner.RunStepRequest
}

func (r *fakeRunner) RunStep(_ context.Context, request steprunner.RunStepRequest) (steprunner.RunStepResult, error) {
	r.called = true
	r.lastRequest = request
	if r.err != nil {
		return steprunner.RunStepResult{}, r.err
	}
	return r.result, nil
}

type fakeBuildScopedRunner struct {
	fakeRunner
	prepareCalls int
	cleanupCalls int
	lastPrepare  steprunner.PrepareBuildRequest
	prepareErr   error
	cleanupErr   error
	onCleanup    func()
}

func (r *fakeBuildScopedRunner) PrepareBuild(_ context.Context, request steprunner.PrepareBuildRequest) error {
	r.prepareCalls++
	r.lastPrepare = request
	return r.prepareErr
}

func (r *fakeBuildScopedRunner) CleanupBuild(_ context.Context, _ string) error {
	r.cleanupCalls++
	if r.onCleanup != nil {
		r.onCleanup()
	}
	return r.cleanupErr
}

func (r *fakeBuildScopedRunner) RunStepStream(ctx context.Context, request steprunner.RunStepRequest, _ steprunner.StepOutputCallback) (steprunner.RunStepResult, error) {
	return r.RunStep(ctx, request)
}

type fakeLogSink struct {
	err    error
	calls  int
	lines  []string
	builds []string
	steps  []string
}

type fakeArtifactRepository struct {
	artifacts map[string][]domain.BuildArtifact
}

func (r *fakeArtifactRepository) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	if r.artifacts == nil {
		r.artifacts = map[string][]domain.BuildArtifact{}
	}
	r.artifacts[artifact.BuildID] = append(r.artifacts[artifact.BuildID], artifact)
	return artifact, nil
}

func (r *fakeArtifactRepository) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	items := r.artifacts[buildID]
	out := make([]domain.BuildArtifact, len(items))
	copy(out, items)
	return out, nil
}

func (r *fakeArtifactRepository) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	for _, item := range r.artifacts[buildID] {
		if item.ID == artifactID {
			return item, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

type recordingStore struct {
	events *[]string
}

func (s *recordingStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	body, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	*s.events = append(*s.events, "save:"+key)
	return int64(len(body)), nil
}

func (s *recordingStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type failingStore struct {
	events *[]string
	err    error
}

func (s *failingStore) Save(_ context.Context, key string, _ io.Reader) (int64, error) {
	if s.events != nil {
		*s.events = append(*s.events, "save:"+key)
	}
	return 0, s.err
}

func (s *failingStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, s.err
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

func TestNewBuildService(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	if svc == nil {
		t.Fatal("expected service instance, got nil")
	}
}

func TestBuildService_CreateBuild(t *testing.T) {
	tests := []struct {
		name        string
		input       CreateBuildInput
		repo        *fakeBuildRepository
		expectErr   error
		errContains string
	}{
		{
			name:      "missing project id",
			input:     CreateBuildInput{},
			repo:      &fakeBuildRepository{},
			expectErr: ErrProjectIDRequired,
		},
		{
			name:        "repository create fails",
			input:       CreateBuildInput{ProjectID: "project-1"},
			repo:        &fakeBuildRepository{createErr: errors.New("create failed")},
			errContains: "create failed",
		},
		{
			name:  "success",
			input: CreateBuildInput{ProjectID: "project-1"},
			repo:  &fakeBuildRepository{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewBuildService(tc.repo, nil, nil)

			build, err := svc.CreateBuild(context.Background(), tc.input)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}

			if tc.errContains != "" {
				if err == nil || err.Error() != tc.errContains {
					t.Fatalf("expected error %q, got %v", tc.errContains, err)
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
				t.Fatalf("expected status %q, got %q", domain.BuildStatusPending, build.Status)
			}

			if build.CreatedAt.IsZero() {
				t.Fatal("expected created_at to be set")
			}

			if build.CreatedAt.Location() != time.UTC {
				t.Fatal("expected created_at to be UTC")
			}
		})
	}
}

func TestBuildService_CreateBuild_WithStepsAutoQueues(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "checkout", Command: "git", Args: []string{"checkout", "."}, Env: map[string]string{"A": "1"}, WorkingDir: "/workspace", TimeoutSeconds: 120},
			{Name: "test"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}
	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 persisted steps, got %d", len(repo.steps))
	}
	if repo.steps[0].StepIndex != 0 || repo.steps[0].Name != "checkout" {
		t.Fatalf("expected first step checkout@0, got %s@%d", repo.steps[0].Name, repo.steps[0].StepIndex)
	}
	if repo.steps[0].Command != "git" {
		t.Fatalf("expected first step command git, got %q", repo.steps[0].Command)
	}
	if len(repo.steps[0].Args) != 2 || repo.steps[0].Args[0] != "checkout" {
		t.Fatalf("expected first step args to be persisted, got %+v", repo.steps[0].Args)
	}
	if repo.steps[0].WorkingDir != "/workspace" {
		t.Fatalf("expected first step working dir /workspace, got %q", repo.steps[0].WorkingDir)
	}
	if repo.steps[0].TimeoutSeconds != 120 {
		t.Fatalf("expected first step timeout 120, got %d", repo.steps[0].TimeoutSeconds)
	}
}

func TestBuildService_GetBuild(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		repo      *fakeBuildRepository
		buildID   string
		expectErr error
	}{
		{
			name: "success",
			repo: &fakeBuildRepository{build: domain.Build{
				ID:        "build-1",
				ProjectID: "project-1",
				Status:    domain.BuildStatusRunning,
				CreatedAt: now,
			}},
			buildID: "build-1",
		},
		{
			name:      "not found",
			repo:      &fakeBuildRepository{getErr: repository.ErrBuildNotFound},
			buildID:   "missing",
			expectErr: ErrBuildNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewBuildService(tc.repo, nil, nil)
			build, err := svc.GetBuild(context.Background(), tc.buildID)

			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if build.ID != tc.repo.build.ID {
				t.Fatalf("expected build id %q, got %q", tc.repo.build.ID, build.ID)
			}
		})
	}
}

func TestBuildService_ListBuilds(t *testing.T) {
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending}}
	svc := NewBuildService(repo, nil, nil)

	builds, err := svc.ListBuilds(context.Background())
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

func TestBuildService_ValidTransitions(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name           string
		initialStatus  domain.BuildStatus
		action         func(*BuildService, context.Context, string) (domain.Build, error)
		expectedStatus domain.BuildStatus
	}{
		{
			name:           "pending to queued",
			initialStatus:  domain.BuildStatusPending,
			action:         (*BuildService).QueueBuild,
			expectedStatus: domain.BuildStatusQueued,
		},
		{
			name:           "queued to running",
			initialStatus:  domain.BuildStatusQueued,
			action:         (*BuildService).StartBuild,
			expectedStatus: domain.BuildStatusRunning,
		},
		{
			name:           "running to success",
			initialStatus:  domain.BuildStatusRunning,
			action:         (*BuildService).CompleteBuild,
			expectedStatus: domain.BuildStatusSuccess,
		},
		{
			name:           "running to failed",
			initialStatus:  domain.BuildStatusRunning,
			action:         (*BuildService).FailBuild,
			expectedStatus: domain.BuildStatusFailed,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{
					ID:        "build-1",
					ProjectID: "project-1",
					Status:    tc.initialStatus,
					CreatedAt: now,
				},
			}

			svc := NewBuildService(repo, nil, nil)

			updated, err := tc.action(svc, context.Background(), "build-1")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if updated.Status != tc.expectedStatus {
				t.Fatalf("expected status %q, got %q", tc.expectedStatus, updated.Status)
			}

			if repo.updateCalls != 1 {
				t.Fatalf("expected UpdateStatus to be called once, got %d", repo.updateCalls)
			}

			if repo.updatedID != "build-1" {
				t.Fatalf("expected UpdateStatus id %q, got %q", "build-1", repo.updatedID)
			}

			if repo.updatedStatus != tc.expectedStatus {
				t.Fatalf("expected UpdateStatus status %q, got %q", tc.expectedStatus, repo.updatedStatus)
			}
		})
	}
}

func TestBuildService_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus domain.BuildStatus
		action        func(*BuildService, context.Context, string) (domain.Build, error)
	}{
		{
			name:          "running to queued is invalid",
			initialStatus: domain.BuildStatusRunning,
			action:        (*BuildService).QueueBuild,
		},
		{
			name:          "pending to running is invalid",
			initialStatus: domain.BuildStatusPending,
			action:        (*BuildService).StartBuild,
		},
		{
			name:          "pending to success is invalid",
			initialStatus: domain.BuildStatusPending,
			action:        (*BuildService).CompleteBuild,
		},
		{
			name:          "success to failed is invalid",
			initialStatus: domain.BuildStatusSuccess,
			action:        (*BuildService).FailBuild,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{
					ID:        "build-1",
					ProjectID: "project-1",
					Status:    tc.initialStatus,
				},
			}

			svc := NewBuildService(repo, nil, nil)

			_, err := tc.action(svc, context.Background(), "build-1")
			if !errors.Is(err, ErrInvalidBuildStatusTransition) {
				t.Fatalf("expected ErrInvalidBuildStatusTransition, got %v", err)
			}

			if repo.updateCalls != 0 {
				t.Fatalf("expected UpdateStatus to not be called, got %d", repo.updateCalls)
			}
		})
	}
}

func TestBuildService_TransitionBuildStatus_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.StartBuild(context.Background(), "missing-build")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound, got %v", err)
	}

	if repo.updateCalls != 0 {
		t.Fatalf("expected UpdateStatus to not be called, got %d", repo.updateCalls)
	}
}

func TestBuildService_TransitionBuildStatus_UpdateError(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:        "build-1",
			ProjectID: "project-1",
			Status:    domain.BuildStatusQueued,
		},
		updateErr: errors.New("update failed"),
	}

	svc := NewBuildService(repo, nil, nil)

	_, err := svc.StartBuild(context.Background(), "build-1")
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected update error, got %v", err)
	}

	if repo.updateCalls != 1 {
		t.Fatalf("expected UpdateStatus to be called once, got %d", repo.updateCalls)
	}
}

func TestBuildService_QueueBuildWithTemplate(t *testing.T) {
	tests := []struct {
		name          string
		template      string
		expectedNames []string
	}{
		{name: "default when omitted", template: "", expectedNames: []string{"default"}},
		{name: "default explicit", template: BuildTemplateDefault, expectedNames: []string{"default"}},
		{name: "test template", template: BuildTemplateTest, expectedNames: []string{"setup", "test", "teardown"}},
		{name: "build template", template: BuildTemplateBuild, expectedNames: []string{"install", "compile"}},
		{name: "fail template", template: BuildTemplateFail, expectedNames: []string{"setup", "verify"}},
		{name: "unknown falls back", template: "unknown", expectedNames: []string{"default"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
			}
			svc := NewBuildService(repo, nil, nil)

			if _, err := svc.QueueBuildWithTemplate(context.Background(), "build-1", tc.template); err != nil {
				t.Fatalf("queue with template returned error: %v", err)
			}

			if len(repo.steps) != len(tc.expectedNames) {
				t.Fatalf("expected %d steps, got %d", len(tc.expectedNames), len(repo.steps))
			}

			for idx, expectedName := range tc.expectedNames {
				if repo.steps[idx].StepIndex != idx {
					t.Fatalf("expected step index %d, got %d", idx, repo.steps[idx].StepIndex)
				}
				if repo.steps[idx].Name != expectedName {
					t.Fatalf("expected step name %q at index %d, got %q", expectedName, idx, repo.steps[idx].Name)
				}
			}
		})
	}
}

func TestBuildService_QueueBuildWithTemplate_FailTemplateCommands(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	if _, err := svc.QueueBuildWithTemplate(context.Background(), "build-1", BuildTemplateFail); err != nil {
		t.Fatalf("queue with fail template returned error: %v", err)
	}

	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 fail-template steps, got %d", len(repo.steps))
	}
	if len(repo.steps[0].Args) < 2 || !strings.Contains(repo.steps[0].Args[1], "exit 0") {
		t.Fatalf("expected first step script to include exit 0, got %+v", repo.steps[0].Args)
	}
	if len(repo.steps[1].Args) < 2 || !strings.Contains(repo.steps[1].Args[1], "exit 1") {
		t.Fatalf("expected second step script to include exit 1, got %+v", repo.steps[1].Args)
	}
}

func TestBuildService_QueueBuildWithTemplate_CustomTemplateCommands(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	customSteps := []QueueBuildCustomStepInput{
		{Command: "echo ok && exit 0"},
		{Name: "failure", Command: "echo fail && exit 1"},
	}

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, customSteps); err != nil {
		t.Fatalf("queue with custom template returned error: %v", err)
	}

	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 custom steps, got %d", len(repo.steps))
	}
	if repo.steps[0].Name != "step-1" {
		t.Fatalf("expected generated first step name step-1, got %q", repo.steps[0].Name)
	}
	if len(repo.steps[0].Args) < 2 || repo.steps[0].Args[0] != "-c" || repo.steps[0].Args[1] != "echo ok && exit 0" {
		t.Fatalf("expected first step to run via sh -c with command, got %+v", repo.steps[0].Args)
	}
	if repo.steps[1].Name != "failure" {
		t.Fatalf("expected explicit step name to persist, got %q", repo.steps[1].Name)
	}
	if len(repo.steps[1].Args) < 2 || repo.steps[1].Args[1] != "echo fail && exit 1" {
		t.Fatalf("expected second step command to persist, got %+v", repo.steps[1].Args)
	}
}

func TestBuildService_QueueBuildWithTemplate_CustomTemplateValidation(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, nil); !errors.Is(err, ErrCustomTemplateStepsRequired) {
		t.Fatalf("expected ErrCustomTemplateStepsRequired, got %v", err)
	}

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, []QueueBuildCustomStepInput{{Name: "bad", Command: "  "}}); !errors.Is(err, ErrCustomTemplateStepCommandRequired) {
		t.Fatalf("expected ErrCustomTemplateStepCommandRequired, got %v", err)
	}
}

func TestBuildService_GetBuildSteps_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.GetBuildSteps(context.Background(), "missing")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildService_GetBuildLogs_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.GetBuildLogs(context.Background(), "missing")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildService_RunStep_DelegatesToRunner(t *testing.T) {
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}
	logSink := &fakeLogSink{}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
	svc := NewBuildService(repo, runner, logSink)

	request := steprunner.RunStepRequest{BuildID: "build-1", StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}}
	result, report, err := svc.RunStep(context.Background(), request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	if !runner.called {
		t.Fatal("expected runner to be called")
	}
	if runner.lastRequest.Command != "echo" {
		t.Fatalf("expected command echo, got %q", runner.lastRequest.Command)
	}
	if result.Status != steprunner.RunStepStatusSuccess {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if repo.steps[0].ExitCode == nil || *repo.steps[0].ExitCode != 0 {
		t.Fatalf("expected persisted exit code 0, got %v", repo.steps[0].ExitCode)
	}
	if repo.steps[0].Stdout == nil || *repo.steps[0].Stdout != "ok\n" {
		t.Fatalf("expected persisted stdout ok, got %v", repo.steps[0].Stdout)
	}
	if logSink.calls == 0 {
		t.Fatal("expected at least one log write")
	}
	foundOutput := false
	for _, line := range logSink.lines {
		if line == "ok" {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Fatalf("expected output line 'ok' in logs, got %#v", logSink.lines)
	}
}

func TestBuildService_RunStep_PreparesBuildScopedEnvironmentWithRepoMetadataAndDefaultImageFallback(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repoURL := "https://github.com/org/repo.git"
	ref := "main"
	commitSHA := "abc123"
	buildID := "build-repo-reuse"

	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, RepoURL: &repoURL, Ref: &ref, CommitSHA: &commitSHA},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(buildRepo, runner, &fakeLogSink{})
	svc.SetDefaultExecutionImage("golang:1.23-alpine")

	request := steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "backend"}
	if _, _, err := svc.RunStep(context.Background(), request); err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if runner.prepareCalls != 1 {
		t.Fatalf("expected one prepare call, got %d", runner.prepareCalls)
	}
	if runner.lastPrepare.BuildID != buildID {
		t.Fatalf("expected prepare build id %q, got %q", buildID, runner.lastPrepare.BuildID)
	}
	if runner.lastPrepare.RepoURL != repoURL {
		t.Fatalf("expected prepare repo url %q, got %q", repoURL, runner.lastPrepare.RepoURL)
	}
	if runner.lastPrepare.Ref != ref {
		t.Fatalf("expected prepare ref %q, got %q", ref, runner.lastPrepare.Ref)
	}
	if runner.lastPrepare.CommitSHA != commitSHA {
		t.Fatalf("expected prepare commit sha %q, got %q", commitSHA, runner.lastPrepare.CommitSHA)
	}
	if runner.lastPrepare.Image != "golang:1.23-alpine" {
		t.Fatalf("expected default execution image fallback, got %q", runner.lastPrepare.Image)
	}
}

func TestBuildService_RunStep_PreparesBuildScopedEnvironmentWithPipelineImageOverride(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repoURL := "https://github.com/org/repo.git"
	ref := "main"
	commitSHA := "abc123"
	buildID := "build-repo-override"
	pipelineYAML := `
version: 1
pipeline:
  name: backend-ci
  image: golang:1.24
steps:
  - name: test
    run: go test ./...
`

	buildRepo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, RepoURL: &repoURL, Ref: &ref, CommitSHA: &commitSHA, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(buildRepo, runner, &fakeLogSink{})
	svc.SetDefaultExecutionImage("alpine:3.20")

	request := steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "test", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}, WorkingDir: "backend"}
	if _, _, err := svc.RunStep(context.Background(), request); err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if runner.prepareCalls != 1 {
		t.Fatalf("expected one prepare call, got %d", runner.prepareCalls)
	}
	if runner.lastPrepare.Image != "golang:1.24" {
		t.Fatalf("expected pipeline execution image override, got %q", runner.lastPrepare.Image)
	}
}

func TestBuildService_RunStep_CleansUpBuildScopedEnvironmentOnTerminalBuild(t *testing.T) {
	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-terminal", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-terminal", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion to persist, got %q", report.CompletionOutcome)
	}
	if runner.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once for terminal build, got %d", runner.cleanupCalls)
	}
}

func TestBuildService_RunStep_CollectsArtifactsBeforeCleanup(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	events := make([]string, 0)
	runner.onCleanup = func() {
		events = append(events, "cleanup")
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, &recordingStore{events: &events}, workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	if len(events) < 2 {
		t.Fatalf("expected artifact save and cleanup events, got %#v", events)
	}
	if !strings.HasPrefix(events[0], "save:") {
		t.Fatalf("expected first event to be artifact save, got %#v", events)
	}
	if events[len(events)-1] != "cleanup" {
		t.Fatalf("expected cleanup after artifact collection, got %#v", events)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 1 {
		t.Fatalf("expected one persisted artifact, got %d", len(artifacts))
	}
	if artifacts[0].LogicalPath != "dist/app" {
		t.Fatalf("expected logical path dist/app, got %q", artifacts[0].LogicalPath)
	}
}

func TestBuildService_RunStep_MissingArtifactPathsDoNotFailBuild(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	if err := os.MkdirAll(filepath.Join(workspaceRoot, buildID), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, artifact.NewFilesystemStore(t.TempDir()), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected no side effect error for missing artifact paths, got %v", report.SideEffectErr)
	}
	if runner.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", runner.cleanupCalls)
	}
	if len(artifactRepo.artifacts[buildID]) != 0 {
		t.Fatalf("expected no persisted artifacts for unmatched paths, got %d", len(artifactRepo.artifacts[buildID]))
	}
}

func TestBuildService_RunStep_ConvergesAfterPartialArtifactPersistence(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"
	now := time.Now().UTC()

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspacePath, "reports"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-one"), 0o644); err != nil {
		t.Fatalf("failed writing dist artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "reports", "junit.xml"), []byte("artifact-two"), 0o644); err != nil {
		t.Fatalf("failed writing report artifact: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	events := make([]string, 0)
	runner.onCleanup = func() {
		events = append(events, "cleanup")
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		buildID: {
			{ID: "existing", BuildID: buildID, LogicalPath: "dist/app", StorageKey: buildID + "/dist/app", CreatedAt: now},
		},
	}}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, &recordingStore{events: &events}, workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 2 {
		t.Fatalf("expected two artifacts after convergence, got %d", len(artifacts))
	}

	seen := map[string]int{}
	for _, item := range artifacts {
		seen[item.LogicalPath]++
	}
	if seen["dist/app"] != 1 || seen["reports/junit.xml"] != 1 {
		t.Fatalf("expected one entry per logical path, got %#v", seen)
	}

	saveCount := 0
	for _, event := range events {
		if strings.HasPrefix(event, "save:") {
			saveCount++
		}
	}
	if saveCount != 1 {
		t.Fatalf("expected only one save for missing artifact, got %d events=%#v", saveCount, events)
	}
}

func TestBuildService_RunStep_SkipsCleanupWhenArtifactCollectionFails(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-terminal"
	claimToken := "claim-active"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	runner := &fakeBuildScopedRunner{fakeRunner: fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", Stderr: ""}}}
	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}

	svc := NewBuildService(repo, runner, &fakeLogSink{})
	svc.SetArtifactPersistence(&fakeArtifactRepository{}, &failingStore{err: errors.New("store unavailable")}, workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: buildID, StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo", Args: []string{"ok"}})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error from artifact collection failure")
	}
	if runner.cleanupCalls != 0 {
		t.Fatalf("expected cleanup to be skipped on artifact failure, got %d", runner.cleanupCalls)
	}
}

func TestBuildService_CollectArtifactsIfTerminal_IsIdempotent(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-idempotent"

	workspacePath := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(filepath.Join(workspacePath, "dist"), 0o755); err != nil {
		t.Fatalf("failed creating workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "dist", "app"), []byte("artifact-body"), 0o644); err != nil {
		t.Fatalf("failed writing artifact file: %v", err)
	}

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusSuccess, CurrentStepIndex: 1, PipelineConfigYAML: &pipelineYAML},
	}
	events := make([]string, 0)
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, nil, &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, &recordingStore{events: &events}, workspaceRoot)

	if _, err := svc.collectArtifactsIfTerminal(context.Background(), buildID); err != nil {
		t.Fatalf("expected first collection to succeed, got %v", err)
	}
	if _, err := svc.collectArtifactsIfTerminal(context.Background(), buildID); err != nil {
		t.Fatalf("expected second collection to succeed, got %v", err)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 1 {
		t.Fatalf("expected one persisted artifact without duplicates, got %d", len(artifacts))
	}

	saveCount := 0
	for _, event := range events {
		if strings.HasPrefix(event, "save:") {
			saveCount++
		}
	}
	if saveCount != 1 {
		t.Fatalf("expected one storage save across repeated runs, got %d events=%#v", saveCount, events)
	}
}

func TestBuildService_RunStep_InprocessRunner_PersistsArtifactsToStorageRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	buildID := "build-inprocess"
	claimToken := "claim-active"

	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n    - reports/*.xml\n"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, PipelineConfigYAML: &pipelineYAML},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	artifactRepo := &fakeArtifactRepository{}

	svc := NewBuildService(repo, inprocessrunner.NewWithWorkspaceRoot(workspaceRoot), &fakeLogSink{})
	svc.SetArtifactPersistence(artifactRepo, artifact.NewFilesystemStore(storageRoot), workspaceRoot)

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    buildID,
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		WorkingDir: ".",
		Command:    "sh",
		Args: []string{
			"-c",
			"mkdir -p dist reports && echo 'hello world' > dist/hello.txt && echo '{\"ok\":true}' > dist/result.json && echo '<testsuite></testsuite>' > reports/test.xml",
		},
	})
	if err != nil {
		t.Fatalf("expected run to succeed, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}

	artifacts := artifactRepo.artifacts[buildID]
	if len(artifacts) != 3 {
		t.Fatalf("expected three persisted artifacts, got %d", len(artifacts))
	}

	expectedStoragePaths := []string{
		filepath.Join(storageRoot, buildID, "dist", "hello.txt"),
		filepath.Join(storageRoot, buildID, "dist", "result.json"),
		filepath.Join(storageRoot, buildID, "reports", "test.xml"),
	}
	for _, expected := range expectedStoragePaths {
		if _, statErr := os.Stat(expected); statErr != nil {
			t.Fatalf("expected persisted artifact at %s, stat failed: %v", expected, statErr)
		}
	}
}

func TestBuildService_RunStep_RunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("runner failed")}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
	svc := NewBuildService(repo, runner, &fakeLogSink{})

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "echo", ClaimToken: claimToken, Command: "echo"})
	if err == nil || err.Error() != "runner failed" {
		t.Fatalf("expected runner error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}
}

func TestBuildService_RunStep_ReturnsExecutionResultWhenCompletionPersistenceFails(t *testing.T) {
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom"}}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build:     domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps:     []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
		updateErr: errors.New("persist failed"),
	}
	svc := NewBuildService(repo, runner, &fakeLogSink{})

	result, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "echo", ClaimToken: claimToken, Command: "sh", Args: []string{"-c", "exit 1"}})
	if err == nil || err.Error() != "persist failed" {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatalf("expected invalid transition outcome on persistence error, got %q", report.CompletionOutcome)
	}
	if result.Status != steprunner.RunStepStatusFailed {
		t.Fatalf("expected failed status from runner result, got %q", result.Status)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected runner exit code 7, got %d", result.ExitCode)
	}
}

func TestBuildService_RunStep_PersistsLogsForSuccessAndFailedResults(t *testing.T) {
	tests := []struct {
		name          string
		runnerResult  steprunner.RunStepResult
		expectedLines []string
	}{
		{
			name: "success output logs",
			runnerResult: steprunner.RunStepResult{
				Status: steprunner.RunStepStatusSuccess,
				Stdout: "line-1\nline-2\n",
				Stderr: "",
			},
			expectedLines: []string{"line-1", "line-2"},
		},
		{
			name: "failed output logs",
			runnerResult: steprunner.RunStepResult{
				Status: steprunner.RunStepStatusFailed,
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

			claimToken := "claim-active"
			repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}, steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
			runner := &fakeRunner{result: tc.runnerResult}
			logStore := logs.NewMemorySink()
			svc := NewBuildService(repo, runner, logStore)

			if _, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "step-1", ClaimToken: claimToken, Command: "echo"}); err != nil {
				t.Fatalf("run step failed: %v", err)
			} else if report.CompletionOutcome != repository.StepCompletionCompleted {
				t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
			} else if report.SideEffectErr != nil {
				t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
			}

			buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
			if err != nil {
				t.Fatalf("get build logs failed: %v", err)
			}
			for _, expectedLine := range tc.expectedLines {
				found := false
				for _, buildLog := range buildLogs {
					if buildLog.Message == expectedLine {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected log line %q in logs, got %#v", expectedLine, buildLogs)
				}
			}
		})
	}
}

func TestBuildService_RunStep_WritesStructuredStepAndBuildMarkers(t *testing.T) {
	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(800 * time.Millisecond)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: startedAt.Add(-2 * time.Second)},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "echo \"hello\""},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"Starting build",
		"Pipeline image: golang:1.26",
		"Workspace: /workspace",
		"Steps: 1",
		"==> Step 1/1: step-1",
		"Image: golang:1.26",
		"Working directory: /workspace",
		"Command:",
		"echo \"hello\"",
		"<== Step 1/1: step-1 succeeded in 0.8s",
		"Build succeeded in",
		"Artifacts collected: 0",
	)
}

func TestBuildService_RunStep_WritesFailureMarkerWithExitCode(t *testing.T) {
	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(4200 * time.Millisecond)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: startedAt.Add(-2 * time.Second)},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 1, Stderr: "boom\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepIndex:  0,
		StepName:   "test",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "go test ./..."},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs failed: %v", err)
	}

	messages := make([]string, 0, len(buildLogs))
	for _, line := range buildLogs {
		messages = append(messages, line.Message)
	}

	assertMessagesContain(t, messages,
		"<== Step 1/1: test failed in 4.2s (exit code 1)",
		"Build failed in",
		"Failure summary: see failed step marker(s) above for exit details",
	)
}

func TestTerminalBuildSummaryDuration_PrefersDeterministicTimestamps(t *testing.T) {
	now := time.Date(2026, time.March, 30, 14, 0, 0, 0, time.UTC)
	created := now.Add(-10 * time.Second)
	started := now.Add(-8 * time.Second)
	finished := now.Add(-3 * time.Second)

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created, StartedAt: &started, FinishedAt: &finished}, now); got != 5*time.Second {
		t.Fatalf("expected finished-started duration of 5s, got %s", got)
	}

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created, FinishedAt: &finished}, now); got != 7*time.Second {
		t.Fatalf("expected finished-created duration of 7s, got %s", got)
	}

	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: created}, now); got != 10*time.Second {
		t.Fatalf("expected fallback now-created duration of 10s, got %s", got)
	}

	futureCreated := now.Add(2 * time.Second)
	if got := terminalBuildSummaryDuration(domain.Build{CreatedAt: futureCreated}, now); got != 0 {
		t.Fatalf("expected non-negative fallback duration, got %s", got)
	}
}

func TestBuildService_RunStep_EmitsTerminalSummaryInStepChunkFlow(t *testing.T) {
	createdAt := time.Now().UTC().Add(-10 * time.Second)
	startedAt := createdAt.Add(2 * time.Second)
	finishedAt := startedAt.Add(3 * time.Second)

	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: createdAt},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	r := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: startedAt, FinishedAt: finishedAt}}
	logStore := logs.NewMemorySink()

	svc := NewBuildService(repo, r, logStore)
	svc.SetDefaultExecutionImage("golang:1.26")

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
		BuildID:    "build-1",
		StepID:     "step-id-1",
		StepIndex:  0,
		StepName:   "step-1",
		ClaimToken: claimToken,
		Command:    "sh",
		Args:       []string{"-c", "echo hello"},
	})
	if err != nil {
		t.Fatalf("run step failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}

	chunks, err := svc.GetStepLogChunks(context.Background(), "build-1", 0, 0, 500)
	if err != nil {
		t.Fatalf("get step log chunks failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected step chunks to be persisted")
	}

	if !containsSystemChunkText(chunks, "==> Step 1/1: step-1") {
		t.Fatalf("expected step marker in chunk flow, got %#v", chunks)
	}
	if !containsSystemChunkText(chunks, "Build succeeded in") {
		t.Fatalf("expected terminal summary in chunk flow, got %#v", chunks)
	}
}

func assertMessagesContain(t *testing.T, messages []string, expected ...string) {
	t.Helper()
	for _, want := range expected {
		found := false
		for _, got := range messages {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected log message containing %q, got %#v", want, messages)
		}
	}
}

func containsSystemChunkText(chunks []logs.StepLogChunk, needle string) bool {
	for _, chunk := range chunks {
		if chunk.Stream == logs.StepLogStreamSystem && strings.Contains(chunk.ChunkText, needle) {
			return true
		}
	}
	return false
}

func TestBuildService_HandleStepResult_PersistedCompletionWithSideEffectFailure(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logErr := errors.New("log sink unavailable")
	svc := NewBuildService(repo, nil, &fakeLogSink{err: logErr})

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error to be reported")
	}
	if !errors.Is(report.SideEffectErr, logErr) {
		t.Fatalf("expected side effect error %v, got %v", logErr, report.SideEffectErr)
	}
	if repo.steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success to remain persisted, got %q", repo.steps[0].Status)
	}
}

func TestBuildService_RunStep_ReportsSideEffectFailureSeparately(t *testing.T) {
	claimToken := "claim-active"
	runner := &fakeRunner{result: steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}}
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logErr := errors.New("write failed")
	svc := NewBuildService(repo, runner, &fakeLogSink{err: logErr})

	_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken, Command: "echo"})
	if err != nil {
		t.Fatalf("expected nil error from run step, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
	}
	if report.SideEffectErr == nil {
		t.Fatal("expected side effect error to be reported")
	}
	if !errors.Is(report.SideEffectErr, logErr) {
		t.Fatalf("expected side effect error %v, got %v", logErr, report.SideEffectErr)
	}
}

func TestBuildService_RunStep_PersistsChunkLogsWithoutStepID(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedText string
		expectedType logs.StepLogStream
	}{
		{
			name:         "echo stdout",
			args:         []string{"-c", "echo hello"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStdout,
		},
		{
			name:         "printf without newline",
			args:         []string{"-c", "printf hello"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStdout,
		},
		{
			name:         "stderr output",
			args:         []string{"-c", "echo hello 1>&2"},
			expectedText: "hello",
			expectedType: logs.StepLogStreamStderr,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			buildID := "build-" + strings.ReplaceAll(tc.name, " ", "-")
			claimToken := "claim-active"
			repo := &fakeBuildRepository{
				build: domain.Build{ID: buildID, Status: domain.BuildStatusRunning, CurrentStepIndex: 0, CreatedAt: time.Now().UTC()},
				steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
			}
			logStore := logs.NewMemorySink()
			svc := NewBuildService(repo, inprocessrunner.New(), logStore)

			_, report, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{
				BuildID:    buildID,
				StepIndex:  0,
				StepName:   "step-1",
				ClaimToken: claimToken,
				Command:    "sh",
				Args:       tc.args,
			})
			if err != nil {
				t.Fatalf("run step failed: %v", err)
			}
			if report.CompletionOutcome != repository.StepCompletionCompleted {
				t.Fatalf("expected completion outcome completed, got %q", report.CompletionOutcome)
			}

			chunks, err := svc.GetStepLogChunks(context.Background(), buildID, 0, 0, 100)
			if err != nil {
				t.Fatalf("get step log chunks failed: %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("expected persisted step log chunks")
			}
			found := false
			for _, chunk := range chunks {
				if chunk.ChunkText == tc.expectedText && chunk.Stream == tc.expectedType {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected chunk text/stream (%q/%q), got %#v", tc.expectedText, tc.expectedType, chunks)
			}
		})
	}
}

func TestBuildService_HandleStepResult_DuplicateCompletionIsNoOp(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logStore := logs.NewMemorySink()
	svc := NewBuildService(repo, nil, logStore)
	request := steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}
	result := steprunner.RunStepResult{
		Status:     steprunner.RunStepStatusSuccess,
		ExitCode:   0,
		Stdout:     "ok\n",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}

	report, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected first completion to complete step")
	}
	if report.SideEffectErr != nil {
		t.Fatalf("expected nil side effect error, got %v", report.SideEffectErr)
	}
	if repo.build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index to advance to 1, got %d", repo.build.CurrentStepIndex)
	}

	buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after first completion failed: %v", err)
	}
	if len(buildLogs) != 1 {
		t.Fatalf("expected one log after first completion, got %d", len(buildLogs))
	}

	report, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate completion should be no-op, got error %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate completion to be no-op")
	}
	if repo.build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index to remain 1, got %d", repo.build.CurrentStepIndex)
	}

	buildLogs, err = svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after duplicate completion failed: %v", err)
	}
	if len(buildLogs) != 1 {
		t.Fatalf("expected duplicate completion to not write extra logs, got %d", len(buildLogs))
	}
}

func TestBuildService_HandleStepResult_MultiStepSuccessDoesNotCompleteBuild(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion to persist")
	}
	if repo.build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected build to remain running, got %q", repo.build.Status)
	}
	if repo.steps[1].Status != domain.BuildStepStatusPending {
		t.Fatalf("expected second step to remain pending/runnable, got %q", repo.steps[1].Status)
	}
}

func TestBuildService_HandleStepResult_FailureMarksBuildFailed(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{
			{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken},
			{StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending},
		},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion to persist")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed, got %q", repo.build.Status)
	}
	if repo.steps[1].Status != domain.BuildStepStatusPending {
		t.Fatalf("expected later step to remain pending after fail-fast, got %q", repo.steps[1].Status)
	}
}

func TestBuildService_HandleStepResult_DuplicateFailureDoesNotDuplicateLogsOrFinalizeTwice(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	logStore := logs.NewMemorySink()
	svc := NewBuildService(repo, nil, logStore)
	request := steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}
	result := steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 9, Stderr: "boom\n", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}

	report, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first failure completion failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected first failure completion to persist")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed after first completion, got %q", repo.build.Status)
	}

	logsAfterFirst, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after first failure failed: %v", err)
	}
	if len(logsAfterFirst) != 1 {
		t.Fatalf("expected one log line after first failure, got %d", len(logsAfterFirst))
	}

	report, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate failure completion should be no-op, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate failure completion to be no-op")
	}
	if repo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build status to remain failed, got %q", repo.build.Status)
	}

	logsAfterDuplicate, err := svc.GetBuildLogs(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("get build logs after duplicate failure failed: %v", err)
	}
	if len(logsAfterDuplicate) != 1 {
		t.Fatalf("expected duplicate failure to not write extra logs, got %d", len(logsAfterDuplicate))
	}
}

func TestBuildService_HandleStepResult_NonRunningStepReturnsInvalidStepTransition(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusPending}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-any"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected non-running completion to not complete")
	}
}

func TestBuildService_HandleStepResult_MissingClaimTokenReturnsInvalidTransition(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected completion without claim token to be rejected")
	}
}

func TestBuildService_HandleStepResult_StaleClaimReturnsStaleOutcome(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-stale"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionStaleClaim {
		t.Fatal("expected stale claim completion to be rejected")
	}
}

func TestBuildService_HandleStepResult_ClaimedCompletionFinalizesBuild(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-active"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatal("expected claimed completion to persist")
	}
	if report.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", report.Step.Status)
	}
	if repo.build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build success, got %q", repo.build.Status)
	}
}

func TestBuildService_RenewStepLease_StaleClaimReturnsDomainError(t *testing.T) {
	active := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &active}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	_, renewed, err := svc.RenewStepLease(context.Background(), "build-1", 0, "claim-stale", time.Now().UTC().Add(time.Minute))
	if !errors.Is(err, ErrStaleStepClaim) {
		t.Fatalf("expected ErrStaleStepClaim, got %v", err)
	}
	if renewed {
		t.Fatal("expected stale renewal to fail")
	}
}

func TestBuildService_RenewStepLease_SucceedsForActiveClaim(t *testing.T) {
	active := "claim-active"
	lease := time.Now().UTC().Add(time.Minute)
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &active}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	step, renewed, err := svc.RenewStepLease(context.Background(), "build-1", 0, "claim-active", lease)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !renewed {
		t.Fatal("expected renewal success")
	}
	if step.LeaseExpiresAt == nil || !step.LeaseExpiresAt.Equal(lease) {
		t.Fatalf("expected lease extension to %s, got %v", lease, step.LeaseExpiresAt)
	}
}

func TestBuildService_CreateBuildFromPipeline(t *testing.T) {
	validYAML := `
version: 1
pipeline:
  name: backend-ci
env:
  CI: "true"
steps:
  - name: Lint
    run: golangci-lint run
    working_dir: backend
    timeout_seconds: 300
    env:
      LINT_STRICT: "1"
  - name: Test
    run: go test ./...
`

	t.Run("creates queued build with correct steps", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: validYAML,
			SourcePath:   ".coyote/pipeline.yml",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if build.Status != domain.BuildStatusQueued {
			t.Errorf("expected queued status, got %s", build.Status)
		}
		if build.ProjectID != "proj-1" {
			t.Errorf("expected project_id proj-1, got %s", build.ProjectID)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline_config_yaml to be set")
		}
		if build.PipelineName == nil || *build.PipelineName != "backend-ci" {
			t.Errorf("expected pipeline_name backend-ci, got %v", build.PipelineName)
		}
		if build.PipelineSource == nil || *build.PipelineSource != ".coyote/pipeline.yml" {
			t.Errorf("expected pipeline_source, got %v", build.PipelineSource)
		}

		if len(repo.steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(repo.steps))
		}

		lint := repo.steps[0]
		if lint.Name != "Lint" {
			t.Errorf("step 0 name: got %q", lint.Name)
		}
		if lint.Command != "sh" || len(lint.Args) < 2 || lint.Args[1] != "golangci-lint run" {
			t.Errorf("step 0 command not resolved correctly: %s %v", lint.Command, lint.Args)
		}
		if lint.WorkingDir != "backend" {
			t.Errorf("step 0 working_dir: got %q", lint.WorkingDir)
		}
		if lint.TimeoutSeconds != 300 {
			t.Errorf("step 0 timeout: got %d", lint.TimeoutSeconds)
		}
		if lint.Env["CI"] != "true" {
			t.Errorf("step 0 should inherit pipeline env CI=true")
		}
		if lint.Env["LINT_STRICT"] != "1" {
			t.Errorf("step 0 should have LINT_STRICT=1")
		}

		test := repo.steps[1]
		if test.Name != "Test" {
			t.Errorf("step 1 name: got %q", test.Name)
		}
		if test.Env["CI"] != "true" {
			t.Errorf("step 1 should inherit pipeline env CI=true")
		}
	})

	t.Run("missing project_id", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			PipelineYAML: validYAML,
		})
		if !errors.Is(err, ErrProjectIDRequired) {
			t.Errorf("expected ErrProjectIDRequired, got %v", err)
		}
	})

	t.Run("empty yaml", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: "",
		})
		if !errors.Is(err, ErrPipelineYAMLRequired) {
			t.Errorf("expected ErrPipelineYAMLRequired, got %v", err)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: "version: 2\nsteps:\n  - name: X\n    run: echo",
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "version") {
			t.Errorf("expected version error, got: %v", err)
		}
	})

	t.Run("env merge step overrides pipeline", func(t *testing.T) {
		yaml := `
version: 1
env:
  SHARED: from-pipeline
steps:
  - name: Step1
    run: echo
    env:
      SHARED: from-step
`
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		_, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: yaml,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.steps[0].Env["SHARED"] != "from-step" {
			t.Errorf("step env should override pipeline env, got %q", repo.steps[0].Env["SHARED"])
		}
	})

	t.Run("pipeline snapshot persisted", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		build, err := svc.CreateBuildFromPipeline(context.Background(), CreatePipelineBuildInput{
			ProjectID:    "proj-1",
			PipelineYAML: validYAML,
			SourcePath:   ".coyote/pipeline.yml",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline YAML snapshot")
		}
		if !strings.Contains(*build.PipelineConfigYAML, "golangci-lint") {
			t.Error("pipeline YAML snapshot should contain original YAML content")
		}
	})
}

// fakeRepoFetcher implements source.RepoFetcher for testing.
type fakeRepoFetcher struct {
	localPath string
	commitSHA string
	err       error
	calls     int
}

func (f *fakeRepoFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	f.calls++
	if f.err != nil {
		return "", "", f.err
	}
	return f.localPath, f.commitSHA, nil
}

func TestBuildService_CreateBuildFromRepo(t *testing.T) {
	// Set up a temp dir with a valid pipeline file.
	setupTempRepo := func(t *testing.T, yamlContent string) string {
		t.Helper()
		tmpDir := t.TempDir()
		coyoteDir := tmpDir + "/.coyote"
		if err := os.MkdirAll(coyoteDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(coyoteDir+"/pipeline.yml", []byte(yamlContent), 0o644); err != nil {
			t.Fatal(err)
		}
		return tmpDir
	}

	validYAML := `version: 1
pipeline:
  name: repo-ci
steps:
  - name: test
    run: go test ./...
  - name: lint
    run: golangci-lint run
`

	t.Run("creates build with repo metadata", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{
			localPath: tmpDir,
			commitSHA: "abc123def456",
		})

		build, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://github.com/org/repo.git",
			Ref:       "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if build.Status != domain.BuildStatusQueued {
			t.Errorf("expected queued, got %s", build.Status)
		}
		if build.RepoURL == nil || *build.RepoURL != "https://github.com/org/repo.git" {
			t.Errorf("expected repo_url, got %v", build.RepoURL)
		}
		if build.Ref == nil || *build.Ref != "main" {
			t.Errorf("expected ref main, got %v", build.Ref)
		}
		if build.CommitSHA == nil || *build.CommitSHA != "abc123def456" {
			t.Errorf("expected commit_sha, got %v", build.CommitSHA)
		}
		if build.PipelineConfigYAML == nil {
			t.Fatal("expected pipeline YAML snapshot")
		}
		if build.PipelineName == nil || *build.PipelineName != "repo-ci" {
			t.Errorf("expected pipeline_name repo-ci, got %v", build.PipelineName)
		}
		if build.PipelineSource == nil || *build.PipelineSource != ".coyote/pipeline.yml" {
			t.Errorf("expected logical pipeline_source, got %v", build.PipelineSource)
		}
	})

	t.Run("converts steps correctly", func(t *testing.T) {
		tmpDir := setupTempRepo(t, validYAML)
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repo.steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(repo.steps))
		}
		if repo.steps[0].Name != "test" || repo.steps[0].Command != "sh" {
			t.Errorf("step 0: got name=%q command=%q", repo.steps[0].Name, repo.steps[0].Command)
		}
		if repo.steps[1].Name != "lint" {
			t.Errorf("step 1: got name=%q", repo.steps[1].Name)
		}
	})

	t.Run("missing project_id", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			RepoURL: "https://example.com/repo.git",
			Ref:     "main",
		})
		if !errors.Is(err, ErrProjectIDRequired) {
			t.Errorf("expected ErrProjectIDRequired, got %v", err)
		}
	})

	t.Run("missing repo_url", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			Ref:       "main",
		})
		if !errors.Is(err, ErrRepoURLRequired) {
			t.Errorf("expected ErrRepoURLRequired, got %v", err)
		}
	})

	t.Run("missing ref", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
		})
		if !errors.Is(err, ErrRefRequired) {
			t.Errorf("expected ErrRefRequired, got %v", err)
		}
	})

	t.Run("repo fetcher not configured", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if !errors.Is(err, ErrRepoFetcherNotConfigured) {
			t.Errorf("expected ErrRepoFetcherNotConfigured, got %v", err)
		}
	})

	t.Run("repo fetch error", func(t *testing.T) {
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{err: errors.New("clone failed")})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err == nil || !strings.Contains(err.Error(), "clone failed") {
			t.Errorf("expected clone error, got %v", err)
		}
	})

	t.Run("pipeline file not found", func(t *testing.T) {
		// Use a temp dir without .coyote/pipeline.yml.
		tmpDir := t.TempDir()
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if !errors.Is(err, ErrPipelineFileNotFound) {
			t.Errorf("expected ErrPipelineFileNotFound, got %v", err)
		}
	})

	t.Run("invalid pipeline YAML", func(t *testing.T) {
		tmpDir := setupTempRepo(t, "not: valid: pipeline")
		repo := &fakeBuildRepository{}
		svc := NewBuildService(repo, nil, nil)
		svc.SetRepoFetcher(&fakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})

		_, err := svc.CreateBuildFromRepo(context.Background(), CreateRepoBuildInput{
			ProjectID: "proj-1",
			RepoURL:   "https://example.com/repo.git",
			Ref:       "main",
		})
		if err == nil {
			t.Fatal("expected parse/validation error")
		}
	})
}
