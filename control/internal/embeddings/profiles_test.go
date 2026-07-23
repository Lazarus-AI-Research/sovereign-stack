package embeddings

import (
	"path/filepath"
	"testing"
)

func TestProfileRegistryCRUD(t *testing.T) {
	registry := NewRegistry(filepath.Join(t.TempDir(), "embedding-profiles.yaml"))

	profile := Profile{
		Provider:        "embeddinggemma",
		Model:           EmbeddingGemmaModel,
		Revision:        "8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73",
		ServedModelName: "embedding-gemma-default",
		Pooling:         "mean",
		Normalization:   "l2",
		Modalities:      []string{"text"},
	}
	if err := registry.Put("gemma-default", profile); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := registry.Put("", profile); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.Put("bad", Profile{Model: "x", ServedModelName: "y"}); err == nil {
		t.Fatal("profile without modalities accepted")
	}

	got, err := registry.Get("gemma-default")
	if err != nil || got.ServedModelName != "embedding-gemma-default" {
		t.Fatalf("get: %v %+v", err, got)
	}

	profiles, err := registry.List()
	if err != nil || len(profiles) != 1 {
		t.Fatalf("list: %v %d", err, len(profiles))
	}

	if err := registry.Delete("gemma-default"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := registry.Get("gemma-default"); err == nil {
		t.Fatal("deleted profile still present")
	}
}

func TestProfileRegistryReadsProductDefaults(t *testing.T) {
	// The shipped deploy config must parse with this reader.
	registry := NewRegistry("../../../deploy/config/embedding-profiles.yaml")
	profiles, err := registry.List()
	if err != nil {
		t.Fatalf("list shipped profiles: %v", err)
	}
	if _, ok := profiles["gemma-default"]; !ok {
		t.Errorf("shipped gemma-default profile missing: %v", profiles)
	}
	if len(profiles) != 1 {
		t.Errorf("expected one shipped embedding profile, got: %v", profiles)
	}
}
