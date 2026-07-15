# LiteLLM Feature Licensing Inventory

design.md §2.2 requires a feature-by-feature licensing inventory for every
LiteLLM capability Sovereign Stack uses. LiteLLM's proxy is MIT-licensed with
specific **enterprise-gated features** (license key required at runtime).
This inventory must be re-verified against the pinned LiteLLM version at
every version bump and before each release (M16 gate).

Pinned image: LiteLLM v1.91.2, source revision
`6950a52a151e606a5e535170d2c9f6bf263593cf`, at the immutable digest recorded
as `LITELLM_IMAGE` in `deploy/.env.example`.

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
| Prometheus `/metrics` | Not used; Control exports gateway health and the product reads open-source spend APIs | No enterprise dependency |
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
| v1.91.2 / `6950a52` | 2026-07-14 | release review | `/key/generate`, `/key/list`, `/key/update`, `/key/delete`, and `/global/spend` are present in the open-source proxy source; the LiteLLM UI and enterprise router are not exposed or required. |
