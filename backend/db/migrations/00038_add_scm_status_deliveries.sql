-- +goose Up

CREATE TABLE IF NOT EXISTS scm_status_deliveries (
    id UUID PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    repository_owner TEXT NOT NULL,
    repository_name TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    context_name TEXT NOT NULL,
    desired_state TEXT NOT NULL,
    last_sent_state TEXT,
    description TEXT NOT NULL,
    details_url TEXT,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    last_attempt_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    claim_expires_at TIMESTAMPTZ,
    claimed_by TEXT,
    failure_category TEXT,
    failure_reason TEXT,
    last_error TEXT,
    sent_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT scm_status_deliveries_logical_key_unique UNIQUE (build_id, provider, context_name, desired_state),
    CONSTRAINT scm_status_deliveries_provider_check CHECK (provider <> ''),
    CONSTRAINT scm_status_deliveries_repository_owner_check CHECK (repository_owner <> ''),
    CONSTRAINT scm_status_deliveries_repository_name_check CHECK (repository_name <> ''),
    CONSTRAINT scm_status_deliveries_commit_sha_check CHECK (commit_sha <> ''),
    CONSTRAINT scm_status_deliveries_context_name_check CHECK (context_name <> ''),
    CONSTRAINT scm_status_deliveries_description_check CHECK (description <> ''),
    CONSTRAINT scm_status_deliveries_desired_state_check CHECK (desired_state IN ('pending', 'success', 'failure', 'error')),
    CONSTRAINT scm_status_deliveries_last_sent_state_check CHECK (last_sent_state IS NULL OR last_sent_state IN ('pending', 'success', 'failure', 'error')),
    CONSTRAINT scm_status_deliveries_status_check CHECK (status IN ('pending', 'sending', 'retry_waiting', 'sent', 'failed_permanent', 'failed_exhausted', 'superseded')),
    CONSTRAINT scm_status_deliveries_attempts_check CHECK (attempts >= 0 AND attempts <= max_attempts),
    CONSTRAINT scm_status_deliveries_max_attempts_check CHECK (max_attempts > 0),
    CONSTRAINT scm_status_deliveries_failure_category_check CHECK (failure_category IS NULL OR failure_category IN ('retryable', 'permanent')),
    CONSTRAINT scm_status_deliveries_state_check CHECK (
        (status = 'pending'
            AND attempts >= 0
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND next_attempt_at IS NULL
            AND sent_at IS NULL
            AND superseded_at IS NULL)
        OR (status = 'sending'
            AND attempts >= 1
            AND claimed_at IS NOT NULL
            AND claim_expires_at IS NOT NULL
            AND claimed_by IS NOT NULL
            AND claim_expires_at > claimed_at
            AND next_attempt_at IS NULL
            AND sent_at IS NULL
            AND superseded_at IS NULL)
        OR (status = 'retry_waiting'
            AND attempts < max_attempts
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND next_attempt_at IS NOT NULL
            AND failure_category = 'retryable'
            AND sent_at IS NULL
            AND superseded_at IS NULL)
        OR (status = 'sent'
            AND attempts >= 1
            AND last_sent_state = desired_state
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND next_attempt_at IS NULL
            AND sent_at IS NOT NULL
            AND superseded_at IS NULL)
        OR (status = 'failed_permanent'
            AND attempts >= 1
            AND failure_category = 'permanent'
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND next_attempt_at IS NULL
            AND sent_at IS NULL
            AND superseded_at IS NULL)
        OR (status = 'failed_exhausted'
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND failure_category = 'retryable'
            AND attempts = max_attempts
            AND sent_at IS NULL
            AND superseded_at IS NULL)
        OR (status = 'superseded'
            AND claimed_at IS NULL
            AND claim_expires_at IS NULL
            AND claimed_by IS NULL
            AND next_attempt_at IS NULL
            AND sent_at IS NULL
            AND superseded_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_scm_status_deliveries_retry_waiting_next_attempt_at
    ON scm_status_deliveries (next_attempt_at)
    WHERE status = 'retry_waiting';

CREATE INDEX IF NOT EXISTS idx_scm_status_deliveries_sending_claim_expires_at
    ON scm_status_deliveries (claim_expires_at)
    WHERE status = 'sending';

CREATE INDEX IF NOT EXISTS idx_scm_status_deliveries_repo_sha_context_build
    ON scm_status_deliveries (provider, repository_owner, repository_name, commit_sha, context_name, build_id);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 00038 is intentionally irreversible: scm_status_deliveries adds a durable claimable SCM delivery ledger with richer retry and supersession states that cannot be safely collapsed automatically';
END $$;
-- +goose StatementEnd