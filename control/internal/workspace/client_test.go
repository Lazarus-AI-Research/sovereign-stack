package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetAppNameUpdatesDisplayNameAndPageTitle(t *testing.T) {
	t.Helper()

	var preferences map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/admin/system-preferences" {
			t.Fatalf("path = %s, want /api/admin/system-preferences", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&preferences); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := New(server.URL).SetAppName(context.Background(), "Lazarus"); err != nil {
		t.Fatalf("SetAppName: %v", err)
	}
	if got := preferences["custom_app_name"]; got != "Lazarus" {
		t.Fatalf("custom_app_name = %q, want Lazarus", got)
	}
	if got := preferences["meta_page_title"]; got != "Lazarus" {
		t.Fatalf("meta_page_title = %q, want Lazarus", got)
	}
}
