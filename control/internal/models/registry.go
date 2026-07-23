// Package models manages the model registry (design.md §2.9, §18.5) and local
// generation-model loading. The dedicated embedding service is managed through
// embedding profiles instead of rewriting the runtime configuration.
package models

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	ID                 string   `yaml:"id" json:"id"`
	Role               string   `yaml:"role" json:"role"`
	Source             string   `yaml:"source" json:"source"`
	Model              string   `yaml:"model" json:"model"`
	Revision           string   `yaml:"revision,omitempty" json:"revision,omitempty"`
	Artifact           string   `yaml:"artifact,omitempty" json:"artifact,omitempty"`
	SHA256             string   `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	BaseURL            string   `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	CredentialID       string   `yaml:"credential_id,omitempty" json:"credential_id,omitempty"`
	Provider           string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Capabilities       []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	CompatibleProfiles []string `yaml:"compatible_profiles,omitempty" json:"compatible_profiles,omitempty"`
	ValidationState    string   `yaml:"validation_state,omitempty" json:"validation_state,omitempty"`
}

func (e Entry) validate() error {
	if e.ID == "" || e.Model == "" {
		return fmt.Errorf("id and model are required")
	}
	switch e.Role {
	case "generation", "embedding", "vision", "audio", "rerank":
	default:
		return fmt.Errorf("unknown role %q", e.Role)
	}
	switch e.Source {
	case "huggingface", "modelscope", "local", "offline", "remote", "cloud":
	default:
		return fmt.Errorf("unknown source %q", e.Source)
	}
	if (e.Source == "huggingface" || e.Source == "modelscope") && e.Revision == "" {
		return fmt.Errorf("source %q requires an immutable revision", e.Source)
	}
	if (e.Source == "remote" || e.Source == "cloud") && e.Provider == "" {
		return fmt.Errorf("source %q requires a provider", e.Source)
	}
	if e.Source == "remote" && e.BaseURL == "" {
		return fmt.Errorf("remote models require base_url")
	}
	if e.ValidationState == "" {
		e.ValidationState = "unvalidated"
	}
	return nil
}

type registryFile struct {
	Models []Entry `yaml:"models"`
}

// Registry is the YAML-file-backed model registry. Writes are whole-file:
// the file is product-managed (header comments are not preserved).
type Registry struct {
	mu   sync.Mutex
	path string
}

func NewRegistry(path string) *Registry { return &Registry{path: path} }

func (r *Registry) load() (registryFile, error) {
	var file registryFile
	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return file, fmt.Errorf("model registry %s: %w", r.path, err)
	}
	return file, nil
}

func (r *Registry) save(file registryFile) error {
	raw, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	header := "# Model registry — managed by Sovereign Control.\n# Validated against schemas/model-registry.schema.json.\n"
	return os.WriteFile(r.path, append([]byte(header), raw...), 0o644)
}

func (r *Registry) List() ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	return file.Models, err
}

func (r *Registry) Get(id string) (Entry, error) {
	entries, err := r.List()
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("model %q not found", id)
}

func (r *Registry) Add(entry Entry) error {
	if err := entry.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	if err != nil {
		return err
	}
	for _, existing := range file.Models {
		if existing.ID == entry.ID {
			return fmt.Errorf("model %q already exists", entry.ID)
		}
	}
	file.Models = append(file.Models, entry)
	return r.save(file)
}

func (r *Registry) Update(id string, patch map[string]string) (Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	if err != nil {
		return Entry{}, err
	}
	for i, entry := range file.Models {
		if entry.ID != id {
			continue
		}
		if v, ok := patch["model"]; ok {
			entry.Model = v
		}
		if v, ok := patch["revision"]; ok {
			entry.Revision = v
		}
		if v, ok := patch["role"]; ok {
			entry.Role = v
		}
		if v, ok := patch["source"]; ok {
			entry.Source = v
		}
		if v, ok := patch["artifact"]; ok {
			entry.Artifact = v
		}
		if v, ok := patch["sha256"]; ok {
			entry.SHA256 = v
		}
		if v, ok := patch["base_url"]; ok {
			entry.BaseURL = v
		}
		if v, ok := patch["credential_id"]; ok {
			entry.CredentialID = v
		}
		if v, ok := patch["provider"]; ok {
			entry.Provider = v
		}
		if v, ok := patch["validation_state"]; ok {
			entry.ValidationState = v
		}
		if err := entry.validate(); err != nil {
			return Entry{}, err
		}
		file.Models[i] = entry
		return entry, r.save(file)
	}
	return Entry{}, fmt.Errorf("model %q not found", id)
}

func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	if err != nil {
		return err
	}
	kept := file.Models[:0]
	found := false
	for _, entry := range file.Models {
		if entry.ID == id {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if !found {
		return fmt.Errorf("model %q not found", id)
	}
	file.Models = kept
	return r.save(file)
}
