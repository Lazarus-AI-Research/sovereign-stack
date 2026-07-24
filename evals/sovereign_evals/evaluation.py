"""Retrieval and cross-modal functional evaluations for the v0.1 gate."""

from __future__ import annotations

import math
import uuid

from sovereign_evals.smoke.checks import (
    SKIPPED,
    SuiteContext,
    _png_data_uri,
    _validate_modality_vector,
    register,
)


def _vectors(ctx: SuiteContext, texts: list[str], target: str = "gateway") -> list[list[float]]:
    with ctx.client(target) as client:
        response = client.post(
            "/v1/embeddings", json={"model": ctx.embedding_alias(), "input": texts}
        )
    response.raise_for_status()
    data = response.json().get("data") or []
    vectors = [row.get("embedding") for row in data]
    if len(vectors) != len(texts) or any(not isinstance(v, list) or not v for v in vectors):
        raise ValueError("embedding response did not contain one vector per input")
    return vectors


@register("retrieval-quality")
def retrieval_quality(ctx: SuiteContext, params: dict):
    """Prove the shipped embedding path and pgvector return the intended top-1 row."""
    import psycopg

    documents = params.get("documents") or [
        "sovereign retrieval sentinel alpha local private AI",
        "postgres databases store relational rows",
        "blue whales are large ocean mammals",
    ]
    query = params.get("query") or documents[0]
    expected = int(params.get("expected_index", 0))
    vectors = _vectors(ctx, documents + [query], params.get("target", "gateway"))
    query_vector = vectors[-1]
    table = f"sovereign_retrieval_{uuid.uuid4().hex}"
    literal = lambda values: "[" + ",".join(f"{value:.8f}" for value in values) + "]"
    with psycopg.connect(ctx.endpoints.pgvector_dsn, connect_timeout=10) as connection:
        with connection.cursor() as cursor:
            cursor.execute("CREATE EXTENSION IF NOT EXISTS vector")
            cursor.execute(f'CREATE TABLE "{table}" (id integer, embedding vector)')
            for index, vector in enumerate(vectors[:-1]):
                cursor.execute(
                    f'INSERT INTO "{table}" VALUES (%s, %s::vector)',
                    (index, literal(vector)),
                )
            cursor.execute(
                f'SELECT id, embedding <=> %s::vector AS distance FROM "{table}" ORDER BY distance LIMIT 1',
                (literal(query_vector),),
            )
            row = cursor.fetchone()
            cursor.execute(f'DROP TABLE "{table}"')
        connection.commit()
    ok = row is not None and row[0] == expected
    metrics = {"expected": expected, "top1": row[0] if row else None, "distance": row[1] if row else None}
    return ok, f"top1={metrics['top1']} expected={expected}", metrics


def _cosine(left: list[float], right: list[float]) -> float:
    numerator = sum(a * b for a, b in zip(left, right))
    denominator = math.sqrt(sum(a * a for a in left)) * math.sqrt(sum(b * b for b in right))
    return numerator / denominator if denominator else 0.0


@register("cross-modal-retrieval")
def cross_modal_retrieval(ctx: SuiteContext, params: dict):
    """Rank a red image over a blue image for a text query using one vector space."""
    if "image" not in (ctx.role("embedding").get("modalities") or []):
        return True, SKIPPED + "image modality not enabled"
    target = params.get("target", "runtime")
    text_vector = _vectors(ctx, ["a solid red square"], target)[0]

    image_vectors = []
    with ctx.client(target) as client:
        for color in ((230, 20, 20), (20, 20, 230)):
            response = client.post(
                "/v1/embeddings",
                json={
                    "model": ctx.embedding_alias(),
                    "messages": [
                        {
                            "role": "user",
                            "content": [
                                {"type": "image_url", "image_url": {"url": _png_data_uri(color)}}
                            ],
                        }
                    ],
                },
            )
            valid, details = _validate_modality_vector(ctx, target, "image", response)
            if not valid:
                return False, details
            image_vectors.append((response.json().get("data") or [{}])[0]["embedding"])
    red_score = _cosine(text_vector, image_vectors[0])
    blue_score = _cosine(text_vector, image_vectors[1])
    metrics = {"red_score": round(red_score, 6), "blue_score": round(blue_score, 6)}
    return red_score > blue_score, f"red={red_score:.4f} blue={blue_score:.4f}", metrics
