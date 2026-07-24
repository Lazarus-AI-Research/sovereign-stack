#!/usr/bin/env python3
"""Generate the immutable release manifest from reviewed source pins.

First-party digest files contain one ``sha256:...`` value and are produced by
the release workflow after pushing each image. Third-party digests and model
revisions come from release-source.json, which is reviewed with the code.
"""

from __future__ import annotations

import argparse
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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, default=Path("release/release-source.json"))
    parser.add_argument("--digest-dir", type=Path, required=True)
    parser.add_argument("--stack-commit", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--image-lock-output", type=Path)
    args = parser.parse_args()

    source = json.loads(args.source.read_text(encoding="utf-8"))
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

    model_fields = {"id", "repository", "revision", "profiles", "role", "artifact", "sha256", "modalities"}
    models = [{key: value for key, value in model.items() if key in model_fields} for model in source["models"]]
    manifest = {
        "schema_version": "1.0",
        "version": version,
        "stack_commit": args.stack_commit,
        "runtime_version": runtime_version,
        "runtime_commit": source["runtime_commit"],
        "created_at": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
        "supported_profiles": source["supported_profiles"],
        "images": images,
        "models": models,
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
