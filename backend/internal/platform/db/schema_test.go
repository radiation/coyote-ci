package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitSchemaIncludesBuildLifecycleAndSteps(t *testing.T) {
	tests := []string{
		"../../../db/init/001_init.sql",
		"../../../db/init/002_build_lifecycle_and_steps.sql",
	}

	for _, relPath := range tests {
		relPath := relPath
		t.Run(relPath, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Clean(relPath))
			if err != nil {
				t.Fatalf("failed to read migration %s: %v", relPath, err)
			}

			sql := string(content)
			required := []string{
				"current_step_index",
				"CREATE TABLE IF NOT EXISTS build_steps",
				"step_index",
				"queued_at",
				"started_at",
				"finished_at",
			}
			for _, token := range required {
				if !strings.Contains(sql, token) {
					t.Fatalf("expected migration %s to contain %q", relPath, token)
				}
			}
		})
	}
}
