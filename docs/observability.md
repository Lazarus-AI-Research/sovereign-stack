# Observability

SovereignStack ships a local Prometheus, Grafana, Loki, OpenTelemetry
Collector, and Phoenix. None is host-published. Sovereign Control exports
dependency-health gauges; the runtime exports state, role, latency, request,
token, memory, and error metrics. Evaluation JSON/HTML reports provide the
release-oriented view of correctness and performance.

Tracing defaults to metadata only. Prompt text, retrieved content, model
responses, and provider secrets are not recorded. Phoenix uses the backed-up
PostgreSQL database rather than container-local SQLite. Loki uses filesystem
storage with a seven-day retention policy; Prometheus, Grafana, and Loki state
live in named volumes.

Useful operator views are available through Control's **Overview** and
**Evaluations** tabs. Container logs remain available with:

```bash
sovereign logs --tail 200 sovereign-runtime
sovereign logs -f sovereign-control
```

Before exporting logs or reports, review them for customer document names and
other environment metadata even though content capture is disabled.
