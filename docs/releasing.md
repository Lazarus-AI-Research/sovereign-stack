# Release Runbook

SovereignStack records a separately versioned Sovereign Runtime release.
Publish Runtime first when it changes; a Stack-only release may reuse the
previous immutable Runtime version and still verifies both signed image digests
before it creates the appliance archive.

## Preflight

1. Run the full test, contract, installer, offline,
   [CUDA](cuda-validation-results.md), and
   [Metal](metal-validation-results.md) gates.
2. Set `VERSION` in the Stack repository. When Runtime changes, set its Python
   package version and tag independently.
3. Set `release/release-source.json`'s `runtime_version` and `runtime_commit` to
   the qualified immutable Runtime release.
4. Confirm all commits have only
   `Eric Hartford <eric.hartford@lazarusai.com>` as author and contain no
   `Co-Authored-By` trailers.
5. Confirm both worktrees are clean and every release artifact is licensed.

## Publish

1. If Runtime changed, tag and push `sovereign-vllm`, then wait for both signed
   runtime images and the signed Metal agent archive.
2. Verify the configured Runtime version's artifacts are still available.
3. Verify their Sigstore identities and run the Metal agent archive install
   check on Apple Silicon.
4. Tag and push SovereignStack as `v<version>`.
5. Confirm the workflow vendors the checksum-pinned EmbeddingGemma Metal
   executable, then wait for signed multi-architecture first-party images,
   SBOM/provenance attestations, the signed release manifest, the signed
   appliance archive, the Apple Silicon package, and the Ubuntu package with
   detached Sigstore bundles. With all six Apple credentials, the macOS package
   is signed, notarized, and stapled. With none, CI publishes an explicitly
   named `-unsigned.pkg`; partial Apple credential configuration fails closed.
6. Install from the public one-command URL on clean certified Mac and CUDA
   hosts; run `sovereign status`, `sovereign smoke`, and `sovereign backup`.
7. Build and install one same-platform offline bundle before announcing.

Do not retag a published version. Fix release-candidate defects in a new RC;
promote to `v0.1.0` only after the clean-host gates are green.
