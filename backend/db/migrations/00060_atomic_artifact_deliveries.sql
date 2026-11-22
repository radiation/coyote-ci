-- +goose Up
ALTER TABLE artifact_trigger_deliveries ADD COLUMN atomic_creation boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE artifact_trigger_deliveries DROP COLUMN atomic_creation;