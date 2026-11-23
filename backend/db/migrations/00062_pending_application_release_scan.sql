-- +goose Up
CREATE INDEX application_releases_pending_scan_idx ON application_releases (created_at, id) WHERE status = 'pending';
DROP INDEX application_releases_pending_idx;

-- +goose Down
CREATE INDEX application_releases_pending_idx ON application_releases (created_at) WHERE status = 'pending';
DROP INDEX application_releases_pending_scan_idx;