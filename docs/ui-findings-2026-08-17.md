# Sovereign Portal UI Findings

Review date: 2026-08-17

Scope: authenticated first use of Sovereign Stack `v0.1.0-rc.6` on an Apple
Silicon Mac with Colima. These findings are intentionally separated from the
installer-hardening work and should be implemented and reviewed in a dedicated
UI PR.

## UI-001 — Chat opens on a 404 page after login

- Severity: high first-use failure
- Observed: immediately after signing in as the first administrator, the
  default Chat page displays `404 not found` instead of the embedded workspace.
- Context: the portal shell and authenticated Control APIs are healthy. An
  HTTP-only SSO probe returned a successful page during installation review,
  so this needs a real browser/iframe navigation test rather than another
  backend health check.
- Likely investigation surface: the `/api/control/v1/workspace/sso` iframe
  handoff, `/apps/chat` prefix stripping, AnythingLLM root-relative routes, the
  one-time SSO redirect, and post-login browser history/navigation.
- Desired behavior: the first authenticated page loads a usable Chat workspace.
  If Workspace is unavailable, show a portal-owned error state with Retry,
  status details, and a link to System—not a raw upstream 404.

Acceptance:

- A browser end-to-end test creates or logs into an admin, lands on Chat, waits
  for the iframe, and asserts that the AnythingLLM composer is visible.
- Reloading the portal, navigating away/back, and performing a full appliance
  restart do not produce a 404.
- Expired/consumed SSO tokens are renewed automatically without exposing a raw
  token URL or upstream error to the user.

## UI-002 — The Models experience is unexpectedly sparse

- Severity: medium product/expectation gap
- Observed: the Models page contains very few choices. The current catalog API
  exposes only the recommended Gemma generation model and EmbeddingGemma.
- User impact: the page looks unfinished or empty without explaining whether
  the small catalog is deliberate, filtered by hardware, already installed, or
  still loading. It is not obvious how to add a compatible local or remote
  model.
- Desired behavior: clearly distinguish Installed, Recommended for this Mac,
  Available, and Custom/remote models. Explain why incompatible models are
  hidden or disabled and provide an obvious Add model action.

Acceptance:

- The page states how many models are installed and available and why the
  curated list may be small.
- The shipped models show role, size, status, compatibility, source, and stable
  alias in plain language.
- Empty/loading/error states are visually distinct and actionable.
- A manager can discover the documented custom/OpenAI-compatible model flow
  without reading the repository README.

## UI-003 — Backups & Recovery renders blank

- Severity: high because it hides a safety-critical feature
- Observed: selecting the Recovery/Backups & Recovery page produces a blank
  content area. The CLI backup path and backend API were independently verified:
  a six-file backup completed and verified successfully.
- User impact: a user cannot tell whether no backups exist, the page is loading,
  access is denied, or the frontend crashed. A blank recovery screen undermines
  confidence before updates/reinstalls.
- Desired behavior: render a stable page shell immediately, then show existing
  backups, create/verify/restore actions, loading progress, or an explicit
  error with retry/support details.

Acceptance:

- A browser test opens the page with zero backups and with at least one verified
  backup; neither state is blank.
- Frontend exceptions and failed API calls produce a visible error boundary and
  Retry action.
- The verified backup created by CLI appears in the portal with timestamp,
  version, size/files, verification state, and safe restore guidance.

## UI-004 — Collapsing the sidebar creates a persistent navigation trap

- Severity: medium to high usability/accessibility defect
- Observed: after collapsing the sidebar, no visible control appears to expand
  it again. Refreshing does not recover because the collapsed state is persisted
  in `localStorage` under `sovereign-sidebar-collapsed`.
- User impact: one click can permanently hide normal navigation for that browser
  profile. Recovering requires developer-tool knowledge or clearing site data.
- Desired behavior: keep an always-visible, sufficiently sized Expand sidebar
  button when collapsed. Persistence is acceptable only when the stored state
  remains reversible.

Acceptance:

- At every supported viewport and zoom level, collapsing leaves a visible
  button with `aria-label="Expand sidebar"` that restores navigation.
- The button is keyboard reachable, has a visible focus state, and works after
  reload with persisted collapsed state.
- Add a defensive escape path: mobile menu, keyboard shortcut, or automatic
  reset if the expand control cannot be rendered.
- Browser tests cover persisted `true`, absent/corrupt localStorage values,
  narrow windows, and 200% zoom.

## Suggested PR boundary

Keep this work out of the installer-hardening PR. A focused portal PR should:

1. Add browser-level coverage for authenticated shell and embedded apps.
2. Fix UI-001, UI-003, and UI-004 as functional regressions.
3. Improve the Models information architecture and empty states in UI-002.
4. Add a top-level error boundary so future page failures never render as an
   unexplained blank area.

Do not fold container-engine, release artifact, launchd, installer progress, or
reinstall changes into this UI PR; those are tracked on the separate
`codex/installer-hardening` branch.

## Implementation status on `codex/ui-followups`

All four findings have repository fixes on this branch:

- UI-001: the 404 was traced to a browser-visible `/apps/chat/sso/simple`
  prefix that AnythingLLM's root-based router cannot recognize. Control now
  sends the iframe to `/sso/simple` directly and supplies a post-login route to
  the first accessible workspace (or Workspace administration for a new
  administrator with no workspace). The iframe keeps the SSO handoff hidden,
  renews it once automatically, and renders portal-owned Retry/System status
  UI for failures.
- UI-002: Models now explains the hardware-curated catalog, separates reviewed
  installed/available and custom/remote counts, exposes role, alias, size,
  compatibility and source details, and has distinct loading, error, and empty
  states with an obvious Add model action.
- UI-003: Backups & Recovery normalizes nullable lists, always renders a stable
  loading/error/empty shell, reports files, aggregate size and verification,
  and is protected by the new top-level portal error boundary.
- UI-004: the collapse control remains visible and keyboard reachable in the
  collapsed layout. Invalid persisted values reset safely, storage failures do
  not trap the session, and Escape expands the sidebar.

The same PR adds the portal half of scoped gateway-key usability: one-time
secret and base-URL Copy controls, a shell-history-safe connectivity example,
normalized metadata, and confirmed Revoke. That panel depends on the
normalized gateway contract in `codex/installer-hardening` and should land
after or alongside it.

The production bundle builds successfully, the complete Control Go suite
passes, and a regression test inspects the embedded production assets. A real
browser journey against a rebuilt appliance is still required before merge to
prove the AnythingLLM composer, restart/reload behavior, zero/nonzero backup
states, persisted collapse state at narrow/200% layouts, and live key
revocation end to end.
