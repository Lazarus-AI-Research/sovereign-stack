# Branding policy

Two rules, opposite in intent. Everything below exists to keep them apart.

## 1. Customer theming is easy

Deployments are meant to be white-labeled. A customer can change, through
Sovereign Control settings or product configuration — no code, no rebuild:

- **Product name** and **company name**
- **Logo** and other brand assets
- **Colors** (primary, accent)

This is deliberately low-friction. It lives in `config/branding.yaml`, is edited
through the Control **Settings → Branding** panel, and applies at runtime — no
rebuild. Colors are pushed onto CSS custom properties (`--accent`, `--primary`)
by `control/web/src/theme.ts`, so changing the accent re-skins the whole UI live;
saving in the panel applies immediately. The login page themes itself too: a
public, allowlisted `GET /api/control/v1/theme` serves only the cosmetic subset
(product name, company/logo/favicon, colors) so it can be read before sign-in,
while the full branding document stays behind auth.

Adding more themeable surface (fonts, additional assets) is welcome; keep it in
the branding config and the `/theme` allowlist so it stays a setting, never a
code change.

## 2. "Powered by Lazarus AI" is fixed

Every deployment carries the attribution **Powered by Lazarus AI**, linking to
<https://github.com/Lazarus-AI-Research/sovereign-stack>:

- **The UI shows it on every page** — login, loading, and every tab.
- **Every non-UI service prints it up front** at startup.

This is a required branding position. It is intentionally *not* part of theming:
the text and link are compile-time constants, not sourced from any
customer-editable value, so re-theming the product cannot alter or remove them.

### The canonical string

> **Powered by Lazarus AI** — https://github.com/Lazarus-AI-Research/sovereign-stack

Every implementation below must match this exactly. Tests pin it so the copies
cannot drift.

### Where it lives

| Surface | Source of truth | Enforced by |
| --- | --- | --- |
| Control UI (every page) | `control/web/src/attribution.ts`, rendered by `PoweredBy.tsx`, mounted at the root in `main.tsx` | `control/internal/web/attribution_test.go` — asserts the **built bundle** carries it |
| Control service (startup) | `control/internal/attribution` | `attribution_test.go` in that package |
| Docker Proxy service (startup) | `docker-proxy/internal/attribution` | `attribution_test.go` in that package |
| Sovereign Runtime (startup) | `lazarus/attribution.py` (sovereign-vllm) | `tests/test_attribution.py` |

### How hard "hard to remove" is

Honest scope: the codebase is Apache-2.0. Anyone with the source can strip this —
trivially, with or without AI. That is not the bar. The bar is that removal is
**inconvenient and conspicuous**, never a toggle:

- It is not a config value, an environment variable, or a feature flag. Theming
  the product cannot touch it.
- Removing it means editing source in a named place and rebuilding.
- A test guards each surface, so a silent removal fails CI and the deletion shows
  up plainly in a diff. To remove it, someone has to also delete the test that
  says "do not remove this," on the record.

### For anyone doing UI work

New pages, new services, redesigns: the attribution comes along for free if you
leave the root mount (`PoweredBy` in `main.tsx`) in place, and any new service
prints `attribution.Banner()` / `attribution.banner()` at startup. Do not move
the badge into a page, do not gate it on a flag, and do not source its text from
config. If you add a new service, add the constant and its guard test with it.
