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
	"google.golang.org/api/googleapi"
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

func TestGCSWorkspaceRevisionStoreConstructorAndKeyValidation(t *testing.T) {
	if _, err := NewGCSWorkspaceRevisionStore(nil, GCSWorkspaceRevisionStoreConfig{Bucket: "workspace-revisions"}); err == nil {
		t.Fatal("expected nil client error")
	}
	client, clientErr := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if clientErr != nil {
		t.Fatalf("create storage client: %v", clientErr)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := NewGCSWorkspaceRevisionStore(client, GCSWorkspaceRevisionStoreConfig{}); err == nil {
		t.Fatal("expected empty bucket error")
	}
	store, storeErr := NewGCSWorkspaceRevisionStore(client, GCSWorkspaceRevisionStoreConfig{Bucket: "workspace-revisions", Prefix: " /coyote/revisions/ "})
	if storeErr != nil || store.Provider() != domain.StorageProviderGCS || store.objectName("workspace-revisions/revision.tar.gz") != "coyote/revisions/workspace-revisions/revision.tar.gz" {
		t.Fatalf("store=%#v err=%v", store, storeErr)
	}
	if _, err := newGCSWorkspaceRevisionStore(nil, ""); err == nil {
		t.Fatal("expected nil object store error")
	}

	for _, revisionID := range []string{"", " ", ".", "..", "../escape", `..\escape`, "nested/revision"} {
		if _, err := workspaceRevisionStorageKey(revisionID); !errors.Is(err, ErrInvalidWorkspaceRevisionObject) {
			t.Fatalf("revision ID %q error=%v", revisionID, err)
		}
	}
	if key, err := workspaceRevisionStorageKey("revision-1"); err != nil || key != "workspace-revisions/revision-1.tar.gz" {
		t.Fatalf("key=%q err=%v", key, err)
	}
	for _, storageKey := range []string{"workspace-revisions", "../workspace-revisions/revision.tar.gz", "/workspace-revisions/revision.tar.gz", `workspace-revisions\revision.tar.gz`, "workspace-revisions/../revision.tar.gz", "workspace-revisions/revision.zip"} {
		if validWorkspaceRevisionStorageKey(storageKey) {
			t.Fatalf("storage key %q was unexpectedly valid", storageKey)
		}
	}
	if !validWorkspaceRevisionStorageKey("workspace-revisions/revision.tar.gz") {
		t.Fatal("expected valid workspace revision storage key")
	}
	if !isGCSPreconditionFailure(&googleapi.Error{Code: 412}) || isGCSPreconditionFailure(&googleapi.Error{Code: 409}) || isGCSPreconditionFailure(errors.New("not an API error")) {
		t.Fatal("unexpected GCS precondition classification")
	}
}

func TestGCSWorkspaceRevisionStoreOperationFailures(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "output.txt"), []byte("workspace"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	objects := &workspaceRevisionObjectStoreFake{objects: map[string][]byte{}, createErr: errors.New("upload failed")}
	store, storeErr := newGCSWorkspaceRevisionStore(objects, "")
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}
	if _, publishErr := store.Publish(context.Background(), "revision-1", sourceRoot); !errors.Is(publishErr, objects.createErr) {
		t.Fatalf("publish error=%v", publishErr)
	}
	if _, publishErr := store.Publish(context.Background(), "../invalid", sourceRoot); !errors.Is(publishErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("invalid revision publish error=%v", publishErr)
	}

	size := int64(1)
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "workspace-revisions/revision-1.tar.gz", StorageProvider: domain.StorageProviderGCS, SizeBytes: &size}
	objects.createErr = nil
	objects.openErr = errors.New("open failed")
	if restoreErr := store.Restore(context.Background(), publication, t.TempDir()); !errors.Is(restoreErr, objects.openErr) {
		t.Fatalf("restore error=%v", restoreErr)
	}
	if deleteErr := store.Delete(context.Background(), publication); deleteErr != nil {
		t.Fatalf("missing delete should remain idempotent: %v", deleteErr)
	}
	objects.deleteErr = errors.New("delete failed")
	if deleteErr := store.Delete(context.Background(), publication); !errors.Is(deleteErr, objects.deleteErr) {
		t.Fatalf("delete error=%v", deleteErr)
	}
}

func TestGCSWorkspaceRevisionStoreRejectsCollisionWhenExistingObjectCannotOpen(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "output.txt"), []byte("workspace"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	objects := &workspaceRevisionObjectStoreFake{objects: map[string][]byte{}, openErr: errors.New("existing object unavailable")}
	store, storeErr := newGCSWorkspaceRevisionStore(objects, "")
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}
	objects.objects["workspace-revisions/revision-1.tar.gz"] = []byte("existing")
	if _, publishErr := store.Publish(context.Background(), "revision-1", sourceRoot); !errors.Is(publishErr, objects.openErr) {
		t.Fatalf("publish collision error=%v", publishErr)
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
	if _, unsupportedErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "unsupported", StorageRoot: t.TempDir()}); unsupportedErr == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, missingFilesystemErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "filesystem"}); missingFilesystemErr == nil {
		t.Fatal("expected unconfigured filesystem store error")
	}

	client, newClientErr := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if newClientErr != nil {
		t.Fatalf("create storage client: %v", newClientErr)
	}
	t.Cleanup(func() { _ = client.Close() })
	newWorkspaceRevisionGCSClient = func(context.Context, ...option.ClientOption) (*storage.Client, error) {
		return client, nil
	}
	gcsResolver, gcsResolverErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{Provider: "gcs", GCSBucket: "workspace-revisions"})
	if gcsResolverErr != nil || gcsResolver.Provider() != domain.StorageProviderGCS {
		t.Fatalf("GCS resolver=%T provider=%q err=%v", gcsResolver, gcsResolver.Provider(), gcsResolverErr)
	}
	defaultFilesystemResolver, defaultFilesystemResolverErr := ResolveWorkspaceRevisionStore(WorkspaceRevisionStoreConfig{StorageRoot: t.TempDir()})
	if defaultFilesystemResolverErr != nil || defaultFilesystemResolver.Provider() != domain.StorageProviderFilesystem {
		t.Fatalf("default resolver=%T provider=%q err=%v", defaultFilesystemResolver, defaultFilesystemResolver.Provider(), defaultFilesystemResolverErr)
	}
}

func TestWorkspaceRevisionStoreResolverRoutesPersistedProviders(t *testing.T) {
	filesystemStore := &workspaceRevisionStoreFake{provider: domain.StorageProviderFilesystem}
	gcsStore := &workspaceRevisionStoreFake{provider: domain.StorageProviderGCS}
	resolver := &WorkspaceRevisionStoreResolver{
		defaultStore: filesystemStore,
		stores: map[domain.StorageProvider]WorkspaceRevisionStore{
			domain.StorageProviderFilesystem: filesystemStore,
			domain.StorageProviderGCS:        gcsStore,
		},
	}
	size := int64(1)
	filesystemPublication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:filesystem", StorageKey: "workspace-revisions/filesystem.tar.gz", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: &size}
	gcsPublication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:gcs", StorageKey: "workspace-revisions/gcs.tar.gz", StorageProvider: domain.StorageProviderGCS, SizeBytes: &size}

	if _, publishErr := resolver.Publish(context.Background(), "revision-1", t.TempDir()); publishErr != nil || filesystemStore.publishCalls != 1 {
		t.Fatalf("publish error=%v calls=%d", publishErr, filesystemStore.publishCalls)
	}
	if restoreErr := resolver.Restore(context.Background(), gcsPublication, t.TempDir()); restoreErr != nil || gcsStore.restoreCalls != 1 {
		t.Fatalf("restore error=%v calls=%d", restoreErr, gcsStore.restoreCalls)
	}
	archive, openErr := resolver.Open(context.Background(), gcsPublication)
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	if closeErr := archive.Close(); closeErr != nil || gcsStore.openCalls != 1 {
		t.Fatalf("close=%v calls=%d", closeErr, gcsStore.openCalls)
	}
	if deleteErr := resolver.Delete(context.Background(), filesystemPublication); deleteErr != nil || filesystemStore.deleteCalls != 1 {
		t.Fatalf("delete error=%v calls=%d", deleteErr, filesystemStore.deleteCalls)
	}

	invalidPublication := filesystemPublication
	invalidPublication.StorageProvider = ""
	if _, openErr := resolver.Open(context.Background(), invalidPublication); !errors.Is(openErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("invalid publication open error=%v", openErr)
	}
	resolver.stores = map[domain.StorageProvider]WorkspaceRevisionStore{}
	if deleteErr := resolver.Delete(context.Background(), filesystemPublication); deleteErr == nil {
		t.Fatal("expected unconfigured provider error")
	}
	resolver.stores = map[domain.StorageProvider]WorkspaceRevisionStore{
		domain.StorageProviderFilesystem: &workspaceRevisionStoreWithoutArchiveReader{provider: domain.StorageProviderFilesystem},
	}
	if _, openErr := resolver.Open(context.Background(), filesystemPublication); openErr == nil {
		t.Fatal("expected archive reader support error")
	}
}

type workspaceRevisionStoreFake struct {
	provider     domain.StorageProvider
	publishCalls int
	restoreCalls int
	openCalls    int
	deleteCalls  int
}

func (s *workspaceRevisionStoreFake) Provider() domain.StorageProvider {
	return s.provider
}

func (s *workspaceRevisionStoreFake) Publish(_ context.Context, _ string, _ string) (domain.WorkspaceRevisionPublication, error) {
	s.publishCalls++
	return domain.WorkspaceRevisionPublication{StorageProvider: s.provider}, nil
}

func (s *workspaceRevisionStoreFake) Restore(_ context.Context, _ domain.WorkspaceRevisionPublication, _ string) error {
	s.restoreCalls++
	return nil
}

func (s *workspaceRevisionStoreFake) Open(_ context.Context, _ domain.WorkspaceRevisionPublication) (io.ReadCloser, error) {
	s.openCalls++
	return io.NopCloser(bytes.NewReader([]byte("archive"))), nil
}

func (s *workspaceRevisionStoreFake) Delete(_ context.Context, _ domain.WorkspaceRevisionPublication) error {
	s.deleteCalls++
	return nil
}

type workspaceRevisionStoreWithoutArchiveReader struct {
	provider domain.StorageProvider
}

func (s *workspaceRevisionStoreWithoutArchiveReader) Provider() domain.StorageProvider {
	return s.provider
}

func (*workspaceRevisionStoreWithoutArchiveReader) Publish(context.Context, string, string) (domain.WorkspaceRevisionPublication, error) {
	return domain.WorkspaceRevisionPublication{}, nil
}

func (*workspaceRevisionStoreWithoutArchiveReader) Restore(context.Context, domain.WorkspaceRevisionPublication, string) error {
	return nil
}

func (*workspaceRevisionStoreWithoutArchiveReader) Delete(context.Context, domain.WorkspaceRevisionPublication) error {
	return nil
}

type workspaceRevisionObjectStoreFake struct {
	objects   map[string][]byte
	createErr error
	openErr   error
	deleteErr error
}

func (s *workspaceRevisionObjectStoreFake) CreateIfAbsent(_ context.Context, objectName string, source io.Reader) (bool, error) {
	if s.createErr != nil {
		return false, s.createErr
	}
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
	if s.openErr != nil {
		return nil, s.openErr
	}
	contents, found := s.objects[objectName]
	if !found {
		return nil, storage.ErrObjectNotExist
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

func (s *workspaceRevisionObjectStoreFake) Delete(_ context.Context, objectName string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if _, found := s.objects[objectName]; !found {
		return storage.ErrObjectNotExist
	}
	delete(s.objects, objectName)
	return nil
}
