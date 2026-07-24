// Sovereign Hostd: allowlisted host lifecycle operations for the Control UI.
package main

import (
	"cmp"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hostservice"
)

func main() {
	home := cmp.Or(os.Getenv("SOVEREIGN_HOME"), filepath.Join(os.Getenv("HOME"), ".sovereign"))
	tokenFile := cmp.Or(os.Getenv("SOVEREIGN_HOSTD_TOKEN_FILE"), filepath.Join(home, "state", "hostd-token"))
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		log.Fatalf("hostd token: %v", err)
	}
	binary := cmp.Or(os.Getenv("SOVEREIGN_CLI_PATH"), filepath.Join(filepath.Dir(os.Args[0]), "sovereign"))
	listen := cmp.Or(os.Getenv("SOVEREIGN_HOSTD_LISTEN"), "127.0.0.1:9191")
	server := hostservice.New(home, binary, strings.TrimSpace(string(raw)))
	log.Printf("sovereign-hostd listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, server.Handler()))
}
