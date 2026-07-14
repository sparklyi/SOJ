package problem

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"SOJ/internal/apperror"
)

func TestValidateTestcaseArchiveResourcesRejectsExceededBudgets(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		limits  testcaseArchiveLimits
		code    string
	}{
		{
			name:    "compressed archive",
			archive: []byte("too large"),
			limits:  testcaseArchiveLimits{maxArchiveBytes: 4},
			code:    "testcase.archive_too_large",
		},
		{
			name: "file count",
			archive: resourceTestZip(t, zip.Store, map[string]string{
				"input1.txt":  "1",
				"output1.txt": "1",
			}),
			limits: testcaseArchiveLimits{maxFiles: 1},
			code:   "testcase.file_count_exceeded",
		},
		{
			name:    "single entry",
			archive: resourceTestZip(t, zip.Store, map[string]string{"input1.txt": "12345"}),
			limits:  testcaseArchiveLimits{maxEntryBytes: 4},
			code:    "testcase.entry_too_large",
		},
		{
			name: "total uncompressed bytes",
			archive: resourceTestZip(t, zip.Store, map[string]string{
				"input1.txt":  "12345",
				"output1.txt": "12345",
			}),
			limits: testcaseArchiveLimits{maxEntryBytes: 10, maxTotalBytes: 9},
			code:   "testcase.total_size_exceeded",
		},
		{
			name:    "compression ratio",
			archive: resourceTestZip(t, zip.Deflate, map[string]string{"input1.txt": strings.Repeat("a", 4096)}),
			limits:  testcaseArchiveLimits{maxEntryBytes: 8192, maxTotalBytes: 8192, maxFiles: 1, maxCompressionRatio: 2},
			code:    "testcase.compression_ratio_exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTestcaseArchiveResources(tt.archive, tt.limits)
			resourceErr, ok := err.(*testcaseArchiveResourceError)
			if !ok || resourceErr.code != tt.code {
				t.Fatalf("error = %T %v, want resource error %q", err, err, tt.code)
			}
		})
	}
}

func TestValidateTestcaseArchiveResourcesAllowsExactBudgets(t *testing.T) {
	archive := resourceTestZip(t, zip.Store, map[string]string{
		"input1.txt":  "12345",
		"output1.txt": "12345",
	})
	limits := testcaseArchiveLimits{
		maxArchiveBytes:     int64(len(archive)),
		maxEntryBytes:       5,
		maxTotalBytes:       10,
		maxFiles:            2,
		maxCompressionRatio: 1,
	}

	if err := validateTestcaseArchiveResources(archive, limits); err != nil {
		t.Fatalf("exact resource budgets returned error: %v", err)
	}
}

func TestReadAllAndCloseRejectsOversizedArchive(t *testing.T) {
	_, err := readAllAndClose(io.NopCloser(strings.NewReader("12345")), 4)
	resourceErr, ok := err.(*testcaseArchiveResourceError)
	if !ok || resourceErr.code != "testcase.archive_too_large" {
		t.Fatalf("error = %T %v, want testcase.archive_too_large", err, err)
	}
}

func TestValidateTestcaseArchiveRejectsHighCompressionRatio(t *testing.T) {
	archive := zipArchive(t, map[string]string{
		"input1.txt":  strings.Repeat("a", 1<<20),
		"output1.txt": "1\n",
	})

	err := validateTestcaseArchive(archive, 1, sha256Hex(archive), defaultMaxTestcaseArchiveBytes)
	assertAppCode(t, err, "problem.testcase_not_ready")
}

func TestParseTestcaseArchiveCasesRejectsOversizedEntry(t *testing.T) {
	archive := zipArchive(t, map[string]string{
		"input1.txt":  strings.Repeat("a", defaultMaxTestcaseEntryBytes+1),
		"output1.txt": "1\n",
	})

	_, err := parseTestcaseArchiveCases(archive, time.Second, 1024)
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != "testcase.entry_too_large" {
		t.Fatalf("error = %T %v, want testcase.entry_too_large", err, err)
	}
}

func resourceTestZip(t *testing.T, method uint16, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
