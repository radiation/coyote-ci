-- +goose Up

CREATE SEQUENCE IF NOT EXISTS builds_build_number_seq;

ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS build_number BIGINT;

ALTER TABLE builds
    ALTER COLUMN build_number SET DEFAULT nextval('builds_build_number_seq');

WITH ordered_builds AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS assigned_build_number
    FROM builds
    WHERE build_number IS NULL
)
UPDATE builds
SET build_number = ordered_builds.assigned_build_number
FROM ordered_builds
WHERE builds.id = ordered_builds.id;

SELECT setval(
    'builds_build_number_seq',
    COALESCE((SELECT MAX(build_number) FROM builds), 0) + 1,
    false
);

ALTER TABLE builds
    ALTER COLUMN build_number SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_build_number ON builds (build_number);