"""Quick benchmark checks (design.md §19.2).

Latency/throughput measurement over the same product paths the smoke suite
uses. Numbers are reported in the check's metrics dict and rendered into
reports; §2.10: no minimum is enforced — administrators judge the results.
"""

from __future__ import annotations

import concurrent.futures
import statistics
import time

from sovereign_evals.smoke.checks import SuiteContext, register


def _percentiles(samples_ms: list[float]) -> dict:
    ordered = sorted(samples_ms)
    return {
        "p50_ms": round(statistics.median(ordered), 1),
        "p95_ms": round(ordered[min(len(ordered) - 1, int(len(ordered) * 0.95))], 1),
        "mean_ms": round(statistics.fmean(ordered), 1),
    }


def _timed_chat(ctx: SuiteContext, target: str, max_tokens: int) -> float:
    started = time.monotonic()
    with ctx.client(target) as client:
        resp = client.post(
            "/v1/chat/completions",
            json={
                "model": ctx.generation_alias(),
                "messages": [{"role": "user", "content": "Write one short sentence about local AI."}],
                "max_tokens": max_tokens,
            },
        )
        resp.raise_for_status()
    return (time.monotonic() - started) * 1000


def _timed_embedding(ctx: SuiteContext, target: str, batch: int) -> float:
    inputs = [f"benchmark document {i}" for i in range(batch)]
    started = time.monotonic()
    with ctx.client(target) as client:
        resp = client.post(
            "/v1/embeddings", json={"model": ctx.embedding_alias(), "input": inputs}
        )
        resp.raise_for_status()
    return (time.monotonic() - started) * 1000


@register("benchmark-generation")
def benchmark_generation(ctx: SuiteContext, params: dict):
    target = params.get("target", "gateway")
    requests = int(params.get("requests", 8))
    concurrency = int(params.get("concurrency", 2))
    max_tokens = int(params.get("max_tokens", 64))

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        samples = list(pool.map(lambda _: _timed_chat(ctx, target, max_tokens), range(requests)))
    metrics = _percentiles(samples) | {"requests": requests, "concurrency": concurrency}
    return True, f"[{target}] {metrics}", metrics


@register("benchmark-embedding")
def benchmark_embedding(ctx: SuiteContext, params: dict):
    target = params.get("target", "gateway")
    batch = int(params.get("batch_size", 16))
    batches = int(params.get("batches", 5))

    samples = [_timed_embedding(ctx, target, batch) for _ in range(batches)]
    total_items = batch * batches
    total_seconds = sum(samples) / 1000
    metrics = _percentiles(samples) | {
        "batch_size": batch,
        "batches": batches,
        "items_per_second": round(total_items / total_seconds, 1) if total_seconds else 0,
    }
    return True, f"[{target}] {metrics}", metrics


@register("benchmark-mixed")
def benchmark_mixed(ctx: SuiteContext, params: dict):
    """§19.2 mixed generation and embedding pressure; flags starvation."""
    target = params.get("target", "gateway")
    generation_requests = int(params.get("generation_requests", 4))
    embedding_batches = int(params.get("embedding_batches", 4))

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        generation_futures = [
            pool.submit(_timed_chat, ctx, target, 32) for _ in range(generation_requests)
        ]
        embedding_futures = [
            pool.submit(_timed_embedding, ctx, target, 8) for _ in range(embedding_batches)
        ]
        generation_samples = [f.result() for f in generation_futures]
        embedding_samples = [f.result() for f in embedding_futures]

    metrics = {
        "generation": _percentiles(generation_samples),
        "embedding": _percentiles(embedding_samples),
    }
    return True, f"[{target}] {metrics}", metrics
