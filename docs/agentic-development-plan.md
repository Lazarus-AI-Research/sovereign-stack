# SovereignStack Agentic Development Plan

**Status:** Proposed execution playbook; owner interview and approval required before implementation  
**Audience:** Product owner, main OMP agent, implementation agents, and reviewers  
**Roadmap:** [SovereignStack development roadmap](development-roadmap.md)

## Purpose

This document defines how SovereignStack should be built quickly with coding agents without sacrificing
architecture, security, correctness, or product coherence. It is an execution system for the roadmap,
not a replacement for product decisions or milestone acceptance gates.

The operating model is:

> One main agent owns architecture and integration. Specialized agents investigate, implement bounded
> slices, and independently review. Parallel implementation begins only after shared contracts are
> approved and stable.

More agents are useful only when work has explicit boundaries. Unbounded agents, overlapping ownership,
and vague prompts create incompatible implementations faster.

## Non-negotiable startup rule

The main agent must begin in read-only mode.

Before editing code or documentation, it must:

1. Read this plan, the development roadmap, and the relevant repository contracts.
2. Inspect the current implementation and distinguish implemented, partial, missing, and unverified work.
3. Interview the product owner about unresolved product, architecture, engineering, security, UX, release,
   and agent-autonomy decisions.
4. Recommend concrete defaults and explain material tradeoffs.
5. Produce a decision register, architectural proposal, dependency graph, and first implementation slice.
6. Obtain explicit owner approval to edit and implement.

Research, analysis, plans, phase transitions, or a plausible first slice are not permission to edit.
The owner must explicitly approve implementation.

The main agent must not ask the owner for facts available from the repository, tools, existing documentation,
or authoritative external sources. It should investigate those facts first and ask only for decisions,
preferences, unavailable business context, or genuinely unresolved product intent.

## Current planning context

Treat the current repository state as the implementation baseline. A release tag may be recorded for
traceability, but it must not redefine "current" or imply that old release claims remain qualified.

Known model-planning context:

- The product requires a lightweight ReAligned model for offline first chat and the 16 GB Mac demo tier.
- ReAligned Qwen3.5 2B is the currently available fallback candidate, not a permanently locked product choice.
- A ReAligned Qwen3.8-family refresh is expected, but exact sizes, artifacts, revisions, quantizations, and
  runtime compatibility must not be invented before publication.
- When Qwen3.8 artifacts are available, they must pass the same qualification gate as Qwen3.5 before becoming
  the default.
- The 16 GB demo must not depend on future SlimServe support. It should use the simplest qualified Metal path.
- A ReAligned SlimServe profile is allowed only after upstream support and SovereignStack qualification.

The main agent must confirm which roadmap decisions are approved, which are qualification candidates, and
which remain open. It must not treat headings such as "locked decisions" as owner approval by themselves.

## Authority and source order

When sources conflict, use this order:

1. Current owner instructions and explicitly approved decisions.
2. Approved architecture decisions, public API contracts, schemas, and security invariants.
3. The development roadmap and release acceptance gates.
4. Current source code, migrations, tests, and observed runtime behavior.
5. Repository design and operations documentation.
6. Versioned UI reference briefs.
7. Source-verified upstream documentation and repositories.
8. Inference, convention, or agent preference.

Conflicts among the first five sources must be surfaced. An agent may not silently choose whichever source
makes implementation easiest.

## Phase A — Read-only discovery and owner interview

### Repository discovery

The main agent should inspect only the files and symbols needed to understand the selected roadmap area.
It should use existing patterns rather than create a second convention.

For the initial program-level review, establish:

- Current component boundaries and dependency direction.
- Current identity, route, model, artifact, runtime, hardware, job, and workspace ownership.
- Current API, persistence, lifecycle, and error contracts.
- Existing release claims versus behavior that has actually been qualified.
- Security boundaries around Control, hostd, the restricted Docker proxy, gateways, runtimes, and secrets.
- Current UI information architecture and its backend sources of truth.
- Hardware and model profiles that are certified, demo-only, experimental, or merely present in source.
- Missing acceptance evidence and hardware-dependent verification gaps.

The discovery output must classify each relevant capability as:

- **Implemented and verified** — observed behavior has current evidence.
- **Implemented but unqualified** — code exists, but the release journey or target hardware has not passed.
- **Partial** — a foundation exists, but the roadmap contract is incomplete.
- **Missing** — no end-to-end implementation exists.
- **Conflicting** — code, documentation, roadmap, or product intent disagree.

### Read-only reconnaissance agents

The main agent may fan out independent read-only research in one OMP batch:

| Agent | Responsibility |
| --- | --- |
| `scout` | Map current files, symbols, callsites, schemas, tests, and repository conventions. |
| `librarian` | Verify upstream engines, SDKs, model artifacts, licenses, and hardware APIs from primary sources. |
| `designer` | Analyze current UI and reference-product workflows; identify interaction and state requirements. |
| `security-reviewer` | Identify trust boundaries, attack paths, secret exposure, and unsafe authority expansion. |
| `reviewer` | Identify architectural contradictions, migration risks, and weak acceptance criteria. |

Research agents must return evidence, not implementation. Their output should contain:

- Files, symbols, URLs, or observed behavior supporting each claim.
- Existing invariants and conventions.
- Missing or conflicting contracts.
- Risks with severity and consequence.
- Decisions that require owner input.
- Proposed acceptance scenarios.
- Explicit uncertainty.

### Interview protocol

The interview should be conducted in coherent batches rather than one open-ended question at a time. The
main agent should present two to five concrete options for material decisions, recommend the safest boring
choice, and explain the tradeoff. It should skip questions already answered by the owner or repository.

#### Product and release

Confirm:

- Primary R0 user and the single most important release journey.
- Controlled evaluator, public beta, and production distinctions.
- Which roadmap outcomes are commitments versus future candidates.
- Required offline behavior and which external capabilities may be enabled.
- What must explicitly not be built in the current release.
- Certified, demo, experimental, community, and unsupported terminology.
- Whether unsigned packages are restricted to controlled evaluation and when signing becomes a release gate.

#### Architecture and data

Confirm:

- Service ownership and dependency direction.
- Which APIs and persisted records are public or compatibility-sensitive.
- Clean-cutover versus backward-compatibility requirements for existing records and clients.
- Migration and rollback expectations.
- State-machine, idempotency, retry, cancellation, and recovery requirements.
- Rules for adding dependencies, services, abstractions, and generated code.
- Engine-adapter boundaries and what may be engine-specific.
- Which runtime facts must be observed rather than inferred.

#### Coding practices

Confirm:

- Preferred language and framework conventions where the repository has more than one valid pattern.
- Error taxonomy and user-facing error requirements.
- Logging, metrics, traces, and audit expectations.
- Unit, contract, integration, browser, hardware, and clean-system test expectations.
- Dependency-update and supply-chain policy.
- Formatting, linting, static analysis, and generated-file policy.
- Branch, commit, and PR practices, including when agents may create them.
- Documentation and changelog expectations.

#### Security and operations

Confirm:

- Threat model and highest-consequence assets.
- Authorization boundaries for administrators, managers, members, hostd, and automation.
- Secret storage, redaction, backup, and support-bundle policy.
- Network egress and telemetry policy.
- Host operation allowlists and prohibited authority.
- Artifact signing, checksum, provenance, and update requirements.
- Recovery objectives for model deployment, upgrades, backup, repair, and optional-service failure.

#### Models, engines, and hardware

Confirm:

- Hardware available for continuous qualification and release gates.
- Minimum supported and demo-only profiles.
- ReAligned model-selection and artifact-freeze process.
- Performance measurements versus pass/fail thresholds.
- Context, concurrency, memory-headroom, and failure expectations.
- Engine order and the conditions for adding SlimServe, SGLang, or general llama.cpp selection.
- Multi-GPU topology, homogeneity, placement, and unsupported-configuration policy.

#### UI and product references

Confirm:

- Reference products and exact workflows to study, including the LM Studio version and platform.
- Which behaviors should be adapted and which should not be copied.
- Brand, visual language, density, accessibility, keyboard, responsive, and localization expectations.
- Required loading, empty, progress, degraded, error, offline, and recovery states.
- Advanced-mode boundaries and role-gated navigation.
- Required browser and device targets.

#### Agent autonomy

Confirm:

- Decisions agents may make without returning to the owner.
- Decisions requiring architecture or owner approval.
- Whether agents may create files, migrations, dependencies, branches, commits, or PRs.
- Required independent reviewers for each risk class.
- Hardware, credentials, services, or environments agents may use.
- Conditions for stopping, escalating, or narrowing scope.

#### Definition of done

Confirm the evidence required to claim:

- A feature is implemented.
- A feature is qualified.
- A hardware profile is supported.
- A release journey passes.
- A UI is ready.
- A migration is safe.
- A security boundary is acceptable.

### Interview output

Before seeking implementation approval, the main agent must present:

1. **Decision register** — approved, proposed, open, and rejected decisions.
2. **Current-state matrix** — implemented, unqualified, partial, missing, and conflicting behavior.
3. **Architecture invariants** — rules that every implementation must obey.
4. **Open risks** — severity, consequence, control, and owner.
5. **Reference requirements** — missing screenshots, recordings, hardware, credentials, or upstream information.
6. **Dependency graph** — contracts and slices in execution order.
7. **First slice** — exact scope, non-goals, acceptance, and proof.

No files are changed until the owner explicitly approves implementation.

## Phase B — Architecture and work graph

The main agent owns architecture synthesis. It must not delegate the repository-wide design to a blank general
agent or choose a design by subagent majority vote.

### Minimal architecture artifacts

Create only artifacts that constrain implementation or preserve a load-bearing decision:

- Component ownership and dependency map.
- Architecture decision records for consequential, hard-to-reverse choices.
- OpenAPI, JSON Schema, database, and typed interface contracts.
- Lifecycle state machines and stable error taxonomies.
- Threat model for new authority or data flows.
- Versioned UI reference briefs for user-facing work.
- Acceptance matrix linking roadmap requirements to observable proof.
- Dependency-ordered issue graph.

Prefer executable contracts over prose. Do not create speculative abstraction documents without a current
consumer or decision.

### Progressive contract locking

Do not attempt to freeze the entire future architecture at once. Lock contracts in dependency order:

1. Domain identity and ownership.
2. Persistence and migration.
3. API representation.
4. Lifecycle transitions and errors.
5. Host and engine adapter contracts.
6. UI consumption.
7. Operations, observability, and recovery.

Once a shared contract is approved, downstream agents may work in parallel against it. Contract changes must
return through the architecture owner rather than being improvised by a consumer.

### Starting architecture invariants to confirm

These are proposed defaults. The owner and main agent must confirm or revise them during the interview.

- Control owns product identity, desired state, deployment records, routes, jobs, and authorization.
- The portal consumes Control APIs and never invents deployment, hardware, or engine state.
- Hostd supplies host facts and fixed allowlisted host operations; it accepts no arbitrary command or path.
- The restricted Docker proxy exposes only explicit lifecycle operations.
- Engine-specific flags and process details remain inside engine adapters.
- Model artifacts have immutable repository, revision, file, format, quantization, checksum, and license identity.
- A deployment plan has no runtime side effects.
- Route changes are explicit, atomic, and recoverable.
- Requested configuration and observed runtime reality are distinct.
- Secrets never appear in catalogs, deployment records, manifests, logs, analytics, or support bundles.
- Offline is the default; network egress requires an explicit administrator-enabled capability.
- Optional-service failure cannot make core Chat unavailable.
- Persistent identities never depend on transient host indexes.
- Unsupported configurations remain visible with actionable reasons but are not presented as working.
- No new public capability appears in the UI before its end-to-end path passes.

## UI reference-driven development

"Make it like LM Studio" is not an actionable specification. Reference products must be converted into
versioned behavior briefs.

### Reference acquisition

For each reference product and platform, capture the relevant version and these journeys where applicable:

- First launch and prerequisite checks.
- Hardware detection and capacity explanation.
- Model discovery, filtering, and details.
- Fit, recommendation, and incompatibility language.
- Download, verification, load, start, stop, and retry.
- Local server and API configuration.
- Active-model and resource visibility.
- Failure, remediation, diagnostics, and recovery.
- Normal versus advanced settings.
- Offline behavior.

Use owner-provided screenshots or recordings, public product material, and direct observation when available.
Do not rely on an agent's memory of a product UI.

### Reference brief

For each borrowed pattern, record:

| Field | Meaning |
| --- | --- |
| Reference behavior | The observed interaction, state, or information hierarchy. |
| User problem | Why the behavior is useful. |
| SovereignStack adaptation | How it maps to this product and its security model. |
| Backend source | The API, job, planner, or deployment state that makes it truthful. |
| Non-goals | What must not be copied or implied. |
| Acceptance | Observable behavior required in the real product. |

Reference interaction principles, not proprietary assets, branding, copy, or trade dress.

### Required state matrix

Every production surface must intentionally handle:

- Loading.
- Empty.
- Ready.
- Long-running progress.
- Degraded dependency.
- Recoverable failure.
- Terminal failure.
- Unauthorized or role-restricted access.
- Offline or unavailable external capability.
- Cancellation, retry, and recovery where supported.
- Desktop, narrow viewport, keyboard, and screen-reader use.

The designer and frontend implementation agent receive both the reference brief and the approved backend
contract. The frontend may not create optimistic product state that the backend cannot explain.

### UI verification

Verify UI changes against the real running portal:

- Browser-drive the complete changed journey.
- Inspect semantic accessibility and keyboard behavior.
- Exercise required viewport sizes.
- Capture the relevant visual states.
- Confirm all displayed status comes from real backend contracts.
- Confirm failures offer direct actions rather than raw internal errors or log-only instructions.

Pixel similarity to a reference product is not the goal. Workflow clarity, state honesty, accessibility, and
SovereignStack product coherence are the goals.

## OMP execution model

### Main-agent responsibilities

The main agent retains:

- Product interpretation.
- Architecture synthesis.
- Shared contract ownership.
- Dependency ordering.
- Task decomposition.
- Integration across agents.
- Final validation.
- Honest reporting of unverified work.

### Agent selection

Use the most specific available agent:

| Agent | Use |
| --- | --- |
| `scout` | Read-only repository exploration and current-state mapping. |
| `librarian` | Source-verified external library, model, engine, and API research. |
| `designer` | Interaction design, reference analysis, implementation guidance, and visual review. |
| `security-reviewer` | Read-only threat and vulnerability review. |
| `reviewer` | Independent correctness, maintainability, and architecture review. |
| `task` | Bounded multi-step implementation with explicit ownership. |
| `sonic` | Strictly mechanical, low-risk updates with no design judgment. |

Start with bundled agents. Add a project-specific agent only when the same specialized role and output contract
recur often enough to justify it. Prefer skills for reusable knowledge and workflows rather than creating an
agent zoo.

### Task packet contract

Every implementation assignment must include:

```text
Target:
  Exact files, symbols, service, or user journey.

Context:
  Current behavior and approved decisions.

Invariants:
  Rules that cannot be violated.

Contract:
  API, schema, state machine, or interface implemented or consumed.

Change:
  Required behavior and migration.

Non-goals:
  Explicit scope the agent must not add.

Ownership:
  Files this agent may edit and shared files owned elsewhere.

Acceptance:
  Observable behavior required.

Evidence:
  Exact scenario, command, or browser journey proving the change.
```

Broad prompts such as "implement M4," "improve deployment," or "make it production ready" are prohibited.

### Structured research and review output

Research and review agents should return a structured result containing:

- Findings with severity.
- File, symbol, URL, or runtime evidence.
- Violated or affected invariant.
- Consequence.
- Recommended action.
- Open questions.
- Verification gaps.

Use OMP output schemas when practical so the main agent can merge findings without interpreting several
incompatible essay formats.

### Parallelization rules

Parallelize only genuinely independent work.

Before every batch:

- Freeze and share the interface each agent implements or consumes.
- Assign one writer per file or shared boundary.
- Name one integration owner for irreducibly shared changes.
- State cross-task contracts in the shared batch context.
- Prevent sibling agents from independently solving the same architecture problem.
- Instruct implementation agents to skip project-wide validation while siblings are still editing.

Suitable parallel work includes separate service implementations, independent read-only reviews, hardware
research, UI reference analysis, and tests that consume an already approved contract.

Unsuitable parallel work includes multiple agents designing one schema, frontend and backend inventing an API
independently, or several agents editing the same migration, OpenAPI file, state machine, or central component.

Use isolated OMP workspaces for independent implementation agents when available. Do not treat successful
patch application as proof that independently designed changes integrate correctly.

### Agent supervision

Use Agent Hub to inspect live activity, transcripts, ownership, and patches. Steer an agent as soon as it drifts
from its task packet. Stop work that begins adding unapproved abstractions or scope.

Reuse an informed idle or parked agent through `hub` for follow-up work rather than spawning a blank replacement.
A prior agent's context is valuable only when its original scope remains relevant.

## Roadmap decomposition

A roadmap milestone is an epic, not a single implementation prompt or PR.

Decompose each milestone into dependency-ordered vertical slices. A valid slice:

- Produces one coherent observable behavior.
- Leaves the repository in a consistent state.
- Has explicit migration and rollback behavior when persistent state changes.
- Can be reviewed without understanding several unrelated changes.
- Has evidence that would fail if the behavior were broken.
- Does not advertise future UI or API capabilities prematurely.

For example, multi-accelerator inventory should be split into contract definition, current single-device
representation, stable host identity, multiple-device enumeration, topology, Metal unified-memory semantics,
incompatibility reporting, portal consumption, and physical-host qualification.

The initial pilot slice for this process should be:

> Represent the currently active single runtime through the first approved deployment-domain contract and
> expose its relationship to the current artifact and stable route.

This slice forces the program to establish domain ownership, identity, API conventions, migration behavior,
current-state introspection, UI consumption, review, and proof before multiplying concurrency.

## Per-slice implementation workflow

### 1. Intake

Restate the selected roadmap acceptance criterion, current behavior, non-goals, dependencies, and required proof.
Reject a slice that cannot be stated as observable behavior.

### 2. Reconnaissance

Use read-only agents to map affected code, existing conventions, external dependencies, security boundaries,
and UI references. Resolve factual questions before asking the owner.

### 3. Contract

The main agent or designated contract owner defines the schema, interface, state transitions, errors, migration,
and acceptance matrix. Consumers do not start implementation until this contract is approved.

### 4. Approval gate

Return to the owner for public APIs, persistent data, trust boundaries, irreversible migration, product
navigation, engine semantics, or release claims.

### 5. Implementation

Fan out bounded independent work. Each agent fixes the source of the behavior, migrates every in-scope caller,
and removes obsolete paths rather than adding indefinite compatibility layers.

### 6. Independent review

Run separate read-only correctness, architecture, security, operations, and design reviews as applicable. The
implementation agent is not the only reviewer of its own work.

### 7. Behavioral verification

The main agent or integration owner runs the actual changed path. Tests support proof but do not replace running
the relevant API, UI, CLI, installer, deployment, or hardware scenario.

### 8. PR preparation

Include only the coherent slice and its required contracts, migrations, tests, and documentation. Exclude
unrelated generated or workspace files. Report unverified hardware or end-to-end paths prominently.

## Engineering and architecture enforcement

Agent instructions guide behavior; executable constraints enforce it.

### Enforcement layers

1. **Context and rules** — architecture, conventions, and non-negotiable restrictions.
2. **Types and schemas** — OpenAPI, JSON Schema, database constraints, generated clients, and typed interfaces.
3. **Dependency boundaries** — package/module rules and adapter interfaces.
4. **Runtime checks** — state-transition validation, authorization, idempotency, checksums, and fail-closed behavior.
5. **Tests** — observable contracts, failure paths, migrations, and release journeys.
6. **CI** — formatting, static analysis, schema validation, security checks, builds, and targeted suites.
7. **Qualification** — real browser, installer, hardware, offline, backup, update, and recovery evidence.

Repeated agent mistakes should become a schema, compiler check, test, lint rule, CI gate, skill, or narrowly
focused hook. Do not respond by endlessly expanding prompts.

### Coding rules

- Correctness before speed; maintainability before cleverness.
- Reuse the existing repository pattern; do not create a second convention.
- Prefer boring direct implementations over speculative abstractions.
- Introduce an abstraction for a real boundary or demonstrated repeated behavior, not a hypothetical future.
- Keep engine-specific behavior behind adapters.
- Keep state explicit, versioned, and explainable.
- Treat retries, cancellation, rollback, and recovery as state-machine behavior, not scattered conditionals.
- Preserve no obsolete aliases, shims, flags, or re-exports after an approved clean cutover.
- Use symbol-aware references before changing exported code.
- Do not suppress symptoms, warnings, or errors instead of fixing their source.
- Avoid unnecessary allocations, copies, polling, and repeated computation in compiled or hot-path code.
- New dependencies require a concrete benefit, license review, supply-chain review, and removal plan if replaced.
- Generated files must come from an authoritative source and reproducible command.

### Test rules

Tests must defend observable contracts and fail on plausible regressions. Prioritize:

- Boundaries and invariants.
- State transitions.
- Migration and rollback.
- Authorization and secret handling.
- Real error propagation and recovery.
- Compatibility behavior.
- Browser journeys.
- Hardware identity and placement.

Do not add tests that merely assert source text, mocks, plumbing calls, or incidental implementation details.
Implementation agents should run only their focused checks while parallel work is active. The integration owner
runs the applicable combined validation once the batch is integrated.

## Security review gates

A security review is mandatory when a slice changes:

- Authentication, authorization, roles, invitations, sessions, or keys.
- Hostd or restricted Docker proxy authority.
- Paths, commands, container arguments, device visibility, or host filesystem access.
- Secret storage, encryption, logging, backups, bundles, or redaction.
- Network exposure, egress, telemetry, external providers, or update channels.
- Artifact download, checksum, signature, provenance, or license handling.
- Tool execution, plugins, workflows, or user-supplied content reaching an interpreter.
- Route switching, multi-tenant access, or cross-workspace data.

Security findings require file- or behavior-level evidence. A generic statement that code "looks secure" is
not an acceptance result.

## Evidence and definition of done

A feature is done only when its specified end-to-end behavior and acceptance criteria are implemented and
observed. Compiling scaffolds, mock-only paths, partial callers, placeholders, or passing unrelated tests are
not completion.

Every slice should maintain an evidence matrix:

| Requirement | Implementation | Proof | Status |
| --- | --- | --- | --- |
| Observable contract | Files and symbols | Request, scenario, or test | Pass/fail/unverified |
| Failure and recovery | State transitions | Forced failure scenario | Pass/fail/unverified |
| Security invariant | Enforcement point | Security review or negative test | Pass/fail/unverified |
| UI behavior | Real portal path | Browser journey and observed state | Pass/fail/unverified |
| Operations behavior | Restart/backup/update path | Runtime scenario | Pass/fail/unverified |

Verification must match the changed surface:

- API — exercise the real request and response.
- UI — browser-drive the actual portal.
- CLI or TUI — launch and interact with the program.
- Deployment lifecycle — exercise preflight through route transition and rollback.
- Installer — run the clean-system journey on the target profile.
- Hardware — observe physical identity, placement, and runtime manifest on target hardware.
- Offline — disable network access and run the promised journey.

Unavailable hardware or credentials must be reported as unverified. Their absence cannot be hidden behind a
unit test or inferred success claim.

## PR discipline

Each PR should implement one coherent slice and contain:

- Approved contract changes.
- Implementation and all migrated callers.
- Required persistence migration and rollback behavior.
- Focused tests for new observable behavior.
- Required documentation and release-claim updates.
- Acceptance matrix and exact verification evidence.
- Explicit unverified environments or journeys.

A PR must not contain unrelated workspace files, opportunistic refactors, speculative future APIs, disabled
warnings, hidden compatibility layers, or generated changes with no source update.

Review order should be:

1. Contract and architecture.
2. Correctness and migration.
3. Security.
4. Operations and failure recovery.
5. UI behavior and accessibility.
6. Maintainability and unnecessary complexity.
7. Verification evidence.

## Learning loop

After each merged slice, identify:

- Which assumption was wrong.
- Which review finding arrived too late.
- Which agent prompt allowed drift.
- Which repository boundary was only prose.
- Which verification step caught the real defect.
- Which repeated manual step should become a skill or command.

Convert recurring failures into executable enforcement. Remove rules that no longer protect a real invariant.
The goal is a progressively safer and faster system, not an ever-growing instruction document.

## Kickoff directive for the main agent

When this document is supplied to the main agent, it should begin with this behavior:

1. Remain read-only.
2. Read the roadmap and authoritative current-state contracts.
3. Use targeted read-only subagents to map the repository and verify external dependencies.
4. Summarize what is implemented, partial, missing, unqualified, or conflicting.
5. Interview the owner in decision-oriented batches, recommending a default for each material choice.
6. Produce the decision register, architecture invariants, dependency graph, and proposed first slice.
7. Ask for explicit approval to implement.
8. Only after approval, execute the per-slice OMP workflow defined here.

The main agent must continue to stop at later approval gates for new public contracts, trust boundaries,
persistent migrations, product semantics, and release claims even after general implementation approval.
