package cache

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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrInvalidCacheKey = errors.New("invalid cache key")

const defaultMaxCacheSizeBytes int64 = 10 * 1024 * 1024 * 1024

type FilesystemStore struct {
	root         string
	maxSizeBytes int64
}

func NewFilesystemStore(root string) *FilesystemStore {
	return NewFilesystemStoreWithMaxSize(root, defaultMaxCacheSizeBytes)
}

func NewFilesystemStoreWithMaxSize(root string, maxSizeBytes int64) *FilesystemStore {
	if maxSizeBytes <= 0 {
		maxSizeBytes = defaultMaxCacheSizeBytes
	}
	return &FilesystemStore{root: strings.TrimSpace(root), maxSizeBytes: maxSizeBytes}
}

func (s *FilesystemStore) Provider() domain.StorageProvider {
	return domain.StorageProviderFilesystem
}

func (s *FilesystemStore) Restore(_ context.Context, key string, destinationRoot string) (RestoreResult, error) {
	archivePath, err := s.resolvePathForKey(key)
	if err != nil {
		return RestoreResult{}, err
	}
	if strings.TrimSpace(destinationRoot) == "" {
		return RestoreResult{}, errors.New("destination root is required")
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return RestoreResult{Hit: false, Compression: "tar.gz"}, nil
		}
		return RestoreResult{}, err
	}
	if info.IsDir() {
		return RestoreResult{}, fmt.Errorf("cache entry is not an archive file: %s", archivePath)
	}

	if mkdirErr := os.MkdirAll(destinationRoot, 0o755); mkdirErr != nil {
		return RestoreResult{}, mkdirErr
	}
	if err := extractTarGz(archivePath, destinationRoot); err != nil {
		return RestoreResult{}, err
	}

	now := time.Now().UTC()
	_ = os.Chtimes(archivePath, now, now)
	return RestoreResult{Hit: true, SizeBytes: info.Size(), Compression: "tar.gz"}, nil
}

func (s *FilesystemStore) Save(_ context.Context, key string, sourceRoot string) (SaveResult, error) {
	archivePath, err := s.resolvePathForKey(key)
	if err != nil {
		return SaveResult{}, err
	}
	if strings.TrimSpace(sourceRoot) == "" {
		return SaveResult{}, errors.New("source root is required")
	}
	if _, statErr := os.Stat(sourceRoot); statErr != nil {
		return SaveResult{}, statErr
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(archivePath), 0o755); mkdirErr != nil {
		return SaveResult{}, mkdirErr
	}

	tmpPath, err := os.CreateTemp(filepath.Dir(archivePath), ".cache-archive-*.tar.gz")
	if err != nil {
		return SaveResult{}, err
	}
	tmpFilePath := tmpPath.Name()
	defer func() { _ = os.Remove(tmpFilePath) }()
	if closeErr := tmpPath.Close(); closeErr != nil {
		return SaveResult{}, closeErr
	}

	if writeErr := writeTarGz(tmpFilePath, sourceRoot); writeErr != nil {
		return SaveResult{}, writeErr
	}

	checksum, size, err := fileDigestAndSize(tmpFilePath)
	if err != nil {
		return SaveResult{}, err
	}

	if err := os.Rename(tmpFilePath, archivePath); err != nil {
		return SaveResult{}, err
	}

	if evictErr := s.evictIfNeeded(); evictErr != nil {
		return SaveResult{}, evictErr
	}

	return SaveResult{SizeBytes: size, Checksum: checksum, Compression: "tar.gz"}, nil
}

func (s *FilesystemStore) TotalSizeBytes() (int64, error) {
	entries, err := s.collectEntries()
	if err != nil {
		return 0, err
	}
	total := int64(0)
	for _, entry := range entries {
		total += entry.size
	}
	return total, nil
}

type cacheEntry struct {
	path    string
	size    int64
	lastUse time.Time
}

func (s *FilesystemStore) evictIfNeeded() error {
	if s.maxSizeBytes <= 0 {
		return nil
	}

	entries, err := s.collectEntries()
	if err != nil {
		return err
	}
	total := int64(0)
	for _, entry := range entries {
		total += entry.size
	}
	if total <= s.maxSizeBytes {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastUse.Before(entries[j].lastUse)
	})

	for _, entry := range entries {
		if total <= s.maxSizeBytes {
			break
		}
		if err := os.Remove(entry.path); err != nil {
			return err
		}
		total -= entry.size
	}

	return nil
}

func (s *FilesystemStore) collectEntries() ([]cacheEntry, error) {
	entries := make([]cacheEntry, 0)
	if strings.TrimSpace(s.root) == "" {
		return entries, errors.New("cache storage root is required")
	}

	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".tar.gz") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, cacheEntry{path: path, size: info.Size(), lastUse: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *FilesystemStore) resolvePathForKey(key string) (string, error) {
	if strings.TrimSpace(s.root) == "" {
		return "", errors.New("cache storage root is required")
	}

	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", ErrInvalidCacheKey
	}
	if strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "/") {
		return "", ErrInvalidCacheKey
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidCacheKey
	}

	full := filepath.Join(s.root, cleaned) + ".tar.gz"
	root := filepath.Clean(s.root)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidCacheKey
	}
	return full, nil
}

func writeTarGz(dstArchivePath string, srcRoot string) error {
	file, err := os.Create(dstArchivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz := gzip.NewWriter(file)
	defer func() { _ = gz.Close() }()
	writer := tar.NewWriter(gz)
	defer func() { _ = writer.Close() }()

	return filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed in cache content: %s", path)
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, "../") {
			return fmt.Errorf("cache content escapes source root: %s", rel)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if writeHeaderErr := writer.WriteHeader(header); writeHeaderErr != nil {
			return writeHeaderErr
		}
		if info.IsDir() {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		_, err = io.Copy(writer, src)
		return err
	})
}

func extractTarGz(srcArchivePath string, destinationRoot string) error {
	file, err := os.Open(srcArchivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		target, err := safeExtractPath(destinationRoot, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(dst, reader); err != nil {
				_ = dst.Close()
				return err
			}
			if err := dst.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry type: %d", header.Typeflag)
		}
	}
}

func safeExtractPath(destinationRoot string, entryName string) (string, error) {
	cleaned := filepath.Clean(strings.ReplaceAll(entryName, "\\", "/"))
	if cleaned == "." || cleaned == "" {
		return filepath.Clean(destinationRoot), nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("archive entry escapes destination: %s", entryName)
	}
	full := filepath.Join(destinationRoot, cleaned)
	rel, err := filepath.Rel(filepath.Clean(destinationRoot), full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes destination: %s", entryName)
	}
	return full, nil
}

func fileDigestAndSize(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
