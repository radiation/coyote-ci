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
			got, err := repo.UpdateStatus(context.Background(), tc.id, tc.newStatus)
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
