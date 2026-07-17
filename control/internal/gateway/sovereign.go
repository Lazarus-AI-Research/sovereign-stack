package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Sovereign drives SovereignGateway's admin API
// (Lazarus-AI-Research/sovereign-gateway, docs/openapi.yaml).
//
// Machine auth is an admin-scoped virtual key sent as a bearer — the console
// session cookie is for humans, and the console itself is an optional build
// feature the appliance does not ship. Every route lives under /admin/v1.
type Sovereign struct {
	base     string
	adminKey string
	http     *http.Client
}

var _ Provider = (*Sovereign)(nil)

// NewSovereign constructs the SovereignGateway provider. adminKey is an
// admin-scoped `yb_…` virtual key.
func NewSovereign(baseURL, adminKey string) *Sovereign {
	return &Sovereign{base: baseURL, adminKey: adminKey, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Sovereign) Name() string { return "sovereign" }

// ErrNoKeyExpiry reports that SovereignGateway cannot express a key lifetime.
//
// It has no expires_at: keys live until revoked. Rejecting the request is
// deliberate — accepting `duration` and issuing a key that never expires would
// hand an operator a credential they believe is short-lived.
var ErrNoKeyExpiry = errors.New("sovereign-gateway does not support key expiry: keys live until revoked, so `duration` cannot be honoured")

// ErrNoKeyLimitUpdate reports that a key's rate limits cannot be changed after
// it is issued.
//
// The admin API sets rpm/tpm at creation only — there is no route to update
// them (the key routes are POST /keys, DELETE /keys/{id}, PUT /keys/{id}/access).
// Reissue the key to change its limits. A spend cap is separate and can be
// changed at any time; see UpdateBudget.
var ErrNoKeyLimitUpdate = errors.New("sovereign-gateway cannot change a key's rate limits after issue: rpm/tpm are set at creation only, so the key must be reissued")

func (c *Sovereign) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gateway %s %s: %s: %s", method, path, resp.Status, sovereignError(raw))
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// sovereignError unwraps the gateway's {"error":{"code","message"}} envelope so
// Control surfaces the message rather than the raw JSON.
func sovereignError(raw []byte) string {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Code != "" {
		return env.Error.Code + ": " + env.Error.Message
	}
	return string(raw)
}

// Healthy probes GET /health, which is 200 whenever the process is up.
func (c *Sovereign) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Models reads the OpenAI-shaped discovery list, the same shape LiteLLM serves.
func (c *Sovereign) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []Model `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/models", nil, &out)
	return out.Data, err
}

// sovereignKey mirrors the ApiKey schema in the gateway's openapi.yaml.
type sovereignKey struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access struct {
		AllowedModels []string `json:"allowed_models"`
	} `json:"access"`
	RPMLimit  *int64 `json:"rpm_limit"`
	TPMLimit  *int64 `json:"tpm_limit"`
	CreatedAt string `json:"created_at"`
}

func (k sovereignKey) normalize() KeyInfo {
	return KeyInfo{
		ID:        k.ID,
		Alias:     k.Name,
		Models:    k.Access.AllowedModels,
		TPMLimit:  k.TPMLimit,
		RPMLimit:  k.RPMLimit,
		CreatedAt: k.CreatedAt,
	}
}

func (c *Sovereign) ListKeys(ctx context.Context) ([]KeyInfo, error) {
	var out []sovereignKey
	if err := c.do(ctx, http.MethodGet, "/admin/v1/keys", nil, &out); err != nil {
		return nil, err
	}
	keys := make([]KeyInfo, 0, len(out))
	for _, k := range out {
		keys = append(keys, k.normalize())
	}
	return keys, nil
}

// GenerateKey issues a key, then attaches a budget if one was asked for.
//
// SovereignGateway splits these: a key carries rate limits, while spend caps are
// separate Budget rows keyed by subject. LiteLLM folds both into key creation,
// so this is two calls where LiteLLM makes one.
func (c *Sovereign) GenerateKey(ctx context.Context, req KeyRequest) (IssuedKey, error) {
	if req.Duration != "" {
		return IssuedKey{}, ErrNoKeyExpiry
	}

	body := map[string]any{"scopes": []string{"inference"}}
	if req.Alias != "" {
		body["name"] = req.Alias
	}
	if len(req.Models) > 0 {
		body["access"] = map[string]any{"allowed_models": req.Models}
	}
	if req.RPMLimit != nil {
		body["rpm_limit"] = *req.RPMLimit
	}
	if req.TPMLimit != nil {
		body["tpm_limit"] = *req.TPMLimit
	}

	var out struct {
		Key   sovereignKey `json:"key"`
		Token string       `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/admin/v1/keys", body, &out); err != nil {
		return IssuedKey{}, err
	}

	info := out.Key.normalize()
	if req.MaxBudget != nil {
		if err := c.putBudget(ctx, info.ID, *req.MaxBudget); err != nil {
			// The key exists but is uncapped. Say so rather than return a key
			// the caller believes is budgeted.
			return IssuedKey{}, fmt.Errorf("key %s was issued but its budget could not be set: %w", info.ID, err)
		}
		info.MaxBudget = req.MaxBudget
	}
	return IssuedKey{KeyInfo: info, Token: out.Token}, nil
}

func (c *Sovereign) DeleteKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/admin/v1/keys/"+url.PathEscape(id), nil, nil)
}

// UpdateBudget applies a spend cap.
//
// The two halves of a LiteLLM budget update land in different places here: the
// spend cap is a Budget row and is changeable, while rpm/tpm live on the key and
// are fixed at creation. Rather than silently drop the limits, reject the
// request — a quota an operator believes they tightened, but did not, is worse
// than an error.
func (c *Sovereign) UpdateBudget(ctx context.Context, id string, update BudgetUpdate) error {
	if update.RPMLimit != nil || update.TPMLimit != nil {
		return ErrNoKeyLimitUpdate
	}
	if update.MaxBudget == nil {
		return nil
	}
	return c.putBudget(ctx, id, *update.MaxBudget)
}

// putBudget upserts a total-period hard block on a key's spend.
//
// PUT /admin/v1/budgets takes the whole Budget, id and timestamps included — the
// caller supplies them, the server does not fill them in. The id is derived from
// the key so the upsert is idempotent: setting a budget twice replaces it rather
// than accumulating rows.
func (c *Sovereign) putBudget(ctx context.Context, keyID string, usd float64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := map[string]any{
		"id":                "key-" + keyID,
		"subject_type":      "key",
		"subject_id":        keyID,
		"period":            "total",
		"hard_limit_micros": usdToMicros(usd),
		"soft_limit_micros": nil,
		"action":            "block",
		"enabled":           true,
		"created_at":        now,
		"updated_at":        now,
		"deleted_at":        nil,
	}
	return c.do(ctx, http.MethodPut, "/admin/v1/budgets", body, nil)
}

// Usage reads the spend rollup, converting micros back to dollars.
func (c *Sovereign) Usage(ctx context.Context) ([]UsageRow, error) {
	var out []struct {
		SubjectType  string `json:"subject_type"`
		SubjectID    string `json:"subject_id"`
		SpendMicros  int64  `json:"spend_micros"`
		RequestCount int64  `json:"request_count"`
	}
	if err := c.do(ctx, http.MethodGet, "/admin/v1/spend", nil, &out); err != nil {
		return nil, err
	}
	rows := make([]UsageRow, 0, len(out))
	for _, r := range out {
		rows = append(rows, UsageRow{
			Subject:  r.SubjectID,
			Spend:    microsToUSD(r.SpendMicros),
			Requests: r.RequestCount,
		})
	}
	return rows, nil
}
