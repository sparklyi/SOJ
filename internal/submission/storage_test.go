package submission

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"
)

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
