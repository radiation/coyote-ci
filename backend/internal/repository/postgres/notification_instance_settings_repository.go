package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationInstanceSettingsRepository struct {
	db *sql.DB
}

func NewNotificationInstanceSettingsRepository(db *sql.DB) *NotificationInstanceSettingsRepository {
	return &NotificationInstanceSettingsRepository{db: db}
}

const notificationInstanceSettingsColumns = `default_commit_author_failure_email_enabled, created_at, updated_at`

func (r *NotificationInstanceSettingsRepository) Get(ctx context.Context) (domain.NotificationInstanceSettings, error) {
	const query = `
		SELECT ` + notificationInstanceSettingsColumns + `
		FROM notification_instance_settings
		WHERE singleton = TRUE
	`

	settings, err := scanNotificationInstanceSettings(r.db.QueryRowContext(ctx, query))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationInstanceSettings{}, repository.ErrNotificationInstanceSettingsNotFound
		}
		return domain.NotificationInstanceSettings{}, err
	}
	return settings, nil
}

func (r *NotificationInstanceSettingsRepository) Upsert(ctx context.Context, settings domain.NotificationInstanceSettings) (domain.NotificationInstanceSettings, error) {
	const query = `
		INSERT INTO notification_instance_settings (
			singleton,
			default_commit_author_failure_email_enabled,
			created_at,
			updated_at
		)
		VALUES (TRUE, $1, $2, $3)
		ON CONFLICT (singleton)
		DO UPDATE SET
			default_commit_author_failure_email_enabled = EXCLUDED.default_commit_author_failure_email_enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING ` + notificationInstanceSettingsColumns + `
	`

	return scanNotificationInstanceSettings(r.db.QueryRowContext(ctx, query,
		settings.DefaultCommitAuthorFailureEmailEnabled,
		settings.CreatedAt,
		settings.UpdatedAt,
	))
}

func scanNotificationInstanceSettings(scanner rowScanner) (domain.NotificationInstanceSettings, error) {
	var settings domain.NotificationInstanceSettings
	err := scanner.Scan(
		&settings.DefaultCommitAuthorFailureEmailEnabled,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	if err != nil {
		return domain.NotificationInstanceSettings{}, err
	}
	return settings, nil
}
