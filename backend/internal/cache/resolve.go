package cache

import (
	"context"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/storage"
)

type StoreConfig struct {
	Provider    string
	StorageRoot string
	MaxSizeMB   int
	GCSBucket   string
	GCSPrefix   string
	GCSProject  string
	Strict      bool
}

func ResolveStore(cfg StoreConfig) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "filesystem":
		return NewFilesystemStoreWithMaxSize(cfg.StorageRoot, int64(cfg.MaxSizeMB)*1024*1024), nil
	case "gcs":
		if strings.TrimSpace(cfg.GCSBucket) == "" {
			if cfg.Strict {
				return nil, fmt.Errorf("WORKER_CACHE_STORAGE_PROVIDER=gcs but WORKER_CACHE_GCS_BUCKET is empty")
			}
			log.Printf("cache storage provider gcs configured but bucket is empty; falling back to filesystem")
			return NewFilesystemStoreWithMaxSize(cfg.StorageRoot, int64(cfg.MaxSizeMB)*1024*1024), nil
		}
		client, err := storage.NewClient(context.Background())
		if err != nil {
			if cfg.Strict {
				return nil, fmt.Errorf("create gcs cache store client: %w", err)
			}
			log.Printf("failed to initialize gcs cache store: %v; falling back to filesystem", err)
			return NewFilesystemStoreWithMaxSize(cfg.StorageRoot, int64(cfg.MaxSizeMB)*1024*1024), nil
		}
		store, err := NewGCSStore(client, GCSStoreConfig{
			Bucket:  cfg.GCSBucket,
			Prefix:  cfg.GCSPrefix,
			Project: cfg.GCSProject,
		})
		if err != nil {
			if cfg.Strict {
				return nil, err
			}
			log.Printf("failed to initialize gcs cache store: %v; falling back to filesystem", err)
			return NewFilesystemStoreWithMaxSize(cfg.StorageRoot, int64(cfg.MaxSizeMB)*1024*1024), nil
		}
		return store, nil
	default:
		return nil, fmt.Errorf("unsupported cache storage provider %q", cfg.Provider)
	}
}
