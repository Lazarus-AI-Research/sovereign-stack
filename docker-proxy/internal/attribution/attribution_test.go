package attribution

import (
	"strings"
	"testing"
)

// Enforcement: a silent removal or edit fails CI. The values must match the
// canonical string in BRANDING.md and the control module's attribution package.

func TestNoticeIsExact(t *testing.T) {
	if Notice != "Powered by Lazarus AI" {
		t.Errorf("attribution notice must read exactly %q, got %q", "Powered by Lazarus AI", Notice)
	}
}

func TestURLPointsAtTheProject(t *testing.T) {
	const want = "https://github.com/Lazarus-AI-Research/sovereign-stack"
	if URL != want {
		t.Errorf("attribution URL must be %q, got %q", want, URL)
	}
}

func TestBannerCarriesBoth(t *testing.T) {
	b := Banner()
	if !strings.Contains(b, Notice) || !strings.Contains(b, URL) {
		t.Errorf("banner %q must contain both the notice and the URL", b)
	}
}
