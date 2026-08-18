# Lazarus Installer Improvement Plan

Review date: 2026-08-17

Scope: ReCursor and Sovereign Stack on a clean Apple Silicon Mac, with Colima
as the validated lightweight container engine. Ubuntu/CUDA and Docker Desktop
remain required release-test targets even though they were not exercised on
this host.

Evidence and exact observations live in `installer-review-2026-08-17.md`. This plan
turns those observations into release gates and an implementation sequence.

## Product-level definition of done

A nontechnical user should be able to:

1. Start from one signed download or one copy-and-paste command.
2. Understand the time, download size, disk use, permissions, and major
   components before committing.
3. Accept a recommended setup without learning Git, Python, Homebrew, Docker,
   Colima, Compose, model formats, tokens, ports, or launchd.
4. See continuous, plain-language progress with no silent wait longer than 30
   seconds.
5. Create the first administrator, reach Chat, and receive a real answer.
6. Stop, start, retry, reinstall, update, back up, restore, and uninstall
   without losing data accidentally.
7. Receive an actionable explanation and one safe recovery action whenever a
   step cannot complete.

The installer is not approved for general users until every P0 release gate
below passes from a clean machine and from an already configured appliance.

## Recommended installer architecture

### 1. Small signed bootstrapper

- Distribute a notarized macOS package/app plus a scriptable CLI entry point.
- Keep the bootstrapper small. It should detect, explain, download, verify, and
  invoke versioned components; it should not embed release-specific URLs in
  ad-hoc shell logic.
- Continue checksum and Sigstore verification, but obtain every component
  version, URL, size, digest, and signer rule from one signed release manifest.
- Fail release publication if any referenced artifact is absent or unverifiable.

### 2. Explicit state machine and journal

- Model installation as named stages: preflight, engine, release, images,
  models, native services, portal, administrator, readiness, smoke, complete.
- Persist stage, artifact bytes, verification result, last error, and recovery
  action under `~/.sovereign/state` using atomic writes.
- Make every stage idempotent. A retry should validate and reuse completed work,
  resume partial downloads, and repair only the failed component.
- Emit the same structured events to the terminal, portal, logs, and optional
  `--json` automation mode.
- Treat interruption as normal: the next launch should say what was found and
  offer Resume, Repair, Start over while preserving data, or Uninstall.

### 3. Separate lifecycle verbs

- `install`: obtain/verify assets and establish the appliance.
- `start` or `up`: start locally available components without registry access.
- `verify`: checksum assets and run diagnostics.
- `test`: run smoke/conformance suites explicitly.
- `update`: backup, stage a new signed release, switch atomically, and roll back.
- `repair`: reconcile services/configuration with the installed manifest.
- `down`: stop containers and all native model services while preserving data.
- `uninstall`: remove software/services but preserve data by default; `--purge`
  must preview exact paths and require explicit confirmation.

## P0 — Release blockers

### P0.1 Fix the Sovereign release contract

Addresses SS-005.

- Add independent manifest fields for stack, runtime container, Metal agent,
  EmbeddingGemma binary, models, schemas, and image digests.
- Have the installer use the manifest's Metal-agent version instead of assuming
  it equals the top-level stack version.
- Release CI must download and verify the exact public artifacts after upload,
  install each supported profile, and only then mark the release available.

Acceptance:

- A clean rc.6-equivalent Apple Silicon install never requests a nonexistent
  artifact and requires no manually staged files.
- Removing or changing any manifest artifact makes release CI fail before
  users can see the release.

### P0.2 Fix first-start dependency ordering and recovery

Addresses SS-007 and SS-008.

- Download and verify Metal assets before starting the runtime consumer.
- Install, start, and health-check the host agent before starting the runtime
  container.
- Make `HOST_AGENT_UNREACHABLE` genuinely recoverable with continued bounded
  retries, or automatically restart the runtime once the agent is ready.
- Surface component name, state, elapsed time, last error, and relevant log from
  the first failed health check; never hide a terminal configuration error
  behind the 45-minute generic timeout.

Acceptance:

- Throttle the model download below the runtime's current five-minute retry
  window: the install still completes without manual restart.
- Kill/restart the host agent during readiness: the runtime returns to healthy
  automatically.

### P0.3 Make native service installation and reinstall idempotent

Addresses SS-013.

- Share one launchd install/replace helper between the runtime agent,
  EmbeddingGemma, and host lifecycle service.
- Detect loaded/unloaded state; validate plists; boot out conditionally; wait
  for label removal; retry bootstrap with bounded backoff; kickstart; health
  check; and restore the previous service on failure.
- Never recommend `sudo` for a per-user LaunchAgent error unless root authority
  is actually required.

Acceptance:

- Run the same installer ten consecutive times. Every run exits successfully,
  existing users/configuration remain intact, and all native endpoints stay or
  return healthy.
- Inject launchctl error 5: retry or rollback succeeds and a working service is
  never left unloaded.

### P0.4 Fix the install smoke gate

Addresses SS-009.

- Use the conformance suite's reasoning-aware token budget for smoke chat, or a
  deterministic no-reasoning readiness request.
- Distinguish HTTP success, nonempty reasoning, nonempty visible content, and
  semantic assertion in output.
- Do not mark `[runtime] ok` as `FAIL` without explaining the empty-content
  condition.

Acceptance:

- Direct and gateway chat pass with the shipped model on three consecutive
  clean starts.
- A deliberately broken route still produces a specific failing component and
  nonzero result.

### P0.5 Publish a usable scoped gateway endpoint

Addresses SS-011, SS-012, and SS-016 and unlocks ReCursor integration.

- Route an intentional path on Caddy's existing origin to LiteLLM, such as
  `/api/openai/v1`, protected by scoped gateway keys.
- Keep the LiteLLM administration UI and master key unreachable.
- Normalize key creation in Control. Return/display only the one-time secret,
  alias, allowed models, budgets/rates, expiration, base URL, and examples.
- Add a Copy key button, Copy base URL button, and a one-command connectivity
  test that never logs the secret.
- Add a confirmed Revoke action backed by the existing Control delete endpoint,
  and verify that revoked keys immediately stop authorizing requests.

Acceptance:

- A host process can list models, chat, stream, and embed through the published
  Caddy origin with a scoped key.
- Requests without a key, with a disallowed model, or beyond a configured limit
  fail correctly.
- A portal administrator can revoke a key without CLI/API work, and the revoked
  key can no longer list models, chat, or embed.
- ReCursor can use the endpoint without Docker-network access or the appliance
  master key.

### P0.6 Make ReCursor distributable

Addresses RC-001, RC-002, RC-003, RC-004, RC-005, and RC-006.

- Publish signed platform wheels/artifacts before promoting the installer.
- Correct CI from `master` to `main` and require it for release.
- Test and pin the supported Python range; select a known-supported runtime
  rather than an untested newest interpreter.
- Decide explicitly whether distribution is private/internal or public. A
  private install must guide browser/device authentication and verify org
  access before downloading.
- Never make the ordinary user fall back to a source build and silent 736-MB
  Rust toolchain download.

Acceptance:

- Clean installation succeeds without Git checkout, compiler, Rust, or Python
  knowledge.
- Every installer-selected Python version is covered by CI.
- No promoted installer points at an empty release page.

## Implementation status on `codex/installer-hardening`

The Sovereign Stack portions of P0.1 through P0.5 are implemented on this
branch. P0.6 remains a separate ReCursor repository and release effort.

- P0.1: manifest schema v1.1 now records independent runtime, Metal agent,
  EmbeddingGemma, model, image, asset, and schema pins. Release CI downloads
  and verifies the exact Metal and EmbeddingGemma artifacts, including digest
  and byte size, before packaging.
- P0.2: portal-only `start` no longer races model setup. Metal generation is
  started and health-checked before container consumers; readiness reports
  named progress and terminal runtime errors, with one recovery restart for a
  host agent that becomes healthy after `HOST_AGENT_UNREACHABLE`.
- P0.3: hostd and EmbeddingGemma share a transactional launchd replacement
  helper with plist validation, bounded retry, kickstart, health verification,
  and rollback to the previous loaded service.
- P0.4: smoke chat uses a 512-token reasoning-aware budget, an exact semantic
  answer, and distinct HTTP, reasoning, visible-content, and semantic results.
- P0.5: Caddy exposes only models, chat completions, and embeddings beneath
  `/api/openai/v1`; LiteLLM management paths remain private. Control allowlists
  one-time key data, returns the public base URL, prevents secret-response
  caching, and restricts key administration to appliance administrators. The
  companion `codex/ui-followups` branch provides Copy and confirmed Revoke UI.
- Additional fixes cover SS-006's partial-download race and SS-015's native
  generation shutdown/startup lifecycle.

Automated verification covers the generated manifest against JSON Schema,
shell parsing, release/config generation, launchd retry and rollback, eval
unit tests, all Control and Docker-proxy Go tests and vet checks, Go builds,
and all three Compose configurations. The real-host acceptance cases in this
plan still remain release gates: slow first download, repeated live launchd
replacement, live scoped-key revocation through Caddy, clean Docker Desktop
and Colima installs, and Ubuntu/CUDA.

## P1 — Grandma-friendly clean install

### P1.1 Guided container-engine setup on macOS

Addresses SS-001, SS-002, and SS-003.

- Detect a healthy existing Docker-compatible daemon and Compose plugin first.
- Offer two clearly named supported choices: Lightweight Colima (recommended)
  and Docker Desktop (use an existing installation).
- For Colima, install or guide installation of Colima, Lima, Docker CLI, and
  Compose; merge `cliPluginsExtraDirs` into existing JSON without overwriting
  unrelated Docker settings.
- Create a dedicated `sovereign` Colima profile with reviewed CPU, RAM, disk,
  architecture, and mount settings.
- Store the chosen Docker context in Sovereign state and pass it explicitly to
  every Docker command. Do not silently change the user's global context.
- Run real compatibility probes: Compose, image pull, bind mount, published
  loopback port, and container-to-host `host.docker.internal` access.

Acceptance:

- Works with no container tooling, with a stopped Colima VM, with Docker
  Desktop, and with an unrelated active Docker context.
- Preserves a preexisting `~/.docker/config.json` byte-for-byte except for the
  required merged plugin directory.

### P1.2 Honest capacity and download preflight

Addresses SS-004 and SS-010.

- Measure the real host path used by `~/.sovereign` and the actual engine data
  disk independently through hostd/Colima, not from a shared mount in Control.
- Show downloads and final disk separately: application images, model weights,
  runtime tools, VM overhead, and safe verification/rollback headroom.
- Check free space again before every large artifact and before extraction.
- Support resumable downloads and explain cleanup of partial files.

Acceptance:

- Low-space tests fail before downloading with exact required/available values
  and a safe cleanup/relocation action.
- Colima-reported capacity is within a reasonable tolerance of authoritative
  host and VM values, never a synthetic petabyte-scale mount.

### P1.3 Progress and error language

Addresses SS-006, SS-008, RC-007, and RC-008.

- Create partial files before monitoring; suppress expected zero-byte states.
- Display stage, file/component, transferred/total, speed, elapsed time, and
  resumability for large downloads.
- For every failure provide: what failed, whether existing data is safe, what
  the installer will retry automatically, and one next action.
- Keep JSON/automation stdout clean; send human progress to stderr or a
  structured event stream.

Acceptance:

- No expected condition prints `error`, `No such file`, or a stack trace.
- No active operation is silent for more than 30 seconds.
- Missing ReCursor provider credentials produce a specific setup action rather
  than `Python loop failed`.

### P1.4 Complete first-run onboarding

- Keep the current single-use admin claim, 12-character minimum, local-only
  default, hardware recommendation, and single portal origin.
- If the recommended model is already registered/ready, say `Already ready`
  instead of returning `ready:false` and relying on background polling.
- Show trustworthy disk/download information before `Use recommended setup`.
- End on a real first chat, then show Start at login, Stop, Backup, and Help.
- Keep administrator creation local; never generate a default password.

Acceptance:

- A first-time user reaches a successful chat without leaving the browser or
  copying a secret.
- Refreshing, closing, or reopening each onboarding step resumes safely.

### P1.5 Bundle or consistently diagnose ReCursor's Pkl dependency

Addresses RC-009 and RC-010.

- Prefer the bundled evaluator for every feature. If external Pkl remains
  required, ship the pinned binary in the signed artifact.
- `recursor doctor` must exercise the same Pkl path used by generated tools and
  name any unavailable feature.
- Do not send ordinary users through a Homebrew auto-update for one hidden
  runtime dependency.

Acceptance:

- `/tool create` works immediately after the consumer installer.
- Doctor fails before first use if any required evaluator path is unavailable.

## P1 — Safe lifecycle and repeat testing

### P1.6 Make ordinary start offline and fast

Addresses SS-014.

- `up` must use installed images and assets by default.
- Pull only during install/update or with explicit `--pull`.
- Skip model-loading language for ready components and do not run smoke unless
  explicitly requested or as a clearly labeled one-time post-install stage.

Acceptance:

- Disconnect networking after a successful install: `down` then `up` succeeds.
- A ready same-version `up` does not contact registries and exits zero.

### P1.7 Stop every component predictably

Addresses SS-015.

- Stop runtime agent, llama.cpp child, EmbeddingGemma, and containers on `down`.
- Recreate/start both native services before their consumers on `up`.
- Report what remains intentionally running, if anything.

Acceptance:

- After `down`, documented ports 9100, 9101, 42666, and 54854 are closed and no
  Sovereign model process remains; volumes/configuration/backups remain.
- After `up`, all readiness components return to ready and the admin can log in.

### P1.8 Reinstall/update/repair guarantees

- Same-version reinstall validates/reuses artifacts and performs no destructive
  database/config reset.
- Update always creates and verifies a backup first, stages alongside the old
  release, migrates once, runs health/smoke, then switches atomically.
- On failure, restore release symlink/config/schema compatibility and restart
  the previous healthy services.
- Repair should be the supported recovery for missing plist, stopped VM,
  damaged config, missing image, or interrupted model download.

Acceptance:

- Admins, sessions where compatible, users, scoped keys, workspace data,
  models, branding, network mode, and backups survive same-version reinstall.
- Kill the updater at every stage; either old or new version starts without
  manual filesystem edits.

## P2 — Cross-product experience

### P2.1 Add a Sovereign provider preset to ReCursor

- Detect the local Sovereign health endpoint and offer `SovereignStack on this
  Mac` in ReCursor's provider picker.
- Prefer a browser-mediated local authorization: ReCursor requests a scoped
  key, the user approves in the Sovereign portal, and ReCursor stores it in the
  OS keychain. Do not copy the master key or require manual Docker access.
- Discover stable aliases (`assistant-large`, embedding alias) from `/v1/models`
  and verify one small request before declaring setup complete.

Acceptance:

- Fresh ReCursor connects to an installed Sovereign appliance in one approval
  flow and completes a prompt.
- Revoking the app key in Sovereign produces a clear reconnect action in
  ReCursor.

### P2.2 Support bundles and self-diagnosis

- Include component health, stage journal, versions/digests, Docker context,
  Colima profile facts, sanitized logs, free-space facts, and last smoke report.
- Redact claims, sessions, provider secrets, gateway keys, prompts, and model
  responses by default.
- Provide a human summary before creating/downloading the bundle.

### P2.3 Cleanup and resource controls

- Show installed footprint by category and safe reclaimable space.
- Provide `uninstall` (preserve data/models), `uninstall --purge` (preview and
  confirm exact paths), and optional removal of the dedicated Colima profile.
- Never remove a shared Docker/Colima installation or unrelated images/config.

## Required automated release matrix

Run clean install, first chat, backup, down/up, same-version reinstall, update,
and uninstall-preserve tests for each supported cell:

- Apple Silicon + no existing engine + automated Colima setup.
- Apple Silicon + existing/running Colima with unrelated context/config.
- Apple Silicon + Docker Desktop.
- Apple Silicon + slow/interrupted network and resumed partial models.
- Apple Silicon + offline restart after a completed online install.
- Apple Silicon + low host disk and low Colima data-disk capacity.
- macOS local GUI and headless SSH flows.
- Ubuntu 24.04 + supported NVIDIA GPU/container toolkit.
- ReCursor public distribution and authenticated private distribution, as
  applicable.
- ReCursor on every permitted Python version, with no external Pkl installed.

For every cell, assert:

- exact release artifacts and signatures;
- no secret in stdout, process arguments, support bundle, or world-readable
  files;
- no silence longer than 30 seconds;
- all errors include component, cause, data-safety statement, and next action;
- first chat and embedding succeed;
- repeat install preserves persistent state;
- `down` releases resources and offline `up` succeeds;
- the worktree/source checkout is not required on the consumer machine.

## Approval checklist

The installer is ready for a nontechnical pilot only when:

- all P0 items pass the automated release matrix;
- the Colima path is a documented, tested first-class option;
- a clean install and same-version reinstall both exit zero;
- the exact advertised smoke suite passes with the shipped model;
- the admin can complete first chat through the portal;
- a host client can use a scoped gateway key through a documented endpoint;
- ReCursor has a published consumer artifact and actionable provider setup;
- backup/restore and uninstall-preserve have been exercised on real data;
- no manual restart, plist edit, Docker context change, secret extraction, or
  source build is needed.
