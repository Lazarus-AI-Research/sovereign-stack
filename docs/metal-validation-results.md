# Apple Metal Validation Results

Status as of 2026-07-15. Host: MacBook Pro `Mac16,5`, Apple M4 Max
(16 CPU cores), 128 GB unified memory, macOS 26.5.2, Docker Desktop 29.6.1.

## Release-candidate gate

The `0.1.0-rc.1` version-pinned installer was exercised from a release checkout
with the exact pinned Metal artifacts and installed the self-contained launchd
host agent. The appliance bridge was rebuilt from the final Sovereign Runtime
source and connected to the host agent through `host.docker.internal`.

| Gate | Result |
|---|---|
| Generation GGUF SHA-256 | `3646b4c147cd235a44d91df1546d3b7d8e29b547dbe4e1f80856419aa455e6fd` |
| Multimodal projector SHA-256 | `58c187648007cab392bd5678b87e862c3e8794017deb945feea2cf256195e96a` |
| Embedding GGUF SHA-256 | `3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7` |
| Agent, token, and launchd plist permissions | owner-only (`0600`) |
| Native generation and embedding roles | healthy |
| Runtime bridge state | healthy, zero restarts |
| Public runtime conformance | **14 passed / 0 failed / 0 skipped** |

Conformance covered liveness, readiness, manifest validation, structured
errors, model aggregation, chat, streaming chat, text completions, text
embeddings, role mismatch and invalid-request behavior, authentication, and
metrics. The embedding response was 768-dimensional and L2-normalized, matching
the runtime manifest.

The validated immutable model revisions are:

- generation: `google/gemma-4-E2B-it-qat-q4_0-gguf` at
  `69536a21d70340464240401ba38223d805f6a709`;
- embedding: `nomic-ai/nomic-embed-text-v1.5-GGUF` at
  `0188c9bf409793f810680a5a431e7b899c46104c`.

## Defects found and fixed

- The Metal release job copied `llama-server` without its required dynamic
  libraries. The distribution now includes the pinned archive's `.dylib`
  closure, the installer copies it while preserving symlinks, and release CI
  executes the installed binary before publishing the archive.
- An immediate launchd re-registration could fail with transient Bootstrap
  error 5 during an in-place upgrade. Agent installation now retries the
  registration before failing.
- A custom-home lifecycle uninstall could fall back to the login session's
  global launchd label when its scoped agent uninstaller was absent. Fallback
  removal is now restricted to the default install home, and lifecycle tests
  isolate both `HOME` and launchctl so they cannot alter a developer's agent.
- Public `curl ... | bash` validation found that piped Bash has no
  `BASH_SOURCE[0]` and that the signed archive's deployment file is four levels
  below the extraction root. The `0.1.0-rc.2` bootstrap handles both cases and
  release CI now installs from the freshly signed archive before publishing.
- Consumer-side `0.1.0-rc.2` validation found that the generated Compose
  environment still used mutable version tags even though the signed manifest
  recorded exact digests. The `0.1.0-rc.3` release generates and consumes a
  manifest-derived image lock and verifies the resolved Compose references.

## Scope

The certified Metal embedding profile is text-only. The Gemma multimodal
projector is pinned and installed, but image-generation input is not advertised
as a v0.1 runtime contract. Common appliance services and the full Control,
gateway, database, backup, evaluation, security, and UI paths were exercised on
the CUDA appliance; the platform-specific Metal gate exercises the native
inference agent, Docker bridge, and complete public runtime API.
