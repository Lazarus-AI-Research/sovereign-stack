"""Smoke suite check implementations (design.md §19.1).

Each check verifies one product-path capability against a live stack. Checks
raise nothing: they return (passed, details) and the runner handles crashes.
"""

from __future__ import annotations

import json
import math
from collections.abc import Callable
from dataclasses import dataclass, field

import httpx

from sovereign_evals.endpoints import Endpoints
from sovereign_evals.schemas import validation_errors

Result = tuple[bool, str]
SKIPPED = "__skipped__"


@dataclass
class SuiteContext:
    endpoints: Endpoints
    _manifest: dict | None = field(default=None, repr=False)

    def client(self, target: str) -> httpx.Client:
        headers = {}
        if key := self.endpoints.api_key(target):
            headers["Authorization"] = f"Bearer {key}"
        return httpx.Client(base_url=self.endpoints.base_url(target), headers=headers, timeout=120.0)

    def manifest(self) -> dict:
        if self._manifest is None:
            with self.client("runtime") as client:
                self._manifest = client.get("/runtime/manifest").json()
        return self._manifest

    def role(self, name: str) -> dict:
        return (self.manifest().get("roles") or {}).get(name) or {}

    def generation_alias(self) -> str:
        return self.role("generation").get("served_model_name", "assistant-large")

    def embedding_alias(self) -> str:
        return self.role("embedding").get("served_model_name", "embedding-omni-default")


REGISTRY: dict[str, Callable[[SuiteContext, dict], Result]] = {}


def register(check_type: str):
    def decorator(fn):
        REGISTRY[check_type] = fn
        return fn

    return decorator


@register("runtime-liveness")
def runtime_liveness(ctx: SuiteContext, params: dict) -> Result:
    with ctx.client("runtime") as client:
        resp = client.get("/health/live")
    ok = resp.status_code == 200 and resp.json().get("status") == "alive"
    return ok, f"status {resp.status_code}"


@register("runtime-readiness")
def runtime_readiness(ctx: SuiteContext, params: dict) -> Result:
    with ctx.client("runtime") as client:
        resp = client.get("/health/ready")
    body = resp.json()
    ok = resp.status_code == 200 and body.get("ready") is True
    return ok, f"state={body.get('state')}"


@register("runtime-roles-loaded")
def runtime_roles_loaded(ctx: SuiteContext, params: dict) -> Result:
    with ctx.client("runtime") as client:
        roles = client.get("/health").json().get("roles") or {}
    problems = []
    for name in ("generation", "embedding"):
        entry = roles.get(name) or {}
        manifest_role = ctx.role(name)
        if not manifest_role.get("enabled"):
            continue
        if not entry.get("model_loaded"):
            problems.append(f"{name} not loaded (status={entry.get('status')})")
    return not problems, "; ".join(problems) or "all enabled roles loaded"


@register("runtime-manifest-valid")
def runtime_manifest_valid(ctx: SuiteContext, params: dict) -> Result:
    errors = validation_errors(ctx.manifest(), "runtime-manifest")
    return not errors, "; ".join(errors[:5]) or "valid"


@register("chat-completion")
def chat_completion(ctx: SuiteContext, params: dict) -> Result:
    target = params.get("target", "gateway")
    with ctx.client(target) as client:
        resp = client.post(
            "/v1/chat/completions",
            json={
                "model": ctx.generation_alias(),
                "messages": [{"role": "user", "content": "Reply with one short sentence."}],
                "max_tokens": 32,
            },
        )
    if resp.status_code != 200:
        return False, f"[{target}] status {resp.status_code}: {resp.text[:200]}"
    content = (resp.json().get("choices") or [{}])[0].get("message", {}).get("content")
    return bool(content and content.strip()), f"[{target}] ok"


@register("chat-streaming")
def chat_streaming(ctx: SuiteContext, params: dict) -> Result:
    target = params.get("target", "gateway")
    saw_chunk = saw_done = False
    with ctx.client(target) as client:
        with client.stream(
            "POST",
            "/v1/chat/completions",
            json={
                "model": ctx.generation_alias(),
                "messages": [{"role": "user", "content": "Count to three."}],
                "max_tokens": 32,
                "stream": True,
            },
        ) as resp:
            if resp.status_code != 200:
                return False, f"[{target}] status {resp.status_code}"
            for line in resp.iter_lines():
                if not line.startswith("data:"):
                    continue
                payload = line[len("data:") :].strip()
                if payload == "[DONE]":
                    saw_done = True
                elif payload:
                    json.loads(payload)
                    saw_chunk = True
    return saw_chunk and saw_done, f"[{target}] chunk={saw_chunk} done={saw_done}"


def _embedding_vector(ctx: SuiteContext, target: str, text: str) -> list[float] | str:
    with ctx.client(target) as client:
        resp = client.post(
            "/v1/embeddings", json={"model": ctx.embedding_alias(), "input": text}
        )
    if resp.status_code != 200:
        return f"[{target}] status {resp.status_code}: {resp.text[:200]}"
    vector = (resp.json().get("data") or [{}])[0].get("embedding")
    if not isinstance(vector, list) or not vector:
        return f"[{target}] no embedding vector in response"
    return vector


@register("text-embedding")
def text_embedding(ctx: SuiteContext, params: dict) -> Result:
    role = ctx.role("embedding")
    if not role.get("enabled"):
        return True, SKIPPED + "embedding role disabled"
    target = params.get("target", "gateway")
    vector = _embedding_vector(ctx, target, "sovereign smoke check")
    if isinstance(vector, str):
        return False, vector
    dims = role.get("dimensions")
    if dims and len(vector) != dims:
        return False, f"[{target}] dim {len(vector)} != manifest {dims}"
    if role.get("normalization") == "l2":
        norm = math.sqrt(sum(v * v for v in vector))
        if abs(norm - 1.0) > 1e-2:
            return False, f"[{target}] L2 norm {norm:.4f} != 1"
    return True, f"[{target}] dim={len(vector)}"


@register("image-embedding")
def image_embedding(ctx: SuiteContext, params: dict) -> Result:
    if "image" not in (ctx.role("embedding").get("modalities") or []):
        return True, SKIPPED + "image modality not enabled"
    return False, "image embedding check not implemented yet (M12)"


@register("audio-embedding")
def audio_embedding(ctx: SuiteContext, params: dict) -> Result:
    if "audio" not in (ctx.role("embedding").get("modalities") or []):
        return True, SKIPPED + "audio modality not enabled"
    return False, "audio embedding check not implemented yet (M12)"


@register("gateway-models")
def gateway_models(ctx: SuiteContext, params: dict) -> Result:
    with ctx.client("gateway") as client:
        resp = client.get("/v1/models")
    if resp.status_code != 200:
        return False, f"status {resp.status_code}"
    ids = {m.get("id") for m in resp.json().get("data", [])}
    expected = {ctx.generation_alias()}
    if ctx.role("embedding").get("enabled"):
        expected.add(ctx.embedding_alias())
    missing = expected - ids
    return not missing, f"missing: {sorted(missing)}" if missing else f"aliases: {sorted(expected)}"


@register("pgvector-roundtrip")
def pgvector_roundtrip(ctx: SuiteContext, params: dict) -> Result:
    if not ctx.role("embedding").get("enabled"):
        return True, SKIPPED + "embedding role disabled"
    import psycopg

    vector = _embedding_vector(ctx, params.get("target", "gateway"), "pgvector roundtrip")
    if isinstance(vector, str):
        return False, vector
    literal = "[" + ",".join(f"{v:.6f}" for v in vector) + "]"
    with psycopg.connect(ctx.endpoints.pgvector_dsn, connect_timeout=10) as conn:
        with conn.cursor() as cur:
            cur.execute("CREATE EXTENSION IF NOT EXISTS vector")
            cur.execute("DROP TABLE IF EXISTS sovereign_smoke_check")
            cur.execute(f"CREATE TABLE sovereign_smoke_check (id text, embedding vector({len(vector)}))")
            cur.execute(
                "INSERT INTO sovereign_smoke_check VALUES ('target', %s::vector)", (literal,)
            )
            cur.execute(
                "SELECT id FROM sovereign_smoke_check ORDER BY embedding <=> %s::vector LIMIT 1",
                (literal,),
            )
            row = cur.fetchone()
            cur.execute("DROP TABLE sovereign_smoke_check")
        conn.commit()
    ok = row is not None and row[0] == "target"
    return ok, f"nearest={row[0] if row else None}, dim={len(vector)}"


@register("metrics-available")
def metrics_available(ctx: SuiteContext, params: dict) -> Result:
    with ctx.client("runtime") as client:
        runtime_metrics = client.get("/metrics")
    if runtime_metrics.status_code != 200 or "# HELP" not in runtime_metrics.text:
        return False, "runtime /metrics not serving Prometheus text"
    resp = httpx.get(
        f"{ctx.endpoints.prometheus_base_url}/api/v1/targets",
        params={"state": "active"},
        timeout=30.0,
    )
    body = resp.json()
    if resp.status_code != 200 or body.get("status") != "success":
        return False, f"prometheus targets: {body}"
    targets = body.get("data", {}).get("activeTargets", [])
    if not targets:
        return False, "prometheus has no active scrape targets"
    down = sorted(
        target.get("labels", {}).get("job", target.get("scrapeUrl", "unknown"))
        for target in targets
        if target.get("health") != "up"
    )
    if down:
        return False, f"prometheus targets down: {down}"
    return True, f"runtime metrics + {len(targets)} prometheus targets up"
