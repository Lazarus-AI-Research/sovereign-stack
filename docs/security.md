# Security

v0.1 is a single-host, localhost-only appliance. Caddy binds to
`127.0.0.1:8880`; operators who intentionally expose it must place an
authenticated TLS reverse proxy in front. All other services are private to
the Compose network.

Control uses an administrator session cookie (`HttpOnly`, `SameSite=Lax`) or a
bearer session token. Generated credentials, the LiteLLM config, agent token,
and vault key are owner-readable only. Provider credentials are encrypted with
AES-256-GCM using a random nonce and record-bound additional authenticated
data. Secret values are accepted on write and never returned by the API.
Control runs as the installing host UID/GID so it can read those owner-only
bind-mounted files without running the container as root.

Control never mounts the Docker socket. A dedicated proxy owns it and enforces:

- an internal bearer token;
- exact operation and service allowlists;
- first-party pull prefixes and exact third-party export digests;
- fixed eval and backup job shapes;
- append-only JSON audit records.

Images, model revisions, and downloadable weights are pinned. Tagged release
images carry build provenance/SBOM attestations and Sigstore signatures; the
release archive and Metal agent are checksum- and signature-verified before
installation.

Metadata tracing is enabled by default, while prompt and response logging are
off. Backups and offline bundles exclude live secrets. Support requests should
use logs and evaluation reports only after reviewing them for customer data.
