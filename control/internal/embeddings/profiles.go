// Package embeddings reads and manages embedding profiles (design.md §10,
// §18.6). Profiles are file-backed product configuration; dimensions are
// never stored here — they are discovered from the embedding service (§10.1).
package embeddings

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	Provider             string   `yaml:"provider" json:"provider"`
	Source               string   `yaml:"source" json:"source"`
	Model                string   `yaml:"model" json:"model"`
	Revision             string   `yaml:"revision" json:"revision"`
	Artifact             string   `yaml:"artifact,omitempty" json:"artifact,omitempty"`
	SHA256               string   `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	ServedModelName      string   `yaml:"served_model_name" json:"served_model_name"`
	Pooling              string   `yaml:"pooling,omitempty" json:"pooling,omitempty"`
	Normalization        string   `yaml:"normalization,omitempty" json:"normalization,omitempty"`
	DistanceMetric       string   `yaml:"distance_metric" json:"distance_metric"`
	QueryPrefix          string   `yaml:"query_prefix,omitempty" json:"query_prefix,omitempty"`
	DocumentPrefix       string   `yaml:"document_prefix,omitempty" json:"document_prefix,omitempty"`
	ChunkingStrategy     string   `yaml:"chunking_strategy" json:"chunking_strategy"`
	PreprocessingVersion string   `yaml:"preprocessing_version" json:"preprocessing_version"`
	Modalities           []string `yaml:"modalities" json:"modalities"`
}

type profilesFile struct {
	EmbeddingProfiles map[string]Profile `yaml:"embedding_profiles"`
}

type Registry struct {
	mu   sync.Mutex
	path string
}

func NewRegistry(path string) *Registry { return &Registry{path: path} }

func (r *Registry) load() (profilesFile, error) {
	var file profilesFile
	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		file.EmbeddingProfiles = map[string]Profile{}
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return file, fmt.Errorf("embedding profiles %s: %w", r.path, err)
	}
	if file.EmbeddingProfiles == nil {
		file.EmbeddingProfiles = map[string]Profile{}
	}
	return file, nil
}

func (r *Registry) save(file profilesFile) error {
	raw, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	header := "# Embedding profiles — managed by Sovereign Control (design.md §10).\n# Validated against schemas/embedding-profile.schema.json.\n"
	return os.WriteFile(r.path, append([]byte(header), raw...), 0o644)
}

func (r *Registry) List() (map[string]Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	return file.EmbeddingProfiles, err
}

func (r *Registry) Get(id string) (Profile, error) {
	profiles, err := r.List()
	if err != nil {
		return Profile{}, err
	}
	profile, ok := profiles[id]
	if !ok {
		return Profile{}, fmt.Errorf("embedding profile %q not found", id)
	}
	return profile, nil
}

func (r *Registry) Put(id string, profile Profile) error {
	if id == "" || profile.Model == "" || profile.ServedModelName == "" || profile.Revision == "" {
		return fmt.Errorf("profile id, model, and served_model_name are required")
	}
	if profile.Source == "" {
		profile.Source = "huggingface"
	}
	if profile.DistanceMetric == "" {
		profile.DistanceMetric = "cosine"
	}
	if profile.ChunkingStrategy == "" {
		profile.ChunkingStrategy = "recursive-v1"
	}
	if profile.PreprocessingVersion == "" {
		profile.PreprocessingVersion = "sovereign-embed-v1"
	}
	if len(profile.Modalities) == 0 {
		return fmt.Errorf("at least one modality is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	if err != nil {
		return err
	}
	file.EmbeddingProfiles[id] = profile
	return r.save(file)
}

func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := r.load()
	if err != nil {
		return err
	}
	if _, ok := file.EmbeddingProfiles[id]; !ok {
		return fmt.Errorf("embedding profile %q not found", id)
	}
	delete(file.EmbeddingProfiles, id)
	return r.save(file)
}
