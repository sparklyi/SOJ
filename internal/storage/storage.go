package storage

import (
	"context"
	"io"
	"time"
)

type Object struct {
	Key         string
	ContentType string
	Size        int64
	Metadata    map[string]string
	Body        io.Reader
}

type ObjectInfo struct {
	Key         string
	ContentType string
	Size        int64
	Metadata    map[string]string
	UpdatedAt   time.Time
}

type ObjectStorage interface {
	Put(ctx context.Context, object Object) (ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (ObjectInfo, error)
}
