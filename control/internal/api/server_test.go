package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
)

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
