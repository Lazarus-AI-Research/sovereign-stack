package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(filepath.Join(t.TempDir(), "model-registry.yaml"))
}

func TestRegistryCRUD(t *testing.T) {
	registry := testRegistry(t)

	entry := Entry{ID: "assistant-dev", Role: "generation", Source: "huggingface", Model: "Qwen/Qwen3-0.6B", Revision: "main"}
	if err := registry.Add(entry); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := registry.Add(entry); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if err := registry.Add(Entry{ID: "x", Role: "wizard", Source: "huggingface", Model: "m"}); err == nil {
		t.Fatal("bad role accepted")
	}

	updated, err := registry.Update("assistant-dev", map[string]string{"revision": "abc123"})
	if err != nil || updated.Revision != "abc123" {
		t.Fatalf("update: %v %+v", err, updated)
	}

	got, err := registry.Get("assistant-dev")
	if err != nil || got.Revision != "abc123" {
		t.Fatalf("get: %v %+v", err, got)
	}

	if err := registry.Delete("assistant-dev"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := registry.Get("assistant-dev"); err == nil {
		t.Fatal("deleted entry still present")
	}
}

func TestLoadJobRewritesConfigRestartsAndWaits(t *testing.T) {
	registry := testRegistry(t)
	registry.Add(Entry{ID: "new-model", Role: "generation", Source: "huggingface", Model: "org/new-model", Revision: "rev1"})

	configPath := filepath.Join(t.TempDir(), "runtime.yaml")
	os.WriteFile(configPath, []byte(`
schema_version: "1.1"
roles:
  generation:
    enabled: true
    task: generate
    source: huggingface
    model: org/old-model
    served_model_name: assistant-large
  embedding:
    enabled: false
    task: embed
`), 0o644)

	var restarted atomic.Bool
	var readyAfter atomic.Int32
	readyAfter.Store(2)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/restart"):
			restarted.Store(true)
			w.Write([]byte(`{"restarted":"sovereign-runtime"}`))
		case strings.HasSuffix(r.URL.Path, "/health/ready"):
			if readyAfter.Add(-1) > 0 {
				w.WriteHeader(503)
				w.Write([]byte(`{"ready":false,"state":"loading"}`))
				return
			}
			w.Write([]byte(`{"ready":true,"state":"healthy"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()

	deps := LoadDeps{
		Registry:     registry,
		ConfigPath:   configPath,
		Proxy:        dockerproxy.New(fake.URL, "token"),
		Runtime:      runtime.New(fake.URL),
		ReadyTimeout: 20 * time.Second,
	}
	payload, _ := json.Marshal(LoadPayload{ModelID: "new-model"})
	result, err := deps.HandleLoad(context.Background(), payload)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !restarted.Load() {
		t.Error("runtime was not restarted via proxy")
	}
	if result.(map[string]any)["state"] != "healthy" {
		t.Errorf("result: %+v", result)
	}

	raw, _ := os.ReadFile(configPath)
	var config map[string]any
	yaml.Unmarshal(raw, &config)
	role := config["roles"].(map[string]any)["generation"].(map[string]any)
	if role["model"] != "org/new-model" || role["revision"] != "rev1" {
		t.Errorf("config not rewritten: %+v", role)
	}
	if role["served_model_name"] != "assistant-large" {
		t.Errorf("stable alias lost: %+v", role)
	}
}

func TestEmbeddingLoadWritesStableAliasAndPoolerPolicy(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "runtime.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.1"
roles:
  generation:
    enabled: true
    task: generate
    source: local
    model: generation.gguf
    served_model_name: assistant-large
  embedding:
    enabled: false
    task: embed
`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := LoadDeps{ConfigPath: configPath}
	entry := Entry{ID: "research", Role: "embedding", Source: "offline", Model: "/models/research", Revision: "abc"}
	if err := deps.rewriteRole(entry, LoadPayload{
		ServedModelName: "research-default", Pooling: "last", Normalization: "none",
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(configPath)
	var config map[string]any
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	role := config["roles"].(map[string]any)["embedding"].(map[string]any)
	if role["enabled"] != true || role["source"] != "local" || role["served_model_name"] != "research-default" {
		t.Errorf("embedding role identity not applied: %+v", role)
	}
	if role["pooling"] != "last" || role["normalization"] != "none" {
		t.Errorf("embedding pooler policy not applied: %+v", role)
	}
}

func TestFailedLoadRestoresPreviousRuntimeConfiguration(t *testing.T) {
	registry := testRegistry(t)
	if err := registry.Add(Entry{ID: "bad-model", Role: "generation", Source: "huggingface", Model: "org/bad", Revision: "rev2"}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "runtime.yaml")
	previous := []byte(`schema_version: "1.1"
roles:
  generation:
    enabled: true
    task: generate
    source: huggingface
    model: org/good
    served_model_name: assistant-large
`)
	if err := os.WriteFile(configPath, previous, 0o644); err != nil {
		t.Fatal(err)
	}
	var restarts atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/restart"):
			restarts.Add(1)
			w.Write([]byte(`{"restarted":"sovereign-runtime"}`))
		case strings.HasSuffix(r.URL.Path, "/health/ready"):
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ready":false,"state":"configuration_error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()
	deps := LoadDeps{
		Registry: registry, ConfigPath: configPath,
		Proxy: dockerproxy.New(fake.URL, "token"), Runtime: runtime.New(fake.URL), ReadyTimeout: 5 * time.Second,
	}
	if _, err := deps.Load(context.Background(), LoadPayload{ModelID: "bad-model"}); err == nil {
		t.Fatal("failed runtime candidate was accepted")
	}
	if restarts.Load() != 2 {
		t.Fatalf("restart count = %d, want candidate plus rollback", restarts.Load())
	}
	restored, _ := os.ReadFile(configPath)
	if string(restored) != string(previous) {
		t.Errorf("runtime config was not restored:\n%s", restored)
	}
}
