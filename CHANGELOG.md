# Changelog

All notable changes to Sovereign Stack are documented in this file.

## [Unreleased]

- Release follow-up changes land here.

## [0.1.0-rc.2] - 2026-07-15

- Fixed the public piped bootstrap when `BASH_SOURCE[0]` is unavailable and
  corrected discovery of the deployment root in the downloaded release archive.
- Added archive path-safety verification and a release-time regression gate that
  installs from the freshly signed archive through the documented piped command.

## [0.1.0-rc.1] - 2026-07-14

- Added one-command, signed online installation for certified Apple Metal and
  Ubuntu NVIDIA CUDA profiles, with safe reinstall and uninstall behavior.
- Added published multi-architecture stack images, runtime release coordination,
  SBOMs, Sigstore signing, immutable release/model manifests, and Apache-2.0
  licensing notices.
- Added same-platform offline bundles containing pinned images, checksums, the
  Metal agent, and optional model caches.
- Completed Sovereign Control management for models, encrypted provider
  credentials, gateway keys/budgets, embedding profiles, versioned indexes,
  evaluations, backups, bundles, branding, and privacy state.
- Added atomic, maintenance-mode pgvector index rebuilds integrated with the
  pinned AnythingLLM workspace image.
- Filled the portable, retrieval, multimodal, mixed-role, quick, full, and
  smoke evaluation suites and release contract validation.
