package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"SOJ/internal/config"
)

// TestLoadLanguagesRejectsUnusableDirectories pins the startup guard that the
// API and the judge agent both inherit from loadLanguages: an existing but
// empty language directory and a missing one must fail, and the two failures
// must stay distinguishable.
func TestLoadLanguagesRejectsUnusableDirectories(t *testing.T) {
	t.Run("existing empty directory", func(t *testing.T) {
		dir := t.TempDir()

		catalog, err := loadLanguages(config.Config{LanguagesDir: dir})
		if err == nil {
			t.Fatal("loadLanguages() error = nil, want empty catalog error")
		}
		if catalog != nil {
			t.Fatalf("loadLanguages() catalog = %+v, want nil", catalog)
		}
		if !strings.Contains(err.Error(), dir) {
			t.Fatalf("loadLanguages() error = %q, want configured directory %q", err, dir)
		}
		if !strings.Contains(err.Error(), "defines no languages") {
			t.Fatalf("loadLanguages() error = %q, want missing languages to be named", err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadLanguages() error = %v, want no filesystem error for a readable directory", err)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "absent")

		catalog, err := loadLanguages(config.Config{LanguagesDir: dir})
		if err == nil {
			t.Fatal("loadLanguages() error = nil, want missing directory error")
		}
		if catalog != nil {
			t.Fatalf("loadLanguages() catalog = %+v, want nil", catalog)
		}
		if !strings.Contains(err.Error(), dir) {
			t.Fatalf("loadLanguages() error = %q, want configured directory %q", err, dir)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadLanguages() error = %v, want wrapped fs.ErrNotExist", err)
		}
		if strings.Contains(err.Error(), "defines no languages") {
			t.Fatalf("loadLanguages() error = %q, want a filesystem error rather than an empty catalog", err)
		}
	})
}
