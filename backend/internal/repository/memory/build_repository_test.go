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
	} else if repo.builds == nil {
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

func TestBuildRepository_RejectsMalformedRepositoryIdentitySnapshots(t *testing.T) {
	whitespace := "  "
	connectionID := "connection-1"
	providerRepositoryID := "provider-repository-1"
	invalidBuild := domain.Build{ID: "build-invalid", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC(), RegisteredRepositoryID: &whitespace, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID}
	repo := NewBuildRepository()
	if _, err := repo.Create(context.Background(), invalidBuild); err == nil {
		t.Fatal("expected malformed snapshot to be rejected")
	}
	if _, err := repo.CreateQueuedBuild(context.Background(), invalidBuild, nil); err == nil {
		t.Fatal("expected queued malformed snapshot to be rejected")
	}
}

func TestBuildRepository_AssignsSequentialBuildNumbersPerJob(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	jobA := "job-a"
	jobB := "job-b"

	first, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		JobID:     &jobA,
		Status:    domain.BuildStatusPending,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create first build failed: %v", err)
	}
	if first.BuildNumber != 1 {
		t.Fatalf("expected first build number 1, got %d", first.BuildNumber)
	}

	second, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-2",
		ProjectID: "project-1",
		JobID:     &jobA,
		Status:    domain.BuildStatusPending,
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create second build failed: %v", err)
	}
	if second.BuildNumber != 2 {
		t.Fatalf("expected second build number 2, got %d", second.BuildNumber)
	}

	rerunOf := first.ID
	rerun, err := repo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:             "build-3",
		ProjectID:      "project-1",
		JobID:          &jobA,
		Status:         domain.BuildStatusPending,
		CreatedAt:      now.Add(2 * time.Minute),
		RerunOfBuildID: &rerunOf,
	}, []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("create rerun build failed: %v", err)
	}
	if rerun.BuildNumber != 3 {
		t.Fatalf("expected rerun build number 3, got %d", rerun.BuildNumber)
	}
	if rerun.RerunOfBuildID == nil || *rerun.RerunOfBuildID != first.ID {
		t.Fatalf("expected rerun_of_build_id=%s, got %v", first.ID, rerun.RerunOfBuildID)
	}

	secondRerun, err := repo.CreateQueuedBuild(context.Background(), domain.Build{
		ID:             "build-4",
		ProjectID:      "project-1",
		JobID:          &jobA,
		Status:         domain.BuildStatusPending,
		CreatedAt:      now.Add(3 * time.Minute),
		RerunOfBuildID: &rerunOf,
	}, []domain.BuildStep{{ID: "step-2", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("create second rerun build failed: %v", err)
	}
	if secondRerun.BuildNumber != 4 {
		t.Fatalf("expected second rerun build number 4, got %d", secondRerun.BuildNumber)
	}

	otherJob, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-5",
		ProjectID: "project-1",
		JobID:     &jobB,
		Status:    domain.BuildStatusPending,
		CreatedAt: now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create other job build failed: %v", err)
	}
	if otherJob.BuildNumber != 1 {
		t.Fatalf("expected first build for other job to be 1, got %d", otherJob.BuildNumber)
	}

	normal, err := repo.GetByID(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("reload normal build failed: %v", err)
	}
	if normal.RerunOfBuildID != nil {
		t.Fatalf("expected normal build rerun_of_build_id to be nil, got %v", normal.RerunOfBuildID)
	}
}

func TestBuildRepository_DerivesBuildMetadataFromSourceAndTrigger(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	actor := "octocat"
	build, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: now,
		Ref:       stringPtr("main"),
		CommitSHA: stringPtr("abc123"),
		Trigger: domain.BuildTrigger{
			Kind:  domain.BuildTriggerKindWebhook,
			Actor: &actor,
		},
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	if build.SourceRef == nil || *build.SourceRef != "main" {
		t.Fatalf("expected source_ref main, got %v", build.SourceRef)
	}
	if build.SourceSHA == nil || *build.SourceSHA != "abc123" {
		t.Fatalf("expected source_sha abc123, got %v", build.SourceSHA)
	}
	if build.TriggerType != domain.BuildTriggerTypeWebhook {
		t.Fatalf("expected webhook trigger type, got %q", build.TriggerType)
	}
	if build.TriggeredBy == nil || *build.TriggeredBy != actor {
		t.Fatalf("expected triggered_by %q, got %v", actor, build.TriggeredBy)
	}
}

func TestBuildRepository_UpdateSourceProvenance_RoundTrip(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	build, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: now,
		RepoURL:   stringPtr("https://github.com/acme/repo.git"),
		Ref:       stringPtr("main"),
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	updated, err := repo.UpdateSourceProvenance(context.Background(), build.ID, repository.SourceProvenanceUpdate{
		CommitSHA:      "deadbeef",
		AuthorName:     "Ada Lovelace",
		AuthorEmail:    "ada@example.com",
		CommitterName:  "Grace Hopper",
		CommitterEmail: "grace@example.com",
	})
	if err != nil {
		t.Fatalf("update source provenance failed: %v", err)
	}
	if updated.SourceAuthorEmail == nil || *updated.SourceAuthorEmail != "ada@example.com" {
		t.Fatalf("expected persisted source author email, got %v", updated.SourceAuthorEmail)
	}

	reloaded, err := repo.GetByID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("reload build failed: %v", err)
	}
	if reloaded.SourceCommitterEmail == nil || *reloaded.SourceCommitterEmail != "grace@example.com" {
		t.Fatalf("expected persisted source committer email, got %v", reloaded.SourceCommitterEmail)
	}
	if reloaded.SourceSHA == nil || *reloaded.SourceSHA != "deadbeef" {
		t.Fatalf("expected source_sha to reflect commit SHA, got %v", reloaded.SourceSHA)
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestBuildRepository_ExplicitBuildNumbersAdvancePerJobCounters(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	jobID := "job-a"

	_, err := repo.Create(context.Background(), domain.Build{
		ID:          "build-explicit-job",
		ProjectID:   "project-1",
		JobID:       &jobID,
		BuildNumber: 7,
		Status:      domain.BuildStatusPending,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create explicit job build failed: %v", err)
	}

	nextForJob, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-next-job",
		ProjectID: "project-1",
		JobID:     &jobID,
		Status:    domain.BuildStatusPending,
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create next job build failed: %v", err)
	}
	if nextForJob.BuildNumber != 8 {
		t.Fatalf("expected next job build number 8, got %d", nextForJob.BuildNumber)
	}

	_, err = repo.Create(context.Background(), domain.Build{
		ID:          "build-explicit-anon",
		ProjectID:   "project-1",
		BuildNumber: 11,
		Status:      domain.BuildStatusPending,
		CreatedAt:   now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create explicit anonymous build failed: %v", err)
	}

	nextAnonymous, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-next-anon",
		ProjectID: "project-1",
		Status:    domain.BuildStatusPending,
		CreatedAt: now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create next anonymous build failed: %v", err)
	}
	if nextAnonymous.BuildNumber != 12 {
		t.Fatalf("expected next anonymous build number 12, got %d", nextAnonymous.BuildNumber)
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

func TestBuildRepository_ListLatestByJobIDs(t *testing.T) {
	repo := NewBuildRepository()
	jobA := "job-a"
	jobB := "job-b"
	now := time.Now().UTC()

	_, err := repo.Create(context.Background(), domain.Build{ID: "build-1", BuildNumber: 1, ProjectID: "project-1", JobID: &jobA, Status: domain.BuildStatusFailed, CreatedAt: now})
	if err != nil {
		t.Fatalf("setup create build-1 failed: %v", err)
	}
	_, err = repo.Create(context.Background(), domain.Build{ID: "build-2", BuildNumber: 2, ProjectID: "project-1", JobID: &jobA, Status: domain.BuildStatusSuccess, CreatedAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("setup create build-2 failed: %v", err)
	}
	_, err = repo.Create(context.Background(), domain.Build{ID: "build-3", BuildNumber: 3, ProjectID: "project-1", JobID: &jobB, Status: domain.BuildStatusQueued, CreatedAt: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("setup create build-3 failed: %v", err)
	}

	latest, err := repo.ListLatestByJobIDs(context.Background(), []string{jobA, jobB, jobA, ""})
	if err != nil {
		t.Fatalf("ListLatestByJobIDs returned error: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected latest builds for 2 jobs, got %d", len(latest))
	}
	if latest[jobA].ID != "build-2" {
		t.Fatalf("expected latest build-2 for %s, got %s", jobA, latest[jobA].ID)
	}
	if latest[jobB].ID != "build-3" {
		t.Fatalf("expected latest build-3 for %s, got %s", jobB, latest[jobB].ID)
	}
}

func TestBuildRepository_ListActive(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()

	for _, build := range []domain.Build{
		{ID: "build-success", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "build-queued", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "build-running", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now.Add(-time.Minute)},
		{ID: "build-preparing", ProjectID: "project-1", Status: domain.BuildStatusPreparing, CreatedAt: now},
	} {
		if _, err := repo.Create(context.Background(), build); err != nil {
			t.Fatalf("setup create %s failed: %v", build.ID, err)
		}
	}

	builds, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive returned error: %v", err)
	}
	if len(builds) != 3 {
		t.Fatalf("expected 3 active builds, got %d", len(builds))
	}
	if builds[0].ID != "build-preparing" || builds[1].ID != "build-running" || builds[2].ID != "build-queued" {
		t.Fatalf("expected active builds ordered by created_at desc, got %#v", builds)
	}
}

func TestBuildRepository_ListPagedFiltersProjectAndClampsParams(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	for _, build := range []domain.Build{
		{ID: "build-old", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "build-other", ProjectID: "project-2", Status: domain.BuildStatusSuccess, CreatedAt: now.Add(-time.Minute)},
		{ID: "build-new", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
	} {
		if _, createErr := repo.Create(context.Background(), build); createErr != nil {
			t.Fatalf("setup create %s failed: %v", build.ID, createErr)
		}
	}

	builds, listErr := repo.ListPaged(context.Background(), repository.ListParams{ProjectID: "project-1", Limit: 1, Offset: 0})
	if listErr != nil {
		t.Fatalf("ListPaged returned error: %v", listErr)
	}
	if len(builds) != 1 || builds[0].ID != "build-new" {
		t.Fatalf("expected newest project-1 build, got %#v", builds)
	}

	empty, emptyErr := repo.ListPaged(context.Background(), repository.ListParams{ProjectID: "project-1", Limit: 1, Offset: 3})
	if emptyErr != nil {
		t.Fatalf("ListPaged offset returned error: %v", emptyErr)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty page beyond result set, got %#v", empty)
	}
}

func TestBuildRepository_ListQueueFiltersAndOrdersEntries(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	queuedEarly := now.Add(-5 * time.Minute)
	queuedLate := now.Add(-2 * time.Minute)
	runningStarted := now.Add(-10 * time.Minute)

	for _, build := range []domain.Build{
		{ID: "build-success", ProjectID: "project-1", Status: domain.BuildStatusSuccess, Priority: 10, CreatedAt: now.Add(-6 * time.Minute)},
		{ID: "build-low", ProjectID: "project-1", Status: domain.BuildStatusQueued, Priority: 3, QueuedAt: &queuedEarly, CreatedAt: now.Add(-5 * time.Minute)},
		{ID: "build-high-late", ProjectID: "project-1", Status: domain.BuildStatusQueued, Priority: 9, QueuedAt: &queuedLate, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "build-high-early", ProjectID: "project-1", Status: domain.BuildStatusQueued, Priority: 9, QueuedAt: &queuedEarly, CreatedAt: now.Add(-4 * time.Minute)},
		{ID: "build-running", ProjectID: "project-1", Status: domain.BuildStatusRunning, StartedAt: &runningStarted, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "build-other-project", ProjectID: "project-2", Status: domain.BuildStatusQueued, Priority: 10, CreatedAt: now.Add(-time.Minute)},
	} {
		if _, createErr := repo.Create(context.Background(), build); createErr != nil {
			t.Fatalf("setup create %s failed: %v", build.ID, createErr)
		}
	}

	entries, listErr := repo.ListQueue(context.Background(), repository.QueueListParams{ProjectID: "project-1"})
	if listErr != nil {
		t.Fatalf("ListQueue returned error: %v", listErr)
	}
	gotIDs := queueEntryBuildIDs(entries)
	wantIDs := []string{"build-high-early", "build-high-late", "build-low", "build-running"}
	if !sameStringSlice(gotIDs, wantIDs) {
		t.Fatalf("expected queue order %v, got %v", wantIDs, gotIDs)
	}

	queuedOnly, queuedErr := repo.ListQueue(context.Background(), repository.QueueListParams{ProjectID: "project-1", Status: string(domain.BuildStatusQueued)})
	if queuedErr != nil {
		t.Fatalf("ListQueue queued filter returned error: %v", queuedErr)
	}
	queuedIDs := queueEntryBuildIDs(queuedOnly)
	wantQueuedIDs := []string{"build-high-early", "build-high-late", "build-low"}
	if !sameStringSlice(queuedIDs, wantQueuedIDs) {
		t.Fatalf("expected queued-only order %v, got %v", wantQueuedIDs, queuedIDs)
	}
}

func TestBuildRepository_ListByJobIDOrdersNewestFirst(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	jobID := "job-1"
	otherJobID := "job-2"
	for _, build := range []domain.Build{
		{ID: "build-old", ProjectID: "project-1", JobID: &jobID, CreatedAt: now.Add(-time.Minute)},
		{ID: "build-new", ProjectID: "project-1", JobID: &jobID, CreatedAt: now},
		{ID: "build-other", ProjectID: "project-1", JobID: &otherJobID, CreatedAt: now.Add(time.Minute)},
	} {
		if _, createErr := repo.Create(context.Background(), build); createErr != nil {
			t.Fatalf("setup create %s failed: %v", build.ID, createErr)
		}
	}

	builds, listErr := repo.ListByJobID(context.Background(), jobID)
	if listErr != nil {
		t.Fatalf("ListByJobID returned error: %v", listErr)
	}
	if len(builds) != 2 || builds[0].ID != "build-new" || builds[1].ID != "build-old" {
		t.Fatalf("expected job builds newest first, got %#v", builds)
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

func queueEntryBuildIDs(entries []domain.QueueEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Build.ID)
	}
	return ids
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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

func TestBuildRepository_CancelBuild_AtomicallyTerminalizesBuildAndCancellableSteps(t *testing.T) {
	repo := NewBuildRepository()
	createdAt := time.Now().UTC().Add(-2 * time.Minute)
	build, err := repo.Create(context.Background(), domain.Build{
		ID:        "build-cancel",
		ProjectID: "project-1",
		Status:    domain.BuildStatusRunning,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	claimToken := "claim-1"
	claimedAt := createdAt.Add(time.Minute)
	leaseExpiresAt := claimedAt.Add(time.Minute)
	workerID := "worker-1"
	finishedAt := createdAt.Add(90 * time.Second)
	exitCode := 0

	steps := []domain.BuildStep{
		{ID: "step-0", BuildID: build.ID, StepIndex: 0, Name: "setup", Status: domain.BuildStepStatusSuccess, FinishedAt: &finishedAt, ExitCode: &exitCode},
		{ID: "step-1", BuildID: build.ID, StepIndex: 1, Name: "test", Status: domain.BuildStepStatusRunning, WorkerID: &workerID, ClaimToken: &claimToken, ClaimedAt: &claimedAt, LeaseExpiresAt: &leaseExpiresAt, StartedAt: &claimedAt},
		{ID: "step-2", BuildID: build.ID, StepIndex: 2, Name: "lint", Status: domain.BuildStepStatusPending},
	}
	_, err = repo.QueueBuild(context.Background(), build.ID, steps)
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), build.ID, domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("start build failed: %v", err)
	}

	canceledAt := time.Now().UTC()
	updatedBuild, updatedSteps, err := repo.CancelBuild(context.Background(), build.ID, "operator cancel", canceledAt)
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if updatedBuild.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected build canceled after cancel, got %q", updatedBuild.Status)
	}
	if updatedSteps != 2 {
		t.Fatalf("expected 2 cancellable steps to be updated, got %d", updatedSteps)
	}

	currentSteps, err := repo.GetStepsByBuildID(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if currentSteps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected terminal success step unchanged, got %q", currentSteps[0].Status)
	}
	if currentSteps[0].FinishedAt == nil || !currentSteps[0].FinishedAt.Equal(finishedAt) {
		t.Fatalf("expected completed step finished_at preserved, got %#v", currentSteps[0].FinishedAt)
	}
	if currentSteps[0].ExitCode == nil || *currentSteps[0].ExitCode != 0 {
		t.Fatalf("expected completed step exit code preserved, got %#v", currentSteps[0].ExitCode)
	}
	if currentSteps[1].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected running step terminalized to canceled, got %q", currentSteps[1].Status)
	}
	if currentSteps[1].ClaimToken != nil || currentSteps[1].ClaimedAt != nil || currentSteps[1].LeaseExpiresAt != nil {
		t.Fatalf("expected running step claim fields cleared, got %#v", currentSteps[1])
	}
	if currentSteps[2].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected pending step terminalized to canceled, got %q", currentSteps[2].Status)
	}
}

func TestBuildRepository_CompleteStepAfterCancelDoesNotResurrectState(t *testing.T) {
	repo := NewBuildRepository()
	now := time.Now().UTC()
	claimToken := "claim-1"
	claimedAt := now.Add(time.Minute)
	leaseExpiresAt := claimedAt.Add(time.Minute)
	workerID := "worker-1"

	if _, err := repo.Create(context.Background(), domain.Build{ID: "build-race", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}); err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	if _, err := repo.QueueBuild(context.Background(), "build-race", []domain.BuildStep{{ID: "step-race", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusRunning, WorkerID: &workerID, ClaimToken: &claimToken, ClaimedAt: &claimedAt, LeaseExpiresAt: &leaseExpiresAt, StartedAt: &claimedAt}}); err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	if _, err := repo.UpdateStatus(context.Background(), "build-race", domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("start build failed: %v", err)
	}

	canceledAt := now.Add(2 * time.Minute)
	if _, _, err := repo.CancelBuild(context.Background(), "build-race", "operator canceled", canceledAt); err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}

	exitCode := 0
	finishedAt := now.Add(3 * time.Minute)
	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:      "build-race",
		StepIndex:    0,
		ClaimToken:   claimToken,
		RequireClaim: true,
		Update: repository.StepUpdate{
			Status:     domain.BuildStepStatusSuccess,
			ExitCode:   &exitCode,
			FinishedAt: &finishedAt,
		},
	})
	if err != nil {
		t.Fatalf("late complete step failed: %v", err)
	}
	if result.Outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %q", result.Outcome)
	}
	if result.Step.Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected reported step to remain canceled, got %q", result.Step.Status)
	}

	build, err := repo.GetByID(context.Background(), "build-race")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected build to remain canceled, got %q", build.Status)
	}
	steps, err := repo.GetStepsByBuildID(context.Background(), "build-race")
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if steps[0].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected persisted step to remain canceled, got %q", steps[0].Status)
	}
	if steps[0].ExitCode != nil {
		t.Fatalf("expected late exit code to be ignored, got %#v", steps[0].ExitCode)
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

func TestBuildRepository_CompleteStep(t *testing.T) {
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
	firstResult, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-advance",
		StepIndex: 0,
		Update: repository.StepUpdate{
			Status:     domain.BuildStepStatusSuccess,
			ExitCode:   &exitCode,
			Stdout:     &stdout,
			StartedAt:  &startedAt,
			FinishedAt: &finishedAt,
		},
	})
	if err != nil {
		t.Fatalf("complete first step failed: %v", err)
	}
	if firstResult.Outcome != repository.StepCompletionCompleted || firstResult.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected first step success completion, got outcome=%q status=%q", firstResult.Outcome, firstResult.Step.Status)
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

	secondResult, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-advance",
		StepIndex: 1,
		Update: repository.StepUpdate{
			Status:     domain.BuildStepStatusSuccess,
			ExitCode:   &exitCode,
			StartedAt:  &startedAt,
			FinishedAt: &finishedAt,
		},
	})
	if err != nil {
		t.Fatalf("complete second step failed: %v", err)
	}
	if secondResult.Outcome != repository.StepCompletionCompleted || secondResult.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected second step success completion, got outcome=%q status=%q", secondResult.Outcome, secondResult.Step.Status)
	}

	build, err = repo.GetByID(context.Background(), "build-advance")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected build success after final step, got %q", build.Status)
	}

	dupResult, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-advance",
		StepIndex: 1,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess},
	})
	if err != nil {
		t.Fatalf("duplicate completion failed: %v", err)
	}
	if dupResult.Outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatal("expected duplicate completion no-op")
	}
	if dupResult.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected duplicate to return existing terminal step, got %q", dupResult.Step.Status)
	}
}

func TestBuildRepository_CompleteStep_FailedStepMarksBuildFailed(t *testing.T) {
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
	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-fail",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, Stderr: &stderr, ErrorMessage: &errMsg, StartedAt: &startedAt, FinishedAt: &finishedAt},
	})
	if err != nil {
		t.Fatalf("complete failed step returned error: %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
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

func TestBuildRepository_CompleteStep_InvalidTransition(t *testing.T) {
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

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:   "build-invalid",
		StepIndex: 0,
		Update:    repository.StepUpdate{Status: domain.BuildStepStatusSuccess},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Outcome != repository.StepCompletionInvalidTransition {
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

func TestBuildRepository_ClaimPendingStepAndReclaimExpiredStep(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-lease", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-lease", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-lease", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	claimA := repository.StepClaim{
		WorkerID:       "worker-a",
		ClaimToken:     "claim-a",
		ClaimedAt:      claimedAt,
		LeaseExpiresAt: claimedAt.Add(30 * time.Second),
	}
	step, claimed, err := repo.ClaimPendingStep(context.Background(), "build-lease", 0, claimA)
	if err != nil {
		t.Fatalf("claim pending step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected claim to succeed")
	}
	if step.ClaimToken == nil || *step.ClaimToken != "claim-a" {
		t.Fatalf("expected claim token claim-a, got %v", step.ClaimToken)
	}

	notExpiredBefore := claimedAt.Add(10 * time.Second)
	_, reclaimed, err := repo.ReclaimExpiredStep(context.Background(), "build-lease", 0, notExpiredBefore, repository.StepClaim{
		WorkerID:       "worker-b",
		ClaimToken:     "claim-b",
		ClaimedAt:      notExpiredBefore,
		LeaseExpiresAt: notExpiredBefore.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("reclaim before lease expiry failed: %v", err)
	}
	if reclaimed {
		t.Fatal("expected reclaim to fail for active lease")
	}

	reclaimAt := claimedAt.Add(2 * time.Minute)
	step, reclaimed, err = repo.ReclaimExpiredStep(context.Background(), "build-lease", 0, reclaimAt, repository.StepClaim{
		WorkerID:       "worker-b",
		ClaimToken:     "claim-b",
		ClaimedAt:      reclaimAt,
		LeaseExpiresAt: reclaimAt.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("reclaim expired step failed: %v", err)
	}
	if !reclaimed {
		t.Fatal("expected reclaim to succeed after lease expiry")
	}
	if step.ClaimToken == nil || *step.ClaimToken != "claim-b" {
		t.Fatalf("expected claim token claim-b, got %v", step.ClaimToken)
	}
}

func TestBuildRepository_CompleteStep_RejectsStaleClaim(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-stale", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-stale", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-stale", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	claimAt := time.Now().UTC()
	_, claimed, err := repo.ClaimPendingStep(context.Background(), "build-stale", 0, repository.StepClaim{
		WorkerID:       "worker-a",
		ClaimToken:     "claim-a",
		ClaimedAt:      claimAt,
		LeaseExpiresAt: claimAt.Add(10 * time.Second),
	})
	if err != nil {
		t.Fatalf("claim pending step failed: %v", err)
	}
	if !claimed {
		t.Fatal("expected initial claim to succeed")
	}

	exitCode := 0
	now := time.Now().UTC()
	staleResult, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:      "build-stale",
		StepIndex:    0,
		ClaimToken:   "stale-claim",
		RequireClaim: true,
		Update:       repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &claimAt, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if staleResult.Outcome != repository.StepCompletionStaleClaim {
		t.Fatalf("expected stale claim outcome, got %q", staleResult.Outcome)
	}

	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:      "build-stale",
		StepIndex:    0,
		ClaimToken:   "claim-a",
		RequireClaim: true,
		Update:       repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &claimAt, FinishedAt: &now},
	})
	if err != nil {
		t.Fatalf("active claim completion failed: %v", err)
	}
	if result.Outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completed outcome, got %q", result.Outcome)
	}
	if result.Step.Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected step success, got %q", result.Step.Status)
	}
}

func TestBuildRepository_RenewStepLease_SucceedsForActiveClaim(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-renew", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-renew", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-renew", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	claimedAt := time.Now().UTC()
	initialLease := claimedAt.Add(20 * time.Second)
	_, claimed, err := repo.ClaimPendingStep(context.Background(), "build-renew", 0, repository.StepClaim{WorkerID: "worker-a", ClaimToken: "claim-a", ClaimedAt: claimedAt, LeaseExpiresAt: initialLease})
	if err != nil || !claimed {
		t.Fatalf("claim failed: %v claimed=%v", err, claimed)
	}

	extendedLease := claimedAt.Add(60 * time.Second)
	step, outcome, err := repo.RenewStepLease(context.Background(), "build-renew", 0, "claim-a", extendedLease)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	if outcome != repository.StepCompletionCompleted {
		t.Fatalf("expected renewal outcome completed, got %q", outcome)
	}
	if step.LeaseExpiresAt == nil || !step.LeaseExpiresAt.Equal(extendedLease) {
		t.Fatalf("expected lease to be extended to %s, got %v", extendedLease, step.LeaseExpiresAt)
	}
}

func TestBuildRepository_RenewStepLease_RejectsStaleAndTerminal(t *testing.T) {
	repo := NewBuildRepository()
	_, err := repo.Create(context.Background(), domain.Build{ID: "build-renew-stale", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("setup create failed: %v", err)
	}
	_, err = repo.QueueBuild(context.Background(), "build-renew-stale", []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "first", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	_, err = repo.UpdateStatus(context.Background(), "build-renew-stale", domain.BuildStatusRunning, nil)
	if err != nil {
		t.Fatalf("set running status failed: %v", err)
	}

	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	leaseA := claimedAt.Add(20 * time.Second)
	_, claimed, err := repo.ClaimPendingStep(context.Background(), "build-renew-stale", 0, repository.StepClaim{WorkerID: "worker-a", ClaimToken: "claim-a", ClaimedAt: claimedAt, LeaseExpiresAt: leaseA})
	if err != nil || !claimed {
		t.Fatalf("claim failed: %v claimed=%v", err, claimed)
	}

	reclaimAt := claimedAt.Add(3 * time.Minute)
	_, reclaimed, err := repo.ReclaimExpiredStep(context.Background(), "build-renew-stale", 0, reclaimAt, repository.StepClaim{WorkerID: "worker-b", ClaimToken: "claim-b", ClaimedAt: reclaimAt, LeaseExpiresAt: reclaimAt.Add(30 * time.Second)})
	if err != nil || !reclaimed {
		t.Fatalf("reclaim failed: %v reclaimed=%v", err, reclaimed)
	}

	_, outcome, err := repo.RenewStepLease(context.Background(), "build-renew-stale", 0, "claim-a", reclaimAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("stale renew expected nil err, got %v", err)
	}
	if outcome != repository.StepCompletionStaleClaim {
		t.Fatalf("expected stale claim outcome, got %q", outcome)
	}

	_, outcome, err = repo.RenewStepLease(context.Background(), "build-renew-stale", 0, "wrong-token", reclaimAt.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("wrong token renew expected nil err, got %v", err)
	}
	if outcome != repository.StepCompletionStaleClaim {
		t.Fatalf("expected stale claim outcome for wrong token, got %q", outcome)
	}

	exitCode := 0
	finishedAt := reclaimAt.Add(3 * time.Minute)
	result, err := repo.CompleteStep(context.Background(), repository.CompleteStepRequest{
		BuildID:      "build-renew-stale",
		StepIndex:    0,
		ClaimToken:   "claim-b",
		RequireClaim: true,
		Update:       repository.StepUpdate{Status: domain.BuildStepStatusSuccess, ExitCode: &exitCode, StartedAt: &reclaimAt, FinishedAt: &finishedAt},
	})
	if err != nil || result.Outcome != repository.StepCompletionCompleted {
		t.Fatalf("active completion failed: err=%v outcome=%q", err, result.Outcome)
	}

	_, outcome, err = repo.RenewStepLease(context.Background(), "build-renew-stale", 0, "claim-b", finishedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("terminal renew expected nil err, got %v", err)
	}
	if outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected terminal duplicate outcome, got %q", outcome)
	}
}
