package language

import "testing"

// TestShippedLanguagesLoad guards the directory the repository ships: a typo in
// any language.yaml fails here instead of at deployment.
func TestShippedLanguagesLoad(t *testing.T) {
	catalog, err := Load("../../deploy/languages")
	if err != nil {
		t.Fatalf("Load(deploy/languages) error = %v", err)
	}

	profiles := catalog.Profiles()
	if len(profiles) == 0 {
		t.Fatal("deploy/languages defines no languages")
	}
	// The smoke test and the seed depend on these two slugs.
	for _, slug := range []string{"go", "cpp17"} {
		if _, ok := catalog.Lookup(slug); !ok {
			t.Fatalf("deploy/languages is missing %q", slug)
		}
	}
	t.Logf("shipped languages: %d", len(profiles))
}
