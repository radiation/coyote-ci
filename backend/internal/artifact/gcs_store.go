package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"cloud.google.com/go/storage"
)

// GCSStoreConfig holds configuration for Google Cloud Storage artifact storage.
type GCSStoreConfig struct {
	Bucket  string // required
	Prefix  string // optional prefix prepended to all keys
	Project string // optional GCP project ID
}

// GCSStore persists and retrieves artifacts from a Google Cloud Storage bucket.
type GCSStore struct {
	client *storage.Client
	bucket string
	prefix string
}

// NewGCSStore creates a GCSStore from a pre-constructed GCS client and config.
// The caller owns the client lifecycle (closing it on shutdown).
func NewGCSStore(client *storage.Client, cfg GCSStoreConfig) (*GCSStore, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is required")
	}
	if client == nil {
		return nil, fmt.Errorf("GCS client is required")
	}

	return &GCSStore{
		client: client,
		bucket: bucket,
		prefix: strings.TrimSpace(cfg.Prefix),
	}, nil
}

func (s *GCSStore) ResolveStorageKey(key string) string {
	return s.objectName(key)
}

func (s *GCSStore) Save(ctx context.Context, key string, src io.Reader) (int64, error) {
	if err := validateKey(key); err != nil {
		return 0, err
	}

	objectName := s.objectName(key)
	obj := s.client.Bucket(s.bucket).Object(objectName)
	writer := obj.NewWriter(ctx)

	written, err := io.Copy(writer, src)
	if err != nil {
		_ = writer.Close()
		return 0, fmt.Errorf("writing artifact to GCS: %w", err)
	}

	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("finalizing GCS artifact upload: %w", err)
	}

	return written, nil
}

func (s *GCSStore) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}

	objectName := s.objectName(key)
	_, err := s.client.Bucket(s.bucket).Object(objectName).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}

	return false, fmt.Errorf("checking artifact in GCS: %w", err)
}

func (s *GCSStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}

	objectName := s.objectName(key)
	reader, err := s.client.Bucket(s.bucket).Object(objectName).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening artifact from GCS: %w", err)
	}
	return reader, nil
}

func (s *GCSStore) objectName(key string) string {
	trimmed := strings.TrimSpace(key)
	if s.prefix == "" {
		return trimmed
	}
	if trimmed == s.prefix || strings.HasPrefix(trimmed, s.prefix+"/") {
		return trimmed
	}
	return path.Join(s.prefix, trimmed)
}
