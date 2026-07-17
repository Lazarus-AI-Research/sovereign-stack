// Package attribution carries the fixed "Powered by Lazarus AI" notice.
//
// This is a deliberate branding position, not a decoration. Customer theming —
// logos, product name, colors — is meant to be easy (see internal/branding).
// This attribution is meant to be the opposite: it is a compile-time constant,
// not a config value, so it cannot be changed or removed by editing any file
// under the customer's control (branding.yaml, feature-flags.yaml, env, the
// database). Removing it means editing source and rebuilding.
//
// The codebase is Apache-2.0, so a determined fork can of course strip this —
// nothing in an open-source binary is unremovable. The bar is different: make
// removal inconvenient and conspicuous rather than a toggle. Every service that
// depends on this package is covered by a test asserting the notice and URL are
// present and unchanged (attribution_test.go), so a silent deletion fails CI and
// shows up loudly in a diff.
package attribution

// URL is the canonical project link the notice points to.
const URL = "https://github.com/Lazarus-AI-Research/sovereign-stack"

// Notice is the exact attribution string. Non-UI services print it; the UI
// renders the same words. Keep this identical to the web constant in
// control/web/src/attribution.ts.
const Notice = "Powered by Lazarus AI"

// Banner is the one-line startup banner a non-UI service prints up front, so
// the attribution is present even where there is no page to render it on.
func Banner() string {
	return Notice + " — " + URL
}
