package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestGCSWorkspaceRevisionStorePublishOpenRestoreDeleteAcrossInstances(t *testing.T) {
	objects := &workspaceRevisionObjectStoreFake{objects: map[string][]byte{}}
	store, storeErr := newGCSWorkspaceRevisionStore(objects, "coyote/revisions")
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}
	otherStore, otherStoreErr := newGCSWorkspaceRevisionStore(objects, "coyote/revisions")
	if otherStoreErr != nil {
		t.Fatalf("create second store: %v", otherStoreErr)
	}
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "output.txt"), []byte("workspace"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}

	publication, publishErr := store.Publish(context.Background(), "revision-1", sourceRoot)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	if publication.StorageProvider != domain.StorageProviderGCS || publication.StorageKey != "workspace-revisions/revision-1.tar.gz" {
		t.Fatalf("publication=%#v", publication)
	}
	repeated, repeatErr := otherStore.Publish(context.Background(), "revision-1", sourceRoot)
	if repeatErr != nil || repeated.ContentDigest != publication.ContentDigest || repeated.StorageKey != publication.StorageKey || repeated.StorageProvider != publication.StorageProvider || repeated.SizeBytes == nil || publication.SizeBytes == nil || *repeated.SizeBytes != *publication.SizeBytes {
		t.Fatalf("repeat publication=%#v err=%v", repeated, repeatErr)
	}

	archive, openErr := otherStore.Open(context.Background(), publication)
	if openErr != nil {
		t.Fatalf("open from second store: %v", openErr)
	}
	archiveBytes, readErr := io.ReadAll(archive)
	closeErr := archive.Close()
	if readErr != nil || closeErr != nil || len(archiveBytes) == 0 {
		t.Fatalf("read archive bytes=%d err=%v close=%v", len(archiveBytes), readErr, closeErr)
	}
	restoreRoot := filepath.Join(t.TempDir(), "restored")
	if restoreErr := otherStore.Restore(context.Background(), publication, restoreRoot); restoreErr != nil {
		t.Fatalf("restore from second store: %v", restoreErr)
	}
	contents, contentsErr := os.ReadFile(filepath.Join(restoreRoot, "output.txt"))
	if contentsErr != nil || string(contents) != "workspace" {
		t.Fatalf("restored contents=%q err=%v", contents, contentsErr)
	}

	if deleteErr := store.Delete(context.Background(), publication); deleteErr != nil {
		t.Fatalf("delete: %v", deleteErr)
	}
	if deleteErr := store.Delete(context.Background(), publication); deleteErr != nil {
		t.Fatalf("idempotent delete: %v", deleteErr)
	}
	if _, missingErr := otherStore.Open(context.Background(), publication); !errors.Is(missingErr, ErrWorkspaceRevisionNotFound) {
		t.Fatalf("open deleted object: %v", missingErr)
	}
}

func TestGCSWorkspaceRevisionStoreRejectsConflictsAndCorruption(t *testing.T) {
	objects := &workspaceRevisionObjectStoreFake{objects: map[string][]byte{}}
	store, storeErr := newGCSWorkspaceRevisionStore(objects, "")
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "output.txt"), []byte("first"), 0o644); writeErr != nil {
		t.Fatalf("write first source: %v", writeErr)
	}
	publication, publishErr := store.Publish(context.Background(), "revision-1", sourceRoot)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "output.txt"), []byte("second"), 0o644); writeErr != nil {
		t.Fatalf("write second source: %v", writeErr)
	}
	if _, conflictErr := store.Publish(context.Background(), "revision-1", sourceRoot); !errors.Is(conflictErr, ErrWorkspaceRevisionConflict) {
		t.Fatalf("conflicting publish: %v", conflictErr)
	}
	corruptPublication := publication
	corruptPublication.ContentDigest = "sha256:corrupt"
	if restoreErr := store.Restore(context.Background(), corruptPublication, filepath.Join(t.TempDir(), "restored")); !errors.Is(restoreErr, ErrWorkspaceRevisionDigestMismatch) {
		t.Fatalf("corrupt restore: %v", restoreErr)
	}
}

func TestGCSWorkspaceRevisionStoreMissingObjectAndProviderValidation(t *testing.T) {
	objects := &workspaceRevisionObjectStoreFake{objects: map[string][]byte{}}
	store, storeErr := newGCSWorkspaceRevisionStore(objects, "")
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}
	size := int64(1)
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:missing", StorageKey: "workspace-revisions/missing.tar.gz", StorageProvider: domain.StorageProviderGCS, SizeBytes: &size}
	if _, missingErr := store.Open(context.Background(), publication); !errors.Is(missingErr, ErrWorkspaceRevisionNotFound) {
		t.Fatalf("missing open: %v", missingErr)
	}
	publication.StorageProvider = domain.StorageProviderFilesystem
	if _, invalidErr := store.Open(context.Background(), publication); !errors.Is(invalidErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("wrong provider open: %v", invalidErr)
	}
}

func TestResolveWorkspaceRevisionStoreSelection(t *testing.T) {
	filesystemStore, filesystemErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "filesystem", StorageRoot: t.TempDir()})
	if filesystemErr != nil || filesystemStore.Provider() != domain.StorageProviderFilesystem {
		t.Fatalf("filesystem resolver store=%T err=%v", filesystemStore, filesystemErr)
	}
	if _, missingBucketErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "gcs"}); missingBucketErr == nil {
		t.Fatal("expected missing GCS bucket error")
	}
	originalNewClient := newWorkspaceRevisionGCSClient
	newWorkspaceRevisionGCSClient = func(context.Context, ...option.ClientOption) (*storage.Client, error) {
		return nil, errors.New("adc unavailable")
	}
	t.Cleanup(func() { newWorkspaceRevisionGCSClient = originalNewClient })
	if _, clientErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "gcs", GCSBucket: "workspace-revisions"}); clientErr == nil {
		t.Fatal("expected explicit GCS client error")
	}
	if _, optionalErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "filesystem", StorageRoot: t.TempDir(), GCSBucket: "workspace-revisions"}); optionalErr != nil {
		t.Fatalf("non-strict optional GCS setup: %v", optionalErr)
	}
	if _, strictErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "filesystem", StorageRoot: t.TempDir(), GCSBucket: "workspace-revisions", Strict: true}); strictErr == nil {
		t.Fatal("expected strict optional GCS client error")
	}
}

type workspaceRevisionObjectStoreFake struct {
	objects map[string][]byte
}

func (s *workspaceRevisionObjectStoreFake) CreateIfAbsent(_ context.Context, objectName string, source io.Reader) (bool, error) {
	if _, found := s.objects[objectName]; found {
		return false, nil
	}
	contents, readErr := io.ReadAll(source)
	if readErr != nil {
		return false, readErr
	}
	s.objects[objectName] = contents
	return true, nil
}

func (s *workspaceRevisionObjectStoreFake) Open(_ context.Context, objectName string) (io.ReadCloser, error) {
	contents, found := s.objects[objectName]
	if !found {
		return nil, storage.ErrObjectNotExist
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

func (s *workspaceRevisionObjectStoreFake) Delete(_ context.Context, objectName string) error {
	if _, found := s.objects[objectName]; !found {
		return storage.ErrObjectNotExist
	}
	delete(s.objects, objectName)
	return nil
}
