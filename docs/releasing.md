# Release Runbook

SovereignStack and Sovereign Runtime share an exact version. Publish Runtime
first because the Stack release verifies both signed runtime image digests
before it creates the appliance archive.

## Preflight

1. Run the full test, contract, installer, offline,
   [CUDA](cuda-validation-results.md), and
   [Metal](metal-validation-results.md) gates.
2. Set `VERSION` and Python package versions in both repositories to the same
   release or release-candidate version.
3. Commit Sovereign Runtime, then set `release/release-source.json`'s
   `runtime_commit` to that immutable commit.
4. Confirm all commits have only
   `Eric Hartford <eric.hartford@lazarusai.com>` as author and contain no
   `Co-Authored-By` trailers.
5. Confirm both worktrees are clean and every release artifact is licensed.

## Publish

1. Tag and push `sovereign-vllm` as `v<version>`.
2. Wait for both signed runtime images and the signed Metal agent archive.
3. Verify their Sigstore identities and run the Metal agent archive install
   check on Apple Silicon.
4. Tag and push SovereignStack as `v<version>`.
5. Wait for signed multi-architecture first-party images, SBOM/provenance
   attestations, the signed release manifest, and the signed appliance archive.
6. Install from the public one-command URL on clean certified Mac and CUDA
   hosts; run `sovereign status`, `sovereign smoke`, and `sovereign backup`.
7. Build and install one same-platform offline bundle before announcing.

Do not retag a published version. Fix release-candidate defects in a new RC;
promote to `v0.1.0` only after the clean-host gates are green.
