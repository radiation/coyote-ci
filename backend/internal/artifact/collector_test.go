package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestCollector_CollectsMatchingFiles(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "dist", "app"), []byte("binary"))
	mustWriteFile(t, filepath.Join(workspace, "reports", "junit.xml"), []byte("xml"))
	mustWriteFile(t, filepath.Join(workspace, "notes", "skip.txt"), []byte("skip"))

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**", "reports/*.xml", "missing/*.txt"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result.Artifacts))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for unmatched pattern, got %d", len(result.Warnings))
	}

	for _, artifact := range result.Artifacts {
		if artifact.SizeBytes <= 0 {
			t.Fatalf("expected positive size for artifact %q", artifact.LogicalPath)
		}
		if artifact.ChecksumSHA256 == nil || *artifact.ChecksumSHA256 == "" {
			t.Fatalf("expected checksum for artifact %q", artifact.LogicalPath)
		}
	}
}

func TestCollector_MissingWorkspaceDoesNotFail(t *testing.T) {
	collector := NewCollector(NewFilesystemStore(t.TempDir()))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: filepath.Join(t.TempDir(), "missing"),
		Patterns:      []string{"dist/**"},
	})
	if err != nil {
		t.Fatalf("expected nil error for missing workspace, got %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %d", len(result.Artifacts))
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning when workspace is missing")
	}
}

func TestCollector_RejectsSymlinkArtifacts(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()
	outsideRoot := t.TempDir()

	outsideFile := filepath.Join(outsideRoot, "secret.txt")
	mustWriteFile(t, outsideFile, []byte("do-not-collect"))

	if err := os.MkdirAll(filepath.Join(workspace, "dist"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	symlinkPath := filepath.Join(workspace, "dist", "secret.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**"},
	})
	if err == nil {
		t.Fatal("expected symlink artifact to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("expected symlink-related error, got %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("expected no persisted artifacts, got %d", len(result.Artifacts))
	}
}

func TestCollector_CollectResultArtifacts_AreSortedByLogicalPath(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "reports", "z.xml"), []byte("z"))
	mustWriteFile(t, filepath.Join(workspace, "dist", "a.txt"), []byte("a"))
	mustWriteFile(t, filepath.Join(workspace, "dist", "m.txt"), []byte("m"))

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**", "reports/*.xml"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(result.Artifacts))
	}

	got := []string{
		result.Artifacts[0].LogicalPath,
		result.Artifacts[1].LogicalPath,
		result.Artifacts[2].LogicalPath,
	}
	want := []string{"dist/a.txt", "dist/m.txt", "reports/z.xml"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected sorted artifacts %v, got %v", want, got)
		}
	}
}

func TestCollector_RejectsAbsoluteArtifactPath(t *testing.T) {
	workspace := t.TempDir()
	collector := NewCollector(NewFilesystemStore(t.TempDir()))

	_, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"/etc/passwd"},
	})
	if err == nil {
		t.Fatal("expected absolute artifact path to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid artifact path") {
		t.Fatalf("expected invalid artifact path error, got %v", err)
	}
}

func TestCollector_RejectsTraversalArtifactPath(t *testing.T) {
	workspace := t.TempDir()
	collector := NewCollector(NewFilesystemStore(t.TempDir()))

	_, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"../secrets/*"},
	})
	if err == nil {
		t.Fatal("expected traversal artifact path to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid artifact path") {
		t.Fatalf("expected invalid artifact path error, got %v", err)
	}
}

func TestCollector_NoOpAndWorkspaceValidationBranches(t *testing.T) {
	if _, err := NewCollector(nil).Collect(context.Background(), CollectRequest{BuildID: "build-1", WorkspacePath: t.TempDir(), Patterns: []string{"dist/**"}}); err == nil {
		t.Fatal("expected nil store error")
	}

	collector := NewCollector(NewFilesystemStore(t.TempDir()))
	for _, request := range []CollectRequest{
		{BuildID: " ", WorkspacePath: t.TempDir(), Patterns: []string{"dist/**"}},
		{BuildID: "build-1", WorkspacePath: " ", Patterns: []string{"dist/**"}},
		{BuildID: "build-1", WorkspacePath: t.TempDir()},
		{BuildID: "build-1", WorkspacePath: t.TempDir(), Patterns: []string{" ", ""}},
	} {
		result, err := collector.Collect(context.Background(), request)
		if err != nil {
			t.Fatalf("expected no-op request to succeed, got %v", err)
		}
		if len(result.Artifacts) != 0 || len(result.Warnings) != 0 {
			t.Fatalf("expected empty no-op result, got %+v", result)
		}
	}

	workspaceFile := filepath.Join(t.TempDir(), "workspace-file")
	if err := os.WriteFile(workspaceFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	_, err := collector.Collect(context.Background(), CollectRequest{BuildID: "build-1", WorkspacePath: workspaceFile, Patterns: []string{"dist/**"}})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected workspace not-directory error, got %v", err)
	}
}

func TestCollector_SkipLogicalPathsAndStoreFailures(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "dist", "skip.txt"), []byte("skip"))
	mustWriteFile(t, filepath.Join(workspace, "dist", "keep.txt"), []byte("keep"))

	collector := NewCollector(NewFilesystemStore(t.TempDir()))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:          "build-1",
		WorkspacePath:    workspace,
		Patterns:         []string{"dist/*.txt", "dist/*.txt"},
		SkipLogicalPaths: map[string]struct{}{"dist/skip.txt": {}},
	})
	if err != nil {
		t.Fatalf("collect with skip paths failed: %v", err)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].LogicalPath != "dist/keep.txt" {
		t.Fatalf("expected only keep artifact, got %+v", result.Artifacts)
	}

	saveErr := errors.New("store offline")
	failingCollector := NewCollector(&failingArtifactStore{saveErr: saveErr})
	result, err = failingCollector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/*.txt"},
	})
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected joined save error, got %v", err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("expected no collected artifacts on store failure, got %+v", result.Artifacts)
	}
}

func TestCollectorPatternHelpers(t *testing.T) {
	workspace := workspaceForCollectorTest(t)
	patterns, err := normalizePatterns([]string{" dist/** ", "dist/**", "reports/*.xml", ""}, workspace)
	if err != nil {
		t.Fatalf("normalize patterns failed: %v", err)
	}
	want := []string{"dist/**", "reports/*.xml"}
	for i := range want {
		if patterns[i] != want[i] {
			t.Fatalf("patterns[%d]: expected %q, got %q", i, want[i], patterns[i])
		}
	}

	matches := map[string]bool{
		"dist/**|dist/app.bin":        true,
		"dist/**|dist/nested/app.bin": true,
		"reports/*.xml|reports/a.xml": true,
		"reports/*.xml|reports/a.txt": false,
		"dist/[|dist/app.bin":         false,
		"dist/**/app|dist/app":        true,
	}
	for key, wantMatch := range matches {
		parts := strings.Split(key, "|")
		if got := matchPathPattern(parts[0], parts[1]); got != wantMatch {
			t.Fatalf("matchPathPattern(%q, %q): expected %v, got %v", parts[0], parts[1], wantMatch, got)
		}
	}

	if pathWithinBase(t.TempDir(), filepath.Join(t.TempDir(), "other")) {
		t.Fatal("expected unrelated paths not to be within base")
	}
}

type failingArtifactStore struct {
	saveErr error
}

func (s *failingArtifactStore) Save(context.Context, string, io.Reader) (int64, error) {
	return 0, s.saveErr
}

func (s *failingArtifactStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func workspaceForCollectorTest(t *testing.T) workspace.Workspace {
	t.Helper()
	return workspace.New("build-1", t.TempDir())
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func TestBuildStorageKey_StepScoped(t *testing.T) {
	key := buildStorageKey("build-1", "step-1", "uuid-abc", "report.xml")
	expected := "builds/build-1/steps/step-1/uuid-abc-report.xml"
	if key != expected {
		t.Fatalf("expected %q, got %q", expected, key)
	}
}

func TestBuildStorageKey_Shared(t *testing.T) {
	key := buildStorageKey("build-1", "", "uuid-abc", "report.xml")
	expected := "builds/build-1/shared/uuid-abc-report.xml"
	if key != expected {
		t.Fatalf("expected %q, got %q", expected, key)
	}
}

func TestCollector_StepScoped_SetsGeneratedIDAndStepID(t *testing.T) {
	workspace := t.TempDir()
	storeRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(workspace, "dist", "app"), []byte("binary"))

	collector := NewCollector(NewFilesystemStore(storeRoot))
	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		StepID:        "step-42",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(result.Artifacts))
	}

	a := result.Artifacts[0]
	if a.StepID != "step-42" {
		t.Fatalf("expected step_id step-42, got %q", a.StepID)
	}
	if a.GeneratedID == "" {
		t.Fatal("expected non-empty generated ID")
	}
	if !strings.Contains(a.StorageKey, "steps/step-42/") {
		t.Fatalf("expected step-scoped storage key, got %q", a.StorageKey)
	}
}

func TestNewGCSStore_RequiresBucket(t *testing.T) {
	_, err := NewGCSStore(nil, GCSStoreConfig{Bucket: ""})
	if err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("expected bucket-related error, got %v", err)
	}
}

type keyResolvingRecordingStore struct {
	events *[]string
	prefix string
}

func (s *keyResolvingRecordingStore) ResolveStorageKey(key string) string {
	trimmedPrefix := strings.TrimSpace(s.prefix)
	if trimmedPrefix == "" {
		return key
	}
	return trimmedPrefix + "/" + key
}

func (s *keyResolvingRecordingStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	body, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	*s.events = append(*s.events, "save:"+key)
	return int64(len(body)), nil
}

func (s *keyResolvingRecordingStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestCollector_Collect_UsesResolvedStorageKeyWhenStoreSupportsIt(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "dist", "app"), []byte("binary"))

	events := make([]string, 0)
	store := &keyResolvingRecordingStore{events: &events, prefix: "artifacts-prefix"}
	collector := NewCollector(store)

	result, err := collector.Collect(context.Background(), CollectRequest{
		BuildID:       "build-1",
		StepID:        "step-1",
		WorkspacePath: workspace,
		Patterns:      []string{"dist/**"},
	})
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(result.Artifacts))
	}

	storageKey := result.Artifacts[0].StorageKey
	if !strings.HasPrefix(storageKey, "artifacts-prefix/builds/build-1/steps/step-1/") {
		t.Fatalf("expected resolved storage key with prefix, got %q", storageKey)
	}
	if len(events) != 1 || !strings.HasPrefix(events[0], "save:"+storageKey) {
		t.Fatalf("expected save event with resolved storage key, got %v", events)
	}
}
