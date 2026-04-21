package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type ManagedImageCatalogRepository struct {
	db *sql.DB
}

func NewManagedImageCatalogRepository(db *sql.DB) *ManagedImageCatalogRepository {
	return &ManagedImageCatalogRepository{db: db}
}

func (r *ManagedImageCatalogRepository) EnsureManagedImage(ctx context.Context, projectID string, name string) (domain.ManagedImage, error) {
	trimmedProjectID := strings.TrimSpace(projectID)
	trimmedName := strings.TrimSpace(name)

	const insertQuery = `
		INSERT INTO managed_images (id, project_id, name)
		VALUES (gen_random_uuid(), $1, $2)
		ON CONFLICT (project_id, name) DO NOTHING
	`
	if _, err := r.db.ExecContext(ctx, insertQuery, trimmedProjectID, trimmedName); err != nil {
		return domain.ManagedImage{}, err
	}

	const selectQuery = `
		SELECT id, project_id, name, description, created_at, updated_at
		FROM managed_images
		WHERE project_id = $1 AND name = $2
	`

	managedImage, err := scanManagedImage(r.db.QueryRowContext(ctx, selectQuery, trimmedProjectID, trimmedName))
	if err != nil {
		return domain.ManagedImage{}, err
	}
	return managedImage, nil
}

func (r *ManagedImageCatalogRepository) FindVersionByFingerprint(ctx context.Context, managedImageID string, dependencyFingerprint string) (domain.ManagedImageVersion, bool, error) {
	trimmedManagedImageID := strings.TrimSpace(managedImageID)
	trimmedFingerprint := strings.TrimSpace(dependencyFingerprint)
	if trimmedManagedImageID == "" || trimmedFingerprint == "" {
		return domain.ManagedImageVersion{}, false, nil
	}

	const query = `
		SELECT id, managed_image_id, version_label, image_ref, image_digest, dependency_fingerprint, source_repository_url, created_at
		FROM managed_image_versions
		WHERE managed_image_id = $1 AND dependency_fingerprint = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	version, err := scanManagedImageVersion(r.db.QueryRowContext(ctx, query, trimmedManagedImageID, trimmedFingerprint))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ManagedImageVersion{}, false, nil
		}
		return domain.ManagedImageVersion{}, false, err
	}
	return version, true, nil
}

func (r *ManagedImageCatalogRepository) CreateVersion(ctx context.Context, version domain.ManagedImageVersion) (domain.ManagedImageVersion, error) {
	const query = `
		INSERT INTO managed_image_versions (
			id,
			managed_image_id,
			version_label,
			image_ref,
			image_digest,
			metadata_json,
			dependency_fingerprint,
			source_repository_url,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, $7, $8)
		RETURNING id, managed_image_id, version_label, image_ref, image_digest, dependency_fingerprint, source_repository_url, created_at
	`

	created, err := scanManagedImageVersion(r.db.QueryRowContext(ctx, query,
		version.ID,
		version.ManagedImageID,
		version.VersionLabel,
		version.ImageRef,
		version.ImageDigest,
		version.DependencyFingerprint,
		version.SourceRepositoryURL,
		version.CreatedAt,
	))
	if err != nil {
		return domain.ManagedImageVersion{}, err
	}
	return created, nil
}

type managedImageScanner interface {
	Scan(dest ...any) error
}

func scanManagedImage(scanner managedImageScanner) (domain.ManagedImage, error) {
	var managedImage domain.ManagedImage
	var description sql.NullString

	if err := scanner.Scan(
		&managedImage.ID,
		&managedImage.ProjectID,
		&managedImage.Name,
		&description,
		&managedImage.CreatedAt,
		&managedImage.UpdatedAt,
	); err != nil {
		return domain.ManagedImage{}, err
	}

	if description.Valid {
		v := description.String
		managedImage.Description = &v
	}
	return managedImage, nil
}

func scanManagedImageVersion(scanner managedImageScanner) (domain.ManagedImageVersion, error) {
	var version domain.ManagedImageVersion
	var dependencyFingerprint sql.NullString
	var sourceRepositoryURL sql.NullString

	if err := scanner.Scan(
		&version.ID,
		&version.ManagedImageID,
		&version.VersionLabel,
		&version.ImageRef,
		&version.ImageDigest,
		&dependencyFingerprint,
		&sourceRepositoryURL,
		&version.CreatedAt,
	); err != nil {
		return domain.ManagedImageVersion{}, err
	}

	if dependencyFingerprint.Valid {
		v := dependencyFingerprint.String
		version.DependencyFingerprint = &v
	}
	if sourceRepositoryURL.Valid {
		v := sourceRepositoryURL.String
		version.SourceRepositoryURL = &v
	}
	return version, nil
}
