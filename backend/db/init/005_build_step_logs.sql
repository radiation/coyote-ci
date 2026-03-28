CREATE TABLE IF NOT EXISTS build_step_logs (
    id BIGSERIAL PRIMARY KEY,
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    step_id UUID NOT NULL REFERENCES build_steps(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    sequence_no BIGINT GENERATED ALWAYS AS IDENTITY,
    stream TEXT NOT NULL,
    chunk_text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (build_id, step_index, sequence_no)
);

CREATE INDEX IF NOT EXISTS idx_build_step_logs_step_sequence
    ON build_step_logs (build_id, step_index, sequence_no);

CREATE INDEX IF NOT EXISTS idx_build_step_logs_build
    ON build_step_logs (build_id, sequence_no);
