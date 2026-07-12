// Package api implements the Sovereign Control API (design.md §18).
// Base path: /api/control/v1. Slice 1 covers §18.1–18.3; models, embedding
// profiles, indexes, gateway admin, evals, backups, and bundles follow.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/branding"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hardware"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/indexes"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
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
	// Models and Jobs enable §18.5 endpoints when non-nil.
	Models *models.Registry
	Jobs   *jobs.Runner
	// Reports is the evals report directory (§18.10); empty disables.
	Reports string
	// Profiles enables §18.6; Indexes enables §18.7.
	Profiles *embeddings.Registry
	Indexes  *indexes.Store
	// GatewayConfigPath enables gateway config regeneration (§18.8).
	GatewayConfigPath string
	// Branding and Features serve the file-backed product configuration.
	Branding *branding.Store
	Features *branding.Store
}

// RebuildRequiredWarning is shown whenever embedding configuration changes
// (design.md §11.2).
const RebuildRequiredWarning = "Changing the embedding model requires rebuilding affected indexes. Existing vectors will remain available until the new index is complete."

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

	// ── §18.5 models ─────────────────────────────────────────────────────

	if s.Models != nil {
		mux.HandleFunc("GET "+p("/models"), func(w http.ResponseWriter, r *http.Request) {
			entries, err := s.Models.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			// annotate with what the runtime actually has loaded
			loaded := map[string]string{}
			if manifest, err := s.Runtime.Manifest(r.Context()); err == nil {
				if roles, ok := manifest["roles"].(map[string]any); ok {
					for roleName, roleValue := range roles {
						if role, ok := roleValue.(map[string]any); ok {
							if engineModel, ok := role["engine_model"].(string); ok {
								loaded[roleName] = engineModel
							}
						}
					}
				}
			}
			out := make([]map[string]any, 0, len(entries))
			for _, entry := range entries {
				out = append(out, map[string]any{
					"id":       entry.ID,
					"role":     entry.Role,
					"source":   entry.Source,
					"model":    entry.Model,
					"revision": entry.Revision,
					"loaded":   loaded[entry.Role] == entry.Model,
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{"models": out})
		})

		addModel := func(w http.ResponseWriter, r *http.Request) {
			var entry models.Entry
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid model entry")
				return
			}
			if err := s.Models.Add(entry); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, entry)
		}
		mux.HandleFunc("POST "+p("/models/local"), addModel)
		mux.HandleFunc("POST "+p("/models/remote"), addModel)

		mux.HandleFunc("PATCH "+p("/models/{id}"), func(w http.ResponseWriter, r *http.Request) {
			var patch map[string]string
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid patch")
				return
			}
			entry, err := s.Models.Update(r.PathValue("id"), patch)
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, entry)
		})

		mux.HandleFunc("DELETE "+p("/models/{id}"), func(w http.ResponseWriter, r *http.Request) {
			if err := s.Models.Delete(r.PathValue("id")); err != nil {
				errorJSON(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
		})

		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/models/{id}/load"), func(w http.ResponseWriter, r *http.Request) {
				modelID := r.PathValue("id")
				if _, err := s.Models.Get(modelID); err != nil {
					errorJSON(w, http.StatusNotFound, err.Error())
					return
				}
				// §2.9: warn, never block.
				jobID, err := s.Jobs.Enqueue(r.Context(), "model-load", models.LoadPayload{ModelID: modelID})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{
					"job_id":  jobID,
					"warning": "This model has not been validated on the selected runtime profile. Loading may fail, consume excessive memory, or perform poorly. Run a smoke test after loading.",
				})
			})
		}
	}

	// ── jobs ─────────────────────────────────────────────────────────────

	if s.Jobs != nil {
		mux.HandleFunc("GET "+p("/jobs/{id}"), func(w http.ResponseWriter, r *http.Request) {
			job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "job not found")
				return
			}
			writeJSON(w, http.StatusOK, job)
		})
	}

	// ── §18.10 evaluations ───────────────────────────────────────────────

	if s.Jobs != nil {
		runSuite := func(suite string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				jobID, err := s.Jobs.Enqueue(r.Context(), "evals-run", map[string]string{"suite": suite})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "suite": suite})
			}
		}
		mux.HandleFunc("POST "+p("/evals/smoke"), runSuite("smoke"))
		mux.HandleFunc("POST "+p("/evals/benchmark/quick"), runSuite("quick"))
		mux.HandleFunc("POST "+p("/evals/benchmark/full"), runSuite("full"))
		mux.HandleFunc("POST "+p("/evals/retrieval"), runSuite("retrieval"))
	}

	if s.Reports != "" {
		mux.HandleFunc("GET "+p("/evals/results"), func(w http.ResponseWriter, r *http.Request) {
			entries, err := os.ReadDir(s.Reports)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"results": []string{}})
				return
			}
			var names []string
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".json") {
					names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(names)))
			writeJSON(w, http.StatusOK, map[string]any{"results": names})
		})

		mux.HandleFunc("GET "+p("/evals/results/{id}"), func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if strings.ContainsAny(id, "/\\.") {
				errorJSON(w, http.StatusBadRequest, "invalid result id")
				return
			}
			raw, err := os.ReadFile(filepath.Join(s.Reports, id+".json"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "result not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(raw)
		})
	}

	// ── §18.6 embedding profiles ─────────────────────────────────────────

	if s.Profiles != nil {
		mux.HandleFunc("GET "+p("/embedding-profiles"), func(w http.ResponseWriter, r *http.Request) {
			profiles, err := s.Profiles.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"embedding_profiles": profiles})
		})

		mux.HandleFunc("GET "+p("/embedding-profiles/{id}"), func(w http.ResponseWriter, r *http.Request) {
			profile, err := s.Profiles.Get(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, profile)
		})

		putProfile := func(w http.ResponseWriter, r *http.Request, id string) {
			var profile embeddings.Profile
			if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid profile")
				return
			}
			if err := s.Profiles.Put(id, profile); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"profile": profile,
				"warning": RebuildRequiredWarning,
			})
		}
		mux.HandleFunc("POST "+p("/embedding-profiles"), func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ID string `json:"id"`
				embeddings.Profile
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil || body.ID == "" {
				errorJSON(w, http.StatusBadRequest, "profile id is required")
				return
			}
			if err := s.Profiles.Put(body.ID, body.Profile); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"id":      body.ID,
				"profile": body.Profile,
				"warning": RebuildRequiredWarning,
			})
		})
		mux.HandleFunc("PATCH "+p("/embedding-profiles/{id}"), func(w http.ResponseWriter, r *http.Request) {
			putProfile(w, r, r.PathValue("id"))
		})
		mux.HandleFunc("DELETE "+p("/embedding-profiles/{id}"), func(w http.ResponseWriter, r *http.Request) {
			if err := s.Profiles.Delete(r.PathValue("id")); err != nil {
				errorJSON(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
		})

		mux.HandleFunc("POST "+p("/embedding-profiles/{id}/validate"), func(w http.ResponseWriter, r *http.Request) {
			profile, err := s.Profiles.Get(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, err.Error())
				return
			}
			manifest, err := s.Runtime.Manifest(r.Context())
			if err != nil {
				errorJSON(w, http.StatusBadGateway, "runtime unreachable: "+err.Error())
				return
			}
			result := map[string]any{"profile": r.PathValue("id"), "loaded": false}
			if roles, ok := manifest["roles"].(map[string]any); ok {
				if embedding, ok := roles["embedding"].(map[string]any); ok {
					loaded := embedding["engine_model"] == profile.Model &&
						embedding["status"] == "healthy"
					result["loaded"] = loaded
					if dims, ok := embedding["dimensions"].(float64); ok && loaded {
						result["dimensions"] = int(dims)
					}
					if !loaded {
						result["detail"] = "profile model is not the currently loaded embedding model; activate it first"
					}
				}
			}
			writeJSON(w, http.StatusOK, result)
		})

		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/embedding-profiles/{id}/activate"), func(w http.ResponseWriter, r *http.Request) {
				if _, err := s.Profiles.Get(r.PathValue("id")); err != nil {
					errorJSON(w, http.StatusNotFound, err.Error())
					return
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "profile-activate",
					embeddings.ActivatePayload{ProfileID: r.PathValue("id")})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{
					"job_id":  jobID,
					"warning": RebuildRequiredWarning,
				})
			})
		}
	}

	// ── §18.7 vector indexes ─────────────────────────────────────────────

	if s.Indexes != nil {
		mux.HandleFunc("GET "+p("/indexes"), func(w http.ResponseWriter, r *http.Request) {
			versions, err := s.Indexes.List(r.Context())
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"indexes": versions})
		})

		mux.HandleFunc("POST "+p("/indexes"), func(w http.ResponseWriter, r *http.Request) {
			var request indexes.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid index request")
				return
			}
			version, err := s.Indexes.Create(r.Context(), request)
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, version)
		})

		mux.HandleFunc("GET "+p("/indexes/{id}"), func(w http.ResponseWriter, r *http.Request) {
			version, err := s.Indexes.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "index version not found")
				return
			}
			writeJSON(w, http.StatusOK, version)
		})

		mux.HandleFunc("POST "+p("/indexes/{id}/activate"), func(w http.ResponseWriter, r *http.Request) {
			version, err := s.Indexes.Activate(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, version)
		})

		mux.HandleFunc("DELETE "+p("/indexes/{id}"), func(w http.ResponseWriter, r *http.Request) {
			if err := s.Indexes.Delete(r.Context(), r.PathValue("id")); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
		})

		mux.HandleFunc("POST "+p("/indexes/{id}/rebuild"), func(w http.ResponseWriter, r *http.Request) {
			// The re-embedding pipeline requires the real embedding runtime
			// and the workspace's chunk store; lands with the remainder of
			// the index-lifecycle milestone.
			errorJSON(w, http.StatusNotImplemented,
				"index rebuild is not implemented yet; create a new index version and re-ingest, then activate it")
		})
	}

	// ── branding + feature flags (§26 product capabilities) ─────────────

	if s.Branding != nil {
		mux.HandleFunc("GET "+p("/branding"), func(w http.ResponseWriter, r *http.Request) {
			doc, err := s.Branding.Get()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, doc)
		})
		mux.HandleFunc("PUT "+p("/branding"), func(w http.ResponseWriter, r *http.Request) {
			var doc map[string]any
			if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid branding document")
				return
			}
			if err := s.Branding.Put(doc); err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, doc)
		})
	}

	if s.Features != nil {
		mux.HandleFunc("GET "+p("/features"), func(w http.ResponseWriter, r *http.Request) {
			doc, err := s.Features.Get()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, doc)
		})
		mux.HandleFunc("PUT "+p("/features"), func(w http.ResponseWriter, r *http.Request) {
			var doc map[string]any
			if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid feature flags document")
				return
			}
			if err := branding.ValidateFlags(doc); err != nil {
				errorJSON(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			if err := s.Features.Put(doc); err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, doc)
		})
	}

	// ── §18.8 gateway ────────────────────────────────────────────────────

	mux.HandleFunc("GET "+p("/gateway/status"), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"healthy": s.Gateway.Healthy(r.Context())})
	})

	mux.HandleFunc("GET "+p("/gateway/models"), func(w http.ResponseWriter, r *http.Request) {
		gatewayModels, err := s.Gateway.Models(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": gatewayModels})
	})

	mux.HandleFunc("GET "+p("/gateway/keys"), func(w http.ResponseWriter, r *http.Request) {
		keys, err := s.Gateway.ListKeys(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, keys)
	})

	mux.HandleFunc("POST "+p("/gateway/keys"), func(w http.ResponseWriter, r *http.Request) {
		var request gateway.KeyRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid key request")
			return
		}
		key, err := s.Gateway.GenerateKey(r.Context(), request)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, key)
	})

	if s.GatewayConfigPath != "" && s.Profiles != nil && s.Models != nil {
		mux.HandleFunc("POST "+p("/gateway/config/regenerate"), func(w http.ResponseWriter, r *http.Request) {
			if err := gateway.GenerateConfig(s.GatewayConfigPath, s.Models, s.Profiles); err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{
				"generated": s.GatewayConfigPath,
				"note":      "POST /gateway/reload to apply",
			})
		})
	}

	mux.HandleFunc("POST "+p("/gateway/reload"), func(w http.ResponseWriter, r *http.Request) {
		if err := s.Proxy.Restart(r.Context(), "sovereign-gateway"); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"restarting": "sovereign-gateway"})
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
