# Lazarus Sovereign Stack

A local-first AI appliance for small offices, workgroups, and customer-owned
hardware. One product, one control plane, one gateway, one runtime endpoint.

The full implementation and design specification lives in [design.md](design.md).

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

## Quick start (development)

```bash
cp deploy/.env.example deploy/.env   # then edit secrets
./deploy/scripts/sovereign validate  # check compose configuration
./deploy/scripts/sovereign up        # SOVEREIGN_PROFILE=cuda|metal|... (default: cpu)
```

The runtime profile for the current host can be suggested with:

```bash
./deploy/scripts/detect-hardware.sh
```

## Development

```bash
make build   # build Go services
make test    # Go tests + evals tests
make images  # build sovereign-control, sovereign-docker-proxy, sovereign-evals images
```

Go modules (`control/`, `docker-proxy/`) are tied together by `go.work`.

## Versioning

`VERSION` is the single version stamp. Release CI builds and tags all
application images from one commit. Production deployments must use immutable
version tags (design.md §7).
