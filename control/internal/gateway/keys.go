package gateway

import (
	"fmt"
	"strings"
)

// IssuedKey is the deliberately small one-time response exposed by Control.
// LiteLLM's implementation response contains internal identifiers and multiple
// key-like fields; none of those are passed through accidentally.
type IssuedKey struct {
	Secret    string   `json:"secret"`
	ID        string   `json:"id,omitempty"`
	Alias     string   `json:"alias"`
	Models    []string `json:"models"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
	TPMLimit  *int64   `json:"tpm_limit,omitempty"`
	RPMLimit  *int64   `json:"rpm_limit,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	BaseURL   string   `json:"base_url"`
}

type KeyMetadata struct {
	ID        string   `json:"id"`
	Alias     string   `json:"alias"`
	Models    []string `json:"models"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
	Spend     *float64 `json:"spend,omitempty"`
	TPMLimit  *int64   `json:"tpm_limit,omitempty"`
	RPMLimit  *int64   `json:"rpm_limit,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type KeyList struct {
	Keys    []KeyMetadata `json:"keys"`
	BaseURL string        `json:"base_url"`
}

// ValidateKeyRequest makes "scoped" an enforced property rather than a UI
// convention. Public customer keys always name at least one exact model and
// cannot silently become unrestricted through an empty or wildcard allowlist.
func ValidateKeyRequest(request KeyRequest) (KeyRequest, error) {
	request.Alias = strings.TrimSpace(request.Alias)
	if request.Alias == "" {
		return KeyRequest{}, fmt.Errorf("key alias is required")
	}
	if len(request.Models) == 0 {
		return KeyRequest{}, fmt.Errorf("at least one allowed model is required")
	}
	models := make([]string, 0, len(request.Models))
	for _, model := range request.Models {
		model = strings.TrimSpace(model)
		if model == "" || model == "*" {
			return KeyRequest{}, fmt.Errorf("allowed models must use exact nonempty aliases")
		}
		models = append(models, model)
	}
	request.Models = models
	if request.MaxBudget != nil && *request.MaxBudget < 0 {
		return KeyRequest{}, fmt.Errorf("max budget cannot be negative")
	}
	if request.TPMLimit != nil && *request.TPMLimit <= 0 {
		return KeyRequest{}, fmt.Errorf("TPM limit must be positive")
	}
	if request.RPMLimit != nil && *request.RPMLimit <= 0 {
		return KeyRequest{}, fmt.Errorf("RPM limit must be positive")
	}
	return request, nil
}

func stringValue(values map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := values[name].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func stringsValue(values map[string]any, name string) []string {
	raw, ok := values[name].([]any)
	if !ok {
		if typed, ok := values[name].([]string); ok {
			return typed
		}
		return []string{}
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			result = append(result, value)
		}
	}
	return result
}

func floatValue(values map[string]any, name string) *float64 {
	if value, ok := values[name].(float64); ok {
		return &value
	}
	return nil
}

func intValue(values map[string]any, name string) *int64 {
	if value, ok := values[name].(float64); ok {
		result := int64(value)
		return &result
	}
	return nil
}

// NormalizeIssuedKey allowlists the secret and useful policy metadata. The
// request is the authoritative fallback because LiteLLM versions vary in how
// much of the submitted policy they echo.
func NormalizeIssuedKey(raw map[string]any, request KeyRequest, baseURL string) (IssuedKey, error) {
	secret := stringValue(raw, "key")
	if secret == "" {
		return IssuedKey{}, fmt.Errorf("gateway returned no one-time key secret")
	}
	models := stringsValue(raw, "models")
	if len(models) == 0 {
		models = append([]string(nil), request.Models...)
	}
	result := IssuedKey{
		Secret: secret, ID: stringValue(raw, "key_name", "token_id", "id"),
		Alias: stringValue(raw, "key_alias"), Models: models,
		MaxBudget: floatValue(raw, "max_budget"), TPMLimit: intValue(raw, "tpm_limit"),
		RPMLimit: intValue(raw, "rpm_limit"), ExpiresAt: stringValue(raw, "expires", "expires_at"),
		BaseURL: baseURL,
	}
	if result.Alias == "" {
		result.Alias = request.Alias
	}
	if result.MaxBudget == nil {
		result.MaxBudget = request.MaxBudget
	}
	if result.TPMLimit == nil {
		result.TPMLimit = request.TPMLimit
	}
	if result.RPMLimit == nil {
		result.RPMLimit = request.RPMLimit
	}
	return result, nil
}

func NormalizeKeyList(raw map[string]any, baseURL string) KeyList {
	items, _ := raw["keys"].([]any)
	if len(items) == 0 {
		items, _ = raw["data"].([]any)
	}
	result := KeyList{Keys: []KeyMetadata{}, BaseURL: baseURL}
	for _, item := range items {
		value, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(value, "key_name", "token", "token_id", "id")
		if id == "" {
			continue
		}
		result.Keys = append(result.Keys, KeyMetadata{
			ID: id, Alias: stringValue(value, "key_alias", "alias"), Models: stringsValue(value, "models"),
			MaxBudget: floatValue(value, "max_budget"), Spend: floatValue(value, "spend"),
			TPMLimit: intValue(value, "tpm_limit"), RPMLimit: intValue(value, "rpm_limit"),
			CreatedAt: stringValue(value, "created_at"), ExpiresAt: stringValue(value, "expires", "expires_at"),
		})
	}
	return result
}
