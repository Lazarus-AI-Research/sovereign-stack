package embeddings

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

const EmbeddingGemmaModel = "ggml-org/embeddinggemma-300M-qat-q4_0-GGUF"

// Client probes the dedicated embeddinggemma service. The configured URL is
// also consumed by LiteLLM and normally ends in /v1; health lives at the
// service root, so retain both forms explicitly.
type Client struct {
	root string
	http *http.Client
}

func NewClient(baseURL string) *Client {
	root := strings.TrimRight(baseURL, "/")
	root = strings.TrimSuffix(root, "/v1")
	return &Client{root: root, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.root+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Probe performs real inference and returns the observed vector dimensions.
// This keeps dimensions discovered from loaded weights rather than guessed
// from profile names or model metadata.
func (c *Client) Probe(ctx context.Context, servedModelName string) (int, error) {
	payload, err := json.Marshal(map[string]any{
		"model": servedModelName,
		"input": "task: search result | query: sovereign readiness probe",
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.root+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("embedding service probe: %s: %s", resp.Status, raw)
	}
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("embedding service probe response: %w", err)
	}
	if len(result.Data) != 1 || len(result.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("embedding service probe returned no vector")
	}
	return len(result.Data[0].Embedding), nil
}
