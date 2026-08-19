package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hardware"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
)

func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /key/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{
			map[string]any{"token": "hashed-key-id", "key_alias": "recursor", "models": []string{"assistant-large"}, "key": "not-a-customer-secret"},
		}})
	})
	mux.HandleFunc("POST /key/generate", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"key": "sk-one-time", "key_name": "hashed-key-id", "key_alias": "recursor",
			"models": []string{"assistant-large"}, "user_id": "internal-user",
		})
	})
	mux.HandleFunc("POST /key/delete", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func fakeRuntime(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "alive", "state": "healthy"})
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ready": true, "state": "healthy"})
	})
	mux.HandleFunc("/runtime/manifest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"schema_version": "1.1", "state": "healthy"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func fakeProxy(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/docker/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "docker": "29.0"})
	})
	mux.HandleFunc("GET /internal/docker/containers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"containers": []map[string]any{
			{"Id": "x", "State": "running", "Labels": map[string]string{"com.docker.compose.service": "sovereign-runtime"}},
		}})
	})
	mux.HandleFunc("POST /internal/docker/containers/sovereign-runtime/restart", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"restarted": "sovereign-runtime"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func testControl(t *testing.T) http.Handler {
	t.Helper()
	server := &Server{
		Runtime: runtime.New(fakeRuntime(t).URL),
		Proxy:   dockerproxy.New(fakeProxy(t).URL, "token"),
		Gateway: gateway.New("http://127.0.0.1:1", "key"), // unreachable → unhealthy
		Version: "test",
	}
	return server.Handler()
}

func get(handler http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestHealth(t *testing.T) {
	rec := get(testControl(t), BasePath+"/health")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("health: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMetrics(t *testing.T) {
	rec := get(testControl(t), "/metrics")
	if rec.Code != 200 {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "version=0.0.4") {
		t.Errorf("metrics content type: %q", got)
	}
	for _, metric := range []string{
		"sovereign_control_up 1",
		"sovereign_control_runtime_up 1",
		"sovereign_control_gateway_up 0",
		"sovereign_control_embeddings_up 0",
		"sovereign_control_docker_proxy_up 1",
	} {
		if !strings.Contains(rec.Body.String(), metric) {
			t.Errorf("metrics missing %q:\n%s", metric, rec.Body.String())
		}
	}
}

func TestStatusAggregation(t *testing.T) {
	rec := get(testControl(t), BasePath+"/status")
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	runtimeStatus := body["runtime"].(map[string]any)
	if runtimeStatus["reachable"] != true || runtimeStatus["ready"] != true {
		t.Errorf("runtime status: %v", runtimeStatus)
	}
	if body["gateway"].(map[string]any)["healthy"] != false {
		t.Errorf("gateway should be unhealthy in this test: %v", body["gateway"])
	}
	services := body["services"].(map[string]any)
	if services["sovereign-runtime"] != "running" {
		t.Errorf("services: %v", services)
	}
}

func TestManifestPassthrough(t *testing.T) {
	rec := get(testControl(t), BasePath+"/runtime/manifest")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"schema_version":"1.1"`) {
		t.Errorf("manifest: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeRestartViaProxy(t *testing.T) {
	handler := testControl(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", BasePath+"/runtime/restart", nil))
	if rec.Code != 202 {
		t.Errorf("restart: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfilesListsKnownProfiles(t *testing.T) {
	rec := get(testControl(t), BasePath+"/profiles")
	var body struct {
		Profiles []map[string]any `json:"profiles"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Profiles) != len(KnownProfiles) {
		t.Errorf("profiles: got %d, want %d", len(body.Profiles), len(KnownProfiles))
	}
}

func TestReadinessReportsIndependentComponents(t *testing.T) {
	rec := get(testControl(t), BasePath+"/readiness")
	if rec.Code != http.StatusOK {
		t.Fatalf("readiness: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Overall    string                    `json:"overall"`
		Components map[string]map[string]any `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"portal", "authentication", "generation", "embeddings", "gateway", "workspace", "observability"} {
		if body.Components[name]["state"] == nil {
			t.Errorf("readiness component %q missing state: %v", name, body.Components)
		}
	}
}

func TestApplicationRegistryIsControlledAndRoleFiltered(t *testing.T) {
	rec := get(testControl(t), BasePath+"/applications")
	if rec.Code != http.StatusOK {
		t.Fatalf("applications: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Applications []map[string]any `json:"applications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Applications) != 1 || body.Applications[0]["id"] != "chat" {
		t.Fatalf("unauthenticated test identity should receive member registry: %v", body.Applications)
	}
}

func TestGatewayKeysAreNormalizedWithPublicBaseURL(t *testing.T) {
	for _, path := range []string{
		BasePath + "/gateway/keys", BasePath + "/gateway/usage", BasePath + "/gateway/budgets",
	} {
		if !adminOnlyPath(path) {
			t.Fatalf("gateway administration path is not admin-only: %s", path)
		}
	}
	server := &Server{Gateway: gateway.New(fakeGateway(t).URL, "master")}
	handler := server.Handler()

	create := httptest.NewRequest(http.MethodPost, "https://ai.example.test"+BasePath+"/gateway/keys",
		bytes.NewBufferString(`{"key_alias":"recursor","models":["assistant-large"]}`))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"secret":"sk-one-time"`) ||
		!strings.Contains(created.Body.String(), `"base_url":"https://ai.example.test/api/openai/v1"`) {
		t.Fatalf("normalized key response: %s", created.Body.String())
	}
	if created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("key response cache control = %q", created.Header().Get("Cache-Control"))
	}
	for _, forbidden := range []string{"internal-user", "user_id"} {
		if strings.Contains(created.Body.String(), forbidden) {
			t.Fatalf("key response leaked %q: %s", forbidden, created.Body.String())
		}
	}

	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet,
		"https://ai.example.test"+BasePath+"/gateway/keys", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"hashed-key-id"`) {
		t.Fatalf("list keys: %d %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "not-a-customer-secret") {
		t.Fatalf("key list leaked implementation key field: %s", listed.Body.String())
	}

	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete,
		"https://ai.example.test"+BasePath+"/gateway/keys/hashed-key-id", nil))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete key: %d %s", deleted.Code, deleted.Body.String())
	}
}

func TestGatewayBaseURLRejectsUntrustedForwardedValues(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://ai.example.test/", nil)
	r.Header.Set("X-Forwarded-Proto", "javascript")
	r.Header.Set("X-Forwarded-Host", "evil.example/path")
	if got := publicGatewayBaseURL(r); got != "https://ai.example.test/api/openai/v1" {
		t.Fatalf("public gateway URL = %q", got)
	}
}

func TestCatalogCompatibilityChecksCapacityBeforeDownload(t *testing.T) {
	item, err := models.CatalogEntry("cuda-x86_64", "assistant-large")
	if err != nil {
		t.Fatal(err)
	}
	compatible, reason := catalogCompatibility(item, hardware.Inventory{
		Profile: "cuda-x86_64", MemoryBytes: 64 << 30, StorageFreeBytes: 1 << 30,
		GPU: &hardware.GPU{Name: "NVIDIA", VRAMBytes: 24 << 30},
	})
	if compatible || !strings.Contains(reason, "disk space") {
		t.Fatalf("low disk compatibility = %v, %q", compatible, reason)
	}
}
