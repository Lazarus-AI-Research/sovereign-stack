package branding

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "branding.yaml"), "Customer branding")
	if err := store.Put(map[string]any{"product_name": "Acme AI", "colors": map[string]any{"primary": "#123456"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	doc, err := store.Get()
	if err != nil || doc["product_name"] != "Acme AI" {
		t.Fatalf("get: %v %+v", err, doc)
	}
}

func TestFlagsPrivacyInvariants(t *testing.T) {
	// defaults-off documents are fine
	if err := ValidateFlags(map[string]any{
		"prompt_logging": map[string]any{"enabled": false},
		"tracing":        map[string]any{"metadata_only": true, "full_trace": false},
	}); err != nil {
		t.Errorf("defaults rejected: %v", err)
	}

	// enabling prompt logging without the §2.4 companions is rejected
	if err := ValidateFlags(map[string]any{
		"prompt_logging": map[string]any{"enabled": true},
	}); err == nil {
		t.Error("bare prompt_logging enable accepted")
	}

	// fully specified capture is accepted
	if err := ValidateFlags(map[string]any{
		"prompt_logging": map[string]any{
			"enabled": true, "pii_redaction": true, "secret_redaction": true,
			"retention_days": 30, "scope": "workspace:abc",
		},
	}); err != nil {
		t.Errorf("explicit capture rejected: %v", err)
	}

	// full_trace without prompt logging config is rejected (§2.3)
	if err := ValidateFlags(map[string]any{
		"tracing": map[string]any{"full_trace": true, "metadata_only": false},
	}); err == nil {
		t.Error("full_trace without prompt logging accepted")
	}
}
