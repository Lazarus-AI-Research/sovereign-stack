# Sovereign Runtime Contract

Authoritative sources: design.md §3.2–3.5, §9, §12–14, §20, §24. This document
fixes the machine-observable contract every runtime image must satisfy. The
conformance harness (`sovereign-evals conformance`) enforces it; the schemas in
`schemas/runtime-config.schema.json` and `schemas/runtime-manifest.schema.json`
are the source of truth for payload shapes.

## The invariant, precisely

```text
One container. One entrypoint (run-sovereign-runtime). One supervising API
process. One port (8000). One OpenAI-compatible base URL (/v1). Multiple model
roles. Shared process-tree fate.
```

**Clarification — "one vLLM process":** upstream vLLM V1 runs its engine core
(and tensor-parallel workers) as child processes even for a single model. The
invariant is therefore defined at the level the rest of the product can
observe: one supervised process tree behind one port. Internal engine-core or
worker children are implementation details; their death is either fatal to the
tree or reported as `runtime_error` — never silently absorbed. This is
consistent with §3.4, which already names process crash as affecting all roles.

## States (§3.2)

```text
initializing → downloading → compiling → loading → smoke_testing
    → healthy | degraded | configuration_error | runtime_error
```

- `downloading` and `compiling` may be skipped when nothing needs doing.
- `degraded`: at least one enabled role is unhealthy while the process and
  accelerator remain operational.
- `configuration_error`: recoverable; the process **must stay alive** with the
  control API serving (when `remain_alive_on_configuration_error: true`, the
  default) so Sovereign Control can diagnose and correct. A configuration
  error must never produce a crash loop.
- `runtime_error`: a run-time fatal condition (engine death, accelerator
  reset). Whether the process exits (letting Docker restart it) or stays up
  for diagnosis is governed by `fail_process_on_generation_error` /
  `fail_process_on_embedding_error` (§12).

## Endpoints

All on port 8000. `/v1/*` requires the API key when `runtime.api_key_env` is
configured; health and manifest endpoints are unauthenticated (internal
network only, §22).

| Endpoint | Contract |
| --- | --- |
| `GET /health/live` | 200 whenever the supervising process and control API are alive — **independent of model readiness**. Docker healthchecks use only this. Body: `{"status": "alive", "state": "<state>"}`. |
| `GET /health/ready` | 200 with `{"ready": true, ...}` only when all required roles are available; otherwise `{"ready": false, "state": ..., "required_roles": {...}}` (status 503). |
| `GET /health` | Aggregate: `status`, `state`, `runtime_id`, per-role `status` / `model_loaded` / `served_model_name` (embedding adds `modalities`), per §13.3. |
| `GET /runtime/manifest` | The manifest, valid against `runtime-manifest.schema.json`. Also written to `$SOVEREIGN_RUNTIME_MANIFEST`. |
| `GET /runtime/errors` | `{"errors": [{code, role?, message, recoverable, first_seen}]}` — non-empty whenever state is `configuration_error`/`runtime_error`/`degraded`. |
| `POST /v1/chat/completions` | Generation role, OpenAI-compatible incl. `stream: true` (SSE terminated by `data: [DONE]`). |
| `POST /v1/completions` | Generation role. |
| `POST /v1/embeddings` | Embedding role. Extended schema below. |
| `GET /v1/models` | Aggregated list across all loaded roles; `id` = `served_model_name`. |
| `GET /metrics` | Prometheus text format, labels per §21. |

**Role routing (§9.3):** the endpoint selects the role; the request `model`
field must equal that role's `served_model_name`. A `model` naming a
different role's alias (e.g. the embedding alias on `/v1/chat/completions`)
returns **404** with an OpenAI-style model-not-found error body.

## Error codes

`/runtime/errors` codes are stable strings; Sovereign Control switches on
them. Initial taxonomy (extend here, never ad hoc):

| Code | Meaning | Recoverable |
| --- | --- | --- |
| `CONFIG_INVALID` | runtime.yaml failed schema/semantic validation | yes |
| `MODEL_NOT_FOUND` | model repo/path does not exist | yes |
| `MODEL_REVISION_NOT_FOUND` | pinned revision could not be resolved | yes |
| `MODEL_DOWNLOAD_FAILED` | network/permission failure fetching weights | yes |
| `MODEL_LOAD_FAILED` | engine failed to load the model | yes |
| `DIMENSION_MISMATCH` | probed embedding dim ≠ expected/declared | yes |
| `ACCELERATOR_UNAVAILABLE` | expected accelerator not present/usable | yes |
| `OUT_OF_MEMORY` | load or runtime OOM | yes |
| `ENGINE_DEAD` | engine core process died at runtime | no |
| `SMOKE_TEST_FAILED` | startup self-test failed | yes |
| `HOST_AGENT_UNREACHABLE` | Metal host inference agent not reachable/compatible | yes |

## Extended `/v1/embeddings` (Sovereign superset of OpenAI)

Text (standard OpenAI): `{"model": ..., "input": "..." | ["...", ...]}`.

Multimodal (LCO-Omni profiles): a `messages` array in OpenAI chat format
carries the content parts; exactly one embedding is returned per request item.

```json
{
  "model": "embedding-omni-default",
  "messages": [
    { "role": "user", "content": [
      { "type": "image_url", "image_url": { "url": "data:image/png;base64,..." } },
      { "type": "text", "text": "optional accompanying text" }
    ]}
  ]
}
```

Audio uses `{"type": "input_audio", "input_audio": {"data": "<base64>", "format": "wav"}}`.

Rules:
- **Data URIs / base64 only. Remote URLs are rejected** — the runtime never
  performs egress fetches (sovereignty).
- Response shape is standard OpenAI (`data[i].embedding`), values L2-normalized
  when the profile says `normalization: l2`.
- The gateway must pass these fields through; the LiteLLM route for this is a
  permanent smoke-suite check (drop_params risk, §15).

## Image contract (§24)

Every runtime image ships `/usr/local/bin/run-sovereign-runtime` (entrypoint)
and `/usr/local/bin/sovereign-runtime-healthcheck` (`--live` probes
`/health/live` only), reads `$SOVEREIGN_RUNTIME_CONFIG`, writes
`$SOVEREIGN_RUNTIME_MANIFEST`, and follows the §24 startup sequence: config →
control API up → initializing → detect accelerator → resolve revisions →
download/verify → compile → load generation → load embedding → serve :8000 →
manifest → startup smoke test (§20) → terminal state → logs + metrics.

Honesty requirements:
- The manifest reports **observed** execution: backend/accelerator as actually
  used (Metal Phase 1 reports CPU execution), embedding dimensions as probed.
- Role-level degradation never implies process-level fault isolation (§3.4).
- Memory weights are best-effort; observed memory is reported via metrics (§3.5).

## Conformance

`sovereign-evals conformance --base-url http://sovereign-runtime:8000` runs
the check suite (shape checks always; behavior checks once ready). Every
runtime image — real, mock, or host-agent-backed — must be green before
release. Golden fixtures live in `evals/sovereign_evals/conformance/fixtures/`.
