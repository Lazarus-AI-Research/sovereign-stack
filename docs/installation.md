# Installation

SovereignStack v0.1 supports two certified host profiles:

- Apple Silicon Mac with at least 32 GB unified memory and 20 GB free disk
  (60 GB or more is recommended when keeping model weights). Docker Desktop,
  Homebrew, Colima, Lima, Docker CLI, and Compose are not prerequisites: when
  no compatible engine is available, the installer provisions its own pinned
  Colima toolchain and private VM.
- Ubuntu 24.04 x86_64 with an NVIDIA display or 3D controller and at least one
  GPU exposing 24 GB VRAM. The NVIDIA driver, Docker Engine, Compose, and
  NVIDIA Container Toolkit are installed and configured by the installer
  after one administrator approval; they are not prerequisites.

On macOS the installer owns only its private `sovereign` Colima profile and
never changes the user's active Docker context. Existing compatible engines
are reused without being stopped, upgraded, reconfigured, or removed.

On Ubuntu, PCI hardware is detected before a driver exists. Driver and
container packages come from authenticated Ubuntu, Docker, and NVIDIA
repositories whose signing-key fingerprints are checked before trust is
installed. A driver or Docker-group change records a private resume journal,
enables a narrowly scoped user systemd unit, and continues automatically after
the required reboot. Host Docker packages are preserved during uninstall.

## One-command install

Tagged releases publish an Apple Silicon `.pkg` and an Ubuntu AMD64 `.deb`.
When Apple credentials are configured, the macOS package is Developer ID
signed, notarized, and stapled. Otherwise its filename ends in `-unsigned.pkg`
and macOS displays the expected Gatekeeper warnings. Opening either package
installs a small native bootstrap and starts the same signed, version-pinned
installation. After dpkg releases its package lock, the Ubuntu package uses a
persistent systemd coordinator and prints one `journalctl` command for live
progress and durable errors; it never hides failure in a detached `nohup` job.
It resumes after a required reboot. The portal opens as soon as the control
plane is ready. Every native package has detached Sigstore and SHA-256 verification
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

CUDA offline bundles currently contain all application images and optional
models, but not the kernel-specific Ubuntu/NVIDIA package closure. A fresh
Ubuntu host therefore needs network access for its first host-provisioning
stage; after a completed online install, ordinary `down`/`up` and bundle image
loading remain offline. The installer fails closed instead of reaching the
network when a fresh CUDA install is explicitly given an offline bundle.

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
sovereign repair
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

`sovereign uninstall` stops services and an installer-owned Colima VM, then
removes release code while preserving appliance data, Docker volumes, and the
managed VM. `sovereign uninstall --purge` prints every owned path, volume, and
managed VM without deleting anything. After reviewing that preview, permanent
deletion deliberately requires both flags:

```bash
sovereign uninstall --purge --yes
```
