-- +goose Up

ALTER TABLE cache_entries ADD COLUMN IF NOT EXISTS content_digest TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS cache_publish_claims (
	job_id TEXT NOT NULL,
	preset TEXT NOT NULL,
	cache_key TEXT NOT NULL,
	claim_token TEXT NOT NULL,
	claimed_by TEXT NOT NULL,
	claimed_at TIMESTAMPTZ NOT NULL,
	claim_expires_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (job_id, preset, cache_key)
);

-- +goose Down

DROP TABLE IF EXISTS cache_publish_claims;
ALTER TABLE cache_entries DROP COLUMN IF EXISTS content_digest;