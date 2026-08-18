// Package api implements the Sovereign Control API (design.md §18).
// Base path: /api/control/v1. Slice 1 covers §18.1–18.3; models, embedding
// profiles, indexes, gateway admin, evals, backups, and bundles follow.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/backups"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/branding"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/bundles"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/credentials"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/embeddings"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/gateway"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hardware"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/hostagent"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/indexes"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/models"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/runtime"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/support"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/updates"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/workspace"
	"github.com/jackc/pgx/v5"
)

const BasePath = "/api/control/v1"

// KnownProfiles mirrors the runtime-config schema's profile enum (minus mock).
var KnownProfiles = []string{
	"cuda-x86_64",
	"cuda-arm64-dgx-spark",
	"rocm-x86_64",
	"rocm-strix-halo",
	"xpu-x86_64",
	"gaudi-x86_64",
	"metal-arm64",
	"cpu-x86_64",
	"cpu-arm64",
}

type Server struct {
	Runtime    *runtime.Client
	Embeddings *embeddings.Client
	Proxy      *dockerproxy.Client
	Gateway    *gateway.Client
	Version    string
	// Auth enables session authentication when non-nil. It is nil only
	// before the database is reachable and in unit tests.
	Auth *auth.Service
	// OperatorToken authorizes the owner-only host CLI without creating a
	// browser session. It is never accepted from query parameters or cookies.
	OperatorToken string
	ClaimFile     string
	// UI serves the embedded web frontend at /. Optional.
	UI http.Handler
	// Models and Jobs enable §18.5 endpoints when non-nil.
	Models *models.Registry
	Jobs   *jobs.Runner
	// Credentials is the encrypted provider-secret vault. API responses expose
	// metadata only; gateway regeneration is the sole decryption consumer.
	Credentials *credentials.Store
	// Reports is the evals report directory (§18.10); empty disables.
	Reports string
	// Profiles enables §18.6; Indexes enables §18.7.
	Profiles           *embeddings.Registry
	Indexes            *indexes.Store
	EmbeddingActivator *embeddings.ActivateDeps
	// GatewayConfigPath enables gateway config regeneration (§18.8).
	GatewayConfigPath string
	// Branding and Features serve the file-backed product configuration.
	Branding *branding.Store
	Features *branding.Store
	// Backups enables §18.11.
	Backups *backups.Deps
	// Bundles enables same-platform offline bundle creation and download.
	Bundles *bundles.Deps
	// Support creates downloadable, secret-redacted diagnostic bundles.
	Support *support.Deps
	Updates *updates.Checker
	// Workspace enables §18.9 (branding apply, status).
	Workspace *workspace.Client
	// HostLifecycle is the optional, narrowly allowlisted host management API.
	HostLifecycle *hostagent.LifecycleClient
	// SovereignRoot is the deploy mount (branding asset paths root here).
	SovereignRoot string
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

// open paths never require a session. /theme is public so the login page can
// render the customer's branding before anyone signs in.
func openPath(path string) bool {
	return path == BasePath+"/health" ||
		path == BasePath+"/auth/login" ||
		path == BasePath+"/theme" ||
		strings.HasPrefix(path, BasePath+"/auth/claim/") ||
		strings.HasPrefix(path, BasePath+"/auth/invitations/")
}

type identityContextKey struct{}

func requestIdentity(r *http.Request) (auth.Identity, bool) {
	identity, ok := r.Context().Value(identityContextKey{}).(auth.Identity)
	return identity, ok
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(auth.SessionTTL.Seconds()),
	})
}

func adminOnlyPath(path string) bool {
	for _, prefix := range []string{
		BasePath + "/auth/setup-claim",
		BasePath + "/users", BasePath + "/invitations", BasePath + "/provider-credentials",
		BasePath + "/backups", BasePath + "/bundles", BasePath + "/branding",
		BasePath + "/support-bundles", BasePath + "/updates", BasePath + "/features", BasePath + "/runtime/restart", BasePath + "/network", BasePath + "/repair",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func memberPath(path string) bool {
	for _, prefix := range []string{
		BasePath + "/auth/", BasePath + "/status", BasePath + "/theme",
		BasePath + "/readiness", BasePath + "/applications",
		BasePath + "/workspace/sso", BasePath + "/workspaces",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func workspaceLoginRedirect(loginPath, role string, slugs []string) (string, error) {
	target, err := url.Parse(loginPath)
	if err != nil || target.IsAbs() || target.Host != "" || target.Path != "/sso/simple" || target.Query().Get("token") == "" {
		return "", fmt.Errorf("workspace identity session returned an invalid login path")
	}
	redirectTo := "/settings/workspaces"
	if len(slugs) > 0 {
		redirectTo = "/workspace/" + url.PathEscape(slugs[0])
	} else if role != "admin" {
		return "", fmt.Errorf("no chat workspace is available for this account")
	}
	query := target.Query()
	query.Set("redirectTo", redirectTo)
	target.RawQuery = query.Encode()
	return target.String(), nil
}

// themeFields is the cosmetic subset of the branding document that is safe to
// serve unauthenticated: everything here is already visible on the login page.
// Serving an allowlist rather than the whole document means a field added to
// branding.yaml later cannot leak by accident.
var themeFields = []string{"product_name", "company_name", "logo", "logo_animated", "favicon", "colors"}

func publicTheme(doc map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range themeFields {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func errorJSON(w http.ResponseWriter, status int, message string) {
	code, action := "operation_failed", "Review the details and try again."
	switch status {
	case http.StatusBadRequest:
		code, action = "invalid_request", "Check the highlighted values and try again."
	case http.StatusUnauthorized:
		code, action = "not_authenticated", "Sign in and try again."
	case http.StatusForbidden:
		code, action = "not_authorized", "Ask an appliance administrator for access."
	case http.StatusNotFound:
		code, action = "not_found", "Refresh the page; the item may have been removed."
	case http.StatusConflict:
		code, action = "conflict", "Refresh the current state and try again."
	case http.StatusUnprocessableEntity:
		code, action = "incompatible_configuration", "Choose a compatible configuration or open Advanced settings."
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		code, action = "dependency_unavailable", "Open System status, then retry when the service is ready."
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		code, action = "operation_timed_out", "Retry the operation. If it repeats, download diagnostics from System."
	}
	writeJSON(w, status, map[string]string{"error": message, "code": code, "action": action})
}

func redactMemberJob(job *jobs.Job) {
	job.Payload = nil
	job.Result = nil
	if job.Error != nil {
		message := "Operation needs attention"
		job.Error = nil
		job.Message = &message
	}
}

func catalogCompatibility(item models.CatalogItem, inventory hardware.Inventory) (bool, string) {
	if !slices.Contains(item.CompatibleProfiles, inventory.Profile) {
		return false, "Requires a supported " + strings.Join(item.CompatibleProfiles, " or ") + " hardware profile."
	}
	if item.MinimumMemoryBytes > 0 && inventory.MemoryBytes > 0 && inventory.MemoryBytes < item.MinimumMemoryBytes {
		return false, fmt.Sprintf("Requires at least %.0f GB of system memory.", float64(item.MinimumMemoryBytes)/(1024*1024*1024))
	}
	if item.MinimumVRAMBytes > 0 && inventory.GPU != nil && inventory.GPU.VRAMBytes > 0 && inventory.GPU.VRAMBytes < item.MinimumVRAMBytes {
		return false, fmt.Sprintf("Requires at least %.0f GB of GPU memory.", float64(item.MinimumVRAMBytes)/(1024*1024*1024))
	}
	// Keep enough headroom for verification, extraction, and rollback. Unknown
	// capacity is not rejected because older installations do not report it.
	requiredDisk := item.DownloadBytes + max(item.DownloadBytes/5, int64(2*1024*1024*1024))
	if inventory.StorageFreeBytes > 0 && inventory.StorageFreeBytes < requiredDisk {
		return false, fmt.Sprintf("Free at least %.1f GB of disk space before installing.", float64(requiredDisk)/(1024*1024*1024))
	}
	return true, "Compatible with this appliance."
}

func metricValue(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

// metrics exposes non-sensitive service health for Sovereign Observe. LiteLLM
// protects its own /metrics endpoint with the master key in the supported
// image, so Control performs the authenticated health probes and exports the
// resulting gauges without placing credentials in Prometheus configuration.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	runtimeUp := false
	if s.Runtime != nil {
		_, err := s.Runtime.Live(ctx)
		runtimeUp = err == nil
	}
	gatewayUp := s.Gateway != nil && s.Gateway.Healthy(ctx)
	embeddingsUp := s.Embeddings != nil && s.Embeddings.Healthy(ctx)
	proxyUp := false
	if s.Proxy != nil {
		_, err := s.Proxy.Status(ctx)
		proxyUp = err == nil
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, `# HELP sovereign_control_up Whether Sovereign Control is serving requests.
# TYPE sovereign_control_up gauge
sovereign_control_up 1
# HELP sovereign_control_runtime_up Whether Control can reach Sovereign Runtime.
# TYPE sovereign_control_runtime_up gauge
sovereign_control_runtime_up %d
# HELP sovereign_control_gateway_up Whether Control can reach the LiteLLM gateway.
# TYPE sovereign_control_gateway_up gauge
sovereign_control_gateway_up %d
# HELP sovereign_control_embeddings_up Whether Control can reach embeddinggemma.
# TYPE sovereign_control_embeddings_up gauge
sovereign_control_embeddings_up %d
# HELP sovereign_control_docker_proxy_up Whether Control can reach the restricted Docker proxy.
# TYPE sovereign_control_docker_proxy_up gauge
sovereign_control_docker_proxy_up %d
`, metricValue(runtimeUp), metricValue(gatewayUp), metricValue(embeddingsUp), metricValue(proxyUp))
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
		setSessionCookie(w, r, token)
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	})

	mux.HandleFunc("POST "+p("/auth/claim/{token}"), func(w http.ResponseWriter, r *http.Request) {
		if s.Auth == nil {
			errorJSON(w, http.StatusServiceUnavailable, "authentication not ready")
			return
		}
		var body struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "username and password are required")
			return
		}
		identity, err := s.Auth.Claim(r.Context(), r.PathValue("token"), body.Username, body.DisplayName, body.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			errorJSON(w, http.StatusGone, "setup link is invalid, expired, or already used")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		token, err := s.Auth.IssueSession(r.Context(), identity.ID)
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		setSessionCookie(w, r, token)
		if s.ClaimFile != "" {
			_ = os.Remove(s.ClaimFile)
		}
		writeJSON(w, http.StatusCreated, identity)
	})

	// The owner-only host CLI uses this to replace an expired first-admin
	// claim. It is protected by the operator token middleware and is inert once
	// any user exists.
	mux.HandleFunc("POST "+p("/auth/setup-claim"), func(w http.ResponseWriter, r *http.Request) {
		if s.Auth == nil {
			errorJSON(w, http.StatusServiceUnavailable, "authentication not ready")
			return
		}
		token, expires, err := s.Auth.EnsureSetupClaim(r.Context())
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		if token == "" {
			errorJSON(w, http.StatusConflict, "the first administrator has already been created")
			return
		}
		if s.ClaimFile != "" {
			if err := os.MkdirAll(filepath.Dir(s.ClaimFile), 0o700); err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := os.WriteFile(s.ClaimFile, []byte(token+"\n"), 0o600); err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token": token, "path": "/claim/" + token, "expires_at": expires,
		})
	})

	mux.HandleFunc("GET "+p("/auth/invitations/{token}"), func(w http.ResponseWriter, r *http.Request) {
		invite, err := s.Auth.InvitationInfo(r.Context(), r.PathValue("token"))
		if err != nil {
			errorJSON(w, http.StatusGone, "invitation is invalid, expired, or already used")
			return
		}
		writeJSON(w, http.StatusOK, invite)
	})

	mux.HandleFunc("POST "+p("/auth/invitations/{token}"), func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "username and password are required")
			return
		}
		identity, err := s.Auth.AcceptInvitation(r.Context(), r.PathValue("token"), body.Username, body.DisplayName, body.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			errorJSON(w, http.StatusGone, "invitation is invalid, expired, or already used")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		token, err := s.Auth.IssueSession(r.Context(), identity.ID)
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		setSessionCookie(w, r, token)
		writeJSON(w, http.StatusCreated, identity)
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
		identity, err := s.Auth.ValidateIdentity(r.Context(), sessionToken(r))
		if err != nil {
			errorJSON(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		writeJSON(w, http.StatusOK, identity)
	})

	forwardAuth := func(minimumRole string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			identity, ok := requestIdentity(r)
			if !ok {
				errorJSON(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			if minimumRole == "manager" && identity.Role == "member" {
				errorJSON(w, http.StatusForbidden, "manager role required")
				return
			}
			w.Header().Set("X-Sovereign-User", identity.Username)
			w.Header().Set("X-Sovereign-Role", identity.Role)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("GET "+p("/auth/forward"), forwardAuth("member"))
	mux.HandleFunc("GET "+p("/auth/forward/manager"), forwardAuth("manager"))

	mux.HandleFunc("GET "+p("/users"), func(w http.ResponseWriter, r *http.Request) {
		users, err := s.Auth.ListUsers(r.Context())
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	})

	mux.HandleFunc("PATCH "+p("/users/{id}"), func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid user id")
			return
		}
		var body struct {
			Role         string   `json:"role"`
			Disabled     bool     `json:"disabled"`
			WorkspaceIDs []string `json:"workspace_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid user update")
			return
		}
		user, err := s.Auth.UpdateUser(r.Context(), id, body.Role, body.Disabled, body.WorkspaceIDs)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, user)
	})

	mux.HandleFunc("POST "+p("/invitations"), func(w http.ResponseWriter, r *http.Request) {
		identity, ok := requestIdentity(r)
		if !ok {
			errorJSON(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		var body struct {
			Role         string   `json:"role"`
			WorkspaceIDs []string `json:"workspace_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid invitation")
			return
		}
		invite, token, err := s.Auth.CreateInvitation(r.Context(), identity.ID, body.Role, body.WorkspaceIDs)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"invitation": invite, "token": token, "path": "/invite/" + token,
		})
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
		var runtimeReadiness runtime.Readiness
		if live, err := s.Runtime.Live(ctx); err == nil {
			runtimeStatus["reachable"] = true
			runtimeStatus["state"] = live.State
			if ready, err := s.Runtime.Ready(ctx); err == nil {
				runtimeReadiness = ready
				runtimeStatus["ready"] = ready.Ready
				runtimeStatus["required_roles"] = ready.RequiredRoles
			}
		}
		status["runtime"] = runtimeStatus
		gatewayHealthy := s.Gateway.Healthy(ctx)
		embeddingStatus := map[string]any{
			"reachable": s.Embeddings != nil && s.Embeddings.Healthy(ctx),
			"backend":   "embeddinggemma",
		}
		if s.Indexes != nil {
			if state, err := s.Indexes.EmbeddingState(ctx); err == nil {
				embeddingStatus["backend"] = state.Provider
				embeddingStatus["profile_id"] = state.ProfileID
				embeddingStatus["served_model_name"] = state.ServedModelName
				switch state.Provider {
				case "sovereign-runtime":
					embeddingStatus["reachable"] = runtimeReadiness.RequiredRoles["embedding"]
				case "openai-compatible":
					embeddingStatus["reachable"] = gatewayHealthy
				}
			}
		}
		status["embeddings"] = embeddingStatus

		status["gateway"] = map[string]any{"healthy": gatewayHealthy}

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

	// Readiness is the stable, user-facing view of appliance startup. Unlike
	// /status it describes independent product capabilities so optional tools
	// never block Chat or make the whole portal look unavailable.
	mux.HandleFunc("GET "+p("/readiness"), func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		type component struct {
			State           string  `json:"state"`
			Message         string  `json:"message"`
			JobID           string  `json:"job_id,omitempty"`
			Action          string  `json:"action,omitempty"`
			ProgressCurrent int64   `json:"progress_current,omitempty"`
			ProgressTotal   int64   `json:"progress_total,omitempty"`
			ProgressUnit    string  `json:"progress_unit,omitempty"`
			RateBytes       float64 `json:"rate_bytes,omitempty"`
			ETASeconds      int64   `json:"eta_seconds,omitempty"`
		}
		components := map[string]component{
			"portal": {State: "ready", Message: "Control portal is ready"},
		}
		authentication := component{State: "starting", Message: "Authentication is starting"}
		if s.Auth != nil {
			authentication = component{State: "ready", Message: "Authentication is ready"}
		}
		generation := component{State: "starting", Message: "Generation runtime is starting"}
		if s.Runtime != nil {
			if ready, err := s.Runtime.Ready(ctx); err == nil && ready.Ready {
				generation = component{State: "ready", Message: "Generation model is ready"}
			} else if err == nil && ready.State != "" {
				generation.State, generation.Message = ready.State, "Generation runtime is "+strings.ReplaceAll(ready.State, "_", " ")
			}
		}
		embedding := component{State: "starting", Message: "EmbeddingGemma is starting"}
		if s.Embeddings != nil && s.Embeddings.Healthy(ctx) {
			embedding = component{State: "ready", Message: "EmbeddingGemma is ready"}
		}
		gatewayComponent := component{State: "starting", Message: "API gateway is starting"}
		if s.Gateway != nil && s.Gateway.Healthy(ctx) {
			gatewayComponent = component{State: "ready", Message: "API gateway is ready"}
		}
		workspaceComponent := component{State: "starting", Message: "Chat workspace is starting"}
		if s.Workspace != nil && s.Workspace.Reachable(ctx) {
			workspaceComponent = component{State: "ready", Message: "Chat workspace is ready"}
		}
		observability := component{State: "starting", Message: "Optional observability tools are starting"}
		if s.Proxy != nil {
			if containers, err := s.Proxy.Containers(ctx); err == nil {
				states := map[string]string{}
				for _, container := range containers {
					states[container.Labels["com.docker.compose.service"]] = container.State
				}
				if states["grafana"] == "running" && states["phoenix"] == "running" {
					observability = component{State: "ready", Message: "Grafana and Phoenix are ready"}
				} else {
					observability = component{State: "degraded", Message: "Chat is available while optional tools start"}
				}
			}
		}
		if s.Jobs != nil {
			if recent, err := s.Jobs.List(ctx, 25); err == nil {
				for _, job := range recent {
					if job.Status != "queued" && job.Status != "running" {
						continue
					}
					if job.Kind == "model-load" {
						generation.State, generation.JobID = job.Stage, job.ID
						generation.ProgressCurrent = job.ProgressCurrent
						if job.ProgressTotal != nil {
							generation.ProgressTotal = *job.ProgressTotal
						}
						if job.ProgressUnit != nil {
							generation.ProgressUnit = *job.ProgressUnit
						}
						if job.ProgressRate != nil {
							generation.RateBytes = float64(*job.ProgressRate)
						}
						if job.ETASeconds != nil {
							generation.ETASeconds = *job.ETASeconds
						}
						if job.Message != nil {
							generation.Message = *job.Message
						}
					}
					if job.Kind == "profile-activate" {
						embedding.State, embedding.JobID = job.Stage, job.ID
						embedding.ProgressCurrent = job.ProgressCurrent
						if job.ProgressTotal != nil {
							embedding.ProgressTotal = *job.ProgressTotal
						}
						if job.ProgressUnit != nil {
							embedding.ProgressUnit = *job.ProgressUnit
						}
						if job.Message != nil {
							embedding.Message = *job.Message
						}
					}
				}
			}
		}
		var installProgress struct {
			Role        string `json:"role"`
			Stage       string `json:"stage"`
			File        string `json:"file"`
			Current     int64  `json:"current"`
			Total       int64  `json:"total"`
			StartedUnix int64  `json:"started_unix"`
		}
		if raw, err := os.ReadFile(filepath.Join(s.SovereignRoot, "state", "install-progress.json")); err == nil && json.Unmarshal(raw, &installProgress) == nil && installProgress.Stage != "complete" {
			elapsed := time.Now().Unix() - installProgress.StartedUnix
			rate, eta := float64(0), int64(0)
			if elapsed > 0 {
				rate = float64(installProgress.Current) / float64(elapsed)
				if rate > 0 && installProgress.Total > installProgress.Current {
					eta = int64(float64(installProgress.Total-installProgress.Current) / rate)
				}
			}
			target := generation
			if installProgress.Role == "embeddings" {
				target = embedding
			}
			target.State, target.Message = installProgress.Stage, "Downloading "+installProgress.File
			target.ProgressCurrent, target.ProgressTotal, target.ProgressUnit = installProgress.Current, installProgress.Total, "bytes"
			target.RateBytes, target.ETASeconds = rate, eta
			if installProgress.Role == "embeddings" {
				embedding = target
			} else {
				generation = target
			}
		}
		components["generation"] = generation
		components["embeddings"] = embedding
		components["gateway"] = gatewayComponent
		components["workspace"] = workspaceComponent
		components["authentication"] = authentication
		components["observability"] = observability
		overall := "ready"
		for _, name := range []string{"authentication", "generation", "embeddings", "gateway", "workspace"} {
			if components[name].State == "failed" || components[name].State == "degraded" {
				overall = "degraded"
				break
			}
			if components[name].State != "ready" {
				overall = "starting"
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"overall": overall, "components": components})
	})

	// Applications are registered centrally so the portal owns service names,
	// paths, roles, and embed behavior. No arbitrary iframe URLs are accepted.
	mux.HandleFunc("GET "+p("/applications"), func(w http.ResponseWriter, r *http.Request) {
		identity, _ := requestIdentity(r)
		level := map[string]int{"member": 0, "manager": 1, "admin": 2}[identity.Role]
		applications := []map[string]any{
			{"id": "chat", "label": "Chat", "icon": "chat", "path": "/api/control/v1/workspace/sso", "health_component": "workspace", "minimum_role": "member", "embed": true},
			{"id": "grafana", "label": "Grafana", "icon": "metrics", "description": "Metrics and operational dashboards", "path": "/apps/grafana/", "health_component": "observability", "minimum_role": "manager", "embed": true},
			{"id": "phoenix", "label": "Phoenix", "icon": "traces", "description": "AI traces and evaluations", "path": "/apps/phoenix/", "health_component": "observability", "minimum_role": "manager", "embed": true},
			{"id": "api-providers", "label": "API & Providers", "icon": "connections", "description": "Connect providers and issue scoped API keys", "path": "/admin/providers", "health_component": "gateway", "minimum_role": "admin", "embed": false},
		}
		filtered := make([]map[string]any, 0, len(applications))
		for _, app := range applications {
			minimum := app["minimum_role"].(string)
			if level >= map[string]int{"member": 0, "manager": 1, "admin": 2}[minimum] {
				filtered = append(filtered, app)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"applications": filtered})
	})

	// Host mutations use sovereign-hostd's fixed operation allowlist. Upgraded
	// installations without hostd keep a read-only status and CLI fallback.
	mux.HandleFunc("GET "+p("/network"), func(w http.ResponseWriter, r *http.Request) {
		fallback := hostagent.HostStatus{
			Managed: false, AccessMode: os.Getenv("SOVEREIGN_ACCESS_MODE"), PublicURL: os.Getenv("SOVEREIGN_PUBLIC_URL"),
			BindAddress: os.Getenv("SOVEREIGN_BIND_ADDRESS"), SiteAddress: os.Getenv("SOVEREIGN_SITE_ADDRESS"), Version: s.Version,
		}
		if s.HostLifecycle == nil {
			writeJSON(w, http.StatusOK, fallback)
			return
		}
		status, err := s.HostLifecycle.Status(r.Context())
		if err != nil {
			writeJSON(w, http.StatusOK, fallback)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("PUT "+p("/network"), func(w http.ResponseWriter, r *http.Request) {
		if s.HostLifecycle == nil {
			errorJSON(w, http.StatusServiceUnavailable, "the signed host lifecycle service is not installed")
			return
		}
		var request struct {
			Mode   string `json:"mode"`
			Target string `json:"target"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			errorJSON(w, http.StatusBadRequest, "invalid network request")
			return
		}
		if err := s.HostLifecycle.SetNetwork(r.Context(), request.Mode, request.Target); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"updating": true, "mode": request.Mode, "target": request.Target})
	})
	mux.HandleFunc("POST "+p("/repair"), func(w http.ResponseWriter, r *http.Request) {
		if s.HostLifecycle == nil {
			errorJSON(w, http.StatusServiceUnavailable, "the signed host lifecycle service is not installed")
			return
		}
		if err := s.HostLifecycle.Repair(r.Context()); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"repaired": true})
	})
	mux.HandleFunc("GET "+p("/updates"), func(w http.ResponseWriter, r *http.Request) {
		info := updates.Info{CurrentVersion: s.Version, CheckedAt: time.Now().UTC().Format(time.RFC3339), CheckError: "release checking is not configured"}
		if s.Updates != nil {
			info = s.Updates.Check(r.Context(), s.Version)
		}
		state := hostagent.UpdateState{State: "unavailable", Message: "Install the host lifecycle service to apply updates"}
		if s.HostLifecycle != nil {
			if current, err := s.HostLifecycle.UpdateStatus(r.Context()); err == nil {
				state = current
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"release": info, "operation": state})
	})
	mux.HandleFunc("POST "+p("/updates/apply"), func(w http.ResponseWriter, r *http.Request) {
		if s.Updates == nil || s.HostLifecycle == nil {
			errorJSON(w, http.StatusServiceUnavailable, "managed updates are not configured")
			return
		}
		var request struct {
			Version string `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		info := s.Updates.Check(r.Context(), s.Version)
		if info.CheckError != "" {
			errorJSON(w, http.StatusServiceUnavailable, info.CheckError)
			return
		}
		if request.Version == "" {
			request.Version = info.LatestVersion
		}
		if !info.Available || request.Version != info.LatestVersion {
			errorJSON(w, http.StatusConflict, "the requested version is not the current verified release-feed update")
			return
		}
		if err := s.HostLifecycle.ApplyUpdate(r.Context(), request.Version); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"scheduled": true, "version": request.Version})
	})

	// ── §18.2 hardware ───────────────────────────────────────────────────

	detect := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, hardware.GetInventory())
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
		mux.HandleFunc("GET "+p("/model-catalog"), func(w http.ResponseWriter, r *http.Request) {
			inventory := hardware.GetInventory()
			registered, err := s.Models.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, "model catalog is temporarily unavailable")
				return
			}
			known := map[string]bool{}
			for _, entry := range registered {
				known[entry.ID] = true
			}
			items := make([]map[string]any, 0)
			for _, entry := range models.Catalog(inventory.Profile) {
				raw, _ := json.Marshal(entry)
				var item map[string]any
				_ = json.Unmarshal(raw, &item)
				compatible, reason := catalogCompatibility(entry, inventory)
				item["registered"] = known[entry.ID]
				item["compatible"] = compatible
				item["compatibility_reason"] = reason
				items = append(items, item)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"catalog_version": models.CatalogVersion, "profile": inventory.Profile, "models": items,
			})
		})
		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/model-catalog/{id}/install"), func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Activate *bool `json:"activate"`
				}
				if r.Body != nil {
					_ = json.NewDecoder(r.Body).Decode(&request)
				}
				profile := hardware.GetInventory().Profile
				item, err := models.CatalogEntry(profile, r.PathValue("id"))
				if err != nil {
					errorJSON(w, http.StatusNotFound, err.Error())
					return
				}
				if compatible, reason := catalogCompatibility(item, hardware.GetInventory()); !compatible {
					errorJSON(w, http.StatusUnprocessableEntity, reason)
					return
				}
				_, registryErr := s.Models.Get(item.ID)
				registered := registryErr == nil
				if !registered {
					if err := s.Models.Add(item.RegistryEntry); err != nil {
						errorJSON(w, http.StatusBadRequest, err.Error())
						return
					}
				}
				if item.Role == "embedding" {
					writeJSON(w, http.StatusAccepted, map[string]any{"model_id": item.ID, "managed_by": "embeddinggemma", "ready": s.Embeddings != nil && s.Embeddings.Healthy(r.Context())})
					return
				}
				if registered && request.Activate != nil && !*request.Activate {
					writeJSON(w, http.StatusAccepted, map[string]any{"model_id": item.ID, "managed_by": "runtime", "ready": false})
					return
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "model-load", models.LoadPayload{ModelID: item.ID})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "model_id": item.ID})
			})
		}
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
			if s.Embeddings != nil && s.Embeddings.Healthy(r.Context()) {
				loaded["embedding"] = embeddings.EmbeddingGemmaModel
			}
			out := make([]map[string]any, 0, len(entries))
			for _, entry := range entries {
				raw, _ := json.Marshal(entry)
				var item map[string]any
				_ = json.Unmarshal(raw, &item)
				item["loaded"] = loaded[entry.Role] == entry.Model
				out = append(out, item)
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
		mux.HandleFunc("POST "+p("/models"), addModel)

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
				entry, err := s.Models.Get(modelID)
				if err != nil {
					errorJSON(w, http.StatusNotFound, err.Error())
					return
				}
				if entry.Source == "remote" || entry.Source == "cloud" {
					if s.GatewayConfigPath == "" {
						errorJSON(w, http.StatusServiceUnavailable, "gateway configuration is unavailable")
						return
					}
					if err := gateway.GenerateConfig(r.Context(), s.GatewayConfigPath, s.Models, s.Profiles, s.Credentials); err != nil {
						errorJSON(w, http.StatusUnprocessableEntity, err.Error())
						return
					}
					if err := s.Proxy.Restart(r.Context(), "sovereign-gateway"); err != nil {
						errorJSON(w, http.StatusBadGateway, err.Error())
						return
					}
					writeJSON(w, http.StatusAccepted, map[string]string{"model_id": modelID, "restarting": "sovereign-gateway"})
					return
				}
				// §2.9: warn, never block.
				load := models.LoadPayload{ModelID: modelID}
				if entry.Role == "embedding" {
					load.ServedModelName = entry.ID
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "model-load", load)
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{
					"job_id":  jobID,
					"warning": "This model has not been validated on the selected runtime profile. Loading may fail, consume excessive memory, or perform poorly. Run a smoke test after loading.",
				})
			})
			mux.HandleFunc("POST "+p("/models/{id}/smoke-test"), func(w http.ResponseWriter, r *http.Request) {
				if _, err := s.Models.Get(r.PathValue("id")); err != nil {
					errorJSON(w, http.StatusNotFound, err.Error())
					return
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "evals-run", map[string]string{"suite": "smoke"})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "model_id": r.PathValue("id"), "suite": "smoke"})
			})
		}
	}

	// ── encrypted provider credentials ──────────────────────────────────

	if s.Credentials != nil {
		mux.HandleFunc("GET "+p("/provider-credentials"), func(w http.ResponseWriter, r *http.Request) {
			items, err := s.Credentials.List(r.Context())
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, "credential metadata unavailable")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"credentials": items})
		})
		putCredential := func(w http.ResponseWriter, r *http.Request) {
			var request credentials.PutRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				errorJSON(w, http.StatusBadRequest, "invalid credential")
				return
			}
			if id := r.PathValue("id"); id != "" {
				request.ID = id
			}
			item, err := s.Credentials.Put(r.Context(), request)
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, item)
		}
		mux.HandleFunc("POST "+p("/provider-credentials"), putCredential)
		mux.HandleFunc("PUT "+p("/provider-credentials/{id}"), putCredential)
		mux.HandleFunc("DELETE "+p("/provider-credentials/{id}"), func(w http.ResponseWriter, r *http.Request) {
			if err := s.Credentials.Delete(r.Context(), r.PathValue("id")); err != nil {
				errorJSON(w, http.StatusNotFound, "credential not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
		})
	}

	// ── jobs ─────────────────────────────────────────────────────────────

	if s.Jobs != nil {
		mux.HandleFunc("GET "+p("/jobs"), func(w http.ResponseWriter, r *http.Request) {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			items, err := s.Jobs.List(r.Context(), limit)
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, "activity is temporarily unavailable")
				return
			}
			if identity, ok := requestIdentity(r); ok && identity.Role == "member" {
				for index := range items {
					redactMemberJob(&items[index])
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"jobs": items})
		})
		mux.HandleFunc("GET "+p("/jobs/events"), func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				errorJSON(w, http.StatusNotImplemented, "activity streaming is unavailable")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Accel-Buffering", "no")
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				items, err := s.Jobs.List(r.Context(), 50)
				if err != nil {
					fmt.Fprintf(w, "event: error\ndata: %s\n\n", strconv.Quote("activity temporarily unavailable"))
				} else {
					if identity, ok := requestIdentity(r); ok && identity.Role == "member" {
						for index := range items {
							redactMemberJob(&items[index])
						}
					}
					payload, _ := json.Marshal(map[string]any{"jobs": items})
					fmt.Fprintf(w, "event: jobs\ndata: %s\n\n", payload)
				}
				flusher.Flush()
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
				}
			}
		})
		mux.HandleFunc("GET "+p("/jobs/{id}"), func(w http.ResponseWriter, r *http.Request) {
			job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "job not found")
				return
			}
			if identity, ok := requestIdentity(r); ok && identity.Role == "member" {
				redactMemberJob(job)
			}
			writeJSON(w, http.StatusOK, job)
		})
		mux.HandleFunc("POST "+p("/jobs/{id}/cancel"), func(w http.ResponseWriter, r *http.Request) {
			job, err := s.Jobs.Cancel(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, job)
		})
		mux.HandleFunc("POST "+p("/jobs/{id}/retry"), func(w http.ResponseWriter, r *http.Request) {
			jobID, err := s.Jobs.Retry(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
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
		mux.HandleFunc("POST "+p("/evals/embedding"), runSuite("embedding"))
		mux.HandleFunc("POST "+p("/evals/retrieval"), runSuite("retrieval"))
		mux.HandleFunc("POST "+p("/evals/mixed-role"), runSuite("mixed-role"))
		mux.HandleFunc("POST "+p("/evals/suite"), func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Suite string `json:"suite"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				errorJSON(w, http.StatusBadRequest, "suite is required")
				return
			}
			allowed := map[string]bool{
				"smoke": true, "quick": true, "full": true, "embedding": true,
				"retrieval": true, "mixed-role": true,
			}
			if !allowed[request.Suite] {
				errorJSON(w, http.StatusBadRequest, "unknown evaluation suite")
				return
			}
			runSuite(request.Suite)(w, r)
		})
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
		profileMutable := func(ctx context.Context, id string) error {
			if s.Indexes == nil {
				return nil
			}
			state, err := s.Indexes.EmbeddingState(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if state.ProfileID == id {
				return fmt.Errorf("active embedding profiles are immutable; create and activate a replacement profile")
			}
			return nil
		}
		validateProfileReference := func(profile embeddings.Profile) error {
			if profile.Provider == "embeddinggemma" {
				return nil
			}
			if s.Models == nil {
				return fmt.Errorf("model registry is unavailable")
			}
			entry, err := s.Models.Get(profile.ModelEntryID)
			if err != nil {
				return err
			}
			if entry.Role != "embedding" {
				return fmt.Errorf("model registry entry %q must have the embedding role", entry.ID)
			}
			if profile.Model != entry.Model || profile.Revision != entry.Revision {
				return fmt.Errorf("embedding profile model and revision must match model registry entry %q", entry.ID)
			}
			remote := entry.Source == "remote" || entry.Source == "cloud"
			if profile.Provider == "openai-compatible" && !remote {
				return fmt.Errorf("openai-compatible profiles require a remote or cloud model entry")
			}
			if profile.Provider == "sovereign-runtime" && remote {
				return fmt.Errorf("sovereign-runtime profiles require a local, offline, Hugging Face, or ModelScope entry")
			}
			return nil
		}
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
			if err := profileMutable(r.Context(), id); err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
			if err := validateProfileReference(profile); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
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
			if err := profileMutable(r.Context(), body.ID); err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
			if err := validateProfileReference(body.Profile); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
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
			if err := profileMutable(r.Context(), r.PathValue("id")); err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
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
			result := map[string]any{"profile": r.PathValue("id"), "loaded": false}
			if s.EmbeddingActivator == nil {
				result["detail"] = "embedding providers are unavailable"
				writeJSON(w, http.StatusOK, result)
				return
			}
			if profile.Provider == "openai-compatible" && s.GatewayConfigPath != "" {
				if err := gateway.GenerateConfig(r.Context(), s.GatewayConfigPath, s.Models, s.Profiles, s.Credentials); err != nil {
					errorJSON(w, http.StatusUnprocessableEntity, err.Error())
					return
				}
				if err := s.Proxy.Restart(r.Context(), "sovereign-gateway"); err != nil {
					errorJSON(w, http.StatusBadGateway, err.Error())
					return
				}
				deadline := time.Now().Add(2 * time.Minute)
				for !s.Gateway.Healthy(r.Context()) {
					if time.Now().After(deadline) {
						errorJSON(w, http.StatusGatewayTimeout, "gateway did not become healthy after embedding route reload")
						return
					}
					select {
					case <-r.Context().Done():
						return
					case <-time.After(2 * time.Second):
					}
				}
			}
			raw, _ := json.Marshal(embeddings.ActivatePayload{ProfileID: r.PathValue("id")})
			probe, probeErr := s.EmbeddingActivator.HandleActivate(r.Context(), raw)
			if probeErr == nil {
				result["loaded"] = true
				result["dimensions"] = probe.(map[string]any)["dimensions"]
			} else {
				result["detail"] = probeErr.Error()
			}
			writeJSON(w, http.StatusOK, result)
		})

		mux.HandleFunc("GET "+p("/embedding-state"), func(w http.ResponseWriter, r *http.Request) {
			if s.Indexes == nil {
				writeJSON(w, http.StatusOK, map[string]any{"active": nil})
				return
			}
			state, err := s.Indexes.EmbeddingState(r.Context())
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusOK, map[string]any{"active": nil})
				return
			}
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"active": state})
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

		if s.Jobs != nil && s.Profiles != nil {
			mux.HandleFunc("POST "+p("/indexes/rebuild"), func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					WorkspaceID      string `json:"workspace_id"`
					ProviderSlug     string `json:"provider_slug"`
					EmbeddingProfile string `json:"embedding_profile"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					errorJSON(w, http.StatusBadRequest, "workspace_id, provider_slug, and embedding_profile are required")
					return
				}
				state, err := s.Indexes.EmbeddingState(r.Context())
				if errors.Is(err, pgx.ErrNoRows) {
					errorJSON(w, http.StatusConflict, "activate an appliance embedding provider before creating an index")
					return
				}
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				if request.EmbeddingProfile == "" {
					request.EmbeddingProfile = state.ProfileID
				}
				if request.EmbeddingProfile != state.ProfileID {
					errorJSON(w, http.StatusConflict, "embedding providers are appliance-wide; activate the requested provider first")
					return
				}
				profile, err := s.Profiles.Get(state.ProfileID)
				if err != nil {
					errorJSON(w, http.StatusBadRequest, err.Error())
					return
				}
				target, err := s.Indexes.CreatePending(r.Context(), indexes.CreateRequest{
					WorkspaceID: request.WorkspaceID, ProviderSlug: request.ProviderSlug,
					ProfileID: request.EmbeddingProfile, ModelID: profile.Model,
					ModelRevision: profile.Revision, Normalization: profile.Normalization,
					DistanceMetric: profile.DistanceMetric, QueryPrefix: profile.QueryPrefix,
					DocumentPrefix: profile.DocumentPrefix, ChunkingStrategy: profile.ChunkingStrategy,
					PreprocessingVersion: profile.PreprocessingVersion,
				})
				if err != nil {
					errorJSON(w, http.StatusBadRequest, err.Error())
					return
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "index-rebuild", indexes.RebuildPayload{
					TargetIndexID: target.ID, ActivateWhenComplete: true,
				})
				if err != nil {
					_ = s.Indexes.Fail(r.Context(), target.ID, err)
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]any{
					"job_id": jobID, "target_index_id": target.ID, "status": "building", "maintenance_mode": true,
				})
			})
		}

		mux.HandleFunc("GET "+p("/indexes/{id}"), func(w http.ResponseWriter, r *http.Request) {
			version, err := s.Indexes.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "index version not found")
				return
			}
			writeJSON(w, http.StatusOK, version)
		})

		mux.HandleFunc("POST "+p("/indexes/{id}/activate"), func(w http.ResponseWriter, r *http.Request) {
			candidate, err := s.Indexes.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "index version not found")
				return
			}
			state, err := s.Indexes.EmbeddingState(r.Context())
			if err != nil || candidate.ProfileID != state.ProfileID {
				errorJSON(w, http.StatusConflict, "only an index built with the appliance-wide active embedding provider can be activated")
				return
			}
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
			if s.Jobs == nil || s.Profiles == nil {
				errorJSON(w, http.StatusServiceUnavailable, "index rebuild jobs are unavailable")
				return
			}
			source, err := s.Indexes.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "source index not found")
				return
			}
			var request struct {
				EmbeddingProfile     string `json:"embedding_profile"`
				ActivateWhenComplete *bool  `json:"activate_when_complete"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&request)
			}
			if request.EmbeddingProfile == "" {
				state, stateErr := s.Indexes.EmbeddingState(r.Context())
				if stateErr != nil {
					errorJSON(w, http.StatusConflict, "activate an appliance embedding provider before rebuilding indexes")
					return
				}
				request.EmbeddingProfile = state.ProfileID
			}
			state, stateErr := s.Indexes.EmbeddingState(r.Context())
			if stateErr != nil || request.EmbeddingProfile != state.ProfileID {
				errorJSON(w, http.StatusConflict, "embedding providers are appliance-wide; activate the requested provider first")
				return
			}
			profile, err := s.Profiles.Get(request.EmbeddingProfile)
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			target, err := s.Indexes.CreatePending(r.Context(), indexes.CreateRequest{
				WorkspaceID: source.WorkspaceID, ProviderSlug: source.ProviderSlug,
				ProfileID: request.EmbeddingProfile, ModelID: profile.Model,
				ModelRevision: profile.Revision, Normalization: profile.Normalization,
				DistanceMetric: profile.DistanceMetric, QueryPrefix: profile.QueryPrefix,
				DocumentPrefix: profile.DocumentPrefix, ChunkingStrategy: profile.ChunkingStrategy,
				PreprocessingVersion: profile.PreprocessingVersion,
			})
			if err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			activate := true
			if request.ActivateWhenComplete != nil {
				activate = *request.ActivateWhenComplete
			}
			jobID, err := s.Jobs.Enqueue(r.Context(), "index-rebuild", indexes.RebuildPayload{
				TargetIndexID: target.ID, ActivateWhenComplete: activate,
			})
			if err != nil {
				_ = s.Indexes.Fail(r.Context(), target.ID, err)
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"job_id": jobID, "target_index_id": target.ID,
				"status": "building", "maintenance_mode": true,
			})
		})
	}

	// ── branding + feature flags (§26 product capabilities) ─────────────

	if s.Branding != nil {
		// Public cosmetic subset, so the login page can theme itself.
		mux.HandleFunc("GET "+p("/theme"), func(w http.ResponseWriter, r *http.Request) {
			doc, err := s.Branding.Get()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, publicTheme(doc))
		})
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

	// ── §18.11 backups ───────────────────────────────────────────────────

	if s.Backups != nil {
		mux.HandleFunc("GET "+p("/backups"), func(w http.ResponseWriter, r *http.Request) {
			manifests, err := s.Backups.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			if manifests == nil {
				manifests = []backups.Manifest{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"backups": manifests})
		})

		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/backups"), func(w http.ResponseWriter, r *http.Request) {
				jobID, err := s.Jobs.Enqueue(r.Context(), "backup", map[string]string{})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
			})

			mux.HandleFunc("POST "+p("/backups/{id}/restore"), func(w http.ResponseWriter, r *http.Request) {
				jobID, err := s.Jobs.Enqueue(r.Context(), "backup-restore",
					backups.RestorePayload{BackupID: r.PathValue("id")})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
			})
		}

		mux.HandleFunc("POST "+p("/backups/{id}/verify"), func(w http.ResponseWriter, r *http.Request) {
			result, err := s.Backups.Verify(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	}

	// ── §18.12 offline bundles ───────────────────────────────────────────

	if s.Bundles != nil {
		mux.HandleFunc("GET "+p("/bundles"), func(w http.ResponseWriter, r *http.Request) {
			manifests, err := s.Bundles.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"bundles": manifests})
		})
		mux.HandleFunc("GET "+p("/bundles/{id}"), func(w http.ResponseWriter, r *http.Request) {
			manifest, err := s.Bundles.Get(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "bundle not found")
				return
			}
			writeJSON(w, http.StatusOK, manifest)
		})
		mux.HandleFunc("GET "+p("/bundles/{id}/download"), func(w http.ResponseWriter, r *http.Request) {
			path, err := s.Bundles.ArchivePath(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "bundle archive not found")
				return
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
			w.Header().Set("Content-Type", "application/gzip")
			http.ServeFile(w, r, path)
		})
		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/bundles"), func(w http.ResponseWriter, r *http.Request) {
				var request bundles.CreatePayload
				if r.Body != nil {
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
						errorJSON(w, http.StatusBadRequest, "invalid bundle request")
						return
					}
				}
				jobID, err := s.Jobs.Enqueue(r.Context(), "bundle-create", request)
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
			})
		}
	}

	if s.Support != nil {
		mux.HandleFunc("GET "+p("/support-bundles"), func(w http.ResponseWriter, r *http.Request) {
			items, err := s.Support.List()
			if err != nil {
				errorJSON(w, http.StatusInternalServerError, "support bundles are unavailable")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"support_bundles": items})
		})
		mux.HandleFunc("GET "+p("/support-bundles/{id}/download"), func(w http.ResponseWriter, r *http.Request) {
			path, err := s.Support.Path(r.PathValue("id"))
			if err != nil {
				errorJSON(w, http.StatusNotFound, "support bundle not found")
				return
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
			w.Header().Set("Content-Type", "application/gzip")
			http.ServeFile(w, r, path)
		})
		if s.Jobs != nil {
			mux.HandleFunc("POST "+p("/support-bundles"), func(w http.ResponseWriter, r *http.Request) {
				jobID, err := s.Jobs.Enqueue(r.Context(), "support-bundle", map[string]string{})
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
			})
		}
	}

	// ── §18.9 workspace ──────────────────────────────────────────────────

	if s.Workspace != nil {
		mux.HandleFunc("GET "+p("/workspaces"), func(w http.ResponseWriter, r *http.Request) {
			workspaces, err := s.Workspace.Workspaces(r.Context())
			if err != nil {
				errorJSON(w, http.StatusBadGateway, err.Error())
				return
			}
			if identity, ok := requestIdentity(r); ok && identity.Role == "member" {
				allowed := map[string]bool{}
				for _, id := range identity.WorkspaceIDs {
					allowed[id] = true
				}
				filtered := workspaces[:0]
				for _, item := range workspaces {
					if allowed[item.ID] {
						filtered = append(filtered, item)
					}
				}
				workspaces = filtered
			}
			writeJSON(w, http.StatusOK, map[string]any{"workspaces": workspaces})
		})
		mux.HandleFunc("GET "+p("/workspace/sso"), func(w http.ResponseWriter, r *http.Request) {
			identity, ok := requestIdentity(r)
			if !ok {
				errorJSON(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			workspaces, err := s.Workspace.Workspaces(r.Context())
			if err != nil {
				errorJSON(w, http.StatusBadGateway, err.Error())
				return
			}
			allowed := map[string]bool{}
			for _, id := range identity.WorkspaceIDs {
				allowed[id] = true
			}
			slugs := []string{}
			for _, item := range workspaces {
				if identity.Role != "member" || allowed[item.ID] {
					slugs = append(slugs, item.Slug)
				}
			}
			loginPath, err := s.Workspace.Session(r.Context(), workspace.SessionRequest{
				Username: identity.Username, Role: identity.Role,
				Suspended: identity.Disabled, WorkspaceSlugs: slugs,
			})
			if err != nil {
				errorJSON(w, http.StatusBadGateway, err.Error())
				return
			}
			redirect, err := workspaceLoginRedirect(loginPath, identity.Role, slugs)
			if err != nil {
				errorJSON(w, http.StatusConflict, err.Error())
				return
			}
			// AnythingLLM's browser router is rooted at /; a visible /apps/chat
			// prefix reaches its wildcard 404 even though Caddy stripped that
			// prefix from the upstream HTTP request.
			http.Redirect(w, r, redirect, http.StatusSeeOther)
		})
		mux.HandleFunc("GET "+p("/workspace/status"), func(w http.ResponseWriter, r *http.Request) {
			name, _ := s.Workspace.AppName(r.Context())
			writeJSON(w, http.StatusOK, map[string]any{
				"reachable": s.Workspace.Reachable(r.Context()),
				"app_name":  name,
			})
		})

		if s.Branding != nil {
			mux.HandleFunc("POST "+p("/workspace/branding/apply"), func(w http.ResponseWriter, r *http.Request) {
				doc, err := s.Branding.Get()
				if err != nil {
					errorJSON(w, http.StatusInternalServerError, err.Error())
					return
				}
				applied := map[string]any{}
				if name, _ := doc["product_name"].(string); name != "" {
					if err := s.Workspace.SetAppName(r.Context(), name); err != nil {
						errorJSON(w, http.StatusBadGateway, err.Error())
						return
					}
					applied["app_name"] = name
				}
				if logo, _ := doc["logo"].(string); logo != "" {
					// branding paths are rooted at the deploy mount
					path := filepath.Join(s.SovereignRoot, strings.TrimPrefix(logo, "/"))
					if err := s.Workspace.UploadLogo(r.Context(), path); err != nil {
						errorJSON(w, http.StatusBadGateway, err.Error())
						return
					}
					applied["logo"] = logo
				}
				writeJSON(w, http.StatusOK, map[string]any{"applied": applied})
			})
		}
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

	mux.HandleFunc("DELETE "+p("/gateway/keys/{id}"), func(w http.ResponseWriter, r *http.Request) {
		if err := s.Gateway.DeleteKey(r.Context(), r.PathValue("id")); err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
	})

	mux.HandleFunc("GET "+p("/gateway/usage"), func(w http.ResponseWriter, r *http.Request) {
		usage, err := s.Gateway.Usage(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, usage)
	})

	mux.HandleFunc("GET "+p("/gateway/budgets"), func(w http.ResponseWriter, r *http.Request) {
		keys, err := s.Gateway.ListKeys(r.Context())
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, keys)
	})

	mux.HandleFunc("PATCH "+p("/gateway/budgets/{id}"), func(w http.ResponseWriter, r *http.Request) {
		var update gateway.BudgetUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid budget update")
			return
		}
		result, err := s.Gateway.UpdateBudget(r.Context(), r.PathValue("id"), update)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	if s.GatewayConfigPath != "" && s.Profiles != nil && s.Models != nil {
		mux.HandleFunc("POST "+p("/gateway/config/regenerate"), func(w http.ResponseWriter, r *http.Request) {
			if err := gateway.GenerateConfig(r.Context(), s.GatewayConfigPath, s.Models, s.Profiles, s.Credentials); err != nil {
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

	mux.HandleFunc("GET /metrics", s.metrics)

	if s.UI != nil {
		mux.Handle("GET /", s.UI)
	}

	// Session and role middleware. Control is the appliance identity authority;
	// downstream apps receive identity only through forward-auth or one-time SSO.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth != nil && strings.HasPrefix(r.URL.Path, BasePath) && !openPath(r.URL.Path) {
			operator := r.Header.Get("X-Sovereign-Operator-Token")
			isOperator := s.OperatorToken != "" && len(operator) == len(s.OperatorToken) &&
				subtle.ConstantTimeCompare([]byte(operator), []byte(s.OperatorToken)) == 1
			identity := auth.Identity{ID: -1, Username: "host-operator", Role: "admin"}
			if !isOperator {
				var err error
				identity, err = s.Auth.ValidateIdentity(r.Context(), sessionToken(r))
				if err != nil {
					errorJSON(w, http.StatusUnauthorized, "not authenticated")
					return
				}
			}
			memberAllowed := memberPath(r.URL.Path) ||
				(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, BasePath+"/jobs"))
			if identity.Role == "member" && !memberAllowed {
				errorJSON(w, http.StatusForbidden, "member role cannot access this control-plane operation")
				return
			}
			if identity.Role == "manager" && adminOnlyPath(r.URL.Path) {
				errorJSON(w, http.StatusForbidden, "administrator role required")
				return
			}
			requestContext := context.WithValue(r.Context(), identityContextKey{}, identity)
			requestContext = jobs.WithInitiator(requestContext, identity.ID)
			r = r.WithContext(requestContext)
		}
		mux.ServeHTTP(w, r)
	})
}
