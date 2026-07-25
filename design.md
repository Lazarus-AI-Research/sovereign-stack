# Lazarus Sovereign Stack

## Implementation and Design Specification

**Version:** 0.7
**Company:** Lazarus AI Research
**Product:** Sovereign Stack
**Status:** Draft — Core architecture locked
**Supersedes:** Version 0.5 and all prior addenda
**Primary deployment:** Docker Compose
**Source hosting:** GitHub
**Image registry:** GitHub Container Registry (GHCR)
**Control plane implementation:** Go
**Primary inference runtime:** Lazarus fork of vLLM
**Embedding backend:** `embeddinggemma.c` with `ggml-org/embeddinggemma-300M-qat-q4_0-GGUF`

---

# 1. Executive Summary

Lazarus Sovereign Stack is a local-first AI appliance for small offices, workgroups, and customer-owned hardware.

It provides a complete private AI environment including:

* Branded chat and document workspace
* Local text generation
* Local text embeddings
* Document ingestion and retrieval
* Image, audio, and text understanding
* Remote and cloud model access
* User and model administration
* Feature controls
* Metrics, logs, traces, benchmarks, and evaluations
* Backup and offline deployment tooling
* A single customer-facing control plane

The product is designed to run without Kubernetes on:

* Mac Studio
* MacBook Pro
* NVIDIA 1–4 GPU workstations
* NVIDIA tinybox-class systems
* NVIDIA DGX Spark
* AMD ROCm workstations
* AMD Strix Halo / Ryzen AI Max+ 395
* Intel XPU systems
* Intel Gaudi 2 / Gaudi 3 systems

The primary product flow is:

```text
Sovereign Workspace
        ↓
Sovereign Gateway
        ↓
Sovereign Runtime
        ↓
Local accelerators and models
```

Initial implementation mapping:

```text
AnythingLLM
    ↓
LiteLLM
    ↓
Lazarus-custom vLLM
```

The core invariant is:

```text
One product.
One control plane.
One gateway.
One product endpoint.
One vLLM generation process.
One isolated embedding service.
One Lazarus vLLM fork.
Multiple platform-specific Docker images.
```

---

# 2. Locked Product Decisions

## 2.1 Workspace providers

Sovereign Stack will support interchangeable user-workspace providers.

Initial provider:

```text
AnythingLLM
```

Future provider:

```text
LibreChat
```

Version 0.1 will focus on AnythingLLM while preserving a provider interface for later LibreChat support.

Sovereign Workspace must never communicate directly with Sovereign Runtime. All model requests pass through Sovereign Gateway.

---

## 2.2 Sovereign Gateway

LiteLLM provides:

* Local model routing
* Remote and cloud model routing
* API keys
* Rate limits
* Budgets and quotas
* Provider credentials
* Usage accounting
* Customer-facing model aliases
* Gateway telemetry

The LiteLLM UI will not be exposed, embedded, or linked.

Sovereign Control will manage LiteLLM through:

* Supported management APIs
* Generated configuration
* Database-backed configuration where appropriate
* Health and metrics endpoints

A feature-by-feature licensing inventory must be maintained for all LiteLLM capabilities used by Sovereign Stack.

---

## 2.3 Phoenix and tracing

Arize Phoenix is enabled by default.

Default behavior:

```yaml
phoenix:
  enabled: true

tracing:
  enabled: true
  metadata_only: true
  full_trace: false
  prompt_logging: false
  response_logging: false
```

Metadata-only tracing may include:

* Model alias
* Runtime profile
* Token counts
* Timing
* Error type
* Request identifier
* Workspace identifier
* Evaluation identifier

Prompt and response content must not be captured unless explicitly enabled by an administrator.

---

## 2.4 Prompt logging

Prompt logging defaults to off.

```yaml
prompt_logging:
  enabled: false
```

When enabling content logging, the administrator must explicitly configure:

* Prompt logging
* Response logging
* Full prompt-response capture
* PII redaction
* Secret redaction
* Retention period
* Workspace or user scope

---

## 2.5 Backups

Normal backups do not include model weights.

Backups include:

* Product configuration
* PostgreSQL databases
* pgvector collections
* Workspace metadata
* User and access configuration
* LiteLLM configuration
* Branding
* Feature flags
* Model registry metadata
* Evaluation results
* Benchmark history
* Dashboard configuration

Backups exclude:

* Model weights
* Hugging Face cache
* ModelScope cache
* Runtime compilation cache
* Temporary downloads

Offline installation bundles may include model weights.

An offline bundle is a distribution artifact, not a backup.

---

## 2.6 Mac Metal

Mac deployments require Docker Desktop.

The Mac generation runtime is distributed and operated through Docker. Native
inference processes that require Metal are installed and managed on the host.

Runtime image:

```text
ghcr.io/lazarus-ai-research/sovereign-runtime:metal-arm64-<version>
```

The signed generation agent and `embeddinggemma` are separate launchd jobs.
Both bind host loopback; Docker Desktop reaches them through
`host.docker.internal`.

Sovereign Gateway and Sovereign Control use:

```text
http://sovereign-runtime:8000
http://host.docker.internal:42666
```

Mac-specific implementation details must not leak into the rest of the product.

---

## 2.7 Lazarus vLLM fork

Lazarus AI Research will maintain a complete fork of vLLM.

The fork includes:

* Generation models
* CUDA support
* ROCm support
* Strix Halo support
* Intel XPU support
* Intel Gaudi 2 / Gaudi 3 support
* Apple Metal support
* DGX Spark support
* Runtime manifests
* Health endpoints
* Metrics normalization
* Model lifecycle management
* Evaluation hooks
* Appliance integration
* Lazarus platform kernels and patches

---

## 2.8 XPU kernels

Lazarus XPU kernel work remains private.

Intel will be responsible for upstreaming appropriate changes to upstream vLLM XPU kernel projects.

The Sovereign XPU image remains a Lazarus-owned integration and optimization target.

---

## 2.9 Model policy

Sovereign Stack will not enforce a strict supported-model allowlist.

Administrators may attempt to load:

* Hugging Face models
* ModelScope models
* Local filesystem models
* Offline-bundled models
* Remote OpenAI-compatible models
* Cloud models through LiteLLM

The system should warn rather than block.

Example:

```text
This model has not been validated on the selected runtime profile.
Loading may fail, consume excessive memory, or perform poorly.
Run a smoke test after loading.
```

---

## 2.10 Performance policy

Sovereign Stack will not define a minimum acceptable benchmark for each hardware target.

The product will provide:

* One-click smoke test
* Quick benchmark
* Full benchmark
* Retrieval evaluation
* Multimodal evaluation
* Historical comparison
* Exportable reports

Administrators determine whether the results meet their needs.

---

## 2.11 Runtime updates

Automatic runtime updating and automatic rollback are deferred.

Version 0.1 uses:

* Explicit image versions
* Manual upgrade procedures
* Exportable configuration
* Runtime validation after manual upgrade

---

# 3. Approved Review Changes

## 3.1 Correct Compose image syntax

Runtime image names must be single-line YAML strings.

Correct:

```yaml
image: ghcr.io/lazarus-ai-research/sovereign-runtime:cuda-x86_64-${SOVEREIGN_VERSION}
```

Do not use folded scalar syntax for image names.

Incorrect:

```yaml
image: >
  ghcr.io/lazarus-ai-research/sovereign-runtime:
  cuda-x86_64-${SOVEREIGN_VERSION}
```

---

## 3.2 Runtime startup must not crash-loop on configuration errors

The runtime must remain alive when possible so Sovereign Control can diagnose and correct configuration.

Required runtime states:

```text
initializing
downloading
compiling
loading
smoke_testing
healthy
degraded
configuration_error
runtime_error
```

A configuration error must not automatically terminate the container.

The runtime must continue exposing:

```text
GET /health
GET /health/live
GET /runtime/manifest
GET /runtime/errors
```

even when no model is successfully loaded.

---

## 3.3 Separate liveness from readiness

Container liveness must not depend on successful model loading.

```text
GET /health/live
```

returns success when the runtime process and control API are alive.

```text
GET /health/ready
```

returns success only when the required roles are available.

Docker healthchecks should use `/health/live`.

Sovereign Control should use `/health/ready` and `/health` for dashboard state.

This prevents:

* Restart loops during model downloads
* Restart loops during kernel compilation
* Restart loops caused by bad model configuration
* Premature failure during first boot

---

## 3.4 Honest failure-isolation semantics

Sovereign Runtime owns generation. Embeddings run in a separate process with
an independent health and restart boundary.

Sovereign Control reports the two service states independently. For example:

```json
{
  "status": "degraded",
  "runtime": {"reachable": true, "ready": true},
  "embeddings": {"reachable": false, "backend": "embeddinggemma"}
}
```

The following shared-host failures may still affect both services:

* Host failure
* Accelerator reset
* Out-of-memory pressure on the shared device
* CUDA, ROCm, XPU, Gaudi, or Metal fatal error
* Process-level out-of-memory condition
* Communication-library hang
* Shared scheduler corruption

The specification must not promise full fault isolation between roles.

---

## 3.5 Memory allocation is advisory

Role memory controls are best-effort policies rather than guaranteed hard partitions.

Example:

```yaml
resource_policy:
  enforcement: best_effort

  generation:
    memory_weight: 82
    priority: high

  embedding:
    memory_weight: 18
    priority: low
```

Interpretation varies by platform:

* NVIDIA discrete VRAM
* AMD discrete VRAM
* Apple unified memory
* Strix Halo unified memory
* Intel XPU memory architecture
* Intel Gaudi on-package HBM
* DGX Spark unified memory

The runtime must report actual observed memory behavior through metrics.

---

## 3.6 Restricted Docker access is required in MVP

Sovereign Control must not mount the unrestricted Docker socket directly in production.

Instead, the MVP includes a restricted internal component:

```text
Sovereign Docker Proxy
```

The proxy exposes an allowlisted subset of Docker operations required by Sovereign Control.

Allowed operations may include:

* Inspect containers
* List Sovereign containers
* Restart approved Sovereign services
* Pull approved Lazarus images
* Run Sovereign Evals
* Read approved service logs
* Inspect image metadata

Disallowed operations include:

* Arbitrary container creation
* Arbitrary host mounts
* Arbitrary command execution
* Access to unrelated containers
* Privileged container creation
* Arbitrary image deletion
* Host filesystem browsing

The proxy must validate:

* Image namespace
* Container labels
* Compose project
* Allowed command
* Allowed mount path
* Allowed environment keys

Development builds may support direct Docker socket access behind an explicit insecure-development option.

---

# 4. Core Product Components

## 4.1 Sovereign Workspace

Initial implementation:

```text
AnythingLLM
```

Responsibilities:

* Chat interface
* Document upload
* Workspace management
* User-facing model selection
* Retrieval interaction
* Customer branding
* User-facing settings

Sovereign Workspace is managed through a provider interface.

---

## 4.2 Sovereign Gateway

Implementation:

```text
LiteLLM
```

Responsibilities:

* Model routing
* Local/remote provider abstraction
* API keys
* Budgets
* Rate limits
* Usage tracking
* Provider credentials
* Stable model aliases
* Gateway telemetry

The Gateway is internal-only.

---

## 4.3 Sovereign Runtime

Implementation:

```text
Lazarus-custom vLLM
```

Responsibilities:

* Chat completions
* Text completions
* Generation
* Runtime health
* Prometheus metrics
* Model lifecycle
* Accelerator reporting
* Smoke testing
* Benchmark hooks
* Evaluation hooks

Locked topology:

```text
One generation container
One vLLM process
One runtime port
```

Required runtime role:

```text
generation
```

Optional roles:

```text
vision
audio
rerank
```

---

## 4.4 Sovereign Control

Implementation:

```text
Go backend
Embedded web frontend
One service
One container
One binary
```

Responsibilities:

* Installation workflow
* Hardware detection
* Runtime profile selection
* Model management
* Embedding profile management
* Workspace configuration
* Gateway administration
* Branding
* Feature controls
* Runtime diagnostics
* Smoke tests
* Benchmarks
* Evaluations
* Vector index management
* Backup and restore
* Offline bundles
* Support bundles

---

## 4.5 Sovereign Observe

Components:

* Prometheus
* Grafana
* Loki
* OpenTelemetry Collector
* Arize Phoenix

Responsibilities:

* Metrics
* Logs
* Metadata traces
* Evaluation traces
* Benchmark history
* Dashboard visualization
* Runtime and gateway diagnostics

---

## 4.6 Sovereign Data

Default implementation:

```text
PostgreSQL 16 + pgvector
```

Responsibilities:

* Sovereign Control state
* LiteLLM state
* Vector storage
* Evaluation history
* Benchmark history
* Model registry
* Embedding profile registry
* Audit records

---

## 4.7 Sovereign Evals

Responsibilities:

* Runtime API conformance
* Generation benchmarks
* Embedding benchmarks
* Mixed-role benchmarks
* Retrieval evaluations
* Text embedding and retrieval evaluations
* Historical comparison
* JSON and HTML reports

---

## 4.8 Sovereign Docker Proxy

Responsibilities:

* Restricted container inspection
* Approved service restart
* Approved image pull
* Evaluation-job launch
* Approved log retrieval
* Compose-project validation

The proxy is internal-only and has no user interface.

---

# 5. High-Level Architecture

```text
┌──────────────────────────────────────────────────────────┐
│                     Caddy / HTTPS                        │
└───────────────────────────┬──────────────────────────────┘
                            │
        ┌───────────────────┼────────────────────┐
        │                   │                    │
┌───────▼─────────┐ ┌───────▼─────────┐ ┌────────▼────────┐
│ Sovereign       │ │ Sovereign       │ │ Sovereign       │
│ Workspace       │ │ Control         │ │ Observe         │
│ AnythingLLM     │ │ Go service      │ │ Grafana/Phoenix │
└───────┬─────────┘ └───────┬─────────┘ └────────┬────────┘
        │                   │                    │
        │          ┌────────▼────────┐   ┌────────▼────────┐
        │          │ Restricted      │   │ Prometheus/Loki │
        │          │ Docker Proxy    │   └─────────────────┘
        │          └─────────────────┘
        │
┌───────▼─────────┐
│ Sovereign       │
│ Gateway         │
│ LiteLLM         │
└───────┬─────────┘
        │
┌───────▼──────────────────────────────────────────────────┐
│ Sovereign Runtime                                       │
│ One Lazarus vLLM process                                │
│                                                        │
│ /v1/chat/completions → generation role                 │
│ /v1/completions      → generation role                 │
└───────┬──────────────────────────────────────────────────┘
        │
┌───────▼────────────────────────────────────────────────────┐
│ CUDA / ROCm / XPU / Gaudi / Metal / DGX Spark / Strix Halo │
└────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────┐
│ embeddinggemma :42666                                      │
│ /v1/embeddings → EmbeddingGemma text vectors               │
└────────────────────────────────────────────────────────────┘

Persistent services:
  PostgreSQL + pgvector
  AnythingLLM data
  Phoenix data
  Evaluation and benchmark reports
```

---

# 6. Repositories

Canonical GitHub organization:

```text
github.com/Lazarus-AI-Research
```

## 6.1 `sovereign-stack`

```text
sovereign-stack/
  README.md
  LICENSE
  VERSION
  CHANGELOG.md
  .env.example

  compose/
    compose.yml
    compose.runtime.cuda.yml
    compose.runtime.dgx-spark.yml
    compose.runtime.rocm.yml
    compose.runtime.strix-halo.yml
    compose.runtime.xpu.yml
    compose.runtime.gaudi.yml
    compose.runtime.metal.yml
    compose.runtime.cpu.yml

  config/
    branding.yaml
    feature-flags.yaml
    model-registry.yaml
    embedding-profiles.yaml
    runtime.yaml

    caddy/
    litellm/
    postgres/
    prometheus/
    grafana/
    loki/
    otel/
    phoenix/
    docker-proxy/

  schemas/
    runtime-config.schema.json
    runtime-manifest.schema.json
    model-registry.schema.json
    embedding-profile.schema.json
    feature-flags.schema.json
    branding.schema.json
    eval-suite.schema.json
    offline-bundle.schema.json
    support-bundle.schema.json

  api/
    sovereign-control.openapi.yaml

  scripts/
    install.sh
    uninstall.sh
    sovereign
    detect-hardware.sh
    generate-config.sh
    create-offline-bundle.sh

  docs/
    architecture.md
    installation.md
    runtime-contract.md
    control-api.md
    hardware-profiles.md
    model-management.md
    embeddings.md
    offline-deployment.md
    backup-restore.md
    observability.md
    security.md

  tests/
    compose/
    integration/
    install/
```

---

## 6.2 `sovereign-control`

```text
sovereign-control/
  cmd/
    sovereign-control/
      main.go

  internal/
    api/
    auth/
    config/
    dockerproxy/
    hardware/
    runtime/
    models/
    embeddings/
    indexes/
    gateway/
    workspace/
    branding/
    features/
    observability/
    evals/
    backups/
    bundles/
    support/
    jobs/
    database/

  web/
    src/
    public/
    dist/

  migrations/
  schemas/
  openapi/
  tests/
```

Published image:

```text
ghcr.io/lazarus-ai-research/sovereign-control:<version>
```

---

## 6.3 `sovereign-vllm`

```text
sovereign-vllm/
  vllm/
  csrc/
  setup.py
  pyproject.toml

  lazarus/
    api/
      health.py
      errors.py
      manifest.py
      accelerator.py
      selftest.py
      benchmark.py

    models/
      generation/
      vision/
      audio/
      rerank/

    platforms/
      cuda/
      rocm/
      xpu/
      gaudi/
      metal/
      dgx_spark/
      strix_halo/

    kernels/
      cuda/
      rocm/
      xpu/
      gaudi/
      metal/

    appliance/
      launcher.py
      config.py
      logging.py
      manifest.py
      diagnostics.py
      state_machine.py

  docker/
    common/
    cuda/
    dgx-spark/
    rocm/
    strix-halo/
    xpu/
    gaudi/
    metal/
    cpu/

  tests/
    conformance/
    multi_role/
    generation/
    embeddings/
    multimodal/
    correctness/
    performance/
    platform/
```

---

## 6.4 `sovereign-evals`

```text
sovereign-evals/
  sovereign_evals/
    smoke/
    conformance/
    generation/
    embeddings/
    multimodal/
    retrieval/
    mixed_role/
    reports/
    phoenix/

  suites/
    smoke.yaml
    quick.yaml
    full.yaml
    retrieval.yaml
    embedding.yaml
    mixed-role.yaml
```

Published image:

```text
ghcr.io/lazarus-ai-research/sovereign-evals:<version>
```

---

## 6.5 `sovereign-docker-proxy`

```text
sovereign-docker-proxy/
  cmd/
    sovereign-docker-proxy/
      main.go

  internal/
    allowlist/
    containers/
    images/
    logs/
    jobs/
    validation/
    audit/
```

Published image:

```text
ghcr.io/lazarus-ai-research/sovereign-docker-proxy:<version>
```

---

# 7. Docker Images

Canonical image namespace:

```text
ghcr.io/lazarus-ai-research
```

Runtime images:

```text
ghcr.io/lazarus-ai-research/sovereign-runtime:cuda-x86_64-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:cuda-arm64-dgx-spark-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:rocm-x86_64-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:rocm-strix-halo-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:xpu-x86_64-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:gaudi-x86_64-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:metal-arm64-<version>
ghcr.io/lazarus-ai-research/sovereign-runtime:cpu-x86_64-<version>
```

Application images:

```text
ghcr.io/lazarus-ai-research/sovereign-control:<version>
ghcr.io/lazarus-ai-research/sovereign-evals:<version>
ghcr.io/lazarus-ai-research/sovereign-backup:<version>
ghcr.io/lazarus-ai-research/sovereign-docker-proxy:<version>
```

Production deployments must use immutable version tags.

---

# 8. Docker Compose

## 8.1 Core Compose file

```yaml
name: sovereign-stack

services:
  caddy:
    image: caddy:2
    container_name: sovereign-caddy
    restart: unless-stopped
    depends_on:
      - sovereign-control
      - sovereign-workspace
    ports:
      - "${HTTP_PORT:-54854}:80"
      - "${HTTPS_PORT:-443}:443"
    volumes:
      - ./config/caddy/Caddyfile:/etc/caddy/Caddyfile:ro
      - ./branding:/srv/branding:ro
      - caddy_data:/data
      - caddy_config:/config
    networks:
      - sovereign

  sovereign-control:
    image: ${SOVEREIGN_CONTROL_IMAGE}
    container_name: sovereign-control
    restart: unless-stopped
    env_file:
      - .env
    environment:
      SOVEREIGN_ROOT: /sovereign
      DATABASE_URL: ${CONTROL_DATABASE_URL}
      LITELLM_BASE_URL: http://sovereign-gateway:4000
      RUNTIME_BASE_URL: http://sovereign-runtime:8000
      DOCKER_PROXY_BASE_URL: http://sovereign-docker-proxy:8081
      PROMETHEUS_BASE_URL: http://prometheus:9090
      PHOENIX_BASE_URL: http://phoenix:6006
      LOKI_BASE_URL: http://loki:3100
    volumes:
      - ./:/sovereign
    expose:
      - "8080"
    networks:
      - sovereign

  sovereign-docker-proxy:
    image: ${SOVEREIGN_DOCKER_PROXY_IMAGE}
    container_name: sovereign-docker-proxy
    restart: unless-stopped
    environment:
      ALLOWED_PROJECT: sovereign-stack
      ALLOWED_IMAGE_PREFIX: ghcr.io/lazarus-ai-research/
      AUDIT_LOG_PATH: /audit/docker-actions.jsonl
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./logs/docker-proxy:/audit
    expose:
      - "8081"
    networks:
      - sovereign

  sovereign-workspace:
    image: mintplexlabs/anythingllm:${ANYTHINGLLM_VERSION}
    container_name: sovereign-workspace
    restart: unless-stopped
    env_file:
      - .env
    volumes:
      - anythingllm_data:/app/server/storage
      - ./branding:/sovereign-branding:ro
    expose:
      - "3001"
    networks:
      - sovereign

  sovereign-gateway:
    image: ${LITELLM_IMAGE}
    container_name: sovereign-gateway
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      sovereign-runtime:
        condition: service_healthy
    env_file:
      - .env
    command:
      - --config
      - /app/config.yaml
      - --host
      - 0.0.0.0
      - --port
      - "4000"
    volumes:
      - ./config/litellm/config.yaml:/app/config.yaml:ro
    expose:
      - "4000"
    networks:
      - sovereign

  sovereign-runtime:
    image: ${SOVEREIGN_RUNTIME_IMAGE}
    container_name: sovereign-runtime
    restart: unless-stopped
    env_file:
      - .env
    environment:
      SOVEREIGN_RUNTIME_CONFIG: /runtime-config/runtime.yaml
      SOVEREIGN_RUNTIME_MANIFEST: /runtime-state/manifest.json
    volumes:
      - ./config/runtime.yaml:/runtime-config/runtime.yaml:ro
      - ./models:/models
      - runtime_state:/runtime-state
    expose:
      - "8000"
    healthcheck:
      test:
        - CMD
        - /usr/local/bin/sovereign-runtime-healthcheck
        - --live
      interval: 15s
      timeout: 10s
      retries: 20
      start_period: 300s
    networks:
      - sovereign

  postgres:
    image: pgvector/pgvector:pg16
    container_name: sovereign-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: postgres
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./config/postgres/init:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test:
        - CMD-SHELL
        - pg_isready -U ${POSTGRES_USER}
      interval: 10s
      timeout: 5s
      retries: 10
    networks:
      - sovereign

  phoenix:
    image: arizephoenix/phoenix:${PHOENIX_VERSION}
    container_name: sovereign-phoenix
    restart: unless-stopped
    env_file:
      - .env
    expose:
      - "6006"
      - "4317"
    networks:
      - sovereign

  prometheus:
    image: prom/prometheus:${PROMETHEUS_VERSION}
    container_name: sovereign-prometheus
    restart: unless-stopped
    volumes:
      - ./config/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus_data:/prometheus
    expose:
      - "9090"
    networks:
      - sovereign

  grafana:
    image: grafana/grafana:${GRAFANA_VERSION}
    container_name: sovereign-grafana
    restart: unless-stopped
    volumes:
      - grafana_data:/var/lib/grafana
      - ./config/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./config/grafana/dashboards:/var/lib/grafana/dashboards:ro
    expose:
      - "3000"
    networks:
      - sovereign

  loki:
    image: grafana/loki:${LOKI_VERSION}
    container_name: sovereign-loki
    restart: unless-stopped
    command:
      - -config.file=/etc/loki/loki.yml
    volumes:
      - ./config/loki/loki.yml:/etc/loki/loki.yml:ro
      - loki_data:/loki
    expose:
      - "3100"
    networks:
      - sovereign

  otel-collector:
    image: otel/opentelemetry-collector-contrib:${OTEL_VERSION}
    container_name: sovereign-otel
    restart: unless-stopped
    command:
      - --config=/etc/otelcol/config.yml
    volumes:
      - ./config/otel/collector.yml:/etc/otelcol/config.yml:ro
    expose:
      - "4317"
      - "4318"
    networks:
      - sovereign

  sovereign-evals:
    image: ${SOVEREIGN_EVALS_IMAGE}
    container_name: sovereign-evals
    profiles:
      - tools
    restart: "no"
    env_file:
      - .env
    volumes:
      - ./config:/sovereign/config:ro
      - ./reports:/sovereign/reports
    networks:
      - sovereign

volumes:
  postgres_data:
  anythingllm_data:
  runtime_state:
  prometheus_data:
  grafana_data:
  loki_data:
  caddy_data:
  caddy_config:

networks:
  sovereign:
    name: sovereign
```

---

## 8.2 Runtime overlays

CUDA:

```yaml
services:
  sovereign-runtime:
    image: ghcr.io/lazarus-ai-research/sovereign-runtime:cuda-x86_64-${SOVEREIGN_VERSION}
    gpus: all
    ipc: host
    environment:
      SOVEREIGN_PROFILE: cuda-x86_64
      VLLM_BACKEND: cuda
      NVIDIA_VISIBLE_DEVICES: all
```

ROCm:

```yaml
services:
  sovereign-runtime:
    image: ghcr.io/lazarus-ai-research/sovereign-runtime:rocm-x86_64-${SOVEREIGN_VERSION}
    devices:
      - /dev/kfd
      - /dev/dri
    group_add:
      - video
      - render
    security_opt:
      - seccomp=unconfined
    environment:
      SOVEREIGN_PROFILE: rocm-x86_64
      VLLM_BACKEND: rocm
```

Intel XPU:

```yaml
services:
  sovereign-runtime:
    image: ghcr.io/lazarus-ai-research/sovereign-runtime:xpu-x86_64-${SOVEREIGN_VERSION}
    devices:
      - /dev/dri
    group_add:
      - video
      - render
    environment:
      SOVEREIGN_PROFILE: xpu-x86_64
      VLLM_BACKEND: xpu
      ZE_ENABLE_PCI_ID_DEVICE_ORDER: "1"
```

Intel Gaudi 2 / Gaudi 3:

```yaml
services:
  sovereign-runtime:
    image: ghcr.io/lazarus-ai-research/sovereign-runtime:gaudi-x86_64-${SOVEREIGN_VERSION}
    runtime: habana
    ipc: host
    cap_add:
      - SYS_NICE
    environment:
      SOVEREIGN_PROFILE: gaudi-x86_64
      VLLM_BACKEND: hpu
      HABANA_VISIBLE_DEVICES: all
      OMPI_MCA_btl_vader_single_copy_mechanism: none
      ONEAPI_DEVICE_SELECTOR: ${ONEAPI_DEVICE_SELECTOR:-level_zero:gpu}
```

Apple Metal:

```yaml
services:
  sovereign-runtime:
    image: ghcr.io/lazarus-ai-research/sovereign-runtime:metal-arm64-${SOVEREIGN_VERSION}
    environment:
      SOVEREIGN_PROFILE: metal-arm64
      VLLM_BACKEND: metal
```

---

# 9. Isolated Inference Services

## 9.1 Core requirement

Sovereign Runtime loads and serves generation models inside one vLLM process.
`embeddinggemma` serves the fixed embedding model in a separate process.

The process exposes one listener:

```text
0.0.0.0:8000
```

Required endpoints:

```text
POST /v1/chat/completions
POST /v1/completions
GET  /v1/models
```

Port 8000 stays private to the Compose network. The embedding service listens
on private port 42666 on CUDA and host loopback port 42666 on Metal. Caddy does
not publish either service.

---

## 9.2 Runtime roles

Required in Sovereign Runtime:

```text
generation
```

Optional:

```text
vision
audio
rerank
```

The runtime embedding role is disabled in shipped profiles.

---

## 9.3 Routing

```text
/v1/chat/completions
  → generation-capable model

/v1/completions
  → generation-capable model

/v1/embeddings
  → LiteLLM → embeddinggemma

/v1/models
  → LiteLLM aggregates generation and embedding aliases
```

The request `model` field determines the specific configured model alias.

---

## 9.4 Independent scheduling and failure boundaries

Generation and embedding requests may run concurrently, but each service owns
its queue, batching, caches, health, and restart policy. Failure of the
embedding process must not reload the generation model.

---

# 10. Embedding Service

## 10.1 Certified model

The certified Sovereign Stack text embedding model is:

```text
ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
```

Product alias:

```text
embedding-gemma-default
```

Supported modality: text.

The model must be pinned to an immutable revision in production.

Example:

```yaml
embedding_profiles:
  gemma-default:
    provider: embeddinggemma
    model: ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
    revision: 8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73
    served_model_name: embedding-gemma-default
    pooling: mean
    normalization: l2
    query_prefix: "task: search result | query: "
    document_prefix: "title: none | text: "
    modalities: [text]
```

The output dimension must be discovered and validated from the loaded checkpoint.

It must not be inferred from the model name or hard-coded without validation.

---

## 10.2 API compatibility

`embeddinggemma` retains its native `POST /api/embed` contract and adds
`POST /v1/embeddings` for LiteLLM and OpenAI clients. The OpenAI route supports
768/512/256/128 dimensions and float/base64 encoding without changing the
native response envelope.

---

## 10.3 Platform conformance

`embedding-gemma-default` is enabled on a platform only after it passes:

* Model load
* Text embedding
* Output normalization
* Dimension validation
* Batch embedding
* Concurrent generation/embedding workload
* pgvector insert and retrieval
* Restart and reload
* Metrics validation

CUDA and Metal use the same GGUF and embedding identity.

---

# 11. Embedding Profile and Index Lifecycle

## 11.1 Every vector collection records its embedding identity

Each vector collection must record:

* Embedding profile ID
* Model repository
* Model revision
* Output dimension
* Normalization
* Distance metric
* Chunking strategy
* Preprocessing version
* Creation date

Example:

```json
{
  "profile_id": "gemma-default",
  "model": "ggml-org/embeddinggemma-300M-qat-q4_0-GGUF",
  "revision": "8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73",
  "dimensions": 768,
  "normalization": "l2",
  "distance": "cosine",
  "preprocessing_version": "sovereign-embeddinggemma-v1"
}
```

The dimension shown is illustrative and must be populated from runtime validation.

---

## 11.2 Changing embedding models requires rebuilding indexes

Sovereign Control must never silently reuse vectors created by another embedding profile.

When an administrator changes an embedding model, the UI must display:

```text
Changing the embedding model requires rebuilding affected indexes.
Existing vectors will remain available until the new index is complete.
```

Required actions:

```text
Create new embedding index
Rebuild existing index
Track rebuild progress
Validate new index
Switch active index
Delete old index
```

Index switching should be atomic where practical.

---

## 11.3 Index versioning

Recommended table:

```sql
CREATE TABLE vectors.index_versions (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    profile_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    model_revision TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    normalization TEXT NOT NULL,
    distance_metric TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ
);
```

Vector rows must reference an index version.

---

# 12. Runtime Configuration

```yaml
schema_version: "1.1"

runtime:
  listen_address: 0.0.0.0
  port: 8000
  api_key_env: SOVEREIGN_RUNTIME_API_KEY
  profile: cuda-x86_64

startup:
  smoke_test_on_start: true
  remain_alive_on_configuration_error: true
  fail_process_on_generation_error: false
  fail_process_on_embedding_error: false

roles:
  generation:
    enabled: true
    task: generate
    source: huggingface
    model: google/gemma-4-E2B-it
    revision: "<immutable-revision>"
    served_model_name: assistant-large
    max_model_len: 32768
    priority: high
    memory_weight: 82
    max_concurrent_requests: 8

  embedding:
    enabled: false
    task: embed

observability:
  prometheus: true
  structured_logs: true
  otlp_endpoint: http://otel-collector:4317

privacy:
  prompt_logging: false
  response_logging: false
  full_trace: false
```

---

# 13. Health and Runtime States

## 13.1 Liveness

```http
GET /health/live
```

Example:

```json
{
  "status": "alive",
  "state": "loading"
}
```

---

## 13.2 Readiness

```http
GET /health/ready
```

Example:

```json
{
  "ready": false,
  "state": "configuration_error",
  "required_roles": {
    "generation": false,
    "embedding": false
  }
}
```

---

## 13.3 Aggregate health

```http
GET /health
```

Example healthy response:

```json
{
  "status": "healthy",
  "state": "healthy",
  "runtime_id": "sovereign-runtime-cuda-x86_64-2026.07.0",
  "roles": {
    "generation": {
      "status": "healthy",
      "model_loaded": true,
      "served_model_name": "assistant-large"
    },
    "embedding": {
      "status": "disabled",
      "model_loaded": false
    }
  }
}
```

Example degraded response:

```json
{
  "status": "degraded",
  "state": "degraded",
  "roles": {
    "generation": {
      "status": "healthy"
    },
    "embedding": {
      "status": "unhealthy",
      "error_code": "MODEL_LOAD_FAILED"
    }
  }
}
```

---

## 13.4 Runtime errors

```http
GET /runtime/errors
```

Example:

```json
{
  "errors": [
    {
      "code": "MODEL_REVISION_NOT_FOUND",
      "role": "embedding",
      "message": "Configured model revision could not be resolved.",
      "recoverable": true,
      "first_seen": "2026-07-11T12:00:00Z"
    }
  ]
}
```

---

# 14. Runtime Manifest

```json
{
  "schema_version": "1.1",
  "runtime_id": "sovereign-runtime-cuda-x86_64-2026.07.0",
  "runtime_version": "2026.07.0",
  "vllm_version": "0.x.y-lazarus",
  "backend": "cuda",
  "profile": "cuda-x86_64",
  "topology": "single_process_multi_role",
  "state": "healthy",

  "api": {
    "openai_compatible": true,
    "port": 8000,
    "base_path": "/v1"
  },

  "roles": {
    "generation": {
      "enabled": true,
      "status": "healthy",
      "task": "generate",
      "served_model_name": "assistant-large",
      "engine_model": "google/gemma-4-E2B-it",
      "revision": "immutable-revision",
      "context_length": 32768
    },

    "embedding": {
      "enabled": false,
      "status": "disabled",
      "task": "embed"
    }
  },

  "resource_policy": {
    "enforcement": "best_effort",
    "generation_memory_weight": 82,
    "embedding_memory_weight": 18
  },

  "accelerator": {
    "vendor": "nvidia",
    "device_count": 1,
    "unified_memory": false
  },

  "health": {
    "status": "healthy",
    "driver": "ok",
    "kernels": "ok",
    "metrics": "ok"
  }
}
```

The example dimension is illustrative.

---

# 15. LiteLLM Configuration

LiteLLM presents one product API while generation and embeddings use separate
internal base URLs.

```yaml
model_list:
  - model_name: assistant-large
    litellm_params:
      model: openai/assistant-large
      api_base: http://sovereign-runtime:8000/v1
      api_key: os.environ/SOVEREIGN_RUNTIME_API_KEY

  - model_name: embedding-gemma-default
    litellm_params:
      model: openai/embedding-gemma-default
      api_base: os.environ/SOVEREIGN_EMBEDDINGS_BASE_URL
      api_key: not-required

general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY

litellm_settings:
  drop_params: true
  request_timeout: 600
```

Remote and cloud models are added through Sovereign Control.

---

# 16. AnythingLLM Integration

Sovereign Workspace must be configured to use:

```text
LLM provider:
  Sovereign Gateway via OpenAI-compatible API

Embedding provider:
  Sovereign Gateway via LiteLLM/OpenAI-compatible embeddings

Vector database:
  PostgreSQL + pgvector
```

Sovereign Control must validate these capabilities against the selected AnythingLLM release before publishing a Sovereign Stack release.

The release process must contain an integration test proving:

* Chat through LiteLLM works
* Embeddings through LiteLLM work
* AnythingLLM can ingest documents
* AnythingLLM writes vectors to pgvector
* Retrieval works after restart
* Changing embedding profiles requires index rebuild
* Lazarus branding is applied

AnythingLLM must not silently create an unmanaged secondary vector store.

---

# 17. PostgreSQL and pgvector

Logical databases:

```text
sovereign_control
litellm
workspace
phoenix
vectors
```

Recommended schema:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE SCHEMA IF NOT EXISTS vectors;

CREATE TABLE vectors.document_chunks (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    document_id UUID NOT NULL,
    index_version_id UUID NOT NULL,
    chunk_index INTEGER NOT NULL,
    content TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    embedding vector,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The implementation may use separate tables per dimension if required by pgvector schema constraints.

Metadata should support:

* Workspace
* User
* Document
* Collection
* Modality
* Source
* Permissions
* File type
* Creation date
* Retention policy
* Embedding profile
* Model revision

pgvector is the only bundled vector provider in version 0.1.

---

# 18. Sovereign Control API

Base path:

```text
/api/control/v1
```

## 18.1 System

```text
GET /health
GET /status
GET /version
```

## 18.2 Hardware

```text
POST /hardware/detect
GET  /hardware
GET  /profiles
POST /profiles/select
```

## 18.3 Runtime

```text
GET  /runtime/status
GET  /runtime/manifest
GET  /runtime/errors
GET  /runtime/accelerator
POST /runtime/restart
POST /runtime/smoke-test
POST /runtime/benchmark
```

## 18.4 Generation runtime role

```text
GET   /runtime/roles
PATCH /runtime/roles/generation
POST  /runtime/roles/generation/load
POST  /runtime/roles/generation/unload
```

The fixed local embedding service is managed through embedding profiles and
service health, not through runtime-role mutation.

## 18.5 Models

```text
GET    /models
POST   /models/local
POST   /models/remote
PATCH  /models/{model_id}
DELETE /models/{model_id}
POST   /models/{model_id}/load
POST   /models/{model_id}/smoke-test
```

## 18.6 Embedding profiles

```text
GET    /embedding-profiles
POST   /embedding-profiles
GET    /embedding-profiles/{profile_id}
PATCH  /embedding-profiles/{profile_id}
DELETE /embedding-profiles/{profile_id}
POST   /embedding-profiles/{profile_id}/validate
POST   /embedding-profiles/{profile_id}/activate
```

## 18.7 Vector indexes

```text
GET    /indexes
POST   /indexes
GET    /indexes/{index_id}
POST   /indexes/{index_id}/rebuild
POST   /indexes/{index_id}/validate
POST   /indexes/{index_id}/activate
DELETE /indexes/{index_id}
```

Rebuild request:

```json
{
  "embedding_profile": "gemma-default",
  "source_index": "workspace-default-v1",
  "activate_when_complete": true
}
```

## 18.8 Gateway

```text
GET  /gateway/status
GET  /gateway/models
GET  /gateway/usage
GET  /gateway/keys
POST /gateway/keys
GET  /gateway/budgets
PATCH /gateway/budgets/{budget_id}
POST /gateway/config/regenerate
POST /gateway/reload
```

## 18.9 Workspace

```text
GET   /workspace/provider
PATCH /workspace/provider
GET   /workspace/status
POST  /workspace/restart
POST  /workspace/validate
```

## 18.10 Evaluations

```text
POST /evals/smoke
POST /evals/benchmark/quick
POST /evals/benchmark/full
POST /evals/embedding
POST /evals/retrieval
POST /evals/mixed-role
POST /evals/suite
GET  /evals/results
GET  /evals/results/{result_id}
```

## 18.11 Backups

```text
GET  /backups
POST /backups
POST /backups/{backup_id}/verify
POST /backups/{backup_id}/restore
```

## 18.12 Offline bundles

```text
GET  /bundles
POST /bundles
GET  /bundles/{bundle_id}
GET  /bundles/{bundle_id}/download
POST /bundles/install
```

## 18.13 Docker operations

Sovereign Control does not call Docker directly.

Internal calls use Sovereign Docker Proxy:

```text
POST /internal/docker/containers/{service}/restart
POST /internal/docker/images/pull
POST /internal/docker/evals/run
GET  /internal/docker/containers/{service}/logs
GET  /internal/docker/status
```

These endpoints are not externally exposed.

---

# 19. Sovereign Evals

## 19.1 Smoke suite

Required checks:

* Runtime liveness
* Runtime readiness
* Generation role loaded
* Embedding service healthy
* Chat completion
* Streaming completion
* Text embedding
* LiteLLM generation route
* LiteLLM embedding route
* pgvector insertion
* pgvector retrieval
* Runtime manifest validation
* Metrics availability

---

## 19.2 Quick benchmark

* Short prefill
* Short decode
* Text embedding throughput
* p50 latency
* p95 latency
* Basic concurrency
* Concurrent generation and embedding requests

---

## 19.3 Full benchmark

* Multiple prompt sizes
* Multiple output lengths
* Multiple concurrency levels
* Long context
* Embedding batch scaling
* Mixed generation/embedding pressure
* Memory behavior
* Scheduler fairness
* Error rate

---

## 19.4 Embedding evaluation

Required metrics:

* Text-text retrieval
* Query-document retrieval
* Batch throughput
* Embedding consistency
* Normalization validation
* Dimension validation

---

## 19.5 Mixed-role benchmark

Example:

```yaml
generation:
  concurrent_users: 4
  requests_per_user: 10

embedding:
  text_batches: 100
  batch_size: 16

duration_seconds: 300
```

Report:

* Generation p50/p95
* Generation token throughput
* Embedding item throughput
* Queue depth per role
* Scheduler wait per role
* Accelerator memory
* System memory
* Error rate
* Role starvation events

---

# 20. Startup Smoke Test

A smoke test runs after successful generation and embedding service startup.

The smoke test is not the Docker liveness check.

Required checks:

```text
Runtime process alive
Runtime manifest valid
Generation role reachable
Embedding service reachable
Chat completion succeeds
Text embedding succeeds
LiteLLM routes succeed
pgvector reachable
Metrics reachable
```

Dashboard states:

```text
Initializing
Downloading
Compiling
Loading
Smoke testing
Healthy
Degraded
Configuration error
Runtime error
```

---

# 21. Observability

Required runtime metric labels:

```text
runtime_profile
backend
role
served_model
engine_model
model_revision
modality
```

Generation metrics:

* Requests
* Errors
* Queue depth
* Prompt tokens
* Generated tokens
* Prefill throughput
* Decode throughput
* Time to first token
* Total latency

Embedding metrics:

* Requests
* Errors
* Queue depth
* Input tokens
* Input items
* Batch size
* Modality
* Embeddings per second
* Total latency

Shared metrics:

* Accelerator memory
* System memory
* Best-effort role allocation
* Scheduler wait time
* Process state
* Model load state
* Configuration-error count
* Smoke-test result

Phoenix remains metadata-only by default.

---

# 22. Security

Only Caddy is externally exposed by default.

```text
80
443
```

Internal-only:

* Sovereign Gateway
* Sovereign Runtime
* Sovereign Embeddings
* PostgreSQL
* Prometheus
* Loki
* Phoenix
* OpenTelemetry Collector
* Sovereign Docker Proxy

Sovereign Docker Proxy must:

* Authenticate Sovereign Control
* Validate Compose project labels
* Restrict image namespaces
* Restrict service names
* Restrict mounts
* Audit all actions
* Reject arbitrary commands
* Reject arbitrary containers

Secrets must not appear in:

* Logs
* Traces
* Grafana labels
* Eval reports
* Support bundles
* Backup manifests
* Runtime manifests

---

# 23. Backup and Offline Bundles

Normal backups include:

* PostgreSQL
* pgvector data
* Workspace state
* Sovereign Control state
* Gateway configuration
* Model metadata
* Embedding profiles
* Index-version metadata
* Branding
* Feature flags
* Evaluations
* Benchmark history

Normal backups exclude:

* Model weights
* Model caches
* Runtime compilation caches

Offline bundles may include:

* Compose files
* Configurations
* Docker images
* Sovereign Runtime image
* Sovereign Control image
* Sovereign Evals image
* Third-party service images
* Generation model weights
* Embedding model weights
* Checksums
* SBOMs
* Signatures

Example:

```bash
sovereign bundle create \
  --profile cuda-x86_64 \
  --include-model assistant-large \
  --include-model embedding-gemma-default \
  --output sovereign-office-cuda.tar
```

---

# 24. Runtime Image Contract

Every generation runtime image contains:

```text
/usr/local/bin/run-sovereign-runtime
/usr/local/bin/sovereign-runtime-healthcheck
```

Required runtime behavior:

```text
1. Read configuration.
2. Start the runtime control API.
3. Enter initializing state.
4. Detect accelerator.
5. Resolve model revisions.
6. Download or verify models.
7. Compile platform components where required.
8. Load generation role.
9. Start one vLLM process on port 8000.
10. Generate runtime manifest.
11. Run the generation-runtime smoke test.
12. Enter healthy, degraded, or configuration_error state.
13. Export structured logs and Prometheus metrics.
```

The runtime control API must remain available during recoverable configuration
errors. Stack orchestration starts and verifies the independent embedding
service before the product smoke suite runs.

---

# 25. Testing Requirements

Every runtime image must pass:

## API conformance

* `/v1/models`
* Chat completions
* Text completions
* Streaming
* Authentication
* Invalid request handling

## Cross-service correctness

* Generation and embedding healthy together
* Independent process and restart boundaries
* Separate private ports
* Concurrent generation and embedding
* Gateway routing and aggregated model listing
* Honest degraded-state reporting

## Embedding correctness

* Text embeddings
* Dimension validation
* Normalization validation
* Batch behavior
* Determinism tolerance
* pgvector compatibility

## Lifecycle

* Downloading state
* Loading state
* Configuration-error state
* Runtime remains alive after bad model config
* Configuration correction without crash loop
* Smoke test after reload

## Failure behavior

* Generation load failure
* Embedding service failure
* Model revision failure
* Accelerator unavailable
* OOM
* Process-fatal error
* Invalid runtime config
* Role starvation

## Security

* Docker proxy rejects arbitrary image
* Docker proxy rejects arbitrary service
* Docker proxy rejects arbitrary command
* No secrets in support bundle
* No prompt contents in default traces

---

# 26. MVP Scope

Required services:

* Sovereign Control
* Sovereign Docker Proxy
* AnythingLLM
* LiteLLM
* Sovereign Runtime
* Sovereign Embeddings
* PostgreSQL with pgvector
* Phoenix
* Prometheus
* Grafana
* Loki
* OpenTelemetry Collector
* Sovereign Evals
* Caddy

Required runtime profiles:

* CUDA x86_64
* Metal ARM64

Required runtime capabilities:

* One vLLM process
* One runtime port
* Generation role
* Chat completions
* Text completions
* Runtime manifest
* Runtime state machine
* Liveness and readiness separation
* Startup smoke test
* Runtime metrics

Required embedding capability:

```text
embeddinggemma.c v0.3.1 with ggml-org/embeddinggemma-300M-qat-q4_0-GGUF
```

The embedding service must pass direct, gateway, retrieval, and restart
isolation conformance on each certified profile.

Required product capabilities:

* AnythingLLM workspace
* Hidden LiteLLM UI
* Local models
* Hugging Face models
* ModelScope models
* Remote/cloud models
* pgvector retrieval
* Embedding profile management
* Index rebuild workflow
* One-click benchmark
* One-click evaluation
* Phoenix metadata tracing
* Prompt logging off
* Backup without weights
* Offline bundle with optional weights
* Lazarus branding
* Restricted Docker access

---

# 27. Post-MVP Scope

* ROCm x86_64
* Strix Halo
* Intel XPU
* DGX Spark
* LibreChat workspace provider
* Video retrieval workflows
* Dedicated reranking role
* External vector providers
* Enterprise authentication
* Runtime update workflow
* Signed update channels
* More granular host-agent security

---

# 28. Final Architectural Rules

```text
1. Sovereign Runtime uses one vLLM process for generation.

2. Sovereign Runtime exposes one port and one OpenAI-compatible base URL.

3. embeddinggemma runs as an independent service: a sibling container on CUDA
   and a loopback-only launchd process on Metal.

4. ggml-org/embeddinggemma-300M-qat-q4_0-GGUF is the certified embedding model.

5. The native /api/embed contract remains available; /v1/embeddings is additive.

6. Embedding-model changes require versioned index rebuilds.

7. Sovereign Gateway routes generation and embeddings to their owning services.

8. AnythingLLM never talks directly to Sovereign Runtime.

9. LiteLLM UI is never exposed.

10. Sovereign Control owns all customer-facing administration.

11. PostgreSQL with pgvector is the bundled vector database.

12. Prompt logging is off by default.

13. Phoenix is metadata-only by default.

14. Backups exclude model weights.

15. Offline bundles may include model weights.

16. Runtime liveness is independent from model readiness.

17. Recoverable configuration errors must not cause crash loops.

18. Generation and embedding have independent process-level fault isolation.

19. Memory allocation policies are best-effort and platform-dependent.

20. Sovereign Control does not receive unrestricted Docker socket access.

21. Every runtime image must pass generation conformance; every embedding
    deployment must pass embedding service conformance.

22. Every embedding profile must pass text retrieval and pgvector conformance.

23. Platform-specific complexity remains inside the owning generation or
    embedding service.

24. Docker Compose is the appliance deployment unit.

25. GitHub Container Registry (GHCR) is the image distribution channel.

26. GitHub is the source and release metadata channel.

27. Sovereign Stack must appear as one coherent local AI product.
```
