# LiteLLM Feature Licensing Inventory

design.md §2.2 requires a feature-by-feature licensing inventory for every
LiteLLM capability Sovereign Stack uses. LiteLLM's proxy is MIT-licensed with
specific **enterprise-gated features** (license key required at runtime).
This inventory must be re-verified against the pinned LiteLLM version at
every version bump and before each release (M16 gate).

Pinned image: see `LITELLM_IMAGE` in `deploy/.env.example`.
**TODO before release: pin an immutable tag and re-verify every row below
against that exact version's docs/source.**

## Features Sovereign Stack uses

| Capability | Where we use it | License tier (verify per pin) |
| --- | --- | --- |
| OpenAI-compatible proxy routing (`model_list`) | Gateway core (§15); all chat/embedding traffic | Open source |
| Master key auth (`general_settings.master_key`) | Gateway admin auth | Open source |
| Virtual keys (`/key/generate`, `/key/list`) | Control §18.8 key management | Open source (basic); some key-management sub-features are enterprise |
| Budgets / spend tracking (`max_budget`) | Planned: §18.8 budgets | Open source (basic budgets); advanced budget routing historically enterprise-adjacent — verify |
| Rate limiting (tpm/rpm per key) | Planned (§2.2) | Open source (basic) — verify tier for per-team limits |
| DB-backed config (`DATABASE_URL`) | Key/spend persistence in the `litellm` database | Open source |
| `drop_params` | §15 config | Open source |
| Prometheus `/metrics` | Sovereign Observe scraping | **Historically enterprise-gated** — verify at pin; if gated, scrape gateway health only and derive usage from DB spend logs instead |
| Request timeout settings | §15 config | Open source |

## Enterprise features we must NOT depend on (unless licensed)

- SSO/JWT auth for the LiteLLM UI (we never expose the UI at all, §2.2)
- Audit logs, guardrails, secret-manager integrations
- Advanced admin UI features (unused: UI is hidden)

## Compliance posture

1. The LiteLLM UI is never exposed, embedded, or linked (§2.2, §28.9).
2. Every capability above is exercised through documented management APIs or
   generated configuration only.
3. If a used feature turns out enterprise-gated at the pinned version, either
   license it commercially or replace it (the table lists fallbacks where
   known).
4. Re-run this review at every `LITELLM_IMAGE` bump; record the verification
   date and version here.

| Verified against version | Date | Verifier | Notes |
| --- | --- | --- | --- |
| _(pending pin)_ | — | — | Initial inventory drafted from general LiteLLM licensing structure; row-by-row verification required at pin time. |
