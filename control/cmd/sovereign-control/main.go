// Sovereign Control: the single customer-facing control plane.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/api"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/branding"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/database"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/indexes"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/web"
)

var version = "dev" // set via -ldflags at release build

func main() {
	addr := cmp.Or(os.Getenv("SOVEREIGN_CONTROL_LISTEN"), ":8080")

	server := &api.Server{
		Runtime: runtime.New(cmp.Or(os.Getenv("RUNTIME_BASE_URL"), "http://sovereign-runtime:8000")),
		Proxy: dockerproxy.New(
			cmp.Or(os.Getenv("DOCKER_PROXY_BASE_URL"), "http://sovereign-docker-proxy:8081"),
			os.Getenv("DOCKER_PROXY_TOKEN"),
		),
		Gateway: gateway.New(
			cmp.Or(os.Getenv("LITELLM_BASE_URL"), "http://sovereign-gateway:4000"),
			os.Getenv("LITELLM_MASTER_KEY"),
		),
		Version: version,
		UI:      web.Handler(),
	}

	sovereignRoot := cmp.Or(os.Getenv("SOVEREIGN_ROOT"), "/sovereign")
	registry := models.NewRegistry(sovereignRoot + "/config/model-registry.yaml")
	server.Models = registry
	server.Reports = sovereignRoot + "/reports"
	profiles := embeddings.NewRegistry(sovereignRoot + "/config/embedding-profiles.yaml")
	server.Profiles = profiles
	server.GatewayConfigPath = sovereignRoot + "/config/litellm/config.yaml"
	server.Branding = branding.NewStore(sovereignRoot+"/config/branding.yaml", "Customer branding")
	server.Features = branding.NewStore(sovereignRoot+"/config/feature-flags.yaml", "Product feature flags")

	if vectorsURL := os.Getenv("PGVECTOR_CONNECTION_STRING"); vectorsURL != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			pool, err := database.Connect(ctx, vectorsURL)
			if err != nil {
				log.Printf("warning: vectors database unavailable, index management disabled: %v", err)
				return
			}
			store := indexes.New(pool)
			if err := store.EnsureSchema(context.Background()); err != nil {
				log.Printf("warning: vectors schema setup failed: %v", err)
				return
			}
			server.Indexes = store
			log.Print("index management ready (vectors database)")
		}()
	}

	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		pool := connectWithRetry(databaseURL)
		authService := auth.New(pool)
		ctx := context.Background()
		if err := authService.Bootstrap(ctx, "admin", os.Getenv("SOVEREIGN_ADMIN_PASSWORD")); err != nil {
			log.Fatalf("admin bootstrap failed: %v", err)
		}
		server.Auth = authService

		runner := jobs.New(pool)
		loader := models.LoadDeps{
			Registry:   registry,
			ConfigPath: cmp.Or(os.Getenv("RUNTIME_CONFIG_PATH"), sovereignRoot+"/config/runtime.yaml"),
			Proxy:      server.Proxy,
			Runtime:    server.Runtime,
		}
		runner.Register("model-load", loader.HandleLoad)
		activator := embeddings.ActivateDeps{
			Registry:   profiles,
			ConfigPath: loader.ConfigPath,
			Proxy:      server.Proxy,
			Runtime:    server.Runtime,
		}
		runner.Register("profile-activate", activator.HandleActivate)
		evalsImage := cmp.Or(os.Getenv("SOVEREIGN_EVALS_IMAGE"), "ghcr.io/lazarus-ai-research/sovereign-evals:latest")
		runner.Register("evals-run", func(ctx context.Context, payload json.RawMessage) (any, error) {
			var body struct {
				Suite string `json:"suite"`
			}
			if err := json.Unmarshal(payload, &body); err != nil {
				return nil, err
			}
			containerID, err := server.Proxy.RunEvals(ctx, evalsImage, body.Suite)
			if err != nil {
				return nil, err
			}
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
