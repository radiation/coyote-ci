-- +goose Up
CREATE INDEX idx_builds_scm_terminal_finished_at_id
    ON builds (finished_at DESC, id DESC)
    WHERE status IN ('success', 'failed', 'canceled')
      AND job_id IS NOT NULL AND registered_repository_id IS NOT NULL
      AND scm_connection_id IS NOT NULL AND provider_repository_id IS NOT NULL;

-- +goose Down
DROP INDEX idx_builds_scm_terminal_finished_at_id;