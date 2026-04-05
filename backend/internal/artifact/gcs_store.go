package artifact

import (
	"context"
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

	return &GCSStore{
		client: client,
		bucket: bucket,
		prefix: strings.TrimSpace(cfg.Prefix),
	}, nil
}

func (s *GCSStore) Save(ctx context.Context, key string, src io.Reader) (int64, error) {
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

func (s *GCSStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	objectName := s.objectName(key)
	reader, err := s.client.Bucket(s.bucket).Object(objectName).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening artifact from GCS: %w", err)
	}
	return reader, nil
}

func (s *GCSStore) objectName(key string) string {
	if s.prefix != "" {
		return path.Join(s.prefix, key)
	}
	return key
}
