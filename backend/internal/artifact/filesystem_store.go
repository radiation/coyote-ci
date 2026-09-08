package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidStorageKey = errors.New("invalid storage key")

// FilesystemStore persists artifacts under a configured root directory.
type FilesystemStore struct {
	root string
}

func NewFilesystemStore(root string) *FilesystemStore {
	return &FilesystemStore{root: strings.TrimSpace(root)}
}

func (s *FilesystemStore) RootPath() string {
	return s.root
}

func (s *FilesystemStore) ResolveStorageKey(key string) string {
	return key
}

func (s *FilesystemStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	storageKey, err := validateStorageKey(key)
	if err != nil {
		return 0, err
	}
	rootPath, rootErr := s.storageRoot()
	if rootErr != nil {
		return 0, rootErr
	}
	if mkdirErr := os.MkdirAll(rootPath, 0o755); mkdirErr != nil {
		return 0, fmt.Errorf("creating artifact storage root: %w", mkdirErr)
	}
	root, openErr := os.OpenRoot(rootPath)
	if openErr != nil {
		return 0, fmt.Errorf("opening artifact storage root: %w", openErr)
	}
	defer func() { _ = root.Close() }()
	if mkdirErr := root.MkdirAll(path.Dir(storageKey), 0o755); mkdirErr != nil {
		return 0, fmt.Errorf("creating artifact directory: %w", mkdirErr)
	}
	temporaryKey := path.Join(path.Dir(storageKey), ".artifact-"+uuid.NewString())
	tmpFile, createErr := root.OpenFile(temporaryKey, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if createErr != nil {
		return 0, fmt.Errorf("creating artifact temp file: %w", createErr)
	}
	defer func() { _ = root.Remove(temporaryKey) }()
	wrote := int64(0)
	defer func() {
		_ = tmpFile.Close()
	}()

	wrote, err = io.Copy(tmpFile, src)
	if err != nil {
		return 0, fmt.Errorf("writing artifact content: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return 0, fmt.Errorf("syncing artifact content: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return 0, fmt.Errorf("closing artifact temp file: %w", err)
	}

	if err := root.Rename(temporaryKey, storageKey); err != nil {
		return 0, fmt.Errorf("moving artifact into place: %w", err)
	}

	return wrote, nil
}

func (s *FilesystemStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	storageKey, err := validateStorageKey(key)
	if err != nil {
		return nil, err
	}
	root, rootErr := s.openRoot()
	if rootErr != nil {
		return nil, rootErr
	}
	file, openErr := root.Open(storageKey)
	if openErr != nil {
		_ = root.Close()
		return nil, openErr
	}
	return &rootFile{File: file, root: root}, nil
}

func (s *FilesystemStore) Exists(_ context.Context, key string) (bool, error) {
	storageKey, err := validateStorageKey(key)
	if err != nil {
		return false, err
	}
	root, rootErr := s.openRoot()
	if rootErr != nil {
		if os.IsNotExist(rootErr) {
			return false, nil
		}
		return false, rootErr
	}
	defer func() { _ = root.Close() }()
	_, statErr := root.Stat(storageKey)
	if statErr == nil {
		return true, nil
	}
	if os.IsNotExist(statErr) {
		return false, nil
	}

	return false, statErr
}

type rootFile struct {
	*os.File
	root *os.Root
}

func (f *rootFile) Close() error {
	fileErr := f.File.Close()
	rootErr := f.root.Close()
	if fileErr != nil {
		return fileErr
	}
	return rootErr
}

func (s *FilesystemStore) storageRoot() (string, error) {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "", errors.New("artifact storage root is required")
	}
	return root, nil
}

func (s *FilesystemStore) openRoot() (*os.Root, error) {
	rootPath, err := s.storageRoot()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("opening artifact storage root: %w", err)
	}
	return root, nil
}

func validateStorageKey(key string) (string, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" || trimmedKey == "." || !fs.ValidPath(trimmedKey) {
		return "", ErrInvalidStorageKey
	}
	if strings.Contains(trimmedKey, "\\") {
		return "", ErrInvalidStorageKey
	}
	return trimmedKey, nil
}
