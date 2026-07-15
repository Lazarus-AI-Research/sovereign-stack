# Sovereign Control API

The complete contract is `api/sovereign-control.openapi.yaml`. It is served
under `/api/control/v1` through the localhost Caddy ingress. `/health` and
`/auth/login` are public; every other Control endpoint requires the
`sovereign_session` cookie or bearer session token.

The API groups cover system/runtime status, models, encrypted provider
credentials, embedding profiles, versioned indexes, workspace discovery,
gateway keys and budgets, evaluations, backups, offline bundles, branding,
feature flags, and long-running jobs. Mutating operations that can take more
than one request return `202` and a `job_id`:

```text
POST /api/control/v1/evals/suite
→ {"job_id":"..."}

GET /api/control/v1/jobs/{job_id}
→ {"status":"queued|running|succeeded|failed", ...}
```

Control is the supported management surface. The LiteLLM UI, workspace admin
internals, runtime control endpoints, and Docker proxy API are not exposed to
the host.
