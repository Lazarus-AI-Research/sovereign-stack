#!/usr/bin/env python3
"""Validate shipped schemas and every checked-in release/config contract."""

from __future__ import annotations

import json
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator, FormatChecker


ROOT = Path(__file__).resolve().parents[1]
SCHEMAS = ROOT / "schemas"
IMAGE_LOCK_KEYS = [
    ("SOVEREIGN_CONTROL_IMAGE", "sovereign-control"),
    ("SOVEREIGN_DOCKER_PROXY_IMAGE", "sovereign-docker-proxy"),
    ("SOVEREIGN_EVALS_IMAGE", "sovereign-evals"),
    ("SOVEREIGN_WORKSPACE_IMAGE", "sovereign-workspace"),
    ("SOVEREIGN_RUNTIME_CUDA_IMAGE", "sovereign-runtime-cuda"),
    ("SOVEREIGN_RUNTIME_METAL_IMAGE", "sovereign-runtime-metal"),
]


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def load_yaml(path: Path):
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def validate(instance_path: Path, schema_name: str, *, yaml_file: bool = True) -> None:
    schema = load_json(SCHEMAS / schema_name)
    instance = load_yaml(instance_path) if yaml_file else load_json(instance_path)
    Draft202012Validator(schema, format_checker=FormatChecker()).validate(instance)


def validate_image_lock(manifest_path: Path, lock_path: Path) -> None:
    manifest = load_json(manifest_path)
    images = {image["name"]: image for image in manifest["images"] if image["first_party"]}
    expected = {
        key: f"{images[name]['reference']}@{images[name]['digest']}"
        for key, name in IMAGE_LOCK_KEYS
    }
    actual: dict[str, str] = {}
    for line in lock_path.read_text(encoding="utf-8").splitlines():
        key, separator, value = line.partition("=")
        if not separator or not key or not value or key in actual:
            raise ValueError(f"invalid image lock line: {line!r}")
        actual[key] = value
    if actual != expected:
        raise ValueError("release/images.env does not match release/manifest.json")


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
        image_lock = ROOT / "release/images.env"
        if not image_lock.exists():
            raise ValueError("release manifest exists without release/images.env")
        validate_image_lock(manifest, image_lock)

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
