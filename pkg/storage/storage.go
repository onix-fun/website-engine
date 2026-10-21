// Package storage defines the object-storage contract used by media.
package storage

import (
	"context"
	"io"
)

type PutOptions struct {
	ContentType  string
	CacheControl string
}

// ObjectStore stores binary media. Implementations must not expose secrets in
// URLs and must generate public URLs only from trusted configuration.
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	PublicURL(key string) (string, error)
	Ready(ctx context.Context) error
}
