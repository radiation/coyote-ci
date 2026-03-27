package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitSchemaIncludesBuildLifecycleAndSteps(t *testing.T) {
	files := []string{
		"../../../db/init/001_init.sql",
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
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("expected init schema (combined SQL) to contain %q", token)
		}
	}
}
