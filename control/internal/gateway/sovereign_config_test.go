package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSecrets resolves whatever it was given, and errors on anything else —
// like the real vault does for an unconfigured credential.
type fakeSecrets map[string]string

func (f fakeSecrets) Secret(_ context.Context, id string) (string, error) {
	if v, ok := f[id]; ok {
		return v, nil
	}
	return "", os.ErrNotExist
}

const testBYOK = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSovereignConfigRequiresBYOKKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.toml")
	c := NewSovereign("http://127.0.0.1:1", "yb_admin")

	// No vault at all.
	err := c.writeServeConfig(context.Background(), path, nil)
	if err == nil {
		t.Fatal("expected an error: without byok_key the gateway stores provider keys in plaintext")
	}
	if !strings.Contains(err.Error(), "plaintext") {
		t.Errorf("the error must say what is at stake, got: %v", err)
	}

	// Vault present but the key is not configured.
	err = c.writeServeConfig(context.Background(), path, fakeSecrets{})
	if err == nil {
		t.Fatal("expected an error when the byok credential is missing")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("no config may be written when byok_key is unresolvable")
	}
}

func TestSovereignConfigHardensDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.toml")
	c := NewSovereign("http://127.0.0.1:1", "yb_admin")

	if err := c.writeServeConfig(context.Background(), path, fakeSecrets{BYOKKeyID: testBYOK}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	// The gateway's own defaults are all wrong for an appliance; the generated
	// config is what makes them right.
	for _, want := range []string{
		`byok_key = "` + testBYOK + `"`, // else provider keys are plaintext at rest
		"budgets_enabled = true",        // §2.2 requires budgets
		"ratelimit_enabled = true",      // §2.2 requires rate limits
		"enabled = false",               // §2.4: no prompt/response capture
		`deployment_mode = "selfhosted"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated config is missing %q:\n%s", want, got)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// It holds the secret-at-rest key.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600: it carries byok_key", perm)
	}
}

// Models are pushed over the admin API and hot-reload, so a model change must
// not ask for a restart.
func TestSovereignNeedsNoReload(t *testing.T) {
	if NewSovereign("", "").NeedsReload() {
		t.Error("sovereign-gateway hot-reloads models; it must not request a restart")
	}
	if !New("", "").NeedsReload() {
		t.Error("litellm reads model_list from its config file and does need one")
	}
}

func TestSovereignSyncModelsReconciles(t *testing.T) {
	var added []map[string]any
	var deleted []string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/v1/models", func(w http.ResponseWriter, r *http.Request) {
		// One current model the product still wants, one it does not.
		json.NewEncoder(w).Encode([]any{
			map[string]any{"id": "d1", "model_name": "assistant-large", "provider": "custom"},
			map[string]any{"id": "d2", "model_name": "retired-model", "provider": "custom"},
		})
	})
	mux.HandleFunc("POST /admin/v1/models", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		added = append(added, body)
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("DELETE /admin/v1/models/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleted = append(deleted, r.PathValue("id"))
		w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := NewSovereign(srv.URL, "yb_admin")
	desired := []map[string]any{
		{"model_name": "assistant-large"},        // already present -> untouched
		{"model_name": "embedding-omni-default"}, // new -> added
	}
	if err := c.reconcile(context.Background(), desired); err != nil {
		t.Fatal(err)
	}

	if len(added) != 1 || added[0]["model_name"] != "embedding-omni-default" {
		t.Errorf("added = %v, want only the new model", added)
	}
	if len(deleted) != 1 || deleted[0] != "d2" {
		t.Errorf("deleted = %v, want the retired model gone so it stops routing", deleted)
	}
}

// An unknown provider must not be guessed into a dialect: the wrong one fails
// later, at request time, with a far worse error.
func TestUnknownProviderRejected(t *testing.T) {
	if _, err := upstreamFormat("bedrock"); err == nil {
		t.Error("an unmapped provider must be an error, not a guess")
	}
	for _, p := range []string{"anthropic", "openai", "openai-compatible", "custom", "gemini", ""} {
		if _, err := upstreamFormat(p); err != nil {
			t.Errorf("provider %q should map: %v", p, err)
		}
	}
}
