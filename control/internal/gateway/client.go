// Package gateway is Sovereign Control's client for LiteLLM management
// (design.md §2.2). The LiteLLM UI is never exposed; this client and
// generated configuration are the only management paths.
package gateway

import (
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
