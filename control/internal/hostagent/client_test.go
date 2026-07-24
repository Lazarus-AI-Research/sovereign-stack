package hostagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigureEmbeddingUsesConstrainedAuthenticatedAPI(t *testing.T) {
	var got EmbeddingRoleRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/agent/admin/roles/embedding" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			t.Errorf("authorization header was not set")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	request := EmbeddingRoleRequest{
		Artifact: "metal/research.gguf", Revision: "0123456789012345678901234567890123456789",
		SHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Pooling: "mean", Normalization: "l2", ContextLength: 2048,
	}
	if err := New(server.URL, "owner-token").ConfigureEmbedding(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got != request {
		t.Errorf("request body = %+v, want %+v", got, request)
	}
}

func TestDisableEmbeddingPropagatesAgentFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"rollback"}`))
	}))
	defer server.Close()
	if err := New(server.URL, "owner-token").DisableEmbedding(context.Background()); err == nil {
		t.Fatal("agent failure was ignored")
	}
}
