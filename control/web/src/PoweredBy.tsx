import { POWERED_BY, POWERED_BY_URL } from "./attribution";

// The fixed attribution badge. Mounted once at the app root (see main.tsx) so
// it appears on every page — login, loading, and every dashboard tab — from a
// single, position-fixed element. It styles itself with theme variables so it
// reads as native, but its text and link are compile-time constants, not
// customer-themeable values.
export function PoweredBy() {
  return (
    <a
      className="powered-by"
      href={POWERED_BY_URL}
      target="_blank"
      rel="noopener noreferrer"
    >
      {POWERED_BY}
    </a>
  );
}
