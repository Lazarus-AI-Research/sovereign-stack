# Hardware Profiles

v0.1 has exactly two supported profiles. Other Compose overlays remain
development or post-MVP material and are not release claims.

| Profile | Certified host | Runtime implementation | v0.1 capability |
| --- | --- | --- | --- |
| `metal-arm64` | Apple Silicon, 32 GB+ unified memory | Linux contract container delegates to a signed launchd-managed llama.cpp Metal agent | text chat, text embeddings, pgvector RAG |
| `cuda-x86_64` | Ubuntu 24.04, NVIDIA GPU with 24 GB+ VRAM | CUDA generation container plus private `embeddinggemma` CUDA service | text chat, text embeddings, pgvector RAG |

`deploy/scripts/detect-hardware.sh` fails closed when the host is outside this
matrix. On CUDA it verifies Ubuntu, GPU memory, `nvidia-smi`, and the Docker
NVIDIA runtime. On Mac it verifies architecture and unified memory. Detection
does not install or change host software.

The validated CUDA baseline uses a 2048-token generation context and eager
execution. Both profiles use the same pinned 768-dimensional EmbeddingGemma
Q4 GGUF; CUDA runs it in a sibling container and Metal runs it on the host.
Exact revisions live in `release/release-source.json` and the release manifest.
