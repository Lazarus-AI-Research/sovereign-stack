// Package runtime is Sovereign Control's client for the runtime contract
// (docs/runtime-contract.md). It never talks to model endpoints — only the
// health/manifest/errors surface.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("runtime %s: %w", path, err)
	}
	return resp.StatusCode, nil
}

type Liveness struct {
	Status string `json:"status"`
	State  string `json:"state"`
}

type Readiness struct {
	Ready         bool            `json:"ready"`
	State         string          `json:"state"`
	RequiredRoles map[string]bool `json:"required_roles"`
}

func (c *Client) Live(ctx context.Context) (Liveness, error) {
	var out Liveness
	_, err := c.get(ctx, "/health/live", &out)
	return out, err
}

func (c *Client) Ready(ctx context.Context) (Readiness, error) {
	var out Readiness
	_, err := c.get(ctx, "/health/ready", &out)
	return out, err
}

// Health, Manifest, and Errors are passed through as raw JSON: the JSON
// Schemas in schemas/ are the contract, and Control should not re-model them.
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	_, err := c.get(ctx, "/health", &out)
	return out, err
}

func (c *Client) Manifest(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	status, err := c.get(ctx, "/runtime/manifest", &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("runtime manifest: status %d", status)
	}
	return out, nil
}

func (c *Client) Errors(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	_, err := c.get(ctx, "/runtime/errors", &out)
	return out, err
}
