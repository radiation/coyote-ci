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
		"../../../db/migrations/00017_add_artifact_packages.sql",
		"../../../db/migrations/00020_add_notification_deliveries.sql",
		"../../../db/migrations/00021_add_notification_targets_and_subscriptions.sql",
		"../../../db/migrations/00023_expand_notification_targets_for_slack_webhooks.sql",
		"../../../db/migrations/00024_add_notification_target_owner_user.sql",
		"../../../db/migrations/00028_add_slack_workspace_integrations.sql",
		"../../../db/migrations/00029_rename_slack_workspace_bot_user_id_to_bot_id.sql",
		"../../../db/migrations/00030_add_user_slack_identities.sql",
		"../../../db/migrations/00032_refactor_notification_delivery_identity.sql",
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
		"CREATE TABLE IF NOT EXISTS builds (\n    id UUID PRIMARY KEY,\n    project_id TEXT NOT NULL,",
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
		"CREATE TABLE IF NOT EXISTS artifact_packages (\n    id UUID PRIMARY KEY,\n    project_id TEXT NOT NULL,",
		"CREATE TABLE IF NOT EXISTS artifact_packages",
		"package_id",
		"CREATE TABLE IF NOT EXISTS artifact_versions",
		"CREATE TABLE IF NOT EXISTS artifact_channels",
		"CREATE TABLE IF NOT EXISTS notification_deliveries",
		"notification_deliveries_build_event_transport_destination_key_key",
		"idx_notification_deliveries_build_id",
		"CREATE TABLE IF NOT EXISTS notification_targets",
		"origin TEXT",
		"notification_targets_config_default_email_recipient_key",
		"CREATE TABLE IF NOT EXISTS slack_workspace_integrations",
		"CREATE TABLE IF NOT EXISTS user_slack_identities",
		"CREATE TABLE IF NOT EXISTS notification_subscriptions",
		"notification_subscriptions_target_event_project_key",
		"notification_subscriptions_target_event_job_key",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("expected init schema (combined SQL) to contain %q", token)
		}
	}
}
