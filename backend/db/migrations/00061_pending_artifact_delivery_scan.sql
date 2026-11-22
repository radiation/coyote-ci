-- +goose Up
CREATE INDEX idx_artifact_delivery_pending ON artifact_trigger_deliveries (id)
WHERE atomic_creation AND status = 'pending' AND queued_build_id IS NULL;

-- +goose Down
DROP INDEX idx_artifact_delivery_pending;