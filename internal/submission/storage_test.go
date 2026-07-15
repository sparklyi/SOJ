package submission

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"SOJ/internal/storage"
)

func TestObjectSourceStoreGetReturnsCloseError(t *testing.T) {
	closeErr := errors.New("close source object")
	store := &recordingObjectStorage{body: &closeErrorReadCloser{Reader: strings.NewReader("source"), err: closeErr}}

	_, err := NewObjectSourceStore(store).Get(context.Background(), "submissions/1/source")
	if !errors.Is(err, closeErr) {
		t.Fatalf("Get error = %v, want close error %v", err, closeErr)
	}
}

func TestParseSnapshotTestcaseCasesAppliesProblemLimits(t *testing.T) {
	archive := snapshotZipArchive(t, map[string]string{
		"input1.txt":  "1 1\n",
		"output1.txt": "2\n",
	})

	cases, err := parseSnapshotTestcaseCases(archive, 10*time.Second, 262144)
	if err != nil {
		t.Fatalf("parseSnapshotTestcaseCases returned error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(cases))
	}
	if cases[0].TimeLimit != 10*time.Second || cases[0].MemoryKB != 262144 {
		t.Fatalf("case limits = %s/%d, want 10s/262144", cases[0].TimeLimit, cases[0].MemoryKB)
	}
}

func snapshotZipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

type closeErrorReadCloser struct {
	io.Reader
	err error
}

func (r *closeErrorReadCloser) Close() error {
	return r.err
}

type recordingObjectStorage struct {
	body io.ReadCloser
}

func (s *recordingObjectStorage) Put(context.Context, storage.Object) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}

func (s *recordingObjectStorage) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return s.body, storage.ObjectInfo{}, nil
}

func (s *recordingObjectStorage) Delete(context.Context, string) error {
	return nil
}

func (s *recordingObjectStorage) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
