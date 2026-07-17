package web

import (
	"io/fs"
	"strings"
	"testing"
)

// The built UI bundle must carry the "Powered by Lazarus AI" attribution. This
// checks the actually-embedded dist — the bytes that ship — so removing the
// PoweredBy component and rebuilding fails here, not just editing source. A
// required branding position; see BRANDING.md.
func TestBundleContainsAttribution(t *testing.T) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		t.Fatal(err)
	}

	var jsFiles, cssFiles int
	foundNotice, foundURL := false, false
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch {
		case strings.HasSuffix(path, ".js"):
			jsFiles++
		case strings.HasSuffix(path, ".css"):
			cssFiles++
		default:
			return nil
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		s := string(data)
		if strings.Contains(s, "Powered by Lazarus AI") {
			foundNotice = true
		}
		if strings.Contains(s, "https://github.com/Lazarus-AI-Research/sovereign-stack") {
			foundURL = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if jsFiles == 0 {
		t.Skip("no built JS bundle in dist (run `npm run build` in control/web); nothing to enforce yet")
	}
	if !foundNotice {
		t.Error(`the built UI bundle does not contain "Powered by Lazarus AI" — the PoweredBy attribution must ship on every page (BRANDING.md)`)
	}
	if !foundURL {
		t.Error("the built UI bundle does not link to the project — the attribution must point at https://github.com/Lazarus-AI-Research/sovereign-stack")
	}
}
