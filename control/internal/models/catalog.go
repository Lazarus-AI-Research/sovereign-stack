package models

import "fmt"

const CatalogVersion = "2026-07-24"

// CatalogItem is the product-facing description of a reviewed model. Registry
// pins remain available under Advanced, but the normal UI only needs this
// metadata and the stable item ID.
type CatalogItem struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name"`
	Description        string   `json:"description"`
	Role               string   `json:"role"`
	Capabilities       []string `json:"capabilities"`
	CompatibleProfiles []string `json:"compatible_profiles"`
	DownloadBytes      int64    `json:"download_bytes"`
	MinimumMemoryBytes int64    `json:"minimum_memory_bytes,omitempty"`
	MinimumVRAMBytes   int64    `json:"minimum_vram_bytes,omitempty"`
	Recommended        bool     `json:"recommended"`
	RegistryEntry      Entry    `json:"-"`
}

func Catalog(profile string) []CatalogItem {
	generation := CatalogItem{
		ID: "assistant-large", DisplayName: "Gemma 4 E2B", Role: "generation",
		Description:  "The recommended private assistant for chat, writing, and knowledge work.",
		Capabilities: []string{"chat", "completions"}, DownloadBytes: 4_500_000_000,
		MinimumMemoryBytes: 32 * 1024 * 1024 * 1024, MinimumVRAMBytes: 24 * 1024 * 1024 * 1024,
		Recommended: true,
	}
	if profile == "metal-arm64" {
		generation.CompatibleProfiles = []string{"metal-arm64"}
		generation.MinimumVRAMBytes = 0
		generation.RegistryEntry = Entry{
			ID: generation.ID, Role: generation.Role, Source: "local",
			Model: "google/gemma-4-E2B-it-qat-q4_0-gguf", Revision: "69536a21d70340464240401ba38223d805f6a709",
			Artifact: "gemma-4-E2B_q4_0-it.gguf", SHA256: "3646b4c147cd235a44d91df1546d3b7d8e29b547dbe4e1f80856419aa455e6fd",
			Capabilities: generation.Capabilities, CompatibleProfiles: generation.CompatibleProfiles, ValidationState: "validated",
		}
	} else {
		generation.CompatibleProfiles = []string{"cuda-x86_64"}
		generation.RegistryEntry = Entry{
			ID: generation.ID, Role: generation.Role, Source: "huggingface", Model: "google/gemma-4-E2B-it",
			Revision: "9dbdf8a839e4e9e0eb56ed80cc8886661d3817cf", Capabilities: generation.Capabilities,
			CompatibleProfiles: generation.CompatibleProfiles, ValidationState: "validated",
		}
	}
	embedding := CatalogItem{
		ID: "embedding-gemma-default", DisplayName: "EmbeddingGemma", Role: "embedding",
		Description:  "Fast, private text embeddings. Recommended for most knowledge and search workloads.",
		Capabilities: []string{"text"}, CompatibleProfiles: []string{"cuda-x86_64", "metal-arm64"},
		DownloadBytes: 235_000_000, MinimumMemoryBytes: 2 * 1024 * 1024 * 1024, Recommended: true,
	}
	embedding.RegistryEntry = Entry{
		ID: embedding.ID, Role: embedding.Role, Source: "huggingface",
		Model: "ggml-org/embeddinggemma-300M-qat-q4_0-GGUF", Revision: "8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73",
		Artifact: "embeddinggemma-300M-qat-Q4_0.gguf", SHA256: "50d28e22432a148f6f8a86eab3700f92add5d1f54baf7790675a2a4dadbccf26",
		Capabilities: embedding.Capabilities, CompatibleProfiles: embedding.CompatibleProfiles, ValidationState: "validated",
	}
	return []CatalogItem{generation, embedding}
}

func CatalogEntry(profile, id string) (CatalogItem, error) {
	for _, item := range Catalog(profile) {
		if item.ID == id {
			return item, nil
		}
	}
	return CatalogItem{}, fmt.Errorf("catalog model %q not found", id)
}
