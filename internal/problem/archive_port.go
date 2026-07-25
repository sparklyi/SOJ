package problem

import (
	"context"
	"io"

	"SOJ/internal/storage"
)

type testcaseArchiveReader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error)
}

type testcaseArchiveWriter interface {
	Put(ctx context.Context, object storage.Object) (storage.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}
