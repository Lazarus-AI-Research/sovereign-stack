// Package hostagent is the narrow Control client for the signed Metal host
// inference agent. Only the constrained embedding role API is exposed here;
// arbitrary processes, paths, and engine flags never cross this boundary.
package hostagent

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
	base  string
	token string
	http  *http.Client
}

type EmbeddingRoleRequest struct {
	Artifact      string `json:"artifact"`
	Revision      string `json:"revision"`
	SHA256        string `json:"sha256"`
	Pooling       string `json:"pooling"`
	Normalization string `json:"normalization"`
	ContextLength int    `json:"context_length"`
}

func New(baseURL, token string) *Client {
	return &Client{base: baseURL, token: token, http: &http.Client{Timeout: 3 * time.Minute}}
}

func (c *Client) do(ctx context.Context, method, path string, body any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
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
		return fmt.Errorf("host agent %s %s: %s: %s", method, path, resp.Status, raw)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) ConfigureEmbedding(ctx context.Context, request EmbeddingRoleRequest) error {
	if c.token == "" {
		return fmt.Errorf("host agent token is not configured")
	}
	return c.do(ctx, http.MethodPut, "/agent/admin/roles/embedding", request)
}

func (c *Client) DisableEmbedding(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("host agent token is not configured")
	}
	return c.do(ctx, http.MethodDelete, "/agent/admin/roles/embedding", nil)
}
