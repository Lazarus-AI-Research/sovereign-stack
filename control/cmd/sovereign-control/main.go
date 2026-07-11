// Sovereign Control: the single customer-facing control plane.
package main

import (
	"cmp"
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/api"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/database"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
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
		// Job handlers register here as milestones land (model loads,
		// index rebuilds, backups, eval runs).
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
