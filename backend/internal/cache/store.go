package cache

import (
	"context"
	"io"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type SaveResult struct {
	SizeBytes   int64
	Checksum    string
	Compression string
}

type RestoreResult struct {
	Hit         bool
	SizeBytes   int64
	Compression string
}

// Store persists cache snapshots keyed by a deterministic cache key.
type Store interface {
	Provider() domain.StorageProvider
	// Restore copies a stored snapshot into destinationRoot.
	Restore(ctx context.Context, key string, destinationRoot string) (RestoreResult, error)
	// Save stores a snapshot from sourceRoot for key.
	Save(ctx context.Context, key string, sourceRoot string) (SaveResult, error)
}

// ArchiveStore is an optional capability for stores that can transfer the
// durable tar.gz representation without extracting and recreating it.
type ArchiveStore interface {
	Open(ctx context.Context, key string) (io.ReadCloser, RestoreResult, error)
	SaveArchive(ctx context.Context, key string, archive io.Reader) (SaveResult, error)
	PromoteArchive(ctx context.Context, sourceKey string, destinationKey string) error
	DeleteArchive(ctx context.Context, key string) error
}
