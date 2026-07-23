# CUDA Validation Results

## 2026-07-23 EmbeddingGemma validation

The `embeddinggemma.c` CUDA path was validated on `quixi-3090-02`
(`nv3090-02`), an Ubuntu host with eight NVIDIA GeForce RTX 3090 GPUs, NVIDIA
driver 580.65.06, Docker 28.2.2, and Docker Compose 2.36.2. Validation used GPU
1 while the existing Sovereign deployment continued serving generation on GPU
0. The production deployment and gateway configuration were not modified.

The validation image was built from the current `embeddinggemma/Dockerfile.cuda`
and had image ID
`sha256:da584e21950145b21ad40881c0e965abf8c07a061f0819a77a7c034c4f877d3a`.
It contained:

- `embeddinggemma.c` v0.3.1, CUDA executable SHA-256
  `6ed7b9eabb5d9f835a7a485fddc96e10b05a25e2b959faa761b55f4be5378de3`;
- `embeddinggemma-300M-qat-Q4_0.gguf` at revision
  `8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73`, SHA-256
  `50d28e22432a148f6f8a86eab3700f92add5d1f54baf7790675a2a4dadbccf26`;
- CUDA 12.9.1 runtime, with all executable dependencies resolved.

The service ran as UID/GID 65532, its model mount was read-only, and its test
port was bound only to loopback. The CUDA process used approximately 988 MiB
of VRAM (approximately 998 MiB total card usage during the test).

### Results

All of the following checks passed:

- `GET /healthz` readiness after model load;
- the original `POST /api/embed` API, returning one requested 128-dimensional
  vector;
- the additive OpenAI-compatible `POST /v1/embeddings` API, returning the
  stable `embedding-gemma-default` alias, 768 dimensions, unit L2 norm, and
  token usage;
- a 32-input batch at 256 dimensions, preserving count and index order;
- OpenAI `encoding_format: base64`, where 256 float32 values decoded to 1,024
  bytes;
- rejection of invalid dimensions with HTTP 400;
- 32 warm concurrent requests from 16 clients, all returning HTTP 200 and 768
  dimensions;
- an isolated LiteLLM instance using the shipped configuration: the embedding
  alias appeared in `/v1/models`, a two-input request returned ordered
  768-dimensional unit vectors, and a request without the gateway key returned
  HTTP 401;
- semantic retrieval through LiteLLM, which ranked the expected private-local-AI
  document first;
- insertion and cosine retrieval through the pinned pgvector image using a real
  `vector(768)` column. The expected ranking was `private-ai`, `tomatoes`, then
  `orchestra`, with cosine similarities 0.663013, 0.250270, and 0.226165;
- failure isolation: restarting only EmbeddingGemma changed its PID and start
  time, while `sovereign-runtime` remained healthy with PID 992445, zero
  restarts, and the unchanged start time
  `2026-07-15T04:07:37.927538997Z`. LiteLLM embedding traffic recovered after
  `GET /healthz` reported readiness.

The direct warm micro-smoke observed 13.35 ms for a single 768-dimensional
OpenAI request, 7.95 ms for the 32-input batch, and approximately 919 requests
per second with 15.22 ms p95 latency in the small concurrency check. The first
native request, including cold-path effects after readiness, took 132.84 ms.
These figures confirm function and basic concurrency only; they are not a
capacity benchmark.

### Isolation and cleanup

The temporary EmbeddingGemma, LiteLLM, and pgvector containers were separate
from the production Compose project. pgvector used a network-disabled,
memory-backed data directory. After validation, all three containers, the
validation image tag, model/config directory, and loopback listeners were
removed. GPU 1 returned to its 4 MiB idle baseline. The production runtime
still had the same PID, start time, zero restart count, and healthy status; all
existing Sovereign containers remained up.

### Remaining release gates

This validates the core CUDA binary, both embedding APIs, LiteLLM routing,
pgvector compatibility, and process-level restart isolation. It does not
replace a fresh installation from the signed offline bundle, a complete
Compose lifecycle test on the target host, prolonged soak testing, or
simultaneous generation-and-embedding pressure on the same GPU. Those remain
release-certification gates.

The earlier 2026-07-15 report covered the retired in-runtime multimodal
embedding implementation and is not evidence for the backend that now ships.
