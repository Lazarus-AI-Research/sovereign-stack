# Third-party notices

SovereignStack composes independently licensed open-source components. Their
licenses remain controlling for those components.

## AnythingLLM

The `sovereign-workspace` image is derived from AnythingLLM 1.15.0 at commit
`1c2b2a7523b83b3640858c2aaf9f9e0ff8847536`, distributed under the MIT
License. Copyright remains with the AnythingLLM contributors. The image keeps
the upstream license at `/app/LICENSE` and marks the Sovereign modifications.

## LiteLLM and observability services

LiteLLM, PostgreSQL/pgvector, Phoenix, Prometheus, Grafana, Loki, OpenTelemetry
Collector, and Caddy are unmodified third-party services. Release automation
records their exact image digests and license metadata in the release SBOM.

## embeddinggemma.c

The local embedding service packages `embeddinggemma.c` v0.3.1 from QuixiAI,
distributed under the MIT License. The CUDA image retains the license at
`/usr/share/licenses/embeddinggemma.c/LICENSE`; the Metal executable is
vendored from the checksum-pinned upstream release into the signed
SovereignStack release archive, and its license is installed beside it.
