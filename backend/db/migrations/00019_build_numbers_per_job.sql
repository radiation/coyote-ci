-- +goose Up

ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS next_build_number BIGINT NOT NULL DEFAULT 1;

ALTER TABLE builds
    ALTER COLUMN build_number DROP DEFAULT;

DROP INDEX IF EXISTS idx_builds_build_number;

WITH ordered_job_builds AS (
    SELECT id,
           job_id,
           ROW_NUMBER() OVER (PARTITION BY job_id ORDER BY created_at ASC, id ASC) AS assigned_build_number
    FROM builds
    WHERE job_id IS NOT NULL
)
UPDATE builds
SET build_number = ordered_job_builds.assigned_build_number
FROM ordered_job_builds
WHERE builds.id = ordered_job_builds.id;

WITH job_next_numbers AS (
    SELECT j.id,
           COALESCE(MAX(b.build_number), 0) + 1 AS next_build_number
    FROM jobs AS j
    LEFT JOIN builds AS b ON b.job_id = j.id
    GROUP BY j.id
)
UPDATE jobs
SET next_build_number = job_next_numbers.next_build_number
FROM job_next_numbers
WHERE jobs.id = job_next_numbers.id;

SELECT setval(
    'builds_build_number_seq',
    COALESCE((SELECT MAX(build_number) FROM builds WHERE job_id IS NULL), 0) + 1,
    false
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_job_id_build_number ON builds (job_id, build_number);

CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_null_job_build_number ON builds (build_number)
    WHERE job_id IS NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_builds_null_job_build_number;
DROP INDEX IF EXISTS idx_builds_job_id_build_number;

WITH ordered_builds AS (
    SELECT id,
           ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS assigned_build_number
    FROM builds
)
UPDATE builds
SET build_number = ordered_builds.assigned_build_number
FROM ordered_builds
WHERE builds.id = ordered_builds.id;

ALTER TABLE builds
    ALTER COLUMN build_number SET DEFAULT nextval('builds_build_number_seq');

SELECT setval(
    'builds_build_number_seq',
    COALESCE((SELECT MAX(build_number) FROM builds), 0) + 1,
    false
);

ALTER TABLE jobs
    DROP COLUMN IF EXISTS next_build_number;

CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_build_number ON builds (build_number);