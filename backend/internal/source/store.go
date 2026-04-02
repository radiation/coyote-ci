package source

import (
	"context"
	"io"
)

// Store persists source bundles outside the control-plane database.
//
// Implementations may target object storage (S3/GCS/Azure Blob) or local disk
// for development workflows. Postgres should store only metadata references.
type Store interface {
	Save(ctx context.Context, key string, src io.Reader) (int64, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
