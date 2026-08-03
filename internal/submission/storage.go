package submission

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"SOJ/internal/apperror"
	"SOJ/internal/postgres/db"
	"SOJ/internal/problem"
	"SOJ/internal/storage"
)

type ObjectSourceStore struct {
	storage storage.ObjectStorage
}

func NewObjectSourceStore(objectStorage storage.ObjectStorage) *ObjectSourceStore {
	return &ObjectSourceStore{storage: objectStorage}
}

func (s *ObjectSourceStore) Put(ctx context.Context, ownerType string, ownerID int64, source []byte) (SourceObject, error) {
	if s.storage == nil {
		return SourceObject{}, apperror.ServiceUnavailable("object storage unavailable")
	}
	sum := sha256.Sum256(source)
	checksum := hex.EncodeToString(sum[:])
	var nonce [8]byte
	if _, err := crand.Read(nonce[:]); err != nil {
		return SourceObject{}, err
	}
	key := fmt.Sprintf("%s/%d/%s-%s", ownerType, ownerID, checksum, hex.EncodeToString(nonce[:]))
	contentType := "text/plain; charset=utf-8"
	info, err := s.storage.Put(ctx, storage.Object{
		Key:         key,
		ContentType: contentType,
		Size:        int64(len(source)),
		Body:        bytes.NewReader(source),
	})
	if err != nil {
		return SourceObject{}, err
	}
	return SourceObject{StorageKey: info.Key, ChecksumSHA256: checksum, SizeBytes: int64(len(source)), ContentType: contentType}, nil
}

func (s *ObjectSourceStore) Get(ctx context.Context, storageKey string) ([]byte, error) {
	if s.storage == nil {
		return nil, apperror.ServiceUnavailable("object storage unavailable")
	}
	body, _, err := s.storage.Get(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	return readAllAndClose(body)
}

func readAllAndClose(reader io.ReadCloser) ([]byte, error) {
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	return data, err
}

type TestcaseSnapshotResolver struct {
	q       *db.Queries
	storage storage.ObjectStorage
}

func NewTestcaseSnapshotResolver(q *db.Queries, objectStorage storage.ObjectStorage) *TestcaseSnapshotResolver {
	return &TestcaseSnapshotResolver{q: q, storage: objectStorage}
}

func (r *TestcaseSnapshotResolver) ReadyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	row, err := r.q.GetReadyTestcaseSetByID(ctx, db.GetReadyTestcaseSetByIDParams{ID: testcaseSetID, ProblemID: problemID})
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	if r.storage == nil {
		return problem.TestcaseSet{}, apperror.ServiceUnavailable("testcase object storage unavailable")
	}
	if strings.TrimSpace(row.StorageKey) == "" {
		return problem.TestcaseSet{}, apperror.BadRequest("testcase.archive_missing", "testcase archive storage key is missing")
	}
	problemRow, err := r.q.GetProblemByID(ctx, problemID)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	body, _, err := r.storage.Get(ctx, row.StorageKey)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	data, err := readAllAndClose(body)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	cases, err := problem.ParseTestcaseArchive(data, problem.TestcaseArchiveOptions{
		ExpectedCaseCount: row.CaseCount,
		ExpectedSHA256:    row.ChecksumSha256,
		TimeLimit:         time.Duration(problemRow.TimeLimitMs) * time.Millisecond,
		MemoryKB:          int64(problemRow.MemoryLimitKb),
	})
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	return problem.TestcaseSet{ID: row.ID, ProblemID: row.ProblemID, Version: int(row.Version), Status: row.Status, Cases: cases}, nil
}

type testcaseMetadata struct {
	ID             int64
	StorageKey     string
	ChecksumSHA256 string
	CaseCount      int32
	TimeLimit      time.Duration
	MemoryKB       int64
}

func (r *TestcaseSnapshotResolver) ReadyTestcaseMetadata(ctx context.Context, problemID, testcaseSetID int64) (testcaseMetadata, error) {
	row, err := r.q.GetReadyTestcaseSetByID(ctx, db.GetReadyTestcaseSetByIDParams{ID: testcaseSetID, ProblemID: problemID})
	if err != nil {
		return testcaseMetadata{}, err
	}
	problemRow, err := r.q.GetProblemByID(ctx, problemID)
	if err != nil {
		return testcaseMetadata{}, err
	}
	if strings.TrimSpace(row.StorageKey) == "" {
		return testcaseMetadata{}, apperror.BadRequest("testcase.archive_missing", "testcase archive storage key is missing")
	}
	if strings.TrimSpace(row.ChecksumSha256) == "" {
		return testcaseMetadata{}, apperror.BadRequest("testcase.checksum_missing", "testcase archive checksum is missing")
	}
	if row.CaseCount <= 0 {
		return testcaseMetadata{}, apperror.BadRequest("testcase.case_count_mismatch", "testcase archive case count is invalid")
	}
	return testcaseMetadata{
		ID:             row.ID,
		StorageKey:     row.StorageKey,
		ChecksumSHA256: row.ChecksumSha256,
		CaseCount:      row.CaseCount,
		TimeLimit:      time.Duration(problemRow.TimeLimitMs) * time.Millisecond,
		MemoryKB:       int64(problemRow.MemoryLimitKb),
	}, nil
}
