#!/usr/bin/env python3
"""Generate the immutable release manifest from reviewed source pins.

First-party digest files contain one ``sha256:...`` value and are produced by
the release workflow after pushing each image. Third-party digests and model
revisions come from release-source.json, which is reviewed with the code.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path


DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
REGISTRY = "ghcr.io/lazarus-ai-research"
IMAGE_LOCK_KEYS = [
    ("SOVEREIGN_CONTROL_IMAGE", "sovereign-control"),
    ("SOVEREIGN_DOCKER_PROXY_IMAGE", "sovereign-docker-proxy"),
    ("SOVEREIGN_EVALS_IMAGE", "sovereign-evals"),
    ("SOVEREIGN_WORKSPACE_IMAGE", "sovereign-workspace"),
    ("SOVEREIGN_RUNTIME_CUDA_IMAGE", "sovereign-runtime-cuda"),
    ("SOVEREIGN_RUNTIME_METAL_IMAGE", "sovereign-runtime-metal"),
    ("SOVEREIGN_EMBEDDINGS_IMAGE", "sovereign-embeddings"),
]


def read_digest(path: Path) -> str:
    value = path.read_text(encoding="utf-8").strip()
    if not DIGEST.fullmatch(value):
        raise ValueError(f"invalid image digest in {path}: {value!r}")
    return value


def checked_component(source: dict, name: str, *, signed: bool = False) -> dict:
    component = dict(source.get(name) or {})
    required = {"version", "artifact", "url", "sha256", "bytes"}
    if signed:
        required.update({"signature_url", "signer_identity_regexp"})
    missing = sorted(required - component.keys())
    if missing:
        raise ValueError(f"release source {name} is missing: {', '.join(missing)}")
    if not re.fullmatch(r"[0-9a-f]{64}", str(component["sha256"])):
        raise ValueError(f"release source {name} has an invalid sha256")
    if not isinstance(component["bytes"], int) or component["bytes"] <= 0:
        raise ValueError(f"release source {name} has an invalid byte size")
    if not str(component["url"]).endswith("/" + str(component["artifact"])):
        raise ValueError(f"release source {name} URL does not name its artifact")
    if signed and component["signature_url"] != component["url"] + ".sigstore.json":
        raise ValueError(f"release source {name} signature URL does not match its artifact")
    return component


def schema_inventory(root: Path) -> list[dict]:
    result = []
    for path in sorted(root.glob("*.json")):
        raw = path.read_bytes()
        result.append({
            "name": path.name,
            "sha256": hashlib.sha256(raw).hexdigest(),
            "bytes": len(raw),
        })
    if not result:
        raise ValueError(f"no JSON schemas found under {root}")
    return result


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, default=Path("release/release-source.json"))
    parser.add_argument("--digest-dir", type=Path, required=True)
    parser.add_argument("--stack-commit", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--image-lock-output", type=Path)
    parser.add_argument("--schema-dir", type=Path, default=Path("schemas"))
    args = parser.parse_args()

    source = json.loads(args.source.read_text(encoding="utf-8"))
    if source.get("schema_version") != "1.3":
        raise ValueError("release source must use schema_version 1.3")
    if not COMMIT.fullmatch(args.stack_commit):
        raise ValueError("--stack-commit must be a full 40-character commit")
    if not COMMIT.fullmatch(source["runtime_commit"]):
        raise ValueError("release source has an invalid runtime_commit")

    version = source["version"]
    runtime_version = source.get("runtime_version", version)
    if not re.fullmatch(r"0\.1\.0(?:-rc\.[0-9]+)?", runtime_version):
        raise ValueError("release source has an invalid runtime_version")
    first_party = [
        ("sovereign-control", f"{REGISTRY}/sovereign-control:{version}", ["linux/amd64", "linux/arm64"]),
        ("sovereign-docker-proxy", f"{REGISTRY}/sovereign-docker-proxy:{version}", ["linux/amd64", "linux/arm64"]),
        ("sovereign-evals", f"{REGISTRY}/sovereign-evals:{version}", ["linux/amd64", "linux/arm64"]),
        ("sovereign-workspace", f"{REGISTRY}/sovereign-workspace:{version}", ["linux/amd64", "linux/arm64"]),
        ("sovereign-runtime-cuda", f"{REGISTRY}/sovereign-runtime:cuda-x86_64-{runtime_version}", ["linux/amd64"]),
        ("sovereign-runtime-metal", f"{REGISTRY}/sovereign-runtime:metal-arm64-{runtime_version}", ["linux/arm64"]),
        ("sovereign-embeddings", f"{REGISTRY}/sovereign-embeddings:{version}", ["linux/amd64"]),
    ]
    images = [
        {
            "name": name,
            "reference": reference,
            "digest": read_digest(args.digest_dir / name),
            "platforms": platforms,
            "first_party": True,
        }
        for name, reference, platforms in first_party
    ]

    # All deployed third-party services must work on both certified hosts. The
    # AnythingLLM entry records the pinned workspace base-image provenance.
    for image in source["third_party_images"]:
        reference, digest = image["reference"].rsplit("@", 1)
        digest = f"{digest}"
        if not DIGEST.fullmatch(digest):
            raise ValueError(f"third-party image {image['name']} is not digest pinned")
        images.append(
            {
                "name": image["name"],
                "reference": reference,
                "digest": digest,
                "platforms": ["linux/amd64", "linux/arm64"],
                "first_party": False,
            }
        )

    metal_agent = checked_component(source, "metal_agent", signed=True)
    embedding_runtime = checked_component(source, "embedding_runtime")
    engine_probe = dict(source.get("engine_probe") or {})
    required_probe_fields = {
        "image", "container_port", "minimum_api_version", "minimum_free_kib"
    }
    if set(engine_probe) != required_probe_fields:
        raise ValueError("release source must define the complete engine probe contract")
    if not re.fullmatch(r"[A-Za-z0-9._/:@-]+@sha256:[0-9a-f]{64}", str(engine_probe["image"])):
        raise ValueError("release source engine probe image must be digest pinned")
    if not isinstance(engine_probe["container_port"], int) or not 1 <= engine_probe["container_port"] <= 65535:
        raise ValueError("release source engine probe container port is invalid")
    if not re.fullmatch(r"[0-9]+\.[0-9]+", str(engine_probe["minimum_api_version"])):
        raise ValueError("release source engine probe API version is invalid")
    if not isinstance(engine_probe["minimum_free_kib"], int) or engine_probe["minimum_free_kib"] < 1048576:
        raise ValueError("release source engine probe free-space requirement is invalid")
    installer_dependencies = source.get("installer_dependencies") or {}
    metal_dependencies = installer_dependencies.get("metal-arm64") or {}
    required_metal_dependencies = {
        "cosign", "colima", "colima_disk_image", "lima", "docker_cli", "docker_compose"
    }
    if set(metal_dependencies) != required_metal_dependencies:
        raise ValueError("release source must pin the complete metal-arm64 installer toolchain")
    checked_dependencies = {}
    for name in sorted(required_metal_dependencies):
        dependency = checked_component(metal_dependencies, name, signed=name == "docker_compose")
        if dependency.get("format") not in {"executable", "tar.gz", "raw.gz"}:
            raise ValueError(f"release source installer dependency {name} has an invalid format")
        checked_dependencies[name] = dependency
    model_fields = {"id", "repository", "revision", "profiles", "role", "artifact", "artifacts", "sha256", "modalities"}
    models = [{key: value for key, value in model.items() if key in model_fields} for model in source["models"]]
    manifest = {
        "schema_version": "1.3",
        "version": version,
        "stack_commit": args.stack_commit,
        "runtime_version": runtime_version,
        "runtime_commit": source["runtime_commit"],
        "created_at": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
        "supported_profiles": source["supported_profiles"],
        "images": images,
        "models": models,
        "metal_agent": metal_agent,
        "embedding_runtime": embedding_runtime,
        "engine_probe": engine_probe,
        "installer_dependencies": {"metal-arm64": checked_dependencies},
        "schemas": schema_inventory(args.schema_dir),
        "assets": source.get("assets", []),
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    if args.image_lock_output:
        by_name = {image["name"]: image for image in images if image["first_party"]}
        lines = [
            f"{key}={by_name[name]['reference']}@{by_name[name]['digest']}"
            for key, name in IMAGE_LOCK_KEYS
        ]
        args.image_lock_output.parent.mkdir(parents=True, exist_ok=True)
        args.image_lock_output.write_text("\n".join(lines) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
