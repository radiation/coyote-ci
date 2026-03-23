package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type fakeBuildRepository struct {
	build         domain.Build
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

func (r *fakeBuildRepository) UpdateStatus(_ context.Context, id string, status domain.BuildStatus) (domain.Build, error) {
	r.updateCalls++
	r.updatedID = id
	r.updatedStatus = status

	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.Status = status
	return r.build, nil
}

func TestNewBuildService(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo)

	if svc == nil {
		t.Fatal("expected service instance, got nil")
	}

	if svc.orchestrator == nil {
		t.Fatal("expected service to initialize orchestrator")
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

			svc := NewBuildService(tc.repo)

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
			expectErr: repository.ErrBuildNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewBuildService(tc.repo)
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

func TestBuildService_ValidTransitions(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name           string
		initialStatus  domain.BuildStatus
		action         func(*BuildService, context.Context, string) (domain.Build, error)
		expectedStatus domain.BuildStatus
	}{
		{
			name:           "pending to running",
			initialStatus:  domain.BuildStatusPending,
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

			svc := NewBuildService(repo)

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
			name:          "pending to queued is invalid",
			initialStatus: domain.BuildStatusPending,
			action:        (*BuildService).QueueBuild,
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

			svc := NewBuildService(repo)

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
	svc := NewBuildService(repo)

	_, err := svc.StartBuild(context.Background(), "missing-build")
	if !errors.Is(err, repository.ErrBuildNotFound) {
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
			Status:    domain.BuildStatusPending,
		},
		updateErr: errors.New("update failed"),
	}

	svc := NewBuildService(repo)

	_, err := svc.StartBuild(context.Background(), "build-1")
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected update error, got %v", err)
	}

	if repo.updateCalls != 1 {
		t.Fatalf("expected UpdateStatus to be called once, got %d", repo.updateCalls)
	}
}
