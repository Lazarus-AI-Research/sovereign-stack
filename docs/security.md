# Security

The default mode is single-host: Caddy binds to `127.0.0.1:8880`. The host CLI
can bind the same authenticated portal to an RFC1918 address, or configure a
public hostname with Caddy-managed TLS. Public cleartext HTTP is rejected
unless the operator supplies an explicit insecure-transport acknowledgement.
All other services remain private to the Compose network.

Control owns multi-user identity with administrator, manager, and member roles.
It uses a session cookie (`HttpOnly`, `SameSite=Lax`, and `Secure` under TLS) or a
bearer session token. The owner-only first-admin claim, LiteLLM config, agent token,
and vault key are owner-readable only. Provider credentials are encrypted with
AES-256-GCM using a random nonce and record-bound additional authenticated
data. Secret values are accepted on write and never returned by the API.
Workspace identities and memberships are mirrored through one-time SSO;
passwords are never shared with Workspace. Control runs as the installing host UID/GID so it can read those owner-only
bind-mounted files without running the container as root.

Control never mounts the Docker socket. A dedicated proxy owns it and enforces:

- an internal bearer token;
- exact operation and service allowlists;
- first-party pull prefixes and exact third-party export digests;
- fixed eval and backup job shapes;
- append-only JSON audit records.

Images, model revisions, and downloadable weights are pinned. Tagged release
images carry build provenance/SBOM attestations and Sigstore signatures. The
release archive and generation Metal agent are checksum- and signature-verified
before installation; the Metal EmbeddingGemma executable is vendored inside
that signed archive and verified again against its pinned checksum.

Metadata tracing is enabled by default, while prompt and response logging are
off. Backups and offline bundles exclude live secrets. Support requests should
use logs and evaluation reports only after reviewing them for customer data.
