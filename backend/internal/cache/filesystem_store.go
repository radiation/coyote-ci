package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidCacheKey = errors.New("invalid cache key")

type FilesystemStore struct {
	root string
}

func NewFilesystemStore(root string) *FilesystemStore {
	return &FilesystemStore{root: strings.TrimSpace(root)}
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
	return nil
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
