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

type SlackWorkspaceIntegrationRepository struct {
	db *sql.DB
}

func NewSlackWorkspaceIntegrationRepository(db *sql.DB) *SlackWorkspaceIntegrationRepository {
	return &SlackWorkspaceIntegrationRepository{db: db}
}

const slackWorkspaceIntegrationColumns = `id::text, workspace_id, workspace_name, workspace_url, bot_user_id, authed_user_id, app_id, bot_token_secret, enabled, connected_at, last_tested_at, last_test_succeeded, created_at, updated_at`

func (r *SlackWorkspaceIntegrationRepository) Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error) {
	const query = `
		SELECT ` + slackWorkspaceIntegrationColumns + `
		FROM slack_workspace_integrations
		WHERE singleton = TRUE
	`

	integration, err := scanSlackWorkspaceIntegration(r.db.QueryRowContext(ctx, query))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
		}
		return domain.SlackWorkspaceIntegration{}, err
	}
	return integration, nil
}

func (r *SlackWorkspaceIntegrationRepository) ConnectOrReplace(ctx context.Context, candidate domain.SlackWorkspaceIntegration, replaceDifferentWorkspace bool) (domain.SlackWorkspaceIntegration, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	existing, err := scanSlackWorkspaceIntegration(tx.QueryRowContext(ctx, `
		SELECT `+slackWorkspaceIntegrationColumns+`
		FROM slack_workspace_integrations
		WHERE singleton = TRUE
		FOR UPDATE
	`))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.SlackWorkspaceIntegration{}, err
	}

	if errors.Is(err, sql.ErrNoRows) {
		created, createErr := scanSlackWorkspaceIntegration(tx.QueryRowContext(ctx, `
			INSERT INTO slack_workspace_integrations (
				id,
				singleton,
				workspace_id,
				workspace_name,
				workspace_url,
				bot_user_id,
				authed_user_id,
				app_id,
				bot_token_secret,
				enabled,
				connected_at,
				last_tested_at,
				last_test_succeeded,
				created_at,
				updated_at
			)
			VALUES ($1, TRUE, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING `+slackWorkspaceIntegrationColumns+`
		`,
			candidate.ID,
			candidate.WorkspaceID,
			nullableOptionalString(candidate.WorkspaceName),
			nullableOptionalString(candidate.WorkspaceURL),
			nullableOptionalString(candidate.BotUserID),
			nullableOptionalString(candidate.AuthedUserID),
			nullableOptionalString(candidate.AppID),
			candidate.BotTokenSecret,
			candidate.Enabled,
			candidate.ConnectedAt,
			nullableTimeOptional(candidate.LastTestedAt),
			nullableBoolOptional(candidate.LastTestSucceeded),
			candidate.CreatedAt,
			candidate.UpdatedAt,
		))
		if createErr != nil {
			if isUniqueViolation(createErr) {
				return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationConflict
			}
			return domain.SlackWorkspaceIntegration{}, createErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return domain.SlackWorkspaceIntegration{}, commitErr
		}
		committed = true
		return created, nil
	}

	if existing.WorkspaceID != candidate.WorkspaceID && !replaceDifferentWorkspace {
		return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationReplaceRequired
	}

	updated, updateErr := scanSlackWorkspaceIntegration(tx.QueryRowContext(ctx, `
		UPDATE slack_workspace_integrations
		SET workspace_id = $1,
			workspace_name = $2,
			workspace_url = $3,
			bot_user_id = $4,
			authed_user_id = $5,
			app_id = $6,
			bot_token_secret = $7,
			enabled = $8,
			connected_at = $9,
			last_tested_at = $10,
			last_test_succeeded = $11,
			updated_at = $12
		WHERE singleton = TRUE
		RETURNING `+slackWorkspaceIntegrationColumns+`
	`,
		candidate.WorkspaceID,
		nullableOptionalString(candidate.WorkspaceName),
		nullableOptionalString(candidate.WorkspaceURL),
		nullableOptionalString(candidate.BotUserID),
		nullableOptionalString(candidate.AuthedUserID),
		nullableOptionalString(candidate.AppID),
		candidate.BotTokenSecret,
		existing.Enabled,
		candidate.ConnectedAt,
		nullableTimeOptional(candidate.LastTestedAt),
		nullableBoolOptional(candidate.LastTestSucceeded),
		candidate.UpdatedAt,
	))
	if updateErr != nil {
		if isUniqueViolation(updateErr) {
			return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationConflict
		}
		return domain.SlackWorkspaceIntegration{}, updateErr
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return domain.SlackWorkspaceIntegration{}, commitErr
	}
	committed = true
	return updated, nil
}

func (r *SlackWorkspaceIntegrationRepository) SetEnabled(ctx context.Context, enabled bool, updatedAt time.Time) (domain.SlackWorkspaceIntegration, error) {
	const query = `
		UPDATE slack_workspace_integrations
		SET enabled = $1,
			updated_at = $2
		WHERE singleton = TRUE
		RETURNING ` + slackWorkspaceIntegrationColumns + `
	`

	integration, err := scanSlackWorkspaceIntegration(r.db.QueryRowContext(ctx, query, enabled, updatedAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
		}
		return domain.SlackWorkspaceIntegration{}, err
	}
	return integration, nil
}

func (r *SlackWorkspaceIntegrationRepository) UpdateLastTestResult(ctx context.Context, testedAt time.Time, succeeded bool) (domain.SlackWorkspaceIntegration, error) {
	const query = `
		UPDATE slack_workspace_integrations
		SET last_tested_at = $1,
			last_test_succeeded = $2,
			updated_at = $1
		WHERE singleton = TRUE
		RETURNING ` + slackWorkspaceIntegrationColumns + `
	`

	integration, err := scanSlackWorkspaceIntegration(r.db.QueryRowContext(ctx, query, testedAt, succeeded))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SlackWorkspaceIntegration{}, repository.ErrSlackWorkspaceIntegrationNotFound
		}
		return domain.SlackWorkspaceIntegration{}, err
	}
	return integration, nil
}

func (r *SlackWorkspaceIntegrationRepository) Delete(ctx context.Context) error {
	const query = `DELETE FROM slack_workspace_integrations WHERE singleton = TRUE`

	res, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrSlackWorkspaceIntegrationNotFound
	}
	return nil
}

type slackWorkspaceIntegrationScanner interface {
	Scan(dest ...any) error
}

func scanSlackWorkspaceIntegration(scanner slackWorkspaceIntegrationScanner) (domain.SlackWorkspaceIntegration, error) {
	var integration domain.SlackWorkspaceIntegration
	var workspaceName sql.NullString
	var workspaceURL sql.NullString
	var botUserID sql.NullString
	var authedUserID sql.NullString
	var appID sql.NullString
	var lastTestedAt sql.NullTime
	var lastTestSucceeded sql.NullBool

	err := scanner.Scan(
		&integration.ID,
		&integration.WorkspaceID,
		&workspaceName,
		&workspaceURL,
		&botUserID,
		&authedUserID,
		&appID,
		&integration.BotTokenSecret,
		&integration.Enabled,
		&integration.ConnectedAt,
		&lastTestedAt,
		&lastTestSucceeded,
		&integration.CreatedAt,
		&integration.UpdatedAt,
	)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}

	integration.WorkspaceName = nullStringPtr(workspaceName)
	integration.WorkspaceURL = nullStringPtr(workspaceURL)
	integration.BotUserID = nullStringPtr(botUserID)
	integration.AuthedUserID = nullStringPtr(authedUserID)
	integration.AppID = nullStringPtr(appID)
	if lastTestedAt.Valid {
		v := lastTestedAt.Time
		integration.LastTestedAt = &v
	}
	if lastTestSucceeded.Valid {
		integration.LastTestSucceeded = boolPtrSlack(lastTestSucceeded.Bool)
	}

	integration.WorkspaceID = strings.TrimSpace(integration.WorkspaceID)
	return integration, nil
}

func nullableTimeOptional(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolOptional(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func boolPtrSlack(value bool) *bool {
	v := value
	return &v
}
