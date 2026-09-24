package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WorkspaceRevisionStoreConfig struct {
	Provider    string
	StorageRoot string
	GCSBucket   string
	GCSPrefix   string
	Strict      bool
}

type WorkspaceRevisionStoreResolver struct {
	defaultStore WorkspaceRevisionStore
	stores       map[domain.StorageProvider]WorkspaceRevisionStore
}

var newWorkspaceRevisionGCSClient = storage.NewClient

func ResolveWorkspaceRevisionStore(cfg WorkspaceRevisionStoreConfig) (*WorkspaceRevisionStoreResolver, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}
	stores := make(map[domain.StorageProvider]WorkspaceRevisionStore)
	if root := strings.TrimSpace(cfg.StorageRoot); root != "" {
		stores[domain.StorageProviderFilesystem] = NewFilesystemWorkspaceRevisionStore(root)
	}
	if bucket := strings.TrimSpace(cfg.GCSBucket); bucket != "" {
		client, clientErr := newWorkspaceRevisionGCSClient(context.Background())
		if clientErr != nil {
			if provider == string(domain.StorageProviderGCS) || cfg.Strict {
				return nil, fmt.Errorf("create gcs workspace revision client: %w", clientErr)
			}
		} else {
			gcsStore, storeErr := NewGCSWorkspaceRevisionStore(client, GCSWorkspaceRevisionStoreConfig{Bucket: bucket, Prefix: cfg.GCSPrefix})
			if storeErr != nil {
				_ = client.Close()
				return nil, storeErr
			}
			stores[domain.StorageProviderGCS] = gcsStore
		}
	}

	var defaultStore WorkspaceRevisionStore
	switch domain.StorageProvider(provider) {
	case domain.StorageProviderFilesystem:
		defaultStore = stores[domain.StorageProviderFilesystem]
	case domain.StorageProviderGCS:
		if strings.TrimSpace(cfg.GCSBucket) == "" {
			return nil, errors.New("WORKSPACE_REVISION_STORAGE_PROVIDER=gcs but WORKSPACE_REVISION_GCS_BUCKET is empty")
		}
		defaultStore = stores[domain.StorageProviderGCS]
	default:
		return nil, fmt.Errorf("unsupported workspace revision storage provider %q", cfg.Provider)
	}
	if defaultStore == nil {
		return nil, fmt.Errorf("workspace revision storage provider %q is not configured", provider)
	}
	return &WorkspaceRevisionStoreResolver{defaultStore: defaultStore, stores: stores}, nil
}

func (r *WorkspaceRevisionStoreResolver) Provider() domain.StorageProvider {
	return r.defaultStore.Provider()
}

func (r *WorkspaceRevisionStoreResolver) Publish(ctx context.Context, revisionID string, sourceRoot string) (domain.WorkspaceRevisionPublication, error) {
	return r.defaultStore.Publish(ctx, revisionID, sourceRoot)
}

func (r *WorkspaceRevisionStoreResolver) Restore(ctx context.Context, publication domain.WorkspaceRevisionPublication, destinationRoot string) error {
	store, err := r.storeForPublication(publication)
	if err != nil {
		return err
	}
	return store.Restore(ctx, publication, destinationRoot)
}

func (r *WorkspaceRevisionStoreResolver) Open(ctx context.Context, publication domain.WorkspaceRevisionPublication) (io.ReadCloser, error) {
	store, err := r.storeForPublication(publication)
	if err != nil {
		return nil, err
	}
	archiveStore, ok := store.(WorkspaceRevisionArchiveReader)
	if !ok {
		return nil, errors.New("workspace revision store does not support archive reads")
	}
	return archiveStore.Open(ctx, publication)
}

func (r *WorkspaceRevisionStoreResolver) Delete(ctx context.Context, publication domain.WorkspaceRevisionPublication) error {
	store, err := r.storeForPublication(publication)
	if err != nil {
		return err
	}
	return store.Delete(ctx, publication)
}

func (r *WorkspaceRevisionStoreResolver) storeForPublication(publication domain.WorkspaceRevisionPublication) (WorkspaceRevisionStore, error) {
	if publication.Validate() != nil {
		return nil, ErrInvalidWorkspaceRevisionObject
	}
	store := r.stores[publication.StorageProvider]
	if store == nil {
		return nil, fmt.Errorf("workspace revision storage provider %q is not configured", publication.StorageProvider)
	}
	return store, nil
}

var _ WorkspaceRevisionStore = (*WorkspaceRevisionStoreResolver)(nil)
var _ WorkspaceRevisionArchiveReader = (*WorkspaceRevisionStoreResolver)(nil)
