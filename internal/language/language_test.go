package language

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLanguage(t *testing.T, dir, slug, definition string) {
	t.Helper()

	path := filepath.Join(dir, slug)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, profileFileName), []byte(definition), 0o600); err != nil {
		t.Fatalf("write definition: %v", err)
	}
}

const goDefinition = `
name: Go
version: "1.24"
source_file: main.go
binary_file: main
compile: ["go", "build", "-o", "{{binary}}", "{{source}}"]
run: ["{{binary}}"]
image: ghcr.io/sparklyi/soj-runner-go:test
`

func TestLoadReadsEveryDirectoryAndDefaultsLimits(t *testing.T) {
	dir := t.TempDir()
	writeLanguage(t, dir, "go", goDefinition)
	writeLanguage(t, dir, "python3", `
name: Python 3
source_file: main.py
compile: ["python3", "-m", "py_compile", "{{source}}"]
run: ["python3", "{{source}}"]
image: ghcr.io/sparklyi/soj-runner-python3:test
`)
	// Supporting files are the image build's business, not the runtime contract.
	if err := os.WriteFile(filepath.Join(dir, "go", "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a language\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}

	catalog, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	profiles := catalog.Profiles()
	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(profiles))
	}
	// Sorted by slug, and the directory name is the slug.
	if profiles[0].Slug != "go" || profiles[1].Slug != "python3" {
		t.Fatalf("slugs = %q, %q", profiles[0].Slug, profiles[1].Slug)
	}
	if profiles[0].TimeLimitMS != DefaultTimeLimitMS || profiles[0].MemoryLimitKB != DefaultMemoryLimitKB {
		t.Fatalf("limits = %d/%d, want defaults", profiles[0].TimeLimitMS, profiles[0].MemoryLimitKB)
	}
	if _, ok := catalog.Lookup("python3"); !ok {
		t.Fatal("Lookup(python3) missed")
	}
	if _, ok := catalog.Lookup("ruby"); ok {
		t.Fatal("Lookup(ruby) found a language that is not configured")
	}
}

func TestLoadRejectsSlugAsAFileKey(t *testing.T) {
	// The directory name is the slug; a slug key in the file would let the two
	// disagree, so it is an unknown field.
	dir := t.TempDir()
	writeLanguage(t, dir, "go", "slug: not-go\n"+goDefinition)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() error = nil, want the slug key rejected")
	}
	if !strings.Contains(err.Error(), "slug") {
		t.Fatalf("Load() error = %v, want it to name the unknown key", err)
	}
}

func TestLoadReportsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		definition string
		want       string
	}{
		{"missing run", "go", "name: Go\nsource_file: main.go\nimage: img:1\n", "run must not be empty"},
		{"binary without compile", "go", "name: Go\nsource_file: main.go\nbinary_file: main\nrun: [\"{{binary}}\"]\nimage: img:1\n", "requires a compile command"},
		{"unknown placeholder", "go", "name: Go\nsource_file: main.go\nrun: [\"{{interpreter}}\", \"{{source}}\"]\nimage: img:1\n", "unknown placeholder"},
		{"source never used", "go", "name: Go\nsource_file: main.go\nrun: [\"true\"]\nimage: img:1\n", "must reference {{source}}"},
		{"missing image", "go", "name: Go\nsource_file: main.go\nrun: [\"{{source}}\"]\n", "image must not be empty"},
		{"invalid slug", "Go", "name: Go\nsource_file: main.go\nrun: [\"{{source}}\"]\nimage: img:1\n", "slug"},
		{"missing definition", "go", "", "no such file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.definition == "" {
				if err := os.MkdirAll(filepath.Join(dir, test.slug), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			} else {
				writeLanguage(t, dir, test.slug, test.definition)
			}

			_, err := Load(dir)
			if err == nil {
				t.Fatal("Load() error = nil, want the definition rejected")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadExpandsImagePlaceholders(t *testing.T) {
	t.Setenv("SOJ_RUNNER_TAG", "1.2.3")

	dir := t.TempDir()
	writeLanguage(t, dir, "go", `
name: Go
source_file: main.go
binary_file: main
compile: ["go", "build", "-o", "{{binary}}", "{{source}}"]
run: ["{{binary}}"]
image: ghcr.io/sparklyi/soj-runner-go:${SOJ_RUNNER_TAG:-main}
`)

	catalog, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profile, _ := catalog.Lookup("go")
	if profile.Image != "ghcr.io/sparklyi/soj-runner-go:1.2.3" {
		t.Fatalf("image = %q, want the tag from the environment", profile.Image)
	}
}

func TestNewRejectsDuplicateAndAppliesDefaults(t *testing.T) {
	profile := Profile{
		Slug:       "go",
		Name:       "Go",
		SourceFile: "main.go",
		BinaryFile: "main",
		Compile:    []string{"go", "build", "-o", "{{binary}}", "{{source}}"},
		Run:        []string{"{{binary}}"},
		Image:      "img:1",
	}

	catalog, err := New(profile, profile)
	if err == nil {
		t.Fatal("New() error = nil, want the duplicate slug rejected")
	}
	if catalog != nil {
		t.Fatal("New() returned a catalog for a rejected set")
	}

	catalog, err = New(profile)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, _ := catalog.Lookup("go")
	if got.TimeLimitMS != DefaultTimeLimitMS || got.MemoryLimitKB != DefaultMemoryLimitKB {
		t.Fatalf("limits = %d/%d, want defaults", got.TimeLimitMS, got.MemoryLimitKB)
	}
}

func TestRuntimeAndDisplayLabels(t *testing.T) {
	catalog, err := New(Profile{
		Slug:       "go",
		Name:       "Go",
		Version:    "1.24",
		SourceFile: "main.go",
		Run:        []string{"{{source}}"},
		Image:      "img:1",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	profile, _ := catalog.Lookup("go")
	if label := profile.RuntimeLabel(); label != "go@1.24" {
		t.Fatalf("RuntimeLabel() = %q", label)
	}
	if line := profile.DisplayRun(); line != "{{source}}" {
		t.Fatalf("DisplayRun() = %q", line)
	}
}
