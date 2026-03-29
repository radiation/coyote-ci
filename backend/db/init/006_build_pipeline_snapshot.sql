ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS pipeline_config_yaml TEXT,
    ADD COLUMN IF NOT EXISTS pipeline_name TEXT,
    ADD COLUMN IF NOT EXISTS pipeline_source TEXT;
