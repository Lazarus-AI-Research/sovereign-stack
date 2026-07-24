package models

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
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
	ModelID         string `json:"model_id"`
	ServedModelName string `json:"served_model_name,omitempty"`
	Pooling         string `json:"pooling,omitempty"`
	Normalization   string `json:"normalization,omitempty"`
}

func (d LoadDeps) HandleLoad(ctx context.Context, payload json.RawMessage) (any, error) {
	var request LoadPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("bad payload: %w", err)
	}
	return d.Load(ctx, request)
}

// Load applies a registry entry to one runtime role. If the candidate cannot
// become ready, both the managed configuration and the prior runtime are
// restored before the error is returned.
func (d LoadDeps) Load(ctx context.Context, request LoadPayload) (any, error) {
	total := int64(4)
	_ = jobs.Report(ctx, jobs.Progress{Stage: "resolving", Message: "Resolving model configuration", Current: 0, Total: &total, Unit: "steps"})
	entry, err := d.Registry.Get(request.ModelID)
	if err != nil {
		return nil, err
	}
	previous, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("runtime config: %w", err)
	}
	if err := d.rewriteRole(entry, request); err != nil {
		return nil, err
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "configuring", Message: "Applying model configuration", Current: 1, Total: &total, Unit: "steps"})
	if err := d.Proxy.Restart(ctx, "sovereign-runtime"); err != nil {
		if rollbackErr := d.rollback(previous); rollbackErr != nil {
			return nil, fmt.Errorf("runtime restart: %w; rollback: %v", err, rollbackErr)
		}
		return nil, fmt.Errorf("runtime restart: %w", err)
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "loading", Message: "Downloading and loading model", Current: 2, Total: &total, Unit: "steps"})
	result, err := d.waitReady(ctx, entry)
	if err == nil {
		_ = jobs.Report(ctx, jobs.Progress{Stage: "validating", Message: "Model is ready", Current: 4, Total: &total, Unit: "steps"})
		return result, nil
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "rolling_back", Message: "Restoring the previous working model", Current: 3, Total: &total, Unit: "steps"})
	if rollbackErr := d.rollback(previous); rollbackErr != nil {
		return result, fmt.Errorf("%w; rollback: %v", err, rollbackErr)
	}
	return result, err
}

// DisableRole removes an optional role from runtime resource allocation while
// retaining its last known configuration for a future re-enable.
func (d LoadDeps) DisableRole(ctx context.Context, name string) error {
	if name == "generation" {
		return fmt.Errorf("generation is required and cannot be disabled")
	}
	previous, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(previous, &config); err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}
	roles, _ := config["roles"].(map[string]any)
	if roles == nil {
		return fmt.Errorf("runtime config has no roles section")
	}
	role, _ := roles[name].(map[string]any)
	if role == nil || role["enabled"] == false {
		return nil
	}
	role["enabled"] = false
	roles[name] = role
	if err := d.writeConfig(config); err != nil {
		return err
	}
	if err := d.Proxy.Restart(ctx, "sovereign-runtime"); err != nil {
		if rollbackErr := d.rollback(previous); rollbackErr != nil {
			return fmt.Errorf("runtime restart: %w; rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("runtime restart: %w", err)
	}
	_, err = d.waitReady(ctx, Entry{Role: name})
	if err == nil {
		return nil
	}
	if rollbackErr := d.rollback(previous); rollbackErr != nil {
		return fmt.Errorf("%w; rollback: %v", err, rollbackErr)
	}
	return err
}

func (d LoadDeps) rollback(previous []byte) error {
	if err := d.restore(previous); err != nil {
		return fmt.Errorf("restore runtime configuration: %w", err)
	}
	if err := d.Proxy.Restart(context.Background(), "sovereign-runtime"); err != nil {
		return fmt.Errorf("restart restored runtime: %w", err)
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), d.timeout())
	defer cancel()
	if _, err := d.waitReady(rollbackCtx, Entry{Role: "rollback"}); err != nil {
		return fmt.Errorf("restored runtime readiness: %w", err)
	}
	return nil
}

func (d LoadDeps) timeout() time.Duration {
	if d.ReadyTimeout > 0 {
		return d.ReadyTimeout
	}
	return 5 * time.Minute
}

func (d LoadDeps) waitReady(ctx context.Context, entry Entry) (map[string]any, error) {

	timeout := d.timeout()
	deadline := time.Now().Add(timeout)
	var last runtime.Readiness
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return map[string]any{"state": last.State, "model": entry.Model, "role": entry.Role}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
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
func (d LoadDeps) rewriteRole(entry Entry, request LoadPayload) error {
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
	source := entry.Source
	if source == "offline" {
		source = "local"
	}
	role["source"] = source
	role["model"] = entry.Model
	if entry.Revision != "" {
		role["revision"] = entry.Revision
	}
	if entry.Role == "generation" {
		role["task"] = "generate"
	} else if entry.Role == "embedding" {
		role["task"] = "embed"
		if request.Pooling != "" {
			role["pooling"] = request.Pooling
		}
		if request.Normalization != "" {
			role["normalization"] = request.Normalization
		}
	}
	if request.ServedModelName != "" {
		role["served_model_name"] = request.ServedModelName
	}
	roles[entry.Role] = role
	config["roles"] = roles

	return d.writeConfig(config)
}

func (d LoadDeps) writeConfig(config map[string]any) error {
	updated, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	header := "# Sovereign Runtime configuration — managed by Sovereign Control.\n# Validated against schemas/runtime-config.schema.json.\n"
	return d.restore(append([]byte(header), updated...))
}

func (d LoadDeps) restore(raw []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(d.ConfigPath), ".runtime-*.yaml")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, d.ConfigPath)
}
