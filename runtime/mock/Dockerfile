# syntax=docker/dockerfile:1
# Contract-faithful mock Sovereign Runtime for GPU-less dev and CI.
# Build context: runtime/mock — docker build -t sovereign-runtime-mock runtime/mock

FROM python:3.12-slim
RUN pip install --no-cache-dir fastapi uvicorn pyyaml prometheus-client
COPY app.py healthcheck.py /opt/mock/

# §24 image contract: both binaries at their canonical paths.
RUN printf '#!/bin/sh\nexec python /opt/mock/app.py "$@"\n' > /usr/local/bin/run-sovereign-runtime && \
    printf '#!/bin/sh\nexec python /opt/mock/healthcheck.py "$@"\n' > /usr/local/bin/sovereign-runtime-healthcheck && \
    chmod +x /usr/local/bin/run-sovereign-runtime /usr/local/bin/sovereign-runtime-healthcheck

EXPOSE 8000
ENTRYPOINT ["run-sovereign-runtime"]
