// Sovereign Control: the single customer-facing control plane.
package main

import (
	"cmp"
	"log"
	"net/http"
	"os"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/api"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
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

	log.Printf("sovereign-control %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
