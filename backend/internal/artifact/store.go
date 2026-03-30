package artifact

import (
	"context"
	"io"
)

// Store persists and streams artifact content by opaque storage key.
type Store interface {
	Save(ctx context.Context, key string, src io.Reader) (int64, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
