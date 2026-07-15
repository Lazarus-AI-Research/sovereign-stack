#!/usr/bin/env python3
"""Validate shipped schemas and every checked-in release/config contract."""

from __future__ import annotations

import json
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator, FormatChecker


ROOT = Path(__file__).resolve().parents[1]
SCHEMAS = ROOT / "schemas"


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def load_yaml(path: Path):
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def validate(instance_path: Path, schema_name: str, *, yaml_file: bool = True) -> None:
    schema = load_json(SCHEMAS / schema_name)
    instance = load_yaml(instance_path) if yaml_file else load_json(instance_path)
    Draft202012Validator(schema, format_checker=FormatChecker()).validate(instance)


def main() -> None:
    for path in sorted(SCHEMAS.glob("*.json")):
        Draft202012Validator.check_schema(load_json(path))

    contracts = [
        (ROOT / "deploy/config/branding.yaml", "branding.schema.json"),
        (ROOT / "deploy/config/feature-flags.yaml", "feature-flags.schema.json"),
        (ROOT / "deploy/config/embedding-profiles.yaml", "embedding-profile.schema.json"),
        (ROOT / "deploy/config/embedding-profiles.metal.yaml", "embedding-profile.schema.json"),
        (ROOT / "deploy/config/model-registry.yaml", "model-registry.schema.json"),
        (ROOT / "deploy/config/model-registry.metal.yaml", "model-registry.schema.json"),
        (ROOT / "deploy/config/runtime.yaml", "runtime-config.schema.json"),
        (ROOT / "deploy/config/runtime.metal.yaml", "runtime-config.schema.json"),
    ]
    for path, schema in contracts:
        validate(path, schema)
    for suite in sorted((ROOT / "evals/suites").glob("*.yaml")):
        validate(suite, "eval-suite.schema.json")

    manifest = ROOT / "release/manifest.json"
    if manifest.exists():
        validate(manifest, "release-manifest.schema.json", yaml_file=False)

    source = load_json(ROOT / "release/release-source.json")
    version = (ROOT / "VERSION").read_text(encoding="utf-8").strip()
    if source["version"] != version:
        raise ValueError(f"release source version {source['version']} != VERSION {version}")

    openapi = load_yaml(ROOT / "api/sovereign-control.openapi.yaml")
    if openapi.get("openapi") != "3.1.0" or len(openapi.get("paths", {})) < 50:
        raise ValueError("OpenAPI contract is incomplete")

    print(f"validated {len(list(SCHEMAS.glob('*.json')))} schemas and all shipped contracts")


if __name__ == "__main__":
    main()
