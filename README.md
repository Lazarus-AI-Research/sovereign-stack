# Lazarus Sovereign Stack

A local-first AI appliance for small offices, workgroups, and customer-owned
hardware. One product, one control plane, one gateway, one runtime endpoint.

The full design specification lives in [design.md](design.md). v0.1 supports
Apple Silicon Macs (32 GB+) and Ubuntu 24.04 NVIDIA CUDA hosts (24 GB+ VRAM).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0/deploy/scripts/install.sh | bash
```

This verifies a signed release, detects the host profile, generates credentials,
starts the appliance, and runs its smoke gate. See
[`docs/installation.md`](docs/installation.md) for prerequisites, release
candidates, offline bundles, and uninstall behavior.

## Repository layout

This is the product monorepo. The Lazarus vLLM fork (`sovereign-vllm`) lives in
its own repository because it tracks upstream vLLM.

| Path | Contents |
| --- | --- |
| `deploy/` | The appliance deployment unit: Compose files, service configuration, install scripts. This subtree is what ships to a customer host. |
| `schemas/` | JSON Schemas — single source of truth for configs, manifests, and bundles. |
| `api/` | Sovereign Control OpenAPI specification. |
| `control/` | Sovereign Control — Go backend with embedded web frontend. |
| `docker-proxy/` | Sovereign Docker Proxy — restricted, allowlisted Docker access. |
| `evals/` | Sovereign Evals — smoke tests, benchmarks, and evaluation suites (Python). |
| `docs/` | Product and operations documentation. |
| `tests/` | Cross-component tests, including the release-gate integration suite. |

## Development deployment

```bash
cp deploy/.env.example deploy/.env   # then edit secrets
SOVEREIGN_SOURCE_DIR="$PWD" SOVEREIGN_SKIP_START=1 \
  ./deploy/scripts/install.sh
sovereign validate
sovereign up
```

The runtime profile for the current host can be suggested with:

```bash
./deploy/scripts/detect-hardware.sh
```

## Development

```bash
make build   # build Go services
make test    # Go tests + evals tests
make images  # build all four SovereignStack application images
```

Go modules (`control/`, `docker-proxy/`) are tied together by `go.work`.

## Versioning

`VERSION` is the single version stamp. Release CI builds and tags all
application images from one commit. Production deployments must use immutable
version tags (design.md §7).

The two-repository publication order and final clean-host checks are in the
[release runbook](docs/releasing.md). Platform gate evidence is recorded for
[Apple Metal](docs/metal-validation-results.md) and
[NVIDIA CUDA](docs/cuda-validation-results.md).
