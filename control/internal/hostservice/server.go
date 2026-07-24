// Package hostservice implements the narrowly allowlisted host lifecycle API.
// It intentionally delegates only reviewed Sovereign CLI operations and never
// accepts command names, filesystem paths, Compose arguments, or shell text.
package hostservice

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const BasePath = "/host/v1"

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type sovereignRunner struct {
	binary string
	home   string
}

func (r sovereignRunner) Run(ctx context.Context, _ string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, r.binary, arguments...)
	command.Env = append(os.Environ(), "SOVEREIGN_HOME="+r.home, "SOVEREIGN_NO_OPEN=1")
	return command.CombinedOutput()
}

type Server struct {
	Home   string
	Token  string
	Runner CommandRunner
	// Restart is invoked after a successful self-update so launchd/systemd
	// replaces the running process with the newly installed binary.
	Restart func()
	// MutationDelay lets the HTTP response clear the portal before an access
	// change restarts Caddy. Tests set it to zero for deterministic execution.
	MutationDelay time.Duration
	mu            sync.Mutex
	operation     sync.Mutex
	audit         sync.Mutex
}

func New(home, binary, token string) *Server {
	return &Server{
		Home: home, Token: token, Runner: sovereignRunner{binary: binary, home: home},
		Restart: func() { os.Exit(1) }, MutationDelay: time.Second,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) authorized(r *http.Request) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return s.Token != "" && len(provided) == len(s.Token) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.Token)) == 1
}

func readEnvironment(path string) map[string]string {
	result := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

var hostnamePattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

type UpdateState struct {
	State     string `json:"state"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) updatePath() string { return s.Home + "/state/update.json" }

func (s *Server) writeUpdateState(state UpdateState) {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.MarshalIndent(state, "", "  ")
	_ = os.MkdirAll(s.Home+"/state", 0o700)
	temporary := s.updatePath() + ".tmp"
	if os.WriteFile(temporary, append(raw, '\n'), 0o600) == nil {
		_ = os.Rename(temporary, s.updatePath())
	}
}

func (s *Server) readUpdateState() UpdateState {
	state := UpdateState{State: "idle", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	raw, err := os.ReadFile(s.updatePath())
	if err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func (s *Server) record(operation, outcome, detail string) {
	event := map[string]string{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"operation": operation,
		"outcome":   outcome,
		"detail":    detail,
	}
	raw, _ := json.Marshal(event)
	s.audit.Lock()
	defer s.audit.Unlock()
	path := s.Home + "/logs/hostd/operations.jsonl"
	if os.MkdirAll(s.Home+"/logs/hostd", 0o700) != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(append(raw, '\n'))
	_ = file.Close()
}

func networkArguments(mode, target string) ([]string, error) {
	switch mode {
	case "desktop":
		return []string{"access", "desktop"}, nil
	case "lan":
		if target == "" {
			return []string{"access", "lan"}, nil
		}
		address, err := netip.ParseAddr(target)
		if err != nil || !address.Is4() || !address.IsPrivate() {
			return nil, fmt.Errorf("LAN target must be a private IPv4 address")
		}
		return []string{"access", "lan", target}, nil
	case "domain":
		if !hostnamePattern.MatchString(target) {
			return nil, fmt.Errorf("domain target must be a valid hostname")
		}
		return []string{"access", "domain", strings.ToLower(target)}, nil
	default:
		return nil, fmt.Errorf("network mode must be desktop, lan, or domain")
	}
}

func privateIPv4Addresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	result := []string{}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 ||
			strings.HasPrefix(networkInterface.Name, "docker") || strings.HasPrefix(networkInterface.Name, "br-") || strings.HasPrefix(networkInterface.Name, "veth") {
			continue
		}
		addresses, _ := networkInterface.Addrs()
		for _, candidate := range addresses {
			address, err := netip.ParsePrefix(candidate.String())
			if err == nil && address.Addr().Is4() && address.Addr().IsPrivate() && !address.Addr().IsLoopback() {
				result = append(result, address.Addr().String())
			}
		}
	}
	return result
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET "+BasePath+"/status", func(w http.ResponseWriter, _ *http.Request) {
		environment := readEnvironment(s.Home + "/.env")
		writeJSON(w, http.StatusOK, map[string]any{
			"managed": true, "access_mode": environment["SOVEREIGN_ACCESS_MODE"],
			"public_url": environment["SOVEREIGN_PUBLIC_URL"], "bind_address": environment["SOVEREIGN_BIND_ADDRESS"],
			"site_address": environment["SOVEREIGN_SITE_ADDRESS"], "version": environment["SOVEREIGN_VERSION"],
			"lan_addresses": privateIPv4Addresses(),
		})
	})
	mux.HandleFunc("PUT "+BasePath+"/network", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Mode   string `json:"mode"`
			Target string `json:"target"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network request"})
			return
		}
		arguments, err := networkArguments(request.Mode, request.Target)
		if err != nil {
			s.record("network", "rejected", request.Mode)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		apply := func() {
			if s.MutationDelay > 0 {
				time.Sleep(s.MutationDelay)
			}
			s.operation.Lock()
			defer s.operation.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if _, err := s.Runner.Run(ctx, "network", arguments...); err != nil {
				s.record("network", "failed", request.Mode)
				return
			}
			s.record("network", "succeeded", request.Mode)
		}
		s.record("network", "scheduled", request.Mode)
		if s.MutationDelay <= 0 {
			apply()
		} else {
			go apply()
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"scheduled": true, "mode": request.Mode})
	})
	mux.HandleFunc("POST "+BasePath+"/repair", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		s.operation.Lock()
		defer s.operation.Unlock()
		output, err := s.Runner.Run(ctx, "repair", "start")
		if err != nil {
			s.record("repair", "failed", "reconcile portal")
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "portal repair failed", "detail": string(output)})
			return
		}
		s.record("repair", "succeeded", "reconcile portal")
		writeJSON(w, http.StatusOK, map[string]any{"repaired": true, "output": string(output)})
	})
	mux.HandleFunc("GET "+BasePath+"/updates", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.readUpdateState())
	})
	mux.HandleFunc("POST "+BasePath+"/updates", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Version string `json:"version"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || !versionPattern.MatchString(request.Version) {
			s.record("update", "rejected", "invalid version")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version must be a semantic release version"})
			return
		}
		s.mu.Lock()
		state := s.readUpdateState()
		if state.State == "scheduled" || state.State == "backing_up" || state.State == "installing" {
			s.mu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]string{"error": "an update is already running"})
			return
		}
		started := time.Now().UTC().Format(time.RFC3339)
		s.writeUpdateState(UpdateState{State: "scheduled", Version: request.Version, Message: "Update scheduled", StartedAt: started})
		s.record("update", "scheduled", request.Version)
		s.mu.Unlock()
		go func(version, startedAt string) {
			time.Sleep(2 * time.Second) // allow the browser response through Caddy
			s.writeUpdateState(UpdateState{State: "installing", Version: version, Message: "Backing up and installing signed release", StartedAt: startedAt})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			s.operation.Lock()
			output, err := s.Runner.Run(ctx, "update", "update", version)
			s.operation.Unlock()
			message := strings.TrimSpace(string(output))
			if len(message) > 2000 {
				message = message[len(message)-2000:]
			}
			if err != nil {
				s.writeUpdateState(UpdateState{State: "rolled_back", Version: version, Message: "Update failed and the previous release was restored. " + message, StartedAt: startedAt})
				s.record("update", "rolled_back", version)
				return
			}
			s.writeUpdateState(UpdateState{State: "complete", Version: version, Message: "Update installed successfully", StartedAt: startedAt})
			s.record("update", "succeeded", version)
			if s.Restart != nil {
				s.Restart()
			}
		}(request.Version, started)
		writeJSON(w, http.StatusAccepted, map[string]any{"scheduled": true, "version": request.Version})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != BasePath+"/health" && !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authorized"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}
