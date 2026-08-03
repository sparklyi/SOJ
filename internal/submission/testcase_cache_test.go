package submission

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/storage"
)

func TestTestcaseCacheReusesParsedCases(t *testing.T) {
	archive := testcaseCacheArchive(t, map[string]string{
		"input1.txt":  "1 2\n",
		"output1.txt": "3\n",
	})
	store := &testcaseCacheStorage{objects: map[string][]byte{"cases.zip": archive}}
	cache := NewTestcaseCache(store, TestcaseCacheOptions{MaxBytes: 64})
	ref := testcaseCacheRef(archive, 3)

	first, err := cache.Load(t.Context(), ref)
	if err != nil {
		t.Fatalf("first Load returned error: %v", err)
	}
	first[0].InputKey = "changed"
	second, err := cache.Load(t.Context(), ref)
	if err != nil {
		t.Fatalf("second Load returned error: %v", err)
	}
	if store.gets != 1 {
		t.Fatalf("storage gets = %d, want 1", store.gets)
	}
	if second[0].InputKey != "1 2\n" {
		t.Fatalf("cached testcase was mutated: %+v", second[0])
	}
}

func TestTestcaseCacheSingleflightLoadsOnce(t *testing.T) {
	archive := testcaseCacheArchive(t, map[string]string{
		"input1.txt":  "1\n",
		"output1.txt": "1\n",
	})
	store := &testcaseCacheStorage{
		objects: map[string][]byte{"cases.zip": archive},
		delay:   25 * time.Millisecond,
	}
	cache := NewTestcaseCache(store, TestcaseCacheOptions{MaxBytes: 64})
	ref := testcaseCacheRef(archive, 7)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.Load(t.Context(), ref)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Load returned error: %v", err)
		}
	}
	if store.gets != 1 {
		t.Fatalf("storage gets = %d, want 1", store.gets)
	}
}

func TestTestcaseCacheEvictsLeastRecentlyUsedSet(t *testing.T) {
	firstArchive := testcaseCacheArchive(t, map[string]string{
		"input1.txt":  "1234",
		"output1.txt": "5",
	})
	secondArchive := testcaseCacheArchive(t, map[string]string{
		"input1.txt":  "ab",
		"output1.txt": "cde",
	})
	store := &testcaseCacheStorage{objects: map[string][]byte{
		"first.zip":  firstArchive,
		"second.zip": secondArchive,
	}}
	cache := NewTestcaseCache(store, TestcaseCacheOptions{MaxBytes: 5})
	firstRef := testcaseCacheRef(firstArchive, 1)
	firstRef.StorageKey = "first.zip"
	secondRef := testcaseCacheRef(secondArchive, 2)
	secondRef.StorageKey = "second.zip"

	if _, err := cache.Load(t.Context(), firstRef); err != nil {
		t.Fatalf("load first set: %v", err)
	}
	if _, err := cache.Load(t.Context(), secondRef); err != nil {
		t.Fatalf("load second set: %v", err)
	}
	if _, err := cache.Load(t.Context(), firstRef); err != nil {
		t.Fatalf("reload first set: %v", err)
	}
	if store.gets != 3 {
		t.Fatalf("storage gets = %d, want 3 after LRU eviction", store.gets)
	}
}

func TestTestcaseCacheRejectsChecksumMismatch(t *testing.T) {
	archive := testcaseCacheArchive(t, map[string]string{
		"input1.txt":  "1\n",
		"output1.txt": "1\n",
	})
	store := &testcaseCacheStorage{objects: map[string][]byte{"cases.zip": archive}}
	cache := NewTestcaseCache(store, TestcaseCacheOptions{MaxBytes: 64})
	ref := testcaseCacheRef(archive, 3)
	ref.ChecksumSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	if _, err := cache.Load(t.Context(), ref); err == nil {
		t.Fatal("Load returned nil error for checksum mismatch")
	}
	if _, err := cache.Load(t.Context(), ref); err == nil {
		t.Fatal("second Load returned nil error for checksum mismatch")
	}
	if store.gets != 2 {
		t.Fatalf("storage gets = %d, want 2 because invalid data is not cached", store.gets)
	}
}

func testcaseCacheRef(data []byte, id int64) judgeevents.TestcaseSetRef {
	sum := sha256.Sum256(data)
	return judgeevents.TestcaseSetRef{
		ID:             id,
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		StorageKey:     "cases.zip",
		CaseCount:      1,
		TimeLimitMS:    1000,
		MemoryKB:       262144,
	}
}

func testcaseCacheArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create testcase entry: %v", err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write testcase entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close testcase archive: %v", err)
	}
	return buffer.Bytes()
}

type testcaseCacheStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
	gets    int
	delay   time.Duration
}

func (s *testcaseCacheStorage) Put(context.Context, storage.Object) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, errors.New("not implemented")
}

func (s *testcaseCacheStorage) Get(_ context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.mu.Lock()
	s.gets++
	delay := s.delay
	data, ok := s.objects[key]
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if !ok {
		return nil, storage.ObjectInfo{}, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), storage.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (s *testcaseCacheStorage) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func (s *testcaseCacheStorage) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, errors.New("not implemented")
}
