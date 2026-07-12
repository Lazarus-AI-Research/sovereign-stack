// Package branding manages customer-facing branding and product feature
// flags (design.md §26). Both are file-backed product configuration; the
// privacy defaults in feature flags are locked by §2.3–2.4 and enforced
// here: content capture can only be enabled explicitly, never by default.
package branding

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Store struct {
	mu   sync.Mutex
	path string
	// name of the top-level document, for the write header
	kind string
}

func NewStore(path, kind string) *Store { return &Store{path: path, kind: kind} }

func (s *Store) Get() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	return doc, nil
}

func (s *Store) Put(doc map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("# %s — managed by Sovereign Control.\n", s.kind)
	return os.WriteFile(s.path, append([]byte(header), raw...), 0o644)
}

// ValidateFlags enforces the §2.3–2.4 invariants on a feature-flags document:
// enabling any content capture requires every explicit companion setting.
func ValidateFlags(doc map[string]any) error {
	promptLogging, _ := doc["prompt_logging"].(map[string]any)
	if enabled, _ := promptLogging["enabled"].(bool); enabled {
		for _, required := range []string{"pii_redaction", "secret_redaction", "retention_days", "scope"} {
			if _, ok := promptLogging[required]; !ok {
				return fmt.Errorf(
					"enabling prompt logging requires explicit %q (design.md §2.4)", required)
			}
		}
	}
	if tracing, ok := doc["tracing"].(map[string]any); ok {
		fullTrace, _ := tracing["full_trace"].(bool)
		metadataOnly, hasMetadataOnly := tracing["metadata_only"].(bool)
		if fullTrace && hasMetadataOnly && metadataOnly {
			return fmt.Errorf("tracing cannot be both full_trace and metadata_only")
		}
		if fullTrace {
			if enabled, _ := promptLogging["enabled"].(bool); !enabled {
				return fmt.Errorf("full_trace requires prompt logging to be explicitly configured first (§2.3)")
			}
		}
	}
	return nil
}
