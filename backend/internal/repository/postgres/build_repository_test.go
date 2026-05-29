package postgres

// Core Postgres BuildRepository tests.
// Step claim/completion/lease lifecycle tests live in build_repository_step_lifecycle_test.go.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNewBuildRepository(t *testing.T) {
	repo := NewBuildRepository(&sql.DB{})
	if repo == nil {
		t.Fatal("expected repository, got nil")
	} else if repo.db == nil {
		t.Fatal("expected db to be set")
	}
}

func TestBuildRepository_Create(t *testing.T) {
	tests := []struct {
		name      string
		expectErr bool
	}{
		{name: "success"},
		{name: "query error", expectErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exec := mock.ExpectQuery("INSERT INTO builds")
			if tc.expectErr {
				exec.WillReturnError(errors.New("insert failed"))
			} else {
				now := time.Now().UTC()
				exec.WillReturnRows(sqlmock.NewRows([]string{"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id"}).AddRow("build-1", 1, "project-1", nil, 5, "pending", now, nil, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "external", nil, nil))
			}

			build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()}
			got, err := repo.Create(context.Background(), build)
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.ID != build.ID {
				t.Fatalf("expected id %q, got %q", build.ID, got.ID)
			}
			if got.BuildNumber != 1 {
				t.Fatalf("expected build number 1, got %d", got.BuildNumber)
			}
			if got.TriggerType != domain.BuildTriggerTypeManual {
				t.Fatalf("expected manual trigger type, got %q", got.TriggerType)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_GetByID(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		err       error
		expectErr error
	}{
		{name: "success"},
		{name: "not found", err: sql.ErrNoRows, expectErr: repository.ErrBuildNotFound},
		{name: "query error", err: errors.New("query failed"), expectErr: errors.New("query failed")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exp := mock.ExpectQuery("SELECT id, build_number, project_id, job_id, priority, status, created_at")
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
				row := make([]driver.Value, len(columns))
				row[0] = "build-1"
				row[1] = 1
				row[2] = "project-1"
				row[4] = 5
				row[5] = "queued"
				row[6] = now
				row[7] = now
				row[10] = 0
				row[11] = 1
				row[19] = "https://github.com/acme/repo.git"
				row[20] = "main"
				row[21] = "abc123"
				row[22] = "webhook"
				row[35] = "octocat"
				row[38] = "external"
				exp.WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))
			}

			got, err := repo.GetByID(context.Background(), "build-1")
			if tc.expectErr != nil {
				if tc.expectErr == repository.ErrBuildNotFound {
					if !errors.Is(err, repository.ErrBuildNotFound) {
						t.Fatalf("expected ErrBuildNotFound, got %v", err)
					}
				} else if err == nil || err.Error() != tc.expectErr.Error() {
					t.Fatalf("expected error %q, got %v", tc.expectErr.Error(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.Status != domain.BuildStatusQueued {
				t.Fatalf("expected queued status, got %q", got.Status)
			}
			if got.SourceRef == nil || *got.SourceRef != "main" {
				t.Fatalf("expected source_ref main, got %v", got.SourceRef)
			}
			if got.SourceSHA == nil || *got.SourceSHA != "abc123" {
				t.Fatalf("expected source_sha abc123, got %v", got.SourceSHA)
			}
			if got.TriggerType != domain.BuildTriggerTypeWebhook {
				t.Fatalf("expected webhook trigger type, got %q", got.TriggerType)
			}
			if got.TriggeredBy == nil || *got.TriggeredBy != "octocat" {
				t.Fatalf("expected triggered_by octocat, got %v", got.TriggeredBy)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_ListLatestByJobIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	jobA := "job-a"
	jobB := "job-b"
	buildListRowColumns := strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ",")
	buildA := make([]driver.Value, len(buildListRowColumns))
	buildA[0] = "build-2"
	buildA[1] = 2
	buildA[2] = "project-1"
	buildA[3] = jobA
	buildA[4] = 5
	buildA[5] = "success"
	buildA[6] = now
	buildA[7] = now
	buildA[8] = now
	buildA[9] = now
	buildA[10] = 1
	buildA[11] = 1
	buildA[21] = "manual"
	buildA[37] = "external"
	buildB := make([]driver.Value, len(buildListRowColumns))
	buildB[0] = "build-3"
	buildB[1] = 3
	buildB[2] = "project-1"
	buildB[3] = jobB
	buildB[4] = 5
	buildB[5] = "queued"
	buildB[6] = now.Add(time.Minute)
	buildB[7] = now.Add(time.Minute)
	buildB[10] = 0
	buildB[11] = 1
	buildB[21] = "manual"
	buildB[37] = "external"

	mock.ExpectQuery(`SELECT DISTINCT ON \(b.job_id\) b.id, b.build_number, b.project_id, b.job_id, b.priority, b.status, b.created_at`).
		WithArgs(jobA, jobB).
		WillReturnRows(sqlmock.NewRows(buildListRowColumns).
			AddRow(buildA...).
			AddRow(buildB...))

	latest, err := repo.ListLatestByJobIDs(context.Background(), []string{jobA, jobB, jobA, ""})
	if err != nil {
		t.Fatalf("ListLatestByJobIDs returned error: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected latest builds for 2 jobs, got %d", len(latest))
	}
	if latest[jobA].ID != "build-2" {
		t.Fatalf("expected build-2 for %s, got %s", jobA, latest[jobA].ID)
	}
	if latest[jobB].ID != "build-3" {
		t.Fatalf("expected build-3 for %s, got %s", jobB, latest[jobB].ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ListActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	buildListRowColumns := strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ",")
	row := make([]driver.Value, len(buildListRowColumns))
	row[0] = "build-running"
	row[1] = 3
	row[2] = "project-1"
	row[4] = 5
	row[5] = "running"
	row[6] = now
	row[10] = 0
	row[11] = 1
	row[21] = "manual"
	row[37] = "external"

	mock.ExpectQuery(`SELECT id, build_number, project_id, job_id, priority, status, created_at`).
		WithArgs(string(domain.BuildStatusPreparing), string(domain.BuildStatusQueued), string(domain.BuildStatusRunning)).
		WillReturnRows(sqlmock.NewRows(buildListRowColumns).AddRow(row...))

	builds, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive returned error: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected 1 active build, got %d", len(builds))
	}
	if builds[0].Status != domain.BuildStatusRunning {
		t.Fatalf("expected running status, got %q", builds[0].Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ListQueue_FiltersAndScansContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	queuedAt := now.Add(15 * time.Second)
	leaseExpiresAt := now.Add(time.Minute)
	jobID := "job-1"

	rows := sqlmock.NewRows(queueEntryMockColumns()).
		AddRow(queueEntryMockRow(domain.Build{
			ID:          "build-queued",
			BuildNumber: 11,
			ProjectID:   "project-1",
			JobID:       &jobID,
			Priority:    8,
			Status:      domain.BuildStatusQueued,
			CreatedAt:   now,
			QueuedAt:    &queuedAt,
		}, stringPtrValue("Platform"), stringPtrValue("platform"), stringPtrValue("Backend CI"), stringPtrValue("worker-1"), &leaseExpiresAt)...)

	mock.ExpectQuery(`SELECT .* FROM builds AS b`).
		WithArgs("project-1", "queued").
		WillReturnRows(rows)

	entries, err := repo.ListQueue(context.Background(), repository.QueueListParams{ProjectID: "project-1", Status: "queued"})
	if err != nil {
		t.Fatalf("ListQueue returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one queue entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Build.ID != "build-queued" {
		t.Fatalf("expected build-queued, got %q", entry.Build.ID)
	}
	if entry.ProjectName == nil || *entry.ProjectName != "Platform" {
		t.Fatalf("expected project name Platform, got %v", entry.ProjectName)
	}
	if entry.ProjectSlug == nil || *entry.ProjectSlug != "platform" {
		t.Fatalf("expected project slug platform, got %v", entry.ProjectSlug)
	}
	if entry.JobName == nil || *entry.JobName != "Backend CI" {
		t.Fatalf("expected job name Backend CI, got %v", entry.JobName)
	}
	if entry.WorkerID == nil || *entry.WorkerID != "worker-1" {
		t.Fatalf("expected worker-1, got %v", entry.WorkerID)
	}
	if entry.LeaseExpiresAt == nil || !entry.LeaseExpiresAt.Equal(leaseExpiresAt) {
		t.Fatalf("expected lease expiry %v, got %v", leaseExpiresAt, entry.LeaseExpiresAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_ListQueue_UsesPriorityAwareGroupedOrdering(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery(`ORDER BY\s+CASE b\.status WHEN 'queued' THEN 0 WHEN 'running' THEN 1 ELSE 2 END ASC,\s+CASE WHEN b\.status = 'queued' THEN b\.priority END DESC,\s+CASE WHEN b\.status = 'queued' THEN COALESCE\(b\.queued_at, b\.created_at\) END ASC,\s+CASE WHEN b\.status = 'running' THEN COALESCE\(b\.started_at, b\.created_at\) END ASC,\s+b\.created_at ASC,\s+b\.id ASC`).
		WillReturnRows(sqlmock.NewRows(queueEntryMockColumns()).
			AddRow(queueEntryMockRow(domain.Build{ID: "build-high", BuildNumber: 12, ProjectID: "project-1", Priority: 9, Status: domain.BuildStatusQueued, CreatedAt: now}, nil, nil, nil, nil, nil)...).
			AddRow(queueEntryMockRow(domain.Build{ID: "build-running", BuildNumber: 13, ProjectID: "project-1", Priority: 3, Status: domain.BuildStatusRunning, CreatedAt: now.Add(time.Second)}, nil, nil, nil, nil, nil)...))

	entries, err := repo.ListQueue(context.Background(), repository.QueueListParams{})
	if err != nil {
		t.Fatalf("ListQueue returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Build.ID != "build-high" || entries[1].Build.ID != "build-running" {
		t.Fatalf("expected queued entry before running entry, got %#v", entries)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_UpdateStatus(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		err       error
		expectErr error
	}{
		{name: "success"},
		{name: "not found", err: sql.ErrNoRows, expectErr: repository.ErrBuildNotFound},
		{name: "query error", err: errors.New("update failed"), expectErr: errors.New("update failed")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sql mock: %v", err)
			}

			repo := NewBuildRepository(db)
			exp := mock.ExpectQuery("UPDATE builds")
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				exp.WillReturnRows(sqlmock.NewRows([]string{"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id"}).AddRow("build-1", 1, "project-1", nil, 5, "running", now, now, now, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "external", nil, nil))
			}

			got, err := repo.UpdateStatus(context.Background(), "build-1", domain.BuildStatusRunning, nil)
			if tc.expectErr != nil {
				if tc.expectErr == repository.ErrBuildNotFound {
					if !errors.Is(err, repository.ErrBuildNotFound) {
						t.Fatalf("expected ErrBuildNotFound, got %v", err)
					}
				} else if err == nil || err.Error() != tc.expectErr.Error() {
					t.Fatalf("expected error %q, got %v", tc.expectErr.Error(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got.Status != domain.BuildStatusRunning {
				t.Fatalf("expected running status, got %q", got.Status)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func queueEntryMockColumns() []string {
	return []string{
		"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id", "project_name", "project_slug", "job_name", "claimed_by", "claim_expires_at",
	}
}

func queueEntryMockRow(build domain.Build, projectName *string, projectSlug *string, jobName *string, workerID *string, leaseExpiresAt *time.Time) []driver.Value {
	row := make([]driver.Value, 45)
	row[0] = build.ID
	row[1] = build.BuildNumber
	row[2] = build.ProjectID
	if build.JobID != nil {
		row[3] = *build.JobID
	}
	row[4] = build.Priority
	row[5] = string(build.Status)
	row[6] = build.CreatedAt
	if build.QueuedAt != nil {
		row[7] = *build.QueuedAt
	}
	if build.StartedAt != nil {
		row[8] = *build.StartedAt
	}
	if build.FinishedAt != nil {
		row[9] = *build.FinishedAt
	}
	row[10] = build.CurrentStepIndex
	row[11] = build.AttemptNumber
	row[21] = "manual"
	row[37] = "external"
	if projectName != nil {
		row[40] = *projectName
	}
	if projectSlug != nil {
		row[41] = *projectSlug
	}
	if jobName != nil {
		row[42] = *jobName
	}
	if workerID != nil {
		row[43] = *workerID
	}
	if leaseExpiresAt != nil {
		row[44] = *leaseExpiresAt
	}
	return row
}

func stringPtrValue(value string) *string {
	return &value
}

func TestBuildRepository_QueueBuild_PersistsBuildAndSteps(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE builds").WillReturnRows(
		sqlmock.NewRows([]string{"id", "build_number", "project_id", "job_id", "priority", "status", "created_at", "queued_at", "started_at", "finished_at", "current_step_index", "attempt_number", "rerun_of_build_id", "rerun_from_step_index", "error_message", "pipeline_config_yaml", "pipeline_name", "pipeline_source", "pipeline_path", "repo_url", "ref", "commit_sha", "trigger_kind", "scm_provider", "event_type", "trigger_repository_owner", "trigger_repository_name", "trigger_repository_url", "trigger_raw_ref", "trigger_ref", "trigger_ref_type", "trigger_ref_name", "trigger_deleted", "trigger_commit_sha", "trigger_delivery_id", "trigger_actor", "requested_image_ref", "resolved_image_ref", "image_source_kind", "managed_image_id", "managed_image_version_id"}).
			AddRow("build-1", 1, "project-1", nil, 5, "queued", now, now, nil, nil, 0, 1, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "manual", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "external", nil, nil),
	)
	mock.ExpectExec("DELETE FROM build_steps").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO build_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	build, err := repo.QueueBuild(context.Background(), "build-1", []domain.BuildStep{
		{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "lint", Status: domain.BuildStepStatusPending},
		{ID: "step-2", BuildID: "build-1", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusPending},
	})
	if err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_GetStepsByBuildID_Ordered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT id, build_id, step_index, node_id, group_name, depends_on_node_ids, name, image, command").WillReturnRows(
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "lint", "", "go", "[\"test\"]", "{}", "/workspace", 60, "success", nil, nil, nil, nil, now, now, 0, "ok", "", nil, "[]", nil, nil, nil, "external", nil, nil).
			AddRow("step-2", "build-1", 1, nil, nil, "[]", "test", "", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "pending", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "[]", nil, nil, nil, "external", nil, nil),
	)

	steps, err := repo.GetStepsByBuildID(context.Background(), "build-1")
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
	if steps[0].Command != "go" || steps[0].WorkingDir != "/workspace" {
		t.Fatalf("expected persisted command and working dir, got command=%q working_dir=%q", steps[0].Command, steps[0].WorkingDir)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_UpdateStepByIndex(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	exitCode := 1
	stdout := "partial output"
	stderr := "step failed"
	errMsg := "step failed"

	mock.ExpectQuery("UPDATE build_steps").WillReturnRows(
		sqlmock.NewRows(stepMockColumns).
			AddRow("step-1", "build-1", 0, nil, nil, "[]", "lint", "", "go", "[\"test\",\"./...\"]", "{}", "/workspace", 60, "failed", "worker-1", nil, nil, nil, now, now, exitCode, stdout, stderr, errMsg, "[]", nil, nil, nil, "external", nil, nil),
	)

	workerID := "worker-1"
	step, err := repo.UpdateStepByIndex(context.Background(), "build-1", 0, repository.StepUpdate{
		Status:       domain.BuildStepStatusFailed,
		WorkerID:     &workerID,
		ExitCode:     &exitCode,
		Stdout:       &stdout,
		Stderr:       &stderr,
		ErrorMessage: &errMsg,
		StartedAt:    &now,
		FinishedAt:   &now,
	})
	if err != nil {
		t.Fatalf("update step failed: %v", err)
	}
	if step.Status != domain.BuildStepStatusFailed {
		t.Fatalf("expected failed step status, got %q", step.Status)
	}
	if step.ExitCode == nil || *step.ExitCode != exitCode {
		t.Fatalf("expected exit code %d, got %v", exitCode, step.ExitCode)
	}
	if step.Stdout == nil || *step.Stdout != stdout {
		t.Fatalf("expected stdout %q, got %v", stdout, step.Stdout)
	}
	if step.Stderr == nil || *step.Stderr != stderr {
		t.Fatalf("expected stderr %q, got %v", stderr, step.Stderr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
