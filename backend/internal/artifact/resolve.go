package artifact

import (
	"context"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

// StoreConfig holds the configuration needed to resolve artifact stores.
type StoreConfig struct {
	Provider    string // "filesystem", "gcs"
	StorageRoot string // filesystem root directory
	GCSBucket   string
	GCSPrefix   string
	GCSProject  string
	Strict      bool // if true, fail on GCS errors instead of falling back to filesystem
}

// ResolveStores builds a StoreResolver from configuration, always registering
// the filesystem store and conditionally registering GCS. When Strict is true,
// GCS initialization errors are fatal rather than falling back silently.
func ResolveStores(cfg StoreConfig) (*StoreResolver, error) {
	stores := make(map[domain.StorageProvider]Store)
	fsStore := NewFilesystemStore(cfg.StorageRoot)
	stores[domain.StorageProviderFilesystem] = fsStore

	defaultProvider := domain.StorageProviderFilesystem

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "gcs":
		if cfg.GCSBucket == "" {
			if cfg.Strict {
				return nil, fmt.Errorf("ARTIFACT_STORAGE_PROVIDER=gcs but ARTIFACT_GCS_BUCKET is empty")
			}
			log.Printf("ARTIFACT_STORAGE_PROVIDER=gcs but ARTIFACT_GCS_BUCKET is empty; falling back to filesystem")
			break
		}
		ctx := context.Background()
		client, err := storage.NewClient(ctx)
		if err != nil {
			if cfg.Strict {
				return nil, fmt.Errorf("failed to create GCS client: %w", err)
			}
			log.Printf("failed to create GCS client: %v; falling back to filesystem", err)
			break
		}
		gcsStore, err := NewGCSStore(client, GCSStoreConfig{
			Bucket:  cfg.GCSBucket,
			Prefix:  cfg.GCSPrefix,
			Project: cfg.GCSProject,
		})
		if err != nil {
			if cfg.Strict {
				return nil, fmt.Errorf("failed to create GCS artifact store: %w", err)
			}
			log.Printf("failed to create GCS artifact store: %v; falling back to filesystem", err)
			break
		}
		stores[domain.StorageProviderGCS] = gcsStore
		defaultProvider = domain.StorageProviderGCS
		log.Printf("artifact storage: gcs bucket=%s prefix=%s", cfg.GCSBucket, cfg.GCSPrefix)
	}

	return NewStoreResolver(defaultProvider, stores), nil
}
