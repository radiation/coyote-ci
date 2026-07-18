package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMConnectionRepository struct {
	db *sql.DB
}

func NewSCMConnectionRepository(db *sql.DB) *SCMConnectionRepository {
	return &SCMConnectionRepository{db: db}
}

const scmConnectionColumns = `c.id, c.provider, c.display_name, c.deployment_kind, c.api_base_url, c.web_base_url, c.enabled, c.health_status, c.health_summary, c.last_health_checked_at, c.created_at, c.updated_at,
	ga.id, ga.app_id, ga.display_name, ga.api_base_url, ga.web_base_url, ga.private_key_secret_ref, ga.webhook_secret_ref, ga.created_at, ga.updated_at,
	gi.connection_id, gi.app_registration_id, gi.installation_id, gi.account_login, gi.account_type, gi.account_id, gi.created_at, gi.updated_at`

func (r *SCMConnectionRepository) CreateGitHubAppRegistration(ctx context.Context, registration domain.GitHubAppRegistration) (domain.GitHubAppRegistration, error) {
	registration = registration.Normalize()
	if err := registration.Validate(); err != nil {
		return domain.GitHubAppRegistration{}, err
	}

	const query = `
		INSERT INTO github_app_registrations (
			id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at
	`
	created, err := scanGitHubAppRegistration(r.db.QueryRowContext(ctx, query,
		registration.ID,
		registration.AppID,
		registration.DisplayName,
		registration.APIBaseURL,
		registration.WebBaseURL,
		registration.PrivateKeySecretRef,
		registration.WebhookSecretRef,
		registration.CreatedAt,
		registration.UpdatedAt,
	))
	if err != nil {
		if isSCMConnectionUniqueViolation(err, "github_app_registrations_app_id_api_base_url_web_base_url_key") || isSCMConnectionUniqueViolation(err, "github_app_registrations_pkey") {
			return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationConflict
		}
		return domain.GitHubAppRegistration{}, err
	}
	return created, nil
}

func (r *SCMConnectionRepository) ListGitHubAppRegistrations(ctx context.Context) ([]domain.GitHubAppRegistration, error) {
	const query = `
		SELECT id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at
		FROM github_app_registrations
		ORDER BY created_at DESC, id DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.GitHubAppRegistration, 0)
	for rows.Next() {
		item, scanErr := scanGitHubAppRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SCMConnectionRepository) GetGitHubAppRegistrationByID(ctx context.Context, id string) (domain.GitHubAppRegistration, error) {
	const query = `
		SELECT id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at
		FROM github_app_registrations
		WHERE id = $1
	`
	registration, err := scanGitHubAppRegistration(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationNotFound
		}
		return domain.GitHubAppRegistration{}, err
	}
	return registration, nil
}

func (r *SCMConnectionRepository) CreateGitHubAppInstallationConnection(ctx context.Context, detail domain.SCMConnectionDetail) (domain.SCMConnectionDetail, error) {
	detail = detail.Normalize()
	if err := detail.Validate(); err != nil {
		return domain.SCMConnectionDetail{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SCMConnectionDetail{}, err
	}
	defer func() { _ = tx.Rollback() }()

	registration, err := getGitHubAppRegistrationForUpdate(ctx, tx, detail.GitHubAppRegistration.ID)
	if err != nil {
		return domain.SCMConnectionDetail{}, err
	}

	const insertConnectionQuery = `
		INSERT INTO scm_connections (
			id, provider, display_name, deployment_kind, api_base_url, web_base_url, enabled, health_status, health_summary, last_health_checked_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err = tx.ExecContext(ctx, insertConnectionQuery,
		detail.Connection.ID,
		string(detail.Connection.Provider),
		detail.Connection.DisplayName,
		string(detail.Connection.DeploymentKind),
		detail.Connection.APIBaseURL,
		detail.Connection.WebBaseURL,
		detail.Connection.Enabled,
		string(detail.Connection.HealthStatus),
		detail.Connection.HealthSummary,
		detail.Connection.LastHealthCheckedAt,
		detail.Connection.CreatedAt,
		detail.Connection.UpdatedAt,
	)
	if err != nil {
		if isSCMConnectionUniqueViolation(err, "scm_connections_pkey") {
			return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionConflict
		}
		return domain.SCMConnectionDetail{}, err
	}

	installation := *detail.GitHubAppInstallation
	const insertInstallationQuery = `
		INSERT INTO github_app_installations (
			connection_id, app_registration_id, installation_id, account_login, account_type, account_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = tx.ExecContext(ctx, insertInstallationQuery,
		installation.ConnectionID,
		installation.AppRegistrationID,
		installation.InstallationID,
		installation.AccountLogin,
		installation.AccountType,
		installation.AccountID,
		installation.CreatedAt,
		installation.UpdatedAt,
	)
	if err != nil {
		if isSCMConnectionUniqueViolation(err, "github_app_installations_app_registration_id_installation_id_key") {
			return domain.SCMConnectionDetail{}, repository.ErrSCMGitHubAppInstallationConflict
		}
		return domain.SCMConnectionDetail{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.SCMConnectionDetail{}, err
	}
	return domain.SCMConnectionDetail{Connection: detail.Connection, GitHubAppRegistration: &registration, GitHubAppInstallation: &installation}, nil
}

func (r *SCMConnectionRepository) List(ctx context.Context) ([]domain.SCMConnectionDetail, error) {
	const query = `
		SELECT ` + scmConnectionColumns + `
		FROM scm_connections c
		LEFT JOIN github_app_installations gi ON gi.connection_id = c.id
		LEFT JOIN github_app_registrations ga ON ga.id = gi.app_registration_id
		ORDER BY c.created_at DESC, c.id DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.SCMConnectionDetail, 0)
	for rows.Next() {
		item, scanErr := scanSCMConnectionDetail(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SCMConnectionRepository) GetByID(ctx context.Context, id string) (domain.SCMConnectionDetail, error) {
	const query = `
		SELECT ` + scmConnectionColumns + `
		FROM scm_connections c
		LEFT JOIN github_app_installations gi ON gi.connection_id = c.id
		LEFT JOIN github_app_registrations ga ON ga.id = gi.app_registration_id
		WHERE c.id = $1
	`
	item, err := scanSCMConnectionDetail(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
		}
		return domain.SCMConnectionDetail{}, err
	}
	return item, nil
}

func (r *SCMConnectionRepository) SetEnabled(ctx context.Context, id string, enabled bool, updatedAt time.Time) (domain.SCMConnectionDetail, error) {
	const query = `
		UPDATE scm_connections
		SET enabled = $2, updated_at = $3
		WHERE id = $1
		RETURNING id
	`
	var returnedID string
	if err := r.db.QueryRowContext(ctx, query, id, enabled, updatedAt.UTC()).Scan(&returnedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
		}
		return domain.SCMConnectionDetail{}, err
	}
	return r.GetByID(ctx, returnedID)
}

func (r *SCMConnectionRepository) UpdateHealth(ctx context.Context, id string, status domain.SCMConnectionHealthStatus, summary *string, checkedAt time.Time, updatedAt time.Time) (domain.SCMConnectionDetail, error) {
	const query = `
		UPDATE scm_connections
		SET health_status = $2, health_summary = $3, last_health_checked_at = $4, updated_at = $5
		WHERE id = $1
		RETURNING id
	`
	var returnedID string
	if err := r.db.QueryRowContext(ctx, query, id, string(status), summary, checkedAt.UTC(), updatedAt.UTC()).Scan(&returnedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
		}
		return domain.SCMConnectionDetail{}, err
	}
	return r.GetByID(ctx, returnedID)
}

func getGitHubAppRegistrationForUpdate(ctx context.Context, tx *sql.Tx, registrationID string) (domain.GitHubAppRegistration, error) {
	const query = `
		SELECT id, app_id, display_name, api_base_url, web_base_url, private_key_secret_ref, webhook_secret_ref, created_at, updated_at
		FROM github_app_registrations
		WHERE id = $1
		FOR UPDATE
	`
	registration, err := scanGitHubAppRegistration(tx.QueryRowContext(ctx, query, registrationID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.GitHubAppRegistration{}, repository.ErrSCMGitHubAppRegistrationNotFound
		}
		return domain.GitHubAppRegistration{}, err
	}
	return registration, nil
}

type scmConnectionDetailScanner interface{ Scan(dest ...any) error }

func scanSCMConnectionDetail(scanner scmConnectionDetailScanner) (domain.SCMConnectionDetail, error) {
	var detail domain.SCMConnectionDetail
	var provider string
	var deploymentKind string
	var healthStatus string
	var connectionHealthSummary sql.NullString
	var connectionLastHealthCheckedAt sql.NullTime
	var registrationID sql.NullString
	var registrationAppID sql.NullString
	var registrationDisplayName sql.NullString
	var registrationAPIBaseURL sql.NullString
	var registrationWebBaseURL sql.NullString
	var registrationPrivateKeySecretRef sql.NullString
	var registrationWebhookSecretRef sql.NullString
	var registrationCreatedAt sql.NullTime
	var registrationUpdatedAt sql.NullTime
	var installationConnectionID sql.NullString
	var installationAppRegistrationID sql.NullString
	var installationID sql.NullString
	var installationAccountLogin sql.NullString
	var installationAccountType sql.NullString
	var installationAccountID sql.NullString
	var installationCreatedAt sql.NullTime
	var installationUpdatedAt sql.NullTime
	if err := scanner.Scan(
		&detail.Connection.ID,
		&provider,
		&detail.Connection.DisplayName,
		&deploymentKind,
		&detail.Connection.APIBaseURL,
		&detail.Connection.WebBaseURL,
		&detail.Connection.Enabled,
		&healthStatus,
		&connectionHealthSummary,
		&connectionLastHealthCheckedAt,
		&detail.Connection.CreatedAt,
		&detail.Connection.UpdatedAt,
		&registrationID,
		&registrationAppID,
		&registrationDisplayName,
		&registrationAPIBaseURL,
		&registrationWebBaseURL,
		&registrationPrivateKeySecretRef,
		&registrationWebhookSecretRef,
		&registrationCreatedAt,
		&registrationUpdatedAt,
		&installationConnectionID,
		&installationAppRegistrationID,
		&installationID,
		&installationAccountLogin,
		&installationAccountType,
		&installationAccountID,
		&installationCreatedAt,
		&installationUpdatedAt,
	); err != nil {
		return domain.SCMConnectionDetail{}, err
	}
	detail.Connection.Provider = domain.SCMProvider(provider)
	detail.Connection.DeploymentKind = domain.SCMDeploymentKind(deploymentKind)
	detail.Connection.HealthStatus = domain.SCMConnectionHealthStatus(healthStatus)
	if connectionHealthSummary.Valid {
		value := connectionHealthSummary.String
		detail.Connection.HealthSummary = &value
	}
	if connectionLastHealthCheckedAt.Valid {
		value := connectionLastHealthCheckedAt.Time
		detail.Connection.LastHealthCheckedAt = &value
	}
	if registrationID.Valid {
		registration := domain.GitHubAppRegistration{
			ID:                  registrationID.String,
			AppID:               registrationAppID.String,
			APIBaseURL:          registrationAPIBaseURL.String,
			WebBaseURL:          registrationWebBaseURL.String,
			PrivateKeySecretRef: registrationPrivateKeySecretRef.String,
			WebhookSecretRef:    registrationWebhookSecretRef.String,
		}
		if registrationDisplayName.Valid {
			value := registrationDisplayName.String
			registration.DisplayName = &value
		}
		if registrationCreatedAt.Valid {
			registration.CreatedAt = registrationCreatedAt.Time
		}
		if registrationUpdatedAt.Valid {
			registration.UpdatedAt = registrationUpdatedAt.Time
		}
		detail.GitHubAppRegistration = &registration
	}
	if installationConnectionID.Valid {
		installation := domain.GitHubAppInstallation{
			ConnectionID:      installationConnectionID.String,
			AppRegistrationID: installationAppRegistrationID.String,
			InstallationID:    installationID.String,
			AccountLogin:      installationAccountLogin.String,
			AccountType:       installationAccountType.String,
			AccountID:         installationAccountID.String,
		}
		if installationCreatedAt.Valid {
			installation.CreatedAt = installationCreatedAt.Time
		}
		if installationUpdatedAt.Valid {
			installation.UpdatedAt = installationUpdatedAt.Time
		}
		detail.GitHubAppInstallation = &installation
	}
	return detail.Normalize(), nil
}

type githubAppRegistrationScanner interface{ Scan(dest ...any) error }

func scanGitHubAppRegistration(scanner githubAppRegistrationScanner) (domain.GitHubAppRegistration, error) {
	var registration domain.GitHubAppRegistration
	var displayName sql.NullString
	if err := scanner.Scan(
		&registration.ID,
		&registration.AppID,
		&displayName,
		&registration.APIBaseURL,
		&registration.WebBaseURL,
		&registration.PrivateKeySecretRef,
		&registration.WebhookSecretRef,
		&registration.CreatedAt,
		&registration.UpdatedAt,
	); err != nil {
		return domain.GitHubAppRegistration{}, err
	}
	if displayName.Valid {
		value := displayName.String
		registration.DisplayName = &value
	}
	return registration.Normalize(), nil
}

func isSCMConnectionUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
