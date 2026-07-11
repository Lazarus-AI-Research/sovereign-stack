package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/allowlist"
	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/audit"
	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/dockerapi"
)

// fakeDocker implements just enough of the Engine API for handler tests.
func fakeDocker(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"Version": "fake"})
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "abc123", "Names": []string{"/sovereign-runtime"}, "State": "running"},
		})
	})
	mux.HandleFunc("/containers/abc123/restart", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	return httptest.NewServer(mux)
}

func testServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	docker := fakeDocker(t)
	t.Cleanup(docker.Close)
	var auditBuf bytes.Buffer
	return &Server{
		Allowlist: &allowlist.Config{
			AllowedProject:       "sovereign-stack",
			AllowedImagePrefixes: []string{"ghcr.io/lazarus-ai-research/"},
			AllowedServices:      []string{"sovereign-runtime"},
			AllowedOperations:    []string{"inspect", "list", "restart", "pull", "logs", "run-evals"},
		},
		Audit:   audit.NewWithWriter(&auditBuf),
		Docker:  dockerapi.NewForTest(docker.URL, docker.Client()),
		Token:   "secret",
		Version: "test",
	}, &auditBuf
}

func request(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	server, auditBuf := testServer(t)
	handler := server.Handler()
	if rec := request(handler, "GET", "/internal/docker/status", "", ""); rec.Code != 401 {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
	if rec := request(handler, "GET", "/internal/docker/status", "wrong", ""); rec.Code != 401 {
		t.Errorf("bad token: got %d, want 401", rec.Code)
	}
	if !strings.Contains(auditBuf.String(), `"decision":"deny"`) {
		t.Error("denied requests must be audited")
	}
}

func TestStatusOK(t *testing.T) {
	server, _ := testServer(t)
	rec := request(server.Handler(), "GET", "/internal/docker/status", "secret", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("status: got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRestartAllowedService(t *testing.T) {
	server, auditBuf := testServer(t)
	rec := request(server.Handler(), "POST", "/internal/docker/containers/sovereign-runtime/restart", "secret", "")
	if rec.Code != 200 {
		t.Fatalf("restart allowed service: got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(auditBuf.String(), `"decision":"allow"`) {
		t.Error("allowed restart must be audited")
	}
}

func TestRestartArbitraryServiceRejected(t *testing.T) {
	server, _ := testServer(t)
	rec := request(server.Handler(), "POST", "/internal/docker/containers/postgres/restart", "secret", "")
	if rec.Code != 403 {
		t.Errorf("restart of non-allowlisted service: got %d, want 403", rec.Code)
	}
}

func TestPullArbitraryImageRejected(t *testing.T) {
	server, auditBuf := testServer(t)
	rec := request(
		server.Handler(), "POST", "/internal/docker/images/pull", "secret",
		`{"image": "docker.io/library/alpine"}`,
	)
	if rec.Code != 403 {
		t.Errorf("arbitrary image pull: got %d, want 403", rec.Code)
	}
	if !strings.Contains(auditBuf.String(), "does not match any allowed prefix") {
		t.Error("deny reason must be audited")
	}
}

func TestEvalsRunRejectsNonEvalsImage(t *testing.T) {
	server, _ := testServer(t)
	rec := request(
		server.Handler(), "POST", "/internal/docker/evals/run", "secret",
		`{"image": "ghcr.io/lazarus-ai-research/sovereign-control:1.0", "suite": "smoke"}`,
	)
	if rec.Code != 403 {
		t.Errorf("evals run with non-evals image: got %d, want 403", rec.Code)
	}
}
