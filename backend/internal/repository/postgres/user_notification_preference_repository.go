package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserNotificationPreferenceRepository struct {
	db *sql.DB
}

func NewUserNotificationPreferenceRepository(db *sql.DB) *UserNotificationPreferenceRepository {
	return &UserNotificationPreferenceRepository{db: db}
}

const userNotificationPreferenceColumns = `user_id::text, commit_author_failure_enabled, created_at, updated_at`

func (r *UserNotificationPreferenceRepository) GetByUserID(ctx context.Context, userID string) (domain.UserNotificationPreference, error) {
	const query = `
		SELECT ` + userNotificationPreferenceColumns + `
		FROM user_notification_preferences
		WHERE user_id = $1
	`

	preference, err := scanUserNotificationPreference(r.db.QueryRowContext(ctx, query, strings.TrimSpace(userID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.UserNotificationPreference{}, repository.ErrUserNotificationPreferenceNotFound
		}
		return domain.UserNotificationPreference{}, err
	}
	return preference, nil
}

func (r *UserNotificationPreferenceRepository) Upsert(ctx context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, error) {
	const query = `
		INSERT INTO user_notification_preferences (user_id, commit_author_failure_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id)
		DO UPDATE SET
			commit_author_failure_enabled = EXCLUDED.commit_author_failure_enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING ` + userNotificationPreferenceColumns + `
	`

	return scanUserNotificationPreference(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(preference.UserID),
		preference.CommitAuthorFailureEnabled,
		preference.CreatedAt,
		preference.UpdatedAt,
	))
}

func scanUserNotificationPreference(scanner rowScanner) (domain.UserNotificationPreference, error) {
	var preference domain.UserNotificationPreference
	err := scanner.Scan(
		&preference.UserID,
		&preference.CommitAuthorFailureEnabled,
		&preference.CreatedAt,
		&preference.UpdatedAt,
	)
	if err != nil {
		return domain.UserNotificationPreference{}, err
	}
	return preference, nil
}
