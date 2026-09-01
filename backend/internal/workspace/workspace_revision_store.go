package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var (
	ErrWorkspaceRevisionConflict         = errors.New("workspace revision object conflict")
	ErrWorkspaceRevisionNotFound         = errors.New("workspace revision object not found")
	ErrInvalidWorkspaceRevisionObject    = errors.New("invalid workspace revision object")
	ErrWorkspaceRevisionDigestMismatch   = errors.New("workspace revision digest mismatch")
	ErrUnsupportedWorkspaceRevisionEntry = errors.New("unsupported workspace revision entry")
	ErrUnsafeWorkspaceRevisionPath       = errors.New("unsafe workspace revision archive path")
	ErrWorkspaceRevisionDestination      = errors.New("workspace revision destination already exists")
)

// WorkspaceRevisionStore holds immutable workspace bytes independently from
// the repository that makes their publication authoritative.
type WorkspaceRevisionStore interface {
	Publish(ctx context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error)
	Restore(ctx context.Context, publication domain.WorkspaceRevisionPublication, destinationRoot string) error
	Delete(ctx context.Context, publication domain.WorkspaceRevisionPublication) error
}

// FilesystemWorkspaceRevisionStore stores tar.gz revision objects below a
// configured root. ContentDigest is SHA-256 over the stored tar.gz bytes.
type FilesystemWorkspaceRevisionStore struct {
	root string
}

func NewFilesystemWorkspaceRevisionStore(root string) *FilesystemWorkspaceRevisionStore {
	return &FilesystemWorkspaceRevisionStore{root: strings.TrimSpace(root)}
}

func (s *FilesystemWorkspaceRevisionStore) Publish(ctx context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error) {
	storageKey, finalPath, err := s.pathForRevisionID(revisionID)
	if err != nil {
		return domain.WorkspaceRevisionPublication{}, err
	}
	if strings.TrimSpace(sourceRoot) == "" {
		return domain.WorkspaceRevisionPublication{}, fmt.Errorf("source root is required")
	}
	if sourceInfo, statErr := os.Lstat(sourceRoot); statErr != nil {
		return domain.WorkspaceRevisionPublication{}, statErr
	} else if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return domain.WorkspaceRevisionPublication{}, fmt.Errorf("%w: source root symlink", ErrUnsupportedWorkspaceRevisionEntry)
	} else if !sourceInfo.IsDir() {
		return domain.WorkspaceRevisionPublication{}, fmt.Errorf("source root is not a directory")
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(finalPath), 0o755); mkdirErr != nil {
		return domain.WorkspaceRevisionPublication{}, mkdirErr
	}

	temporary, createErr := os.CreateTemp(filepath.Dir(finalPath), ".workspace-revision-*.tmp")
	if createErr != nil {
		return domain.WorkspaceRevisionPublication{}, createErr
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if writeErr := writeWorkspaceRevisionArchive(ctx, temporary, sourceRoot); writeErr != nil {
		_ = temporary.Close()
		return domain.WorkspaceRevisionPublication{}, writeErr
	}
	if syncErr := temporary.Sync(); syncErr != nil {
		_ = temporary.Close()
		return domain.WorkspaceRevisionPublication{}, syncErr
	}
	if closeErr := temporary.Close(); closeErr != nil {
		return domain.WorkspaceRevisionPublication{}, closeErr
	}

	digest, size, digestErr := workspaceRevisionDigestAndSize(ctx, temporaryPath)
	if digestErr != nil {
		return domain.WorkspaceRevisionPublication{}, digestErr
	}
	publication := publicationForWorkspaceRevision(storageKey, digest, size)
	if linkErr := os.Link(temporaryPath, finalPath); linkErr == nil {
		if syncErr := syncWorkspaceRevisionDirectory(filepath.Dir(finalPath)); syncErr != nil {
			return domain.WorkspaceRevisionPublication{}, syncErr
		}
		return publication, nil
	} else if !os.IsExist(linkErr) {
		return domain.WorkspaceRevisionPublication{}, linkErr
	}

	existingDigest, existingSize, existingErr := workspaceRevisionDigestAndSize(ctx, finalPath)
	if existingErr != nil {
		return domain.WorkspaceRevisionPublication{}, existingErr
	}
	if existingDigest != digest || existingSize != size {
		return domain.WorkspaceRevisionPublication{}, ErrWorkspaceRevisionConflict
	}
	return publicationForWorkspaceRevision(storageKey, existingDigest, existingSize), nil
}

func (s *FilesystemWorkspaceRevisionStore) Restore(ctx context.Context, publication domain.WorkspaceRevisionPublication, destinationRoot string) error {
	archivePath, err := s.pathForPublication(publication)
	if err != nil {
		return err
	}
	if strings.TrimSpace(destinationRoot) == "" {
		return fmt.Errorf("destination root is required")
	}
	if _, statErr := os.Stat(destinationRoot); statErr == nil {
		return ErrWorkspaceRevisionDestination
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	archive, openErr := os.Open(archivePath)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return ErrWorkspaceRevisionNotFound
		}
		return openErr
	}
	defer func() { _ = archive.Close() }()

	if mkdirErr := os.MkdirAll(filepath.Dir(destinationRoot), 0o755); mkdirErr != nil {
		return mkdirErr
	}
	staging, stagingErr := os.MkdirTemp(filepath.Dir(destinationRoot), ".workspace-restore-*")
	if stagingErr != nil {
		return stagingErr
	}
	defer func() { _ = os.RemoveAll(staging) }()

	hasher := sha256.New()
	countingReader := &workspaceRevisionCountingReader{reader: archive, writer: hasher}
	gzipReader, gzipErr := gzip.NewReader(countingReader)
	if gzipErr != nil {
		return gzipErr
	}
	if extractErr := extractWorkspaceRevisionArchive(ctx, tar.NewReader(gzipReader), staging); extractErr != nil {
		_ = gzipReader.Close()
		return extractErr
	}
	if _, copyErr := copyWorkspaceRevision(ctx, io.Discard, gzipReader); copyErr != nil {
		_ = gzipReader.Close()
		return copyErr
	}
	if closeErr := gzipReader.Close(); closeErr != nil {
		return closeErr
	}
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if digest != publication.ContentDigest {
		return ErrWorkspaceRevisionDigestMismatch
	}
	if _, statErr := os.Stat(destinationRoot); statErr == nil {
		return ErrWorkspaceRevisionDestination
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return os.Rename(staging, destinationRoot)
}

func (s *FilesystemWorkspaceRevisionStore) Delete(ctx context.Context, publication domain.WorkspaceRevisionPublication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	archivePath, err := s.pathForPublication(publication)
	if err != nil {
		return err
	}
	if removeErr := os.Remove(archivePath); removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
}

func (s *FilesystemWorkspaceRevisionStore) pathForRevisionID(revisionID string) (string, string, error) {
	trimmed := strings.TrimSpace(revisionID)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\\`) || trimmed == "." || trimmed == ".." {
		return "", "", ErrInvalidWorkspaceRevisionObject
	}
	return s.pathForStorageKey(path.Join("workspace-revisions", trimmed+".tar.gz"))
}

func (s *FilesystemWorkspaceRevisionStore) pathForPublication(publication domain.WorkspaceRevisionPublication) (string, error) {
	if err := publication.Validate(); err != nil {
		return "", ErrInvalidWorkspaceRevisionObject
	}
	_, archivePath, err := s.pathForStorageKey(publication.StorageKey)
	return archivePath, err
}

func (s *FilesystemWorkspaceRevisionStore) pathForStorageKey(storageKey string) (string, string, error) {
	if strings.TrimSpace(s.root) == "" {
		return "", "", ErrInvalidWorkspaceRevisionObject
	}
	if strings.Contains(storageKey, "\\") || path.IsAbs(storageKey) || !strings.HasPrefix(storageKey, "workspace-revisions/") || !strings.HasSuffix(storageKey, ".tar.gz") {
		return "", "", ErrInvalidWorkspaceRevisionObject
	}
	cleanKey := path.Clean(storageKey)
	if cleanKey != storageKey || cleanKey == "workspace-revisions" {
		return "", "", ErrInvalidWorkspaceRevisionObject
	}
	archivePath := filepath.Join(filepath.Clean(s.root), filepath.FromSlash(cleanKey))
	relativePath, err := filepath.Rel(filepath.Clean(s.root), archivePath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "", ErrInvalidWorkspaceRevisionObject
	}
	return cleanKey, archivePath, nil
}

func writeWorkspaceRevisionArchive(ctx context.Context, destination *os.File, sourceRoot string) error {
	gzipWriter := gzip.NewWriter(destination)
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.WalkDir(sourceRoot, func(entryPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entryPath == sourceRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %s", ErrUnsupportedWorkspaceRevisionEntry, entryPath)
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", ErrUnsupportedWorkspaceRevisionEntry, entryPath)
		}
		relativePath, relErr := filepath.Rel(sourceRoot, entryPath)
		if relErr != nil {
			return relErr
		}
		archiveName, pathErr := safeWorkspaceRevisionArchivePath(relativePath)
		if pathErr != nil {
			return pathErr
		}
		header := &tar.Header{Name: archiveName, Mode: int64(info.Mode().Perm()), ModTime: unixEpoch}
		if info.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Name += "/"
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = info.Size()
		}
		if headerErr := tarWriter.WriteHeader(header); headerErr != nil {
			return headerErr
		}
		if info.IsDir() {
			return nil
		}
		source, openErr := os.Open(entryPath)
		if openErr != nil {
			return openErr
		}
		_, copyErr := copyWorkspaceRevision(ctx, tarWriter, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return walkErr
	}
	if closeErr := tarWriter.Close(); closeErr != nil {
		_ = gzipWriter.Close()
		return closeErr
	}
	return gzipWriter.Close()
}

var unixEpoch = time.Unix(0, 0).UTC()

func extractWorkspaceRevisionArchive(ctx context.Context, reader *tar.Reader, destinationRoot string) error {
	directoryModes := make(map[string]os.FileMode)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return applyWorkspaceRevisionDirectoryModes(directoryModes)
		}
		if err != nil {
			return err
		}
		target, pathErr := workspaceRevisionRestorePath(destinationRoot, header.Name)
		if pathErr != nil {
			return pathErr
		}
		mode := os.FileMode(header.Mode) & 0o777
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			directoryModes[target] = mode
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, openErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if openErr != nil {
				return openErr
			}
			_, copyErr := copyWorkspaceRevision(ctx, file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("%w: tar type %d", ErrUnsupportedWorkspaceRevisionEntry, header.Typeflag)
		}
	}
}

func applyWorkspaceRevisionDirectoryModes(directoryModes map[string]os.FileMode) error {
	directories := make([]string, 0, len(directoryModes))
	for directory := range directoryModes {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(left int, right int) bool {
		return len(directories[left]) > len(directories[right])
	})
	for _, directory := range directories {
		if err := os.Chmod(directory, directoryModes[directory]); err != nil {
			return err
		}
	}
	return nil
}

func safeWorkspaceRevisionArchivePath(value string) (string, error) {
	if strings.Contains(value, "\\") || looksLikeWindowsDrivePath(value) {
		return "", ErrUnsafeWorkspaceRevisionPath
	}
	cleaned := path.Clean(value)
	if value == "" || path.IsAbs(value) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrUnsafeWorkspaceRevisionPath
	}
	return cleaned, nil
}

func workspaceRevisionRestorePath(destinationRoot string, archiveName string) (string, error) {
	cleaned, err := safeWorkspaceRevisionArchivePath(archiveName)
	if err != nil {
		return "", err
	}
	target := filepath.Join(destinationRoot, filepath.FromSlash(cleaned))
	relativePath, relErr := filepath.Rel(filepath.Clean(destinationRoot), target)
	if relErr != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", ErrUnsafeWorkspaceRevisionPath
	}
	return target, nil
}

func publicationForWorkspaceRevision(storageKey string, digest string, size int64) domain.WorkspaceRevisionPublication {
	return domain.WorkspaceRevisionPublication{ContentDigest: digest, StorageKey: storageKey, SizeBytes: &size}
}

func workspaceRevisionDigestAndSize(ctx context.Context, archivePath string) (string, int64, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	size, err := copyWorkspaceRevision(ctx, hasher, file)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func syncWorkspaceRevisionDirectory(directory string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}

type workspaceRevisionCountingReader struct {
	reader io.Reader
	writer io.Writer
}

func (r *workspaceRevisionCountingReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if read > 0 {
		if _, writeErr := r.writer.Write(buffer[:read]); writeErr != nil {
			return read, writeErr
		}
	}
	return read, err
}

func copyWorkspaceRevision(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			copied += int64(written)
			if writeErr != nil {
				return copied, writeErr
			}
			if written != read {
				return copied, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
	}
}
