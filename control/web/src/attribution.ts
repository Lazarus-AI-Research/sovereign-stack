// The fixed "Powered by Lazarus AI" attribution.
//
// This is a required branding position, NOT part of customer theming. Logos,
// product name, and colors are meant to be easy to change (see the branding
// settings). This is meant to be the opposite: these constants are baked into
// the bundle at build time and are not sourced from the branding API, config,
// or any customer-editable value — so re-theming the product cannot alter or
// remove them.
//
// Removing this means editing source and rebuilding the bundle. The codebase is
// Apache-2.0, so a fork can strip it; the point is that it is inconvenient and
// conspicuous, not a toggle. A test asserts the built bundle still contains the
// URL (see control/internal/web/attribution_test.go), so a silent removal fails
// CI. Keep POWERED_BY identical to control/internal/attribution and BRANDING.md.

export const POWERED_BY = "Powered by Lazarus AI";
export const POWERED_BY_URL =
  "https://github.com/Lazarus-AI-Research/sovereign-stack";
