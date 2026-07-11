// Sovereign Docker Proxy: restricted, allowlisted Docker access for
// Sovereign Control (design.md §3.6). Fails closed: no token, no allowlist,
// or no audit log means no service.
package main

import (
	"log"

	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
