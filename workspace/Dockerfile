FROM mintplexlabs/anythingllm@sha256:de3b7dfdb25b3d83776cda5c90489c36ffc9a292d53a8b04b0a28d868f1fd7d4

ARG VERSION=dev
LABEL org.opencontainers.image.title="Sovereign Workspace" \
      org.opencontainers.image.description="AnythingLLM with Sovereign versioned-index integration" \
      org.opencontainers.image.source="https://github.com/Lazarus-AI-Research/sovereign-stack" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0 AND MIT"

COPY workspace/sovereign/preload.js /app/server/sovereign/preload.js
COPY workspace/MODIFICATIONS.md /usr/share/doc/sovereign-workspace/MODIFICATIONS.md
COPY LICENSE /usr/share/licenses/sovereign-workspace/Apache-2.0.txt
COPY THIRD_PARTY_NOTICES.md /usr/share/licenses/sovereign-workspace/THIRD_PARTY_NOTICES.md

ENV NODE_OPTIONS="--require=/app/server/sovereign/preload.js"
EXPOSE 3011
