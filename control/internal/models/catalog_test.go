package models

import "testing"

func TestCatalogUsesProfileSpecificGenerationArtifact(t *testing.T) {
	metal, err := CatalogEntry("metal-arm64", "assistant-large")
	if err != nil {
		t.Fatal(err)
	}
	if metal.RegistryEntry.Source != "local" || metal.RegistryEntry.Artifact == "" {
		t.Fatalf("metal catalog entry: %+v", metal.RegistryEntry)
	}
	cuda, err := CatalogEntry("cuda-x86_64", "assistant-large")
	if err != nil {
		t.Fatal(err)
	}
	if cuda.RegistryEntry.Source != "huggingface" || cuda.RegistryEntry.Revision == "" {
		t.Fatalf("cuda catalog entry: %+v", cuda.RegistryEntry)
	}
}

func TestEmbeddingGemmaRemainsTheDefaultEmbedding(t *testing.T) {
	entry, err := CatalogEntry("cuda-x86_64", "embedding-gemma-default")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Role != "embedding" || !entry.Recommended || entry.RegistryEntry.Model == "" {
		t.Fatalf("embedding catalog entry: %+v", entry)
	}
}
