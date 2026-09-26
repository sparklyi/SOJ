package problem

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

func archiveZipWithMethod(t *testing.T, method uint16, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func hasFindingCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestValidateArchiveAcceptsValidArchive(t *testing.T) {
	archive := archiveZipWithMethod(t, zip.Deflate,
		map[string]string{
			"cases/2.in":   "2 3\n",
			"cases/2.ans":  "5\n",
			"cases/10.in":  "10 20\n",
			"cases/10.out": "30\n",
			"1.IN":         "1 1\n",
			"1.Ans":        "2\n",
		})

	manifest, findings := ValidateArchive(bytes.NewReader(archive), int64(len(archive)))
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if len(manifest.Cases) != 3 {
		t.Fatalf("cases = %+v, want 3", manifest.Cases)
	}
	gotKeys := []string{manifest.Cases[0].Key, manifest.Cases[1].Key, manifest.Cases[2].Key}
	if strings.Join(gotKeys, ",") != "1,2,10" {
		t.Fatalf("case order = %v, want natural order 1,2,10", gotKeys)
	}
	if manifest.Cases[1].InputPath != "cases/2.in" || manifest.Cases[1].OutputPath != "cases/2.ans" {
		t.Fatalf("nested paths not preserved: %+v", manifest.Cases[1])
	}
	if manifest.Cases[0].InputPath != "1.IN" || manifest.Cases[0].OutputPath != "1.Ans" {
		t.Fatalf("case-insensitive extensions not accepted: %+v", manifest.Cases[0])
	}
}

func TestValidateArchiveIgnoresDotfilesAndMacOSX(t *testing.T) {
	archive := archiveZipWithMethod(t, zip.Deflate, map[string]string{
		".DS_Store":        "junk",
		"cases/.hidden.in": "junk",
		"__MACOSX/1.in":    "junk",
		"cases/1.in":       "1\n",
		"cases/1.ans":      "1\n",
	})

	manifest, findings := ValidateArchive(bytes.NewReader(archive), int64(len(archive)))
	if len(findings) != 3 {
		t.Fatalf("findings = %+v, want 3 warnings", findings)
	}
	if len(manifest.Warnings) != 3 {
		t.Fatalf("warnings = %+v, want 3", manifest.Warnings)
	}
	for _, warning := range findings {
		if warning.Severity != ProblemCheckSeverityWarning || warning.Code != codeFileIgnored {
			t.Fatalf("finding = %+v, want file_ignored warning", warning)
		}
	}
}

func TestValidateArchiveErrorFindings(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		options ArchiveOptions
		want    []string
	}{
		{
			name:    "not a zip",
			archive: []byte("not a zip archive"),
			want:    []string{codeZipInvalid},
		},
		{
			name:    "invalid file name",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"README.md": "x"}),
			want:    []string{codeFileNameInvalid, codeArchiveEmpty},
		},
		{
			name:    "empty entry",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "", "1.ans": "1\n"}),
			want:    []string{codeEntryEmpty},
		},
		{
			name:    "output missing",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "1\n"}),
			want:    []string{codeOutputMissing, codeArchiveEmpty},
		},
		{
			name:    "input missing",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.ans": "1\n"}),
			want:    []string{codeInputMissing, codeArchiveEmpty},
		},
		{
			name:    "duplicate key",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "1\n", "1.IN": "2\n", "1.ans": "1\n"}),
			want:    []string{codeCaseDuplicate},
		},
		{
			name:    "empty archive",
			archive: archiveZipWithMethod(t, zip.Store, nil),
			want:    []string{codeArchiveEmpty},
		},
		{
			name:    "file count exceeded",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "1\n", "1.ans": "1\n"}),
			options: ArchiveOptions{MaxFiles: 1},
			want:    []string{codeFileCountExceeded},
		},
		{
			name:    "entry too large",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "12345", "1.ans": "1\n"}),
			options: ArchiveOptions{MaxEntryBytes: 4},
			want:    []string{codeEntryTooLarge},
		},
		{
			name:    "total size exceeded",
			archive: archiveZipWithMethod(t, zip.Store, map[string]string{"1.in": "12345", "1.ans": "12345"}),
			options: ArchiveOptions{MaxEntryBytes: 10, MaxTotalBytes: 9},
			want:    []string{codeTotalSizeExceeded},
		},
		{
			name:    "compression ratio exceeded",
			archive: archiveZipWithMethod(t, zip.Deflate, map[string]string{"1.in": strings.Repeat("a", 4096), "1.ans": "1\n"}),
			options: ArchiveOptions{MaxEntryBytes: 8192, MaxTotalBytes: 8192, MaxFiles: 4, MaxCompressionRatio: 2},
			want:    []string{codeCompressionExceeded},
		},
		{
			name:    "archive too large",
			archive: []byte("12345"),
			options: ArchiveOptions{MaxBytes: 4},
			want:    []string{codeArchiveTooLarge},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArchive(bytes.NewReader(tt.archive), int64(len(tt.archive)), tt.options)
			for _, code := range tt.want {
				if !hasFindingCode(result.findings, code) {
					t.Fatalf("findings = %+v, want code %s", result.findings, code)
				}
			}
			for _, finding := range result.findings {
				if finding.Severity != ProblemCheckSeverityError {
					t.Fatalf("finding = %+v, want error severity", finding)
				}
			}
		})
	}
}

func TestVerifyArchiveRejectsChecksumMismatch(t *testing.T) {
	archive := archiveZipWithMethod(t, zip.Deflate, map[string]string{"1.in": "1\n", "1.ans": "1\n"})

	if _, findings := VerifyArchive(bytes.NewReader(archive), int64(len(archive)), sha256Hex(archive)); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for matching checksum", findings)
	}
	_, findings := VerifyArchive(bytes.NewReader(archive), int64(len(archive)), strings.Repeat("0", 64))
	if len(findings) != 1 || findings[0].Code != codeArchiveCorrupted {
		t.Fatalf("findings = %+v, want archive_corrupted", findings)
	}
}

func TestLoadArchiveRetainsContentsAndAppliesLimits(t *testing.T) {
	archive := archiveZipWithMethod(t, zip.Deflate, map[string]string{"2.in": "2 3\n", "2.ans": "5\n", "1.in": "1 1\n", "1.ans": "2\n"})

	cases, findings := LoadArchive(bytes.NewReader(archive), int64(len(archive)), ArchiveOptions{
		ExpectedSHA256: sha256Hex(archive),
		TimeLimit:      10 * time.Second,
		MemoryKB:       262144,
	})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if len(cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(cases))
	}
	if cases[0].ID != 1 || cases[0].InputKey != "1 1\n" || cases[0].OutputKey != "2\n" {
		t.Fatalf("first case = %+v", cases[0])
	}
	if cases[1].ID != 2 || cases[1].InputKey != "2 3\n" {
		t.Fatalf("second case = %+v", cases[1])
	}
	if cases[0].TimeLimit != 10*time.Second || cases[0].MemoryKB != 262144 {
		t.Fatalf("limits not applied: %+v", cases[0])
	}
}

func TestArchiveFindingsError(t *testing.T) {
	if err := ArchiveFindingsError([]Finding{warningFinding(codeFileIgnored, "x", "ignored")}); err != nil {
		t.Fatalf("warnings must not produce an error, got %v", err)
	}
	err := ArchiveFindingsError([]Finding{errorFinding(codeArchiveCorrupted, "", "bad")})
	if err == nil {
		t.Fatal("error finding must produce an error")
	}
	assertHTTPStatus(t, err, 422)

	if err := ArchiveFindingsError([]Finding{errorFinding(codeArchiveTooLarge, "", "too big")}); err == nil {
		t.Fatal("archive_too_large must produce an error")
	} else {
		assertHTTPStatus(t, err, 413)
	}
}

func TestNaturalLessOrdersNumericRuns(t *testing.T) {
	ordered := []string{"1", "2", "10", "a2", "a10", "a11", "b", "b2"}
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if !naturalLess(ordered[i], ordered[j]) {
				t.Fatalf("naturalLess(%q, %q) = false, want true", ordered[i], ordered[j])
			}
			if naturalLess(ordered[j], ordered[i]) {
				t.Fatalf("naturalLess(%q, %q) = true, want false", ordered[j], ordered[i])
			}
		}
	}
	if naturalLess("a", "a") {
		t.Fatal("naturalLess must be irreflexive")
	}
	if !naturalLess("01", "1") {
		t.Fatal("equal numeric value must fall back to byte order")
	}
}

func TestSortTestcaseKeysIsDeterministicTotalOrder(t *testing.T) {
	keys := []string{"10", "2", "a10", "a2", "1"}
	sortTestcaseKeys(keys)
	if got := strings.Join(keys, ","); got != "1,2,10,a2,a10" {
		t.Fatalf("sorted keys = %s", got)
	}
}
