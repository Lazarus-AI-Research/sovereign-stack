// Sovereign Docker Proxy: restricted, allowlisted Docker access for
// Sovereign Control (design.md §3.6). Never mounts more capability than the
// allowlist grants; audits every action.
// Scaffold — serves only the status endpoint so the container comes up.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

var version = "dev" // set via -ldflags at release build

func main() {
	addr := os.Getenv("SOVEREIGN_DOCKER_PROXY_LISTEN")
	if addr == "" {
		addr = ":8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/docker/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":%q}`, version)
	})

	log.Printf("sovereign-docker-proxy %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
