package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidStorageKey = errors.New("invalid storage key")

// FilesystemStore persists artifacts under a configured root directory.
type FilesystemStore struct {
	root string
}

func NewFilesystemStore(root string) *FilesystemStore {
	return &FilesystemStore{root: strings.TrimSpace(root)}
}

func (s *FilesystemStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	fullPath, err := s.resolvePath(key)
	if err != nil {
		return 0, err
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkdirErr != nil {
		return 0, fmt.Errorf("creating artifact directory: %w", mkdirErr)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), ".artifact-*")
	if err != nil {
		return 0, fmt.Errorf("creating artifact temp file: %w", err)
	}

	wrote := int64(0)
	defer func() {
		_ = tmpFile.Close()
	}()

	wrote, err = io.Copy(tmpFile, src)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return 0, fmt.Errorf("writing artifact content: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return 0, fmt.Errorf("syncing artifact content: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return 0, fmt.Errorf("closing artifact temp file: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), fullPath); err != nil {
		_ = os.Remove(tmpFile.Name())
		return 0, fmt.Errorf("moving artifact into place: %w", err)
	}

	return wrote, nil
}

func (s *FilesystemStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath, err := s.resolvePath(key)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}

	return file, nil
}

func (s *FilesystemStore) resolvePath(key string) (string, error) {
	if strings.TrimSpace(s.root) == "" {
		return "", errors.New("artifact storage root is required")
	}

	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "", ErrInvalidStorageKey
	}
	if strings.Contains(trimmedKey, "\\") {
		return "", ErrInvalidStorageKey
	}
	if strings.HasPrefix(trimmedKey, "/") {
		return "", ErrInvalidStorageKey
	}

	cleaned := filepath.Clean(trimmedKey)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidStorageKey
	}

	fullPath := filepath.Join(s.root, cleaned)
	rootPath := filepath.Clean(s.root)
	if rel, err := filepath.Rel(rootPath, fullPath); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidStorageKey
	}

	return fullPath, nil
}
