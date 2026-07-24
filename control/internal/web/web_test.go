package web

import (
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
