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

const cacheEntryColumns = `id, job_id, preset, cache_key, storage_provider, object_key, size_bytes, checksum, compression, status, created_by_build_id, created_by_step_id, created_at, updated_at, last_accessed_at`

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
			compression,
			status,
			created_by_build_id,
			created_by_step_id,
			created_at,
			updated_at,
			last_accessed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW(), NULL)
		ON CONFLICT (job_id, preset, cache_key)
		DO UPDATE SET
			storage_provider = EXCLUDED.storage_provider,
			object_key = EXCLUDED.object_key,
			size_bytes = EXCLUDED.size_bytes,
			checksum = EXCLUDED.checksum,
			compression = EXCLUDED.compression,
			status = EXCLUDED.status,
			created_by_build_id = EXCLUDED.created_by_build_id,
			created_by_step_id = EXCLUDED.created_by_step_id,
			updated_at = NOW()
		RETURNING ` + cacheEntryColumns

	id := uuid.NewString()
	return scanCacheEntry(r.db.QueryRowContext(
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
