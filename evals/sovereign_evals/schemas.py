"""Locate, load, and validate against the Sovereign contract schemas.

The schemas at the repository root (schemas/) are the single source of truth.
In a checkout we walk upward from this file until we find them; in the evals
image they are copied to /app/schemas. Override with SOVEREIGN_SCHEMAS_DIR.
"""

from __future__ import annotations

import json
import os
from functools import cache
from pathlib import Path
from typing import Any

import jsonschema


def schemas_dir() -> Path:
    env = os.environ.get("SOVEREIGN_SCHEMAS_DIR")
    if env:
        return Path(env)
    here = Path(__file__).resolve()
    for parent in here.parents:
        candidate = parent / "schemas"
        if (candidate / "runtime-manifest.schema.json").is_file():
            return candidate
    raise FileNotFoundError(
        "schemas/ directory not found in any parent directory; set SOVEREIGN_SCHEMAS_DIR"
    )


@cache
def load_schema(name: str) -> dict[str, Any]:
    path = schemas_dir() / f"{name}.schema.json"
    return json.loads(path.read_text())


def validation_errors(instance: Any, schema_name: str) -> list[str]:
    """Return human-readable validation errors; empty list means valid."""
    validator = jsonschema.Draft202012Validator(load_schema(schema_name))
    return [
        f"{'/'.join(str(p) for p in error.absolute_path) or '<root>'}: {error.message}"
        for error in validator.iter_errors(instance)
    ]
