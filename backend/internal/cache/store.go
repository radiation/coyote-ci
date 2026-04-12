package cache

import "context"

// Store persists cache snapshots keyed by a deterministic cache key.
type Store interface {
	// Restore copies a stored snapshot into destinationRoot.
	// Returns hit=true when a snapshot existed for key.
	Restore(ctx context.Context, key string, destinationRoot string) (bool, error)
	// Save stores a snapshot from sourceRoot for key.
	Save(ctx context.Context, key string, sourceRoot string) error
}
