package submission

import (
	"bytes"
	"context"
	"io"
	"testing"

	"SOJ/internal/storage"
)

func TestObjectSourceStoreCreatesUniqueKeysForRepeatedSource(t *testing.T) {
	store := NewObjectSourceStore(&fakeObjectStorage{objects: map[string][]byte{}})
	ctx := context.Background()

	first, err := store.Put(ctx, "submission", 5, []byte("package main"))
	if err != nil {
		t.Fatalf("first Put returned error: %v", err)
	}
	second, err := store.Put(ctx, "submission", 5, []byte("package main"))
	if err != nil {
		t.Fatalf("second Put returned error: %v", err)
	}
	if first.StorageKey == second.StorageKey {
		t.Fatalf("storage keys both %q", first.StorageKey)
	}
	if first.ChecksumSHA256 != second.ChecksumSHA256 {
		t.Fatalf("checksums differ: %q != %q", first.ChecksumSHA256, second.ChecksumSHA256)
	}
}

type fakeObjectStorage struct {
	objects map[string][]byte
}

func (s *fakeObjectStorage) Put(ctx context.Context, object storage.Object) (storage.ObjectInfo, error) {
	data, err := io.ReadAll(object.Body)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	s.objects[object.Key] = data
	return storage.ObjectInfo{Key: object.Key, Size: object.Size, ContentType: object.ContentType}, nil
}

func (s *fakeObjectStorage) Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(s.objects[key])), storage.ObjectInfo{Key: key, Size: int64(len(s.objects[key]))}, nil
}

func (s *fakeObjectStorage) Delete(ctx context.Context, key string) error { return nil }

func (s *fakeObjectStorage) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{Key: key, Size: int64(len(s.objects[key]))}, nil
}
