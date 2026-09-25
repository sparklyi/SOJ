package submission

import (
	"testing"

	"SOJ/internal/language"
)

// testCatalog is the language set the submission tests execute against. It
// mirrors deploy/languages/go/language.yaml.
func testCatalog(t *testing.T) *language.Catalog {
	t.Helper()

	catalog, err := language.New(language.Profile{
		Slug:       "go",
		Name:       "Go",
		Version:    "1.24",
		SourceFile: "main.go",
		BinaryFile: "main",
		Compile:    []string{"go", "build", "-o", "{{binary}}", "{{source}}"},
		Run:        []string{"{{binary}}"},
		Image:      "ghcr.io/sparklyi/soj-runner-go:test",
	})
	if err != nil {
		t.Fatalf("build test catalog: %v", err)
	}
	return catalog
}
