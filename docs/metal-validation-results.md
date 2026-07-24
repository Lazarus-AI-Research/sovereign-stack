# Apple Metal Validation Results

The Metal results recorded on 2026-07-15 covered the former embedding engine
inside the host runtime agent and are not evidence for the current embedding
backend.

The active design now installs `embeddinggemma.c` v0.3.1 as a dedicated,
loopback-only launchd service. The process uses Metal directly on the host;
Docker Desktop reaches it through `host.docker.internal`. Generation continues
through the separately authenticated Sovereign Runtime host agent.

Release certification must rerun these gates before describing the new Metal
embedding path as validated:

- checksum verification for the Metal executable and EmbeddingGemma GGUF;
- launchd installation, upgrade, stop/start, and scoped uninstall;
- direct and gateway text embeddings at 768 dimensions with L2 normalization;
- Docker Desktop access to the loopback-bound host service;
- pgvector retrieval and concurrent generation/embedding pressure;
- online and same-platform offline installation.

Historical reports remain archived separately and intentionally are not used
as current release evidence.
