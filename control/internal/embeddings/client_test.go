package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientHealthAndProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{0.5, 0.5, 0.5}}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL + "/v1")
	if !client.Healthy(context.Background()) {
		t.Fatal("healthy service reported unhealthy")
	}
	dimensions, err := client.Probe(context.Background(), "embedding-gemma-default")
	if err != nil || dimensions != 3 {
		t.Fatalf("probe = %d, %v", dimensions, err)
	}
}
