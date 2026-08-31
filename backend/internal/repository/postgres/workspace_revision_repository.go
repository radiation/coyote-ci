package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const workspaceRevisionColumns = `id, producing_execution_job_id, build_id, node_id, attempt_number, parent_revision_id, status, content_digest, storage_key, size_bytes, created_at, published_at, deleted_at`

type WorkspaceRevisionRepository struct {
	db *sql.DB
}

func NewWorkspaceRevisionRepository(db *sql.DB) *WorkspaceRevisionRepository {
	return &WorkspaceRevisionRepository{db: db}
}

func (r *WorkspaceRevisionRepository) CreatePublishing(ctx context.Context, revision domain.WorkspaceRevision) (domain.WorkspaceRevision, error) {
	if err := revision.ValidateForCreate(); err != nil {
		return domain.WorkspaceRevision{}, err
	}
	var jobBuildID string
	var jobNodeID string
	var jobAttemptNumber int
	jobErr := r.db.QueryRowContext(ctx, `SELECT build_id, node_id, attempt_number FROM build_jobs WHERE id = $1`, revision.ProducingExecutionJobID).Scan(&jobBuildID, &jobNodeID, &jobAttemptNumber)
	if jobErr != nil {
		if errors.Is(jobErr, sql.ErrNoRows) {
			return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
		}
		return domain.WorkspaceRevision{}, jobErr
	}
	if jobBuildID != revision.BuildID || jobNodeID != revision.NodeID || jobAttemptNumber != revision.AttemptNumber {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}

	query := fmt.Sprintf(`
		INSERT INTO workspace_revisions (
			id, producing_execution_job_id, build_id, node_id, attempt_number, parent_revision_id, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (producing_execution_job_id) DO NOTHING
		RETURNING %s`, workspaceRevisionColumns)
	created, err := scanWorkspaceRevision(r.db.QueryRowContext(ctx, query, revision.ID, revision.ProducingExecutionJobID, revision.BuildID, revision.NodeID, revision.AttemptNumber, revision.ParentRevisionID, revision.Status, revision.CreatedAt.UTC()))
	if err == nil {
		return created, nil
	}
	if isWorkspaceRevisionUniqueViolation(err) {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRevision{}, err
	}

	existing, getErr := r.GetByProducingExecutionJob(ctx, revision.ProducingExecutionJobID)
	if getErr != nil {
		return domain.WorkspaceRevision{}, getErr
	}
	if existing.ID == revision.ID && sameWorkspaceRevisionCreate(existing, revision) {
		return existing, nil
	}
	return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
}

func (r *WorkspaceRevisionRepository) MarkPublishedIfClaimed(ctx context.Context, revisionID string, claimToken string, publication domain.WorkspaceRevisionPublication, publishedAt time.Time) (domain.WorkspaceRevision, error) {
	if err := publication.Validate(); err != nil || strings.TrimSpace(revisionID) == "" || strings.TrimSpace(claimToken) == "" || publishedAt.IsZero() {
		return domain.WorkspaceRevision{}, domain.ErrInvalidWorkspaceRevision
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WorkspaceRevision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	revisionQuery := fmt.Sprintf("SELECT %s FROM workspace_revisions WHERE id = $1 FOR UPDATE", workspaceRevisionColumns)
	revision, scanErr := scanWorkspaceRevision(tx.QueryRowContext(ctx, revisionQuery, revisionID))
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
		}
		return domain.WorkspaceRevision{}, scanErr
	}
	if revision.Status == domain.WorkspaceRevisionStatusDeleted {
		return revision, repository.ErrWorkspaceRevisionConflict
	}
	if revision.Status == domain.WorkspaceRevisionStatusPublished {
		if sameWorkspaceRevisionPublication(revision, publication) {
			if err = tx.Commit(); err != nil {
				return domain.WorkspaceRevision{}, err
			}
			committed = true
			return revision, nil
		}
		return revision, repository.ErrWorkspaceRevisionConflict
	}

	var owned bool
	ownershipErr := tx.QueryRowContext(ctx, `
		SELECT status = 'running' AND claim_token = $2 AND claim_expires_at > NOW()
		FROM build_jobs
		WHERE id = $1
		FOR UPDATE
	`, revision.ProducingExecutionJobID, claimToken).Scan(&owned)
	if ownershipErr != nil {
		if errors.Is(ownershipErr, sql.ErrNoRows) {
			return revision, repository.ErrWorkspaceRevisionStaleClaim
		}
		return domain.WorkspaceRevision{}, ownershipErr
	}
	if !owned {
		return revision, repository.ErrWorkspaceRevisionStaleClaim
	}

	publishQuery := fmt.Sprintf(`
		UPDATE workspace_revisions
		SET status = 'published', content_digest = $2, storage_key = $3, size_bytes = $4, published_at = $5
		WHERE id = $1 AND status = 'publishing'
		RETURNING %s`, workspaceRevisionColumns)
	updated, updateErr := scanWorkspaceRevision(tx.QueryRowContext(ctx, publishQuery,
		revisionID, publication.ContentDigest, publication.StorageKey, publication.SizeBytes, publishedAt.UTC()))
	if updateErr != nil {
		return domain.WorkspaceRevision{}, updateErr
	}
	if err = tx.Commit(); err != nil {
		return domain.WorkspaceRevision{}, err
	}
	committed = true
	return updated, nil
}

func (r *WorkspaceRevisionRepository) GetByProducingExecutionJob(ctx context.Context, executionJobID string) (domain.WorkspaceRevision, error) {
	revision, err := scanWorkspaceRevision(r.db.QueryRowContext(ctx, `SELECT `+workspaceRevisionColumns+` FROM workspace_revisions WHERE producing_execution_job_id = $1`, executionJobID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	return revision, err
}

func (r *WorkspaceRevisionRepository) getByID(ctx context.Context, revisionID string) (domain.WorkspaceRevision, error) {
	revision, err := scanWorkspaceRevision(r.db.QueryRowContext(ctx, `SELECT `+workspaceRevisionColumns+` FROM workspace_revisions WHERE id = $1`, revisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	return revision, err
}

func (r *WorkspaceRevisionRepository) GetPublishedByBuildNode(ctx context.Context, buildID string, nodeID string) (domain.WorkspaceRevision, error) {
	publishedLookupQuery := fmt.Sprintf(`
		SELECT wr.%s
		FROM workspace_revisions AS wr
		INNER JOIN build_jobs AS bj ON bj.id = wr.producing_execution_job_id
		WHERE wr.build_id = $1 AND wr.node_id = $2 AND wr.status = 'published' AND bj.status = 'success'
		ORDER BY bj.attempt_number DESC, bj.created_at DESC, wr.created_at DESC
		LIMIT 1`, strings.ReplaceAll(workspaceRevisionColumns, ", ", ", wr."))
	revision, err := scanWorkspaceRevision(r.db.QueryRowContext(ctx, publishedLookupQuery, buildID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionNotFound
	}
	return revision, err
}

func (r *WorkspaceRevisionRepository) MarkDeleted(ctx context.Context, revisionID string, deletedAt time.Time) (domain.WorkspaceRevision, error) {
	if strings.TrimSpace(revisionID) == "" || deletedAt.IsZero() {
		return domain.WorkspaceRevision{}, domain.ErrInvalidWorkspaceRevision
	}
	deleteQuery := fmt.Sprintf(`
		UPDATE workspace_revisions
		SET status = 'deleted', deleted_at = $2
		WHERE id = $1 AND status = 'published'
		RETURNING %s`, workspaceRevisionColumns)
	updated, err := scanWorkspaceRevision(r.db.QueryRowContext(ctx, deleteQuery, revisionID, deletedAt.UTC()))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRevision{}, err
	}
	existing, getErr := r.getByID(ctx, revisionID)
	if getErr == nil && existing.Status == domain.WorkspaceRevisionStatusDeleted {
		return existing, nil
	}
	if errors.Is(getErr, repository.ErrWorkspaceRevisionNotFound) {
		return domain.WorkspaceRevision{}, getErr
	}
	if getErr != nil {
		return domain.WorkspaceRevision{}, getErr
	}
	return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionConflict
}

func scanWorkspaceRevision(scanner rowScanner) (domain.WorkspaceRevision, error) {
	var revision domain.WorkspaceRevision
	var parentRevisionID sql.NullString
	var contentDigest sql.NullString
	var storageKey sql.NullString
	var sizeBytes sql.NullInt64
	var publishedAt sql.NullTime
	var deletedAt sql.NullTime
	var status string
	err := scanner.Scan(&revision.ID, &revision.ProducingExecutionJobID, &revision.BuildID, &revision.NodeID, &revision.AttemptNumber, &parentRevisionID, &status, &contentDigest, &storageKey, &sizeBytes, &revision.CreatedAt, &publishedAt, &deletedAt)
	if err != nil {
		return domain.WorkspaceRevision{}, err
	}
	revision.Status = domain.WorkspaceRevisionStatus(status)
	if parentRevisionID.Valid {
		revision.ParentRevisionID = &parentRevisionID.String
	}
	if contentDigest.Valid {
		revision.ContentDigest = &contentDigest.String
	}
	if storageKey.Valid {
		revision.StorageKey = &storageKey.String
	}
	if sizeBytes.Valid {
		value := sizeBytes.Int64
		revision.SizeBytes = &value
	}
	if publishedAt.Valid {
		value := publishedAt.Time
		revision.PublishedAt = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time
		revision.DeletedAt = &value
	}
	return revision, nil
}

func sameWorkspaceRevisionCreate(left domain.WorkspaceRevision, right domain.WorkspaceRevision) bool {
	return left.ID == right.ID && left.ProducingExecutionJobID == right.ProducingExecutionJobID && left.BuildID == right.BuildID && left.NodeID == right.NodeID && left.AttemptNumber == right.AttemptNumber && nullableStringEqual(left.ParentRevisionID, right.ParentRevisionID) && left.Status == right.Status
}

func isWorkspaceRevisionUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func sameWorkspaceRevisionPublication(revision domain.WorkspaceRevision, publication domain.WorkspaceRevisionPublication) bool {
	return nullableStringEqual(revision.ContentDigest, &publication.ContentDigest) && nullableStringEqual(revision.StorageKey, &publication.StorageKey) && nullableInt64Equal(revision.SizeBytes, publication.SizeBytes)
}

func nullableStringEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func nullableInt64Equal(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
