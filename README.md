# Lazarus SovereignStack

SovereignStack is a local-first AI appliance for small offices, workgroups, and
customer-owned hardware. It combines a chat and knowledge workspace, an
operator control plane, an OpenAI-compatible model gateway, local inference,
vector search, evaluations, observability, and backups in one managed stack.

The current public preview is
[`v0.1.0-rc.6`](https://github.com/Lazarus-AI-Research/sovereign-stack/releases/tag/v0.1.0-rc.6).
It supports Apple Silicon Macs and Ubuntu NVIDIA CUDA hosts.
Release-candidate users should pin both the installer URL and
`SOVEREIGN_VERSION` exactly as shown below.

## What is included

- **Sovereign Workspace** for chat, documents, and retrieval-augmented
  generation.
- **Sovereign Control** for models, provider credentials, embedding profiles,
  evaluations, backups, offline bundles, and system health.
- **Sovereign Runtime** for local generation, plus a dedicated
  `embeddinggemma.c` service for text embeddings.
- **LiteLLM and pgvector** for stable model aliases, gateway policy, and local
  vector storage.
- **Prometheus, Grafana, Loki, OpenTelemetry, and Phoenix** for local
  operational visibility.
- Signed release archives, signed first-party images, immutable image digests,
  first-admin claiming, and a restricted Docker control proxy.

## Supported hosts

| Profile | Host requirements | v0.1 capability |
| --- | --- | --- |
| `metal-arm64` | Apple Silicon Mac, 32 GB+ unified memory | Text chat through a signed generation agent, plus text embeddings and pgvector RAG through a loopback-only Metal service |
| `cuda-x86_64` | Ubuntu 24.04 x86_64, NVIDIA display/3D PCI device with a 24 GB+ GPU | Text chat, text embeddings, and pgvector RAG |

Both profiles require at least 20 GB free disk and network access for the
initial online install. At least 60 GB free disk is recommended when retaining
model weights. Docker, Compose, Colima, NVIDIA drivers, and NVIDIA Container
Toolkit are provisioned when missing. Ubuntu asks once for administrator
approval and resumes automatically after a required driver/group reboot; the
appliance itself continues to run as the login user.

See [hardware profiles](docs/hardware-profiles.md) for the exact support matrix.

## Install

Tagged releases provide an Apple Silicon `.pkg` and an Ubuntu AMD64 `.deb` for
a native one-click install. A filename ending in `-unsigned.pkg` means Apple
credentials were not configured and macOS will display Gatekeeper warnings;
the release page still provides its SHA-256 and Sigstore bundle. The signed
shell path below remains the supported choice for headless servers and
automation. The Ubuntu package prints a `journalctl` command for its persistent
post-package coordinator; installation and any reboot continuation remain
inspectable rather than disappearing into a detached process.

Run the same command on a supported Mac or CUDA host. The installer detects the
profile, verifies the signed release, generates appliance secrets, pulls the
exact digest-pinned images, and starts the portal immediately while models
continue loading. It opens the one-time first-administrator setup page in the
default browser, then completes runtime smoke tests before reporting success.
The default local portal address is <http://127.0.0.1:54854/>.

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash
```

The first run can take a while because it downloads a pinned signature verifier,
container images, and model weights. The portal opens as soon as its core
services are ready; downloads and model loading remain visible under
**Activity** while installation finishes. Set `HF_TOKEN` in the installer
environment before running the command only when a configured model repository
requires authentication.

To choose a profile explicitly:

```bash
# Apple Silicon
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash -s -- --profile metal-arm64

# Ubuntu NVIDIA CUDA
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash -s -- --profile cuda-x86_64
```

When installing over SSH, the installer selects private-LAN access and prints
one reachable portal URL (plus a QR code when `qrencode` is installed). You can
also choose access explicitly:

```bash
# Local desktop
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash -s -- --access desktop

# Headless/private network
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash -s -- --access lan

# Public domain with automatic HTTPS
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash -s -- --domain ai.example.com
```

The default install locations are:

| Path | Purpose |
| --- | --- |
| `~/.sovereign` | Releases, configuration, secrets, models, reports, backups, and appliance state |
| `~/.local/bin/sovereign` | Management CLI |

PostgreSQL and observability service data live in Docker-managed named volumes,
not under `~/.sovereign`; the storage table in [initial configuration](#3-choose-logging-and-tracing)
explains their backup and retention behavior.

Use `--home` or `SOVEREIGN_HOME` for a different appliance directory, and
`SOVEREIGN_BIN_DIR` for a different CLI directory. Full installer options and
offline installation are documented in [installation](docs/installation.md).

## First use

The installer opens the single Sovereign Portal. On a headless host, open the
one URL printed by the installer. The first browser session walks through:

1. Creating the first administrator (there is no default password).
2. Choosing local-only or private-network access.
3. Reviewing the detected hardware and recommended model.
4. Watching provisioning progress and opening Chat.

Chat, Activity, Grafana, Phoenix, models, embeddings, provider connections,
people, backups, updates, repair, and diagnostics all remain inside that portal.
Normal operation does not require Docker commands, container access, or
memorized service ports.

The `sovereign` CLI remains available for automation and recovery. If a setup
link expires before the first administrator is created, run
`sovereign admin setup-link` on the host to issue another 30-minute link.

The shipped local models and privacy-preserving observability defaults are
ready to use without a provider key. The next section explains how to keep
those defaults or deliberately change them before adding production data.

The portal binds to host loopback by default. Administrators can change this in
**Network Access** without SSH; the compatible `sovereign access` commands
remain available. Public cleartext HTTP still requires the deliberately long
`--i-understand-this-is-insecure` CLI acknowledgement. Internal service ports
are never published.

## Initial configuration

Complete this walkthrough after the installer smoke test passes and before
adding production documents. The examples assume the default
`~/.sovereign` appliance directory; replace it with the value passed to
`--home` when using a custom location.

The installed defaults are:

| Setting | CUDA | Apple Silicon |
| --- | --- | --- |
| Generation route | Local `assistant-large` | Local `assistant-large` |
| Embedding profile | `gemma-default` (`embedding-gemma-default`) | `gemma-default` (`embedding-gemma-default`) |
| Phoenix tracing | Metadata only | Metadata only |
| Prompt and response logging | Off | Off |
| Full-content traces | Off | Off |

These defaults need no cloud account and are the recommended starting point.

### 1. Choose the generation model

Open **Models** in the portal. The recommended, hardware-compatible model is
shown first and installs with one confirmation. The active local model is
served to Workspace using the stable `assistant-large` route, even if its
underlying checkpoint changes.

To use another local Hugging Face, ModelScope, or local-path model:

1. Select **Add custom model** to open Advanced configuration.
2. Set **Role** to **Generation**, choose the source, and enter the model or
   repository. Catalog models should include an immutable commit revision.
3. Save it, select **Load**, wait for the runtime to become ready, and then
   select **Test**.

Loading a local generation model replaces the currently loaded generation
role. Workspace continues to use `assistant-large`, so no Workspace setting
needs to change. A gated Hugging Face repository also requires `HF_TOKEN` in
`~/.sovereign/.env`; keep that file owner-readable with mode `0600`.

To use a cloud or OpenAI-compatible remote model:

1. Under **API & Providers**, save the provider credential. The secret is
   encrypted and is not returned after submission.
2. Under **Models**, add a **Generation** model. Give it a short
   **Product ID**, such as `team-coding-model`; select the credential and set
   the remote base URL when applicable.
3. Select **Load** to regenerate and restart the private gateway, then select
   **Test**.
4. Set Workspace's generation preference to that Product ID in
   `~/.sovereign/.env`:

   ```dotenv
   GENERIC_OPEN_AI_MODEL_PREF=team-coding-model
   ```

5. Apply and verify the change:

   ```bash
   chmod 600 "$HOME/.sovereign/.env"
   sovereign up
   sovereign smoke
   ```

Keep `GENERIC_OPEN_AI_MODEL_PREF=assistant-large` for a local model. Remote and
cloud routes send requests outside the appliance and may incur provider costs;
review the provider's data policy first. Use **Access > Gateway
keys** to issue separate client keys with allowed-model, spend, RPM, and TPM
limits.

### 2. Choose the embedding model

Embedding identity includes the checkpoint, pooling, normalization, prefixes,
preprocessing, and vector dimensions. It cannot be changed underneath an
existing index.

Fresh installs activate the certified `gemma-default` profile automatically:

| Profile | Hosts | Use it for | Workspace settings |
| --- | --- | --- | --- |
| `gemma-default` | CUDA or Apple Silicon | Text retrieval through `embeddinggemma.c` | Stable alias `embedding-gemma-default`; query prefix `task: search result \| query: `; passage prefix `title: none \| text: ` |

For a specialized local or OpenAI-compatible embedding model:

1. Under **Models**, add a model-registry entry with the **Embedding** role.
2. Under **Embeddings**, choose **Add provider**, select that registry entry,
   and set the stable alias, pooling, normalization, and prefixes.
3. Select **Validate**, then **Activate everywhere**. Control places retrieval
   in maintenance, rebuilds every workspace, validates the candidates, and
   changes the appliance provider and all workspace bindings atomically.
4. Run `sovereign smoke embedding` and `sovereign smoke retrieval`.

Any failed activation restores the previous provider and indexes. No
AnythingLLM environment variables or per-workspace provider edits are needed.

### 3. Choose logging and tracing

The recommended v0.1 privacy posture is metadata-only tracing with all content
capture disabled. Verify it under **Settings > Privacy posture**:

```yaml
# ~/.sovereign/config/feature-flags.yaml
phoenix:
  enabled: true
tracing:
  enabled: true
  metadata_only: true
  full_trace: false
  prompt_logging: false
  response_logging: false
prompt_logging:
  enabled: false
```

Do **not** enable full traces for v0.1. The Settings view is intentionally
read-only for these controls, and v0.1 does not provide the complete redaction,
scope, retention, consent, and runtime enforcement needed for prompt or
response capture. `full_trace`, `prompt_logging`, and `response_logging` must
remain `false` for the supported configuration.

v0.1 also does not expose a supported global `debug`, `info`, `warning`, or
`error` selector. Services use their shipped levels; the runtime currently
logs at `INFO`. `structured_logs: true` is retained in
`~/.sovereign/config/runtime.yaml`, but it is not a product-wide verbosity
control. Use `sovereign logs` to select services and time windows:

```bash
sovereign logs --tail 200 sovereign-runtime
sovereign logs --since 30m sovereign-gateway
sovereign logs -f sovereign-control
```

Logging and observability data is stored as follows:

| Data | Storage and retention |
| --- | --- |
| Container stdout/stderr | Docker's host logging driver; inspect with `sovereign logs`. Rotation, quotas, and physical location are controlled by the host Docker configuration and are not included in Sovereign backups. |
| Restricted Docker actions | Append-only `~/.sovereign/logs/docker-proxy/docker-actions.jsonl`; retain or archive it according to local audit policy. It is not included in Sovereign backups. |
| Phoenix traces | The appliance PostgreSQL `phoenix` database in a Docker named volume; included in `sovereign backup`. With the supported defaults, traces must contain metadata rather than prompt or response content. |
| Loki | Docker named volume `loki_data`, with a default `168h` retention setting. v0.1 does not automatically ship all container stdout/stderr to Loki, so only data explicitly sent to Loki appears there. |
| Prometheus and Grafana | Docker named volumes `prometheus_data` and `grafana_data`; operational state is local and is not part of the normal product backup. |
| Evaluation reports | `~/.sovereign/reports`; review them for customer metadata before exporting. |

Configure Docker log rotation at the host level before sustained production
use. Docker manages the physical location of named volumes; changing
`SOVEREIGN_HOME` does not move them.

### 4. Review the other common settings

- **Portal access:** Use **Network Access** to choose this computer, a trusted
  private network, or a domain with automatic public TLS. The compatible
  `sovereign access` commands remain available for recovery. Public cleartext
  HTTP is rejected unless its explicit insecure acknowledgement flag is supplied.
- **Context limit:** Workspace defaults to
  `GENERIC_OPEN_AI_MODEL_TOKEN_LIMIT=2048`. Do not set it above the active
  route's supported context length. Runtime model length, memory allocation,
  and concurrency are hardware-specific advanced settings in
  `~/.sovereign/config/runtime.yaml`; changing them requires **System >
  Restart runtime** and the full evaluation gate.
- **Provider access:** Store provider secrets under **API & Providers**, not in
  model registry files. Issue scoped gateway keys rather than sharing the
  appliance master key.
- **Branding:** Set the product name, company name, and colors under
  **Settings > Branding**.
- **Backups:** Run `sovereign backup` after initial configuration and copy the
  verified backup off the appliance. Backups exclude `.env`, first-admin claim
  material, encryption keys, gateway secret configuration, and model
  caches. Encrypted provider credential records are in the database but need
  the excluded vault key, so preserve required secrets separately using an
  approved process.
- **Offline use:** After models are loaded and tested, create a same-platform
  offline bundle with `sovereign bundle create --include-models`.

Finish initial configuration with:

```bash
sovereign validate
sovereign up
sovereign smoke full
sovereign status
sovereign backup
```

Files under `~/.sovereign/config` are managed by Control and may be rewritten
when models or embedding profiles change. Prefer Control for supported changes,
and keep a record of deliberate `.env` overrides: the installer regenerates
that file, so non-secret preferences must be checked after an upgrade.

## CLI usage

```text
sovereign up
sovereign open
sovereign url
sovereign access desktop
sovereign access lan [private-ip]
sovereign access domain <hostname>
sovereign admin setup-link
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
`mixed-role`, and `full`. Reports are stored
under `~/.sovereign/reports` and are also visible in Control.

## Models and provider credentials

Local release models are pinned to immutable revisions. Stable aliases keep the
workspace independent of engine-specific names:

- `assistant-large` for generation.
- `embedding-gemma-default` for text embeddings on every certified profile.

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
VERSION=0.1.0-rc.6
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
- **Ubuntu provisioning failure** — inspect
  `~/.sovereign/state/install-journal.env`, confirm the host is Ubuntu 24.04
  with a supported NVIDIA PCI device, then re-run the same installer. Completed
  package and release stages are reconciled rather than discarded.
- **Slow first start** — model and image downloads can be large. Check
  `sovereign status` and `sovereign logs -f sovereign-runtime` before retrying.
- **Gated model download** — export a valid `HF_TOKEN`, then re-run the same
  version-pinned installer; completed verified downloads are retained.
- **Port 54854 already in use** — set `SOVEREIGN_HTTP_PORT` during install and
  run `sovereign url` to print the resulting portal URL.
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

`VERSION` is the source version stamp. Release CI builds all Stack images from
one commit, signs them with Sigstore, resolves the separately versioned Runtime
release, and publishes a signed manifest containing exact image digests,
versions, and source commits. The installer consumes those immutable references
rather than mutable tags.

The two-repository publication order and clean-host release gates are defined
in the [release runbook](docs/releasing.md).
