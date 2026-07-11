# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f control/Dockerfile .
# Schemas are copied from the repo-root single source of truth.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY control/web/package.json control/web/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY control/web/ ./
# vite outDir ../internal/web/dist → /src/internal/web/dist
RUN npm run build

FROM golang:1.25 AS build
WORKDIR /src
COPY go.work go.work
COPY control/ control/
COPY docker-proxy/ docker-proxy/
COPY schemas/ schemas/
COPY --from=web /src/internal/web/dist control/internal/web/dist
ARG VERSION=dev
RUN cd control && CGO_ENABLED=0 go build \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/sovereign-control ./cmd/sovereign-control

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sovereign-control /usr/local/bin/sovereign-control
COPY --from=build /src/schemas /usr/share/sovereign/schemas
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sovereign-control"]
