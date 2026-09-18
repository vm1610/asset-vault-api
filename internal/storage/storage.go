// Package storage abstracts object storage behind a single interface so the
// API and worker layers can run against a local filesystem backend in
// development and tests, or against S3 in production, without changing
// their code.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotFound = errors.New("storage: object not found")

// Storage persists and retrieves binary objects identified by key.
type Storage interface {
	// Put streams src into the object identified by key.
	Put(ctx context.Context, key string, src io.Reader, size int64, contentType string) error

	// Get returns a reader for the object identified by key. Callers must
	// close the returned ReadCloser.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object identified by key. Deleting a missing key
	// is not an error.
	Delete(ctx context.Context, key string) error

	// PresignGet returns a time-limited URL that can be used to download the
	// object identified by key without further authentication.
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}
