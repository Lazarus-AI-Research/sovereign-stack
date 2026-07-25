# Changelog

All notable changes to Sovereign Stack are documented in this file.

## [Unreleased]

## [0.1.0-rc.6] - 2026-07-25

- Changed the default loopback and private-LAN portal port from `8880` to the
  one-time randomly selected `54854`. Existing installations keep their saved
  port, and `SOVEREIGN_HTTP_PORT` remains available as an override.
- Added bounded retries when release CI resolves and verifies the separately
  versioned Runtime images, avoiding failures on transient registry errors.

## [0.1.0-rc.5] - 2026-07-24

- Added an explicitly named unsigned macOS package fallback when Apple
  Developer ID and notarization credentials are not configured. Release CI
  still publishes SHA-256 and Sigstore verification artifacts for the package.
- Decoupled Stack and Runtime release versions so a Stack-only release can
  reuse an already-qualified, immutable signed Runtime release.

## [0.1.0-rc.4] - 2026-07-24

- Replaced the runtime-hosted embedding models with the pinned
  `embeddinggemma.c` v0.3.1 service and EmbeddingGemma Q4 GGUF on both
  certified profiles.
- Added an isolated CUDA sibling service and a loopback-only, launchd-managed
  Metal host service while retaining the product's OpenAI embedding route.
- Added an upgrade migration that preserves unrelated generation and remote
  model configuration, with one-time backups of changed embedding config.
- Added a unified, responsive Control Portal for chat, activity, tools,
  system health, models, networking, updates, recovery, and administration.
- Added guided hardware-aware onboarding, curated model recommendations, and
  observable background operations with progress, cancellation, and retry.
- Added portal-first installation and an authenticated host lifecycle service
  for safe access-mode changes, repair, updates, and support diagnostics.
- Added signed native Linux and notarized macOS installer packaging workflows.

## [0.1.0-rc.3] - 2026-07-15

- Made installed first-party image references consume the exact digests from
  the signed release manifest, including both certified runtime overlays.
- Added manifest-to-image-lock validation and release-time assertions over the
  fully resolved Compose configuration.

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
