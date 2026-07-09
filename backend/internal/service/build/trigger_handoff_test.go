package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestReadOptionalString(t *testing.T) {
	if got := readOptionalString(nil); got != "" {
		t.Fatalf("expected empty string for nil pointer, got %q", got)
	}
	value := " value "
	if got := readOptionalString(&value); got != "value" {
		t.Fatalf("expected trimmed string, got %q", got)
	}
}

func TestPrepareTriggerArtifactHandoff_NonArtifactNoop(t *testing.T) {
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	if err := svc.prepareTriggerArtifactHandoff(context.Background(), domain.Build{Trigger: domain.BuildTrigger{Kind: domain.BuildTriggerKindManual}}); err != nil {
		t.Fatalf("expected non-artifact trigger to no-op, got %v", err)
	}
}

func TestPrepareTriggerArtifactHandoff_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		build   domain.Build
		wantErr string
	}{
		{
			name: "missing producer project",
			build: domain.Build{ProjectID: "project-1", Trigger: domain.BuildTrigger{
				Kind: domain.BuildTriggerKindArtifact,
			}},
			wantErr: "producer project id is required",
		},
		{
			name: "missing build project",
			build: domain.Build{Trigger: domain.BuildTrigger{
				Kind:              domain.BuildTriggerKindArtifact,
				ProducerProjectID: testStringPtr("project-1"),
			}},
			wantErr: "build project id is required",
		},
		{
			name: "incomplete provenance",
			build: domain.Build{ProjectID: "project-1", Trigger: domain.BuildTrigger{
				Kind:              domain.BuildTriggerKindArtifact,
				ProducerProjectID: testStringPtr("project-1"),
			}},
			wantErr: "provenance is incomplete",
		},
		{
			name: "invalid artifact path",
			build: domain.Build{ProjectID: "project-1", Trigger: domain.BuildTrigger{
				Kind:              domain.BuildTriggerKindArtifact,
				ProducerProjectID: testStringPtr("project-1"),
				ProducerBuildID:   testStringPtr("build-upstream"),
				ArtifactID:        testStringPtr("artifact-1"),
				ArtifactPath:      testStringPtr("../secret.txt"),
			}},
			wantErr: "invalid trigger artifact path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
			err := svc.prepareTriggerArtifactHandoff(context.Background(), tc.build)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestPrepareTriggerArtifactHandoff_RequiresWorkspaceRoot(t *testing.T) {
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	err := svc.prepareTriggerArtifactHandoff(context.Background(), domain.Build{
		ID:        "build-downstream",
		ProjectID: "project-1",
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: testStringPtr("project-1"),
			ProducerBuildID:   testStringPtr("build-upstream"),
			ArtifactID:        testStringPtr("artifact-1"),
			ArtifactPath:      testStringPtr("dist/app.tgz"),
		},
	})
	if !errors.Is(err, ErrExecutionWorkspaceRootNotConfigured) {
		t.Fatalf("expected workspace root error, got %v", err)
	}
}

func TestPrepareTriggerArtifactHandoff_DestinationAlreadyExists(t *testing.T) {
	workspaceRoot := t.TempDir()
	destination := filepath.Join(workspaceRoot, "build-downstream", ".coyote", "trigger-artifacts", "dist")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "app.tgz"), []byte("already"), 0o644); err != nil {
		t.Fatalf("seed existing handoff file: %v", err)
	}

	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	svc.SetExecutionWorkspaceRoot(workspaceRoot)
	err := svc.prepareTriggerArtifactHandoff(context.Background(), domain.Build{
		ID:        "build-downstream",
		ProjectID: "project-1",
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: testStringPtr("project-1"),
			ProducerBuildID:   testStringPtr("build-upstream"),
			ArtifactID:        testStringPtr("artifact-1"),
			ArtifactPath:      testStringPtr("dist/app.tgz"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing destination error, got %v", err)
	}
}

func TestPrepareTriggerArtifactHandoff_PathMismatch(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	store := artifact.NewFilesystemStore(storageRoot)
	if _, err := store.Save(ctx, "producer/dist/app.tgz", strings.NewReader("artifact-body")); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-upstream": {{
			ID:              "artifact-1",
			BuildID:         "build-upstream",
			LogicalPath:     "dist/other.tgz",
			StorageKey:      "producer/dist/app.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
		}},
	}}
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	svc.SetExecutionWorkspaceRoot(workspaceRoot)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(store), workspaceRoot)
	err := svc.prepareTriggerArtifactHandoff(ctx, domain.Build{
		ID:        "build-downstream",
		ProjectID: "project-1",
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: testStringPtr("project-1"),
			ProducerBuildID:   testStringPtr("build-upstream"),
			ArtifactID:        testStringPtr("artifact-1"),
			ArtifactPath:      testStringPtr("dist/app.tgz"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "path mismatch") {
		t.Fatalf("expected path mismatch error, got %v", err)
	}
}

func TestPrepareTriggerArtifactHandoff_UsesMetadataChecksumFallback(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	body := []byte("artifact-body")
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	store := artifact.NewFilesystemStore(storageRoot)
	if _, err := store.Save(ctx, "producer/dist/app.tgz", strings.NewReader(string(body))); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-upstream": {{
			ID:              "artifact-1",
			BuildID:         "build-upstream",
			LogicalPath:     "dist/app.tgz",
			StorageKey:      "producer/dist/app.tgz",
			StorageProvider: domain.StorageProviderFilesystem,
			ChecksumSHA256:  &checksum,
		}},
	}}
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	svc.SetExecutionWorkspaceRoot(workspaceRoot)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(store), workspaceRoot)
	err := svc.prepareTriggerArtifactHandoff(ctx, domain.Build{
		ID:        "build-downstream",
		ProjectID: "project-1",
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: testStringPtr("project-1"),
			ProducerBuildID:   testStringPtr("build-upstream"),
			ArtifactID:        testStringPtr("artifact-1"),
			ArtifactPath:      testStringPtr("dist/app.tgz"),
		},
	})
	if err != nil {
		t.Fatalf("expected checksum fallback success, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspaceRoot, "build-downstream", ".coyote", "trigger-artifacts", "dist", "app.tgz")); statErr != nil {
		t.Fatalf("expected handed off file, stat failed: %v", statErr)
	}
}

func TestCopyTriggerArtifactToWorkspace_NoChecksumSuccess(t *testing.T) {
	workspaceRoot := t.TempDir()
	destination := filepath.Join(workspaceRoot, "dist", "app.tgz")
	if err := copyTriggerArtifactToWorkspace(workspaceRoot, destination, strings.NewReader("artifact-body"), ""); err != nil {
		t.Fatalf("expected copy success without checksum, got %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(body) != "artifact-body" {
		t.Fatalf("unexpected destination body %q", string(body))
	}
}

func TestCopyTriggerArtifactToWorkspace_CopyFailure(t *testing.T) {
	workspaceRoot := t.TempDir()
	err := copyTriggerArtifactToWorkspace(workspaceRoot, filepath.Join(workspaceRoot, "dist", "app.tgz"), failingReader{err: errors.New("read failed")}, "")
	if err == nil || !strings.Contains(err.Error(), "writing trigger artifact content") {
		t.Fatalf("expected copy failure, got %v", err)
	}
}

func TestCopyTriggerArtifactToWorkspace_RejectsSymlinkTraversal(t *testing.T) {
	workspaceRoot := t.TempDir()
	outsideRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "build"), 0o755); err != nil {
		t.Fatalf("mkdir build workspace: %v", err)
	}
	linkPath := filepath.Join(workspaceRoot, "build", ".coyote")
	if err := os.Symlink(outsideRoot, linkPath); err != nil {
		t.Fatalf("create .coyote symlink: %v", err)
	}
	destination := filepath.Join(workspaceRoot, "build", ".coyote", "trigger-artifacts", "dist", "app.tgz")
	err := copyTriggerArtifactToWorkspace(filepath.Join(workspaceRoot, "build"), destination, strings.NewReader("artifact-body"), "")
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected symlink traversal rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outsideRoot, "trigger-artifacts")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no writes outside workspace, got stat err=%v", statErr)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read(_ []byte) (int, error) {
	return 0, f.err
}

func testStringPtr(value string) *string {
	return &value
}

var _ io.Reader = failingReader{}
