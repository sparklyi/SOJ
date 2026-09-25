package app

import (
	"fmt"

	"SOJ/internal/config"
	"SOJ/internal/language"
)

// loadLanguages reads the language directory named by the configuration. Both
// the API and the judge agent need it: the API publishes the catalog, the agent
// executes it.
func loadLanguages(cfg config.Config) (*language.Catalog, error) {
	catalog, err := language.Load(cfg.LanguagesDir)
	if err != nil {
		return nil, fmt.Errorf("load languages from %s: %w", cfg.LanguagesDir, err)
	}
	if len(catalog.Profiles()) == 0 {
		return nil, fmt.Errorf("%s defines no languages", cfg.LanguagesDir)
	}
	return catalog, nil
}

// probeImage picks a runner image for the sandbox probe, which runs a container
// that does nothing. Any language's image works.
func probeImage(catalog *language.Catalog) string {
	profiles := catalog.Profiles()
	if len(profiles) == 0 {
		return ""
	}
	return profiles[0].Image
}
