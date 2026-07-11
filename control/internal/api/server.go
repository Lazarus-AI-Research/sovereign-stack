// Package api implements the Sovereign Control API (design.md §18).
// Base path: /api/control/v1. Slice 1 covers §18.1–18.3; models, embedding
// profiles, indexes, gateway admin, evals, backups, and bundles follow.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hardware"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
)

const BasePath = "/api/control/v1"

// KnownProfiles mirrors the runtime-config schema's profile enum (minus mock).
var KnownProfiles = []string{
	"cuda-x86_64",
	"cuda-arm64-dgx-spark",
	"rocm-x86_64",
	"rocm-strix-halo",
	"xpu-x86_64",
	"metal-arm64",
	"cpu-x86_64",
	"cpu-arm64",
}

type Server struct {
	Runtime *runtime.Client
	Proxy   *dockerproxy.Client
	Gateway *gateway.Client
	Version string
	// Auth enables session authentication when non-nil. It is nil only
	// before the database is reachable and in unit tests.
	Auth *auth.Service
	// UI serves the embedded web frontend at /. Optional.
	UI http.Handler
}

const sessionCookie = "sovereign_session"

func sessionToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}

// open paths never require a session.
func openPath(path string) bool {
	return path == BasePath+"/health" || path == BasePath+"/auth/login"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func errorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	p := func(path string) string { return BasePath + path }

	// ── auth ─────────────────────────────────────────────────────────────

	mux.HandleFunc("POST "+p("/auth/login"), func(w http.ResponseWriter, r *http.Request) {
		if s.Auth == nil {
			errorJSON(w, http.StatusServiceUnavailable, "authentication not ready")
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "username and password are required")
			return
		}
		token, err := s.Auth.Login(r.Context(), body.Username, body.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			errorJSON(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(auth.SessionTTL.Seconds()),
		})
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	})

	mux.HandleFunc("POST "+p("/auth/logout"), func(w http.ResponseWriter, r *http.Request) {
		if s.Auth != nil {
			if token := sessionToken(r); token != "" {
				s.Auth.Logout(r.Context(), token)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	})

	mux.HandleFunc("GET "+p("/auth/me"), func(w http.ResponseWriter, r *http.Request) {
		if s.Auth == nil {
			errorJSON(w, http.StatusServiceUnavailable, "authentication not ready")
			return
		}
		username, err := s.Auth.Validate(r.Context(), sessionToken(r))
		if err != nil {
			errorJSON(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"username": username})
	})

	// ── §18.1 system ─────────────────────────────────────────────────────

	mux.HandleFunc("GET "+p("/health"), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
	})

	mux.HandleFunc("GET "+p("/version"), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
	})

	mux.HandleFunc("GET "+p("/status"), func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		status := map[string]any{"control": map[string]any{"status": "ok", "version": s.Version}}

		runtimeStatus := map[string]any{"reachable": false}
		if live, err := s.Runtime.Live(ctx); err == nil {
			runtimeStatus["reachable"] = true
			runtimeStatus["state"] = live.State
			if ready, err := s.Runtime.Ready(ctx); err == nil {
				runtimeStatus["ready"] = ready.Ready
				runtimeStatus["required_roles"] = ready.RequiredRoles
			}
		}
		status["runtime"] = runtimeStatus

		status["gateway"] = map[string]any{"healthy": s.Gateway.Healthy(ctx)}

		proxyStatus := map[string]any{"reachable": false}
		if info, err := s.Proxy.Status(ctx); err == nil {
			proxyStatus["reachable"] = true
			proxyStatus["docker"] = info["docker"]
		}
		status["docker_proxy"] = proxyStatus

		if containers, err := s.Proxy.Containers(ctx); err == nil {
			services := map[string]string{}
			for _, container := range containers {
				service := container.Labels["com.docker.compose.service"]
				if service == "" && len(container.Names) > 0 {
					service = container.Names[0]
				}
				services[service] = container.State
			}
			status["services"] = services
		}

		writeJSON(w, http.StatusOK, status)
	})

	// ── §18.2 hardware ───────────────────────────────────────────────────

	detect := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"detections": hardware.Detect()})
	}
	mux.HandleFunc("POST "+p("/hardware/detect"), detect)
	mux.HandleFunc("GET "+p("/hardware"), detect)

	mux.HandleFunc("GET "+p("/profiles"), func(w http.ResponseWriter, r *http.Request) {
		recommended := map[string]bool{}
		for _, detection := range hardware.Detect() {
			recommended[detection.Profile] = true
		}
		profiles := make([]map[string]any, 0, len(KnownProfiles))
		for _, profile := range KnownProfiles {
			profiles = append(profiles, map[string]any{
				"profile":     profile,
				"recommended": recommended[profile],
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	})

	// ── §18.3 runtime ────────────────────────────────────────────────────

	mux.HandleFunc("GET "+p("/runtime/status"), func(w http.ResponseWriter, r *http.Request) {
		health, err := s.Runtime.Health(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, "runtime unreachable: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, health)
	})

	mux.HandleFunc("GET "+p("/runtime/manifest"), func(w http.ResponseWriter, r *http.Request) {
		manifest, err := s.Runtime.Manifest(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, "runtime unreachable: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, manifest)
	})

	mux.HandleFunc("GET "+p("/runtime/errors"), func(w http.ResponseWriter, r *http.Request) {
		runtimeErrors, err := s.Runtime.Errors(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, "runtime unreachable: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, runtimeErrors)
	})

	mux.HandleFunc("POST "+p("/runtime/restart"), func(w http.ResponseWriter, r *http.Request) {
		if err := s.Proxy.Restart(r.Context(), "sovereign-runtime"); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"restarting": "sovereign-runtime"})
	})

	if s.UI != nil {
		mux.Handle("GET /", s.UI)
	}

	// Session middleware: everything under the base path except open paths
	// requires a valid session once Auth is wired.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth != nil && strings.HasPrefix(r.URL.Path, BasePath) && !openPath(r.URL.Path) {
			if _, err := s.Auth.Validate(r.Context(), sessionToken(r)); err != nil {
				errorJSON(w, http.StatusUnauthorized, "not authenticated")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
