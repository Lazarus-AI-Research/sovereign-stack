// Sovereign Control: the single customer-facing control plane.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/api"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/attribution"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/backups"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/branding"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/bundles"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/credentials"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/database"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hostagent"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/indexes"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/support"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/updates"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/web"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/workspace"
	"github.com/jackc/pgx/v5"
)

var version = "dev" // set via -ldflags at release build

func main() {
	// Attribution up front, before anything else can fail (§ branding policy).
	log.Print(attribution.Banner())

	addr := cmp.Or(os.Getenv("SOVEREIGN_CONTROL_LISTEN"), ":8080")
	runtimeBaseURL := cmp.Or(os.Getenv("RUNTIME_BASE_URL"), "http://sovereign-runtime:8000")
	gatewayBaseURL := cmp.Or(os.Getenv("LITELLM_BASE_URL"), "http://sovereign-gateway:4000")

	server := &api.Server{
		Runtime: runtime.New(runtimeBaseURL),
		Embeddings: embeddings.NewClient(cmp.Or(
			os.Getenv("SOVEREIGN_EMBEDDINGS_BASE_URL"),
			"http://sovereign-embeddings:42666/v1",
		)),
		Proxy: dockerproxy.New(
			cmp.Or(os.Getenv("DOCKER_PROXY_BASE_URL"), "http://sovereign-docker-proxy:8081"),
			os.Getenv("DOCKER_PROXY_TOKEN"),
		),
		Gateway: gateway.New(
			gatewayBaseURL,
			os.Getenv("LITELLM_MASTER_KEY"),
		),
		Version:       version,
		UI:            web.Handler(),
		OperatorToken: os.Getenv("SOVEREIGN_OPERATOR_TOKEN"),
	}

	sovereignRoot := cmp.Or(os.Getenv("SOVEREIGN_ROOT"), "/sovereign")
	server.ClaimFile = cmp.Or(os.Getenv("SOVEREIGN_ADMIN_CLAIM_FILE"), sovereignRoot+"/state/admin-claim")
	hostLifecycleToken := os.Getenv("SOVEREIGN_HOSTD_TOKEN") // upgrade/dev compatibility
	if hostLifecycleToken == "" {
		tokenPath := cmp.Or(os.Getenv("SOVEREIGN_HOSTD_TOKEN_FILE"), sovereignRoot+"/state/hostd-token")
		if raw, err := os.ReadFile(tokenPath); err == nil {
			hostLifecycleToken = strings.TrimSpace(string(raw))
		}
	}
	server.HostLifecycle = hostagent.NewLifecycle(
		cmp.Or(os.Getenv("SOVEREIGN_HOSTD_URL"), "http://host.docker.internal:9191"),
		hostLifecycleToken,
	)
	server.Updates = updates.New(cmp.Or(
		os.Getenv("SOVEREIGN_RELEASE_FEED_URL"),
		"https://api.github.com/repos/Lazarus-AI-Research/sovereign-stack/releases/latest",
	))
	registry := models.NewRegistry(sovereignRoot + "/config/model-registry.yaml")
	server.Models = registry
	server.Reports = sovereignRoot + "/reports"
	server.SovereignRoot = sovereignRoot
	server.Workspace = workspace.NewWithIndexAdmin(
		cmp.Or(os.Getenv("WORKSPACE_BASE_URL"), "http://sovereign-workspace:3001"),
		cmp.Or(os.Getenv("WORKSPACE_INDEX_ADMIN_URL"), "http://sovereign-workspace:3011"),
		os.Getenv("WORKSPACE_INDEX_ADMIN_TOKEN"),
	)
	profiles := embeddings.NewRegistry(sovereignRoot + "/config/embedding-profiles.yaml")
	server.Profiles = profiles
	server.GatewayConfigPath = cmp.Or(os.Getenv("LITELLM_CONFIG_PATH"), sovereignRoot+"/config/litellm/config.yaml")
	server.Branding = branding.NewStore(sovereignRoot+"/config/branding.yaml", "Customer branding")
	server.Features = branding.NewStore(sovereignRoot+"/config/feature-flags.yaml", "Product feature flags")

	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		pool := connectWithRetry(databaseURL)
		authService := auth.New(pool)
		ctx := context.Background()
		if password := os.Getenv("SOVEREIGN_ADMIN_PASSWORD"); password != "" {
			// Upgrade compatibility: appliances that already carry the legacy
			// bootstrap password retain their existing administrator unchanged.
			if err := authService.Bootstrap(ctx, "admin", password); err != nil {
				log.Fatalf("admin bootstrap failed: %v", err)
			}
		} else {
			token, expires, err := authService.EnsureSetupClaim(ctx)
			if err != nil {
				log.Fatalf("admin claim bootstrap failed: %v", err)
			}
			if token != "" {
				if err := os.MkdirAll(filepath.Dir(server.ClaimFile), 0o700); err != nil {
					log.Fatalf("admin claim directory: %v", err)
				}
				if err := os.WriteFile(server.ClaimFile, []byte(token+"\n"), 0o600); err != nil {
					log.Fatalf("admin claim file: %v", err)
				}
				log.Printf("first administrator claim ready until %s", expires.Format(time.RFC3339))
			} else {
				_ = os.Remove(server.ClaimFile)
			}
		}
		server.Auth = authService
		vaultPath := cmp.Or(os.Getenv("SOVEREIGN_VAULT_KEY_FILE"), sovereignRoot+"/secrets/control-vault.key")
		if key, err := credentials.LoadKey(vaultPath, os.Getenv("SOVEREIGN_VAULT_KEY")); err != nil {
			log.Printf("warning: provider credential vault disabled: %v", err)
		} else if vault, err := credentials.New(pool, key); err != nil {
			log.Printf("warning: provider credential vault disabled: %v", err)
		} else {
			server.Credentials = vault
			log.Print("encrypted provider credential vault ready")
		}

		runner := jobs.New(pool)
		loader := models.LoadDeps{
			Registry:   registry,
			ConfigPath: cmp.Or(os.Getenv("RUNTIME_CONFIG_PATH"), sovereignRoot+"/config/runtime.yaml"),
			Proxy:      server.Proxy,
			Runtime:    server.Runtime,
		}
		hostAgent := hostagent.New(
			cmp.Or(os.Getenv("SOVEREIGN_AGENT_URL"), "http://host.docker.internal:9100"),
			os.Getenv("SOVEREIGN_AGENT_TOKEN"),
		)
		runner.Register("model-load", loader.HandleLoad)
		backupDeps := &backups.Deps{
			Proxy:     server.Proxy,
			Root:      sovereignRoot,
			Databases: []string{"sovereign_control", "litellm", "workspace", "phoenix", "vectors"},
		}
		server.Backups = backupDeps
		runner.Register("backup", backupDeps.HandleBackup)
		runner.Register("backup-restore", backupDeps.HandleRestore)
		bundleImages := []string{}
		for _, key := range []string{
			"SOVEREIGN_CONTROL_IMAGE", "SOVEREIGN_DOCKER_PROXY_IMAGE", "SOVEREIGN_EVALS_IMAGE",
			"SOVEREIGN_WORKSPACE_IMAGE", "SOVEREIGN_RUNTIME_IMAGE", "SOVEREIGN_EMBEDDINGS_IMAGE", "CADDY_IMAGE", "LITELLM_IMAGE",
			"PGVECTOR_IMAGE", "PHOENIX_IMAGE", "PROMETHEUS_IMAGE", "GRAFANA_IMAGE", "LOKI_IMAGE", "OTEL_IMAGE",
		} {
			if image := os.Getenv(key); image != "" {
				bundleImages = append(bundleImages, image)
			}
		}
		bundleDeps := &bundles.Deps{
			Root: sovereignRoot, ReleaseRoot: cmp.Or(os.Getenv("SOVEREIGN_RELEASE_MOUNT"), "/sovereign-release"),
			Version: version, Profile: os.Getenv("SOVEREIGN_PROFILE"), Images: bundleImages, Proxy: server.Proxy,
		}
		server.Bundles = bundleDeps
		runner.Register("bundle-create", bundleDeps.HandleCreate)
		supportDeps := &support.Deps{Root: sovereignRoot, Version: version, Profile: os.Getenv("SOVEREIGN_PROFILE")}
		server.Support = supportDeps
		runner.Register("support-bundle", supportDeps.Handle)
		activator := embeddings.ActivateDeps{
			Registry: profiles,
			Service:  server.Embeddings,
			Providers: map[string]embeddings.Prober{
				"embeddinggemma":    server.Embeddings,
				"sovereign-runtime": embeddings.NewOpenAIClient(runtimeBaseURL+"/v1", os.Getenv("SOVEREIGN_RUNTIME_API_KEY")),
				"openai-compatible": embeddings.NewOpenAIClient(gatewayBaseURL+"/v1", os.Getenv("LITELLM_MASTER_KEY")),
			},
		}
		server.EmbeddingActivator = &activator
		runner.Register("profile-activate", activator.HandleActivate)
		if vectorsURL := os.Getenv("PGVECTOR_CONNECTION_STRING"); vectorsURL != "" {
			vectorsPool := connectPoolWithRetry(vectorsURL)
			store := indexes.New(vectorsPool)
			if err := store.EnsureSchema(context.Background()); err != nil {
				log.Fatalf("vectors schema setup failed: %v", err)
			}
			server.Indexes = store
			rebuilder := indexes.RebuildDeps{
				Store: store, Profiles: profiles, Activator: activator, Workspace: server.Workspace,
			}
			runner.Register("index-rebuild", rebuilder.Handle)
			prepareEmbedding := func(ctx context.Context, _ string, profile embeddings.Profile) error {
				if profile.Provider == "sovereign-runtime" {
					entry, err := registry.Get(profile.ModelEntryID)
					if err != nil {
						return err
					}
					if entry.Role != "embedding" {
						return fmt.Errorf("model registry entry %q has role %q, want embedding", entry.ID, entry.Role)
					}
					if os.Getenv("SOVEREIGN_PROFILE") == "metal-arm64" {
						pooling := cmp.Or(profile.Pooling, "mean")
						normalization := cmp.Or(profile.Normalization, "l2")
						if err := hostAgent.ConfigureEmbedding(ctx, hostagent.EmbeddingRoleRequest{
							Artifact: entry.Artifact, Revision: entry.Revision, SHA256: entry.SHA256,
							Pooling: pooling, Normalization: normalization, ContextLength: 2048,
						}); err != nil {
							return err
						}
					}
					_, err = loader.Load(ctx, models.LoadPayload{
						ModelID: entry.ID, ServedModelName: profile.ServedModelName,
						Pooling: profile.Pooling, Normalization: profile.Normalization,
					})
					if err != nil {
						if os.Getenv("SOVEREIGN_PROFILE") == "metal-arm64" {
							_ = hostAgent.DisableEmbedding(context.Background())
						}
						return err
					}
				} else {
					if err := loader.DisableRole(ctx, "embedding"); err != nil {
						return err
					}
					if os.Getenv("SOVEREIGN_PROFILE") == "metal-arm64" {
						if err := hostAgent.DisableEmbedding(ctx); err != nil {
							return err
						}
					}
				}
				if err := gateway.GenerateConfig(ctx, server.GatewayConfigPath, registry, profiles, server.Credentials); err != nil {
					return err
				}
				if err := server.Proxy.Restart(ctx, "sovereign-gateway"); err != nil {
					return err
				}
				deadline := time.Now().Add(2 * time.Minute)
				for !server.Gateway.Healthy(ctx) {
					if time.Now().After(deadline) {
						return fmt.Errorf("gateway did not become healthy after embedding route reload")
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(2 * time.Second):
					}
				}
				return nil
			}
			runner.Register("profile-activate", indexes.GlobalActivationDeps{
				Store: store, Profiles: profiles, Activator: activator, Rebuilder: rebuilder,
				Workspace: server.Workspace, Prepare: prepareEmbedding,
			}.Handle)
			go initializeDefaultEmbedding(ctx, store, runner, server.Embeddings, profiles)
			log.Print("versioned index management ready (vectors database)")
		}
		evalsImage := cmp.Or(os.Getenv("SOVEREIGN_EVALS_IMAGE"), "ghcr.io/lazarus-ai-research/sovereign-evals:"+version)
		runner.Register("evals-run", func(ctx context.Context, payload json.RawMessage) (any, error) {
			total := int64(2)
			_ = jobs.Report(ctx, jobs.Progress{Stage: "preparing", Message: "Preparing evaluation suite", Current: 0, Total: &total, Unit: "steps"})
			var body struct {
				Suite string `json:"suite"`
			}
			if err := json.Unmarshal(payload, &body); err != nil {
				return nil, err
			}
			_ = jobs.Report(ctx, jobs.Progress{Stage: "running", Message: "Running " + body.Suite + " evaluation", Current: 1, Total: &total, Unit: "steps"})
			containerID, err := server.Proxy.RunEvals(ctx, evalsImage, body.Suite)
			if err != nil {
				return nil, err
			}
			_ = jobs.Report(ctx, jobs.Progress{Stage: "complete", Message: "Evaluation complete", Current: 2, Total: &total, Unit: "steps"})
			return map[string]string{"container_id": containerID, "suite": body.Suite}, nil
		})
		server.Jobs = runner
		go runner.Run(ctx)
	} else {
		log.Print("warning: DATABASE_URL not set — authentication disabled, state is ephemeral")
	}

	log.Printf("sovereign-control %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}

// connectWithRetry migrates and connects, retrying while postgres starts.
// Persistent failure is fatal: Docker restarts us, and compose gates startup
// on the postgres healthcheck anyway.
func connectWithRetry(databaseURL string) *databasePool {
	deadline := time.Now().Add(90 * time.Second)
	for {
		err := database.Migrate(databaseURL)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pool, connectErr := database.Connect(ctx, databaseURL)
			cancel()
			if connectErr == nil {
				return pool
			}
			err = connectErr
		}
		if time.Now().After(deadline) {
			log.Fatalf("database not ready after 90s: %v", err)
		}
		log.Printf("waiting for database: %v", err)
		time.Sleep(3 * time.Second)
	}
}

type databasePool = database.Pool

// Fresh appliances ship with EmbeddingGemma already selected. Initialization
// does not delay the portal: it waits for the tiny sidecar in the background,
// records the state immediately when there are no workspaces, and uses the
// normal transactional rebuild job when upgrading an appliance with data.
func initializeDefaultEmbedding(ctx context.Context, store *indexes.Store, runner *jobs.Runner, service *embeddings.Client, profiles *embeddings.Registry) {
	if _, err := store.EmbeddingState(ctx); err == nil {
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("default embedding state: %v", err)
		return
	}
	profile, err := profiles.Get("gemma-default")
	if err != nil {
		log.Printf("default embedding profile unavailable: %v", err)
		return
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		dimensions, probeErr := service.Probe(ctx, profile.ServedModelName)
		if probeErr == nil && dimensions > 0 {
			release, err := store.ActivationLock(ctx)
			if err != nil {
				log.Printf("default embedding activation lock: %v", err)
				return
			}
			if _, err := store.EmbeddingState(ctx); err == nil {
				release()
				return
			} else if !errors.Is(err, pgx.ErrNoRows) {
				release()
				log.Printf("default embedding state recheck: %v", err)
				return
			}
			bindings, err := store.Bindings(ctx)
			if err != nil {
				release()
				log.Printf("default embedding bindings: %v", err)
				return
			}
			if len(bindings) == 0 {
				err = store.ActivateBatch(ctx, nil, indexes.EmbeddingState{
					ProfileID: "gemma-default", Provider: profile.Provider,
					ServedModelName: profile.ServedModelName, Dimensions: dimensions,
				})
				if err != nil {
					log.Printf("default embedding state initialization: %v", err)
				}
				release()
				return
			}
			release()
			if _, err := runner.Enqueue(ctx, "profile-activate", embeddings.ActivatePayload{ProfileID: "gemma-default", InitializeOnly: true}); err != nil {
				log.Printf("default embedding rebuild enqueue: %v", err)
			}
			return
		}
		if time.Now().After(deadline) {
			log.Printf("default embedding service was not ready before initialization deadline: %v", probeErr)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func connectPoolWithRetry(databaseURL string) *databasePool {
	deadline := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, err := database.Connect(ctx, databaseURL)
		cancel()
		if err == nil {
			return pool
		}
		if time.Now().After(deadline) {
			log.Fatalf("database not ready after 90s: %v", err)
		}
		log.Printf("waiting for database: %v", err)
		time.Sleep(3 * time.Second)
	}
}
