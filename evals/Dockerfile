# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f evals/Dockerfile .

FROM python:3.12-slim@sha256:c3d81d25b3154142b0b42eb1e61300024426268edeb5b5a26dd7ddf64d9daf28
WORKDIR /app
COPY evals/pyproject.toml ./
COPY evals/sovereign_evals ./sovereign_evals
COPY evals/suites ./suites
COPY schemas ./schemas
ENV SOVEREIGN_SCHEMAS_DIR=/app/schemas \
    SOVEREIGN_SUITES_DIR=/app/suites
RUN pip install --no-cache-dir .
ENTRYPOINT ["sovereign-evals"]
