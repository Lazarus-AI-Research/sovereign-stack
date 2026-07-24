# Sovereign Control API

The complete contract is `api/sovereign-control.openapi.yaml`. It is served
under `/api/control/v1` through the portal origin. `/health`, `/auth/login`,
first-admin claims, invitations, and public theme data are unauthenticated;
every other Control endpoint requires the
`sovereign_session` cookie or bearer session token.

Control is the identity authority and enforces admin, manager, and member
roles. The API groups cover users and invitations, independent readiness, the
controlled application registry, system/runtime status, curated and custom models, encrypted provider
credentials, embedding profiles, versioned indexes, workspace discovery,
gateway keys and budgets, evaluations, backups, offline and support bundles,
network access, updates, repair, branding, feature flags, and long-running
jobs. Mutating operations that can take more
than one request return `202` and a `job_id`:

```text
POST /api/control/v1/evals/suite
→ {"job_id":"..."}

GET /api/control/v1/jobs/{job_id}
→ {"status":"queued|running|succeeded|failed|canceled","stage":"loading","progress_current":2,"progress_total":4, ...}

GET /api/control/v1/jobs/events
→ event: jobs (server-sent updates; polling remains supported)
```

Jobs preserve the original enqueue/get contracts and add progress, heartbeat,
initiating-user audit metadata, cancellation, retry relationships, stable error
codes, and suggested recovery actions. `GET /readiness` reports portal,
generation, embeddings, gateway, and workspace independently so optional
service startup cannot make the whole product appear unavailable.

Embedding activation is appliance-wide and asynchronous: it rebuilds and
atomically switches every workspace index. Per-workspace endpoints accept only
the currently active appliance profile.

Control is the supported management surface. The LiteLLM UI, workspace admin
internals, runtime control endpoints, and Docker proxy API are not exposed to
the host.
