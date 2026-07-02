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

const userNotificationPreferenceColumns = `user_id::text, commit_author_failure_email_enabled, commit_author_failure_slack_enabled, commit_author_failure_email_source, commit_author_success_email_enabled, commit_author_success_slack_enabled, commit_author_success_email_source, created_at, updated_at`

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

func (r *UserNotificationPreferenceRepository) InitializeIfAbsent(ctx context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, bool, error) {
	const query = `
		INSERT INTO user_notification_preferences (
			user_id,
			commit_author_failure_email_enabled,
			commit_author_failure_slack_enabled,
			commit_author_failure_email_source,
			commit_author_success_email_enabled,
			commit_author_success_slack_enabled,
			commit_author_success_email_source,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING ` + userNotificationPreferenceColumns + `
	`

	inserted, err := scanUserNotificationPreference(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(preference.UserID),
		preference.CommitAuthorFailureEmailEnabled,
		preference.CommitAuthorFailureSlackEnabled,
		preference.CommitAuthorFailureEmailSource,
		preference.CommitAuthorSuccessEmailEnabled,
		preference.CommitAuthorSuccessSlackEnabled,
		nullablePreferenceSource(preference.CommitAuthorSuccessEmailSource),
		preference.CreatedAt,
		preference.UpdatedAt,
	))
	if err == nil {
		return inserted, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.UserNotificationPreference{}, false, err
	}

	existing, getErr := r.GetByUserID(ctx, preference.UserID)
	if getErr != nil {
		return domain.UserNotificationPreference{}, false, getErr
	}
	return existing, false, nil
}

func (r *UserNotificationPreferenceRepository) Upsert(ctx context.Context, preference domain.UserNotificationPreference) (domain.UserNotificationPreference, error) {
	const query = `
		INSERT INTO user_notification_preferences (
			user_id,
			commit_author_failure_email_enabled,
			commit_author_failure_slack_enabled,
			commit_author_failure_email_source,
			commit_author_success_email_enabled,
			commit_author_success_slack_enabled,
			commit_author_success_email_source,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id)
		DO UPDATE SET
			commit_author_failure_email_enabled = EXCLUDED.commit_author_failure_email_enabled,
			commit_author_failure_slack_enabled = EXCLUDED.commit_author_failure_slack_enabled,
			commit_author_failure_email_source = EXCLUDED.commit_author_failure_email_source,
			commit_author_success_email_enabled = EXCLUDED.commit_author_success_email_enabled,
			commit_author_success_slack_enabled = EXCLUDED.commit_author_success_slack_enabled,
			commit_author_success_email_source = EXCLUDED.commit_author_success_email_source,
			updated_at = EXCLUDED.updated_at
		RETURNING ` + userNotificationPreferenceColumns + `
	`

	return scanUserNotificationPreference(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(preference.UserID),
		preference.CommitAuthorFailureEmailEnabled,
		preference.CommitAuthorFailureSlackEnabled,
		preference.CommitAuthorFailureEmailSource,
		preference.CommitAuthorSuccessEmailEnabled,
		preference.CommitAuthorSuccessSlackEnabled,
		nullablePreferenceSource(preference.CommitAuthorSuccessEmailSource),
		preference.CreatedAt,
		preference.UpdatedAt,
	))
}

func scanUserNotificationPreference(scanner rowScanner) (domain.UserNotificationPreference, error) {
	var preference domain.UserNotificationPreference
	var successSource sql.NullString
	err := scanner.Scan(
		&preference.UserID,
		&preference.CommitAuthorFailureEmailEnabled,
		&preference.CommitAuthorFailureSlackEnabled,
		&preference.CommitAuthorFailureEmailSource,
		&preference.CommitAuthorSuccessEmailEnabled,
		&preference.CommitAuthorSuccessSlackEnabled,
		&successSource,
		&preference.CreatedAt,
		&preference.UpdatedAt,
	)
	if err != nil {
		return domain.UserNotificationPreference{}, err
	}
	if successSource.Valid {
		source := domain.UserNotificationPreferenceSource(successSource.String)
		preference.CommitAuthorSuccessEmailSource = &source
	}
	return preference, nil
}

func nullablePreferenceSource(source *domain.UserNotificationPreferenceSource) sql.NullString {
	if source == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*source), Valid: true}
}
