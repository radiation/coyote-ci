package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestFilesystemWorkspaceRevisionStorePublishRestoreAndDelete(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewFilesystemWorkspaceRevisionStore(storeRoot)
	sourceRoot := filepath.Join(t.TempDir(), "source")
	executablePath := filepath.Join(sourceRoot, "bin", "run.sh")
	if err := os.MkdirAll(filepath.Dir(executablePath), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\necho coyote\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "README.md"), []byte("workspace"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := os.Chmod(sourceRoot, 0o751); err != nil {
		t.Fatalf("set source root mode: %v", err)
	}

	publication, err := store.Publish(context.Background(), "revision-1", sourceRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if publication.StorageKey != "workspace-revisions/revision-1.tar.gz" || publication.SizeBytes == nil || *publication.SizeBytes <= 0 {
		t.Fatalf("unexpected publication: %+v", publication)
	}
	archivePath := filepath.Join(storeRoot, "workspace-revisions", "revision-1.tar.gz")
	archiveBytes, readErr := os.ReadFile(archivePath)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	wantDigest := sha256.Sum256(archiveBytes)
	if publication.ContentDigest != "sha256:"+hex.EncodeToString(wantDigest[:]) || *publication.SizeBytes != int64(len(archiveBytes)) {
		t.Fatalf("publication does not describe archive: %+v", publication)
	}
	archive, openErr := store.Open(context.Background(), publication)
	if openErr != nil {
		t.Fatalf("open archive: %v", openErr)
	}
	streamedBytes, readErr := io.ReadAll(archive)
	closeErr := archive.Close()
	if readErr != nil || closeErr != nil || string(streamedBytes) != string(archiveBytes) {
		t.Fatalf("stream archive = %d bytes, %v, %v; want stored bytes", len(streamedBytes), readErr, closeErr)
	}

	repeated, repeatErr := store.Publish(context.Background(), "revision-1", sourceRoot)
	if repeatErr != nil || repeated.ContentDigest != publication.ContentDigest || repeated.StorageKey != publication.StorageKey || repeated.SizeBytes == nil || *repeated.SizeBytes != *publication.SizeBytes {
		t.Fatalf("identical publish = %+v, %v; want %+v, nil", repeated, repeatErr, publication)
	}
	restoreRoot := filepath.Join(t.TempDir(), "restore")
	if restoreErr := store.Restore(context.Background(), publication, restoreRoot); restoreErr != nil {
		t.Fatalf("restore: %v", restoreErr)
	}
	if restored, restoredErr := os.ReadFile(filepath.Join(restoreRoot, "bin", "run.sh")); restoredErr != nil || string(restored) != "#!/bin/sh\necho coyote\n" {
		t.Fatalf("restored executable = %q, %v", restored, restoredErr)
	}
	if info, statErr := os.Stat(filepath.Join(restoreRoot, "bin", "run.sh")); statErr != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("restored mode = %v, %v", info.Mode(), statErr)
	}
	if info, statErr := os.Stat(restoreRoot); statErr != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("restored root mode = %v, %v", info.Mode(), statErr)
	}
	if deleteErr := store.Delete(context.Background(), publication); deleteErr != nil {
		t.Fatalf("delete: %v", deleteErr)
	}
	if deleteErr := store.Delete(context.Background(), publication); deleteErr != nil {
		t.Fatalf("idempotent delete: %v", deleteErr)
	}
	if _, openErr := store.Open(context.Background(), publication); !errors.Is(openErr, ErrWorkspaceRevisionNotFound) {
		t.Fatalf("open deleted object: %v", openErr)
	}
	if restoreErr := store.Restore(context.Background(), publication, filepath.Join(t.TempDir(), "missing")); !errors.Is(restoreErr, ErrWorkspaceRevisionNotFound) {
		t.Fatalf("restore deleted object: %v", restoreErr)
	}
}

func TestArchiveDirectoryCreatesRestorableTransportArchive(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "source.txt"), []byte("workspace"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	archive, publication, archiveErr := ArchiveDirectory(context.Background(), sourceRoot)
	if archiveErr != nil {
		t.Fatalf("archive directory: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	if publication.StorageKey != "transport/source.tar.gz" || publication.Validate() != nil {
		t.Fatalf("publication=%#v", publication)
	}
	destinationRoot := filepath.Join(t.TempDir(), "restore")
	if restoreErr := RestoreArchive(context.Background(), archive, publication, destinationRoot); restoreErr != nil {
		t.Fatalf("restore archive: %v", restoreErr)
	}
	contents, readErr := os.ReadFile(filepath.Join(destinationRoot, "source.txt"))
	if readErr != nil || string(contents) != "workspace" {
		t.Fatalf("restored contents=%q err=%v", contents, readErr)
	}
}

func TestArchiveDirectoryRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, archiveErr := ArchiveDirectory(ctx, t.TempDir()); !errors.Is(archiveErr, context.Canceled) {
		t.Fatalf("archive directory: %v", archiveErr)
	}
}

func TestArchiveDirectoryRejectsMissingAndNonDirectoryRoots(t *testing.T) {
	fileRoot := filepath.Join(t.TempDir(), "source-file")
	if writeErr := os.WriteFile(fileRoot, []byte("content"), 0o600); writeErr != nil {
		t.Fatalf("write source file: %v", writeErr)
	}
	for _, sourceRoot := range []string{filepath.Join(t.TempDir(), "missing"), fileRoot} {
		if _, _, archiveErr := ArchiveDirectory(context.Background(), sourceRoot); archiveErr == nil {
			t.Fatalf("expected archive failure for %q", sourceRoot)
		}
	}
}

func TestRestoreArchiveRejectsInvalidInputBeforeCreatingDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "restore")
	if restoreErr := RestoreArchive(context.Background(), nil, domain.WorkspaceRevisionPublication{}, destination); !errors.Is(restoreErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("restore invalid input: %v", restoreErr)
	}
	if restoreErr := RestoreArchive(context.Background(), strings.NewReader("archive"), domain.WorkspaceRevisionPublication{}, destination); !errors.Is(restoreErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("restore invalid publication: %v", restoreErr)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("invalid restore created destination: %v", statErr)
	}
}

func TestRestoreArchiveRejectsExistingAndBlankDestinations(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "source.txt"), []byte("workspace"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	archive, publication, archiveErr := ArchiveDirectory(context.Background(), sourceRoot)
	if archiveErr != nil {
		t.Fatalf("archive directory: %v", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	if restoreErr := RestoreArchive(context.Background(), archive, publication, " "); restoreErr == nil {
		t.Fatal("expected blank destination error")
	}
	existingDestination := filepath.Join(t.TempDir(), "restore")
	if mkdirErr := os.Mkdir(existingDestination, 0o755); mkdirErr != nil {
		t.Fatalf("create destination: %v", mkdirErr)
	}
	if restoreErr := RestoreArchive(context.Background(), archive, publication, existingDestination); !errors.Is(restoreErr, ErrWorkspaceRevisionDestination) {
		t.Fatalf("existing destination restore: %v", restoreErr)
	}
}

func TestFilesystemWorkspaceRevisionStoreRejectsConflictsAndUnsupportedEntries(t *testing.T) {
	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	sourceRoot := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := store.Publish(context.Background(), "revision-1", sourceRoot); err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("second"), 0o644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	if _, err := store.Publish(context.Background(), "revision-1", sourceRoot); !errors.Is(err, ErrWorkspaceRevisionConflict) {
		t.Fatalf("conflicting publish: %v", err)
	}
	if err := os.Symlink("file.txt", filepath.Join(sourceRoot, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	if _, err := store.Publish(context.Background(), "revision-2", sourceRoot); !errors.Is(err, ErrUnsupportedWorkspaceRevisionEntry) {
		t.Fatalf("symlink publish: %v", err)
	}
	sourceLink := filepath.Join(t.TempDir(), "source-link")
	if err := os.Symlink(sourceRoot, sourceLink); err != nil {
		t.Fatalf("make source root symlink: %v", err)
	}
	if _, err := store.Publish(context.Background(), "revision-symlink-root", sourceLink); !errors.Is(err, ErrUnsupportedWorkspaceRevisionEntry) {
		t.Fatalf("source root symlink publish: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(filepath.Join(sourceRoot, "link")); err != nil {
			t.Fatalf("remove symlink: %v", err)
		}
		if fifoErr := syscall.Mkfifo(filepath.Join(sourceRoot, "pipe"), 0o644); fifoErr != nil {
			t.Fatalf("make fifo: %v", fifoErr)
		}
		if _, err := store.Publish(context.Background(), "revision-3", sourceRoot); !errors.Is(err, ErrUnsupportedWorkspaceRevisionEntry) {
			t.Fatalf("fifo publish: %v", err)
		}
	}
}

func TestFilesystemWorkspaceRevisionStoreRestoreDefersReadOnlyDirectoryMode(t *testing.T) {
	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	sourceRoot := t.TempDir()
	readOnlyDirectory := filepath.Join(sourceRoot, "read-only")
	if err := os.Mkdir(readOnlyDirectory, 0o755); err != nil {
		t.Fatalf("mkdir source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(readOnlyDirectory, "child.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write source child: %v", err)
	}
	if err := os.Chmod(readOnlyDirectory, 0o555); err != nil {
		t.Fatalf("make source directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyDirectory, 0o755) })

	publication, err := store.Publish(context.Background(), "revision-read-only", sourceRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	restoreRoot := filepath.Join(t.TempDir(), "restore")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(restoreRoot, "read-only"), 0o755) })
	if restoreErr := store.Restore(context.Background(), publication, restoreRoot); restoreErr != nil {
		t.Fatalf("restore: %v", restoreErr)
	}
	if restored, readErr := os.ReadFile(filepath.Join(restoreRoot, "read-only", "child.txt")); readErr != nil || string(restored) != "content" {
		t.Fatalf("read restored child = %q, %v", restored, readErr)
	}
	if info, statErr := os.Stat(filepath.Join(restoreRoot, "read-only")); statErr != nil || info.Mode().Perm() != 0o555 {
		t.Fatalf("restored directory mode = %v, %v", info.Mode(), statErr)
	}
}

func TestFilesystemWorkspaceRevisionStoreRestoreRejectsUnsafeAndCorruptArchives(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewFilesystemWorkspaceRevisionStore(storeRoot)
	for _, testCase := range []struct {
		name      string
		entryName string
		typeflag  byte
		content   string
		want      error
	}{
		{name: "absolute", entryName: "/outside", typeflag: tar.TypeReg, content: "bad", want: ErrUnsafeWorkspaceRevisionPath},
		{name: "traversal", entryName: "../outside", typeflag: tar.TypeReg, content: "bad", want: ErrUnsafeWorkspaceRevisionPath},
		{name: "windows", entryName: `C:\\outside`, typeflag: tar.TypeReg, content: "bad", want: ErrUnsafeWorkspaceRevisionPath},
		{name: "symlink", entryName: "link", typeflag: tar.TypeSymlink, want: ErrUnsupportedWorkspaceRevisionEntry},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			publication := writeRevisionFixture(t, storeRoot, testCase.entryName, testCase.typeflag, testCase.content)
			destination := filepath.Join(t.TempDir(), "restore")
			if err := store.Restore(context.Background(), publication, destination); !errors.Is(err, testCase.want) {
				t.Fatalf("restore: %v", err)
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("failed restore exposed destination: %v", err)
			}
		})
	}
	publication := writeRevisionFixture(t, storeRoot, "file.txt", tar.TypeReg, "content")
	publication.ContentDigest = "sha256:bad"
	if err := store.Restore(context.Background(), publication, filepath.Join(t.TempDir(), "restore")); !errors.Is(err, ErrWorkspaceRevisionDigestMismatch) {
		t.Fatalf("digest mismatch: %v", err)
	}
	if err := store.Restore(context.Background(), publicationForWorkspaceRevision("workspace-revisions/missing.tar.gz", "sha256:bad", 1), filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrWorkspaceRevisionNotFound) {
		t.Fatalf("missing revision: %v", err)
	}
}

func TestFilesystemWorkspaceRevisionStoreRestoreRejectsDestinationAndCancellation(t *testing.T) {
	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	publication, err := store.Publish(context.Background(), "revision-1", sourceRoot)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if mkdirErr := os.Mkdir(destination, 0o755); mkdirErr != nil {
		t.Fatalf("mkdir destination: %v", mkdirErr)
	}
	if restoreErr := store.Restore(context.Background(), publication, destination); !errors.Is(restoreErr, ErrWorkspaceRevisionDestination) {
		t.Fatalf("restore existing destination: %v", restoreErr)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, publishErr := store.Publish(canceled, "revision-2", sourceRoot); !errors.Is(publishErr, context.Canceled) {
		t.Fatalf("cancelled publish: %v", publishErr)
	}
	if restoreErr := store.Restore(context.Background(), publicationForWorkspaceRevision("workspace-revisions/../escape.tar.gz", "sha256:bad", 1), filepath.Join(t.TempDir(), "unsafe")); !errors.Is(restoreErr, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("unsafe storage key: %v", restoreErr)
	}
}

func TestFilesystemWorkspaceRevisionStoreRestoreRejectsMalformedArchives(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewFilesystemWorkspaceRevisionStore(storeRoot)
	archivePath := filepath.Join(storeRoot, "workspace-revisions", "malformed.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	for _, fixture := range []struct {
		name     string
		contents []byte
	}{
		{name: "gzip", contents: []byte("not gzip")},
		{name: "tar", contents: gzipBytes(t, []byte("not a tar archive"))},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if err := os.WriteFile(archivePath, fixture.contents, 0o600); err != nil {
				t.Fatalf("write malformed archive: %v", err)
			}
			digest := sha256.Sum256(fixture.contents)
			publication := publicationForWorkspaceRevision("workspace-revisions/malformed.tar.gz", "sha256:"+hex.EncodeToString(digest[:]), int64(len(fixture.contents)))
			destination := filepath.Join(t.TempDir(), "restore")
			if err := store.Restore(context.Background(), publication, destination); err == nil {
				t.Fatal("expected malformed archive restore failure")
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("malformed restore exposed destination: %v", err)
			}
		})
	}
}

func TestFilesystemWorkspaceRevisionStoreValidationAndCancellation(t *testing.T) {
	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	sourceRoot := t.TempDir()
	sourceFile := filepath.Join(t.TempDir(), "source-file")
	if err := os.WriteFile(sourceFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	for _, testCase := range []struct {
		name       string
		revisionID string
		sourceRoot string
	}{
		{name: "blank revision", revisionID: " ", sourceRoot: sourceRoot},
		{name: "path revision", revisionID: "nested/revision", sourceRoot: sourceRoot},
		{name: "blank source", revisionID: "revision", sourceRoot: " "},
		{name: "missing source", revisionID: "revision", sourceRoot: filepath.Join(t.TempDir(), "missing")},
		{name: "file source", revisionID: "revision", sourceRoot: sourceFile},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := store.Publish(context.Background(), testCase.revisionID, testCase.sourceRoot); err == nil {
				t.Fatal("expected publish validation failure")
			}
		})
	}
	if err := store.Restore(context.Background(), domain.WorkspaceRevisionPublication{}, filepath.Join(t.TempDir(), "restore")); !errors.Is(err, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("invalid publication restore: %v", err)
	}
	if err := store.Delete(context.Background(), domain.WorkspaceRevisionPublication{}); !errors.Is(err, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("invalid publication delete: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	publication := publicationForWorkspaceRevision("workspace-revisions/revision.tar.gz", "sha256:one", 1)
	if err := store.Delete(canceled, publication); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delete: %v", err)
	}
	if restoreErr := store.Restore(context.Background(), publication, " "); restoreErr == nil {
		t.Fatal("expected blank destination restore failure")
	}
	destinationFile := filepath.Join(t.TempDir(), "destination-file")
	if writeErr := os.WriteFile(destinationFile, []byte("file"), 0o644); writeErr != nil {
		t.Fatalf("write destination file: %v", writeErr)
	}
	if restoreErr := store.Restore(context.Background(), publication, destinationFile); !errors.Is(restoreErr, ErrWorkspaceRevisionDestination) {
		t.Fatalf("file destination restore: %v", restoreErr)
	}
}

func TestWorkspaceRevisionStorePathAndStreamHelpers(t *testing.T) {
	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	for _, storageKey := range []string{"", "other/revision.tar.gz", "workspace-revisions/revision", "workspace-revisions/../revision.tar.gz", `workspace-revisions\\revision.tar.gz`} {
		if _, _, err := store.pathForStorageKey(storageKey); !errors.Is(err, ErrInvalidWorkspaceRevisionObject) {
			t.Fatalf("storage key %q: %v", storageKey, err)
		}
	}
	if _, _, err := NewFilesystemWorkspaceRevisionStore(" ").pathForStorageKey("workspace-revisions/revision.tar.gz"); !errors.Is(err, ErrInvalidWorkspaceRevisionObject) {
		t.Fatalf("blank root: %v", err)
	}
	for _, archivePath := range []string{"", "..", "../outside", "/outside", `C:\\outside`, `nested\\file`} {
		if _, err := safeWorkspaceRevisionArchivePath(archivePath); !errors.Is(err, ErrUnsafeWorkspaceRevisionPath) {
			t.Fatalf("archive path %q: %v", archivePath, err)
		}
	}
	if archivePath, err := safeWorkspaceRevisionArchivePath("."); err != nil || archivePath != "." {
		t.Fatalf("root archive path = %q, %v", archivePath, err)
	}
	if archivePath, err := safeWorkspaceRevisionArchivePath("nested/../file.txt"); err != nil || archivePath != "file.txt" {
		t.Fatalf("normalized archive path = %q, %v", archivePath, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyWorkspaceRevision(canceled, io.Discard, strings.NewReader("content")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy: %v", err)
	}
	if _, err := copyWorkspaceRevision(context.Background(), shortWorkspaceRevisionWriter{}, strings.NewReader("content")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
	if _, err := (&workspaceRevisionCountingReader{reader: strings.NewReader("content"), writer: failingWorkspaceRevisionWriter{}}).Read(make([]byte, len("content"))); err == nil {
		t.Fatal("expected counting reader write failure")
	}
}

func TestWorkspaceRevisionArchiveHelpersRejectInvalidRootsAndEntries(t *testing.T) {
	archive, err := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer func() { _ = archive.Close() }()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	if err := writeWorkspaceRevisionArchive(context.Background(), archive, missingRoot); err == nil {
		t.Fatal("expected missing root archive failure")
	}
	sourceFile := filepath.Join(t.TempDir(), "source-file")
	if err := os.WriteFile(sourceFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := writeWorkspaceRevisionArchive(context.Background(), archive, sourceFile); !errors.Is(err, ErrUnsupportedWorkspaceRevisionEntry) {
		t.Fatalf("file root archive: %v", err)
	}
	sourceLink := filepath.Join(t.TempDir(), "source-link")
	if err := os.Symlink(sourceFile, sourceLink); err != nil {
		t.Fatalf("make source link: %v", err)
	}
	if err := writeWorkspaceRevisionArchive(context.Background(), archive, sourceLink); !errors.Is(err, ErrUnsupportedWorkspaceRevisionEntry) {
		t.Fatalf("symlink root archive: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeWorkspaceRevisionArchive(canceled, archive, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled archive: %v", err)
	}

	for _, testCase := range []struct {
		name   string
		header tar.Header
		want   error
	}{
		{name: "regular root", header: tar.Header{Name: ".", Typeflag: tar.TypeReg}, want: ErrUnsafeWorkspaceRevisionPath},
		{name: "empty name", header: tar.Header{Name: "", Typeflag: tar.TypeReg}, want: ErrUnsafeWorkspaceRevisionPath},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			buffer := new(strings.Builder)
			writer := tar.NewWriter(buffer)
			if err := writer.WriteHeader(&testCase.header); err != nil {
				t.Fatalf("write header: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close tar: %v", err)
			}
			if err := extractWorkspaceRevisionArchive(context.Background(), tar.NewReader(strings.NewReader(buffer.String())), t.TempDir()); !errors.Is(err, testCase.want) {
				t.Fatalf("extract: %v", err)
			}
		})
	}
	canceledRestore, cancelRestore := context.WithCancel(context.Background())
	cancelRestore()
	if err := extractWorkspaceRevisionArchive(canceledRestore, tar.NewReader(strings.NewReader("")), t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled extract: %v", err)
	}
}

func TestFilesystemWorkspaceRevisionStoreRestoreRejectsArchiveDirectory(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewFilesystemWorkspaceRevisionStore(storeRoot)
	archivePath := filepath.Join(storeRoot, "workspace-revisions", "directory.tar.gz")
	if err := os.MkdirAll(archivePath, 0o755); err != nil {
		t.Fatalf("mkdir archive directory: %v", err)
	}
	publication := publicationForWorkspaceRevision("workspace-revisions/directory.tar.gz", "sha256:bad", 1)
	if err := store.Restore(context.Background(), publication, filepath.Join(t.TempDir(), "restore")); err == nil {
		t.Fatal("expected archive directory restore failure")
	}
}

func TestWorkspaceRevisionStoreHelperFailures(t *testing.T) {
	if _, _, digestErr := workspaceRevisionDigestAndSize(context.Background(), filepath.Join(t.TempDir(), "missing")); digestErr == nil {
		t.Fatal("expected missing archive digest failure")
	}
	archivePath := filepath.Join(t.TempDir(), "archive")
	if writeErr := os.WriteFile(archivePath, []byte("content"), 0o644); writeErr != nil {
		t.Fatalf("write archive: %v", writeErr)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, digestErr := workspaceRevisionDigestAndSize(canceled, archivePath); !errors.Is(digestErr, context.Canceled) {
		t.Fatalf("cancelled archive digest: %v", digestErr)
	}
	if syncErr := syncWorkspaceRevisionDirectory(filepath.Join(t.TempDir(), "missing")); syncErr == nil {
		t.Fatal("expected missing directory sync failure")
	}

	archive, createErr := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	if createErr != nil {
		t.Fatalf("create archive: %v", createErr)
	}
	if closeErr := archive.Close(); closeErr != nil {
		t.Fatalf("close archive: %v", closeErr)
	}
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("content"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	closedFile, openErr := os.OpenFile(archive.Name(), os.O_WRONLY, 0)
	if openErr != nil {
		t.Fatalf("open archive: %v", openErr)
	}
	if closeErr := closedFile.Close(); closeErr != nil {
		t.Fatalf("close output: %v", closeErr)
	}
	if writeErr := writeWorkspaceRevisionArchive(context.Background(), closedFile, sourceRoot); writeErr == nil {
		t.Fatal("expected closed archive output failure")
	}

	duplicateTar := tarBytes(t, []tar.Header{
		{Name: "file.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
		{Name: "file.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
	})
	if extractErr := extractWorkspaceRevisionArchive(context.Background(), tar.NewReader(strings.NewReader(duplicateTar)), t.TempDir()); extractErr == nil {
		t.Fatal("expected duplicate archive entry failure")
	}
	if extractErr := extractWorkspaceRevisionArchive(context.Background(), tar.NewReader(strings.NewReader("not a tar archive")), t.TempDir()); extractErr == nil {
		t.Fatal("expected malformed tar extraction failure")
	}
	if modeErr := applyWorkspaceRevisionDirectoryModes(map[string]os.FileMode{filepath.Join(t.TempDir(), "missing"): 0o755}); modeErr == nil {
		t.Fatal("expected missing directory mode failure")
	}
}

func TestFilesystemWorkspaceRevisionStoreFilesystemFailures(t *testing.T) {
	sourceRoot := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("content"), 0o644); writeErr != nil {
		t.Fatalf("write source: %v", writeErr)
	}
	storageRootFile := filepath.Join(t.TempDir(), "storage-root-file")
	if writeErr := os.WriteFile(storageRootFile, []byte("not a directory"), 0o644); writeErr != nil {
		t.Fatalf("write storage root file: %v", writeErr)
	}
	if _, publishErr := NewFilesystemWorkspaceRevisionStore(storageRootFile).Publish(context.Background(), "revision", sourceRoot); publishErr == nil {
		t.Fatal("expected storage root publish failure")
	}

	store := NewFilesystemWorkspaceRevisionStore(t.TempDir())
	publication, publishErr := store.Publish(context.Background(), "revision", sourceRoot)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	destinationParentFile := filepath.Join(t.TempDir(), "destination-parent-file")
	if writeErr := os.WriteFile(destinationParentFile, []byte("not a directory"), 0o644); writeErr != nil {
		t.Fatalf("write destination parent file: %v", writeErr)
	}
	if restoreErr := store.Restore(context.Background(), publication, filepath.Join(destinationParentFile, "restore")); restoreErr == nil {
		t.Fatal("expected destination parent restore failure")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if restoreErr := store.Restore(canceled, publication, filepath.Join(t.TempDir(), "restore")); !errors.Is(restoreErr, context.Canceled) {
		t.Fatalf("cancelled restore: %v", restoreErr)
	}

	archivePath := filepath.Join(store.root, "workspace-revisions", "revision.tar.gz")
	if removeErr := os.Remove(archivePath); removeErr != nil {
		t.Fatalf("remove archive: %v", removeErr)
	}
	if mkdirErr := os.Mkdir(archivePath, 0o755); mkdirErr != nil {
		t.Fatalf("create archive directory: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(archivePath, "child"), []byte("content"), 0o644); writeErr != nil {
		t.Fatalf("write archive directory child: %v", writeErr)
	}
	if deleteErr := store.Delete(context.Background(), publication); deleteErr == nil {
		t.Fatal("expected nonempty archive directory delete failure")
	}
}

func tarBytes(t *testing.T, headers []tar.Header) string {
	t.Helper()
	buffer := new(strings.Builder)
	writer := tar.NewWriter(buffer)
	for _, header := range headers {
		if writeErr := writer.WriteHeader(&header); writeErr != nil {
			t.Fatalf("write tar header: %v", writeErr)
		}
	}
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close tar writer: %v", closeErr)
	}
	return buffer.String()
}

type shortWorkspaceRevisionWriter struct{}

func (shortWorkspaceRevisionWriter) Write(contents []byte) (int, error) {
	return len(contents) - 1, nil
}

type failingWorkspaceRevisionWriter struct{}

func (failingWorkspaceRevisionWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func gzipBytes(t *testing.T, contents []byte) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create gzip fixture: %v", err)
	}
	writer := gzip.NewWriter(file)
	if _, writeErr := writer.Write(contents); writeErr != nil {
		t.Fatalf("write gzip fixture: %v", writeErr)
	}
	if closeWriterErr := writer.Close(); closeWriterErr != nil {
		t.Fatalf("close gzip fixture: %v", closeWriterErr)
	}
	if closeFileErr := file.Close(); closeFileErr != nil {
		t.Fatalf("close gzip file: %v", closeFileErr)
	}
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gzip fixture: %v", err)
	}
	return result
}

func writeRevisionFixture(t *testing.T, storeRoot string, entryName string, typeflag byte, content string) domain.WorkspaceRevisionPublication {
	t.Helper()
	archivePath := filepath.Join(storeRoot, "workspace-revisions", "fixture.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	file, createErr := os.Create(archivePath)
	if createErr != nil {
		t.Fatalf("create fixture: %v", createErr)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: entryName, Typeflag: typeflag, Mode: 0o755, Size: int64(len(content))}
	if typeflag == tar.TypeSymlink {
		header.Size = 0
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("write fixture header: %v", err)
	}
	if content != "" && typeflag != tar.TypeSymlink {
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("write fixture body: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	bytes, readErr := os.ReadFile(archivePath)
	if readErr != nil {
		t.Fatalf("read fixture: %v", readErr)
	}
	digest := sha256.Sum256(bytes)
	return publicationForWorkspaceRevision("workspace-revisions/fixture.tar.gz", "sha256:"+hex.EncodeToString(digest[:]), int64(len(bytes)))
}
