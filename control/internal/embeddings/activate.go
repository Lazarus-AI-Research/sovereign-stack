package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
)

// ActivateDeps executes the "profile-activate" job. embeddinggemma is a
// model-specialized service, so activation validates that the requested
// profile describes that model and probes the already-running service. Prefix
// and index identity changes still flow through the normal rebuild process.
type ActivateDeps struct {
	Registry  *Registry
	Service   *Client // retained for original callers
	Providers map[string]Prober
}

type ActivatePayload struct {
	ProfileID      string `json:"profile_id"`
	InitializeOnly bool   `json:"initialize_only,omitempty"`
}

func (d ActivateDeps) HandleActivate(ctx context.Context, payload json.RawMessage) (any, error) {
	var request ActivatePayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("bad payload: %w", err)
	}
	profile, err := d.Registry.Get(request.ProfileID)
	if err != nil {
		return nil, err
	}
	provider := d.Providers[profile.Provider]
	if provider == nil && profile.Provider == "embeddinggemma" {
		provider = d.Service
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider %q is unavailable", profile.Provider)
	}
	dimensions, err := provider.Probe(ctx, profile.ServedModelName)
	if err != nil {
		return map[string]any{"state": "unhealthy", "profile": request.ProfileID}, err
	}
	return map[string]any{
		"state":      "healthy",
		"profile":    request.ProfileID,
		"dimensions": dimensions,
	}, nil
}
