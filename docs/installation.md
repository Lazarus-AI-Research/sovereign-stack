# Installation

SovereignStack v0.1 supports two certified host profiles:

- Apple Silicon Mac with at least 32 GB unified memory, Docker Desktop, and 20 GB free disk (60 GB or more is recommended when keeping model weights).
- Ubuntu 24.04 x86_64 with one NVIDIA GPU exposing at least 24 GB VRAM, Docker Engine with Compose v2, the NVIDIA driver, and NVIDIA Container Toolkit.

The installer manages SovereignStack only. It does not modify host drivers,
Docker, or operating-system packages.

## One-command install

Tagged releases publish an Apple Silicon `.pkg` and an Ubuntu AMD64 `.deb`.
When Apple credentials are configured, the macOS package is Developer ID
signed, notarized, and stapled. Otherwise its filename ends in `-unsigned.pkg`
and macOS displays the expected Gatekeeper warnings. Opening either package
installs a small native bootstrap and starts the same signed, version-pinned
installation in the background; the portal opens as soon as the control plane
is ready. Every native package has detached Sigstore and SHA-256 verification
artifacts on the release page, including the explicitly unsigned macOS package.
The default local portal address is <http://127.0.0.1:54854/>.

For the stable release:

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0/deploy/scripts/install.sh | bash
```

For a release candidate, pin both the bootstrap script and requested version:

```bash
curl -fsSL https://raw.githubusercontent.com/Lazarus-AI-Research/sovereign-stack/v0.1.0-rc.6/deploy/scripts/install.sh \
  | SOVEREIGN_VERSION=0.1.0-rc.6 bash
```

The bootstrap downloads the versioned release archive, verifies its SHA-256
checksum and Sigstore identity, detects the certified profile, generates
owner-only appliance secrets, pulls digest-pinned images from the signed
release manifest, starts the portal, opens guided first-run setup, then
provisions the generation runtime and embedding service while **Activity**
reports progress. A Hugging Face token is only needed
when a configured repository requires one: set `HF_TOKEN` in the installer
environment.

The appliance is installed under `~/.sovereign`; the management command is
placed at `~/.local/bin/sovereign`. Add that directory to `PATH` if needed.
The installer opens the correct portal URL automatically on a desktop. Over
SSH it defaults to a detected private-LAN address and prints that address (and
a QR code when available), so `127.0.0.1` is never presented as the primary
remote result. The first user chooses the administrator username and password
through a single-use, 30-minute setup link; `sovereign admin setup-link`
remains a recovery path for an expired link.

Network access is normally changed inside **Network Access** in the portal.
The compatible recovery commands remain:

```bash
sovereign access lan                       # trusted RFC1918 network
sovereign access domain ai.example.com     # public hostname, automatic TLS
sovereign url                              # print the current address
```

## Common operations

```bash
sovereign status
sovereign open
sovereign logs -f sovereign-runtime
sovereign smoke
sovereign down
sovereign up
```

Re-running the same installer is idempotent: it replaces release code while
preserving configuration, encrypted credentials, models, databases, reports,
and backups. The EmbeddingGemma upgrade removes only the retired local
embedding entries, preserves unrelated generation and remote model entries,
and stores one-time `*.pre-embeddinggemma` configuration backups.

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
