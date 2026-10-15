# syntax=docker/dockerfile:1.20@sha256:26147acbda4f14c5add9946e2fd2ed543fc402884fd75146bd342a7f6271dc1d
#
# One Dockerfile for every service: --build-arg SERVICE=redirect|api|worker|nightly|shortenerctl.
# The build stage runs on the build host's platform and cross-compiles for the
# target (no emulation), so linux/arm64 images build quickly on amd64 hosts.
# Build args carry no secrets: SERVICE, VERSION (scripts/version.sh, e.g.
# v0.1.0+3b2d556f3a67) and REVISION (the 12-character git SHA).

FROM --platform=$BUILDPLATFORM golang:1.27.2-trixie@sha256:47667934874913a449b96faa7ef4777fa285d9ec29126ee80ed61c45b7cccf0e AS build
ARG TARGETOS
ARG TARGETARCH
ARG SERVICE
ARG VERSION=dev
WORKDIR /src
ENV GOTOOLCHAIN=local GOFLAGS=-mod=readonly
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    test -n "$SERVICE" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -buildid= -X github.com/perspectivism/url-shortener-ecs-fargate/internal/version.value=${VERSION}" \
      -o /out/app ./cmd/${SERVICE}

# Distroless static: no shell, no package manager, CA certificates and tzdata
# only. The :nonroot variant runs as UID/GID 65532.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG SERVICE
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="shortener-${SERVICE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.source="https://github.com/perspectivism/url-shortener-ecs-fargate"
COPY --from=build /out/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]
