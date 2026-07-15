# Hardware Profiles

v0.1 has exactly two supported profiles. Other Compose overlays remain
development or post-MVP material and are not release claims.

| Profile | Certified host | Runtime implementation | v0.1 capability |
| --- | --- | --- | --- |
| `metal-arm64` | Apple Silicon, 32 GB+ unified memory | Linux contract container delegates to a signed launchd-managed llama.cpp Metal agent | text chat, text embeddings, pgvector RAG |
| `cuda-x86_64` | Ubuntu 24.04, NVIDIA GPU with 24 GB+ VRAM | single Sovereign Runtime container using the CUDA engine | text chat, text/image/audio embeddings, cross-modal retrieval |

`deploy/scripts/detect-hardware.sh` fails closed when the host is outside this
matrix. On CUDA it verifies Ubuntu, GPU memory, `nvidia-smi`, and the Docker
NVIDIA runtime. On Mac it verifies architecture and unified memory. Detection
does not install or change host software.

The validated CUDA baseline uses generation and embedding concurrency of two,
2048-token generation context, eager execution, and memory weights 52/40. The
Metal baseline uses pinned Q4 Gemma and Q8 Nomic GGUF artifacts with checksums.
Exact revisions live in `release/release-source.json` and the release manifest.
