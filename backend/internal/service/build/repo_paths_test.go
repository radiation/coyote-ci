package build

import (
	"path/filepath"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

func TestResolveRepoPipelinePath(t *testing.T) {
	repoRoot := t.TempDir()

	tests := []struct {
		name          string
		requestedPath string
		wantEffective string
		wantErr       bool
	}{
		{name: "default path", requestedPath: "", wantEffective: ".coyote/pipeline.yml"},
		{name: "custom relative path", requestedPath: " ci/coyote.yml ", wantEffective: "ci/coyote.yml"},
		{name: "absolute path rejected", requestedPath: filepath.Join(repoRoot, "pipeline.yml"), wantErr: true},
		{name: "parent traversal rejected", requestedPath: "../pipeline.yml", wantErr: true},
		{name: "parent directory rejected", requestedPath: "..", wantErr: true},
		{name: "empty after clean rejected", requestedPath: ".", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			absPath, effectivePath, err := resolveRepoPipelinePath(repoRoot, tc.requestedPath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if effectivePath != tc.wantEffective {
				t.Fatalf("expected effective path %q, got %q", tc.wantEffective, effectivePath)
			}
			if absPath != filepath.Join(repoRoot, filepath.FromSlash(tc.wantEffective)) {
				t.Fatalf("expected abs path below repo root, got %q", absPath)
			}
		})
	}
}

func TestResolveRepoStepWorkingDirs(t *testing.T) {
	tests := []struct {
		name           string
		pipelinePath   string
		workingDir     string
		wantWorkingDir string
		wantErr        bool
	}{
		{name: "defaults to pipeline directory", pipelinePath: "ci/pipeline.yml", wantWorkingDir: "ci"},
		{name: "relative working dir below pipeline directory", pipelinePath: "ci/pipeline.yml", workingDir: "scripts", wantWorkingDir: "ci/scripts"},
		{name: "root pipeline defaults to dot", pipelinePath: "pipeline.yml", wantWorkingDir: "."},
		{name: "dot working dir keeps pipeline directory", pipelinePath: "ci/pipeline.yml", workingDir: ".", wantWorkingDir: "ci"},
		{name: "backslash working dir normalized", pipelinePath: "ci/pipeline.yml", workingDir: "scripts\\linux", wantWorkingDir: "ci/scripts/linux"},
		{name: "absolute working dir rejected", pipelinePath: "ci/pipeline.yml", workingDir: "/tmp", wantErr: true},
		{name: "escaping working dir rejected", pipelinePath: "ci/pipeline.yml", workingDir: "../outside", wantErr: true},
		{name: "combined path escape rejected", pipelinePath: "ci/pipeline.yml", workingDir: "../../outside", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved := &pipeline.ResolvedPipeline{Steps: []pipeline.ResolvedStep{{Name: "test", WorkingDir: tc.workingDir}}}
			got, err := resolveRepoStepWorkingDirs(tc.pipelinePath, resolved)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Steps[0].WorkingDir != tc.wantWorkingDir {
				t.Fatalf("expected working dir %q, got %q", tc.wantWorkingDir, got.Steps[0].WorkingDir)
			}
		})
	}
}

func TestResolveRepoStepWorkingDirs_NilPipeline(t *testing.T) {
	if _, err := resolveRepoStepWorkingDirs("ci/pipeline.yml", nil); err == nil {
		t.Fatal("expected nil pipeline error")
	}
}
