ALTER TABLE build_steps
    ADD COLUMN node_id TEXT,
    ADD COLUMN group_name TEXT,
    ADD COLUMN depends_on_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE build_steps
SET node_id = 'step-' || step_index::text
WHERE node_id IS NULL;

ALTER TABLE build_steps
    ALTER COLUMN node_id SET NOT NULL,
    ALTER COLUMN node_id SET DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_build_steps_build_id_node_id
    ON build_steps (build_id, node_id);

ALTER TABLE build_jobs
    ADD COLUMN node_id TEXT,
    ADD COLUMN group_name TEXT,
    ADD COLUMN depends_on_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE build_jobs AS bj
SET node_id = COALESCE(bs.node_id, 'step-' || bj.step_index::text),
    group_name = bs.group_name,
    depends_on_node_ids = COALESCE(bs.depends_on_node_ids, '[]'::jsonb)
FROM build_steps AS bs
WHERE bs.id = bj.step_id
  AND bj.node_id IS NULL;

UPDATE build_jobs
SET node_id = 'step-' || step_index::text
WHERE node_id IS NULL;

ALTER TABLE build_jobs
    ALTER COLUMN node_id SET NOT NULL,
    ALTER COLUMN node_id SET DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_build_jobs_build_id_node_id
    ON build_jobs (build_id, node_id);
