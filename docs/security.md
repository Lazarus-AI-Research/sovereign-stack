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

Host lifecycle changes use the separately signed `sovereign-hostd` process.
Control authenticates with an owner-readable random token. The service accepts
only fixed status, desktop/LAN/domain publication, repair, and semantic-version
update requests; target IP addresses and hostnames are validated before any
process starts. It uses `execve`-style argument arrays rather than a shell and
does not accept command names, paths, Compose options, Docker requests, or
arbitrary environment values. Mutations are recorded in an owner-readable
JSONL audit. The unauthenticated surface is limited to `/host/v1/health`.

The first-administrator claim is random, short-lived, single-use, removed from
disk after consumption, and can only be renewed by the owner-authenticated host
CLI. Embedded applications come from a compiled allowlist with fixed
same-origin paths and role requirements; the API cannot register arbitrary
iframe URLs. Caddy remains the only browser ingress and Control remains the
forward-auth authority.

Images, model revisions, and downloadable weights are pinned. Tagged release
images carry build provenance/SBOM attestations and Sigstore signatures. The
release archive and generation Metal agent are checksum- and signature-verified
before installation; the Metal EmbeddingGemma executable is vendored inside
that signed archive and verified again against its pinned checksum.
When Apple signing credentials are unavailable, the macOS bootstrap package is
explicitly named `-unsigned.pkg` and produces the expected Gatekeeper warnings.
It still has detached SHA-256 and GitHub OIDC/Sigstore verification artifacts,
and its bootstrap verifies the signed appliance archive before installation.

Metadata tracing is enabled by default, while prompt and response logging are
off. Backups and offline bundles exclude live secrets. Portal-generated support
bundles include only approved configuration, capacity/version metadata, and the
allowlisted host-operation timeline; environment values are redacted by
allowlist and prompt/response logs are never collected.

Updates accept only the latest version returned by the configured release feed.
The existing installer verifies checksums and Sigstore identity, creates a
verified pre-update backup, runs health and smoke validation, and restores the
previous release symlink and services when installation or validation fails.
