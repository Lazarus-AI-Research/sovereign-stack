# Offline Deployment

Offline bundles are same-platform distribution artifacts. They contain the
installed release, every pinned container image, a manifest, per-file
checksums, the Metal host agent when applicable, the complete pinned macOS
container toolchain and Docker Compose Sigstore material, and optionally model
caches. They are not backups and never contain live configuration, provider
secrets, gateway keys, databases, or workspace data.

Create a complete bundle on a connected machine with the same hardware
profile:

```bash
sovereign bundle create --include-models \
  --output ~/sovereign-offline.tar.gz
```

To limit weight size, repeat `--include-model` with one or more supported IDs:

```bash
sovereign bundle create \
  --include-model assistant-large \
  --include-model embedding-gemma-default \
  --output ~/sovereign-cuda.tar.gz
```

Without either option, the bundle includes images but no weights. The Control
UI exposes the same complete-cache operation under **Resilience**.

Transfer the archive through the approved medium and install it on the target:

```bash
./install.sh --version 0.1.0 --profile cuda-x86_64 \
  --offline-bundle /media/sovereign-offline.tar.gz
```

The installer rejects unsafe archive paths, verifies every entry against
`checksums.sha256`, checks profile and version equality, loads images, restores
the optional model cache, provisions a private engine on macOS when none
exists, and marks the installation offline so later starts do not pull. A
missing tool, signature bundle, or probe image is a hard failure; offline
installation never falls back to downloading it.

The current CUDA bundle does not contain the kernel-specific Ubuntu driver,
Docker, and NVIDIA Toolkit package closure. Create it only after completing
online host provisioning on the target, or pre-provision an identical Ubuntu
host through an approved package mirror. A fresh CUDA host with missing system
packages fails closed. Locally created bundles provide integrity but not signer
identity; verify the signed online release before creating one and protect the
transfer medium. Official release archives and runtime assets are independently
signed with Sigstore.
