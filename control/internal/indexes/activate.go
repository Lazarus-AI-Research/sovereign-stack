package indexes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	workspaceapi "github.com/Lazarus-AI-Research/sovereign-stack/control/internal/workspace"
	"github.com/jackc/pgx/v5"
)

// GlobalActivationDeps performs the appliance-wide provider cutover. The old
// provider and active indexes remain the committed state until every candidate
// validates; a failed attempt restores the prior provider before maintenance
// is released.
type GlobalActivationDeps struct {
	Store     *Store
	Profiles  *embeddings.Registry
	Activator embeddings.ActivateDeps
	Rebuilder RebuildDeps
	Workspace *workspaceapi.Client
	Prepare   func(context.Context, string, embeddings.Profile) error
}

func (d GlobalActivationDeps) Handle(ctx context.Context, payload json.RawMessage) (result any, returnErr error) {
	var request embeddings.ActivatePayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("bad activation payload: %w", err)
	}
	profile, err := d.Profiles.Get(request.ProfileID)
	if err != nil {
		return nil, err
	}
	release, err := d.Store.ActivationLock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if request.InitializeOnly {
		if state, err := d.Store.EmbeddingState(ctx); err == nil {
			return map[string]any{"state": state, "initialized": false}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	previousState, previousStateErr := d.Store.EmbeddingState(ctx)
	if previousStateErr != nil && !errors.Is(previousStateErr, pgx.ErrNoRows) {
		return nil, previousStateErr
	}

	bindings, err := d.Store.Bindings(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		known[binding.WorkspaceID] = binding
	}
	if d.Workspace != nil {
		workspaces, err := d.Workspace.Workspaces(ctx)
		if err != nil {
			return nil, fmt.Errorf("list workspaces for global rebuild: %w", err)
		}
		for _, workspace := range workspaces {
			known[workspace.ID] = Binding{WorkspaceID: workspace.ID, ProviderSlug: workspace.Slug}
		}
	}

	targetIDs := make([]string, 0, len(known))
	maintenanceSet := false
	providerPrepared := false
	defer func() {
		if returnErr == nil {
			return
		}
		for _, targetID := range targetIDs {
			_ = d.Store.Fail(context.Background(), targetID, returnErr)
		}
		if providerPrepared && d.Prepare != nil {
			rollbackID := previousState.ProfileID
			if previousStateErr != nil {
				rollbackID = "gemma-default"
			}
			if previousProfile, err := d.Profiles.Get(rollbackID); err == nil {
				_ = d.Prepare(context.Background(), rollbackID, previousProfile)
			}
		}
		if maintenanceSet {
			_ = d.Store.SetAllMaintenance(context.Background(), false, "")
		}
	}()
	for _, binding := range known {
		target, err := d.Store.CreatePending(ctx, CreateRequest{
			WorkspaceID: binding.WorkspaceID, ProviderSlug: binding.ProviderSlug,
			ProfileID: request.ProfileID, ModelID: profile.Model,
			ModelRevision: profile.Revision, Normalization: profile.Normalization,
			DistanceMetric: profile.DistanceMetric, QueryPrefix: profile.QueryPrefix,
			DocumentPrefix: profile.DocumentPrefix, ChunkingStrategy: profile.ChunkingStrategy,
			PreprocessingVersion: profile.PreprocessingVersion,
		})
		if err != nil {
			return nil, err
		}
		targetIDs = append(targetIDs, target.ID)
	}
	if err := d.Store.SetAllMaintenance(ctx, true, "Appliance embedding provider change in progress"); err != nil {
		return nil, err
	}
	maintenanceSet = true

	if d.Prepare != nil {
		providerPrepared = true
		if err := d.Prepare(ctx, request.ProfileID, profile); err != nil {
			return nil, fmt.Errorf("prepare embedding provider: %w", err)
		}
	}
	raw, _ := json.Marshal(request)
	probe, err := d.Activator.HandleActivate(ctx, raw)
	if err != nil {
		return nil, err
	}
	dimensions, _ := probe.(map[string]any)["dimensions"].(int)
	if dimensions < 1 {
		return nil, fmt.Errorf("embedding provider returned no dimensions")
	}
	for _, targetID := range targetIDs {
		rebuildPayload, _ := json.Marshal(RebuildPayload{
			TargetIndexID: targetID, ActivateWhenComplete: false, KeepMaintenance: true,
		})
		if _, err := d.Rebuilder.Handle(ctx, rebuildPayload); err != nil {
			return nil, fmt.Errorf("rebuild index %s: %w", targetID, err)
		}
	}
	state := EmbeddingState{
		ProfileID: request.ProfileID, Provider: profile.Provider,
		ServedModelName: profile.ServedModelName, Dimensions: dimensions,
	}
	if err := d.Store.ActivateBatch(ctx, targetIDs, state); err != nil {
		return nil, err
	}
	state, _ = d.Store.EmbeddingState(ctx)
	return map[string]any{
		"state": state, "rebuilt_workspaces": len(targetIDs), "indexes": targetIDs,
	}, nil
}
