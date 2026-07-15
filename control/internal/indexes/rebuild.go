package indexes

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	workspaceapi "github.com/Lazarus-AI-Research/sovereign-stack/control/internal/workspace"
)

type RebuildPayload struct {
	TargetIndexID        string `json:"target_index_id"`
	ActivateWhenComplete bool   `json:"activate_when_complete"`
}

type RebuildDeps struct {
	Store     *Store
	Profiles  *embeddings.Registry
	Activator embeddings.ActivateDeps
	Workspace *workspaceapi.Client
}

func (d RebuildDeps) Handle(ctx context.Context, payload json.RawMessage) (result any, returnErr error) {
	var request RebuildPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("bad rebuild payload: %w", err)
	}
	target, err := d.Store.Get(ctx, request.TargetIndexID)
	if err != nil {
		return nil, err
	}
	profile, err := d.Profiles.Get(target.ProfileID)
	if err != nil {
		d.Store.Fail(ctx, target.ID, err)
		return nil, err
	}
	previous, previousErr := d.Store.Active(ctx, target.WorkspaceID)
	if err := d.Store.SetMaintenance(ctx, target.WorkspaceID, true,
		"Embedding index rebuild in progress"); err != nil {
		return nil, err
	}

	rollback := func(cause error) {
		_ = d.Store.Fail(context.Background(), target.ID, cause)
		if previousErr == nil && previous.ProfileID != target.ProfileID {
			raw, _ := json.Marshal(embeddings.ActivatePayload{ProfileID: previous.ProfileID})
			_, _ = d.Activator.HandleActivate(context.Background(), raw)
		}
		_ = d.Store.SetMaintenance(context.Background(), target.WorkspaceID, false, "")
	}
	defer func() {
		if returnErr != nil {
			rollback(returnErr)
		}
	}()

	activatePayload, _ := json.Marshal(embeddings.ActivatePayload{ProfileID: target.ProfileID})
	activation, err := d.Activator.HandleActivate(ctx, activatePayload)
	if err != nil {
		return nil, fmt.Errorf("load target embedding profile: %w", err)
	}
	dimensions, _ := activation.(map[string]any)["dimensions"].(int)
	if dimensions < 1 {
		return nil, fmt.Errorf("target embedding profile did not report dimensions")
	}
	if err := d.Store.SetDimensions(ctx, target.ID, dimensions); err != nil {
		return nil, err
	}
	if err := d.Store.SetStatus(ctx, target.ID, "building"); err != nil {
		return nil, err
	}

	rebuilt, err := d.Workspace.RebuildIndex(ctx, workspaceapi.RebuildRequest{
		WorkspaceSlug: target.ProviderSlug, IndexVersion: target.ID,
		QueryPrefix: profile.QueryPrefix, DocumentPrefix: profile.DocumentPrefix,
		PreprocessingVer: profile.PreprocessingVersion,
	})
	if err != nil {
		return nil, err
	}
	if err := d.Store.Progress(ctx, target.ID, rebuilt.DocumentCount,
		rebuilt.ProcessedDocuments, rebuilt.VectorCount); err != nil {
		return nil, err
	}
	if len(rebuilt.Failures) > 0 || rebuilt.ProcessedDocuments != rebuilt.DocumentCount {
		return nil, fmt.Errorf("rebuild incomplete: processed %d/%d documents; failures=%v",
			rebuilt.ProcessedDocuments, rebuilt.DocumentCount, rebuilt.Failures)
	}
	if rebuilt.VectorCount < 1 {
		return nil, fmt.Errorf("rebuilt index contains no vectors")
	}
	if err := d.Store.SetStatus(ctx, target.ID, "validating"); err != nil {
		return nil, err
	}
	actual, err := d.Store.CountVectors(ctx, target.ID, target.ProviderSlug)
	if err != nil || actual != rebuilt.VectorCount {
		return nil, fmt.Errorf("vector validation failed: workspace=%d database=%d: %w",
			rebuilt.VectorCount, actual, err)
	}
	if request.ActivateWhenComplete {
		active, err := d.Store.Activate(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"index": active, "rebuild": rebuilt}, nil
	}
	if err := d.Store.SetMaintenance(ctx, target.WorkspaceID, false, ""); err != nil {
		return nil, err
	}
	return map[string]any{"index_id": target.ID, "status": "validating", "rebuild": rebuilt}, nil
}
