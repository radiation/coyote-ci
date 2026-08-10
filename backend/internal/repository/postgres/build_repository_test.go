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

func buildColumnPosition(columns []string, want string) int {
	for idx, column := range columns {
		if column == want {
			return idx
		}
	}
	return -1
}

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
				columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
				row := make([]driver.Value, len(columns))
				row[buildColumnPosition(columns, "id")] = "build-1"
				row[buildColumnPosition(columns, "build_number")] = 1
				row[buildColumnPosition(columns, "project_id")] = "project-1"
				row[buildColumnPosition(columns, "priority")] = 5
				row[buildColumnPosition(columns, "status")] = "pending"
				row[buildColumnPosition(columns, "created_at")] = now
				row[buildColumnPosition(columns, "current_step_index")] = 0
				row[buildColumnPosition(columns, "attempt_number")] = 1
				row[buildColumnPosition(columns, "trigger_kind")] = "webhook"
				row[buildColumnPosition(columns, "pull_request_number")] = int64(42)
				row[buildColumnPosition(columns, "pull_request_action")] = "opened"
				row[buildColumnPosition(columns, "pull_request_url")] = "https://github.example.com/acme/repo/pull/42"
				row[buildColumnPosition(columns, "pull_request_base_ref")] = "main"
				row[buildColumnPosition(columns, "pull_request_base_sha")] = "base-sha"
				row[buildColumnPosition(columns, "pull_request_head_ref")] = "feature/pr-42"
				row[buildColumnPosition(columns, "pull_request_head_sha")] = "head-sha"
				row[buildColumnPosition(columns, "pull_request_source_mode")] = "head"
				exec.WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))
			}

			provider := "github"
			eventType := "pull_request"
			headRef := "feature/pr-42"
			headSHA := "head-sha"
			build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC(), Trigger: domain.BuildTrigger{Kind: domain.BuildTriggerKindWebhook, SCMProvider: &provider, EventType: &eventType, Ref: &headRef, RefName: &headRef, CommitSHA: &headSHA, PullRequest: &domain.PullRequestSnapshot{Number: 42, Action: "opened", URL: "https://github.example.com/acme/repo/pull/42", BaseRef: "main", BaseSHA: "base-sha", HeadRef: headRef, HeadSHA: headSHA, SourceMode: domain.PullRequestSourceModeHead}}}
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
				t.Fatalf("expected manual trigger type from returned row, got %q", got.TriggerType)
			}
			if got.Trigger.PullRequest == nil || got.Trigger.PullRequest.HeadSHA != "head-sha" {
				t.Fatalf("expected pull-request snapshot after create, got %+v", got.Trigger.PullRequest)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestBuildRepository_RejectsMalformedRepositoryIdentitySnapshotsBeforeQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	whitespace := "  "
	connectionID := "connection-1"
	providerRepositoryID := "provider-repository-1"
	invalidBuild := domain.Build{ID: "build-invalid", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC(), RegisteredRepositoryID: &whitespace, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID}
	repo := NewBuildRepository(db)
	if _, createErr := repo.Create(context.Background(), invalidBuild); createErr == nil {
		t.Fatal("expected malformed snapshot to be rejected")
	}
	if _, queueErr := repo.CreateQueuedBuild(context.Background(), invalidBuild, nil); queueErr == nil {
		t.Fatal("expected queued malformed snapshot to be rejected")
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("expected no database query for malformed snapshots: %v", expectationsErr)
	}
}

func TestBuildRepository_Create_ExplicitJobBuildNumber(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	jobID := "job-1"
	columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
	row := make([]driver.Value, len(columns))
	row[buildColumnPosition(columns, "id")] = "build-1"
	row[buildColumnPosition(columns, "build_number")] = 7
	row[buildColumnPosition(columns, "project_id")] = "project-1"
	row[buildColumnPosition(columns, "job_id")] = jobID
	row[buildColumnPosition(columns, "priority")] = 5
	row[buildColumnPosition(columns, "status")] = "pending"
	row[buildColumnPosition(columns, "created_at")] = now
	row[buildColumnPosition(columns, "current_step_index")] = 0
	row[buildColumnPosition(columns, "attempt_number")] = 1
	row[buildColumnPosition(columns, "trigger_kind")] = "manual"
	row[buildColumnPosition(columns, "image_source_kind")] = "external"
	mock.ExpectQuery(`GREATEST\(next_build_number, \$4 \+ 1\)[\s\S]*NULLIF\(\$4, 0\)`).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))

	build := domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, BuildNumber: 7, Status: domain.BuildStatusPending, CreatedAt: now}
	got, err := repo.Create(context.Background(), build)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.BuildNumber != 7 {
		t.Fatalf("expected build number 7, got %d", got.BuildNumber)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestBuildRepository_CreateQueuedBuild_ExplicitJobBuildNumber(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	jobID := "job-1"
	buildColumns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
	buildRow := make([]driver.Value, len(buildColumns))
	buildRow[0] = "build-queued"
	buildRow[1] = 9
	buildRow[2] = "project-1"
	buildRow[3] = jobID
	buildRow[4] = 5
	buildRow[5] = "queued"
	buildRow[6] = now
	buildRow[7] = now
	buildRow[buildColumnPosition(buildColumns, "current_step_index")] = 0
	buildRow[buildColumnPosition(buildColumns, "attempt_number")] = 1
	buildRow[buildColumnPosition(buildColumns, "trigger_kind")] = "manual"
	buildRow[buildColumnPosition(buildColumns, "image_source_kind")] = "external"

	mock.ExpectBegin()
	mock.ExpectQuery(`GREATEST\(next_build_number, \$4 \+ 1\)[\s\S]*NULLIF\(\$4, 0\)`).
		WillReturnRows(sqlmock.NewRows(buildColumns).AddRow(buildRow...))
	mock.ExpectExec(`INSERT INTO build_steps`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	queuedAt := now
	build := domain.Build{ID: "build-queued", ProjectID: "project-1", JobID: &jobID, BuildNumber: 9, CreatedAt: now, QueuedAt: &queuedAt}
	got, err := repo.CreateQueuedBuild(context.Background(), build, []domain.BuildStep{{ID: "step-1", StepIndex: 0, Name: "test", Status: domain.BuildStepStatusPending}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.BuildNumber != 9 {
		t.Fatalf("expected build number 9, got %d", got.BuildNumber)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
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
				row[buildColumnPosition(columns, "current_step_index")] = 0
				row[buildColumnPosition(columns, "attempt_number")] = 1
				row[buildColumnPosition(columns, "repo_url")] = "https://github.com/acme/repo.git"
				row[buildColumnPosition(columns, "ref")] = "main"
				row[buildColumnPosition(columns, "commit_sha")] = "abc123"
				row[buildColumnPosition(columns, "source_author_email")] = "ada@example.com"
				row[buildColumnPosition(columns, "source_committer_email")] = "grace@example.com"
				row[buildColumnPosition(columns, "trigger_kind")] = "webhook"
				row[buildColumnPosition(columns, "trigger_actor")] = "octocat"
				row[buildColumnPosition(columns, "pull_request_number")] = int64(42)
				row[buildColumnPosition(columns, "pull_request_action")] = "synchronize"
				row[buildColumnPosition(columns, "pull_request_url")] = "https://github.example.com/acme/repo/pull/42"
				row[buildColumnPosition(columns, "pull_request_base_ref")] = "main"
				row[buildColumnPosition(columns, "pull_request_base_sha")] = "base-sha"
				row[buildColumnPosition(columns, "pull_request_head_ref")] = "feature/pr-42"
				row[buildColumnPosition(columns, "pull_request_head_sha")] = "abc123"
				row[buildColumnPosition(columns, "pull_request_source_mode")] = "head"
				row[buildColumnPosition(columns, "image_source_kind")] = "external"
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
			if got.Trigger.PullRequest == nil || got.Trigger.PullRequest.Number != 42 || got.Trigger.PullRequest.HeadSHA != "abc123" || got.Trigger.PullRequest.SourceMode != domain.PullRequestSourceModeHead {
				t.Fatalf("expected pull-request snapshot round trip, got %+v", got.Trigger.PullRequest)
			}
			if got.SourceAuthorEmail == nil || *got.SourceAuthorEmail != "ada@example.com" {
				t.Fatalf("expected source_author_email ada@example.com, got %v", got.SourceAuthorEmail)
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
	buildA[buildColumnPosition(buildListRowColumns, "current_step_index")] = 1
	buildA[buildColumnPosition(buildListRowColumns, "attempt_number")] = 1
	buildA[buildColumnPosition(buildListRowColumns, "trigger_kind")] = "manual"
	buildA[buildColumnPosition(buildListRowColumns, "image_source_kind")] = "external"
	buildB := make([]driver.Value, len(buildListRowColumns))
	buildB[0] = "build-3"
	buildB[1] = 3
	buildB[2] = "project-1"
	buildB[3] = jobB
	buildB[4] = 5
	buildB[5] = "queued"
	buildB[6] = now.Add(time.Minute)
	buildB[7] = now.Add(time.Minute)
	buildB[buildColumnPosition(buildListRowColumns, "current_step_index")] = 0
	buildB[buildColumnPosition(buildListRowColumns, "attempt_number")] = 1
	buildB[buildColumnPosition(buildListRowColumns, "trigger_kind")] = "manual"
	buildB[buildColumnPosition(buildListRowColumns, "image_source_kind")] = "external"

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
	row[buildColumnPosition(buildListRowColumns, "current_step_index")] = 0
	row[buildColumnPosition(buildListRowColumns, "attempt_number")] = 1
	row[buildColumnPosition(buildListRowColumns, "trigger_kind")] = "manual"
	row[buildColumnPosition(buildListRowColumns, "image_source_kind")] = "external"

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

func TestBuildRepository_UpdateSourceProvenance(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sql mock: %v", err)
	}

	repo := NewBuildRepository(db)
	now := time.Now().UTC()
	columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
	row := make([]driver.Value, len(columns))
	row[buildColumnPosition(columns, "id")] = "build-1"
	row[buildColumnPosition(columns, "build_number")] = 1
	row[buildColumnPosition(columns, "project_id")] = "project-1"
	row[buildColumnPosition(columns, "priority")] = 5
	row[buildColumnPosition(columns, "status")] = "queued"
	row[buildColumnPosition(columns, "created_at")] = now
	row[buildColumnPosition(columns, "current_step_index")] = 0
	row[buildColumnPosition(columns, "attempt_number")] = 1
	row[buildColumnPosition(columns, "commit_sha")] = "deadbeef"
	row[buildColumnPosition(columns, "source_author_name")] = "Ada Lovelace"
	row[buildColumnPosition(columns, "source_author_email")] = "ada@example.com"
	row[buildColumnPosition(columns, "source_committer_name")] = "Grace Hopper"
	row[buildColumnPosition(columns, "source_committer_email")] = "grace@example.com"
	row[buildColumnPosition(columns, "trigger_kind")] = "manual"
	row[buildColumnPosition(columns, "image_source_kind")] = "external"

	mock.ExpectQuery(`UPDATE builds`).
		WithArgs("build-1", "deadbeef", "Ada Lovelace", "ada@example.com", "Grace Hopper", "grace@example.com").
		WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))

	build, err := repo.UpdateSourceProvenance(context.Background(), "build-1", repository.SourceProvenanceUpdate{
		CommitSHA:      "deadbeef",
		AuthorName:     "Ada Lovelace",
		AuthorEmail:    "ada@example.com",
		CommitterName:  "Grace Hopper",
		CommitterEmail: "grace@example.com",
	})
	if err != nil {
		t.Fatalf("UpdateSourceProvenance returned error: %v", err)
	}
	if build.SourceAuthorEmail == nil || *build.SourceAuthorEmail != "ada@example.com" {
		t.Fatalf("expected source_author_email ada@example.com, got %v", build.SourceAuthorEmail)
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
				columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
				row := make([]driver.Value, len(columns))
				row[buildColumnPosition(columns, "id")] = "build-1"
				row[buildColumnPosition(columns, "build_number")] = 1
				row[buildColumnPosition(columns, "project_id")] = "project-1"
				row[buildColumnPosition(columns, "priority")] = 5
				row[buildColumnPosition(columns, "status")] = "running"
				row[buildColumnPosition(columns, "created_at")] = now
				row[buildColumnPosition(columns, "queued_at")] = now
				row[buildColumnPosition(columns, "started_at")] = now
				row[buildColumnPosition(columns, "current_step_index")] = 0
				row[buildColumnPosition(columns, "attempt_number")] = 1
				row[buildColumnPosition(columns, "trigger_kind")] = "manual"
				row[buildColumnPosition(columns, "image_source_kind")] = "external"
				exp.WillReturnRows(sqlmock.NewRows(columns).AddRow(row...))
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
	columns := strings.Split(strings.ReplaceAll(buildListColumns, " ", ""), ",")
	return append(columns, "project_name", "project_slug", "job_name", "claimed_by", "claim_expires_at")
}

func queueEntryMockRow(build domain.Build, projectName *string, projectSlug *string, jobName *string, workerID *string, leaseExpiresAt *time.Time) []driver.Value {
	columns := queueEntryMockColumns()
	row := make([]driver.Value, len(columns))
	row[buildColumnPosition(columns, "id")] = build.ID
	row[buildColumnPosition(columns, "build_number")] = build.BuildNumber
	row[buildColumnPosition(columns, "project_id")] = build.ProjectID
	if build.JobID != nil {
		row[buildColumnPosition(columns, "job_id")] = *build.JobID
	}
	row[buildColumnPosition(columns, "priority")] = build.Priority
	row[buildColumnPosition(columns, "status")] = string(build.Status)
	row[buildColumnPosition(columns, "created_at")] = build.CreatedAt
	if build.QueuedAt != nil {
		row[buildColumnPosition(columns, "queued_at")] = *build.QueuedAt
	}
	if build.StartedAt != nil {
		row[buildColumnPosition(columns, "started_at")] = *build.StartedAt
	}
	if build.FinishedAt != nil {
		row[buildColumnPosition(columns, "finished_at")] = *build.FinishedAt
	}
	row[buildColumnPosition(columns, "current_step_index")] = build.CurrentStepIndex
	row[buildColumnPosition(columns, "attempt_number")] = build.AttemptNumber
	row[buildColumnPosition(columns, "trigger_kind")] = "manual"
	row[buildColumnPosition(columns, "image_source_kind")] = "external"
	if projectName != nil {
		row[buildColumnPosition(columns, "project_name")] = *projectName
	}
	if projectSlug != nil {
		row[buildColumnPosition(columns, "project_slug")] = *projectSlug
	}
	if jobName != nil {
		row[buildColumnPosition(columns, "job_name")] = *jobName
	}
	if workerID != nil {
		row[buildColumnPosition(columns, "claimed_by")] = *workerID
	}
	if leaseExpiresAt != nil {
		row[buildColumnPosition(columns, "claim_expires_at")] = *leaseExpiresAt
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
	columns := strings.Split(strings.ReplaceAll(buildColumns, " ", ""), ",")
	row := make([]driver.Value, len(columns))
	row[buildColumnPosition(columns, "id")] = "build-1"
	row[buildColumnPosition(columns, "build_number")] = 1
	row[buildColumnPosition(columns, "project_id")] = "project-1"
	row[buildColumnPosition(columns, "priority")] = 5
	row[buildColumnPosition(columns, "status")] = "queued"
	row[buildColumnPosition(columns, "created_at")] = now
	row[buildColumnPosition(columns, "queued_at")] = now
	row[buildColumnPosition(columns, "current_step_index")] = 0
	row[buildColumnPosition(columns, "attempt_number")] = 1
	row[buildColumnPosition(columns, "trigger_kind")] = "manual"
	row[buildColumnPosition(columns, "image_source_kind")] = "external"

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE builds").WillReturnRows(
		sqlmock.NewRows(columns).AddRow(row...),
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
