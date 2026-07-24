# Architecture

SovereignStack is a local appliance with one customer-facing portal origin and
a private Compose network:

```text
browser → Caddy → Sovereign portal + Control /api/control/v1
              ├→ /apps/chat (AnythingLLM, Control SSO)
              ├→ /apps/grafana
              └→ /apps/phoenix

AnythingLLM → LiteLLM gateway → Sovereign Runtime :8000 (generation)
                           └──→ embeddinggemma :42666 (embeddings)
        └──→ PostgreSQL + pgvector

Sovereign Control → restricted Docker proxy → Docker socket
                  → runtime, embeddings, gateway, workspace, evals, backups, bundles
                  → authenticated sovereign-hostd → allowlisted network/update/repair operations
```

Caddy is the only host-published service. It defaults to loopback, supports a
private LAN binding, and obtains certificates when configured with a domain.
LiteLLM's UI is never exposed;
Control owns model routes, keys, budgets, and encrypted provider credentials.
LiteLLM presents one OpenAI-compatible product endpoint while routing
generation and embedding requests to independent services. On CUDA,
`embeddinggemma` is a private sibling container. On Apple Silicon it is a
loopback-only, launchd-managed Metal process reached from Docker Desktop via
`host.docker.internal`.

The signed `sovereign-hostd` process is managed by launchd or a user systemd
unit. It exposes a versioned bearer-authenticated API only to Control's host
bridge path. The protocol accepts fixed network, repair, status, and signed
update requests; it never accepts a shell command, filesystem path, Compose
argument, or unrestricted Docker operation. Existing CLI commands remain the
fallback for upgraded installations without hostd.

Long operations use the PostgreSQL-backed job queue. Jobs report stages,
progress, heartbeat, cancellation, retry, and actionable failures to the
Activity Center over same-origin server-sent events. The portal and each
runtime capability publish independent readiness so Chat navigation remains
available while models or optional tools start.

Persistent state lives in named Docker volumes and `~/.sovereign`. Release code
is versioned under `releases/<version>` and selected through `current`; config,
secrets, models, reports, backups, and bundles are shared across reinstalls.

Control is the identity authority; its users, roles, and workspace memberships
are mirrored into AnythingLLM with one-time SSO and no password sharing.
AnythingLLM is extended by a small reviewed preload layer. Workspace namespaces
resolve through `vectors.workspace_bindings`; index rebuilds write to a new
namespace, validate counts, then switch the binding atomically. The old active
index remains available until maintenance begins and is retained for rollback.
The embedding provider is appliance-wide: activation prepares the provider,
rebuilds every workspace, validates all candidates, then changes every binding
and the global provider state in one transaction. The built-in default is
EmbeddingGemma; advanced profiles can route to Sovereign Runtime or another
OpenAI-compatible service.
