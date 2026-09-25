// Package language describes the programming languages the judge can run.
//
// A language is data, not behavior: a source filename, the argv that compiles
// it, the argv that runs it, and the container image that provides its
// toolchain. The sandbox executes those argv in that image; nothing here knows
// how to compile or run anything itself. That is why languages live in files
// under the directory named by languages_dir rather than in code: adding one is
// a directory, not a release.
package language

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"SOJ/internal/envsubst"

	"go.yaml.in/yaml/v3"
)

// profileFileName is the only file read from a language directory. A Dockerfile
// and any other supporting files may sit next to it; they belong to the image
// build, not to the runtime contract.
const profileFileName = "language.yaml"

// Default limits for a language that does not set its own. They are insert
// defaults for the catalog row; administrators tune the row afterwards.
const (
	DefaultTimeLimitMS   = 1000
	DefaultMemoryLimitKB = 262144
)

// Profile is one language's execution definition.
type Profile struct {
	// Slug is the directory name. It is the key the API, the judge event, the
	// catalog rows, the runner images, and the frontend all agree on.
	Slug string `yaml:"-"`

	Name          string   `yaml:"name"`
	Version       string   `yaml:"version"`
	SourceFile    string   `yaml:"source_file"`
	BinaryFile    string   `yaml:"binary_file"`
	Compile       []string `yaml:"compile"`
	Run           []string `yaml:"run"`
	Image         string   `yaml:"image"`
	TimeLimitMS   int      `yaml:"time_limit_ms"`
	MemoryLimitKB int      `yaml:"memory_limit_kb"`
}

// RuntimeLabel names the toolchain for judge attempt manifests.
func (p Profile) RuntimeLabel() string {
	if p.Version == "" {
		return p.Slug
	}
	return p.Slug + "@" + p.Version
}

// DisplayCompile and DisplayRun render argv for the catalog's display columns.
// The judge never reads those columns; they exist so an administrator can see
// what a language does.
func (p Profile) DisplayCompile() string { return strings.Join(p.Compile, " ") }
func (p Profile) DisplayRun() string     { return strings.Join(p.Run, " ") }

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

var placeholderPattern = regexp.MustCompile(`\{\{[A-Za-z_]+\}\}`)

var allowedPlaceholders = []string{"{{source}}", "{{binary}}"}

func (p Profile) validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if !slugPattern.MatchString(p.Slug) {
		add("slug %q must match %s", p.Slug, slugPattern)
	}
	if strings.TrimSpace(p.Name) == "" {
		add("%s: name must not be empty", p.Slug)
	}
	if strings.TrimSpace(p.SourceFile) == "" {
		add("%s: source_file must not be empty", p.Slug)
	}
	if len(p.Run) == 0 {
		add("%s: run must not be empty", p.Slug)
	}
	// An interpreted language may compile (a syntax check) without producing a
	// binary; the reverse -- a binary nothing built -- is always a mistake.
	if p.BinaryFile != "" && len(p.Compile) == 0 {
		add("%s: binary_file %q requires a compile command", p.Slug, p.BinaryFile)
	}
	if strings.TrimSpace(p.Image) == "" {
		add("%s: image must not be empty", p.Slug)
	}
	if p.TimeLimitMS <= 0 {
		add("%s: time_limit_ms must be greater than zero", p.Slug)
	}
	if p.MemoryLimitKB <= 0 {
		add("%s: memory_limit_kb must be greater than zero", p.Slug)
	}

	usesSource := false
	for _, argv := range [][]string{p.Compile, p.Run} {
		for _, arg := range argv {
			for _, ref := range placeholderPattern.FindAllString(arg, -1) {
				if !slices.Contains(allowedPlaceholders, ref) {
					add("%s: unknown placeholder %s (allowed: %s)",
						p.Slug, ref, strings.Join(allowedPlaceholders, ", "))
					continue
				}
				if ref == "{{source}}" {
					usesSource = true
				}
			}
		}
	}
	if !usesSource {
		add("%s: compile or run must reference {{source}}", p.Slug)
	}

	return errors.Join(errs...)
}

func (p Profile) withDefaults() Profile {
	if p.TimeLimitMS == 0 {
		p.TimeLimitMS = DefaultTimeLimitMS
	}
	if p.MemoryLimitKB == 0 {
		p.MemoryLimitKB = DefaultMemoryLimitKB
	}
	return p
}

// Catalog is an immutable set of language profiles, keyed by slug.
type Catalog struct {
	profiles []Profile
	bySlug   map[string]Profile
}

// New validates and indexes profiles. Profiles are kept in slug order so that
// listings and reconciliations are deterministic.
func New(profiles ...Profile) (*Catalog, error) {
	catalog := &Catalog{bySlug: make(map[string]Profile, len(profiles))}
	for _, profile := range profiles {
		profile = profile.withDefaults()
		if err := profile.validate(); err != nil {
			return nil, err
		}
		if _, duplicate := catalog.bySlug[profile.Slug]; duplicate {
			return nil, fmt.Errorf("language %q is defined twice", profile.Slug)
		}
		catalog.bySlug[profile.Slug] = profile
		catalog.profiles = append(catalog.profiles, profile)
	}
	slices.SortFunc(catalog.profiles, func(a, b Profile) int { return strings.Compare(a.Slug, b.Slug) })
	return catalog, nil
}

// Load reads every language directory under dir.
//
// The directory name is the slug, so a language cannot be renamed in one place
// and used under another. A directory without language.yaml is an error: it
// means someone dropped a folder that does not describe anything.
func Load(dir string) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), profileFileName)
		profile, err := loadProfile(path)
		if err != nil {
			return nil, err
		}
		profile.Slug = entry.Name()
		profiles = append(profiles, profile)
	}
	return New(profiles...)
}

func loadProfile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	expanded, err := envsubst.Expand(string(data))
	if err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}

	var profile Profile
	decoder := yaml.NewDecoder(strings.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&profile); err != nil && !errors.Is(err, io.EOF) {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	return profile, nil
}

// Lookup returns the profile for a slug.
func (c *Catalog) Lookup(slug string) (Profile, bool) {
	profile, ok := c.bySlug[slug]
	return profile, ok
}

// Profiles returns every profile in slug order.
func (c *Catalog) Profiles() []Profile {
	return slices.Clone(c.profiles)
}
