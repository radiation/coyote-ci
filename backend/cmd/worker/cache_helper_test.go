package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCacheMountPathsCreatesAllPresetPaths(t *testing.T) {
	root := t.TempDir()
	if err := ensureCacheMountPaths(root, "go", "."); err != nil {
		t.Fatalf("ensure cache mount paths: %v", err)
	}
	for _, path := range []string{"paths/000", "paths/001"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil || !info.IsDir() {
			t.Fatalf("cache path %s: info=%v err=%v", path, info, err)
		}
	}
}

func TestEnsureCacheMountPathsRejectsUnsupportedPreset(t *testing.T) {
	if err := ensureCacheMountPaths(t.TempDir(), "unknown", "."); err == nil {
		t.Fatal("expected unsupported cache preset error")
	}
}

func TestRunCacheSaveAfterBuildTreatsObservationFailuresAsSideEffects(t *testing.T) {
	t.Setenv(workspaceHelperPodName, "pod")
	t.Setenv(workspaceHelperNamespace, "ci")
	t.Setenv(workspaceHelperPodUID, "pod-uid")
	originalClient := newWorkspacePublishPodClient
	t.Cleanup(func() { newWorkspacePublishPodClient = originalClient })
	newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
		return nil, errors.New("Kubernetes API unavailable")
	}
	if err := runCacheSaveAfterBuild(context.Background()); err != nil {
		t.Fatalf("cache save observation error must be non-fatal: %v", err)
	}
}

func TestRunCacheSaveAfterBuildTreatsMissingPodIdentityAsSideEffect(t *testing.T) {
	t.Setenv(workspaceHelperPodName, "")
	t.Setenv(workspaceHelperNamespace, "")
	t.Setenv(workspaceHelperPodUID, "")
	if err := runCacheSaveAfterBuild(context.Background()); err != nil {
		t.Fatalf("missing cache save identity must be non-fatal: %v", err)
	}
}
