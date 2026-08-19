package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeIssuedKeyDoesNotExposeImplementationResponse(t *testing.T) {
	budget := 12.5
	issued, err := NormalizeIssuedKey(map[string]any{
		"key": "sk-secret", "key_name": "hashed-id", "user_id": "internal-user",
		"metadata": map[string]any{"private": true},
	}, KeyRequest{Alias: "recursor", Models: []string{"assistant-large"}, MaxBudget: &budget}, "https://ai.example/api/openai/v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(issued)
	text := string(raw)
	for _, forbidden := range []string{"internal-user", "metadata", "private"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("normalized response leaked %q: %s", forbidden, text)
		}
	}
	if issued.Secret != "sk-secret" || issued.ID != "hashed-id" || issued.Alias != "recursor" || issued.BaseURL == "" {
		t.Fatalf("issued key = %+v", issued)
	}
}

func TestNormalizeKeyListReturnsRevocableMetadataWithoutSecrets(t *testing.T) {
	result := NormalizeKeyList(map[string]any{"keys": []any{
		map[string]any{"token": "hashed-id", "key_alias": "recursor", "models": []any{"assistant-large"}, "key": "must-not-leak"},
	}}, "http://127.0.0.1:54854/api/openai/v1")
	if len(result.Keys) != 1 || result.Keys[0].ID != "hashed-id" || result.Keys[0].Alias != "recursor" {
		t.Fatalf("key list = %+v", result)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "must-not-leak") {
		t.Fatalf("key list leaked a secret: %s", raw)
	}
}

func TestValidateKeyRequestRequiresAnExactModelScope(t *testing.T) {
	request, err := ValidateKeyRequest(KeyRequest{Alias: " recursor ", Models: []string{" assistant-large "}})
	if err != nil {
		t.Fatal(err)
	}
	if request.Alias != "recursor" || len(request.Models) != 1 || request.Models[0] != "assistant-large" {
		t.Fatalf("validated request = %+v", request)
	}
	for _, invalid := range []KeyRequest{
		{Alias: "", Models: []string{"assistant-large"}},
		{Alias: "recursor"},
		{Alias: "recursor", Models: []string{"*"}},
		{Alias: "recursor", Models: []string{""}},
	} {
		if _, err := ValidateKeyRequest(invalid); err == nil {
			t.Fatalf("accepted unscoped request: %+v", invalid)
		}
	}
}
