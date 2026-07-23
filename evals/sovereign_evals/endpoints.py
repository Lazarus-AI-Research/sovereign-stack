"""Where the stack lives, from the runner's point of view.

Defaults are the in-network compose service addresses; every value can be
overridden by env (the compose .env is passed to the evals container) or CLI
flags for running from the host.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field


def _env(name: str, default: str) -> str:
    return os.environ.get(name) or default


@dataclass
class Endpoints:
    runtime_base_url: str = field(
        default_factory=lambda: _env("RUNTIME_BASE_URL", "http://sovereign-runtime:8000")
    )
    runtime_api_key: str | None = field(
        default_factory=lambda: os.environ.get("SOVEREIGN_RUNTIME_API_KEY") or None
    )
    gateway_base_url: str = field(
        default_factory=lambda: _env("LITELLM_BASE_URL", "http://sovereign-gateway:4000")
    )
    gateway_api_key: str | None = field(
        default_factory=lambda: os.environ.get("LITELLM_MASTER_KEY") or None
    )
    embeddings_base_url: str = field(
        default_factory=lambda: _env(
            "SOVEREIGN_EMBEDDINGS_BASE_URL", "http://sovereign-embeddings:42666/v1"
        ).removesuffix("/v1")
    )
    embedding_model: str = field(
        default_factory=lambda: _env("EMBEDDING_MODEL_PREF", "embedding-gemma-default")
    )
    prometheus_base_url: str = field(
        default_factory=lambda: _env("PROMETHEUS_BASE_URL", "http://prometheus:9090")
    )
    pgvector_dsn: str = field(
        default_factory=lambda: _env(
            "PGVECTOR_CONNECTION_STRING",
            "postgresql://sovereign:change-me@postgres:5432/vectors",
        )
    )

    def base_url(self, target: str) -> str:
        if target == "gateway":
            return self.gateway_base_url
        if target == "embeddings":
            return self.embeddings_base_url
        return self.runtime_base_url

    def api_key(self, target: str) -> str | None:
        if target == "gateway":
            return self.gateway_api_key
        if target == "embeddings":
            return None
        return self.runtime_api_key
