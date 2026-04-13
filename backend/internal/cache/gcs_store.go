package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type GCSStoreConfig struct {
	Bucket string
	Prefix string
}

type GCSStore struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCSStore(client *storage.Client, cfg GCSStoreConfig) (*GCSStore, error) {
	if client == nil {
		return nil, errors.New("gcs client is required")
	}
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("gcs cache bucket is required")
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	return &GCSStore{client: client, bucket: bucket, prefix: prefix}, nil
}

func (s *GCSStore) Provider() domain.StorageProvider {
	return domain.StorageProviderGCS
}

func (s *GCSStore) Restore(ctx context.Context, key string, destinationRoot string) (RestoreResult, error) {
	objectKey := s.objectKey(key)
	reader, err := s.client.Bucket(s.bucket).Object(objectKey).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return RestoreResult{Hit: false, Compression: "tar.gz"}, nil
		}
		return RestoreResult{}, err
	}
	defer func() { _ = reader.Close() }()

	tmp, err := os.CreateTemp("", "coyote-cache-download-*.tar.gz")
	if err != nil {
		return RestoreResult{}, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, readErr := tmp.ReadFrom(reader); readErr != nil {
		_ = tmp.Close()
		return RestoreResult{}, readErr
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return RestoreResult{}, closeErr
	}

	if extractErr := extractTarGz(tmpPath, destinationRoot); extractErr != nil {
		return RestoreResult{}, extractErr
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return RestoreResult{}, err
	}

	return RestoreResult{Hit: true, SizeBytes: info.Size(), Compression: "tar.gz"}, nil
}

func (s *GCSStore) Save(ctx context.Context, key string, sourceRoot string) (SaveResult, error) {
	tmp, err := os.CreateTemp("", "coyote-cache-upload-*.tar.gz")
	if err != nil {
		return SaveResult{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if writeErr := writeTarGz(tmpPath, sourceRoot); writeErr != nil {
		return SaveResult{}, writeErr
	}

	checksum, size, err := fileDigestAndSize(tmpPath)
	if err != nil {
		return SaveResult{}, err
	}

	file, err := os.Open(tmpPath)
	if err != nil {
		return SaveResult{}, err
	}
	defer func() { _ = file.Close() }()

	writer := s.client.Bucket(s.bucket).Object(s.objectKey(key)).NewWriter(ctx)
	writer.ContentType = "application/gzip"
	if _, err := io.Copy(writer, file); err != nil {
		_ = writer.Close()
		return SaveResult{}, err
	}
	if err := writer.Close(); err != nil {
		return SaveResult{}, err
	}

	return SaveResult{SizeBytes: size, Checksum: checksum, Compression: "tar.gz"}, nil
}

func (s *GCSStore) objectKey(key string) string {
	trimmed := strings.Trim(strings.TrimSpace(key), "/")
	if s.prefix == "" {
		return fmt.Sprintf("%s.tar.gz", trimmed)
	}
	return path.Join(s.prefix, fmt.Sprintf("%s.tar.gz", trimmed))
}
