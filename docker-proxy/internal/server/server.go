// Package server wires auth, allowlist, audit, and the Docker client into
// the proxy's internal HTTP surface (design.md §18.13).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/allowlist"
	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/audit"
	"github.com/Lazarus-AI-Research/sovereign-stack/docker-proxy/internal/dockerapi"
)

type Server struct {
	Allowlist *allowlist.Config
	Audit     *audit.Logger
	Docker    *dockerapi.Client
	Token     string
	Version   string
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func errorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) audited(op string, handler func(w http.ResponseWriter, r *http.Request) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry := audit.Entry{Method: r.Method, Path: r.URL.Path}
		if s.Token == "" || r.Header.Get("Authorization") != "Bearer "+s.Token {
			entry.Decision, entry.Reason = "deny", "invalid or missing token"
			s.Audit.Log(entry)
			errorJSON(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		if decision := s.Allowlist.OperationAllowed(op); !decision.Allowed {
			entry.Decision, entry.Reason = "deny", decision.Reason
			s.Audit.Log(entry)
			errorJSON(w, http.StatusForbidden, decision.Reason)
			return
		}
		detail, err := handler(w, r)
		entry.Detail = detail
		if err != nil {
			entry.Decision, entry.Reason = "deny", err.Error()
		} else {
			entry.Decision = "allow"
		}
		s.Audit.Log(entry)
	}
}

func (s *Server) requireService(w http.ResponseWriter, r *http.Request) (string, bool) {
	service := r.PathValue("service")
	if decision := s.Allowlist.ServiceAllowed(service); !decision.Allowed {
		errorJSON(w, http.StatusForbidden, decision.Reason)
		return service, false
	}
	return service, true
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /internal/docker/status", s.audited("inspect", func(w http.ResponseWriter, r *http.Request) (string, error) {
		if err := s.Docker.Ping(r.Context()); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return "", err
		}
		version, _ := s.Docker.Version(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": s.Version,
			"docker":  version["Version"],
			"project": s.Allowlist.AllowedProject,
		})
		return "", nil
	}))

	mux.HandleFunc("GET /internal/docker/containers", s.audited("list", func(w http.ResponseWriter, r *http.Request) (string, error) {
		containers, err := s.Docker.ListProject(r.Context(), s.Allowlist.AllowedProject)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return "", err
		}
		writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
		return fmt.Sprintf("%d containers", len(containers)), nil
	}))

	mux.HandleFunc("GET /internal/docker/containers/{service}", s.audited("inspect", func(w http.ResponseWriter, r *http.Request) (string, error) {
		service, ok := s.requireService(w, r)
		if !ok {
			return service, fmt.Errorf("service %q denied", service)
		}
		container, err := s.Docker.FindService(r.Context(), s.Allowlist.AllowedProject, service)
		if err != nil {
			errorJSON(w, http.StatusNotFound, err.Error())
			return service, err
		}
		inspection, err := s.Docker.Inspect(r.Context(), container.ID)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return service, err
		}
		writeJSON(w, http.StatusOK, inspection)
		return service, nil
	}))

	mux.HandleFunc("POST /internal/docker/containers/{service}/restart", s.audited("restart", func(w http.ResponseWriter, r *http.Request) (string, error) {
		service, ok := s.requireService(w, r)
		if !ok {
			return service, fmt.Errorf("service %q denied", service)
		}
		container, err := s.Docker.FindService(r.Context(), s.Allowlist.AllowedProject, service)
		if err != nil {
			errorJSON(w, http.StatusNotFound, err.Error())
			return service, err
		}
		if err := s.Docker.Restart(r.Context(), container.ID); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return service, err
		}
		writeJSON(w, http.StatusOK, map[string]string{"restarted": service})
		return service, nil
	}))

	mux.HandleFunc("GET /internal/docker/containers/{service}/logs", s.audited("logs", func(w http.ResponseWriter, r *http.Request) (string, error) {
		service, ok := s.requireService(w, r)
		if !ok {
			return service, fmt.Errorf("service %q denied", service)
		}
		tail := 200
		if t := r.URL.Query().Get("tail"); t != "" {
			if parsed, err := strconv.Atoi(t); err == nil && parsed > 0 && parsed <= 10000 {
				tail = parsed
			}
		}
		container, err := s.Docker.FindService(r.Context(), s.Allowlist.AllowedProject, service)
		if err != nil {
			errorJSON(w, http.StatusNotFound, err.Error())
			return service, err
		}
		logs, err := s.Docker.Logs(r.Context(), container.ID, tail)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return service, err
		}
		writeJSON(w, http.StatusOK, map[string]string{"service": service, "logs": logs})
		return service, nil
	}))

	mux.HandleFunc("POST /internal/docker/images/pull", s.audited("pull", func(w http.ResponseWriter, r *http.Request) (string, error) {
		var body struct {
			Image string `json:"image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Image == "" {
			errorJSON(w, http.StatusBadRequest, "body must be {\"image\": \"<ref>\"}")
			return "", fmt.Errorf("bad request")
		}
		if decision := s.Allowlist.ImageAllowed(body.Image); !decision.Allowed {
			errorJSON(w, http.StatusForbidden, decision.Reason)
			return body.Image, fmt.Errorf("%s", decision.Reason)
		}
		if err := s.Docker.PullImage(r.Context(), body.Image); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return body.Image, err
		}
		writeJSON(w, http.StatusOK, map[string]string{"pulled": body.Image})
		return body.Image, nil
	}))

	mux.HandleFunc("POST /internal/docker/evals/run", s.audited("run-evals", func(w http.ResponseWriter, r *http.Request) (string, error) {
		var body struct {
			Image string `json:"image"`
			Suite string `json:"suite"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Image == "" || body.Suite == "" {
			errorJSON(w, http.StatusBadRequest, "body must be {\"image\": \"<ref>\", \"suite\": \"<name>\"}")
			return "", fmt.Errorf("bad request")
		}
		if decision := s.Allowlist.EvalsImageAllowed(body.Image); !decision.Allowed {
			errorJSON(w, http.StatusForbidden, decision.Reason)
			return body.Image, fmt.Errorf("%s", decision.Reason)
		}
		// Environment passthrough is limited to configured keys; values come
		// from the proxy's own environment, never from the request.
		var env []string
		for _, key := range s.Allowlist.Evals.EnvKeys {
			if value := os.Getenv(key); value != "" {
				env = append(env, key+"="+value)
			}
		}
		id, err := s.Docker.RunJob(
			r.Context(),
			body.Image,
			s.Allowlist.Evals.Network,
			s.Allowlist.Evals.Binds,
			env,
			[]string{"suite", body.Suite},
		)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return body.Suite, err
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"container_id": id, "suite": body.Suite})
		return fmt.Sprintf("suite=%s id=%s", body.Suite, id), nil
	}))

	return mux
}

func Run() error {
	token := os.Getenv("DOCKER_PROXY_TOKEN")
	if token == "" {
		return fmt.Errorf("DOCKER_PROXY_TOKEN is required (the proxy fails closed)")
	}
	allowlistPath := os.Getenv("ALLOWLIST_PATH")
	if allowlistPath == "" {
		allowlistPath = "/etc/sovereign/allowlist.yaml"
	}
	config, err := allowlist.Load(allowlistPath)
	if err != nil {
		return err
	}
	auditPath := os.Getenv("AUDIT_LOG_PATH")
	if auditPath == "" {
		auditPath = "/audit/docker-actions.jsonl"
	}
	auditLog, err := audit.Open(auditPath)
	if err != nil {
		return err
	}
	socket := os.Getenv("DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	addr := os.Getenv("SOVEREIGN_DOCKER_PROXY_LISTEN")
	if addr == "" {
		addr = ":8081"
	}

	server := &Server{
		Allowlist: config,
		Audit:     auditLog,
		Docker:    dockerapi.New(socket),
		Token:     token,
		Version:   os.Getenv("SOVEREIGN_VERSION"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Docker.Ping(ctx); err != nil {
		log.Printf("warning: docker not reachable at startup: %v", err)
	}

	log.Printf("sovereign-docker-proxy listening on %s (project=%s)", addr, config.AllowedProject)
	return http.ListenAndServe(addr, server.Handler())
}
