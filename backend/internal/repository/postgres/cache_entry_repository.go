package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type CacheEntryRepository struct {
	db *sql.DB
}

func NewCacheEntryRepository(db *sql.DB) *CacheEntryRepository {
	return &CacheEntryRepository{db: db}
}

const cacheEntryColumns = `id, job_id, preset, cache_key, storage_provider, object_key, size_bytes, checksum, content_digest, compression, status, created_by_build_id, created_by_step_id, created_at, updated_at, last_accessed_at`

func (r *CacheEntryRepository) FindReadyByKey(ctx context.Context, jobID string, preset string, cacheKey string) (domain.CacheEntry, bool, error) {
	const query = `
		SELECT ` + cacheEntryColumns + `
		FROM cache_entries
		WHERE job_id = $1
		  AND preset = $2
		  AND cache_key = $3
		  AND status = 'ready'
	`

	entry, err := scanCacheEntry(r.db.QueryRowContext(
		ctx,
		query,
		strings.TrimSpace(jobID),
		strings.TrimSpace(strings.ToLower(preset)),
		strings.TrimSpace(cacheKey),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CacheEntry{}, false, nil
		}
		return domain.CacheEntry{}, false, err
	}
	return entry, true, nil
}

func (r *CacheEntryRepository) Upsert(ctx context.Context, input repository.CacheEntryUpsertInput) (domain.CacheEntry, error) {
	return upsertCacheEntry(ctx, r.db, input)
}

type cacheEntryQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func upsertCacheEntry(ctx context.Context, queryer cacheEntryQueryer, input repository.CacheEntryUpsertInput) (domain.CacheEntry, error) {
	const query = `
		INSERT INTO cache_entries (
			id,
			job_id,
			preset,
			cache_key,
			storage_provider,
			object_key,
			size_bytes,
			checksum,
			content_digest,
			compression,
			status,
			created_by_build_id,
			created_by_step_id,
			created_at,
			updated_at,
			last_accessed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW(), NULL)
		ON CONFLICT (job_id, preset, cache_key)
		DO UPDATE SET
			storage_provider = EXCLUDED.storage_provider,
			object_key = EXCLUDED.object_key,
			size_bytes = EXCLUDED.size_bytes,
			checksum = EXCLUDED.checksum,
			content_digest = EXCLUDED.content_digest,
			compression = EXCLUDED.compression,
			status = EXCLUDED.status,
			created_by_build_id = EXCLUDED.created_by_build_id,
			created_by_step_id = EXCLUDED.created_by_step_id,
			updated_at = NOW()
		RETURNING ` + cacheEntryColumns

	id := uuid.NewString()
	return scanCacheEntry(queryer.QueryRowContext(
		ctx,
		query,
		id,
		strings.TrimSpace(input.JobID),
		strings.TrimSpace(strings.ToLower(input.Preset)),
		strings.TrimSpace(input.CacheKey),
		string(input.StorageProvider),
		strings.TrimSpace(input.ObjectKey),
		input.SizeBytes,
		strings.TrimSpace(input.Checksum),
		strings.TrimSpace(input.ContentDigest),
		strings.TrimSpace(input.Compression),
		string(input.Status),
		strings.TrimSpace(input.CreatedByBuildID),
		strings.TrimSpace(input.CreatedByStepID),
	))
}

func (r *CacheEntryRepository) MarkAccessed(ctx context.Context, id string, accessedAt time.Time) error {
	const query = `
		UPDATE cache_entries
		SET last_accessed_at = $2,
			updated_at = NOW()
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, strings.TrimSpace(id), accessedAt.UTC())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrCacheEntryNotFound
	}
	return nil
}

func (r *CacheEntryRepository) TryAcquirePublishClaim(ctx context.Context, jobID, preset, cacheKey, claimant string, now time.Time, lease time.Duration) (domain.CachePublishClaim, bool, error) {
	claim := domain.CachePublishClaim{JobID: strings.TrimSpace(jobID), Preset: strings.TrimSpace(strings.ToLower(preset)), CacheKey: strings.TrimSpace(cacheKey), ClaimToken: uuid.NewString(), ClaimedBy: strings.TrimSpace(claimant), ClaimedAt: now.UTC(), ExpiresAt: now.UTC().Add(lease)}
	const query = `
		INSERT INTO cache_publish_claims (job_id, preset, cache_key, claim_token, claimed_by, claimed_at, claim_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (job_id, preset, cache_key) DO UPDATE SET
			claim_token = EXCLUDED.claim_token, claimed_by = EXCLUDED.claimed_by, claimed_at = EXCLUDED.claimed_at,
			claim_expires_at = EXCLUDED.claim_expires_at, updated_at = NOW()
		WHERE cache_publish_claims.claim_expires_at <= $6
		RETURNING claim_token, (xmax <> 0) AS reclaimed`
	var token string
	var reclaimed bool
	err := r.db.QueryRowContext(ctx, query, claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken, claim.ClaimedBy, claim.ClaimedAt, claim.ExpiresAt).Scan(&token, &reclaimed)
	if errors.Is(err, sql.ErrNoRows) {
		return claim, false, nil
	}
	if err != nil {
		return domain.CachePublishClaim{}, false, err
	}
	claim.Reclaimed = reclaimed
	return claim, true, nil
}

func (r *CacheEntryRepository) ReleasePublishClaim(ctx context.Context, claim domain.CachePublishClaim) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM cache_publish_claims WHERE job_id = $1 AND preset = $2 AND cache_key = $3 AND claim_token = $4`, claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken)
	return err
}

func (r *CacheEntryRepository) ValidatePublishClaim(ctx context.Context, claim domain.CachePublishClaim, claimant string, now time.Time) error {
	var valid bool
	err := r.db.QueryRowContext(ctx, `
		SELECT TRUE
		FROM cache_publish_claims
		WHERE job_id = $1 AND preset = $2 AND cache_key = $3
			AND claim_token = $4 AND claimed_by = $5 AND claim_expires_at > $6
	`, strings.TrimSpace(claim.JobID), strings.TrimSpace(strings.ToLower(claim.Preset)), strings.TrimSpace(claim.CacheKey), strings.TrimSpace(claim.ClaimToken), strings.TrimSpace(claimant), now.UTC()).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrCachePublishClaimStale
	}
	return err
}

func (r *CacheEntryRepository) CompletePublishClaim(ctx context.Context, claim domain.CachePublishClaim, input repository.CacheEntryUpsertInput, now time.Time) (domain.CacheEntry, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CacheEntry{}, err
	}
	defer func() { _ = tx.Rollback() }()
	err = tx.QueryRowContext(ctx, `SELECT claim_expires_at FROM cache_publish_claims WHERE job_id = $1 AND preset = $2 AND cache_key = $3 AND claim_token = $4 AND claim_expires_at > $5 FOR UPDATE`, claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken, now.UTC()).Scan(new(time.Time))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CacheEntry{}, repository.ErrCachePublishClaimStale
	}
	if err != nil {
		return domain.CacheEntry{}, err
	}
	entry, err := upsertCacheEntry(ctx, tx, input)
	if err != nil {
		return domain.CacheEntry{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM cache_publish_claims WHERE job_id = $1 AND preset = $2 AND cache_key = $3 AND claim_token = $4`, claim.JobID, claim.Preset, claim.CacheKey, claim.ClaimToken); err != nil {
		return domain.CacheEntry{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.CacheEntry{}, err
	}
	return entry, nil
}

func scanCacheEntry(scanner rowScanner) (domain.CacheEntry, error) {
	var entry domain.CacheEntry
	var status string
	var provider string
	var lastAccessedAt sql.NullTime
	err := scanner.Scan(
		&entry.ID,
		&entry.JobID,
		&entry.Preset,
		&entry.CacheKey,
		&provider,
		&entry.ObjectKey,
		&entry.SizeBytes,
		&entry.Checksum,
		&entry.ContentDigest,
		&entry.Compression,
		&status,
		&entry.CreatedByBuildID,
		&entry.CreatedByStepID,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&lastAccessedAt,
	)
	if err != nil {
		return domain.CacheEntry{}, err
	}
	entry.StorageProvider = domain.StorageProvider(provider)
	entry.Status = domain.CacheEntryStatus(status)
	if lastAccessedAt.Valid {
		at := lastAccessedAt.Time
		entry.LastAccessedAt = &at
	}
	return entry, nil
}
