package problem

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"SOJ/internal/apperror"
)

var caseNameRE = regexp.MustCompile(`^(?:input|output)([0-9]+)\.(?:txt|in|out)$`)

const (
	defaultMaxTestcaseArchiveBytes       = 128 << 20
	defaultMaxTestcaseUploadRequestBytes = defaultMaxTestcaseArchiveBytes + (1 << 20)
	defaultMaxTestcaseEntryBytes         = 16 << 20
	defaultMaxTestcaseTotalBytes         = 128 << 20
	defaultMaxTestcaseFiles              = 2048
	defaultMaxTestcaseCompressionRatio   = 200
)

type testcaseArchiveLimits struct {
	maxArchiveBytes     int64
	maxEntryBytes       uint64
	maxTotalBytes       uint64
	maxFiles            int
	maxCompressionRatio uint64
}

var defaultTestcaseArchiveLimits = testcaseArchiveLimits{
	maxArchiveBytes:     defaultMaxTestcaseArchiveBytes,
	maxEntryBytes:       defaultMaxTestcaseEntryBytes,
	maxTotalBytes:       defaultMaxTestcaseTotalBytes,
	maxFiles:            defaultMaxTestcaseFiles,
	maxCompressionRatio: defaultMaxTestcaseCompressionRatio,
}

type testcaseArchiveResourceError struct {
	code    string
	message string
}

func (e *testcaseArchiveResourceError) Error() string {
	return e.message
}

func validateTestcaseArchiveResources(data []byte, limits testcaseArchiveLimits) error {
	if limits.maxArchiveBytes > 0 && int64(len(data)) > limits.maxArchiveBytes {
		return testcaseArchiveLimitError("testcase.archive_too_large", "testcase archive is too large")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	fileCount := 0
	var totalBytes uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		fileCount++
		if limits.maxFiles > 0 && fileCount > limits.maxFiles {
			return testcaseArchiveLimitError("testcase.file_count_exceeded", "testcase archive has too many files")
		}
		if limits.maxEntryBytes > 0 && file.UncompressedSize64 > limits.maxEntryBytes {
			return testcaseArchiveLimitError("testcase.entry_too_large", "testcase archive entry is too large")
		}
		if limits.maxTotalBytes > 0 && (file.UncompressedSize64 > limits.maxTotalBytes || totalBytes > limits.maxTotalBytes-file.UncompressedSize64) {
			return testcaseArchiveLimitError("testcase.total_size_exceeded", "testcase archive expands to too much data")
		}
		totalBytes += file.UncompressedSize64
		if compressionRatioExceeded(file.UncompressedSize64, file.CompressedSize64, limits.maxCompressionRatio) {
			return testcaseArchiveLimitError("testcase.compression_ratio_exceeded", "testcase archive compression ratio is too high")
		}
	}
	return nil
}

func verifyTestcaseArchiveContents(data []byte, limits testcaseArchiveLimits) error {
	if err := validateTestcaseArchiveResources(data, limits); err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var totalBytes uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		readBytes, err := verifyZipFile(file, limits.maxEntryBytes)
		if err != nil {
			return err
		}
		if limits.maxTotalBytes > 0 && (readBytes > limits.maxTotalBytes || totalBytes > limits.maxTotalBytes-readBytes) {
			return testcaseArchiveLimitError("testcase.total_size_exceeded", "testcase archive expands to too much data")
		}
		totalBytes += readBytes
	}
	return nil
}

func testcaseArchiveLimitError(code, message string) error {
	return &testcaseArchiveResourceError{code: code, message: message}
}

func compressionRatioExceeded(uncompressed, compressed, maxRatio uint64) bool {
	if maxRatio == 0 || uncompressed == 0 {
		return false
	}
	if compressed == 0 {
		return true
	}
	if compressed > math.MaxUint64/maxRatio {
		return false
	}
	return uncompressed > compressed*maxRatio
}

func validateTestcaseArchive(data []byte, expectedCaseCount int32, expectedSHA256 string, maxSizeBytes int64) error {
	if len(data) == 0 {
		return testcaseNotReady("testcase archive is required")
	}
	if maxSizeBytes > 0 && int64(len(data)) > maxSizeBytes {
		return testcaseNotReady("testcase archive is too large")
	}
	if expectedCaseCount <= 0 {
		return testcaseNotReady("case_count must be positive")
	}
	if strings.TrimSpace(expectedSHA256) == "" {
		return testcaseNotReady("sha256 is required")
	}
	actualSHA256 := sha256Hex(data)
	if !strings.EqualFold(expectedSHA256, actualSHA256) {
		return testcaseNotReady("sha256 does not match archive content")
	}
	limits := defaultTestcaseArchiveLimits
	limits.maxArchiveBytes = maxSizeBytes
	if err := verifyTestcaseArchiveContents(data, limits); err != nil {
		return testcaseNotReady(testcaseArchiveErrorMessage(err))
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return testcaseNotReady("testcase archive must be a valid zip file")
	}

	inputs := map[string]bool{}
	outputs := map[string]bool{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Base(file.Name)
		matches := caseNameRE.FindStringSubmatch(strings.ToLower(name))
		if len(matches) != 2 {
			return testcaseNotReady("testcase input/output file name is invalid")
		}
		if file.UncompressedSize64 == 0 {
			return testcaseNotReady("testcase input/output files must not be empty")
		}
		if strings.HasPrefix(strings.ToLower(name), "input") {
			inputs[matches[1]] = true
		} else {
			outputs[matches[1]] = true
		}
	}

	if int32(len(inputs)) != expectedCaseCount || int32(len(outputs)) != expectedCaseCount {
		return testcaseNotReady("case_count does not match input/output pairs")
	}
	for id := range inputs {
		if !outputs[id] {
			return testcaseNotReady("each input must have a matching output")
		}
	}
	for id := range outputs {
		if !inputs[id] {
			return testcaseNotReady("each output must have a matching input")
		}
	}
	return nil
}

func parseTestcaseArchiveCases(data []byte, defaultTimeLimit time.Duration, defaultMemoryKB int64) ([]Testcase, error) {
	if err := validateTestcaseArchiveResources(data, defaultTestcaseArchiveLimits); err != nil {
		return nil, testcaseArchiveBadRequest(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, apperror.BadRequest("testcase.zip_invalid", "testcase archive must be a valid zip file")
	}

	inputs := map[string]string{}
	outputs := map[string]string{}
	var totalBytes uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Base(file.Name)
		lower := strings.ToLower(name)
		matches := caseNameRE.FindStringSubmatch(lower)
		if len(matches) != 2 {
			continue
		}
		content, err := readZipFile(file, defaultTestcaseArchiveLimits.maxEntryBytes)
		if err != nil {
			return nil, err
		}
		contentBytes := uint64(len(content))
		if contentBytes > defaultTestcaseArchiveLimits.maxTotalBytes || totalBytes > defaultTestcaseArchiveLimits.maxTotalBytes-contentBytes {
			return nil, apperror.BadRequest("testcase.total_size_exceeded", "testcase archive expands to too much data")
		}
		totalBytes += contentBytes
		if strings.HasPrefix(lower, "input") {
			inputs[matches[1]] = content
		} else {
			outputs[matches[1]] = content
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

	cases := make([]Testcase, 0, len(ids))
	for i, id := range ids {
		cases = append(cases, Testcase{
			ID:        int64(i + 1),
			InputKey:  inputs[id],
			OutputKey: outputs[id],
			TimeLimit: defaultTimeLimit,
			MemoryKB:  defaultMemoryKB,
		})
	}
	if len(cases) == 0 {
		return nil, apperror.BadRequest("testcase.case_count_mismatch", "testcase archive has no input/output pairs")
	}
	return cases, nil
}

func readZipFile(file *zip.File, maxBytes uint64) (string, error) {
	reader, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open testcase file %s: %w", file.Name, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, limitedReadBytes(maxBytes)))
	if err != nil {
		return "", fmt.Errorf("read testcase file %s: %w", file.Name, err)
	}
	if maxBytes > 0 && uint64(len(data)) > maxBytes {
		return "", apperror.BadRequest("testcase.entry_too_large", "testcase archive entry is too large")
	}
	return string(data), nil
}

func verifyZipFile(file *zip.File, maxBytes uint64) (uint64, error) {
	reader, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = reader.Close() }()
	readBytes, err := io.Copy(io.Discard, io.LimitReader(reader, limitedReadBytes(maxBytes)))
	if err != nil {
		return 0, err
	}
	if maxBytes > 0 && uint64(readBytes) > maxBytes {
		return 0, testcaseArchiveLimitError("testcase.entry_too_large", "testcase archive entry is too large")
	}
	return uint64(readBytes), nil
}

func limitedReadBytes(maxBytes uint64) int64 {
	if maxBytes == 0 || maxBytes >= math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(maxBytes) + 1
}

func testcaseArchiveErrorMessage(err error) string {
	if resourceErr, ok := err.(*testcaseArchiveResourceError); ok {
		return resourceErr.message
	}
	return "testcase archive must be a valid zip file"
}

func testcaseArchiveBadRequest(err error) error {
	if resourceErr, ok := err.(*testcaseArchiveResourceError); ok {
		return apperror.BadRequest(resourceErr.code, resourceErr.message)
	}
	return apperror.BadRequest("testcase.zip_invalid", "testcase archive must be a valid zip file")
}

func testcaseNotReady(message string) error {
	return apperror.Unprocessable("problem.testcase_not_ready", message)
}
