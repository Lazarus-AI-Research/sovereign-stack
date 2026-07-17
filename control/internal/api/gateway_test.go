package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
)

// stubGateway is a gateway.Provider that returns whatever a test needs. It
// exists only because §18.8 is now written against an interface: before, these
// paths could only be exercised against a real LiteLLM.
type stubGateway struct {
	name        string
	healthy     bool
	keys        []gateway.KeyInfo
	issueErr    error
	budgetErr   error
	needsReload bool
	configErr   error
	configured  int
}

func (s *stubGateway) GenerateConfig(context.Context, string, *models.Registry, *embeddings.Registry, gateway.SecretResolver) error {
	s.configured++
	return s.configErr
}
func (s *stubGateway) NeedsReload() bool { return s.needsReload }

func (s *stubGateway) Name() string                 { return s.name }
func (s *stubGateway) Healthy(context.Context) bool { return s.healthy }
func (s *stubGateway) Models(context.Context) ([]gateway.Model, error) {
	return []gateway.Model{{ID: "assistant-large"}}, nil
}
func (s *stubGateway) ListKeys(context.Context) ([]gateway.KeyInfo, error) { return s.keys, nil }
func (s *stubGateway) GenerateKey(context.Context, gateway.KeyRequest) (gateway.IssuedKey, error) {
	if s.issueErr != nil {
		return gateway.IssuedKey{}, s.issueErr
	}
	return gateway.IssuedKey{KeyInfo: gateway.KeyInfo{ID: "k1"}, Token: "tok"}, nil
}
func (s *stubGateway) DeleteKey(context.Context, string) error { return nil }
func (s *stubGateway) UpdateBudget(context.Context, string, gateway.BudgetUpdate) error {
	return s.budgetErr
}
func (s *stubGateway) Usage(context.Context) ([]gateway.UsageRow, error) {
	return []gateway.UsageRow{{Subject: "k1", Spend: 1.5, Requests: 3}}, nil
}

func controlWith(g gateway.Provider) http.Handler {
	return (&Server{Gateway: g, Version: "test"}).Handler()
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

// The §18.8 response shape must not depend on which gateway is installed.
func TestGatewayStatusReportsProvider(t *testing.T) {
	for _, name := range []string{"litellm", "sovereign"} {
		rec := do(controlWith(&stubGateway{name: name, healthy: true}), "GET", BasePath+"/gateway/status", "")
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		if body["healthy"] != true || body["provider"] != name {
			t.Errorf("%s: body = %v", name, body)
		}
	}
}

func TestGatewayKeysNormalizedEnvelope(t *testing.T) {
	g := &stubGateway{name: "sovereign", keys: []gateway.KeyInfo{{ID: "k1", Alias: "web"}}}
	rec := do(controlWith(g), "GET", BasePath+"/gateway/keys", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Keys []gateway.KeyInfo `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Keys) != 1 || body.Keys[0].ID != "k1" || body.Keys[0].Alias != "web" {
		t.Errorf("keys = %+v", body.Keys)
	}
}

// An operation the configured gateway cannot express is a 422 (the request is
// unsupported), not a 502 (the gateway is broken). The distinction matters:
// 502 says retry, 422 says this will never work.
func TestUnsupportedKeyExpiryIs422(t *testing.T) {
	g := &stubGateway{name: "sovereign", issueErr: gateway.ErrNoKeyExpiry}
	rec := do(controlWith(g), "POST", BasePath+"/gateway/keys", `{"duration":"30d"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "expiry") {
		t.Errorf("error should say why: %s", rec.Body.String())
	}
}

func TestUnsupportedRateLimitUpdateIs422(t *testing.T) {
	g := &stubGateway{name: "sovereign", budgetErr: gateway.ErrNoKeyLimitUpdate}
	rec := do(controlWith(g), "PATCH", BasePath+"/gateway/budgets/k1", `{"rpm_limit":10}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

// A gateway that is merely unreachable is still a 502.
func TestGatewayFailureIs502(t *testing.T) {
	g := &stubGateway{name: "litellm", issueErr: context.DeadlineExceeded}
	rec := do(controlWith(g), "POST", BasePath+"/gateway/keys", `{}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestGatewayUsageEnvelope(t *testing.T) {
	rec := do(controlWith(&stubGateway{name: "sovereign"}), "GET", BasePath+"/gateway/usage", "")
	var body struct {
		Usage []gateway.UsageRow `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Usage) != 1 || body.Usage[0].Spend != 1.5 {
		t.Errorf("usage = %+v", body.Usage)
	}
}
