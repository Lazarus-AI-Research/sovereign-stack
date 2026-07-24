# SovereignStack UX Improvement Plan

Status: Implemented; release qualification and moderated usability measurement pending
Scope: Installation, onboarding, navigation, model management, embedded tools, operations, updates, recovery, accessibility, and compatibility

Implementation note (2026-07-24): milestones 1–5 and the engineering portions
of milestone 6 are implemented in this repository. Native release publication
requires the configured Apple Developer ID/notarization secrets. The 8-of-10
first-time-user measure remains a release research gate because it requires
moderated sessions rather than a code change.

## 1. Objective

Make SovereignStack as easy to install and use as Ollama, LM Studio, and Open WebUI while preserving the appliance's security, auditability, advanced configuration, and existing APIs.

The target experience is:

1. The user launches one signed installer or one server install command.
2. SovereignStack starts its control portal before downloading model weights.
3. A browser opens automatically on desktop; a headless install presents one reachable URL.
4. The user creates the first administrator in the browser.
5. SovereignStack detects the hardware and recommends a supported default model.
6. The user accepts the recommendation with one click and sees download, verification, and load progress.
7. Chat becomes usable as soon as the model is ready.
8. Grafana, Phoenix, API/provider configuration, users, backups, updates, and diagnostics remain accessible through the same authenticated portal and navigation.
9. Normal operation never requires entering a Docker container, remembering ports, or editing configuration files.

## 2. Product principles

- **Task first:** Chat and the user's work are primary. Infrastructure is secondary.
- **Useful defaults:** Detect hardware and recommend a known-good configuration.
- **Progressive disclosure:** Hide repository IDs, revisions, artifacts, checksums, aliases, pooling, and runtime details until the user chooses Advanced.
- **Portal first:** Start the control experience before optional runtimes and models are ready.
- **Observable operations:** Every long operation reports its current stage, progress, failure, and recovery actions.
- **One origin:** Users enter through one portal and never memorize service URLs or ports.
- **Safe host control:** Do not solve host management by mounting an unrestricted Docker socket into the portal.
- **Additive compatibility:** Preserve existing APIs, CLI behavior, model profiles, and custom embedding support while adding simpler workflows.
- **Local by default:** Diagnostics and product analytics stay on the appliance unless the owner explicitly opts in.

## 3. Problems to address

### 3.1 Installation and access

- Model downloads currently delay portal startup.
- Desktop installs do not provide a true native one-click package and lifecycle experience.
- Headless users can receive a loopback URL that is not reachable from their computer.
- Changing LAN or domain access requires host-side commands.
- Users must understand Docker, Compose, ports, or SSH to recover from common problems.
- There is no stable local discovery address or lightweight host launcher.

### 3.2 Onboarding

- First-run setup creates an administrator but does not guide hardware selection, network access, model provisioning, or the first prompt.
- Claim-link recovery depends on the host CLI.
- Model readiness is reduced to a binary status and provides no useful progress or action.

### 3.3 Navigation and information architecture

- Too many infrastructure destinations appear as equal top-level choices.
- Chat, models, embeddings, evaluations, observability, people, credentials, resilience, and settings compete for attention.
- The current Access label describes credentials and gateway keys rather than network access.
- Grafana and Phoenix are reachable, but the experience feels like a collection of services rather than one product.
- The portal lacks a strong responsive/mobile navigation model, keyboard navigation, and remembered sidebar state.

### 3.4 Models and embeddings

- The primary model workflow exposes product IDs, providers, repositories, revisions, artifacts, checksums, and stable aliases.
- Hardware detection does not provide enough capacity data to make reliable model recommendations.
- Downloads are performed outside the control-plane job system.
- Users cannot see byte progress, rate, ETA, verification, loading, or smoke-test stages.
- Users cannot safely cancel or retry operations.
- EmbeddingGemma should be the simple default for most customers, while sovereign-vLLM and OpenAI-compatible/custom embeddings must remain available.

### 3.5 Errors, updates, and recovery

- UI errors frequently expose raw error messages without an actionable recovery step.
- Destructive operations use browser-native confirmation dialogs.
- There is no complete signed update, health-validation, and automatic rollback experience.
- Support-bundle functionality is incomplete.
- Repair and restore operations are not presented as guided workflows.

### 3.6 Frontend quality

- The main dashboard is too monolithic to evolve safely.
- Shared modal, toast, progress, skeleton, empty-state, and error-state primitives are incomplete.
- Accessibility, mobile behavior, localization readiness, and end-to-end UX testing need explicit ownership and release gates.

## 4. Target information architecture

After onboarding, `/` should open Chat. The application shell should contain the following:

### Primary navigation

- Chat
- Activity
- Tools
- System status

### Tools

- Grafana
- Phoenix
- API and provider connections
- Additional installed applications discovered from a controlled application registry

### Administration

- Models
- Embeddings
- Evaluations
- Users and invitations
- Network access
- Backups and recovery
- Updates
- Advanced settings

### Account menu

- Profile
- Appearance
- Documentation
- Sign out

Administration should be role-gated and visually secondary. The sidebar should collapse on desktop, become a drawer on mobile, remember its state, and expose accessible names and keyboard navigation.

The existing Access section should be renamed **API & Providers**. A new **Network Access** section should own desktop, LAN, and domain publication.

## 5. Architecture decisions

### 5.1 Portal-first installation

Split installation into two stages:

1. Install and start the minimum control plane: Caddy, Control, database, and the assets required to render the portal.
2. Provision runtimes, models, and optional services as observable background jobs.

Online model downloads must move out of the blocking installer path. Offline bundles can retain prepackaged artifacts, but the portal should still display verification and loading progress.

### 5.2 Narrow host-management service

Introduce a signed `sovereign-hostd` service managed by launchd on macOS and systemd on Linux. It should expose a versioned, allowlisted local API over a Unix socket.

Permitted operations should be narrowly defined:

- Report host OS, CPU, RAM, GPU/VRAM, storage, and service state.
- Start, stop, restart, and reconcile the SovereignStack application.
- Change approved network publication modes.
- Stage and apply signed updates.
- Roll back to a known-good release.
- Collect redacted host diagnostics.

It must not expose a shell, arbitrary filesystem operations, arbitrary Compose arguments, or unrestricted Docker access. Control should authenticate through socket permissions and a versioned request protocol. Every mutating operation should be audited.

The existing `sovereign` CLI remains supported and becomes a client of the same host-management interface where practical.

### 5.3 Unified background jobs

Extend the job model beyond `queued|running|succeeded|failed` with structured operation state:

- Stage and human-readable status
- Current and total work
- Bytes completed and total
- Transfer rate and ETA
- Structured error code
- Suggested recovery actions
- Cancellation request and cancellation state
- Retry relationship
- Heartbeat/last-progress time
- Initiating user and audit metadata

Add APIs to list recent jobs, stream job changes with server-sent events, request cancellation, and retry eligible failures. Polling may remain as a compatibility fallback.

Handlers must accept a progress reporter and cancellation context. Existing job creation and lookup contracts should remain valid.

### 5.4 Curated model catalog

Create a signed catalog that separates product-friendly choices from registry implementation details. Each entry should define:

- Display name, description, and capabilities
- Runtime and accelerator compatibility
- Minimum and recommended VRAM/RAM
- Required disk space and download size
- Repository, revision, artifact, and checksum
- Default context/configuration
- Stability channel and replacement/update relationship

Catalog metadata should be cacheable for offline operation. Advanced users must still be able to register custom local, sovereign-vLLM, or OpenAI-compatible models through the existing additive APIs.

### 5.5 Unified readiness model

Replace the binary readiness label with a resource that reports independent components:

- Portal
- Authentication
- Generation model
- Embedding model
- Workspace
- Gateway
- Observability tools

Each component should report `starting`, `downloading`, `verifying`, `loading`, `ready`, `degraded`, or `failed`, plus an optional related job and recovery action. Slow optional tools must not block Chat or the rest of the portal.

### 5.6 Same-origin application registry

Represent bundled tools in a controlled application registry containing route, label, icon, required role, health endpoint, and embed/open behavior. Caddy should continue routing tools under the portal origin, and Control should remain the authentication authority.

Do not let arbitrary administrators add unrestricted iframe URLs. Content security policy, allowed origins, and authentication forwarding must remain explicit.

## 6. Delivery plan

### Milestone 0: Baseline and contracts

Deliverables:

- Record clean-install and first-chat timings on macOS, Ubuntu desktop, and an Ubuntu headless host.
- Document the current API routes and CLI behavior that must remain compatible.
- Define the job-event, readiness, model-catalog, application-registry, and hostd protocols.
- Threat-model hostd, claim/bootstrap flows, embedded applications, and update rollback.
- Add feature flags for new portal navigation, managed downloads, and hostd integration.

Exit criteria:

- Protocols have versioning and compatibility rules.
- Security review has no unresolved critical risks.
- Automated tests capture the existing APIs that must not break.

### Milestone 1: Frontend shell and activity foundation

Deliverables:

- Break the dashboard into route-level pages and shared components.
- Implement the new responsive application shell and role-gated administration area.
- Default `/` to Chat after onboarding.
- Add the Tools section and move Grafana, Phoenix, and provider configuration into it.
- Rename Access to API & Providers and introduce the Network Access destination.
- Add modal, confirmation, toast, tooltip, skeleton, empty-state, error-state, and progress primitives.
- Add the Activity Center and global system-status indicator.
- Add keyboard navigation, responsive layouts, focus management, and persisted sidebar state.

Exit criteria:

- All existing portal functions remain reachable.
- No destructive action uses `window.confirm`.
- Core navigation works at 320 px width and with keyboard-only input.
- Chat renders even when optional services are slow or unavailable.

### Milestone 2: Observable jobs and readiness

Deliverables:

- Add backward-compatible database migrations for job progress, cancellation, retry, and audit data.
- Add progress reporting and cancellation support to job handlers.
- Add job list, event stream, cancellation, and retry APIs.
- Implement the unified readiness endpoint.
- Connect model loads, index rebuilds, evaluations, backups, restores, bundles, and update preparation to the Activity Center.
- Add stable application error codes and UI mappings with user actions.

Exit criteria:

- Long-running operations survive page refresh and continue reporting state.
- Eligible operations can be canceled and retried.
- A stalled job is detected and explained.
- Raw technical errors are available for diagnostics but are not the primary user message.

### Milestone 3: Portal-first installer and host lifecycle

Deliverables:

- Reorder installation so the control portal starts before online model downloads.
- Convert runtime/model provisioning into managed jobs.
- Implement and package `sovereign-hostd` for launchd and systemd.
- Make the `sovereign` CLI use the host-management protocol while preserving its commands and output contracts where possible.
- Add desktop browser launch and stable local discovery with a displayed fallback URL.
- Add headless installation selection for LAN or domain exposure before onboarding.
- Add macOS signed/notarized packaging and an Ubuntu `.deb`; retain the shell installer for servers and automation.
- Display a single reachable URL and optional QR code after headless installation.

Exit criteria:

- No model download blocks the initial portal.
- Desktop installation opens the correct browser URL automatically.
- A remote install never presents an unusable loopback URL as its primary result.
- Normal access-mode changes require no container access or manual configuration editing.
- Existing installations without hostd continue to work through the CLI fallback.

### Milestone 4: Guided onboarding and model experience

Deliverables:

- Expand first-run setup into a resumable wizard:
  1. Create administrator.
  2. Confirm detected hardware.
  3. Choose local, LAN, or domain access where applicable.
  4. Accept a recommended model or choose another curated model.
  5. Watch provisioning progress.
  6. Send the first prompt.
- Improve hardware inventory with GPU model, usable VRAM, RAM, free disk, OS, architecture, and acceleration support.
- Add the signed curated model catalog and recommendation engine.
- Replace registry-first forms with friendly model cards and a searchable picker.
- Show downloading, verification, loading, smoke testing, ready, stale, and failed states.
- Add download cancellation, retry, disk preflight, and compatibility explanations.
- Ship EmbeddingGemma as the recommended default embedding profile.
- Put custom embedding models, sovereign-vLLM embedding serving, pooling, normalization, prefixes, repository pins, aliases, and provider credentials under Advanced.
- Preserve all existing registry and embedding APIs.

Exit criteria:

- A supported default installation requires no model IDs, revisions, checksums, aliases, or pooling knowledge.
- Installing the recommended model takes one confirmation.
- Installing another curated model takes no more than two selections/clicks.
- Incompatible hardware and insufficient disk are detected before download.
- Existing custom generation and embedding configurations still load without migration loss.

### Milestone 5: Updates, repair, backup, and support

Deliverables:

- Add a signed release feed and version/update API.
- Show update notifications and a once-per-version What's New experience.
- Support download, staging, scheduling, pre-update backup, application, health validation, and automatic rollback.
- Finish redacted support-bundle generation and portal download.
- Add a Repair workflow that reconciles configuration and restarts only unhealthy components.
- Redesign backup, verification, restore, and rollback as guided operations with explicit impact summaries.
- Add a local operation/audit history and diagnostic timeline.

Exit criteria:

- A normal update is one click after review.
- A failed post-update health check restores the previous working release automatically.
- Users can generate a redacted support bundle without terminal access.
- Restore and repair communicate scope, downtime, and reversibility before execution.

### Milestone 6: Product polish and release qualification

Deliverables:

- Add accessibility testing and resolve WCAG 2.2 AA blockers.
- Add localization scaffolding and remove hard-coded layout assumptions.
- Add reduced-motion support and complete light/dark theme validation.
- Improve empty states, inline help, documentation links, and terminology consistency.
- Conduct moderated clean-install usability tests with people unfamiliar with the stack.
- Optimize portal and route-loading performance; lazy-load optional administrative tools.

Exit criteria:

- No critical automated or manual accessibility failures.
- At least 8 of 10 first-time test users complete install, onboarding, model setup, and first chat without documentation or assistance.
- Optional tool failures do not degrade core Chat availability.

## 7. Compatibility and rollout

All changes must be additive:

- Keep existing model, embedding, runtime, and gateway endpoints.
- Add fields to API responses without removing or changing the meaning of existing fields.
- Use new versioned endpoints when a response shape cannot be safely extended.
- Keep custom model and embedding configuration available under Advanced.
- Preserve sovereign-vLLM's ability to serve embeddings for specialized deployments.
- Preserve the current EmbeddingGemma API while adding catalog-managed lifecycle operations around it.
- Keep the current `sovereign` commands operational.
- Migrate existing database state forward; never replace user model profiles or access configuration with recommendations.
- Make hostd optional during the upgrade transition and clearly report when a portal action requires hostd or the CLI fallback.
- Release the new portal and managed-download path behind feature flags, test upgrades, and then make them the default in a later release.

## 8. Test strategy

### Automated browser journeys

- First administrator creation
- Expired and renewed bootstrap setup
- Hardware recommendation acceptance
- Alternative curated model selection
- Custom model configuration
- EmbeddingGemma default setup
- sovereign-vLLM/custom embedding setup
- Download interruption, cancellation, resume/retry, verification failure, and insufficient disk
- First chat before and after model readiness
- Role-gated navigation
- Grafana and Phoenix launch under the authenticated portal
- LAN/domain access changes
- Update success, failed health check, and rollback
- Backup, verify, restore, repair, and support bundle
- Mobile and keyboard-only navigation

### Clean-system matrix

- Supported macOS Apple Silicon versions
- Supported Ubuntu NVIDIA systems
- Ubuntu headless installation over SSH
- Online and offline bundle installation
- Fresh install and upgrade from each supported release
- Slow, interrupted, and unavailable network conditions
- Low disk, unsupported GPU, missing driver, and runtime crash scenarios

### Security checks

- Hostd authorization and request allowlist
- No arbitrary command or path injection
- No unrestricted Docker socket exposure
- Setup-link expiry and replay prevention
- Same-origin routing and iframe content-security policy
- Secret redaction in errors, logs, audits, and support bundles
- Signed catalog, runtime, model, and update verification

## 9. Product success measures

Track these locally and expose them to administrators. Do not transmit them without opt-in.

- Time from installer launch to reachable portal
- Time from administrator creation to first successful response
- Percentage of installations completed without manual recovery
- Model download and load failure rates by error code
- Percentage of failures recovered through Retry or Repair
- Number of terminal/CLI steps required after installer launch
- Upgrade success and automatic rollback rates
- First-time usability completion rate

Targets:

- Zero container commands in every supported user journey.
- Zero memorized service URLs or ports.
- Portal available as soon as core services are installed; model downloads never block it.
- One confirmation for the recommended model.
- One-click normal update with automatic health validation.
- At least 80% unaided first-install-to-first-chat completion in usability testing, with a follow-up target of 90%.

## 10. Recommended implementation order

The critical path is:

1. Job progress and readiness contracts.
2. Portal shell and Activity Center.
3. Portal-first installation.
4. Narrow host lifecycle service.
5. Hardware inventory and curated model catalog.
6. Guided onboarding and model picker.
7. Updates, rollback, repair, and support.
8. Accessibility, localization, performance, and usability qualification.

Do not begin with a visual reskin. Without observable jobs and portal-first installation, the product would still make users wait without meaningful feedback. Conversely, the job/readiness foundation can be shipped incrementally and improves models, embeddings, evaluations, backups, restores, and updates through one shared system.

## 11. Definition of Ollama/Open WebUI-level UX

SovereignStack reaches the intended UX level when a supported customer can:

- Install it through a signed package or one server command.
- Reach one correct portal URL without understanding networking or containers.
- Create the administrator and configure the appliance entirely in the browser.
- Accept a hardware-appropriate model without model-registry expertise.
- Understand, cancel, retry, and recover every meaningful background operation.
- Chat while treating Grafana, Phoenix, providers, models, users, updates, backups, and diagnostics as parts of the same product.
- Update or repair the installation without SSH or container access.
- Use advanced/custom model and embedding capabilities when needed without the simplified defaults limiting them.
