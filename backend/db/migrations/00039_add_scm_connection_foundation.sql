-- +goose Up

CREATE TABLE IF NOT EXISTS scm_connections (
    id UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    display_name TEXT NOT NULL,
    deployment_kind TEXT NOT NULL,
    api_base_url TEXT NOT NULL,
    web_base_url TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    health_summary TEXT,
    last_health_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT scm_connections_provider_check CHECK (provider IN ('github', 'gitlab', 'bitbucket')),
    CONSTRAINT scm_connections_display_name_check CHECK (display_name <> ''),
    CONSTRAINT scm_connections_deployment_kind_check CHECK (deployment_kind IN ('cloud', 'self_hosted')),
    CONSTRAINT scm_connections_api_base_url_check CHECK (api_base_url <> ''),
    CONSTRAINT scm_connections_web_base_url_check CHECK (web_base_url <> ''),
    CONSTRAINT scm_connections_health_status_check CHECK (health_status IN ('unknown', 'healthy', 'degraded', 'unhealthy', 'revoked')),
    CONSTRAINT scm_connections_health_summary_length_check CHECK (health_summary IS NULL OR length(health_summary) <= 512)
);

CREATE TABLE IF NOT EXISTS github_app_registrations (
    id UUID PRIMARY KEY,
    app_id TEXT NOT NULL,
    display_name TEXT,
    api_base_url TEXT NOT NULL,
    web_base_url TEXT NOT NULL,
    private_key_secret_ref TEXT NOT NULL,
    webhook_secret_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT github_app_registrations_app_id_check CHECK (app_id <> ''),
    CONSTRAINT github_app_registrations_api_base_url_check CHECK (api_base_url <> ''),
    CONSTRAINT github_app_registrations_web_base_url_check CHECK (web_base_url <> ''),
    CONSTRAINT github_app_registrations_private_key_secret_ref_check CHECK (private_key_secret_ref <> ''),
    CONSTRAINT github_app_registrations_webhook_secret_ref_check CHECK (webhook_secret_ref <> ''),
    CONSTRAINT github_app_registrations_app_id_api_base_url_web_base_url_key UNIQUE (app_id, api_base_url, web_base_url)
);

CREATE TABLE IF NOT EXISTS github_app_installations (
    connection_id UUID PRIMARY KEY REFERENCES scm_connections(id) ON DELETE CASCADE,
    app_registration_id UUID NOT NULL REFERENCES github_app_registrations(id) ON DELETE RESTRICT,
    installation_id TEXT NOT NULL,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL,
    account_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT github_app_installations_installation_id_check CHECK (installation_id <> ''),
    CONSTRAINT github_app_installations_account_login_check CHECK (account_login <> ''),
    CONSTRAINT github_app_installations_account_type_check CHECK (account_type <> ''),
    CONSTRAINT github_app_installations_account_id_check CHECK (account_id <> ''),
    CONSTRAINT github_app_installations_app_registration_id_installation_id_key UNIQUE (app_registration_id, installation_id)
);

CREATE TABLE IF NOT EXISTS scm_registered_repositories (
    id UUID PRIMARY KEY,
    connection_id UUID NOT NULL REFERENCES scm_connections(id) ON DELETE RESTRICT,
    provider_repository_id TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    repository_name TEXT NOT NULL,
    full_name TEXT NOT NULL,
    clone_url TEXT NOT NULL,
    web_url TEXT NOT NULL,
    default_branch TEXT,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    metadata_refreshed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT scm_registered_repositories_provider_repository_id_check CHECK (provider_repository_id <> ''),
    CONSTRAINT scm_registered_repositories_owner_name_check CHECK (owner_name <> ''),
    CONSTRAINT scm_registered_repositories_repository_name_check CHECK (repository_name <> ''),
    CONSTRAINT scm_registered_repositories_full_name_check CHECK (full_name <> ''),
    CONSTRAINT scm_registered_repositories_clone_url_check CHECK (clone_url <> ''),
    CONSTRAINT scm_registered_repositories_web_url_check CHECK (web_url <> ''),
    CONSTRAINT scm_registered_repositories_connection_id_provider_repository_id_key UNIQUE (connection_id, provider_repository_id)
);

CREATE INDEX IF NOT EXISTS idx_scm_connections_provider_created_at
    ON scm_connections (provider, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_github_app_installations_app_registration_id
    ON github_app_installations (app_registration_id);

CREATE INDEX IF NOT EXISTS idx_scm_registered_repositories_connection_id_created_at
    ON scm_registered_repositories (connection_id, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_scm_registered_repositories_connection_id_created_at;
DROP INDEX IF EXISTS idx_github_app_installations_app_registration_id;
DROP INDEX IF EXISTS idx_scm_connections_provider_created_at;
DROP TABLE IF EXISTS scm_registered_repositories;
DROP TABLE IF EXISTS github_app_installations;
DROP TABLE IF EXISTS github_app_registrations;
DROP TABLE IF EXISTS scm_connections;