# Lazarus SovereignStack

SovereignStack is a local-first AI appliance for small offices, workgroups, and
customer-owned hardware. It combines a chat and knowledge workspace, an
operator control plane, an OpenAI-compatible model gateway, local inference,
vector search, evaluations, observability, and backups in one managed stack.

The current public preview is
[`v0.1.0-rc.3`](https://github.com/Lazarus-AI-Research/sovereign-stack/releases/tag/v0.1.0-rc.3).
It supports Apple Silicon Macs and Ubuntu NVIDIA CUDA hosts.
Release-candidate users should pin both the installer URL and
`SOVEREIGN_VERSION` exactly as shown below.

## What is included

- **Sovereign Workspace** for chat, documents, and retrieval-augmented
  generation.
- **Sovereign Control** for models, provider credentials, embedding profiles,
  evaluations, backups, offline bundles, and system health.
- **Sovereign Runtime** for local generation and embeddings, with certified
  Metal and CUDA profiles.
- **LiteLLM and pgvector** for stable model aliases, gateway policy, and local
  vector storage.
- **Prometheus, Grafana, Loki, OpenTelemetry, and Phoenix** for local
  operational visibility.
- Signed release archives, signed first-party images, immutable image digests,
  generated credentials, and a restricted Docker control proxy.

## Supported hosts

| Profile | Host requirements | v0.1 capability |
| --- | --- | --- |
| `metal-arm64` | Apple Silicon Mac, 32 GB+ unified memory, Docker Desktop with Compose v2 | Text chat, text embeddings, and pgvector RAG through a signed Metal host agent |
| `cuda-x86_64` | Ubuntu 24.04 x86_64, NVIDIA GPU with 24 GB+ VRAM, Docker Engine with Compose v2, NVIDIA driver, and NVIDIA Container Toolkit | Text chat, text/image/audio embeddings, and cross-modal retrieval |

Both profiles require `curl`, `tar`, `openssl`, a running Docker daemon, at
least 20 GB free disk, and network access for the initial install. At least
60 GB free disk is recommended when retaining model weights. The installer
checks these prerequisites but never installs or changes Docker, GPU drivers,
or operating-system packages.

See [hardware profiles](docs/hardware-profiles.md) for the exact support matrix.

## Install

Run the same command on a supported Mac or CUDA host. The installer detects the
profile, verifies the signed release, generates credentials, pulls the exact
digest-pinned images, starts the appliance, waits for the runtime, and runs the
smoke suite.

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.3/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.3 bash
```

The first run can take a while because it downloads a pinned signature verifier,
container images, and model weights. Leave the installer running until it
prints the local URL and credentials path. Set `HF_TOKEN` in the installer
environment before running the command only when a configured model repository
requires authentication.

To choose a profile explicitly:

```bash
# Apple Silicon
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.3/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.3 bash -s -- --profile metal-arm64

# Ubuntu NVIDIA CUDA
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.3/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.3 bash -s -- --profile cuda-x86_64
```

The default install locations are:

| Path | Purpose |
| --- | --- |
| `~/.sovereign` | Releases, configuration, credentials, models, databases, reports, backups, and appliance state |
| `~/.local/bin/sovereign` | Management CLI |
| `~/.sovereign/credentials` | Generated Control URL, username, and password; owner-readable only |

Use `--home` or `SOVEREIGN_HOME` for a different appliance directory, and
`SOVEREIGN_BIN_DIR` for a different CLI directory. Full installer options and
offline installation are documented in [installation](docs/installation.md).

## First use

Add the CLI to `PATH` if your shell does not already include `~/.local/bin`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Confirm the installed release and health:

```bash
sovereign version
sovereign validate
sovereign status
```

Open these local URLs:

- Workspace: <http://127.0.0.1:8880/>
- Sovereign Control: <http://127.0.0.1:8880/control/>

Read the generated Control login when needed:

```bash
cat "$HOME/.sovereign/credentials"
```

In Control, use **Models** to inspect the active local routes or add a remote
provider and encrypted credential. Use **Knowledge** to manage embedding
profiles and versioned indexes, **Evaluations** to run and inspect gates, and
**Resilience** for backups and offline bundles. In Workspace, create a
workspace, add documents if desired, and chat through the stable
`assistant-large` and platform-appropriate embedding aliases.

The public ingress binds to host loopback by default. Remote access requires an
operator-managed TLS reverse proxy and an explicit security review; do not
expose internal container ports directly.

## CLI usage

```text
sovereign up
sovereign down
sovereign status
sovereign logs [compose log options]
sovereign smoke [suite]
sovereign backup
sovereign restore <backup-id> --yes
sovereign bundle create [options]
sovereign bundle install <archive>
sovereign validate
sovereign uninstall [--purge]
sovereign version
```

Common operations:

```bash
# Start or update containers, wait for readiness, and run the smoke gate
sovereign up

# Inspect health and follow runtime or control logs
sovereign status
sovereign logs --tail 200 sovereign-runtime
sovereign logs -f sovereign-control

# Run the default end-to-end smoke suite or a deeper suite
sovereign smoke
sovereign smoke full

# Stop without deleting configuration or data
sovereign down
```

Additional evaluation suites include `quick`, `embedding`, `retrieval`,
`mixed-role`, and the CUDA-specific `omni-embedding` suite. Reports are stored
under `~/.sovereign/reports` and are also visible in Control.

## Models and provider credentials

Local release models are pinned to immutable revisions. Stable aliases keep the
workspace independent of engine-specific names:

- `assistant-large` for generation.
- `embedding-text-compact` for the Metal text embedding route.
- `embedding-omni-default` for CUDA text, image, and audio embeddings.

Control can also register OpenAI-compatible endpoints and cloud presets for
OpenAI, Anthropic, and Gemini. Provider API keys are encrypted credential
records; model registry files and API list responses do not contain the secret.
See [model management](docs/model-management.md) and
[versioned embeddings](docs/embeddings.md).

## Update or reinstall

Re-run a version-pinned installer to reinstall the same release or move to a
newer one. Release code is replaced atomically while configuration, generated
credentials, models, databases, reports, backups, and Docker volumes are
preserved.

```bash
VERSION=0.1.0-rc.3
curl -fsSL "https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v${VERSION}/deploy/scripts/install.sh" \
  | SOVEREIGN_VERSION="$VERSION" bash
```

Do not install from `main`; always pin a published version. The installer
rejects missing signatures, unsafe archive paths, unsupported hardware, and
inconsistent release metadata.

## Backup and restore

```bash
# Create a backup and verify every manifest entry
sovereign backup

# Restore only after selecting a verified backup ID
sovereign restore 20260714-120000 --yes
```

Backups include product configuration, branding, and the appliance PostgreSQL
databases. They intentionally exclude secrets and model caches. Copy important
backups off the appliance host. See [backup and restore](docs/backup-restore.md).

## Offline deployment

Create a same-platform bundle on a connected, installed host:

```bash
sovereign bundle create --include-models \
  --output "$HOME/sovereign-offline.tar.gz"
```

Transfer it through an approved medium and install it on a target with the same
hardware profile. Bundles contain release files, pinned images, checksums, the
Metal agent when applicable, and optionally model caches; they never contain
live credentials or customer data. See [offline deployment](docs/offline-deployment.md)
for clean-host installation and selective model options.

## Uninstall

The safe default stops services and removes release code while preserving data
and Docker volumes:

```bash
sovereign uninstall
```

Permanent removal requires both explicit flags:

```bash
sovereign uninstall --purge --yes
```

## Troubleshooting

- **`sovereign: command not found`** — add `~/.local/bin` to `PATH`, or invoke
  `~/.local/bin/sovereign` directly.
- **Docker prerequisite failure** — start Docker and confirm
  `docker info` and `docker compose version` both succeed as the installing
  user.
- **Slow first start** — model and image downloads can be large. Check
  `sovereign status` and `sovereign logs -f sovereign-runtime` before retrying.
- **Gated model download** — export a valid `HF_TOKEN`, then re-run the same
  version-pinned installer; completed verified downloads are retained.
- **Port 8880 already in use** — set `SOVEREIGN_HTTP_PORT` during install and
  use the URL recorded in `~/.sovereign/credentials`.
- **Another SovereignStack owns fixed containers** — stop that installation
  before starting this one. Takeover is deliberately refused to protect
  database volumes.
- **Configuration check** — run `sovereign validate`; it renders the exact
  installed Compose configuration without starting services.

## Development

For a source checkout:

```bash
cp deploy/.env.example deploy/.env   # edit development-only values
SOVEREIGN_SOURCE_DIR="$PWD" SOVEREIGN_SKIP_START=1 \
  ./deploy/scripts/install.sh
sovereign validate
sovereign up
```

Build and test commands:

```bash
make build   # build Go services
make test    # Go tests and evals tests
make images  # build all four SovereignStack application images
make validate
```

Go modules in `control/` and `docker-proxy/` are tied together by `go.work`.

## Documentation

- [Architecture](docs/architecture.md)
- [Installation](docs/installation.md)
- [Security](docs/security.md)
- [Control API](docs/control-api.md)
- [Runtime contract](docs/runtime-contract.md)
- [Model management](docs/model-management.md)
- [Observability](docs/observability.md)
- [Backup and restore](docs/backup-restore.md)
- [Offline deployment](docs/offline-deployment.md)
- [Release runbook](docs/releasing.md)
- [Apple Metal validation](docs/metal-validation-results.md)
- [NVIDIA CUDA validation](docs/cuda-validation-results.md)

The full design specification is in [design.md](design.md).

## Repository layout

| Path | Contents |
| --- | --- |
| `deploy/` | Shipped Compose files, configuration, installation, and management scripts |
| `schemas/` | JSON Schemas for configuration, release manifests, and offline bundles |
| `api/` | Sovereign Control OpenAPI specification |
| `control/` | Go control-plane backend and embedded web frontend |
| `docker-proxy/` | Restricted, allowlisted Docker API proxy |
| `evals/` | Smoke tests, benchmarks, and evaluation suites |
| `docs/` | Product, operations, security, and release documentation |
| `tests/` | Cross-component and release-gate integration tests |

The Lazarus `sovereign-vllm` runtime fork is maintained in a separate
repository because it tracks upstream vLLM.

## Versioning and release integrity

`VERSION` is the source version stamp. Release CI builds all first-party images
from one Stack commit, signs them with Sigstore, resolves the paired Runtime
release, and publishes a signed manifest containing exact image digests and
source commits. The installer consumes those immutable references rather than
mutable tags.

The two-repository publication order and clean-host release gates are defined
in the [release runbook](docs/releasing.md).
