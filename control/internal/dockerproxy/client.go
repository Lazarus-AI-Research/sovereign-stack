// Package dockerproxy is Sovereign Control's client for the restricted
// Docker proxy (design.md §18.13). Control never touches the Docker socket.
package dockerproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{base: baseURL, token: token, http: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
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
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker proxy %s %s: %s: %s", method, path, resp.Status, payload)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/internal/docker/status", nil, &out)
	return out, err
}

type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

func (c *Client) Containers(ctx context.Context) ([]ContainerSummary, error) {
	var out struct {
		Containers []ContainerSummary `json:"containers"`
	}
	err := c.do(ctx, http.MethodGet, "/internal/docker/containers", nil, &out)
	return out.Containers, err
}

func (c *Client) Restart(ctx context.Context, service string) error {
	return c.do(ctx, http.MethodPost, "/internal/docker/containers/"+service+"/restart", nil, nil)
}

func (c *Client) Logs(ctx context.Context, service string, tail int) (string, error) {
	var out struct {
		Logs string `json:"logs"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/internal/docker/containers/%s/logs?tail=%d", service, tail), nil, &out)
	return out.Logs, err
}

func (c *Client) Pull(ctx context.Context, image string) error {
	return c.do(ctx, http.MethodPost, "/internal/docker/images/pull", map[string]string{"image": image}, nil)
}

// ExportImages writes a Docker-compatible archive through the restricted
// proxy. The proxy independently validates every reference against its export
// allowlist.
func (c *Client) ExportImages(ctx context.Context, images []string, destination string) error {
	payload, err := json.Marshal(map[string]any{"images": images})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/docker/images/export", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	client := *c.http
	client.Timeout = 0 // multi-gigabyte image exports legitimately take minutes
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker proxy image export: %s: %s", resp.Status, raw)
	}
	temporary := destination + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, destination)
}

func (c *Client) RunEvals(ctx context.Context, image, suite string) (string, error) {
	var out struct {
		ContainerID string `json:"container_id"`
	}
	err := c.do(ctx, http.MethodPost, "/internal/docker/evals/run",
		map[string]string{"image": image, "suite": suite}, &out)
	return out.ContainerID, err
}

// RunBackup launches the fixed backup container (mode: dump or restore).
func (c *Client) RunBackup(ctx context.Context, mode, stamp string) (string, error) {
	var out struct {
		ContainerID string `json:"container_id"`
	}
	err := c.do(ctx, http.MethodPost, "/internal/docker/backup/run",
		map[string]string{"mode": mode, "stamp": stamp}, &out)
	return out.ContainerID, err
}
