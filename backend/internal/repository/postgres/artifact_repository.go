package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ArtifactRepository struct {
	db *sql.DB
}

func NewArtifactRepository(db *sql.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

const artifactColumns = `id, build_id, step_id, logical_path, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at`

func (r *ArtifactRepository) Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	const query = `
		INSERT INTO build_artifacts (
			id,
			build_id,
			step_id,
			logical_path,
			storage_key,
			storage_provider,
			size_bytes,
			content_type,
			checksum_sha256,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10, NOW()))
		RETURNING ` + artifactColumns

	var createdAt any
	if !artifact.CreatedAt.IsZero() {
		createdAt = artifact.CreatedAt
	}

	provider := string(artifact.StorageProvider)
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}

	return scanArtifact(r.db.QueryRowContext(
		ctx,
		query,
		artifact.ID,
		artifact.BuildID,
		artifact.StepID,
		artifact.LogicalPath,
		artifact.StorageKey,
		provider,
		artifact.SizeBytes,
		artifact.ContentType,
		artifact.ChecksumSHA256,
		createdAt,
	))
}

func (r *ArtifactRepository) ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE build_id = $1
		ORDER BY created_at ASC, logical_path ASC
	`

	return scanArtifactRows(r.db.QueryContext(ctx, query, buildID))
}

func (r *ArtifactRepository) ListByStepID(ctx context.Context, stepID string) ([]domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE step_id = $1
		ORDER BY created_at ASC, logical_path ASC
	`

	return scanArtifactRows(r.db.QueryContext(ctx, query, stepID))
}

func (r *ArtifactRepository) GetByID(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	const query = `
		SELECT ` + artifactColumns + `
		FROM build_artifacts
		WHERE build_id = $1 AND id = $2
	`

	artifact, err := scanArtifact(r.db.QueryRowContext(ctx, query, buildID, artifactID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.BuildArtifact{}, repository.ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, err
	}

	return artifact, nil
}

func scanArtifactRows(rows *sql.Rows, queryErr error) ([]domain.BuildArtifact, error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() {
		_ = rows.Close()
	}()

	artifacts := make([]domain.BuildArtifact, 0)
	for rows.Next() {
		artifact, scanErr := scanArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return artifacts, nil
}

func scanArtifact(scanner rowScanner) (domain.BuildArtifact, error) {
	var artifact domain.BuildArtifact
	var stepID sql.NullString
	var storageProvider string
	var contentType sql.NullString
	var checksum sql.NullString

	err := scanner.Scan(
		&artifact.ID,
		&artifact.BuildID,
		&stepID,
		&artifact.LogicalPath,
		&artifact.StorageKey,
		&storageProvider,
		&artifact.SizeBytes,
		&contentType,
		&checksum,
		&artifact.CreatedAt,
	)
	if err != nil {
		return domain.BuildArtifact{}, err
	}

	if stepID.Valid {
		v := stepID.String
		artifact.StepID = &v
	}
	artifact.StorageProvider = domain.StorageProvider(storageProvider)
	if contentType.Valid {
		v := contentType.String
		artifact.ContentType = &v
	}
	if checksum.Valid {
		v := checksum.String
		artifact.ChecksumSHA256 = &v
	}

	return artifact, nil
}
