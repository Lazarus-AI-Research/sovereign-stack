package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
)

// ActivateDeps executes the "profile-activate" job: point the runtime's
// embedding role at a profile and restart. The §11.2 index-rebuild
// requirement is enforced at the API layer: activation responses always
// carry the rebuild warning.
type ActivateDeps struct {
	Registry     *Registry
	ConfigPath   string
	Proxy        *dockerproxy.Client
	Runtime      *runtime.Client
	ReadyTimeout time.Duration
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
	if err := d.rewriteEmbeddingRole(profile); err != nil {
		return nil, err
	}
	if err := d.Proxy.Restart(ctx, "sovereign-runtime"); err != nil {
		return nil, fmt.Errorf("runtime restart: %w", err)
	}

	timeout := d.ReadyTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	var last runtime.Readiness
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		ready, err := d.Runtime.Ready(ctx)
		if err != nil {
			continue
		}
		last = ready
		if ready.Ready {
			// §10.1: report the runtime's discovered dimensions.
			dimensions := 0
			if manifest, err := d.Runtime.Manifest(ctx); err == nil {
				if roles, ok := manifest["roles"].(map[string]any); ok {
					if embedding, ok := roles["embedding"].(map[string]any); ok {
						if dims, ok := embedding["dimensions"].(float64); ok {
							dimensions = int(dims)
						}
					}
				}
			}
			return map[string]any{
				"state":      ready.State,
				"profile":    request.ProfileID,
				"dimensions": dimensions,
			}, nil
		}
		if ready.State == "configuration_error" || ready.State == "runtime_error" || ready.State == "degraded" {
			break
		}
	}
	return map[string]any{"state": last.State, "profile": request.ProfileID},
		fmt.Errorf("runtime not ready after profile activation: state=%s", last.State)
}

func (d ActivateDeps) rewriteEmbeddingRole(profile Profile) error {
	raw, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}
	roles, _ := config["roles"].(map[string]any)
	if roles == nil {
		return fmt.Errorf("runtime config has no roles section")
	}
	role, _ := roles["embedding"].(map[string]any)
	if role == nil {
		role = map[string]any{}
	}
	role["enabled"] = true
	role["task"] = "embed"
	role["source"] = profile.Source
	role["model"] = profile.Model
	role["served_model_name"] = profile.ServedModelName
	if profile.Revision != "" {
		role["revision"] = profile.Revision
	}
	if profile.Pooling != "" {
		role["pooling"] = profile.Pooling
	}
	if profile.Normalization != "" {
		role["normalization"] = profile.Normalization
	}
	roles["embedding"] = role
	config["roles"] = roles

	updated, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	header := "# Sovereign Runtime configuration — managed by Sovereign Control.\n# Validated against schemas/runtime-config.schema.json.\n"
	return os.WriteFile(d.ConfigPath, append([]byte(header), updated...), 0o644)
}
