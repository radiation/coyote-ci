package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserSlackIdentityRepository struct {
	db *sql.DB
}

func NewUserSlackIdentityRepository(db *sql.DB) *UserSlackIdentityRepository {
	return &UserSlackIdentityRepository{db: db}
}

const userSlackIdentityColumns = `id::text, user_id::text, slack_workspace_integration_id::text, slack_user_id, slack_display_name, slack_real_name, slack_handle, slack_email, profile_image_url, enabled, linked_at, last_verified_at, created_at, updated_at`

func (r *UserSlackIdentityRepository) GetByUserID(ctx context.Context, userID string) (domain.UserSlackIdentity, error) {
	const query = `
		SELECT ` + userSlackIdentityColumns + `
		FROM user_slack_identities
		WHERE user_id = $1
	`

	identity, err := scanUserSlackIdentity(r.db.QueryRowContext(ctx, query, strings.TrimSpace(userID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
		}
		return domain.UserSlackIdentity{}, err
	}
	return identity, nil
}

func (r *UserSlackIdentityRepository) Upsert(ctx context.Context, identity domain.UserSlackIdentity) (domain.UserSlackIdentity, error) {
	const query = `
		INSERT INTO user_slack_identities (
			id,
			user_id,
			slack_workspace_integration_id,
			slack_user_id,
			slack_display_name,
			slack_real_name,
			slack_handle,
			slack_email,
			profile_image_url,
			enabled,
			linked_at,
			last_verified_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (user_id) DO UPDATE
		SET slack_workspace_integration_id = EXCLUDED.slack_workspace_integration_id,
			slack_user_id = EXCLUDED.slack_user_id,
			slack_display_name = EXCLUDED.slack_display_name,
			slack_real_name = EXCLUDED.slack_real_name,
			slack_handle = EXCLUDED.slack_handle,
			slack_email = EXCLUDED.slack_email,
			profile_image_url = EXCLUDED.profile_image_url,
			enabled = EXCLUDED.enabled,
			linked_at = CASE
				WHEN user_slack_identities.slack_workspace_integration_id = EXCLUDED.slack_workspace_integration_id
				 AND user_slack_identities.slack_user_id = EXCLUDED.slack_user_id
				THEN user_slack_identities.linked_at
				ELSE EXCLUDED.linked_at
			END,
			last_verified_at = EXCLUDED.last_verified_at,
			updated_at = EXCLUDED.updated_at
		RETURNING ` + userSlackIdentityColumns + `
	`

	stored, err := scanUserSlackIdentity(r.db.QueryRowContext(
		ctx,
		query,
		identity.ID,
		strings.TrimSpace(identity.UserID),
		strings.TrimSpace(identity.SlackWorkspaceIntegrationID),
		strings.TrimSpace(identity.SlackUserID),
		nullableOptionalString(identity.SlackDisplayName),
		nullableOptionalString(identity.SlackRealName),
		nullableOptionalString(identity.SlackHandle),
		nullableOptionalString(identity.SlackEmail),
		nullableOptionalString(identity.ProfileImageURL),
		identity.Enabled,
		identity.LinkedAt,
		nullableTimeOptional(identity.LastVerifiedAt),
		identity.CreatedAt,
		identity.UpdatedAt,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityConflict
		}
		return domain.UserSlackIdentity{}, err
	}
	return stored, nil
}

func (r *UserSlackIdentityRepository) SetEnabled(ctx context.Context, userID string, enabled bool, updatedAt time.Time) (domain.UserSlackIdentity, error) {
	const query = `
		UPDATE user_slack_identities
		SET enabled = $2,
			updated_at = $3
		WHERE user_id = $1
		RETURNING ` + userSlackIdentityColumns + `
	`

	identity, err := scanUserSlackIdentity(r.db.QueryRowContext(ctx, query, strings.TrimSpace(userID), enabled, updatedAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.UserSlackIdentity{}, repository.ErrUserSlackIdentityNotFound
		}
		return domain.UserSlackIdentity{}, err
	}
	return identity, nil
}

func (r *UserSlackIdentityRepository) DeleteByUserID(ctx context.Context, userID string) error {
	const query = `DELETE FROM user_slack_identities WHERE user_id = $1`

	result, err := r.db.ExecContext(ctx, query, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrUserSlackIdentityNotFound
	}
	return nil
}

func (r *UserSlackIdentityRepository) CountByWorkspaceIntegrationID(ctx context.Context, workspaceIntegrationID string) (int, error) {
	const query = `SELECT COUNT(*) FROM user_slack_identities WHERE slack_workspace_integration_id = $1`

	var count int
	if err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(workspaceIntegrationID)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type userSlackIdentityScanner interface {
	Scan(dest ...any) error
}

func scanUserSlackIdentity(scanner userSlackIdentityScanner) (domain.UserSlackIdentity, error) {
	var identity domain.UserSlackIdentity
	var slackDisplayName sql.NullString
	var slackRealName sql.NullString
	var slackHandle sql.NullString
	var slackEmail sql.NullString
	var profileImageURL sql.NullString
	var lastVerifiedAt sql.NullTime

	err := scanner.Scan(
		&identity.ID,
		&identity.UserID,
		&identity.SlackWorkspaceIntegrationID,
		&identity.SlackUserID,
		&slackDisplayName,
		&slackRealName,
		&slackHandle,
		&slackEmail,
		&profileImageURL,
		&identity.Enabled,
		&identity.LinkedAt,
		&lastVerifiedAt,
		&identity.CreatedAt,
		&identity.UpdatedAt,
	)
	if err != nil {
		return domain.UserSlackIdentity{}, err
	}

	identity.SlackDisplayName = nullStringPtr(slackDisplayName)
	identity.SlackRealName = nullStringPtr(slackRealName)
	identity.SlackHandle = nullStringPtr(slackHandle)
	identity.SlackEmail = nullStringPtr(slackEmail)
	identity.ProfileImageURL = nullStringPtr(profileImageURL)
	if lastVerifiedAt.Valid {
		value := lastVerifiedAt.Time
		identity.LastVerifiedAt = &value
	}
	identity.UserID = strings.TrimSpace(identity.UserID)
	identity.SlackWorkspaceIntegrationID = strings.TrimSpace(identity.SlackWorkspaceIntegrationID)
	identity.SlackUserID = strings.TrimSpace(identity.SlackUserID)
	return identity, nil
}
