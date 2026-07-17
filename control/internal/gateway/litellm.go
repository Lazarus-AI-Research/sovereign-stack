package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LiteLLM drives LiteLLM's management API (design.md §2.2). The LiteLLM UI is
// never exposed; this client and generated configuration are the only
// management paths.
//
// Only LiteLLM's open-source management surface is used;
// docs/litellm-licensing-inventory.md tracks which capabilities are
// enterprise-gated, and nothing here may depend on one.
type LiteLLM struct {
	base      string
	masterKey string
	http      *http.Client
}

var _ Provider = (*LiteLLM)(nil)

// New constructs the LiteLLM provider.
func New(baseURL, masterKey string) *LiteLLM {
	return &LiteLLM{base: baseURL, masterKey: masterKey, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *LiteLLM) Name() string { return "litellm" }

func (c *LiteLLM) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.masterKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gateway %s: %s: %s", path, resp.Status, payload)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *LiteLLM) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.masterKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gateway %s: %s: %s", path, resp.Status, raw)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Healthy probes LiteLLM's liveliness endpoint.
func (c *LiteLLM) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health/liveliness", nil)
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

func (c *LiteLLM) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []Model `json:"data"`
	}
	err := c.get(ctx, "/v1/models", &out)
	return out.Data, err
}

// litellmKey is LiteLLM's key-info row. LiteLLM addresses a key by its token,
// so the token becomes KeyInfo.ID.
type litellmKey struct {
	Token     string   `json:"token"`
	KeyName   string   `json:"key_name"`
	KeyAlias  string   `json:"key_alias"`
	Models    []string `json:"models"`
	MaxBudget *float64 `json:"max_budget"`
	Spend     *float64 `json:"spend"`
	TPMLimit  *int64   `json:"tpm_limit"`
	RPMLimit  *int64   `json:"rpm_limit"`
	CreatedAt string   `json:"created_at"`
}

func (k litellmKey) normalize() KeyInfo {
	id := k.Token
	if id == "" {
		id = k.KeyName
	}
	return KeyInfo{
		ID:        id,
		Alias:     k.KeyAlias,
		Models:    k.Models,
		MaxBudget: k.MaxBudget,
		Spend:     k.Spend,
		TPMLimit:  k.TPMLimit,
		RPMLimit:  k.RPMLimit,
		CreatedAt: k.CreatedAt,
	}
}

func (c *LiteLLM) ListKeys(ctx context.Context) ([]KeyInfo, error) {
	var out struct {
		Keys []litellmKey `json:"keys"`
	}
	if err := c.get(ctx, "/key/list", &out); err != nil {
		return nil, err
	}
	keys := make([]KeyInfo, 0, len(out.Keys))
	for _, k := range out.Keys {
		keys = append(keys, k.normalize())
	}
	return keys, nil
}

func (c *LiteLLM) GenerateKey(ctx context.Context, req KeyRequest) (IssuedKey, error) {
	var out struct {
		Key string `json:"key"`
		litellmKey
	}
	if err := c.post(ctx, "/key/generate", req, &out); err != nil {
		return IssuedKey{}, err
	}
	info := out.litellmKey.normalize()
	// /key/generate returns the token as "key"; it is also the delete handle.
	if info.ID == "" {
		info.ID = out.Key
	}
	if info.Alias == "" {
		info.Alias = req.Alias
	}
	return IssuedKey{KeyInfo: info, Token: out.Key}, nil
}

func (c *LiteLLM) DeleteKey(ctx context.Context, id string) error {
	return c.post(ctx, "/key/delete", map[string]any{"keys": []string{id}}, nil)
}

// UpdateBudget updates the basic open-source virtual-key quota fields.
func (c *LiteLLM) UpdateBudget(ctx context.Context, id string, update BudgetUpdate) error {
	payload := map[string]any{"key": id}
	raw, err := json.Marshal(update)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	return c.post(ctx, "/key/update", payload, nil)
}

// Usage returns LiteLLM's aggregate spend view. Local models normally report
// zero monetary cost while token/request counts remain available in traces.
func (c *LiteLLM) Usage(ctx context.Context) ([]UsageRow, error) {
	var out []struct {
		Key      string  `json:"api_key"`
		Spend    float64 `json:"spend"`
		Requests int64   `json:"api_requests"`
	}
	if err := c.get(ctx, "/global/spend", &out); err != nil {
		return nil, err
	}
	rows := make([]UsageRow, 0, len(out))
	for _, r := range out {
		rows = append(rows, UsageRow{Subject: r.Key, Spend: r.Spend, Requests: r.Requests})
	}
	return rows, nil
}
