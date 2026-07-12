package models

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

// LoadDeps executes the "model-load" job: point the runtime config's role at
// a registry model, restart the runtime via the restricted proxy, and wait
// for readiness. The runtime's own state machine reports the outcome —
// a bad model leaves it alive in configuration_error, never crash-looping.
type LoadDeps struct {
	Registry   *Registry
	ConfigPath string
	Proxy      *dockerproxy.Client
	Runtime    *runtime.Client
	// ReadyTimeout bounds the post-restart wait (model download + load).
	ReadyTimeout time.Duration
}

type LoadPayload struct {
	ModelID string `json:"model_id"`
}

func (d LoadDeps) HandleLoad(ctx context.Context, payload json.RawMessage) (any, error) {
	var request LoadPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("bad payload: %w", err)
	}
	entry, err := d.Registry.Get(request.ModelID)
	if err != nil {
		return nil, err
	}
	if err := d.rewriteRole(entry); err != nil {
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
			continue // restarting
		}
		last = ready
		if ready.Ready {
			return map[string]any{"state": ready.State, "model": entry.Model, "role": entry.Role}, nil
		}
		// terminal non-ready states end the wait early
		if ready.State == "configuration_error" || ready.State == "runtime_error" || ready.State == "degraded" {
			break
		}
	}
	return map[string]any{"state": last.State, "model": entry.Model, "role": entry.Role},
		fmt.Errorf("runtime not ready after load: state=%s (see /runtime/errors)", last.State)
}

// rewriteRole updates the active runtime config file. Comments are not
// preserved: the file is managed by Sovereign Control once installed.
func (d LoadDeps) rewriteRole(entry Entry) error {
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
	role, _ := roles[entry.Role].(map[string]any)
	if role == nil {
		role = map[string]any{}
	}
	role["enabled"] = true
	role["source"] = entry.Source
	role["model"] = entry.Model
	if entry.Revision != "" {
		role["revision"] = entry.Revision
	}
	if entry.Role == "generation" {
		role["task"] = "generate"
	} else if entry.Role == "embedding" {
		role["task"] = "embed"
	}
	roles[entry.Role] = role
	config["roles"] = roles

	updated, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	header := "# Sovereign Runtime configuration — managed by Sovereign Control.\n# Validated against schemas/runtime-config.schema.json.\n"
	return os.WriteFile(d.ConfigPath, append([]byte(header), updated...), 0o644)
}
