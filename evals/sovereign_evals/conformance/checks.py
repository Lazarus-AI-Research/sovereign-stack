"""Individual conformance checks.

Each check receives a Context and returns a CheckResult. Shape checks run in
any runtime state; behavior checks are skipped (not failed) when the runtime
never becomes ready — the runner reports readiness separately.
"""

from __future__ import annotations

import json
import math
from collections.abc import Callable
from dataclasses import dataclass, field

import httpx

from sovereign_evals.schemas import validation_errors

STATES = {
    "initializing",
    "downloading",
    "compiling",
    "loading",
    "smoke_testing",
    "healthy",
    "degraded",
    "configuration_error",
    "runtime_error",
}


@dataclass
class Context:
    base_url: str
    api_key: str | None = None
    ready: bool = False
    manifest: dict | None = None
    client: httpx.Client = field(init=False)

    def __post_init__(self) -> None:
        headers = {}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        self.client = httpx.Client(base_url=self.base_url, headers=headers, timeout=60.0)

    def role(self, name: str) -> dict:
        return ((self.manifest or {}).get("roles") or {}).get(name) or {}


@dataclass
class CheckResult:
    check_id: str
    name: str
    passed: bool
    skipped: bool = False
    details: str = ""


Check = Callable[[Context], CheckResult]
CHECKS: list[tuple[str, str, bool, Check]] = []  # (id, name, needs_ready, fn)


def check(check_id: str, name: str, needs_ready: bool = False):
    def decorator(fn: Check) -> Check:
        CHECKS.append((check_id, name, needs_ready, fn))
        return fn

    return decorator


def _result(check_id: str, name: str, passed: bool, details: str = "") -> CheckResult:
    return CheckResult(check_id=check_id, name=name, passed=passed, details=details)


# ── Shape checks: valid in every runtime state ───────────────────────────────


@check("liveness", "GET /health/live is alive regardless of readiness")
def check_liveness(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/health/live")
    if resp.status_code != 200:
        return _result("liveness", check_liveness.__doc__, False, f"status {resp.status_code}")
    body = resp.json()
    ok = body.get("status") == "alive" and body.get("state") in STATES
    return _result("liveness", "liveness", ok, "" if ok else f"body: {body}")


@check("readiness-shape", "GET /health/ready has ready flag and valid state")
def check_readiness_shape(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/health/ready")
    if resp.status_code not in (200, 503):
        return _result("readiness-shape", "readiness", False, f"status {resp.status_code}")
    body = resp.json()
    ok = isinstance(body.get("ready"), bool) and body.get("state") in STATES
    if body.get("ready") is False and resp.status_code != 503:
        ok = False
    if body.get("ready") is True and resp.status_code != 200:
        ok = False
    return _result("readiness-shape", "readiness", ok, "" if ok else f"body: {body}")


@check("health-shape", "GET /health aggregates state and role health")
def check_health_shape(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/health")
    if resp.status_code != 200:
        return _result("health-shape", "health", False, f"status {resp.status_code}")
    body = resp.json()
    roles = body.get("roles") or {}
    ok = (
        body.get("state") in STATES
        and isinstance(body.get("status"), str)
        and "generation" in roles
        and "embedding" in roles
    )
    return _result("health-shape", "health", ok, "" if ok else f"body: {body}")


@check("manifest-valid", "GET /runtime/manifest validates against the schema")
def check_manifest(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/runtime/manifest")
    if resp.status_code != 200:
        return _result("manifest-valid", "manifest", False, f"status {resp.status_code}")
    manifest = resp.json()
    errors = validation_errors(manifest, "runtime-manifest")
    if not errors:
        ctx.manifest = manifest
    return _result("manifest-valid", "manifest", not errors, "; ".join(errors[:5]))


@check("errors-shape", "GET /runtime/errors lists structured errors")
def check_errors_shape(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/runtime/errors")
    if resp.status_code != 200:
        return _result("errors-shape", "errors", False, f"status {resp.status_code}")
    body = resp.json()
    errors = body.get("errors")
    if not isinstance(errors, list):
        return _result("errors-shape", "errors", False, f"body: {body}")
    for item in errors:
        if not (isinstance(item.get("code"), str) and isinstance(item.get("recoverable"), bool)):
            return _result("errors-shape", "errors", False, f"malformed item: {item}")
    return _result("errors-shape", "errors", True)


# ── Behavior checks: require a ready runtime ─────────────────────────────────


@check("models-aggregated", "GET /v1/models lists every loaded role's alias", needs_ready=True)
def check_models(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/v1/models")
    if resp.status_code != 200:
        return _result("models-aggregated", "models", False, f"status {resp.status_code}")
    ids = {m.get("id") for m in resp.json().get("data", [])}
    expected = {
        role.get("served_model_name")
        for role in (ctx.manifest or {}).get("roles", {}).values()
        if role.get("enabled") and role.get("status") == "healthy" and role.get("served_model_name")
    }
    missing = expected - ids
    return _result("models-aggregated", "models", not missing, f"missing: {sorted(missing)}" if missing else "")


@check("chat-completion", "POST /v1/chat/completions returns content", needs_ready=True)
def check_chat(ctx: Context) -> CheckResult:
    model = ctx.role("generation").get("served_model_name")
    resp = ctx.client.post(
        "/v1/chat/completions",
        json={
            "model": model,
            "messages": [{"role": "user", "content": "Reply with the single word: sovereign"}],
            "max_tokens": 16,
        },
    )
    if resp.status_code != 200:
        return _result("chat-completion", "chat", False, f"status {resp.status_code}: {resp.text[:200]}")
    content = (resp.json().get("choices") or [{}])[0].get("message", {}).get("content")
    ok = isinstance(content, str) and len(content.strip()) > 0
    return _result("chat-completion", "chat", ok, "" if ok else "empty content")


@check("chat-streaming", "streaming chat emits SSE terminated by [DONE]", needs_ready=True)
def check_streaming(ctx: Context) -> CheckResult:
    model = ctx.role("generation").get("served_model_name")
    saw_chunk = saw_done = False
    with ctx.client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": model,
            "messages": [{"role": "user", "content": "Count to three."}],
            "max_tokens": 32,
            "stream": True,
        },
    ) as resp:
        if resp.status_code != 200:
            return _result("chat-streaming", "streaming", False, f"status {resp.status_code}")
        for line in resp.iter_lines():
            if not line.startswith("data:"):
                continue
            payload = line[len("data:") :].strip()
            if payload == "[DONE]":
                saw_done = True
            elif payload:
                json.loads(payload)
                saw_chunk = True
    ok = saw_chunk and saw_done
    return _result("chat-streaming", "streaming", ok, f"chunk={saw_chunk} done={saw_done}")


@check("text-completion", "POST /v1/completions returns text", needs_ready=True)
def check_completions(ctx: Context) -> CheckResult:
    model = ctx.role("generation").get("served_model_name")
    resp = ctx.client.post(
        "/v1/completions",
        json={"model": model, "prompt": "Sovereign Stack is", "max_tokens": 8},
    )
    if resp.status_code != 200:
        return _result("text-completion", "completions", False, f"status {resp.status_code}")
    text = (resp.json().get("choices") or [{}])[0].get("text")
    ok = isinstance(text, str)
    return _result("text-completion", "completions", ok, "" if ok else "no text")


@check("text-embedding", "POST /v1/embeddings matches manifest dims and norm", needs_ready=True)
def check_embeddings(ctx: Context) -> CheckResult:
    role = ctx.role("embedding")
    if not role.get("enabled"):
        return CheckResult(
            "text-embedding", "embeddings", passed=True, skipped=True, details="embedding role disabled"
        )
    resp = ctx.client.post(
        "/v1/embeddings",
        json={"model": role.get("served_model_name"), "input": "sovereign stack"},
    )
    if resp.status_code != 200:
        return _result("text-embedding", "embeddings", False, f"status {resp.status_code}: {resp.text[:200]}")
    vector = (resp.json().get("data") or [{}])[0].get("embedding")
    if not isinstance(vector, list) or not vector:
        return _result("text-embedding", "embeddings", False, "no embedding vector")
    dims = role.get("dimensions")
    if dims and len(vector) != dims:
        return _result("text-embedding", "embeddings", False, f"dim {len(vector)} != manifest {dims}")
    if role.get("normalization") == "l2":
        norm = math.sqrt(sum(v * v for v in vector))
        if abs(norm - 1.0) > 1e-2:
            return _result("text-embedding", "embeddings", False, f"L2 norm {norm:.4f} != 1")
    return _result("text-embedding", "embeddings", True)


@check("role-mismatch-404", "wrong-role model alias returns 404", needs_ready=True)
def check_role_mismatch(ctx: Context) -> CheckResult:
    # The embedding alias is the canonical wrong-role probe; fall back to a
    # nonexistent alias for single-role runtimes — 404 required either way.
    embed_alias = ctx.role("embedding").get("served_model_name") or "no-such-model"
    resp = ctx.client.post(
        "/v1/chat/completions",
        json={"model": embed_alias, "messages": [{"role": "user", "content": "hi"}]},
    )
    ok = resp.status_code == 404
    return _result("role-mismatch-404", "role mismatch", ok, f"status {resp.status_code}")


@check("invalid-request", "malformed request returns a 4xx, not a crash", needs_ready=True)
def check_invalid_request(ctx: Context) -> CheckResult:
    resp = ctx.client.post("/v1/chat/completions", json={})
    ok = 400 <= resp.status_code < 500
    return _result("invalid-request", "invalid request", ok, f"status {resp.status_code}")


@check("auth-enforced", "bad API key is rejected on /v1", needs_ready=True)
def check_auth(ctx: Context) -> CheckResult:
    if not ctx.api_key:
        return CheckResult("auth-enforced", "auth", True, skipped=True, details="no api key configured")
    resp = httpx.post(
        f"{ctx.base_url}/v1/chat/completions",
        headers={"Authorization": "Bearer definitely-wrong-key"},
        json={
            "model": ctx.role("generation").get("served_model_name"),
            "messages": [{"role": "user", "content": "hi"}],
        },
        timeout=30.0,
    )
    ok = resp.status_code in (401, 403)
    return _result("auth-enforced", "auth", ok, f"status {resp.status_code}")


@check("metrics-exposed", "GET /metrics serves Prometheus text")
def check_metrics(ctx: Context) -> CheckResult:
    resp = ctx.client.get("/metrics")
    if resp.status_code != 200:
        return _result("metrics-exposed", "metrics", False, f"status {resp.status_code}")
    ok = "# HELP" in resp.text or "# TYPE" in resp.text
    return _result("metrics-exposed", "metrics", ok, "" if ok else "no prometheus exposition markers")
