# Offline Deployment

Offline bundles are same-platform distribution artifacts. They contain the
installed release, every pinned container image, a manifest, per-file
checksums, the Metal host agent when applicable, and optionally model caches.
They are not backups and never contain live configuration, provider secrets,
gateway keys, databases, or workspace data.

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
  --include-model embedding-omni-default \
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
the optional model cache, and marks the installation offline so later starts
do not pull. Locally created bundles provide integrity but not signer identity;
verify the signed online release before creating one and protect the transfer
medium. Official release archives and runtime assets are independently signed
with Sigstore.
