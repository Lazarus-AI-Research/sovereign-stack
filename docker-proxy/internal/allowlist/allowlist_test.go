package allowlist

import (
	"os"
	"path/filepath"
	"testing"
)

func testConfig() *Config {
	return &Config{
		AllowedProject:       "sovereign-stack",
		AllowedImagePrefixes: []string{"ghcr.io/lazarus-ai-research/"},
		AllowedServices:      []string{"sovereign-runtime", "sovereign-gateway"},
		AllowedOperations:    []string{"inspect", "list", "restart", "pull", "logs", "run-evals"},
		Evals: EvalsJob{
			ImagePrefix: "ghcr.io/lazarus-ai-research/sovereign-evals",
			Network:     "sovereign",
		},
	}
}

// §25 security rows: arbitrary image, service, and command are rejected.

func TestArbitraryImageRejected(t *testing.T) {
	cases := []struct {
		image string
		want  bool
	}{
		{"ghcr.io/lazarus-ai-research/sovereign-runtime:cpu-x86_64-0.1.0", true},
		{"docker.io/library/alpine:latest", false},
		{"ghcr.io/attacker/lazarus-ai-research/evil", false},
		{"ghcr.io/lazarus-ai-researchevil/image", false},
		{"ghcr.io/lazarus-ai-research/ok@sha256:deadbeef", true},
		{"evil.example.com/ghcr.io/lazarus-ai-research/x", false},
		{"", false},
	}
	config := testConfig()
	for _, tc := range cases {
		if got := config.ImageAllowed(tc.image).Allowed; got != tc.want {
			t.Errorf("ImageAllowed(%q) = %v, want %v", tc.image, got, tc.want)
		}
	}
}

func TestArbitraryServiceRejected(t *testing.T) {
	config := testConfig()
	for service, want := range map[string]bool{
		"sovereign-runtime":  true,
		"sovereign-gateway":  true,
		"postgres":           false, // not restartable via proxy in this config
		"sovereign-runtime2": false,
		"../etc":             false,
		"":                   false,
	} {
		if got := config.ServiceAllowed(service).Allowed; got != want {
			t.Errorf("ServiceAllowed(%q) = %v, want %v", service, got, want)
		}
	}
}

func TestArbitraryOperationRejected(t *testing.T) {
	config := testConfig()
	for op, want := range map[string]bool{
		"restart": true,
		"exec":    false, // arbitrary command execution is never allowed
		"create":  false,
		"delete":  false,
	} {
		if got := config.OperationAllowed(op).Allowed; got != want {
			t.Errorf("OperationAllowed(%q) = %v, want %v", op, got, want)
		}
	}
}

func TestEvalsImageMustBeEvalsImage(t *testing.T) {
	config := testConfig()
	// A Lazarus image that is not the evals image must still be rejected here.
	if config.EvalsImageAllowed("ghcr.io/lazarus-ai-research/sovereign-control:1.0").Allowed {
		t.Error("non-evals Lazarus image accepted for evals run")
	}
	if !config.EvalsImageAllowed("ghcr.io/lazarus-ai-research/sovereign-evals:1.0").Allowed {
		t.Error("legitimate evals image rejected")
	}
}

func TestExportAllowsOnlyFirstPartyOrExactPins(t *testing.T) {
	config := testConfig()
	config.AllowedExportImages = []string{"caddy@sha256:exact"}
	if !config.ExportImageAllowed("ghcr.io/lazarus-ai-research/sovereign-control:0.1.0").Allowed {
		t.Fatal("first-party image should be exportable")
	}
	if !config.ExportImageAllowed("caddy@sha256:exact").Allowed {
		t.Fatal("exact third-party pin should be exportable")
	}
	if config.ExportImageAllowed("caddy:latest").Allowed {
		t.Fatal("mutable third-party image should be rejected")
	}
}

func TestLoadValidatesRequiredFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.yaml")
	os.WriteFile(path, []byte("allowed_project: \"\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("expected error for missing allowed_project")
	}
	os.WriteFile(path, []byte("allowed_project: p\nallowed_image_prefixes: [x/]\n"), 0o644)
	if _, err := Load(path); err != nil {
		t.Errorf("valid minimal config rejected: %v", err)
	}
}
