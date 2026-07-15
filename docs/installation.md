# Installation

SovereignStack v0.1 supports two certified host profiles:

- Apple Silicon Mac with at least 32 GB unified memory, Docker Desktop, and 20 GB free disk (60 GB or more is recommended when keeping model weights).
- Ubuntu 24.04 x86_64 with one NVIDIA GPU exposing at least 24 GB VRAM, Docker Engine with Compose v2, the NVIDIA driver, and NVIDIA Container Toolkit.

The installer manages SovereignStack only. It does not modify host drivers,
Docker, or operating-system packages.

## One-command install

For the stable release:

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0/deploy/scripts/install.sh | bash
```

For a release candidate, pin both the bootstrap script and requested version:

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.1/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.1 bash
```

The bootstrap downloads the versioned release archive, verifies its SHA-256
checksum and Sigstore identity, detects the certified profile, generates
owner-only credentials, pulls pinned images, starts the stack, waits for both
runtime roles, and runs the smoke suite. A Hugging Face token is only needed
when a configured repository requires one: set `HF_TOKEN` in the installer
environment.

The appliance is installed under `~/.sovereign`; the management command is
placed at `~/.local/bin/sovereign`. Add that directory to `PATH` if needed.
The default URLs are:

- Workspace: `http://127.0.0.1:8880/`
- Control: `http://127.0.0.1:8880/control/`

The generated administrator password is in `~/.sovereign/credentials` with
mode `0600`.

## Common operations

```bash
sovereign status
sovereign logs -f sovereign-runtime
sovereign smoke
sovereign down
sovereign up
```

Re-running the same installer is idempotent: it replaces release code while
preserving configuration, encrypted credentials, models, databases, reports,
and backups.

The appliance is intentionally single-instance. Startup refuses to take over
fixed SovereignStack containers owned by a different install directory; stop
the other installation or migrate it deliberately instead of risking an
implicit database-volume takeover.

## Removal

`sovereign uninstall` stops services and removes release code while preserving
data and Docker volumes. Permanent deletion deliberately requires both flags:

```bash
sovereign uninstall --purge --yes
```
