package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/branding"
)

func TestPublicThemeAllowlist(t *testing.T) {
	// A branding doc mixing cosmetic fields with something that must not leak.
	doc := map[string]any{
		"product_name": "Acme AI",
		"company_name": "Acme",
		"logo":         "/assets/acme.png",
		"favicon":      "/assets/acme.ico",
		"colors":       map[string]any{"primary": "#101820", "accent": "#00b3a4"},
		"secret_note":  "must not be served publicly",
	}
	got := publicTheme(doc)

	for _, k := range []string{"product_name", "company_name", "logo", "favicon", "colors"} {
		if _, ok := got[k]; !ok {
			t.Errorf("theme is missing cosmetic field %q", k)
		}
	}
	if _, leaked := got["secret_note"]; leaked {
		t.Error("publicTheme leaked a non-cosmetic field; it must serve an allowlist, not the whole doc")
	}
}

func TestThemeIsPublicButBrandingIsGated(t *testing.T) {
	// /theme must be open so the login page can theme itself; the full branding
	// document (edited via /branding) stays behind a session.
	if !openPath(BasePath + "/theme") {
		t.Error("/theme must be an open path — the login page reads it before auth")
	}
	if openPath(BasePath + "/branding") {
		t.Error("/branding must stay behind auth; only the cosmetic /theme subset is public")
	}
}

func TestThemeServedWithoutSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "branding.yaml")
	if err := os.WriteFile(path, []byte("product_name: Acme AI\ncolors:\n  accent: \"#00b3a4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Auth left nil: the point here is that the handler serves the cosmetic
	// subset. Auth gating is covered by TestThemeIsPublicButBrandingIsGated.
	s := &Server{Branding: branding.NewStore(path, "Customer branding")}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", BasePath+"/theme", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /theme = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["product_name"] != "Acme AI" {
		t.Errorf("theme body = %v", body)
	}
	if _, ok := body["colors"]; !ok {
		t.Error("theme must carry colors")
	}
}
