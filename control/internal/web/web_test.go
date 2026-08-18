package web

import (
	"io/fs"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestPortalAssetsUseDedicatedRoute(t *testing.T) {
	handler := Handler()
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	if index.Code != 200 {
		t.Fatalf("index status: %d", index.Code)
	}
	match := regexp.MustCompile(`(?:src|href)="(/portal-assets/[^"]+)"`).FindStringSubmatch(index.Body.String())
	if len(match) != 2 {
		t.Fatalf("portal index does not reference /portal-assets: %s", index.Body.String())
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest("GET", match[1], nil))
	if asset.Code != 200 || strings.Contains(asset.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("asset %s: status=%d content-type=%q", match[1], asset.Code, asset.Header().Get("Content-Type"))
	}
}

func TestBuiltPortalContainsRecoveryAndNavigationRegressions(t *testing.T) {
	files, err := fs.ReadDir(dist, "dist/portal-assets")
	if err != nil {
		t.Fatal(err)
	}
	var dashboard, stylesheet string
	for _, file := range files {
		raw, readErr := fs.ReadFile(dist, "dist/portal-assets/"+file.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasPrefix(file.Name(), "Dashboard-") && strings.HasSuffix(file.Name(), ".js") {
			dashboard = string(raw)
		}
		if strings.HasPrefix(file.Name(), "index-") && strings.HasSuffix(file.Name(), ".css") {
			stylesheet = string(raw)
		}
	}
	for _, expected := range []string{
		"Retry with a new session", "No backups yet", "Model library",
		"Copy this key now", "Revoke gateway key", "Expand sidebar",
	} {
		if !strings.Contains(dashboard, expected) {
			t.Errorf("built Dashboard asset is missing %q", expected)
		}
	}
	if !strings.Contains(stylesheet, ".sidebar-collapsed .brand .collapse-button{display:inline-grid") {
		t.Error("collapsed sidebar does not keep its expand control visible")
	}
}
