# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f control/Dockerfile .
# Schemas are copied from the repo-root single source of truth.

# TODO: add a node build stage for control/web once the frontend exists,
# and embed its dist/ output in the binary.

FROM golang:1.25 AS build
WORKDIR /src
COPY go.work go.work
COPY control/ control/
COPY docker-proxy/ docker-proxy/
COPY schemas/ schemas/
ARG VERSION=dev
RUN cd control && CGO_ENABLED=0 go build \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/sovereign-control ./cmd/sovereign-control

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sovereign-control /usr/local/bin/sovereign-control
COPY --from=build /src/schemas /usr/share/sovereign/schemas
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sovereign-control"]
