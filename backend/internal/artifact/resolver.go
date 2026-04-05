package artifact

import (
	"fmt"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

// StoreResolver maps storage providers to their Store implementations and
// routes reads to the correct backend based on persisted artifact metadata.
type StoreResolver struct {
	stores          map[domain.StorageProvider]Store
	defaultProvider domain.StorageProvider
}

// NewStoreResolver creates a resolver with the given default and store map.
func NewStoreResolver(defaultProvider domain.StorageProvider, stores map[domain.StorageProvider]Store) *StoreResolver {
	return &StoreResolver{
		stores:          stores,
		defaultProvider: defaultProvider,
	}
}

// Resolve returns the Store for the given provider.
func (r *StoreResolver) Resolve(provider domain.StorageProvider) (Store, error) {
	if s, ok := r.stores[provider]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("no store configured for provider %q", provider)
}

// Default returns the Store for the configured default provider.
func (r *StoreResolver) Default() Store {
	return r.stores[r.defaultProvider]
}

// DefaultProvider returns the configured default storage provider.
func (r *StoreResolver) DefaultProvider() domain.StorageProvider {
	return r.defaultProvider
}
