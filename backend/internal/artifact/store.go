package artifact

import (
	"context"
	"errors"
	"io"
	"os"
)

// Store persists and streams artifact content by opaque storage key.
type Store interface {
	Save(ctx context.Context, key string, src io.Reader) (int64, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// KeyResolver maps a generated logical storage key to the final provider-native
// key that will be persisted in artifact metadata and later used for reads.
//
// Contract:
//   - Implementations should be deterministic and idempotent.
//   - Calling ResolveStorageKey multiple times with an already-resolved key
//     should return the same key.
//   - Resolution is expected to happen before Save so the exact same key can be
//     passed to Save and persisted in metadata.
//   - Implementations should avoid I/O and perform key translation only.
type KeyResolver interface {
	ResolveStorageKey(key string) string
}

// ExistenceChecker allows a store to provide efficient existence checks without
// requiring a full blob read.
type ExistenceChecker interface {
	Exists(ctx context.Context, key string) (bool, error)
}

func resolveStorageKey(store Store, key string) string {
	resolver, ok := store.(KeyResolver)
	if !ok {
		return key
	}

	resolved := resolver.ResolveStorageKey(key)
	if resolved == "" {
		return key
	}

	return resolved
}

// Exists checks whether a blob key exists in a store.
//
// If the store provides ExistenceChecker, it is used directly.
// Otherwise, Exists falls back to Open+Close semantics and maps
// os.ErrNotExist to a false, nil result.
func Exists(ctx context.Context, store Store, key string) (bool, error) {
	if store == nil {
		return false, errors.New("artifact store is required")
	}

	if checker, ok := store.(ExistenceChecker); ok {
		return checker.Exists(ctx, key)
	}

	rc, err := store.Open(ctx, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	_ = rc.Close()

	return true, nil
}
