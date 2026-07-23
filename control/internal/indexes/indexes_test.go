// Integration tests against a real Postgres with pgvector. Skipped unless
// TEST_VECTORS_DATABASE_URL is set.
package indexes

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/database"
)

const workspace = "11111111-1111-1111-1111-111111111111"

func testStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	url := os.Getenv("TEST_VECTORS_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_VECTORS_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	store := New(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	pool.Exec(ctx, "TRUNCATE vectors.workspace_bindings, vectors.index_versions CASCADE")
	return ctx, store
}

func create(t *testing.T, ctx context.Context, store *Store, profile string) Version {
	t.Helper()
	version, err := store.Create(ctx, CreateRequest{
		WorkspaceID:    workspace,
		ProviderSlug:   "default",
		ProfileID:      profile,
		ModelID:        "ggml-org/embeddinggemma-300M-qat-q4_0-GGUF",
		ModelRevision:  "8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73",
		Dimensions:     768,
		Normalization:  "l2",
		DistanceMetric: "cosine",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return version
}

func TestCreateRequiresDiscoveredDimensions(t *testing.T) {
	ctx, store := testStore(t)
	_, err := store.Create(ctx, CreateRequest{
		WorkspaceID: workspace, ProviderSlug: "default", ProfileID: "p", ModelID: "m", DistanceMetric: "cosine",
	})
	if err == nil {
		t.Fatal("zero dimensions accepted")
	}
}

func TestActivateIsAtomicPerWorkspace(t *testing.T) {
	ctx, store := testStore(t)
	first := create(t, ctx, store, "gemma-default")
	second := create(t, ctx, store, "gemma-reduced")
	store.Progress(ctx, first.ID, 1, 1, 1)
	store.Progress(ctx, second.ID, 1, 1, 1)

	if _, err := store.Activate(ctx, first.ID); err != nil {
		t.Fatalf("activate first: %v", err)
	}
	if _, err := store.Activate(ctx, second.ID); err != nil {
		t.Fatalf("activate second: %v", err)
	}

	versions, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	active := 0
	for _, version := range versions {
		if version.Status == "active" {
			active++
			if version.ID != second.ID {
				t.Errorf("wrong active version: %s", version.ID)
			}
		}
	}
	if active != 1 {
		t.Errorf("expected exactly one active version, got %d", active)
	}

	// the replaced version is inactive but still present (§11.2)
	replaced, _ := store.Get(ctx, first.ID)
	if replaced.Status != "inactive" {
		t.Errorf("replaced version status: %s", replaced.Status)
	}
}

func TestActiveVersionCannotBeDeleted(t *testing.T) {
	ctx, store := testStore(t)
	version := create(t, ctx, store, "gemma-default")
	store.Progress(ctx, version.ID, 1, 1, 1)
	if _, err := store.Activate(ctx, version.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := store.Delete(ctx, version.ID); err == nil {
		t.Fatal("active version deleted")
	}
	replacement := create(t, ctx, store, "gemma-reduced")
	store.Progress(ctx, replacement.ID, 1, 1, 1)
	store.Activate(ctx, replacement.ID)
	if err := store.Delete(ctx, version.ID); err != nil {
		t.Fatalf("delete inactive: %v", err)
	}
}
