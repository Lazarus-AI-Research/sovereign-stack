package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
)

func TestGenerateConfigRoutesAliasesToOwningServices(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.yaml")
	profilesPath := filepath.Join(dir, "profiles.yaml")
	outputPath := filepath.Join(dir, "litellm.yaml")
	if err := os.WriteFile(modelsPath, []byte("models: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := `embedding_profiles:
  gemma-default:
    provider: embeddinggemma
    model: ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
    served_model_name: embedding-gemma-default
`
	if err := os.WriteFile(profilesPath, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := GenerateConfig(context.Background(), outputPath,
		models.NewRegistry(modelsPath), embeddings.NewRegistry(profilesPath), nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		ModelList []struct {
			ModelName     string            `yaml:"model_name"`
			LiteLLMParams map[string]string `yaml:"litellm_params"`
		} `yaml:"model_list"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	routes := map[string]map[string]string{}
	for _, route := range config.ModelList {
		routes[route.ModelName] = route.LiteLLMParams
	}
	if got := routes["assistant-large"]["api_base"]; got != "http://sovereign-runtime:8000/v1" {
		t.Fatalf("generation api_base = %q", got)
	}
	if got := routes["assistant-large"]["api_key"]; got != "os.environ/SOVEREIGN_RUNTIME_API_KEY" {
		t.Fatalf("generation api_key = %q", got)
	}
	if got := routes["embedding-gemma-default"]["api_base"]; got != "os.environ/SOVEREIGN_EMBEDDINGS_BASE_URL" {
		t.Fatalf("embedding api_base = %q", got)
	}
	if got := routes["embedding-gemma-default"]["api_key"]; got != "not-required" {
		t.Fatalf("embedding api_key = %q", got)
	}
}
