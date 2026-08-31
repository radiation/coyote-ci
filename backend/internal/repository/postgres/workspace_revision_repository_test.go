package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWorkspaceRevisionRepositoryMarkPublishedIfClaimed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	publishing := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "publishing", nil, nil, nil, now, nil, nil)
	publishedAt := now.Add(time.Minute)
	published := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "published", "sha256:one", "revisions/revision-1", nil, now, publishedAt, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("SELECT status = 'running' AND claim_token = \\$2.*FROM build_jobs").WithArgs("job-1", "claim-active").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(true))
	mock.ExpectQuery("UPDATE workspace_revisions.*status = 'published'").WithArgs("revision-1", "sha256:one", "revisions/revision-1", nil, publishedAt).WillReturnRows(workspaceRevisionMockRows(published))
	mock.ExpectCommit()

	repo := NewWorkspaceRevisionRepository(db)
	revision, publishErr := repo.MarkPublishedIfClaimed(context.Background(), "revision-1", "claim-active", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}, publishedAt)
	if publishErr != nil || revision.Status != domain.WorkspaceRevisionStatusPublished {
		t.Fatalf("publish revision=%#v err=%v", revision, publishErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryMarkPublishedIfClaimedRejectsStaleClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	publishing := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "publishing", nil, nil, nil, now, nil, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("SELECT status = 'running' AND claim_token = \\$2.*FROM build_jobs").WithArgs("job-1", "claim-stale").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(false))
	mock.ExpectRollback()

	repo := NewWorkspaceRevisionRepository(db)
	_, publishErr := repo.MarkPublishedIfClaimed(context.Background(), "revision-1", "claim-stale", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}, now)
	if !errors.Is(publishErr, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("expected stale claim, got %v", publishErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryCreateAndLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	publishing := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "publishing", nil, nil, nil, now, nil, nil)
	published := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "published", "sha256:one", "revisions/revision-1", int64(10), now, now, nil)
	mock.ExpectQuery("SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = \\$1").WithArgs("job-1").WillReturnRows(sqlmock.NewRows([]string{"build_id", "node_id", "attempt_number"}).AddRow("build-1", "compile", 1))
	mock.ExpectQuery("INSERT INTO workspace_revisions").WithArgs("revision-1", "job-1", "build-1", "compile", 1, nil, domain.WorkspaceRevisionStatusPublishing, now).WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*producing_execution_job_id = \\$1").WithArgs("job-1").WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("SELECT wr.id, wr.producing_execution_job_id.*status = 'published'").WithArgs("build-1", "compile").WillReturnRows(workspaceRevisionMockRows(published))

	repo := NewWorkspaceRevisionRepository(db)
	created, createErr := repo.CreatePublishing(context.Background(), domain.WorkspaceRevision{ID: "revision-1", ProducingExecutionJobID: "job-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 1, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: now})
	if createErr != nil || created.Status != domain.WorkspaceRevisionStatusPublishing {
		t.Fatalf("create revision=%#v err=%v", created, createErr)
	}
	if _, lookupErr := repo.GetByProducingExecutionJob(context.Background(), "job-1"); lookupErr != nil {
		t.Fatalf("lookup by producing job: %v", lookupErr)
	}
	lookup, lookupErr := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile")
	if lookupErr != nil || lookup.ID != "revision-1" {
		t.Fatalf("lookup published revision=%#v err=%v", lookup, lookupErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryPublicationIsIdempotentAndDeletionIsTerminal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	published := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "published", "sha256:one", "revisions/revision-1", nil, now, now, nil)
	deletedAt := now.Add(time.Minute)
	deleted := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "deleted", "sha256:one", "revisions/revision-1", nil, now, now, deletedAt)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(published))
	mock.ExpectCommit()
	mock.ExpectQuery("UPDATE workspace_revisions.*status = 'deleted'").WithArgs("revision-1", deletedAt).WillReturnRows(workspaceRevisionMockRows(deleted))

	repo := NewWorkspaceRevisionRepository(db)
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}
	if _, publishErr := repo.MarkPublishedIfClaimed(context.Background(), "revision-1", "claim-expired", publication, now); publishErr != nil {
		t.Fatalf("idempotent publication: %v", publishErr)
	}
	markedDeleted, deleteErr := repo.MarkDeleted(context.Background(), "revision-1", deletedAt)
	if deleteErr != nil || markedDeleted.Status != domain.WorkspaceRevisionStatusDeleted {
		t.Fatalf("delete revision=%#v err=%v", markedDeleted, deleteErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryCreateAndDeleteAreIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	publishing := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "publishing", nil, nil, nil, now, nil, nil)
	deleted := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "deleted", "sha256:one", "revisions/revision-1", nil, now, now, now)
	mock.ExpectQuery("SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = \\$1").WithArgs("job-1").WillReturnRows(sqlmock.NewRows([]string{"build_id", "node_id", "attempt_number"}).AddRow("build-1", "compile", 1))
	mock.ExpectQuery("INSERT INTO workspace_revisions").WithArgs("revision-1", "job-1", "build-1", "compile", 1, nil, domain.WorkspaceRevisionStatusPublishing, now).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*producing_execution_job_id = \\$1").WithArgs("job-1").WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("UPDATE workspace_revisions.*status = 'deleted'").WithArgs("revision-1", now).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*WHERE id = \\$1").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(deleted))

	repo := NewWorkspaceRevisionRepository(db)
	revision := domain.WorkspaceRevision{ID: "revision-1", ProducingExecutionJobID: "job-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 1, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: now}
	if _, createErr := repo.CreatePublishing(context.Background(), revision); createErr != nil {
		t.Fatalf("idempotent create: %v", createErr)
	}
	if markedDeleted, deleteErr := repo.MarkDeleted(context.Background(), "revision-1", now); deleteErr != nil || markedDeleted.Status != domain.WorkspaceRevisionStatusDeleted {
		t.Fatalf("idempotent delete revision=%#v err=%v", markedDeleted, deleteErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryRejectsConflictingPublishedMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	published := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "published", "sha256:one", "revisions/revision-1", nil, now, now, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(published))
	mock.ExpectRollback()

	repo := NewWorkspaceRevisionRepository(db)
	_, publishErr := repo.MarkPublishedIfClaimed(context.Background(), "revision-1", "claim-active", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:two", StorageKey: "revisions/revision-1"}, now)
	if !errors.Is(publishErr, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected immutable metadata conflict, got %v", publishErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryRejectsInvalidRequests(t *testing.T) {
	repo := NewWorkspaceRevisionRepository(nil)
	if _, err := repo.CreatePublishing(context.Background(), domain.WorkspaceRevision{}); !errors.Is(err, domain.ErrInvalidWorkspaceRevision) {
		t.Fatalf("expected invalid create request, got %v", err)
	}
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), "", "claim", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}, time.Now().UTC()); !errors.Is(err, domain.ErrInvalidWorkspaceRevision) {
		t.Fatalf("expected invalid publication request, got %v", err)
	}
	if _, err := repo.MarkDeleted(context.Background(), "", time.Now().UTC()); !errors.Is(err, domain.ErrInvalidWorkspaceRevision) {
		t.Fatalf("expected invalid deletion request, got %v", err)
	}
}

func TestWorkspaceRevisionRepositoryRejectsExpiredLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	publishing := workspaceRevisionMockRow("revision-1", "job-1", "build-1", "compile", 1, "publishing", nil, nil, nil, now, nil, nil)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("revision-1").WillReturnRows(workspaceRevisionMockRows(publishing))
	mock.ExpectQuery("claim_expires_at > NOW\\(\\)").WithArgs("job-1", "claim-expired").WillReturnRows(sqlmock.NewRows([]string{"owned"}).AddRow(false))
	mock.ExpectRollback()

	repo := NewWorkspaceRevisionRepository(db)
	_, publishErr := repo.MarkPublishedIfClaimed(context.Background(), "revision-1", "claim-expired", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/revision-1"}, now)
	if !errors.Is(publishErr, repository.ErrWorkspaceRevisionStaleClaim) {
		t.Fatalf("expected expired lease to reject publication, got %v", publishErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryCreateRejectsIDCollisionAndOwnerMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery("SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = \\$1").WithArgs("job-id-collision").WillReturnRows(sqlmock.NewRows([]string{"build_id", "node_id", "attempt_number"}).AddRow("build-1", "compile", 1))
	mock.ExpectQuery("INSERT INTO workspace_revisions").WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "workspace_revisions_pkey"})
	mock.ExpectQuery("SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = \\$1").WithArgs("job-mismatch").WillReturnRows(sqlmock.NewRows([]string{"build_id", "node_id", "attempt_number"}).AddRow("build-1", "compile", 1))

	repo := NewWorkspaceRevisionRepository(db)
	_, collisionErr := repo.CreatePublishing(context.Background(), domain.WorkspaceRevision{ID: "revision-1", ProducingExecutionJobID: "job-id-collision", BuildID: "build-1", NodeID: "compile", AttemptNumber: 1, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: now})
	if !errors.Is(collisionErr, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected revision id collision, got %v", collisionErr)
	}
	_, mismatchErr := repo.CreatePublishing(context.Background(), domain.WorkspaceRevision{ID: "revision-2", ProducingExecutionJobID: "job-mismatch", BuildID: "other-build", NodeID: "compile", AttemptNumber: 1, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: now})
	if !errors.Is(mismatchErr, repository.ErrWorkspaceRevisionConflict) {
		t.Fatalf("expected authoritative owner mismatch, got %v", mismatchErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryMarkDeletedPropagatesLookupError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	wantErr := errors.New("database unavailable")
	mock.ExpectQuery("UPDATE workspace_revisions.*status = 'deleted'").WithArgs("revision-1", now).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*WHERE id = \\$1").WithArgs("revision-1").WillReturnError(wantErr)

	repo := NewWorkspaceRevisionRepository(db)
	_, deleteErr := repo.MarkDeleted(context.Background(), "revision-1", now)
	if !errors.Is(deleteErr, wantErr) {
		t.Fatalf("expected lookup error %v, got %v", wantErr, deleteErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryMapsNotFoundResults(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT id, producing_execution_job_id.*producing_execution_job_id = \\$1").WithArgs("missing-job").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT wr.id, wr.producing_execution_job_id.*status = 'published'").WithArgs("build-1", "compile").WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*FOR UPDATE").WithArgs("missing-revision").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	repo := NewWorkspaceRevisionRepository(db)
	if _, err := repo.GetByProducingExecutionJob(context.Background(), "missing-job"); !errors.Is(err, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("expected missing producer lookup, got %v", err)
	}
	if _, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile"); !errors.Is(err, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("expected missing published lookup, got %v", err)
	}
	if _, err := repo.MarkPublishedIfClaimed(context.Background(), "missing-revision", "claim", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/1"}, time.Now().UTC()); !errors.Is(err, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("expected missing revision publication, got %v", err)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionRepositoryPropagatesDatabaseErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	wantErr := errors.New("database unavailable")
	mock.ExpectQuery("SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = \\$1").WithArgs("job-1").WillReturnError(wantErr)
	mock.ExpectQuery("SELECT id, producing_execution_job_id.*producing_execution_job_id = \\$1").WithArgs("job-1").WillReturnError(wantErr)
	mock.ExpectQuery("SELECT wr.id, wr.producing_execution_job_id.*status = 'published'").WithArgs("build-1", "compile").WillReturnError(wantErr)
	mock.ExpectQuery("UPDATE workspace_revisions.*status = 'deleted'").WithArgs("revision-1", now).WillReturnError(wantErr)

	repo := NewWorkspaceRevisionRepository(db)
	revision := domain.WorkspaceRevision{ID: "revision-1", ProducingExecutionJobID: "job-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 1, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: now}
	if _, err := repo.CreatePublishing(context.Background(), revision); !errors.Is(err, wantErr) {
		t.Fatalf("expected create database error, got %v", err)
	}
	if _, err := repo.GetByProducingExecutionJob(context.Background(), "job-1"); !errors.Is(err, wantErr) {
		t.Fatalf("expected producer lookup database error, got %v", err)
	}
	if _, err := repo.GetPublishedByBuildNode(context.Background(), "build-1", "compile"); !errors.Is(err, wantErr) {
		t.Fatalf("expected published lookup database error, got %v", err)
	}
	if _, err := repo.MarkDeleted(context.Background(), "revision-1", now); !errors.Is(err, wantErr) {
		t.Fatalf("expected deletion database error, got %v", err)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatalf("unmet expectations: %v", expectationErr)
	}
}

func TestWorkspaceRevisionComparisonHelpers(t *testing.T) {
	parent := "parent-1"
	size := int64(10)
	left := domain.WorkspaceRevision{ID: "revision-1", ProducingExecutionJobID: "job-1", BuildID: "build-1", NodeID: "compile", AttemptNumber: 1, ParentRevisionID: &parent, Status: domain.WorkspaceRevisionStatusPublishing}
	right := left
	if !sameWorkspaceRevisionCreate(left, right) {
		t.Fatal("expected equivalent revisions")
	}
	right.ParentRevisionID = nil
	if sameWorkspaceRevisionCreate(left, right) {
		t.Fatal("expected distinct parent revision")
	}
	published := domain.WorkspaceRevision{ContentDigest: stringValue("sha256:one"), StorageKey: stringValue("revisions/1"), SizeBytes: &size}
	if !sameWorkspaceRevisionPublication(published, domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/1", SizeBytes: &size}) {
		t.Fatal("expected equivalent publication")
	}
	if sameWorkspaceRevisionPublication(published, domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/1"}) {
		t.Fatal("expected distinct optional size")
	}
}

func stringValue(value string) *string {
	return &value
}

func workspaceRevisionMockRows(row []driver.Value) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "producing_execution_job_id", "build_id", "node_id", "attempt_number", "parent_revision_id", "status", "content_digest", "storage_key", "size_bytes", "created_at", "published_at", "deleted_at"}).AddRow(row...)
}

func workspaceRevisionMockRow(id string, jobID string, buildID string, nodeID string, attempt int, status string, digest driver.Value, storageKey driver.Value, size driver.Value, createdAt time.Time, publishedAt driver.Value, deletedAt driver.Value) []driver.Value {
	return []driver.Value{id, jobID, buildID, nodeID, attempt, nil, status, digest, storageKey, size, createdAt, publishedAt, deletedAt}
}
