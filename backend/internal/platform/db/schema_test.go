package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitSchemaIncludesBuildLifecycleAndSteps(t *testing.T) {
	files := []string{
		"../../../db/migrations/00001_init_schema.sql",
		"../../../db/migrations/00002_add_build_step_cache_config.sql",
		"../../../db/migrations/00003_add_cache_entries.sql",
		"../../../db/migrations/00008_add_version_tags.sql",
		"../../../db/migrations/00010_add_build_artifact_types.sql",
		"../../../db/migrations/00011_add_build_artifact_names.sql",
	}

	var builder strings.Builder
	for _, relPath := range files {
		content, err := os.ReadFile(filepath.Clean(relPath))
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", relPath, err)
		}
		builder.WriteString(string(content))
		builder.WriteString("\n")
	}

	sql := builder.String()
	required := []string{
		"current_step_index",
		"pipeline_config_yaml",
		"pipeline_name",
		"pipeline_source",
		"pipeline_path",
		"repo_url",
		"ref",
		"commit_sha",
		"CREATE TABLE IF NOT EXISTS build_steps",
		"step_index",
		"command",
		"working_dir",
		"timeout_seconds",
		"claim_token",
		"claimed_at",
		"lease_expires_at",
		"stdout",
		"stderr",
		"queued_at",
		"started_at",
		"finished_at",
		"CREATE TABLE IF NOT EXISTS build_artifacts",
		"artifact_name",
		"logical_path",
		"storage_key",
		"CREATE TABLE IF NOT EXISTS cache_entries",
		"cache_key",
		"object_key",
		"CREATE TABLE IF NOT EXISTS version_tags",
		"version_text",
		"target_type",
		"artifact_id",
		"managed_image_version_id",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("expected init schema (combined SQL) to contain %q", token)
		}
	}
}
