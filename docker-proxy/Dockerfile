# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f docker-proxy/Dockerfile .

FROM golang:1.25@sha256:9669c2ed28435af1b8989acb9fd5235346a912016cc8b152b694a9a91dced0a6 AS build
WORKDIR /src
COPY go.work go.work
COPY control/ control/
COPY docker-proxy/ docker-proxy/
ARG VERSION=dev
RUN cd docker-proxy && CGO_ENABLED=0 go build \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/sovereign-docker-proxy ./cmd/sovereign-docker-proxy

# Root variant: the proxy's entire purpose is mediated access to the Docker
# socket, which is root-owned inside the VM. The allowlist + audit layer is
# the security boundary; the image stays distroless.
FROM gcr.io/distroless/static-debian12@sha256:61b7ccecebc7c474a531717de80a94709d20547cdcdaf740c25876f2a8e38b44
COPY --from=build /out/sovereign-docker-proxy /usr/local/bin/sovereign-docker-proxy
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/sovereign-docker-proxy"]
