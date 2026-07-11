# syntax=docker/dockerfile:1
# Build context: repository root — docker build -f docker-proxy/Dockerfile .

FROM golang:1.25 AS build
WORKDIR /src
COPY go.work go.work
COPY control/ control/
COPY docker-proxy/ docker-proxy/
ARG VERSION=dev
RUN cd docker-proxy && CGO_ENABLED=0 go build \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/sovereign-docker-proxy ./cmd/sovereign-docker-proxy

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sovereign-docker-proxy /usr/local/bin/sovereign-docker-proxy
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/sovereign-docker-proxy"]
