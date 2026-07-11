# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f evals/Dockerfile .

FROM python:3.12-slim
WORKDIR /app
COPY evals/pyproject.toml ./
COPY evals/sovereign_evals ./sovereign_evals
COPY evals/suites ./suites
RUN pip install --no-cache-dir .
ENTRYPOINT ["sovereign-evals"]
