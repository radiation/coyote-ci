package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type GCSWorkspaceRevisionStoreConfig struct {
	Bucket string
	Prefix string
}

type workspaceRevisionObjectStore interface {
	CreateIfAbsent(context.Context, string, io.Reader) (bool, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type GCSWorkspaceRevisionStore struct {
	objects workspaceRevisionObjectStore
	prefix  string
}

func NewGCSWorkspaceRevisionStore(client *storage.Client, cfg GCSWorkspaceRevisionStoreConfig) (*GCSWorkspaceRevisionStore, error) {
	if client == nil {
		return nil, errors.New("gcs workspace revision client is required")
	}
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("gcs workspace revision bucket is required")
	}
	return newGCSWorkspaceRevisionStore(&gcsWorkspaceRevisionObjectStore{bucket: client.Bucket(bucket)}, cfg.Prefix)
}

func newGCSWorkspaceRevisionStore(objects workspaceRevisionObjectStore, prefix string) (*GCSWorkspaceRevisionStore, error) {
	if objects == nil {
		return nil, errors.New("gcs workspace revision object store is required")
	}
	return &GCSWorkspaceRevisionStore{objects: objects, prefix: strings.Trim(strings.TrimSpace(prefix), "/")}, nil
}

func (*GCSWorkspaceRevisionStore) Provider() domain.StorageProvider {
	return domain.StorageProviderGCS
}

func (s *GCSWorkspaceRevisionStore) Publish(ctx context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error) {
	storageKey, err := workspaceRevisionStorageKey(revisionID)
	if err != nil {
		return domain.WorkspaceRevisionPublication{}, err
	}
	archive, createErr := os.CreateTemp("", "coyote-workspace-revision-*.tar.gz")
	if createErr != nil {
		return domain.WorkspaceRevisionPublication{}, createErr
	}
	archivePath := archive.Name()
	defer func() { _ = os.Remove(archivePath) }()

	if writeErr := writeWorkspaceRevisionArchive(ctx, archive, sourceRoot); writeErr != nil {
		_ = archive.Close()
		return domain.WorkspaceRevisionPublication{}, writeErr
	}
	if closeErr := archive.Close(); closeErr != nil {
		return domain.WorkspaceRevisionPublication{}, closeErr
	}
	digest, size, digestErr := workspaceRevisionDigestAndSize(ctx, archivePath)
	if digestErr != nil {
		return domain.WorkspaceRevisionPublication{}, digestErr
	}
	publication := domain.WorkspaceRevisionPublication{
		ContentDigest:   digest,
		StorageKey:      storageKey,
		StorageProvider: domain.StorageProviderGCS,
		SizeBytes:       &size,
	}

	source, openErr := os.Open(archivePath)
	if openErr != nil {
		return domain.WorkspaceRevisionPublication{}, openErr
	}
	created, uploadErr := s.objects.CreateIfAbsent(ctx, s.objectName(storageKey), source)
	closeErr := source.Close()
	if uploadErr != nil {
		return domain.WorkspaceRevisionPublication{}, uploadErr
	}
	if closeErr != nil {
		return domain.WorkspaceRevisionPublication{}, closeErr
	}
	if created {
		return publication, nil
	}

	existing, existingErr := s.Open(ctx, publication)
	if existingErr != nil {
		return domain.WorkspaceRevisionPublication{}, existingErr
	}
	validateErr := ValidateArchiveWithLimits(ctx, existing, publication, WorkspaceRevisionRestoreLimits{})
	existingCloseErr := existing.Close()
	if validateErr != nil {
		if errors.Is(validateErr, ErrWorkspaceRevisionDigestMismatch) {
			return domain.WorkspaceRevisionPublication{}, ErrWorkspaceRevisionConflict
		}
		return domain.WorkspaceRevisionPublication{}, validateErr
	}
	if existingCloseErr != nil {
		return domain.WorkspaceRevisionPublication{}, existingCloseErr
	}
	return publication, nil
}

func (s *GCSWorkspaceRevisionStore) Restore(ctx context.Context, publication domain.WorkspaceRevisionPublication, destinationRoot string) error {
	if err := s.validatePublication(publication); err != nil {
		return err
	}
	archive, openErr := s.Open(ctx, publication)
	if openErr != nil {
		return openErr
	}
	defer func() { _ = archive.Close() }()
	return RestoreArchive(ctx, archive, publication, destinationRoot)
}

func (s *GCSWorkspaceRevisionStore) Open(ctx context.Context, publication domain.WorkspaceRevisionPublication) (io.ReadCloser, error) {
	if err := s.validatePublication(publication); err != nil {
		return nil, err
	}
	reader, err := s.objects.Open(ctx, s.objectName(publication.StorageKey))
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, ErrWorkspaceRevisionNotFound
	}
	return reader, err
}

func (s *GCSWorkspaceRevisionStore) Delete(ctx context.Context, publication domain.WorkspaceRevisionPublication) error {
	if err := s.validatePublication(publication); err != nil {
		return err
	}
	err := s.objects.Delete(ctx, s.objectName(publication.StorageKey))
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	return err
}

func (s *GCSWorkspaceRevisionStore) validatePublication(publication domain.WorkspaceRevisionPublication) error {
	if publication.Validate() != nil || publication.StorageProvider != domain.StorageProviderGCS || !validWorkspaceRevisionStorageKey(publication.StorageKey) {
		return ErrInvalidWorkspaceRevisionObject
	}
	return nil
}

func (s *GCSWorkspaceRevisionStore) objectName(storageKey string) string {
	if s.prefix == "" {
		return storageKey
	}
	return path.Join(s.prefix, storageKey)
}

type gcsWorkspaceRevisionObjectStore struct {
	bucket *storage.BucketHandle
}

func (s *gcsWorkspaceRevisionObjectStore) CreateIfAbsent(ctx context.Context, objectName string, source io.Reader) (bool, error) {
	writer := s.bucket.Object(objectName).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	if _, copyErr := io.Copy(writer, source); copyErr != nil {
		_ = writer.Close()
		return false, copyErr
	}
	if closeErr := writer.Close(); closeErr != nil {
		if isGCSPreconditionFailure(closeErr) {
			return false, nil
		}
		return false, closeErr
	}
	return true, nil
}

func (s *gcsWorkspaceRevisionObjectStore) Open(ctx context.Context, objectName string) (io.ReadCloser, error) {
	return s.bucket.Object(objectName).NewReader(ctx)
}

func (s *gcsWorkspaceRevisionObjectStore) Delete(ctx context.Context, objectName string) error {
	return s.bucket.Object(objectName).Delete(ctx)
}

func isGCSPreconditionFailure(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 412
}

func workspaceRevisionStorageKey(revisionID string) (string, error) {
	trimmed := strings.TrimSpace(revisionID)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\\`) || trimmed == "." || trimmed == ".." {
		return "", ErrInvalidWorkspaceRevisionObject
	}
	return path.Join("workspace-revisions", trimmed+".tar.gz"), nil
}

func validWorkspaceRevisionStorageKey(storageKey string) bool {
	if strings.Contains(storageKey, "\\") || path.IsAbs(storageKey) || !strings.HasPrefix(storageKey, "workspace-revisions/") || !strings.HasSuffix(storageKey, ".tar.gz") {
		return false
	}
	return path.Clean(storageKey) == storageKey && storageKey != "workspace-revisions"
}
