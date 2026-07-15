# Architecture

SovereignStack is a local appliance with one public loopback ingress and a
private Compose network:

```text
browser → Caddy :8880 → AnythingLLM workspace
                    └→ Sovereign Control /api/control/v1

AnythingLLM → LiteLLM gateway → Sovereign Runtime :8000
        └──→ PostgreSQL + pgvector

Sovereign Control → restricted Docker proxy → Docker socket
                  → runtime, gateway, workspace, evals, backups, bundles
```

Caddy is the only host-published service. LiteLLM's UI is never exposed;
Control owns model routes, keys, budgets, and encrypted provider credentials.
The runtime presents one OpenAI-compatible endpoint with generation and
embedding roles. On Apple Silicon the contract container delegates inference
to a bearer-authenticated host Metal agent; consumers see the same API.

Persistent state lives in named Docker volumes and `~/.sovereign`. Release code
is versioned under `releases/<version>` and selected through `current`; config,
secrets, models, reports, backups, and bundles are shared across reinstalls.

AnythingLLM is extended by a small reviewed preload layer. Workspace namespaces
resolve through `vectors.workspace_bindings`; index rebuilds write to a new
namespace, validate counts, then switch the binding atomically. The old active
index remains available until maintenance begins and is retained for rollback.
