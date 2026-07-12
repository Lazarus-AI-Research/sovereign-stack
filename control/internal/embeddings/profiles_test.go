package embeddings

import (
	"path/filepath"
	"testing"
)

func TestProfileRegistryCRUD(t *testing.T) {
	registry := NewRegistry(filepath.Join(t.TempDir(), "embedding-profiles.yaml"))

	profile := Profile{
		Provider:        "sovereign-runtime",
		Model:           "Qwen/Qwen3-Embedding-0.6B",
		Revision:        "main",
		ServedModelName: "embedding-text-compact",
		Pooling:         "last",
		Normalization:   "l2",
		Modalities:      []string{"text"},
	}
	if err := registry.Put("text-compact", profile); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := registry.Put("", profile); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.Put("bad", Profile{Model: "x", ServedModelName: "y"}); err == nil {
		t.Fatal("profile without modalities accepted")
	}

	got, err := registry.Get("text-compact")
	if err != nil || got.ServedModelName != "embedding-text-compact" {
		t.Fatalf("get: %v %+v", err, got)
	}

	profiles, err := registry.List()
	if err != nil || len(profiles) != 1 {
		t.Fatalf("list: %v %d", err, len(profiles))
	}

	if err := registry.Delete("text-compact"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := registry.Get("text-compact"); err == nil {
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
	if _, ok := profiles["omni-default"]; !ok {
		t.Errorf("shipped omni-default profile missing: %v", profiles)
	}
	if _, ok := profiles["text-compact"]; !ok {
		t.Errorf("shipped text-compact profile missing: %v", profiles)
	}
}
