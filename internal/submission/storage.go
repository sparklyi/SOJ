package submission

import (
	"archive/zip"
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
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

func (r *TestcaseSnapshotResolver) CurrentReadyTestcaseSet(ctx context.Context, problemID int64) (problem.TestcaseSet, error) {
	row, err := r.q.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	return r.testcaseSetFromRow(ctx, row.ID, row.ProblemID, int(row.Version), row.Status, row.StorageKey)
}

func (r *TestcaseSnapshotResolver) ReadyTestcaseSet(ctx context.Context, problemID, testcaseSetID int64) (problem.TestcaseSet, error) {
	row, err := r.q.GetReadyTestcaseSetByID(ctx, db.GetReadyTestcaseSetByIDParams{ID: testcaseSetID, ProblemID: problemID})
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	return r.testcaseSetFromRow(ctx, row.ID, row.ProblemID, int(row.Version), row.Status, row.StorageKey)
}

func (r *TestcaseSnapshotResolver) testcaseSetFromRow(ctx context.Context, id, problemID int64, version int, status, storageKey string) (problem.TestcaseSet, error) {
	if r.storage == nil {
		return problem.TestcaseSet{}, apperror.ServiceUnavailable("testcase object storage unavailable")
	}
	if strings.TrimSpace(storageKey) == "" {
		return problem.TestcaseSet{}, apperror.BadRequest("testcase.archive_missing", "testcase archive storage key is missing")
	}
	problemRow, err := r.q.GetProblemByID(ctx, problemID)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	body, _, err := r.storage.Get(ctx, storageKey)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	data, err := readAllAndClose(body)
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	cases, err := parseSnapshotTestcaseCases(data, time.Duration(problemRow.TimeLimitMs)*time.Millisecond, int64(problemRow.MemoryLimitKb))
	if err != nil {
		return problem.TestcaseSet{}, err
	}
	return problem.TestcaseSet{ID: id, ProblemID: problemID, Version: version, Status: status, Cases: cases}, nil
}

var snapshotCaseNameRE = regexp.MustCompile(`^(input|output)(\d+)\.txt$`)

func parseSnapshotTestcaseCases(data []byte, defaultTimeLimit time.Duration, defaultMemoryKB int64) ([]problem.Testcase, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, apperror.BadRequest("testcase.zip_invalid", "testcase archive must be a valid zip file")
	}
	inputs := map[string]string{}
	outputs := map[string]string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(path.Base(file.Name))
		matches := snapshotCaseNameRE.FindStringSubmatch(name)
		if len(matches) != 3 {
			continue
		}
		content, err := readSnapshotZipFile(file)
		if err != nil {
			return nil, err
		}
		if matches[1] == "input" {
			inputs[matches[2]] = content
		} else {
			outputs[matches[2]] = content
		}
	}

	ids := make([]string, 0, len(inputs))
	for id := range inputs {
		if _, ok := outputs[id]; !ok {
			return nil, apperror.BadRequest("testcase.output_missing", "each input must have a matching output")
		}
		ids = append(ids, id)
	}
	for id := range outputs {
		if _, ok := inputs[id]; !ok {
			return nil, apperror.BadRequest("testcase.input_missing", "each output must have a matching input")
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})

	cases := make([]problem.Testcase, 0, len(ids))
	for i, id := range ids {
		cases = append(cases, problem.Testcase{ID: int64(i + 1), InputKey: inputs[id], OutputKey: outputs[id], TimeLimit: defaultTimeLimit, MemoryKB: defaultMemoryKB})
	}
	if len(cases) == 0 {
		return nil, apperror.BadRequest("testcase.case_count_mismatch", "testcase archive has no input/output pairs")
	}
	return cases, nil
}

func readSnapshotZipFile(file *zip.File) (string, error) {
	reader, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open testcase file %s: %w", file.Name, err)
	}
	data, err := readAllAndClose(reader)
	if err != nil {
		return "", fmt.Errorf("read testcase file %s: %w", file.Name, err)
	}
	return string(data), nil
}
