# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f control/Dockerfile .
# Schemas are copied from the repo-root single source of truth.

FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS web
WORKDIR /src/web
COPY control/web/package.json control/web/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY control/web/ ./
# vite outDir ../internal/web/dist → /src/internal/web/dist
RUN npm run build

FROM golang:1.25@sha256:9669c2ed28435af1b8989acb9fd5235346a912016cc8b152b694a9a91dced0a6 AS build
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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
COPY --from=build /out/sovereign-control /usr/local/bin/sovereign-control
COPY --from=build /src/schemas /usr/share/sovereign/schemas
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sovereign-control"]
