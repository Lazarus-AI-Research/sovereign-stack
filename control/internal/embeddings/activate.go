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
	Registry *Registry
	Service  *Client
}

type ActivatePayload struct {
	ProfileID string `json:"profile_id"`
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
	if profile.Provider != "embeddinggemma" || profile.Model != EmbeddingGemmaModel {
		return nil, fmt.Errorf("profile %q is not compatible with the embeddinggemma backend", request.ProfileID)
	}
	if d.Service == nil {
		return nil, fmt.Errorf("embedding service is unavailable")
	}
	dimensions, err := d.Service.Probe(ctx, profile.ServedModelName)
	if err != nil {
		return map[string]any{"state": "unhealthy", "profile": request.ProfileID}, err
	}
	return map[string]any{
		"state":      "healthy",
		"profile":    request.ProfileID,
		"dimensions": dimensions,
	}, nil
}
