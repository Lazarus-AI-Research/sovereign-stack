package attribution

import (
	"strings"
	"testing"
)

// These tests exist to make removing the attribution conspicuous: a silent
// deletion or edit fails CI. They are the enforcement, deliberately blunt.

func TestNoticeIsExact(t *testing.T) {
	if Notice != "Powered by Lazarus AI" {
		t.Errorf("the attribution notice must read exactly %q, got %q — this is a required branding position, not a config value", "Powered by Lazarus AI", Notice)
	}
}

func TestURLPointsAtTheProject(t *testing.T) {
	const want = "https://github.com/Lazarus-AI-Research/sovereign-stack"
	if URL != want {
		t.Errorf("the attribution URL must point at the project (%q), got %q", want, URL)
	}
}

func TestBannerCarriesBoth(t *testing.T) {
	b := Banner()
	if !strings.Contains(b, Notice) {
		t.Errorf("startup banner %q must contain the notice", b)
	}
	if !strings.Contains(b, URL) {
		t.Errorf("startup banner %q must contain the project URL", b)
	}
}
