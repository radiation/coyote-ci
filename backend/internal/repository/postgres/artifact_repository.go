package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type ArtifactRepository struct {
	db *sql.DB
}

func NewArtifactRepository(db *sql.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

const artifactColumns = `id, build_id, step_id, logical_path, artifact_type, storage_key, storage_provider, size_bytes, content_type, checksum_sha256, created_at`

func (r *ArtifactRepository) Create(ctx context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	const query = `
		INSERT INTO build_artifacts (
			id,
			build_id,
			step_id,
			logical_path,
			artifact_type,
			storage_key,
			storage_provider,
			size_bytes,
			content_type,
			checksum_sha256,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, NOW()))
		RETURNING ` + artifactColumns

	var createdAt any
	if !artifact.CreatedAt.IsZero() {
		createdAt = artifact.CreatedAt
	}

	provider := string(artifact.StorageProvider)
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}

	var artifactType any
	if artifact.ArtifactType != "" {
		artifactType = string(artifact.ArtifactType)
	}

	return scanArtifact(r.db.QueryRowContext(
		ctx,
		query,
		artifact.ID,
		artifact.BuildID,
		artifact.StepID,
		artifact.LogicalPath,
		artifactType,
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

func (r *ArtifactRepository) ListForBrowse(ctx context.Context, query string) ([]domain.ArtifactBrowseRecord, error) {
	selectColumns := `
		` + qualifyColumns("a", artifactColumns) + `,
		` + qualifyColumns("b", buildListColumns) + `,
		s.id,
		s.step_index,
		s.name
	`
	browseQuery := `
		SELECT ` + selectColumns + `
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		LEFT JOIN build_steps s ON s.id = a.step_id
		WHERE (
			$1 = ''
			OR a.logical_path ILIKE $2
			OR b.project_id ILIKE $2
			OR COALESCE(b.job_id::text, '') ILIKE $2
			OR EXISTS (
				SELECT 1
				FROM version_tags vt
				WHERE vt.artifact_id = a.id
				  AND vt.version_text ILIKE $2
			)
		)
		ORDER BY a.created_at DESC, a.logical_path ASC, b.created_at DESC
	`

	trimmedQuery := strings.TrimSpace(query)
	likeQuery := "%" + trimmedQuery + "%"
	rows, err := r.db.QueryContext(ctx, browseQuery, trimmedQuery, likeQuery)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	records := make([]domain.ArtifactBrowseRecord, 0)
	for rows.Next() {
		record, scanErr := scanArtifactBrowseRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
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
	var artifactType sql.NullString
	var storageProvider string
	var contentType sql.NullString
	var checksum sql.NullString

	err := scanner.Scan(
		&artifact.ID,
		&artifact.BuildID,
		&stepID,
		&artifact.LogicalPath,
		&artifactType,
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
	if artifactType.Valid {
		artifact.ArtifactType = domain.ArtifactType(artifactType.String)
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

func scanArtifactBrowseRecord(scanner rowScanner) (domain.ArtifactBrowseRecord, error) {
	var record domain.ArtifactBrowseRecord
	var artifactStepID sql.NullString
	var artifactType sql.NullString
	var artifactStorageProvider string
	var artifactContentType sql.NullString
	var artifactChecksum sql.NullString
	var buildNulls buildNullFields
	var stepID sql.NullString
	var stepIndex sql.NullInt64
	var stepName sql.NullString

	err := scanner.Scan(
		&record.Artifact.ID,
		&record.Artifact.BuildID,
		&artifactStepID,
		&record.Artifact.LogicalPath,
		&artifactType,
		&record.Artifact.StorageKey,
		&artifactStorageProvider,
		&record.Artifact.SizeBytes,
		&artifactContentType,
		&artifactChecksum,
		&record.Artifact.CreatedAt,
		&record.Build.ID,
		&record.Build.BuildNumber,
		&record.Build.ProjectID,
		&buildNulls.jobID,
		&buildNulls.status,
		&record.Build.CreatedAt,
		&buildNulls.queuedAt,
		&buildNulls.startedAt,
		&buildNulls.finishedAt,
		&record.Build.CurrentStepIndex,
		&record.Build.AttemptNumber,
		&buildNulls.rerunOfBuildID,
		&buildNulls.rerunFromStepIdx,
		&buildNulls.errorMessage,
		&buildNulls.pipelineName,
		&buildNulls.pipelineSource,
		&buildNulls.pipelinePath,
		&buildNulls.repoURL,
		&buildNulls.ref,
		&buildNulls.commitSHA,
		&buildNulls.triggerKind,
		&buildNulls.scmProvider,
		&buildNulls.eventType,
		&buildNulls.triggerRepositoryOwner,
		&buildNulls.triggerRepositoryName,
		&buildNulls.triggerRepositoryURL,
		&buildNulls.triggerRawRef,
		&buildNulls.triggerRef,
		&buildNulls.triggerRefType,
		&buildNulls.triggerRefName,
		&buildNulls.triggerDeleted,
		&buildNulls.triggerCommitSHA,
		&buildNulls.triggerDeliveryID,
		&buildNulls.triggerActor,
		&buildNulls.requestedImageRef,
		&buildNulls.resolvedImageRef,
		&buildNulls.imageSourceKind,
		&buildNulls.managedImageID,
		&buildNulls.managedImageVersionID,
		&stepID,
		&stepIndex,
		&stepName,
	)
	if err != nil {
		return domain.ArtifactBrowseRecord{}, err
	}

	if artifactStepID.Valid {
		v := artifactStepID.String
		record.Artifact.StepID = &v
	}
	if artifactType.Valid {
		record.Artifact.ArtifactType = domain.ArtifactType(artifactType.String)
	}
	record.Artifact.StorageProvider = domain.StorageProvider(artifactStorageProvider)
	if artifactContentType.Valid {
		v := artifactContentType.String
		record.Artifact.ContentType = &v
	}
	if artifactChecksum.Valid {
		v := artifactChecksum.String
		record.Artifact.ChecksumSHA256 = &v
	}
	buildNulls.applyTo(&record.Build)
	if stepID.Valid {
		record.Step = &domain.BuildStep{
			ID:        stepID.String,
			BuildID:   record.Build.ID,
			StepIndex: int(stepIndex.Int64),
			Name:      stepName.String,
		}
	}

	return record, nil
}
