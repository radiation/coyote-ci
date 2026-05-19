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

type ArtifactLabelRepository struct {
	db *sql.DB
}

func NewArtifactLabelRepository(db *sql.DB) *ArtifactLabelRepository {
	return &ArtifactLabelRepository{db: db}
}

const artifactLabelSelectColumns = `id, job_id, label_value, label_kind, artifact_id, created_at`

func (r *ArtifactLabelRepository) ListByArtifactID(ctx context.Context, artifactID string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + artifactLabelSelectColumns + `
		FROM (
			SELECT
				av.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				av.version_text AS label_value,
				'` + string(domain.VersionTagKindVersion) + `' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE av.artifact_id = $1

			UNION ALL

			SELECT
				ac.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				ac.channel_name AS label_value,
				'` + string(domain.VersionTagKindChannel) + `' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ac.current_artifact_id = $1
		) labels
		ORDER BY created_at ASC, id ASC
	`
	return scanArtifactLabelRows(r.db.QueryContext(ctx, query, strings.TrimSpace(artifactID)))
}

func (r *ArtifactLabelRepository) ListByArtifactIDs(ctx context.Context, artifactIDs []string) ([]domain.VersionTag, error) {
	trimmed := uniqueTrimmedStrings(artifactIDs)
	if len(trimmed) == 0 {
		return []domain.VersionTag{}, nil
	}
	versionQuery, versionArgs := stringListQuery(`
			SELECT
				av.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				av.version_text AS label_value,
				'`+string(domain.VersionTagKindVersion)+`' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE av.artifact_id IN (%s)
	`, 1, trimmed)
	channelQuery, channelArgs := stringListQuery(`
			SELECT
				ac.id,
				COALESCE(ap.job_id::text, '') AS job_id,
				ac.channel_name AS label_value,
				'`+string(domain.VersionTagKindChannel)+`' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ac.current_artifact_id IN (%s)
	`, 1+len(versionArgs), trimmed)
	query := `
		SELECT ` + artifactLabelSelectColumns + `
		FROM (
	` + versionQuery + `
			UNION ALL
	` + channelQuery + `
		) labels
		ORDER BY created_at ASC, id ASC
	`
	args := append(versionArgs, channelArgs...)
	return scanArtifactLabelRows(r.db.QueryContext(ctx, query, args...))
}

func (r *ArtifactLabelRepository) ListByJobID(ctx context.Context, jobID string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + artifactLabelSelectColumns + `
		FROM (
			SELECT
				av.id,
				ap.job_id::text AS job_id,
				av.version_text AS label_value,
				'` + string(domain.VersionTagKindVersion) + `' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE ap.job_id = $1

			UNION ALL

			SELECT
				ac.id,
				ap.job_id::text AS job_id,
				ac.channel_name AS label_value,
				'` + string(domain.VersionTagKindChannel) + `' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ap.job_id = $1
		) labels
		ORDER BY created_at ASC, id ASC
	`
	return scanArtifactLabelRows(r.db.QueryContext(ctx, query, strings.TrimSpace(jobID)))
}

func (r *ArtifactLabelRepository) ListByJobIDAndValue(ctx context.Context, jobID string, value string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + artifactLabelSelectColumns + `
		FROM (
			SELECT
				av.id,
				ap.job_id::text AS job_id,
				av.version_text AS label_value,
				'` + string(domain.VersionTagKindVersion) + `' AS label_kind,
				av.artifact_id,
				av.created_at
			FROM artifact_versions av
			JOIN artifact_packages ap ON ap.id = av.package_id
			WHERE ap.job_id = $1 AND av.version_text = $2

			UNION ALL

			SELECT
				ac.id,
				ap.job_id::text AS job_id,
				ac.channel_name AS label_value,
				'` + string(domain.VersionTagKindChannel) + `' AS label_kind,
				ac.current_artifact_id AS artifact_id,
				ac.updated_at AS created_at
			FROM artifact_channels ac
			JOIN artifact_packages ap ON ap.id = ac.package_id
			WHERE ap.job_id = $1 AND ac.channel_name = $2
		) labels
		ORDER BY created_at ASC, id ASC
	`
	return scanArtifactLabelRows(r.db.QueryContext(ctx, query, strings.TrimSpace(jobID), strings.TrimSpace(value)))
}

func (r *ArtifactLabelRepository) ListReleaseVersionsByJobID(ctx context.Context, jobID string) ([]string, error) {
	const query = `
		SELECT DISTINCT av.version_text
		FROM artifact_versions av
		JOIN artifact_packages ap ON ap.id = av.package_id
		WHERE ap.job_id = $1
		ORDER BY av.version_text ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ArtifactLabelRepository) CreateForArtifacts(ctx context.Context, params repository.CreateArtifactLabelsParams) ([]domain.VersionTag, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	targets, err := r.resolveTargets(ctx, tx, strings.TrimSpace(params.JobID), params.ArtifactIDs)
	if err != nil {
		return nil, err
	}
	created := make([]domain.VersionTag, 0, len(targets))
	for _, target := range targets {
		switch params.Kind {
		case domain.VersionTagKindVersion:
			tag, createErr := r.insertArtifactVersion(ctx, tx, strings.TrimSpace(params.JobID), target, strings.TrimSpace(params.Value))
			if createErr != nil {
				return nil, createErr
			}
			created = append(created, tag)
		case domain.VersionTagKindChannel:
			tag, createErr := r.upsertArtifactChannel(ctx, tx, strings.TrimSpace(params.JobID), target, strings.TrimSpace(params.Value))
			if createErr != nil {
				return nil, createErr
			}
			created = append(created, tag)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

type artifactLabelTarget struct {
	PackageID  string
	ArtifactID string
	CreatedAt  time.Time
}

func (r *ArtifactLabelRepository) resolveTargets(ctx context.Context, tx *sql.Tx, jobID string, artifactIDs []string) ([]artifactLabelTarget, error) {
	trimmed := uniqueTrimmedStrings(artifactIDs)
	if len(trimmed) == 0 {
		return []artifactLabelTarget{}, nil
	}
	query, args := stringListQuery(`
		SELECT a.id, a.package_id, a.created_at, b.job_id
		FROM build_artifacts a
		JOIN builds b ON b.id = a.build_id
		WHERE a.id IN (%s)
		ORDER BY a.package_id ASC, a.created_at DESC, a.id DESC
	`, 1, trimmed)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	found := map[string]struct{}{}
	selectedByPackage := map[string]artifactLabelTarget{}
	jobMatch := map[string]bool{}
	for rows.Next() {
		var artifactID string
		var packageID string
		var createdAt time.Time
		var targetJobID sql.NullString
		if err := rows.Scan(&artifactID, &packageID, &createdAt, &targetJobID); err != nil {
			return nil, err
		}
		found[artifactID] = struct{}{}
		jobMatch[artifactID] = targetJobID.Valid && strings.TrimSpace(targetJobID.String) == jobID
		if !jobMatch[artifactID] {
			continue
		}
		if _, ok := selectedByPackage[packageID]; !ok {
			selectedByPackage[packageID] = artifactLabelTarget{PackageID: packageID, ArtifactID: artifactID, CreatedAt: createdAt}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, artifactID := range trimmed {
		if _, ok := found[artifactID]; !ok {
			return nil, repository.ErrVersionTagTargetNotFound
		}
		if !jobMatch[artifactID] {
			return nil, repository.ErrVersionTagTargetJobMismatch
		}
	}
	selected := make([]artifactLabelTarget, 0, len(selectedByPackage))
	for _, target := range selectedByPackage {
		selected = append(selected, target)
	}
	return selected, nil
}

func (r *ArtifactLabelRepository) insertArtifactVersion(ctx context.Context, tx *sql.Tx, jobID string, target artifactLabelTarget, value string) (domain.VersionTag, error) {
	const query = `
		INSERT INTO artifact_versions (id, package_id, artifact_id, version_text, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id, artifact_id, created_at
	`
	var createdID string
	var artifactID string
	var createdAt time.Time
	err := tx.QueryRowContext(ctx, query, uuid.NewString(), target.PackageID, target.ArtifactID, value).Scan(&createdID, &artifactID, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			existingID, existingArtifactID, existingCreatedAt, loadErr := r.loadArtifactVersion(ctx, tx, target.PackageID, value)
			if loadErr != nil {
				return domain.VersionTag{}, loadErr
			}
			if existingArtifactID == target.ArtifactID {
				return toArtifactVersionTag(existingID, jobID, domain.VersionTagKindVersion, value, existingArtifactID, existingCreatedAt), nil
			}
			return domain.VersionTag{}, repository.ErrVersionTagConflict
		}
		return domain.VersionTag{}, err
	}
	return toArtifactVersionTag(createdID, jobID, domain.VersionTagKindVersion, value, artifactID, createdAt), nil
}

func (r *ArtifactLabelRepository) loadArtifactVersion(ctx context.Context, tx *sql.Tx, packageID string, value string) (string, string, time.Time, error) {
	const query = `
		SELECT id, artifact_id, created_at
		FROM artifact_versions
		WHERE package_id = $1 AND version_text = $2
	`
	var (
		id         string
		artifactID string
		createdAt  time.Time
	)
	if err := tx.QueryRowContext(ctx, query, packageID, value).Scan(&id, &artifactID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", time.Time{}, repository.ErrVersionTagConflict
		}
		return "", "", time.Time{}, err
	}
	return id, artifactID, createdAt, nil
}

func (r *ArtifactLabelRepository) upsertArtifactChannel(ctx context.Context, tx *sql.Tx, jobID string, target artifactLabelTarget, value string) (domain.VersionTag, error) {
	const selectQuery = `
		SELECT id, current_artifact_id, created_at, updated_at
		FROM artifact_channels
		WHERE package_id = $1 AND channel_name = $2
		FOR UPDATE
	`
	var channelID string
	var currentArtifactID string
	var createdAt time.Time
	var updatedAt time.Time
	err := tx.QueryRowContext(ctx, selectQuery, target.PackageID, value).Scan(&channelID, &currentArtifactID, &createdAt, &updatedAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.VersionTag{}, err
		}
		const insertQuery = `
			INSERT INTO artifact_channels (id, package_id, channel_name, current_artifact_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
			RETURNING id, current_artifact_id, updated_at
		`
		channelID = uuid.NewString()
		if err := tx.QueryRowContext(ctx, insertQuery, channelID, target.PackageID, value, target.ArtifactID).Scan(&channelID, &currentArtifactID, &updatedAt); err != nil {
			if isUniqueViolation(err) {
				return domain.VersionTag{}, repository.ErrVersionTagConflict
			}
			return domain.VersionTag{}, err
		}
		if insertErr := r.insertChannelEvent(ctx, tx, target.PackageID, value, nil, target.ArtifactID); insertErr != nil {
			return domain.VersionTag{}, insertErr
		}
		return toArtifactVersionTag(channelID, jobID, domain.VersionTagKindChannel, value, currentArtifactID, updatedAt), nil
	}
	if currentArtifactID == target.ArtifactID {
		return toArtifactVersionTag(channelID, jobID, domain.VersionTagKindChannel, value, currentArtifactID, updatedAt), nil
	}
	const updateQuery = `
		UPDATE artifact_channels
		SET current_artifact_id = $3,
		    updated_at = NOW()
		WHERE package_id = $1 AND channel_name = $2
		RETURNING id, current_artifact_id, updated_at
	`
	previousArtifactID := currentArtifactID
	if err := tx.QueryRowContext(ctx, updateQuery, target.PackageID, value, target.ArtifactID).Scan(&channelID, &currentArtifactID, &updatedAt); err != nil {
		return domain.VersionTag{}, err
	}
	if insertErr := r.insertChannelEvent(ctx, tx, target.PackageID, value, &previousArtifactID, target.ArtifactID); insertErr != nil {
		return domain.VersionTag{}, insertErr
	}
	return toArtifactVersionTag(channelID, jobID, domain.VersionTagKindChannel, value, currentArtifactID, updatedAt), nil
}

func (r *ArtifactLabelRepository) insertChannelEvent(ctx context.Context, tx *sql.Tx, packageID string, value string, previousArtifactID *string, newArtifactID string) error {
	const query = `
		INSERT INTO artifact_channel_events (id, package_id, channel_name, previous_artifact_id, new_artifact_id, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`
	_, err := tx.ExecContext(ctx, query, uuid.NewString(), packageID, value, previousArtifactID, newArtifactID)
	return err
}

func scanArtifactLabelRows(rows *sql.Rows, queryErr error) ([]domain.VersionTag, error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]domain.VersionTag, 0)
	for rows.Next() {
		tag, err := scanArtifactLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanArtifactLabel(scanner rowScanner) (domain.VersionTag, error) {
	var id string
	var jobID string
	var value string
	var kind string
	var artifactID string
	var createdAt time.Time
	if err := scanner.Scan(&id, &jobID, &value, &kind, &artifactID, &createdAt); err != nil {
		return domain.VersionTag{}, err
	}
	return toArtifactVersionTag(id, jobID, domain.VersionTagKind(kind), value, artifactID, createdAt), nil
}

func toArtifactVersionTag(id string, jobID string, kind domain.VersionTagKind, value string, artifactID string, createdAt time.Time) domain.VersionTag {
	artifactID = strings.TrimSpace(artifactID)
	return domain.VersionTag{
		ID:         id,
		JobID:      strings.TrimSpace(jobID),
		Kind:       kind,
		Version:    strings.TrimSpace(value),
		TargetType: domain.VersionTagTargetArtifact,
		ArtifactID: &artifactID,
		CreatedAt:  createdAt,
	}
}
