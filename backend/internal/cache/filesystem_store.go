package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

func (s *FilesystemStore) Restore(_ context.Context, key string, destinationRoot string) (bool, error) {
	entryPath, err := s.resolvePathForKey(key)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(destinationRoot) == "" {
		return false, errors.New("destination root is required")
	}

	info, err := os.Stat(entryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("cache entry is not a directory: %s", entryPath)
	}

	if mkdirErr := os.MkdirAll(destinationRoot, 0o755); mkdirErr != nil {
		return false, mkdirErr
	}
	if copyErr := copyDir(entryPath, destinationRoot); copyErr != nil {
		return false, copyErr
	}
	now := time.Now().UTC()
	_ = os.Chtimes(entryPath, now, now)
	return true, nil
}

func (s *FilesystemStore) Save(_ context.Context, key string, sourceRoot string) error {
	entryPath, err := s.resolvePathForKey(key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sourceRoot) == "" {
		return errors.New("source root is required")
	}

	if _, statErr := os.Stat(sourceRoot); statErr != nil {
		return statErr
	}

	if evictErr := s.evictIfNeeded(); evictErr != nil {
		return evictErr
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(entryPath), 0o755); mkdirErr != nil {
		return mkdirErr
	}

	tmpPath, err := os.MkdirTemp(filepath.Dir(entryPath), ".cache-entry-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tmpPath)
	}()

	if copyErr := copyDir(sourceRoot, tmpPath); copyErr != nil {
		return copyErr
	}

	if removeErr := os.RemoveAll(entryPath); removeErr != nil {
		return removeErr
	}
	if renameErr := os.Rename(tmpPath, entryPath); renameErr != nil {
		return renameErr
	}
	now := time.Now().UTC()
	_ = os.Chtimes(entryPath, now, now)

	if evictErr := s.evictIfNeeded(); evictErr != nil {
		return evictErr
	}
	return nil
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

	entries, totalSize, err := s.collectEntries()
	if err != nil {
		return err
	}
	if totalSize <= s.maxSizeBytes {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastUse.Before(entries[j].lastUse)
	})

	evictedCount := 0
	bytesReclaimed := int64(0)
	for _, entry := range entries {
		if totalSize <= s.maxSizeBytes {
			break
		}
		if removeErr := os.RemoveAll(entry.path); removeErr != nil {
			return removeErr
		}
		totalSize -= entry.size
		bytesReclaimed += entry.size
		evictedCount++
	}

	if evictedCount > 0 {
		log.Printf("cache eviction: entries_evicted=%d bytes_reclaimed=%d", evictedCount, bytesReclaimed)
	}

	return nil
}

func (s *FilesystemStore) collectEntries() ([]cacheEntry, int64, error) {
	entries := make([]cacheEntry, 0)
	totalSize := int64(0)
	if strings.TrimSpace(s.root) == "" {
		return entries, 0, errors.New("cache storage root is required")
	}

	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return entries, 0, nil
		}
		return nil, 0, err
	}

	walkErr := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path == s.root {
			return nil
		}

		pathsDir := filepath.Join(path, "paths")
		pathsInfo, statErr := os.Stat(pathsDir)
		if statErr != nil || !pathsInfo.IsDir() {
			return nil
		}

		size, lastUse, entryErr := dirSizeAndModTime(path)
		if entryErr != nil {
			return entryErr
		}
		entries = append(entries, cacheEntry{path: path, size: size, lastUse: lastUse})
		totalSize += size

		return filepath.SkipDir
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}

	return entries, totalSize, nil
}

func dirSizeAndModTime(root string) (int64, time.Time, error) {
	size := int64(0)
	latest := time.Time{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, infoErr := os.Lstat(path)
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			mod := info.ModTime()
			if mod.After(latest) {
				latest = mod
			}
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		mod := info.ModTime()
		if mod.After(latest) {
			latest = mod
		}
		return nil
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	return size, latest, nil
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

	full := filepath.Join(s.root, cleaned)
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

func copyDir(srcRoot string, dstRoot string) error {
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
		if rel == "." {
			return nil
		}

		dst := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkdirErr != nil {
			return mkdirErr
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}

		dstFile, err := os.Create(dst)
		if err != nil {
			_ = srcFile.Close()
			return err
		}
		if _, copyErr := io.Copy(dstFile, srcFile); copyErr != nil {
			_ = dstFile.Close()
			_ = srcFile.Close()
			return copyErr
		}
		if closeErr := dstFile.Close(); closeErr != nil {
			_ = srcFile.Close()
			return closeErr
		}
		if closeErr := srcFile.Close(); closeErr != nil {
			return closeErr
		}
		return nil
	})
}
