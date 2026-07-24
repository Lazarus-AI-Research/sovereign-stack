package hostservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls [][]string
}

func (r *recordingRunner) Run(_ context.Context, operation string, arguments ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{operation}, arguments...))
	return []byte("ok"), nil
}

func TestNetworkArgumentsAreAllowlisted(t *testing.T) {
	tests := []struct {
		mode, target string
		ok           bool
	}{
		{"desktop", "", true}, {"lan", "192.168.1.8", true}, {"domain", "ai.example.com", true},
		{"lan", "8.8.8.8", false}, {"domain", "https://example.com", false}, {"wan-http", "example.com", false},
	}
	for _, test := range tests {
		_, err := networkArguments(test.mode, test.target)
		if (err == nil) != test.ok {
			t.Errorf("networkArguments(%q, %q): err=%v, want ok=%v", test.mode, test.target, err, test.ok)
		}
	}
}

func TestHandlerRequiresAuthenticationAndOnlyRunsAllowlistedArguments(t *testing.T) {
	runner := &recordingRunner{}
	home := t.TempDir()
	server := New(home, "/unused", "secret-token")
	server.Runner = runner
	server.MutationDelay = 0
	handler := server.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, BasePath+"/repair", strings.NewReader("{}")))
	if unauthorized.Code != http.StatusUnauthorized || len(runner.calls) != 0 {
		t.Fatalf("unauthorized repair: status=%d calls=%v", unauthorized.Code, runner.calls)
	}

	rejected := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, BasePath+"/network", strings.NewReader(`{"mode":"domain","target":"example.com; touch /tmp/pwned"}`))
	request.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusBadRequest || len(runner.calls) != 0 {
		t.Fatalf("injected network request: status=%d calls=%v", rejected.Code, runner.calls)
	}

	accepted := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, BasePath+"/network", strings.NewReader(`{"mode":"lan","target":"192.168.10.25"}`))
	request.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusAccepted || len(runner.calls) != 1 {
		t.Fatalf("valid network request: status=%d calls=%v", accepted.Code, runner.calls)
	}
	want := []string{"network", "access", "lan", "192.168.10.25"}
	if strings.Join(runner.calls[0], "|") != strings.Join(want, "|") {
		t.Fatalf("runner arguments=%v, want %v", runner.calls[0], want)
	}
	audit, err := os.ReadFile(filepath.Join(home, "logs", "hostd", "operations.jsonl"))
	if err != nil || !strings.Contains(string(audit), `"outcome":"succeeded"`) || strings.Contains(string(audit), "touch") {
		t.Fatalf("safe operation audit: %q, %v", audit, err)
	}
}

func TestHealthIsTheOnlyUnauthenticatedHostRoute(t *testing.T) {
	server := New(t.TempDir(), "/unused", "secret-token")
	server.Runner = &recordingRunner{}
	for _, test := range []struct {
		path string
		want int
	}{{BasePath + "/health", http.StatusOK}, {BasePath + "/status", http.StatusUnauthorized}, {BasePath + "/updates", http.StatusUnauthorized}} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.want {
			t.Errorf("GET %s = %d, want %d", test.path, recorder.Code, test.want)
		}
	}
}
