package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestHostWorkspaceMaterializer_PrepareWorkspace_CreatesWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-1"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		canonicalRoot = root
	}
	expected := filepath.Join(canonicalRoot, "build-1")
	if workspacePath != expected {
		t.Fatalf("expected workspace %q, got %q", expected, workspacePath)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected workspace path to exist, got error %v", err)
	}
}

func TestHostWorkspaceMaterializer_PrepareWorkspace_ReusesExistingDirectory(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)

	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-2"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	contentPath := filepath.Join(workspacePath, "README.md")
	if writeErr := os.WriteFile(contentPath, []byte("ok"), 0o644); writeErr != nil {
		t.Fatalf("write workspace content: %v", writeErr)
	}

	workspacePathAgain, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-2", RepoURL: "https://example.com/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("prepare workspace failed on reuse: %v", err)
	}
	if workspacePathAgain != workspacePath {
		t.Fatalf("expected same workspace path %q, got %q", workspacePath, workspacePathAgain)
	}
	if _, statErr := os.Stat(contentPath); statErr != nil {
		t.Fatalf("expected workspace content to remain on reuse, got %v", statErr)
	}
}

func TestHostWorkspaceMaterializer_CleanupWorkspace(t *testing.T) {
	root := t.TempDir()
	m := NewHostWorkspaceMaterializer(root)
	workspacePath := filepath.Join(root, "build-3")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	if err := m.CleanupWorkspace(context.Background(), "build-3"); err != nil {
		t.Fatalf("cleanup workspace failed: %v", err)
	}

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace to be removed, stat err=%v", err)
	}
}

func TestHostWorkspaceMaterializer_PrepareWorkspace_ReturnsCanonicalWorkspacePath(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "real-root")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("creating root symlink: %v", err)
	}

	m := NewHostWorkspaceMaterializer(symlinkRoot)
	workspacePath, err := m.PrepareWorkspace(context.Background(), WorkspacePrepareRequest{BuildID: "build-canonical"})
	if err != nil {
		t.Fatalf("prepare workspace failed: %v", err)
	}

	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatalf("eval real root symlink: %v", err)
	}
	expected := filepath.Join(canonicalRoot, "build-canonical")
	if workspacePath != expected {
		t.Fatalf("expected canonical workspace %q, got %q", expected, workspacePath)
	}
}

func TestHostWorkspaceMaterializer_MaterializeReusesLinearPredecessorWorkspace(t *testing.T) {
	revisionStore := &workspaceRevisionStoreStub{}
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), &workspaceRevisionRepositoryStub{}, revisionStore)
	ctx := context.Background()

	sourceWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "generate",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize source workspace: %v", err)
	}
	marker := filepath.Join(sourceWorkspace.Path, "generated.txt")
	if writeErr := os.WriteFile(marker, []byte("generated"), 0o644); writeErr != nil {
		t.Fatalf("write source workspace marker: %v", writeErr)
	}
	if commitErr := materializer.Commit(ctx, sourceWorkspace, "claim-generate"); commitErr != nil {
		t.Fatalf("commit source workspace: %v", commitErr)
	}

	predecessorWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "consume",
		Input: domain.WorkspaceInputPlan{
			Mode:           domain.WorkspaceInputModePredecessor,
			ProducerNodeID: "generate",
		},
	})
	if err != nil {
		t.Fatalf("materialize predecessor workspace: %v", err)
	}
	if predecessorWorkspace.Path != sourceWorkspace.Path {
		t.Fatalf("expected predecessor to reuse %q, got %q", sourceWorkspace.Path, predecessorWorkspace.Path)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("expected predecessor to retain source workspace contents: %v", statErr)
	}
	if revisionStore.restoreCalls != 0 {
		t.Fatalf("expected local predecessor workspace to skip restore, got %d calls", revisionStore.restoreCalls)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRestoresMissingLinearPredecessorWorkspace(t *testing.T) {
	revisionStore := workspace.NewFilesystemWorkspaceRevisionStore(t.TempDir())
	predecessorRoot := t.TempDir()
	marker := filepath.Join(predecessorRoot, "generated.txt")
	if writeErr := os.WriteFile(marker, []byte("generated"), 0o644); writeErr != nil {
		t.Fatalf("write predecessor marker: %v", writeErr)
	}
	publication, publishErr := revisionStore.Publish(context.Background(), "revision-1", predecessorRoot)
	if publishErr != nil {
		t.Fatalf("publish predecessor workspace: %v", publishErr)
	}
	revisionRepo := &workspaceRevisionRepositoryStub{revision: publishedWorkspaceRevision("build-1", "generate", publication)}
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), revisionRepo, revisionStore)

	materialized, materializeErr := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "consume",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "generate"},
	})
	if materializeErr != nil {
		t.Fatalf("restore predecessor workspace: %v", materializeErr)
	}
	if revisionRepo.lookupCalls != 1 {
		t.Fatalf("expected one revision lookup, got %d", revisionRepo.lookupCalls)
	}
	contents, readErr := os.ReadFile(filepath.Join(materialized.Path, "generated.txt"))
	if readErr != nil || string(contents) != "generated" {
		t.Fatalf("expected restored predecessor contents, contents=%q err=%v", contents, readErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRejectsExistingMismatchedPredecessorWorkspace(t *testing.T) {
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), &workspaceRevisionRepositoryStub{}, &workspaceRevisionStoreStub{})
	workspace, materializeErr := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "compile",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if materializeErr != nil {
		t.Fatalf("materialize source workspace: %v", materializeErr)
	}
	if commitErr := materializer.Commit(context.Background(), workspace, "claim-compile"); commitErr != nil {
		t.Fatalf("commit source workspace: %v", commitErr)
	}

	_, materializeErr = materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "consume",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "generate"},
	})
	if !errors.Is(materializeErr, ErrWorkspaceLineageUnavailable) {
		t.Fatalf("expected mismatched local lineage error, got %v", materializeErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeMissingRevisionFails(t *testing.T) {
	revisionRepo := &workspaceRevisionRepositoryStub{lookupErr: repository.ErrWorkspaceRevisionNotFound}
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), revisionRepo, &workspaceRevisionStoreStub{})

	_, materializeErr := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "generate"},
	})
	if !errors.Is(materializeErr, repository.ErrWorkspaceRevisionNotFound) {
		t.Fatalf("expected missing revision error, got %v", materializeErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRestoreFailureDoesNotCreateWorkspace(t *testing.T) {
	revisionRepo := &workspaceRevisionRepositoryStub{revision: publishedWorkspaceRevision("build-1", "generate", domain.WorkspaceRevisionPublication{ContentDigest: "sha256:abc", StorageKey: "workspace-revisions/revision-1.tar.gz"})}
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), revisionRepo, &workspaceRevisionStoreStub{restoreErr: errors.New("archive unavailable")})

	_, materializeErr := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "generate"},
	})
	if materializeErr == nil || !strings.Contains(materializeErr.Error(), "archive unavailable") {
		t.Fatalf("expected restore failure, got %v", materializeErr)
	}
	if _, statErr := os.Stat(filepath.Join(materializer.WorkspaceRoot(), "build-1")); !os.IsNotExist(statErr) {
		t.Fatalf("expected restore failure not to create workspace, stat err=%v", statErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRetainsLegacyDirectoryForFanOutAndFanIn(t *testing.T) {
	revisionStore := &workspaceRevisionStoreStub{}
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), &workspaceRevisionRepositoryStub{}, revisionStore)
	ctx := context.Background()

	sourceWorkspace, err := materializer.Materialize(ctx, MaterializeWorkspaceRequest{
		BuildID: "build-1",
		NodeID:  "compile",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize source workspace: %v", err)
	}

	for _, testCase := range []struct {
		nodeID string
		input  domain.WorkspaceInputPlan
	}{
		{
			nodeID: "fan-out-b",
			input: domain.WorkspaceInputPlan{
				Mode:                       domain.WorkspaceInputModePredecessor,
				ProducerNodeID:             "compile",
				IsolatedWritableDescendant: true,
			},
		},
		{
			nodeID: "fan-out-c",
			input: domain.WorkspaceInputPlan{
				Mode:                       domain.WorkspaceInputModePredecessor,
				ProducerNodeID:             "compile",
				IsolatedWritableDescendant: true,
			},
		},
		{
			nodeID: "fan-in",
			input: domain.WorkspaceInputPlan{
				Mode:                 domain.WorkspaceInputModeFanIn,
				CommonAncestorNodeID: "compile",
			},
		},
	} {
		workspace, materializeErr := materializer.Materialize(ctx, MaterializeWorkspaceRequest{BuildID: "build-1", NodeID: testCase.nodeID, Input: testCase.input})
		if materializeErr != nil {
			t.Fatalf("materialize legacy-compatible input %#v: %v", testCase.input, materializeErr)
		}
		if workspace.Path != sourceWorkspace.Path {
			t.Fatalf("expected legacy-compatible input %#v to reuse %q, got %q", testCase.input, sourceWorkspace.Path, workspace.Path)
		}
		if commitErr := materializer.Commit(ctx, workspace, "claim-"+testCase.nodeID); commitErr != nil {
			t.Fatalf("commit legacy-compatible input %#v: %v", testCase.input, commitErr)
		}
	}
	if revisionStore.restoreCalls != 0 {
		t.Fatalf("expected configured local fan-out and fan-in to skip restore, got %d calls", revisionStore.restoreCalls)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRejectsPortableFanOutAndFanIn(t *testing.T) {
	materializer := NewHostWorkspaceMaterializerWithRevisionStore(t.TempDir(), &workspaceRevisionRepositoryStub{}, &workspaceRevisionStoreStub{})
	for _, testCase := range []struct {
		input   domain.WorkspaceInputPlan
		wantErr error
	}{
		{input: domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "compile", IsolatedWritableDescendant: true}, wantErr: ErrWorkspaceFanOutUnsupported},
		{input: domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeFanIn, CommonAncestorNodeID: "compile"}, wantErr: ErrWorkspaceFanInUnsupported},
	} {
		_, materializeErr := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{BuildID: "build-1", Input: testCase.input})
		if !errors.Is(materializeErr, testCase.wantErr) {
			t.Fatalf("expected %v for %#v, got %v", testCase.wantErr, testCase.input, materializeErr)
		}
	}
}

func TestHostWorkspaceMaterializer_CommitAndRelease(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	workspace, err := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource},
	})
	if err != nil {
		t.Fatalf("materialize workspace: %v", err)
	}
	if commitErr := materializer.Commit(context.Background(), workspace, "claim-1"); commitErr != nil {
		t.Fatalf("commit workspace: %v", commitErr)
	}
	if releaseErr := materializer.Release(context.Background(), workspace); releaseErr != nil {
		t.Fatalf("release workspace: %v", releaseErr)
	}
	if _, statErr := os.Stat(workspace.Path); !os.IsNotExist(statErr) {
		t.Fatalf("expected released workspace to be removed, stat err=%v", statErr)
	}
}

func TestHostWorkspaceMaterializer_MaterializeRejectsUnknownInput(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	_, err := materializer.Materialize(context.Background(), MaterializeWorkspaceRequest{
		BuildID: "build-1",
		Input:   domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputMode("unknown")},
	})
	if !errors.Is(err, ErrWorkspaceInputUnsupported) {
		t.Fatalf("expected unsupported input error, got %v", err)
	}
}

func TestHostWorkspaceMaterializer_CommitRequiresMaterializedWorkspace(t *testing.T) {
	materializer := NewHostWorkspaceMaterializer(t.TempDir())
	err := materializer.Commit(context.Background(), MaterializedWorkspace{}, "claim-1")
	if err == nil {
		t.Fatal("expected incomplete materialized workspace to be rejected")
	}
}

func TestHostWorkspaceMaterializer_WorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	materializer := NewHostWorkspaceMaterializer(root)
	if got := materializer.WorkspaceRoot(); got == "" {
		t.Fatal("expected workspace root")
	}
}

type workspaceRevisionRepositoryStub struct {
	revision    domain.WorkspaceRevision
	lookupErr   error
	lookupCalls int
}

func (s *workspaceRevisionRepositoryStub) CreatePublishing(context.Context, domain.WorkspaceRevision) (domain.WorkspaceRevision, error) {
	return domain.WorkspaceRevision{}, errors.New("unexpected CreatePublishing call")
}

func (s *workspaceRevisionRepositoryStub) MarkPublishedIfClaimed(context.Context, string, string, domain.WorkspaceRevisionPublication, time.Time) (domain.WorkspaceRevision, error) {
	return domain.WorkspaceRevision{}, errors.New("unexpected MarkPublishedIfClaimed call")
}

func (s *workspaceRevisionRepositoryStub) GetByProducingExecutionJob(context.Context, string) (domain.WorkspaceRevision, error) {
	return domain.WorkspaceRevision{}, errors.New("unexpected GetByProducingExecutionJob call")
}

func (s *workspaceRevisionRepositoryStub) GetPublishedByBuildNode(context.Context, string, string) (domain.WorkspaceRevision, error) {
	s.lookupCalls++
	return s.revision, s.lookupErr
}

func (s *workspaceRevisionRepositoryStub) MarkDeleted(context.Context, string, time.Time) (domain.WorkspaceRevision, error) {
	return domain.WorkspaceRevision{}, errors.New("unexpected MarkDeleted call")
}

type workspaceRevisionStoreStub struct {
	restoreErr   error
	restoreCalls int
}

func (s *workspaceRevisionStoreStub) Publish(context.Context, string, string) (domain.WorkspaceRevisionPublication, error) {
	return domain.WorkspaceRevisionPublication{}, errors.New("unexpected Publish call")
}

func (s *workspaceRevisionStoreStub) Restore(context.Context, domain.WorkspaceRevisionPublication, string) error {
	s.restoreCalls++
	return s.restoreErr
}

func (s *workspaceRevisionStoreStub) Delete(context.Context, domain.WorkspaceRevisionPublication) error {
	return errors.New("unexpected Delete call")
}

func publishedWorkspaceRevision(buildID string, nodeID string, publication domain.WorkspaceRevisionPublication) domain.WorkspaceRevision {
	return domain.WorkspaceRevision{
		BuildID:       buildID,
		NodeID:        nodeID,
		Status:        domain.WorkspaceRevisionStatusPublished,
		ContentDigest: &publication.ContentDigest,
		StorageKey:    &publication.StorageKey,
		SizeBytes:     publication.SizeBytes,
	}
}
