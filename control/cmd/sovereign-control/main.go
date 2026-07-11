// Sovereign Control: the single customer-facing control plane.
// Scaffold — serves only the health endpoint so the container comes up.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

var version = "dev" // set via -ldflags at release build

func main() {
	addr := os.Getenv("SOVEREIGN_CONTROL_LISTEN")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/control/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":%q}`, version)
	})

	log.Printf("sovereign-control %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
