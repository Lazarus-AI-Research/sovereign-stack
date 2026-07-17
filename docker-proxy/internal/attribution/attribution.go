// Package attribution carries the fixed "Powered by Lazarus AI" notice for the
// Docker Proxy service.
//
// This is a required branding position (see the project BRANDING.md). It is a
// compile-time constant, not a config value, so no customer-editable file can
// change it. The proxy is a separate Go module from control, so the constant is
// duplicated here rather than imported — the value is pinned by the test in this
// package, and the canonical string lives in BRANDING.md, so the two modules
// cannot drift silently.
package attribution

// URL is the canonical project link the notice points to.
const URL = "https://github.com/Lazarus-AI-Research/sovereign-stack"

// Notice is the exact attribution string.
const Notice = "Powered by Lazarus AI"

// Banner is the one-line startup banner the service prints up front.
func Banner() string {
	return Notice + " — " + URL
}
