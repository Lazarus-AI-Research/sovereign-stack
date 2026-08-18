# Lazarus Project Installation Friction Log

Review date: 2026-08-17

Goal: installation and first use should be understandable and completable by a
nontechnical user without prior knowledge of Git, Python, containers, model
providers, or developer tooling.

## Test host

- Apple Silicon (`arm64`)
- macOS 15.7.9
- Ample disk space
- Initially available: Git, GitHub CLI, `uv`, Node.js, npm, system Python
- Initially absent from `PATH`: Docker, Go, Rust/Cargo, Pkl

## ReCursor

### RC-001 — The current installer has no release to install

- Severity: blocker
- Observed: `install.sh` downloads a platform wheel from the latest GitHub
  release, but the repository currently has no published releases.
- User experience: installation ends before ReCursor is installed; a
  nontechnical user has no useful recovery path.
- Improvement: publish and continuously smoke-test platform wheels before
  directing users to the installer. Give the installer a plain-language
  "no release is available yet" message with a support path.

### RC-002 — Private repository authentication is an installation prerequisite

- Severity: blocker for unauthenticated users
- Observed: the installer requires `GH_TOKEN`, `GITHUB_TOKEN`, or an
  authenticated GitHub CLI while the repository and releases are private.
- User experience: the user must understand GitHub accounts, private-org
  access, tokens, or `gh auth login` before the one-line installer works.
- Improvement: decide whether distribution is internal-only. For internal
  distribution, provide a guided browser/device login with an explicit access
  check. For general distribution, publish artifacts somewhere that does not
  require repository credentials.

### RC-003 — Source fallback requires developer tooling

- Severity: high
- Observed: because no wheel exists, the working path is a repository checkout
  followed by `uv sync --locked --all-groups`.
- User experience: this is a developer build, not a grandma-friendly fallback.
- Improvement: do not expose source installation as the normal recovery path.
  Build signed/notarized platform artifacts and keep source setup in contributor
  documentation.

### RC-004 — Python version selection is broader than tested CI versions

- Severity: medium
- Observed: `uv sync` selected CPython 3.14.7. Project CI tests Python 3.11 and
  3.12, while project metadata permits any Python version `>=3.11`.
- User experience: installation can select a newer, untested interpreter and
  fail in ways that are difficult to diagnose.
- Improvement: either test every permitted version or cap/pin the supported
  range. The installer/build should select a known-supported interpreter.

### RC-005 — Main-branch CI trigger appears stale

- Severity: high release-readiness risk
- Observed: the default branch is `main`, but `.github/workflows/ci.yml` runs
  on pushes to `master`. GitHub reports no workflow runs for `main`.
- User experience: installer artifacts can be prepared from a branch whose
  normal pushes did not receive the expected CI gate.
- Improvement: change the push trigger to `main` (or both branches) and require
  successful checks before release.

### RC-006 — Source fallback silently downloads a large native toolchain

- Severity: high for the source fallback
- Observed: Rust/Cargo was not installed or on the shell `PATH`. During
  `uv sync`, the native build helper downloaded Rust 1.97.1 into
  `~/Library/Caches/puccinialin`. The resulting toolchain occupies 736 MB,
  separate from Python packages and the repository.
- User experience: a source installation pauses for a large, unexplained
  download and consumes substantial disk. On slow or metered connections it
  can look hung or unexpectedly expensive.
- Improvement: platform wheels should remain the consumer path. If source
  installation is offered, preflight disk/network requirements, announce the
  toolchain download and size, show progress, and document cleanup.

### RC-007 — Missing model credentials produce a generic runtime failure

- Severity: high for automation and first-use recovery
- Observed: with clean user state, `recursor --print "Say hello" --json`
  returns exit 1 with `"message": "Python loop failed."` and no instruction
  to connect a provider. `recursor doctor --json` correctly identifies the
  missing provider credentials and recommends setup, but the attempted command
  does not surface that diagnosis.
- User experience: the first real command fails for a reason the user cannot
  understand or fix from its output.
- Improvement: preflight provider readiness before creating a loop. Return a
  specific `provider_not_configured` error and a plain-language action such as
  "Run recursor to connect ChatGPT, Claude, Gemini, or a custom endpoint."

### RC-008 — Automation mode emits a terminal warning

- Severity: low
- Observed: explicit noninteractive `--print ... --json` emitted
  `Warning: Input is not a terminal (fd=0).` before the JSON result.
- User experience: the warning looks like a setup problem even though
  nonterminal input is expected for automation. Depending on stream routing,
  it can also complicate strict machine consumers.
- Improvement: do not initialize terminal UI/input code for explicit
  automation entry points; keep stdout reserved for the promised result.

### RC-009 — Required external Pkl CLI is undocumented and not diagnosed

- Severity: high for generated-tool features
- Observed: the README requirements list Python, `uv`, and Rust only. A clean
  source install passed `recursor doctor`, including its `pkl.evaluator` check,
  but 3 tests failed because generated tool profiles separately call an
  external `pkl` executable and require Pkl 0.32.x.
- User experience: installation and doctor both say the environment is usable,
  then `/tool create` fails later with a new prerequisite.
- Improvement: either use the bundled evaluator consistently or install/check
  the external CLI during setup. Doctor must exercise both Pkl paths and name
  features that remain unavailable.
- Recovery used during review: `brew install pkl`, which installed Pkl 0.32.1
  (99.5 MB) after first auto-updating Homebrew. The 9 capability-candidate tests
  then passed.

### RC-010 — Homebrew fallback has its own surprising side effects

- Severity: low to medium
- Observed: installing one missing prerequisite first auto-updated two Homebrew
  taps, displayed analytics/donation notices, reported five unrelated outdated
  packages, then installed Pkl.
- User experience: a simple product install appears to start maintaining the
  whole computer and presents unrelated package-manager information.
- Improvement: ship or manage the exact runtime dependency within ReCursor's
  signed package rather than sending ordinary users through Homebrew.

### ReCursor onboarding behavior that worked well

- First launch opened a concise provider picker with ChatGPT account, Claude
  account, three API-key choices, and a custom endpoint.
- Selecting ChatGPT opened browser-based account authorization without asking
  the user to create or paste an API key.
- `Esc` and `Ctrl-C` cancelled cleanly and explained how to restart setup.
- `recursor doctor --json` verified Python, package installation, native schema,
  provider readiness, bundled Pkl evaluation, and Git workspace state. It did
  not detect the missing external Pkl CLI needed by tool profiles (RC-009).
- The bundled `hello_rwp.pkl` workflow validated successfully.

## Sovereign Stack

### SS-001 — Missing Docker produces a terse, non-actionable error

- Severity: blocker
- Observed: the release installer stopped with only `error: docker is
  required` when no Docker client/daemon was present.
- User experience: a nontechnical macOS user is not told what Docker is,
  whether it is the client or daemon that is missing, or which supported
  installation path to choose.
- Improvement: preflight the client, daemon, active context, Compose plugin,
  architecture, resources, and free disk separately. Offer an automatic or
  guided macOS container-runtime setup and verify it before continuing.

### SS-002 — Docker Desktop is presented as mandatory but Colima works

- Severity: high documentation and adoption friction
- Observed: the documented macOS path treats Docker Desktop as a requirement.
  A dedicated Colima VM with the Docker CLI completed image pulls, Compose
  startup, container-to-host networking, runtime inference, embeddings,
  pgvector, and observability checks. The existing
  `host.docker.internal:host-gateway` mapping reached the native macOS agent.
- User experience: users are directed to a large GUI product with licensing,
  startup, account, and background-service implications that Sovereign does
  not technically require.
- Improvement: document and test Docker Desktop and Colima as supported
  engines. Detect a compatible running daemon rather than a Docker Desktop
  application. For a friendly default, offer to install/start a named Colima
  profile with known-good resources.

### SS-003 — The Colima path currently requires several manual pieces

- Severity: high for the lighter-weight macOS path
- Observed: the working setup required Colima 0.10.3, Lima 2.2.0, Docker CLI
  29.7.2, Docker Compose 5.5.0, a dedicated 8-CPU/16-GiB/100-GiB profile, and a
  manual `cliPluginsExtraDirs` entry in `~/.docker/config.json` pointing at
  `/opt/homebrew/lib/docker/cli-plugins`.
- User experience: knowing that "Docker" consists of a client, Compose plugin,
  context, and separate VM daemon is far beyond an ordinary user's expected
  knowledge.
- Improvement: provide an idempotent setup command that preserves existing
  Docker configuration, installs missing components, configures the Compose
  plugin, starts the named profile, switches or explicitly targets its context,
  and runs a real host-network compatibility test.

### SS-004 — Download and disk requirements are not disclosed up front

- Severity: high
- Observed: three native model artifacts totaled about 4.61 GB. The final
  Docker image set was 11.74 GB, while `~/.sovereign` occupied 4.6 GB. On the
  host, the Colima VM used about 13.1 GB (including the Docker data disk), for
  roughly 17.7 GB across the VM and Sovereign home before allowing headroom.
  The largest individual image was the 4.88-GB Workspace image.
- User experience: the install can consume many gigabytes without an up-front
  warning, fail late on a small disk, or be expensive on a metered connection.
- Improvement: calculate free-space requirements before downloading, show a
  componentized estimate and progress, recommend safe headroom, support resume,
  and document how to remove downloads, images, and the dedicated VM.

### SS-005 — The signed rc.6 release requests a Metal artifact that does not exist

- Severity: blocker for Apple Silicon installation
- Observed: Sovereign Stack `v0.1.0-rc.6` requested
  `sovereign-metal-agent-0.1.0-rc.6-arm64.tar.gz` from the `sovereign-vllm`
  rc.6 release, but that project has releases only through rc.4. The rc.6 stack
  manifest itself pins the Metal runtime/container to rc.4.
- User experience: the official installer fails mid-install with no reasonable
  end-user recovery path.
- Improvement: encode the Metal-agent version independently from the stack
  version and derive both archive and signature URLs from the signed manifest.
  Release CI must install every supported profile from an empty machine before
  publishing the top-level release.
- Recovery used during review: downloaded the rc.4 arm64 agent, verified SHA-256
  `ab8eabebac94f719325ce57f901962544ad068debc7b9f274334303b2fda393d`
  and its Sigstore bundle, then staged it in the rc.6 runtime directory.

### SS-006 — Download progress prints false-looking file errors

- Severity: high user-experience friction
- Observed: the progress monitor runs `wc -c` on each `.part` file before
  `curl` has created it, printing messages such as `No such file or directory`
  for model downloads that subsequently complete and verify successfully.
- User experience: the terminal visibly says a required model file does not
  exist, which looks like a fatal or corrupted installation.
- Improvement: create the partial file before starting the monitor or treat a
  missing file as zero bytes without allowing shell redirection to emit an
  error. Keep progress output stable and distinguish retryable events from
  failures.

### SS-007 — First-start ordering leaves the runtime permanently unready

- Severity: blocker
- Observed: the runtime container started before the 4.61-GB Metal models and
  native host agent were installed. It retried the absent agent for five
  minutes, entered `configuration_error` with `HOST_AGENT_UNREACHABLE`, and did
  not recover when the healthy agent later appeared. Container DNS and TCP
  connectivity to `host.docker.internal:9100` worked under Colima. Restarting
  only `sovereign-runtime` after the host agent was ready immediately restored
  runtime readiness.
- User experience: a normal slow first download produces a broken install even
  though every dependency eventually becomes healthy.
- Improvement: install and health-check host services before starting their
  container consumers. The runtime should also retry a recoverable
  `HOST_AGENT_UNREACHABLE` state or be restarted automatically when the agent
  becomes available.

### SS-008 — Readiness can look hung for up to 45 minutes

- Severity: high
- Observed: `sovereign up` prints only "models are loading in the background"
  and then polls silently. Its default timeout is 2,700 seconds. In this run it
  concealed the non-retrying configuration error described in SS-007.
- User experience: there is no way to tell slow loading from a broken service,
  what component is pending, or how long remains.
- Improvement: show named readiness stages, elapsed time, last health state,
  actionable errors, and log locations. Detect terminal configuration errors
  immediately rather than continuing the generic readiness loop.

### SS-009 — Smoke-test token budget creates false installation failures

- Severity: blocker at the final install step
- Observed: after the runtime restart, 11 smoke checks passed and two chat
  checks failed. Both direct and gateway requests returned HTTP 200 but empty
  visible content because the reasoning-capable model exhausted the smoke
  test's 32-token budget before answering. The same direct request with 512
  tokens returned exactly `sovereign` after 111 reasoning characters. The
  conformance test already documents this behavior and uses 512 tokens.
- User experience: the installer exits nonzero and reports failure even though
  the portal, runtime, gateway, embeddings, vector database, streaming, and
  metrics are healthy.
- Improvement: give the smoke chat check the same reasoning-aware budget as the
  conformance check, or use a deterministic readiness prompt/configuration that
  guarantees visible output. Report HTTP success with empty content accurately
  instead of the confusing detail `[runtime] ok` beside `FAIL`.

### SS-010 — Onboarding receives an implausible Colima free-space value

- Severity: high because it weakens the model-install safety gate
- Observed: the authenticated hardware API reported
  `1,003,860,291,223,552` free bytes (about 913 TiB), while macOS reported about
  3.6 TiB available. The Control container is measuring a Colima-shared or
  virtual filesystem rather than an authoritative host path.
- User experience: the recommendation screen can claim a model is compatible
  even when the real host lacks room for download, verification, extraction,
  and rollback.
- Improvement: have the signed host lifecycle service report free space for
  both `~/.sovereign` and the container-engine data disk. Pass those values to
  Control explicitly and show which locations will be consumed. Add a Colima
  integration test that compares the reported order of magnitude with host
  `statfs`/`df`.

### SS-011 — Issued gateway keys have no reachable customer API endpoint

- Severity: blocker for external clients, including ReCursor
- Observed: the portal successfully issued a model-scoped LiteLLM key, and the
  key completed chat and 768-dimensional embedding requests from inside the
  private Compose network. Caddy is the only published service, however, and
  has no route to the gateway. A keyed request to the natural portal endpoint
  `/v1/models` returned portal HTML rather than the OpenAI-compatible API.
- User experience: an administrator can create a key but cannot use it from a
  host application and is given no usable base URL. ReCursor's custom
  OpenAI-compatible provider cannot connect through the supported public
  surface.
- Improvement: publish a deliberate authenticated gateway path on the same
  origin (for example `/api/openai/v1`), document its base URL, preserve key
  budget/model restrictions, and add host-side chat/embedding tests. Keep
  LiteLLM's administrative UI and master key private.

### SS-012 — The one-time key display exposes a raw implementation response

- Severity: high usability and secret-handling friction
- Observed: after issuing a gateway key, the portal renders the entire raw
  LiteLLM response with dozens of internal fields and both key-like fields,
  under the instruction "Copy this response now."
- User experience: users cannot tell which value is the credential, where to
  use it, or which fields matter, and may copy/store substantially more
  sensitive implementation detail than necessary.
- Improvement: normalize the response in Control and display only the
  one-time secret, key alias, allowed models, limits, expiration, base URL, and
  ready-to-copy curl/Python examples. Provide explicit copy and confirmation
  affordances without logging the secret.

### SS-016 — The portal cannot revoke a gateway key

- Severity: high security and administration friction
- Observed: Control and the OpenAPI contract implement
  `DELETE /gateway/keys/{id}`, and that endpoint successfully revoked the
  disposable test key. The portal API client and Gateway keys panel expose only
  list/create operations and provide no Revoke button.
- User experience: an administrator who loses a key, issues it with the wrong
  scope, or finishes a test cannot disable it through the advertised portal.
- Improvement: list useful normalized metadata for each key and add a confirmed
  Revoke action with immediate feedback. Test that the revoked key stops
  authorizing requests and retain an auditable, non-secret event.

### SS-013 — Repeat installation is not launchd-idempotent

- Severity: blocker for reinstall and update workflows
- Observed: rerunning the same signed rc.6 installer preserved data and reused
  verified model files, but replacing the native launchd services produced
  `Bootstrap failed: 5: Input/output error`. The runtime-agent installer retried
  and recovered; the EmbeddingGemma installer did not retry, exited the entire
  install with status 5, and left the previously healthy embedding service
  unloaded. Manually bootstrapping its existing plist restored health.
- User experience: approving or retrying the installer can break a working
  appliance and finish with an opaque macOS error suggesting rerunning as root.
- Improvement: make both installers use one tested launchd helper: validate the
  plist, boot out only when loaded, wait for label removal, retry bootstrap with
  bounded backoff, kickstart, and verify the health endpoint. On failure,
  restore/restart the previous service and explain recovery without suggesting
  root unnecessarily.

### SS-014 — Normal `up` behaves like an update/install operation

- Severity: high reliability and expectations friction
- Observed: every `sovereign up` checks/pulls all images, prints first-start
  model-loading text even when models are already ready, and runs the full
  smoke suite. The repeat run restored services quickly but still exited
  nonzero because of SS-009.
- User experience: an ordinary restart appears to redownload the appliance,
  depends on registry/network availability unless offline mode was explicitly
  staged, and can report failure for a healthy system.
- Improvement: make `up` a fast, offline-capable start-and-readiness command.
  Move explicit pulls to install/update, make smoke opt-in or a separate
  post-install gate, and show `already ready` when appropriate. Retain a
  `--pull`/`--verify` mode for operators.

### SS-015 — `down` leaves the native generation model running

- Severity: high lifecycle and resource-management friction
- Observed: `sovereign down` removed all containers and stopped the native
  EmbeddingGemma service, but the launchd runtime agent and llama.cpp generation
  server remained healthy on port 9101 with the 3.2-GB model loaded.
- User experience: "Stop the appliance" does not release all CPU/memory/model
  resources, and there is no message explaining that a major component remains
  active.
- Improvement: stop both Metal launchd services on `down` and restart both on
  `up`, or provide clearly distinct `pause` and `down` semantics. Test that all
  documented ports/processes disappear while volumes, configuration, users,
  and backups remain intact.

## Implementation follow-up

The `codex/installer-hardening` branch implements repository fixes for SS-005
through SS-009, SS-011 through SS-013, and SS-015. SS-016's backend
normalization and authorization are on that branch; its portal Copy/Revoke
workflow is isolated on the companion `codex/ui-followups` branch.

Notable follow-up details:

- The Metal agent remains independently pinned to `0.1.0-rc.4`; both its
  74,820,904-byte size and SHA-256 are enforced from the release manifest.
- Customer gateway ingress is an explicit allowlist for `/v1/models`,
  `/v1/chat/completions`, and `/v1/embeddings`. Other `/api/openai/*` paths
  return 404 rather than reaching LiteLLM management or Workspace routes.
- Launchd error-5 retry and replacement rollback are covered by the lifecycle
  fixture, including restoration of the prior loaded service.
- Generated manifests are validated against the v1.1 JSON Schema. The full Go
  suites, eval tests, shell lifecycle tests, Go builds, and Compose validation
  pass locally.

These code-level results do not replace the release matrix: a signed candidate
must still pass a clean real-machine install, throttled/interrupted downloads,
ten live same-version reinstalls, real chat/embedding and key revocation
through Caddy, offline restart, and the supported macOS and Ubuntu profiles.

### Sovereign behavior that worked well

- The signed release artifacts, checksums, and Sigstore verification provide a
  strong basis for a safe one-command installer once version coordination is
  fixed.
- The portal, authentication readiness, workspace, runtime, gateway,
  embeddings, pgvector, and observability services all ran under Colima.
- After the runtime-ordering workaround, the runtime and Workspace containers
  reported healthy and direct local generation returned the requested answer.
- The single-use first-administrator claim created a disposable local admin,
  authenticated sessions worked, and hardware/model/network onboarding APIs
  completed without requiring a cloud account.
- Chat Workspace SSO, Grafana, and Phoenix all loaded through the single portal
  origin for the administrator role.
- A scoped gateway key enforced the intended private-network path and completed
  real chat and embedding requests; SS-011 prevents supported host clients from
  using that same key today.
- `sovereign backup` completed four stages and immediately verified the six-file
  backup as valid before repeat-install testing.
- The full `down`/`up` cycle retained the administrator, session, database,
  scoped key, models, configuration, backup, and volume data.

## Verification notes

- Repository checkouts:
  - `/Users/auroter/Code/Lazarus/ReCursor`
  - `/Users/auroter/Code/Lazarus/sovereign-stack`
- ReCursor `uv sync --locked --all-groups`: succeeded.
- ReCursor help and doctor: passed.
- ReCursor isolated first-launch provider picker: passed; no account was
  connected during review.
- ReCursor bundled `hello_rwp.pkl` validation: passed.
- Initial Python suite: 872 passed, 5 skipped, and 3 failed solely because the
  external Pkl CLI was absent.
- After installing Pkl 0.32.1, the affected capability test file passed 9/9.
- ReCursor clean-state noninteractive prompt: failed with an insufficiently
  actionable missing-provider error (RC-007).
- Sovereign release tested: signed `v0.1.0-rc.6` on Apple Silicon with the
  manifest-pinned rc.4 Metal runtime.
- Container engine: Colima profile `sovereign`, running with 8 CPUs, 16 GiB RAM,
  and a 100-GiB virtual disk through Docker context `colima-sovereign`.
- Final stack: 12 containers running; Postgres, Runtime, and Workspace healthy;
  portal and host APIs returned `status: ok`.
- Native services: host inference agent, llama.cpp generation model, and
  EmbeddingGemma health endpoints became ready.
- Final smoke suite: 11 passed and 2 false-negative chat failures caused by the
  32-token reasoning budget (SS-009).
- First administrator: disposable `testadmin` account created; login and role
  authorization verified through the portal API.
- Authenticated application checks: Chat SSO, Grafana, and Phoenix returned
  successful HTML responses from the single portal origin.
- Scoped-key checks: internal gateway chat returned the requested phrase and
  embeddings returned 768 dimensions; no supported host gateway route exists
  (SS-011).
- The disposable scoped key was revoked through Control's backend API after
  testing; the portal has no equivalent control (SS-016).
- Backup: `20260817-223203.101438` created and verified before reinstall tests.
- Repeat installer: preserved persistent data but exited 5 and unloaded
  EmbeddingGemma because of launchd bootstrap handling (SS-013); service was
  manually restored.
- Full lifecycle: `down`/`up` retained persistent state and recreated 12 healthy
  containers, but `down` left the Metal generation agent running (SS-015).
- Final storage measurements: 11.74 GB Docker images, 4.6 GB Sovereign home,
  and approximately 13.1 GB used by Colima VM state/data (Docker images are
  contained within that VM figure, not additional to it).
