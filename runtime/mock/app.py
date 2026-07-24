"""Contract-faithful mock Sovereign Runtime.

Implements the full runtime contract (docs/runtime-contract.md) with a
simulated state machine and canned inference so Control, Gateway, and Evals
development never blocks on GPUs. Honest about being a mock: manifest reports
profile/backend "mock".

Knobs (env):
  SOVEREIGN_RUNTIME_CONFIG    runtime.yaml path (missing/broken → configuration_error)
  SOVEREIGN_RUNTIME_MANIFEST  where to write the manifest file
  MOCK_STATE_DELAY            seconds per transient state (default 0.5)
  MOCK_FAIL_MODE              none | configuration_error | degraded_embedding | runtime_error
  MOCK_EMBEDDING_DIM          embedding dimensions (default 384)
"""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import math
import os
import random
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from pathlib import Path

import yaml
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, Response, StreamingResponse
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, generate_latest

REQUESTS = Counter(
    "sovereign_requests_total",
    "Requests served by the mock runtime",
    ["role", "served_model"],
)
STATE_GAUGE = Gauge("sovereign_runtime_state", "Runtime state machine position", ["state"])

RUNTIME_VERSION = "0.1.0-dev"


def _now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


class MockRuntime:
    def __init__(self) -> None:
        self.state = "initializing"
        self.errors: list[dict] = []
        self.config: dict | None = None
        self.config_error: str | None = None
        self.api_key: str | None = None
        self.fail_mode = os.environ.get("MOCK_FAIL_MODE", "none")
        self.state_delay = float(os.environ.get("MOCK_STATE_DELAY", "0.5"))
        self.dim = int(os.environ.get("MOCK_EMBEDDING_DIM", "384"))
        self._load_config()

    # ── configuration ────────────────────────────────────────────────────

    def _load_config(self) -> None:
        path = os.environ.get("SOVEREIGN_RUNTIME_CONFIG", "")
        if not path or not Path(path).is_file():
            self.config_error = f"runtime config not found: {path!r}"
            return
        try:
            self.config = yaml.safe_load(Path(path).read_text())
        except yaml.YAMLError as exc:
            self.config_error = f"runtime config is not valid YAML: {exc}"
            return
        if not isinstance(self.config, dict) or "roles" not in self.config:
            self.config_error = "runtime config missing required 'roles' section"
            return
        key_env = (self.config.get("runtime") or {}).get("api_key_env")
        if key_env:
            self.api_key = os.environ.get(key_env) or None

    def _role_cfg(self, role: str) -> dict:
        return ((self.config or {}).get("roles") or {}).get(role) or {}

    @property
    def generation_alias(self) -> str:
        return self._role_cfg("generation").get("served_model_name", "assistant-large")

    @property
    def embedding_alias(self) -> str:
        return self._role_cfg("embedding").get("served_model_name", "embedding-gemma-default")

    # ── state machine ────────────────────────────────────────────────────

    def _set_state(self, state: str) -> None:
        self.state = state
        for known in (
            "initializing",
            "downloading",
            "compiling",
            "loading",
            "smoke_testing",
            "healthy",
            "degraded",
            "configuration_error",
            "runtime_error",
        ):
            STATE_GAUGE.labels(state=known).set(1.0 if known == state else 0.0)

    def _record_error(self, code: str, message: str, recoverable: bool, role: str | None = None) -> None:
        error: dict = {
            "code": code,
            "message": message,
            "recoverable": recoverable,
            "first_seen": _now(),
        }
        if role:
            error["role"] = role
        self.errors.append(error)

    async def lifecycle(self) -> None:
        if self.config_error or self.fail_mode == "configuration_error":
            message = self.config_error or "failure injected by MOCK_FAIL_MODE"
            self._record_error("CONFIG_INVALID", message, recoverable=True)
            self._set_state("configuration_error")
            self._write_manifest()
            return
        for state in ("initializing", "downloading", "loading", "smoke_testing"):
            self._set_state(state)
            await asyncio.sleep(self.state_delay)
        if self.fail_mode == "degraded_embedding":
            self._record_error(
                "MODEL_LOAD_FAILED", "failure injected by MOCK_FAIL_MODE", recoverable=True, role="embedding"
            )
            self._set_state("degraded")
        elif self.fail_mode == "runtime_error":
            self._record_error("ENGINE_DEAD", "failure injected by MOCK_FAIL_MODE", recoverable=False)
            self._set_state("runtime_error")
        else:
            self._set_state("healthy")
        self._write_manifest()

    def role_status(self, role: str) -> str:
        if self.state in ("configuration_error", "runtime_error"):
            return "unhealthy"
        if self.state == "degraded" and role == "embedding":
            return "unhealthy"
        if self.state == "healthy" or self.state == "degraded":
            return "healthy"
        return "loading"

    @property
    def ready(self) -> bool:
        return self.state == "healthy"

    # ── contract payloads ────────────────────────────────────────────────

    def manifest(self) -> dict:
        embedding_status = self.role_status("embedding")
        embedding: dict = {
            "enabled": True,
            "status": embedding_status,
            "task": "embed",
            "served_model_name": self.embedding_alias,
            "engine_model": self._role_cfg("embedding").get("model", "mock/embedding"),
            "revision": self._role_cfg("embedding").get("revision", "mock"),
            "dimensions": self.dim,
            "pooling": "last",
            "normalization": "l2",
            "modalities": ["text"],
        }
        if embedding_status == "unhealthy" and self.state == "degraded":
            embedding["error_code"] = "MODEL_LOAD_FAILED"
        return {
            "schema_version": "1.1",
            "runtime_id": f"sovereign-runtime-mock-{RUNTIME_VERSION}",
            "runtime_version": RUNTIME_VERSION,
            "vllm_version": "mock",
            "backend": "mock",
            "profile": "mock",
            "topology": "single_process_multi_role",
            "state": self.state,
            "api": {"openai_compatible": True, "port": PORT, "base_path": "/v1"},
            "roles": {
                "generation": {
                    "enabled": True,
                    "status": self.role_status("generation"),
                    "task": "generate",
                    "served_model_name": self.generation_alias,
                    "engine_model": self._role_cfg("generation").get("model", "mock/generation"),
                    "revision": self._role_cfg("generation").get("revision", "mock"),
                    "context_length": 32768,
                },
                "embedding": embedding,
            },
            "resource_policy": {
                "enforcement": "best_effort",
                "generation_memory_weight": self._role_cfg("generation").get("memory_weight", 82),
                "embedding_memory_weight": self._role_cfg("embedding").get("memory_weight", 18),
            },
            "accelerator": {"vendor": "none", "device_count": 0, "unified_memory": False},
            "health": {
                "status": "healthy" if self.state == "healthy" else self.state,
                "driver": "ok",
                "kernels": "ok",
                "metrics": "ok",
            },
        }

    def _write_manifest(self) -> None:
        path = os.environ.get("SOVEREIGN_RUNTIME_MANIFEST")
        if not path:
            return
        target = Path(path)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(self.manifest(), indent=2))


PORT = 8000
runtime = MockRuntime()


@asynccontextmanager
async def lifespan(app: FastAPI):
    task = asyncio.create_task(runtime.lifecycle())
    yield
    task.cancel()


app = FastAPI(title="Sovereign Runtime (mock)", lifespan=lifespan)


@app.middleware("http")
async def enforce_api_key(request: Request, call_next):
    if runtime.api_key and request.url.path.startswith("/v1/"):
        expected = f"Bearer {runtime.api_key}"
        if request.headers.get("Authorization") != expected:
            return JSONResponse(
                status_code=401,
                content={"error": {"message": "invalid API key", "type": "authentication_error"}},
            )
    return await call_next(request)


def _model_not_found(model: object) -> JSONResponse:
    return JSONResponse(
        status_code=404,
        content={
            "error": {
                "message": f"model {model!r} is not served by this role",
                "type": "invalid_request_error",
                "code": "model_not_found",
            }
        },
    )


# ── health and runtime endpoints ─────────────────────────────────────────


@app.get("/health/live")
def health_live() -> dict:
    return {"status": "alive", "state": runtime.state}


@app.get("/health/ready")
def health_ready() -> JSONResponse:
    body = {
        "ready": runtime.ready,
        "state": runtime.state,
        "required_roles": {
            "generation": runtime.role_status("generation") == "healthy",
            "embedding": runtime.role_status("embedding") == "healthy",
        },
    }
    return JSONResponse(status_code=200 if runtime.ready else 503, content=body)


@app.get("/health")
def health() -> dict:
    status = "healthy" if runtime.state == "healthy" else runtime.state
    embedding: dict = {
        "status": runtime.role_status("embedding"),
        "model_loaded": runtime.role_status("embedding") == "healthy",
        "served_model_name": runtime.embedding_alias,
        "modalities": ["text"],
    }
    if runtime.state == "degraded":
        embedding["error_code"] = "MODEL_LOAD_FAILED"
    return {
        "status": status,
        "state": runtime.state,
        "runtime_id": f"sovereign-runtime-mock-{RUNTIME_VERSION}",
        "roles": {
            "generation": {
                "status": runtime.role_status("generation"),
                "model_loaded": runtime.role_status("generation") == "healthy",
                "served_model_name": runtime.generation_alias,
            },
            "embedding": embedding,
        },
    }


@app.get("/runtime/manifest")
def runtime_manifest() -> dict:
    return runtime.manifest()


@app.get("/runtime/errors")
def runtime_errors() -> dict:
    return {"errors": runtime.errors}


@app.get("/metrics")
def metrics() -> Response:
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)


# ── OpenAI-compatible surface ────────────────────────────────────────────


@app.get("/v1/models")
def list_models() -> dict:
    data = []
    for role, alias in (("generation", runtime.generation_alias), ("embedding", runtime.embedding_alias)):
        if runtime.role_status(role) == "healthy":
            data.append({"id": alias, "object": "model", "owned_by": "sovereign"})
    return {"object": "list", "data": data}


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    if "model" not in body or "messages" not in body:
        return JSONResponse(
            status_code=400,
            content={"error": {"message": "model and messages are required", "type": "invalid_request_error"}},
        )
    if body["model"] != runtime.generation_alias or runtime.role_status("generation") != "healthy":
        return _model_not_found(body.get("model"))
    REQUESTS.labels(role="generation", served_model=runtime.generation_alias).inc()

    content = "This is the Sovereign mock runtime. All systems nominal."
    created = int(time.time())
    if body.get("stream"):

        async def sse():
            chunks = [{"role": "assistant"}, {"content": content[:20]}, {"content": content[20:]}]
            for delta in chunks:
                payload = {
                    "id": "chatcmpl-mock",
                    "object": "chat.completion.chunk",
                    "created": created,
                    "model": runtime.generation_alias,
                    "choices": [{"index": 0, "delta": delta, "finish_reason": None}],
                }
                yield f"data: {json.dumps(payload)}\n\n"
            done = {
                "id": "chatcmpl-mock",
                "object": "chat.completion.chunk",
                "created": created,
                "model": runtime.generation_alias,
                "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
            }
            yield f"data: {json.dumps(done)}\n\n"
            yield "data: [DONE]\n\n"

        return StreamingResponse(sse(), media_type="text/event-stream")

    return {
        "id": "chatcmpl-mock",
        "object": "chat.completion",
        "created": created,
        "model": runtime.generation_alias,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 8, "completion_tokens": 12, "total_tokens": 20},
    }


@app.post("/v1/completions")
async def completions(request: Request):
    body = await request.json()
    if "model" not in body or "prompt" not in body:
        return JSONResponse(
            status_code=400,
            content={"error": {"message": "model and prompt are required", "type": "invalid_request_error"}},
        )
    if body["model"] != runtime.generation_alias or runtime.role_status("generation") != "healthy":
        return _model_not_found(body.get("model"))
    REQUESTS.labels(role="generation", served_model=runtime.generation_alias).inc()
    return {
        "id": "cmpl-mock",
        "object": "text_completion",
        "created": int(time.time()),
        "model": runtime.generation_alias,
        "choices": [{"index": 0, "text": " a local-first AI appliance.", "finish_reason": "stop"}],
        "usage": {"prompt_tokens": 4, "completion_tokens": 6, "total_tokens": 10},
    }


def _embed(text: str, dim: int) -> list[float]:
    seed = int.from_bytes(hashlib.sha256(text.encode()).digest()[:8], "big")
    rng = random.Random(seed)
    vector = [rng.uniform(-1.0, 1.0) for _ in range(dim)]
    norm = math.sqrt(sum(v * v for v in vector))
    return [v / norm for v in vector]


@app.post("/v1/embeddings")
async def embeddings(request: Request):
    body = await request.json()
    if "model" not in body or "input" not in body:
        return JSONResponse(
            status_code=400,
            content={"error": {"message": "model and input are required", "type": "invalid_request_error"}},
        )
    if body["model"] != runtime.embedding_alias or runtime.role_status("embedding") != "healthy":
        return _model_not_found(body.get("model"))
    REQUESTS.labels(role="embedding", served_model=runtime.embedding_alias).inc()
    inputs = body["input"] if isinstance(body["input"], list) else [body["input"]]
    data = [
        {"object": "embedding", "index": i, "embedding": _embed(str(text), runtime.dim)}
        for i, text in enumerate(inputs)
    ]
    return {
        "object": "list",
        "data": data,
        "model": runtime.embedding_alias,
        "usage": {"prompt_tokens": len(inputs) * 4, "total_tokens": len(inputs) * 4},
    }


def main() -> None:
    global PORT
    import uvicorn

    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=int(os.environ.get("SOVEREIGN_RUNTIME_PORT", "8000")))
    args = parser.parse_args()
    PORT = args.port
    uvicorn.run(app, host=args.host, port=args.port, log_level="warning")


if __name__ == "__main__":
    main()
