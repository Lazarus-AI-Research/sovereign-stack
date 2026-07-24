package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LifecycleClient struct {
	base  string
	token string
	http  *http.Client
}

type HostStatus struct {
	Managed      bool     `json:"managed"`
	AccessMode   string   `json:"access_mode"`
	PublicURL    string   `json:"public_url"`
	BindAddress  string   `json:"bind_address"`
	SiteAddress  string   `json:"site_address"`
	Version      string   `json:"version"`
	LANAddresses []string `json:"lan_addresses,omitempty"`
}

type UpdateState struct {
	State     string `json:"state"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

func NewLifecycle(baseURL, token string) *LifecycleClient {
	return &LifecycleClient{base: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 5 * time.Minute}}
}

func (c *LifecycleClient) request(ctx context.Context, method, path string, body any, result any) error {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("host lifecycle %s: %s", response.Status, raw)
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	return json.NewDecoder(response.Body).Decode(result)
}

func (c *LifecycleClient) Status(ctx context.Context) (HostStatus, error) {
	var result HostStatus
	if c.token == "" {
		return result, fmt.Errorf("host lifecycle token is not configured")
	}
	err := c.request(ctx, http.MethodGet, "/host/v1/status", nil, &result)
	return result, err
}

func (c *LifecycleClient) SetNetwork(ctx context.Context, mode, target string) error {
	return c.request(ctx, http.MethodPut, "/host/v1/network", map[string]string{"mode": mode, "target": target}, nil)
}

func (c *LifecycleClient) Repair(ctx context.Context) error {
	return c.request(ctx, http.MethodPost, "/host/v1/repair", map[string]string{}, nil)
}

func (c *LifecycleClient) UpdateStatus(ctx context.Context) (UpdateState, error) {
	var result UpdateState
	err := c.request(ctx, http.MethodGet, "/host/v1/updates", nil, &result)
	return result, err
}

func (c *LifecycleClient) ApplyUpdate(ctx context.Context, version string) error {
	return c.request(ctx, http.MethodPost, "/host/v1/updates", map[string]string{"version": version}, nil)
}
