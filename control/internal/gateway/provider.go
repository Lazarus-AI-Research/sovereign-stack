package gateway

import (
	"context"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
)

// Provider is a Sovereign Gateway implementation.
//
// design.md §2.1 establishes interchangeable workspace providers; the gateway is
// the same shape. §2.6's rule carries over too: which implementation is
// installed must not leak above this boundary. Every method therefore returns
// Sovereign-owned types rather than a vendor's JSON, so Control's §18.8 surface
// keeps one shape whichever gateway is configured.
//
// Two implementations exist: LiteLLM (default) and SovereignGateway.
type Provider interface {
	// Name identifies the implementation, for diagnostics and /gateway/status.
	Name() string

	// Healthy reports whether the gateway is reachable. It never returns an
	// error: unreachable is a state, not a failure (§3.3).
	Healthy(ctx context.Context) bool

	// Models lists the model names the gateway will route.
	Models(ctx context.Context) ([]Model, error)

	// ListKeys returns virtual-key metadata. The token itself is never
	// returned by any gateway — only at issue time.
	ListKeys(ctx context.Context) ([]KeyInfo, error)

	// GenerateKey issues a virtual key. The token in the result is shown once
	// and cannot be retrieved again.
	GenerateKey(ctx context.Context, req KeyRequest) (IssuedKey, error)

	// DeleteKey revokes a key by its Handle (see KeyInfo.ID).
	DeleteKey(ctx context.Context, id string) error

	// UpdateBudget changes a key's quota.
	UpdateBudget(ctx context.Context, id string, update BudgetUpdate) error

	// Usage returns spend accounting. Local models normally report zero
	// monetary cost while token and request counts remain populated.
	Usage(ctx context.Context) ([]UsageRow, error)

	// GenerateConfig renders the gateway's configuration from the product's
	// registries (§2.2: generated configuration; the gateway UI is never
	// exposed). path is the config file Control owns and the gateway reads.
	//
	// What this costs differs by provider and the caller must not assume:
	// LiteLLM reads its model list from the file, so a change needs a reload;
	// SovereignGateway keeps models in its database and takes them over the
	// admin API, so they hot-reload and only process settings live in the file.
	// Reload accordingly — see NeedsReload.
	GenerateConfig(ctx context.Context, path string, models *models.Registry, profiles *embeddings.Registry, secrets SecretResolver) error

	// NeedsReload reports whether GenerateConfig's result requires restarting
	// the gateway to take effect.
	NeedsReload() bool
}

// Model is a routable model name as the gateway reports it.
type Model struct {
	ID string `json:"id"`
}

// KeyRequest asks for a virtual key.
//
// The JSON tags are Control's existing §18.8 wire contract and are deliberately
// unchanged; each Provider maps them onto its own API.
type KeyRequest struct {
	Alias  string   `json:"key_alias,omitempty"`
	Models []string `json:"models,omitempty"`
	// MaxBudget is US dollars. See the note on KeyInfo.MaxBudget.
	MaxBudget *float64 `json:"max_budget,omitempty"`
	TPMLimit  *int64   `json:"tpm_limit,omitempty"`
	RPMLimit  *int64   `json:"rpm_limit,omitempty"`
	// Duration is a key lifetime such as "30d".
	//
	// Only LiteLLM implements expiry. SovereignGateway has no concept of it —
	// its keys live until revoked — and rejects this field rather than
	// accepting it and silently issuing a key that never expires.
	Duration string `json:"duration,omitempty"`
}

// KeyInfo is virtual-key metadata, normalized across providers.
type KeyInfo struct {
	// ID is the handle DeleteKey and UpdateBudget take. It is whatever the
	// provider identifies a key by, and is not portable between providers:
	// LiteLLM addresses keys by their token, SovereignGateway by an opaque id.
	ID     string   `json:"id"`
	Alias  string   `json:"alias,omitempty"`
	Models []string `json:"models,omitempty"`
	// MaxBudget and Spend are US dollars.
	//
	// Dollars, not micros, because this is Control's established wire shape and
	// what the UI renders. SovereignGateway is micros-native (i64, 1 USD =
	// 1_000_000) and converts at its adapter boundary — the impedance mismatch
	// belongs in the adapter, not in the product's API.
	MaxBudget *float64 `json:"max_budget,omitempty"`
	Spend     *float64 `json:"spend,omitempty"`
	TPMLimit  *int64   `json:"tpm_limit,omitempty"`
	RPMLimit  *int64   `json:"rpm_limit,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// IssuedKey is a freshly created key plus its one-time token.
type IssuedKey struct {
	KeyInfo
	// Token is the bearer credential. Returned once, at creation, and never
	// retrievable again.
	Token string `json:"token"`
}

// BudgetUpdate changes a key's quota. Nil fields are left alone.
type BudgetUpdate struct {
	// MaxBudget is US dollars.
	MaxBudget *float64 `json:"max_budget,omitempty"`
	TPMLimit  *int64   `json:"tpm_limit,omitempty"`
	RPMLimit  *int64   `json:"rpm_limit,omitempty"`
}

// UsageRow is one spend accounting row.
type UsageRow struct {
	// Subject is what the spend is attributed to — a key handle, user, or team,
	// depending on what the gateway tracks.
	Subject string `json:"subject"`
	// Spend is US dollars.
	Spend    float64 `json:"spend"`
	Requests int64   `json:"requests,omitempty"`
}

// usdToMicros converts dollars to the integer micros SovereignGateway stores
// (1 USD = 1_000_000). Rounding is explicit: money must not inherit float
// truncation.
func usdToMicros(usd float64) int64 {
	if usd < 0 {
		return int64(usd*1e6 - 0.5)
	}
	return int64(usd*1e6 + 0.5)
}

// microsToUSD converts SovereignGateway's integer micros back to dollars.
func microsToUSD(micros int64) float64 {
	return float64(micros) / 1e6
}
