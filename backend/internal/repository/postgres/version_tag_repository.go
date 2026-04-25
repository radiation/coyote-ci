package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type VersionTagRepository struct {
	db *sql.DB
}

func NewVersionTagRepository(db *sql.DB) *VersionTagRepository {
	return &VersionTagRepository{db: db}
}

const versionTagColumns = `id, job_id, version_text, target_type, artifact_id, managed_image_version_id, created_at`

func (r *VersionTagRepository) ListByArtifactID(ctx context.Context, artifactID string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + versionTagColumns + `
		FROM version_tags
		WHERE artifact_id = $1
		ORDER BY created_at ASC, id ASC
	`
	return scanVersionTagRows(r.db.QueryContext(ctx, query, strings.TrimSpace(artifactID)))
}

func (r *VersionTagRepository) ListByArtifactIDs(ctx context.Context, artifactIDs []string) ([]domain.VersionTag, error) {
	trimmed := uniqueTrimmedStrings(artifactIDs)
	if len(trimmed) == 0 {
		return []domain.VersionTag{}, nil
	}
	query, args := stringListQuery(`
		SELECT `+versionTagColumns+`
		FROM version_tags
		WHERE artifact_id IN (%s)
		ORDER BY created_at ASC, id ASC
	`, 1, trimmed)
	return scanVersionTagRows(r.db.QueryContext(ctx, query, args...))
}

func (r *VersionTagRepository) ListByManagedImageVersionID(ctx context.Context, managedImageVersionID string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + versionTagColumns + `
		FROM version_tags
		WHERE managed_image_version_id = $1
		ORDER BY created_at ASC, id ASC
	`
	return scanVersionTagRows(r.db.QueryContext(ctx, query, strings.TrimSpace(managedImageVersionID)))
}

func (r *VersionTagRepository) ListByJobID(ctx context.Context, jobID string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + versionTagColumns + `
		FROM version_tags
		WHERE job_id = $1
		ORDER BY created_at ASC, id ASC
	`
	return scanVersionTagRows(r.db.QueryContext(ctx, query, strings.TrimSpace(jobID)))
}

func (r *VersionTagRepository) ListByJobIDAndVersion(ctx context.Context, jobID string, version string) ([]domain.VersionTag, error) {
	const query = `
		SELECT ` + versionTagColumns + `
		FROM version_tags
		WHERE job_id = $1 AND version_text = $2
		ORDER BY created_at ASC, id ASC
	`
	return scanVersionTagRows(r.db.QueryContext(ctx, query, strings.TrimSpace(jobID), strings.TrimSpace(version)))
}

func (r *VersionTagRepository) CreateForTargets(ctx context.Context, params repository.CreateVersionTagsParams) ([]domain.VersionTag, error) {
	jobID := strings.TrimSpace(params.JobID)
	version := strings.TrimSpace(params.Version)
	artifactIDs := uniqueTrimmedStrings(params.ArtifactIDs)
	managedImageVersionIDs := uniqueTrimmedStrings(params.ManagedImageVersionIDs)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := r.validateArtifactTargets(ctx, tx, jobID, artifactIDs); err != nil {
		return nil, err
	}
	if err := r.validateManagedImageVersionTargets(ctx, tx, jobID, managedImageVersionIDs); err != nil {
		return nil, err
	}
	if err := r.ensureNoDuplicateTargets(ctx, tx, jobID, version, artifactIDs, managedImageVersionIDs); err != nil {
		return nil, err
	}

	created := make([]domain.VersionTag, 0, len(artifactIDs)+len(managedImageVersionIDs))
	for _, artifactID := range artifactIDs {
		tag, createErr := r.insertVersionTag(ctx, tx, domain.VersionTag{
			ID:         uuid.NewString(),
			JobID:      jobID,
			Version:    version,
			TargetType: domain.VersionTagTargetArtifact,
			ArtifactID: versionTagStringPtr(artifactID),
		})
		if createErr != nil {
			return nil, createErr
		}
		created = append(created, tag)
	}
	for _, managedImageVersionID := range managedImageVersionIDs {
		tag, createErr := r.insertVersionTag(ctx, tx, domain.VersionTag{
			ID:                    uuid.NewString(),
			JobID:                 jobID,
			Version:               version,
			TargetType:            domain.VersionTagTargetManagedImageVersion,
			ManagedImageVersionID: versionTagStringPtr(managedImageVersionID),
		})
		if createErr != nil {
			return nil, createErr
		}
		created = append(created, tag)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *VersionTagRepository) validateArtifactTargets(ctx context.Context, tx *sql.Tx, jobID string, artifactIDs []string) error {
	if len(artifactIDs) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(artifactIDs))
	args := make([]any, 0, len(artifactIDs)+1)
	for index, artifactID := range artifactIDs {
		placeholders = append(placeholders, "$"+strconv.Itoa(index+1))
		args = append(args, artifactID)
	}
	query := fmt.Sprintf(`
		SELECT build_artifacts.id
		FROM build_artifacts
		JOIN builds ON builds.id = build_artifacts.build_id
		WHERE build_artifacts.id IN (%s) AND builds.job_id = $%d
	`, strings.Join(placeholders, ", "), len(args)+1)
	args = append(args, jobID)
	artifactRows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() {
		_ = artifactRows.Close()
	}()

	found := map[string]struct{}{}
	for artifactRows.Next() {
		var artifactID string
		if scanErr := artifactRows.Scan(&artifactID); scanErr != nil {
			return scanErr
		}
		found[artifactID] = struct{}{}
	}
	if rowsErr := artifactRows.Err(); rowsErr != nil {
		return rowsErr
	}
	if len(found) == len(artifactIDs) {
		return nil
	}

	existenceQuery, existenceArgs := stringListQuery(`SELECT id FROM build_artifacts WHERE id IN (%s)`, 1, artifactIDs)
	existingRows, err := tx.QueryContext(ctx, existenceQuery, existenceArgs...)
	if err != nil {
		return err
	}
	defer func() {
		_ = existingRows.Close()
	}()
	existing := map[string]struct{}{}
	for existingRows.Next() {
		var artifactID string
		if scanErr := existingRows.Scan(&artifactID); scanErr != nil {
			return scanErr
		}
		existing[artifactID] = struct{}{}
	}
	if rowsErr := existingRows.Err(); rowsErr != nil {
		return rowsErr
	}
	for _, artifactID := range artifactIDs {
		if _, ok := existing[artifactID]; !ok {
			return repository.ErrVersionTagTargetNotFound
		}
		if _, ok := found[artifactID]; !ok {
			return repository.ErrVersionTagTargetJobMismatch
		}
	}
	return nil
}

func (r *VersionTagRepository) validateManagedImageVersionTargets(ctx context.Context, tx *sql.Tx, jobID string, managedImageVersionIDs []string) error {
	if len(managedImageVersionIDs) == 0 {
		return nil
	}
	query, args := stringListQuery(`
		SELECT managed_image_versions.id
		FROM managed_image_versions
		JOIN managed_images ON managed_images.id = managed_image_versions.managed_image_id
		JOIN jobs ON jobs.project_id = managed_images.project_id
		WHERE managed_image_versions.id IN (%s) AND jobs.id = $%d
	`, 1, managedImageVersionIDs)
	query = fmt.Sprintf(query, len(args)+1)
	args = append(args, jobID)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()
	found := map[string]struct{}{}
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return scanErr
		}
		found[id] = struct{}{}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return rowsErr
	}
	for _, id := range managedImageVersionIDs {
		if _, ok := found[id]; !ok {
			break
		}
	}

	existenceQuery, existenceArgs := stringListQuery(`SELECT id FROM managed_image_versions WHERE id IN (%s)`, 1, managedImageVersionIDs)
	existingRows, err := tx.QueryContext(ctx, existenceQuery, existenceArgs...)
	if err != nil {
		return err
	}
	defer func() {
		_ = existingRows.Close()
	}()
	existing := map[string]struct{}{}
	for existingRows.Next() {
		var id string
		if scanErr := existingRows.Scan(&id); scanErr != nil {
			return scanErr
		}
		existing[id] = struct{}{}
	}
	if err := existingRows.Err(); err != nil {
		return err
	}
	for _, id := range managedImageVersionIDs {
		if _, ok := existing[id]; !ok {
			return repository.ErrVersionTagTargetNotFound
		}
		if _, ok := found[id]; !ok {
			return repository.ErrVersionTagTargetJobMismatch
		}
	}
	return nil
}

func (r *VersionTagRepository) ensureNoDuplicateTargets(ctx context.Context, tx *sql.Tx, jobID string, version string, artifactIDs []string, managedImageVersionIDs []string) error {
	if len(artifactIDs) > 0 {
		artifactQuery, args := stringListQuery(`
			SELECT 1
			FROM version_tags
			WHERE job_id = $1 AND version_text = $2 AND artifact_id IN (%s)
			LIMIT 1
		`, 3, artifactIDs)
		var marker int
		err := tx.QueryRowContext(ctx, artifactQuery, append([]any{jobID, version}, args...)...).Scan(&marker)
		if err == nil {
			return repository.ErrVersionTagConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if len(managedImageVersionIDs) > 0 {
		imageQuery, args := stringListQuery(`
			SELECT 1
			FROM version_tags
			WHERE job_id = $1 AND version_text = $2 AND managed_image_version_id IN (%s)
			LIMIT 1
		`, 3, managedImageVersionIDs)
		var marker int
		err := tx.QueryRowContext(ctx, imageQuery, append([]any{jobID, version}, args...)...).Scan(&marker)
		if err == nil {
			return repository.ErrVersionTagConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func (r *VersionTagRepository) insertVersionTag(ctx context.Context, tx *sql.Tx, tag domain.VersionTag) (domain.VersionTag, error) {
	const query = `
		INSERT INTO version_tags (
			id,
			job_id,
			version_text,
			target_type,
			artifact_id,
			managed_image_version_id
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + versionTagColumns

	created, err := scanVersionTag(tx.QueryRowContext(ctx, query,
		tag.ID,
		tag.JobID,
		tag.Version,
		string(tag.TargetType),
		tag.ArtifactID,
		tag.ManagedImageVersionID,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.VersionTag{}, repository.ErrVersionTagConflict
		}
		return domain.VersionTag{}, err
	}

	return created, nil
}

func scanVersionTagRows(rows *sql.Rows, queryErr error) ([]domain.VersionTag, error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]domain.VersionTag, 0)
	for rows.Next() {
		tag, err := scanVersionTag(rows)
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

func scanVersionTag(scanner rowScanner) (domain.VersionTag, error) {
	var tag domain.VersionTag
	var targetType string
	var artifactID sql.NullString
	var managedImageVersionID sql.NullString
	if err := scanner.Scan(
		&tag.ID,
		&tag.JobID,
		&tag.Version,
		&targetType,
		&artifactID,
		&managedImageVersionID,
		&tag.CreatedAt,
	); err != nil {
		return domain.VersionTag{}, err
	}
	tag.TargetType = domain.VersionTagTargetType(targetType)
	if artifactID.Valid {
		tag.ArtifactID = versionTagStringPtr(artifactID.String)
	}
	if managedImageVersionID.Valid {
		tag.ManagedImageVersionID = versionTagStringPtr(managedImageVersionID.String)
	}
	return tag, nil
}

func versionTagStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func stringListQuery(base string, startIndex int, values []string) (string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for index, value := range values {
		placeholders = append(placeholders, "$"+strconv.Itoa(startIndex+index))
		args = append(args, value)
	}
	return fmt.Sprintf(base, strings.Join(placeholders, ", ")), args
}
