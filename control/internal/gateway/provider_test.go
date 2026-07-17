package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Both implementations must satisfy the same contract, so the shared behaviour
// is asserted once and run against each. Anything a provider genuinely cannot
// express is asserted separately, and must fail loudly rather than silently.

func ptrF(v float64) *float64 { return &v }
func ptrI(v int64) *int64     { return &v }

// ---- LiteLLM ------------------------------------------------------------

// liteLLMFake serves the subset of LiteLLM's management API Control uses.
func liteLLMFake(t *testing.T, seen *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health/liveliness", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": "assistant-large"}}})
	})
	mux.HandleFunc("/key/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"token":      "sk-abc",
			"key_alias":  "web",
			"models":     []string{"assistant-large"},
			"max_budget": 12.5,
			"spend":      2.25,
			"rpm_limit":  60,
		}}})
	})
	mux.HandleFunc("/key/generate", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		(*seen)["generate"] = body
		json.NewEncoder(w).Encode(map[string]any{"key": "sk-new", "key_alias": "web"})
	})
	mux.HandleFunc("/key/delete", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		(*seen)["delete"] = body
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/key/update", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		(*seen)["update"] = body
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/global/spend", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{map[string]any{"api_key": "sk-abc", "spend": 2.25, "api_requests": 7}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLiteLLMNormalizesKeys(t *testing.T) {
	seen := map[string]any{}
	srv := liteLLMFake(t, &seen)
	c := New(srv.URL, "master")

	if !c.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}
	if got := c.Name(); got != "litellm" {
		t.Errorf("Name = %q", got)
	}

	keys, err := c.ListKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys", len(keys))
	}
	// LiteLLM addresses a key by its token, so that is the handle.
	if keys[0].ID != "sk-abc" {
		t.Errorf("ID = %q, want the token", keys[0].ID)
	}
	if keys[0].Alias != "web" || keys[0].MaxBudget == nil || *keys[0].MaxBudget != 12.5 {
		t.Errorf("normalize dropped fields: %+v", keys[0])
	}
}

func TestLiteLLMDeleteUsesTokenHandle(t *testing.T) {
	seen := map[string]any{}
	srv := liteLLMFake(t, &seen)
	if err := New(srv.URL, "master").DeleteKey(context.Background(), "sk-abc"); err != nil {
		t.Fatal(err)
	}
	body, _ := seen["delete"].(map[string]any)
	list, _ := body["keys"].([]any)
	if len(list) != 1 || list[0] != "sk-abc" {
		t.Errorf("delete body = %v", body)
	}
}

// LiteLLM is the only provider that can express expiry; it must pass through.
func TestLiteLLMPassesDurationThrough(t *testing.T) {
	seen := map[string]any{}
	srv := liteLLMFake(t, &seen)
	_, err := New(srv.URL, "master").GenerateKey(context.Background(), KeyRequest{Duration: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := seen["generate"].(map[string]any)
	if body["duration"] != "30d" {
		t.Errorf("duration not forwarded: %v", body)
	}
}

func TestLiteLLMUsage(t *testing.T) {
	seen := map[string]any{}
	srv := liteLLMFake(t, &seen)
	rows, err := New(srv.URL, "master").Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Subject != "sk-abc" || rows[0].Spend != 2.25 || rows[0].Requests != 7 {
		t.Errorf("usage = %+v", rows)
	}
}

// ---- SovereignGateway ---------------------------------------------------

// sovereignFake serves the subset of the gateway's /admin/v1 API Control uses,
// in the shapes docs/openapi.yaml declares.
func sovereignFake(t *testing.T, seen *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []any{map[string]any{"id": "assistant-large", "object": "model"}},
		})
	})
	mux.HandleFunc("GET /admin/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{map[string]any{
			"id":         "key-uuid-1",
			"name":       "web",
			"access":     map[string]any{"allowed_models": []string{"assistant-large"}},
			"rpm_limit":  60,
			"created_at": "2026-07-17T00:00:00Z",
		}})
	})
	mux.HandleFunc("POST /admin/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		(*seen)["create"] = body
		json.NewEncoder(w).Encode(map[string]any{
			"key":   map[string]any{"id": "key-uuid-1", "name": "web"},
			"token": "yb_secret",
		})
	})
	mux.HandleFunc("DELETE /admin/v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		(*seen)["deleted"] = r.PathValue("id")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("PUT /admin/v1/budgets", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		(*seen)["budget"] = body
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /admin/v1/spend", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{map[string]any{
			"subject_type":  "key",
			"subject_id":    "key-uuid-1",
			"spend_micros":  2250000,
			"request_count": 7,
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSovereignNormalizesKeys(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)
	c := NewSovereign(srv.URL, "yb_admin")

	if !c.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}
	if got := c.Name(); got != "sovereign" {
		t.Errorf("Name = %q", got)
	}

	keys, err := c.ListKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys", len(keys))
	}
	// The gateway addresses a key by an opaque id, not by its token.
	if keys[0].ID != "key-uuid-1" {
		t.Errorf("ID = %q, want the opaque id", keys[0].ID)
	}
	if keys[0].Alias != "web" {
		t.Errorf("name did not map to Alias: %+v", keys[0])
	}
	if len(keys[0].Models) != 1 || keys[0].Models[0] != "assistant-large" {
		t.Errorf("access.allowed_models did not map to Models: %+v", keys[0])
	}
}

// The interesting half of the port: a LiteLLM-shaped request with a dollar
// budget becomes a key plus a micros-denominated Budget row.
func TestSovereignGenerateKeySplitsBudgetAndConvertsToMicros(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)

	issued, err := NewSovereign(srv.URL, "yb_admin").GenerateKey(context.Background(), KeyRequest{
		Alias:     "web",
		Models:    []string{"assistant-large"},
		MaxBudget: ptrF(12.5),
		RPMLimit:  ptrI(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Token != "yb_secret" {
		t.Errorf("token = %q", issued.Token)
	}
	if issued.ID != "key-uuid-1" {
		t.Errorf("ID = %q", issued.ID)
	}

	create, _ := seen["create"].(map[string]any)
	if create["name"] != "web" {
		t.Errorf("alias did not map to name: %v", create)
	}
	// A budget must not be smuggled onto key creation — the gateway has no such field.
	if _, ok := create["max_budget"]; ok {
		t.Error("max_budget must not be sent to the key endpoint")
	}

	budget, _ := seen["budget"].(map[string]any)
	if budget == nil {
		t.Fatal("no budget row was written")
	}
	// 12.5 USD -> 12_500_000 micros, exactly.
	if got := budget["hard_limit_micros"].(float64); got != 12_500_000 {
		t.Errorf("hard_limit_micros = %v, want 12500000", got)
	}
	if budget["subject_type"] != "key" || budget["subject_id"] != "key-uuid-1" {
		t.Errorf("budget not attached to the key: %v", budget)
	}
	// PUT /budgets takes the whole Budget; a partial body is rejected by the server.
	for _, required := range []string{"id", "period", "action", "enabled", "created_at", "updated_at"} {
		if _, ok := budget[required]; !ok {
			t.Errorf("budget body is missing required field %q", required)
		}
	}
}

func TestSovereignDeleteUsesOpaqueID(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)
	if err := NewSovereign(srv.URL, "yb_admin").DeleteKey(context.Background(), "key-uuid-1"); err != nil {
		t.Fatal(err)
	}
	if seen["deleted"] != "key-uuid-1" {
		t.Errorf("deleted = %v", seen["deleted"])
	}
}

func TestSovereignUsageConvertsMicrosToDollars(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)
	rows, err := NewSovereign(srv.URL, "yb_admin").Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Spend != 2.25 || rows[0].Requests != 7 {
		t.Errorf("usage = %+v, want 2.25 USD / 7 requests", rows)
	}
}

// ---- the two gaps, which must fail loudly -------------------------------

func TestSovereignRejectsKeyExpiry(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)
	_, err := NewSovereign(srv.URL, "yb_admin").GenerateKey(context.Background(), KeyRequest{Duration: "30d"})
	if !errors.Is(err, ErrNoKeyExpiry) {
		t.Fatalf("err = %v, want ErrNoKeyExpiry", err)
	}
	// It must not have issued an unexpiring key behind the operator's back.
	if _, ok := seen["create"]; ok {
		t.Error("a key was issued despite the unsupported expiry")
	}
}

func TestSovereignRejectsRateLimitUpdate(t *testing.T) {
	seen := map[string]any{}
	srv := sovereignFake(t, &seen)
	c := NewSovereign(srv.URL, "yb_admin")

	err := c.UpdateBudget(context.Background(), "key-uuid-1", BudgetUpdate{RPMLimit: ptrI(10)})
	if !errors.Is(err, ErrNoKeyLimitUpdate) {
		t.Fatalf("err = %v, want ErrNoKeyLimitUpdate", err)
	}
	// A spend cap alone is supported and must still work.
	if err := c.UpdateBudget(context.Background(), "key-uuid-1", BudgetUpdate{MaxBudget: ptrF(1)}); err != nil {
		t.Fatalf("spend-cap-only update failed: %v", err)
	}
	if seen["budget"] == nil {
		t.Error("spend cap was not written")
	}
}

// ---- money --------------------------------------------------------------

func TestUSDMicrosRoundTrip(t *testing.T) {
	for _, usd := range []float64{0, 0.01, 0.1, 1, 12.5, 99.99, 1000.005} {
		if got := microsToUSD(usdToMicros(usd)); got != usd {
			t.Errorf("round trip %v -> %d -> %v", usd, usdToMicros(usd), got)
		}
	}
	// Rounding must be explicit, not truncation: 0.07 in binary float is
	// 0.070000000000000007, and 0.07*1e6 truncates to 69999.
	if got := usdToMicros(0.07); got != 70_000 {
		t.Errorf("usdToMicros(0.07) = %d, want 70000 (truncation bug)", got)
	}
	if got := usdToMicros(-1.5); got != -1_500_000 {
		t.Errorf("usdToMicros(-1.5) = %d", got)
	}
}
