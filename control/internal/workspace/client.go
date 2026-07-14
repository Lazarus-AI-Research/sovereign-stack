// Package workspace is Sovereign Control's client for the workspace
// provider (AnythingLLM, §2.1/§18.9). Branding flows from the product's
// branding.yaml into the workspace so the customer never sees provider
// branding by default (§16).
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) Reachable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/ping", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < 500
}

// UploadLogo replaces the workspace's wordmark (multipart file upload).
func (c *Client) UploadLogo(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("branding logo: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("logo", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/system/upload-logo", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("workspace logo upload: %s: %s", resp.Status, payload)
	}
	return nil
}

// SetAppName sets both the workspace's displayed product name and the
// server-rendered browser title. AnythingLLM generates its production index
// page dynamically, so changing only custom_app_name leaves provider branding
// in the page metadata.
func (c *Client) SetAppName(ctx context.Context, name string) error {
	payload, _ := json.Marshal(map[string]string{
		"custom_app_name": name,
		"meta_page_title": name,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/admin/system-preferences", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("workspace app name: %s: %s", resp.Status, raw)
	}
	return nil
}

func (c *Client) AppName(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/system/custom-app-name", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		CustomAppName *string `json:"customAppName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.CustomAppName == nil {
		return "", nil
	}
	return *out.CustomAppName, nil
}
