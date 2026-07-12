// Package gateway is Sovereign Control's client for LiteLLM management
// (design.md §2.2). The LiteLLM UI is never exposed; this client and
// generated configuration are the only management paths.
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

type Client struct {
	base      string
	masterKey string
	http      *http.Client
}

func New(baseURL, masterKey string) *Client {
	return &Client{base: baseURL, masterKey: masterKey, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
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

// Healthy probes LiteLLM's liveliness endpoint.
func (c *Client) Healthy(ctx context.Context) bool {
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

type Model struct {
	ID string `json:"id"`
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []Model `json:"data"`
	}
	err := c.get(ctx, "/v1/models", &out)
	return out.Data, err
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
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

type KeyRequest struct {
	Alias     string   `json:"key_alias,omitempty"`
	Models    []string `json:"models,omitempty"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
}

// GenerateKey creates a scoped virtual key via LiteLLM's management API.
func (c *Client) GenerateKey(ctx context.Context, request KeyRequest) (map[string]any, error) {
	var out map[string]any
	err := c.post(ctx, "/key/generate", request, &out)
	return out, err
}

// ListKeys returns LiteLLM's key info list (management API).
func (c *Client) ListKeys(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.get(ctx, "/key/list", &out)
	return out, err
}
