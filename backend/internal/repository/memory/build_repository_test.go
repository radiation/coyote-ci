package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNewBuildRepository(t *testing.T) {
	repo := NewBuildRepository()
	if repo == nil {
		t.Fatal("expected repository, got nil")
	}
	if repo.builds == nil {
		t.Fatal("expected builds map to be initialized")
	}
}

func TestBuildRepository_Create(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		in   domain.Build
	}{
		{
			name: "keeps provided id",
			in: domain.Build{
				ID:        "build-1",
				ProjectID: "project-1",
				Status:    domain.BuildStatusPending,
				CreatedAt: now,
			},
		},
		{
			name: "generates id when empty",
			in: domain.Build{
				ProjectID: "project-2",
				Status:    domain.BuildStatusPending,
				CreatedAt: now,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := NewBuildRepository()
			got, err := repo.Create(context.Background(), tc.in)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.ID == "" {
				t.Fatal("expected id to be present")
			}
			if got.ProjectID != tc.in.ProjectID {
				t.Fatalf("expected project %q, got %q", tc.in.ProjectID, got.ProjectID)
			}
		})
	}
}

func TestBuildRepository_GetByID(t *testing.T) {
	repo := NewBuildRepository()
	build, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	tests := []struct {
		name      string
		id        string
		expectErr error
	}{
		{name: "existing build", id: build.ID},
		{name: "missing build", id: "missing", expectErr: repository.ErrBuildNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := repo.GetByID(context.Background(), tc.id)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.ID != build.ID {
				t.Fatalf("expected id %q, got %q", build.ID, got.ID)
			}
		})
	}
}

func TestBuildRepository_UpdateStatus(t *testing.T) {
	repo := NewBuildRepository()
	created, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	tests := []struct {
		name           string
		id             string
		newStatus      domain.BuildStatus
		expectErr      error
		expectedStatus domain.BuildStatus
	}{
		{
			name:           "updates existing status",
			id:             created.ID,
			newStatus:      domain.BuildStatusRunning,
			expectedStatus: domain.BuildStatusRunning,
		},
		{
			name:      "missing build",
			id:        "missing",
			newStatus: domain.BuildStatusRunning,
			expectErr: repository.ErrBuildNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := repo.UpdateStatus(context.Background(), tc.id, tc.newStatus, nil)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.Status != tc.expectedStatus {
				t.Fatalf("expected status %q, got %q", tc.expectedStatus, got.Status)
			}
		})
	}
}

func TestBuildRepository_QueueBuild_PersistsOrderedSteps(t *testing.T) {
	repo := NewBuildRepository()
	created, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	queued, err := repo.QueueBuild(context.Background(), created.ID, []domain.BuildStep{
		{ID: "step-2", BuildID: created.ID, StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
		{ID: "step-1", BuildID: created.ID, StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	if queued.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", queued.Status)
	}

	steps, err := repo.GetStepsByBuildID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].StepIndex != 0 || steps[0].Name != "lint" {
		t.Fatalf("expected first step lint@0, got %s@%d", steps[0].Name, steps[0].StepIndex)
	}
	if steps[1].StepIndex != 1 || steps[1].Name != "test" {
		t.Fatalf("expected second step test@1, got %s@%d", steps[1].Name, steps[1].StepIndex)
	}
}

func TestBuildRepository_PersistsBuildAndStepStatusUpdates(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-2",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	_, err = repo.QueueBuild(context.Background(), "build-2", []domain.BuildStep{
		{ID: "step-1", BuildID: "build-2", StepIndex: 0, Name: "default", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	_, err = repo.UpdateStatus(context.Background(), "build-2", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("update running status failed: %v", err)
	}

	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(2 * time.Second)
	exitCode := 0
	_, err = repo.UpdateStepByIndex(context.Background(), "build-2", 0, repository.StepUpdate{
		Status:     domain.BuildStepStatusSuccess,
		ExitCode:   &exitCode,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
	})
	if err != nil {
		t.Fatalf("update step status failed: %v", err)
	}

	_, err = repo.UpdateCurrentStepIndex(context.Background(), "build-2", 1)
	if err != nil {
		t.Fatalf("update current step index failed: %v", err)
	}

	_, err = repo.UpdateStatus(context.Background(), "build-2", domain.BuildStatusSuccess, nil)
	if err != nil {
		t.Fatalf("update success status failed: %v", err)
	}

	build, err := repo.GetByID(context.Background(), "build-2")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected success status, got %q", build.Status)
	}
	if build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index 1, got %d", build.CurrentStepIndex)
	}

	steps, err := repo.GetStepsByBuildID(context.Background(), "build-2")
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", steps[0].Status)
	}
	if steps[0].ExitCode == nil || *steps[0].ExitCode != 0 {
		t.Fatalf("expected step exit code 0, got %v", steps[0].ExitCode)
	}
}

func TestBuildRepository_ClaimStepIfPending(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-claim",
		ProjectID: "project-1",
		Status:    domain.BuildStatusQueued,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	_, err = repo.QueueBuild(context.Background(), "build-claim", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "default", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	startedAt := time.Now().UTC()
	step, claimed, err := repo.ClaimStepIfPending(context.Background(), "build-claim", 0, nil, startedAt)
	if err != nil {
		t.Fatalf("claim step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected step to be claimed")
	}
	if step.Status != domain.BuildStepStatusRunning {
		t.Fatalf("expected running step status, got %q", step.Status)
	}

	_, claimed, err = repo.ClaimStepIfPending(context.Background(), "build-claim", 0, nil, startedAt)
	if err != nil {
		t.Fatalf("second claim returned error: %v", err)
	}
	if claimed {
		t.Fatal("expected second claim to fail for non-pending step")
	}
}

func TestBuildRepository_CompleteStepIfRunning(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-complete",
		ProjectID: "project-1",
		Status:    domain.BuildStatusRunning,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	_, err = repo.QueueBuild(context.Background(), "build-complete", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "default", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	startedAt := time.Now().UTC().Add(-2 * time.Second)
	_, claimed, err := repo.ClaimStepIfPending(context.Background(), "build-complete", 0, nil, startedAt)
	if err != nil {
		t.Fatalf("claim step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim to succeed")
	}

	finishedAt := time.Now().UTC()
	exitCode := 0
	stdout := "ok\n"
	step, completed, err := repo.CompleteStepIfRunning(context.Background(), "build-complete", 0, repository.StepUpdate{
		Status:     domain.BuildStepStatusSuccess,
		ExitCode:   &exitCode,
		Stdout:     &stdout,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
	})
	if err != nil {
		t.Fatalf("complete step failed: %v", err)
	}
	if !completed {
		t.Fatal("expected completion to succeed")
	}
	if step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected success status, got %q", step.Status)
	}
	if step.ExitCode == nil || *step.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", step.ExitCode)
	}
	if step.Stdout == nil || *step.Stdout != stdout {
		t.Fatalf("expected stdout %q, got %v", stdout, step.Stdout)
	}

	dup, completed, err := repo.CompleteStepIfRunning(context.Background(), "build-complete", 0, repository.StepUpdate{
		Status: domain.BuildStepStatusSuccess,
	})
	if err != nil {
		t.Fatalf("duplicate completion failed: %v", err)
	}
	if completed {
		t.Fatal("expected duplicate completion to be no-op")
	}
	if dup.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected terminal status to remain success, got %q", dup.Status)
	}
}

func TestBuildRepository_CompleteStepAndAdvanceBuild(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-advance",
		ProjectID: "project-1",
		Status:    domain.BuildStatusRunning,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	_, err = repo.QueueBuild(context.Background(), "build-advance", []domain.BuildStep{
		{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending},
		{ID: "step-2", StepIndex: 1, Name: "second", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}

	_, err = repo.UpdateStatus(context.Background(), "build-advance", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	startedAt := time.Now().UTC().Add(-2 * time.Second)
	_, claimed, err := repo.ClaimStepIfPending(context.Background(), "build-advance", 0, nil, startedAt)
	if err != nil {
		t.Fatalf("claim first step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected first step claim")
	}

	finishedAt := time.Now().UTC()
	exitCode := 0
	stdout := "ok\n"
	firstStep, outcome, err := repo.CompleteStepAndAdvanceBuild(context.Background(), "build-advance", 0, repository.StepUpdate{
		Status:     domain.BuildStepStatusSuccess,
		ExitCode:   &exitCode,
		Stdout:     &stdout,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
	})
	if err != nil {
		t.Fatalf("complete first step failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted || firstStep.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected first step success completion, got outcome=%q status=%q", outcome, firstStep.Status)
	}

	build, err := repo.GetByID(context.Background(), "build-advance")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusRunning {
		t.Fatalf("expected build to remain running after non-final success, got %q", build.Status)
	}
	if build.CurrentStepIndex != 1 {
		t.Fatalf("expected current step index 1, got %d", build.CurrentStepIndex)
	}

	_, claimed, err = repo.ClaimStepIfPending(context.Background(), "build-advance", 1, nil, startedAt)
	if err != nil {
		t.Fatalf("claim second step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected second step claim")
	}

	secondStep, outcome, err := repo.CompleteStepAndAdvanceBuild(context.Background(), "build-advance", 1, repository.StepUpdate{
		Status:     domain.BuildStepStatusSuccess,
		ExitCode:   &exitCode,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
	})
	if err != nil {
		t.Fatalf("complete second step failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted || secondStep.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected second step success completion, got outcome=%q status=%q", outcome, secondStep.Status)
	}

	build, err = repo.GetByID(context.Background(), "build-advance")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build success after final step, got %q", build.Status)
	}

	dup, outcome, err := repo.CompleteStepAndAdvanceBuild(context.Background(), "build-advance", 1, repository.StepUpdate{Status: domain.BuildStepStatusSuccess})
	if err != nil {
		t.Fatalf("duplicate completion failed: %v", err)
	}
	if outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate completion no-op")
	}
	if dup.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected duplicate to return existing terminal step, got %q", dup.Status)
	}
}

func TestBuildRepository_CompleteStepAndAdvanceBuild_FailedStepMarksBuildFailed(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-fail", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-fail", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}, {ID: "step-2", StepIndex: 1, Name: "second", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-fail", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	startedAt := time.Now().UTC().Add(-1 * time.Second)
	_, claimed, err := repo.ClaimStepIfPending(context.Background(), "build-fail", 0, nil, startedAt)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim")
	}

	finishedAt := time.Now().UTC()
	exitCode := 17
	stderr := "boom"
	errMsg := "boom"
	_, outcome, err := repo.CompleteStepAndAdvanceBuild(context.Background(), "build-fail", 0, repository.StepUpdate{Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, Stderr: &stderr, ErrorMessage: &errMsg, StartedAt: &startedAt, FinishedAt: &finishedAt})
	if err != nil {
		t.Fatalf("complete failed step returned error: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatal("expected completion")
	}

	build, err := repo.GetByID(context.Background(), "build-fail")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed, got %q", build.Status)
	}
}

func TestBuildRepository_CompleteStepAndAdvanceBuild_InvalidTransition(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-invalid", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-invalid", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-invalid", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	_, outcome, err := repo.CompleteStepAndAdvanceBuild(context.Background(), "build-invalid", 0, repository.StepUpdate{Status: domain.BuildStepStatusSuccess})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if outcome != repository.StepCompletionInvalidTransition {
		t.Fatal("expected invalid transition to not complete")
	}
}

func TestBuildRepository_CreateQueuedBuild(t *testing.T) {
	repo := NewBuildRepository()
	build, err := repo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:        "build-queued",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: time.Now().UTC(),
	}, []domain.BuildStep{
		{ID: "step-1", StepIndex: 0, Name: "checkout", Status: domain.BuildStepStatusPending},
		{ID: "step-2", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}

	steps, err := repo.GetStepsByBuildID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Name != "checkout" || steps[1].Name != "test" {
		t.Fatalf("unexpected step ordering: %+v", steps)
	}
}
