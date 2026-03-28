package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
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

func (r *fakeBuildRepository) CompleteStepAndAdvanceBuild(_ context.Context, buildID string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	step, completed, err := r.CompleteStepIfRunning(context.Background(), buildID, stepIndex, update)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	if !completed {
		if step.Status == domain.BuildStepStatusSuccess || step.Status == domain.BuildStepStatusFailed {
			return step, repository.StepCompletionDuplicateTerminal, nil
		}
		if step.ID == "" && step.Name == "" {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
		}
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
	}

	if update.Status == domain.BuildStepStatusFailed {
		r.build.Status = domain.BuildStatusFailed
		r.build.ErrorMessage = step.ErrorMessage
		return step, repository.StepCompletionCompleted, nil
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

	return step, repository.StepCompletionCompleted, nil
}

func (r *fakeBuildRepository) CompleteClaimedStepAndAdvanceBuild(_ context.Context, buildID string, stepIndex int, claimToken string, update repository.StepUpdate) (domain.BuildStep, repository.StepCompletionOutcome, error) {
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

		break
	}

	step, outcome, err := r.CompleteStepAndAdvanceBuild(context.Background(), buildID, stepIndex, update)
	if err != nil {
		return domain.BuildStep{}, repository.StepCompletionInvalidTransition, err
	}
	if outcome == repository.StepCompletionCompleted {
		for i := range r.steps {
			if r.steps[i].StepIndex == stepIndex {
				r.steps[i].ClaimToken = nil
				r.steps[i].ClaimedAt = nil
				r.steps[i].LeaseExpiresAt = nil
				step = r.steps[i]
				break
			}
		}
	}

	return step, outcome, nil
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
	result, err := svc.RunStep(context.Background(), request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
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
	if logSink.calls != 1 {
		t.Fatalf("expected one log write, got %d", logSink.calls)
	}
	if logSink.lines[0] != "ok" {
		t.Fatalf("expected trimmed log line, got %q", logSink.lines[0])
	}
}

func TestBuildService_RunStep_RunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("runner failed")}
	claimToken := "claim-active"
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0}, steps: []domain.BuildStep{{StepIndex: 0, Name: "echo", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}}}
	svc := NewBuildService(repo, runner, &fakeLogSink{})

	_, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "echo", ClaimToken: claimToken, Command: "echo"})
	if err == nil || err.Error() != "runner failed" {
		t.Fatalf("expected runner error, got %v", err)
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

			if _, err := svc.RunStep(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepName: "step-1", ClaimToken: claimToken, Command: "echo"}); err != nil {
				t.Fatalf("run step failed: %v", err)
			}

			buildLogs, err := svc.GetBuildLogs(context.Background(), "build-1")
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

	_, completed, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if !completed {
		t.Fatal("expected first completion to complete step")
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

	_, completed, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate completion should be no-op, got error %v", err)
	}
	if completed {
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

	_, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !completed {
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

	_, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !completed {
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

	_, completed, err := svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("first failure completion failed: %v", err)
	}
	if !completed {
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

	_, completed, err = svc.HandleStepResult(context.Background(), request, result)
	if err != nil {
		t.Fatalf("duplicate failure completion should be no-op, got %v", err)
	}
	if completed {
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

	_, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-any"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if !errors.Is(err, ErrInvalidBuildStepTransition) {
		t.Fatalf("expected ErrInvalidBuildStepTransition, got %v", err)
	}
	if completed {
		t.Fatal("expected non-running completion to not complete")
	}
}

func TestBuildService_HandleStepResult_MissingClaimTokenReturnsInvalidTransition(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	_, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if !errors.Is(err, ErrInvalidBuildStepTransition) {
		t.Fatalf("expected ErrInvalidBuildStepTransition, got %v", err)
	}
	if completed {
		t.Fatal("expected completion without claim token to be rejected")
	}
}

func TestBuildService_HandleStepResult_StaleClaimReturnsDomainError(t *testing.T) {
	claimToken := "claim-active"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0},
		steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}},
	}
	svc := NewBuildService(repo, nil, logs.NewMemorySink())

	_, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-stale"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if !errors.Is(err, ErrStaleStepClaim) {
		t.Fatalf("expected ErrStaleStepClaim, got %v", err)
	}
	if completed {
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

	step, completed, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: "claim-active"}, steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !completed {
		t.Fatal("expected claimed completion to persist")
	}
	if step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", step.Status)
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
