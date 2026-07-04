package problem

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"SOJ/internal/apperror"
)

var caseNameRE = regexp.MustCompile(`^(?:input|output)([0-9]+)\.(?:txt|in|out)$`)

const defaultMaxTestcaseArchiveBytes = 128 << 20

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
		name := path.Base(file.Name)
		lower := strings.ToLower(name)
		matches := caseNameRE.FindStringSubmatch(lower)
		if len(matches) != 2 {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			return nil, err
		}
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

func readZipFile(file *zip.File) (string, error) {
	reader, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open testcase file %s: %w", file.Name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read testcase file %s: %w", file.Name, err)
	}
	return string(data), nil
}

func testcaseNotReady(message string) error {
	return apperror.Unprocessable("problem.testcase_not_ready", message)
}
