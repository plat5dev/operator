FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /out/operator ./cmd/operator && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/operator-audit ./cmd/operator-audit

FROM debian:bookworm-slim AS base

ARG VERSION=dev
ARG REVISION=
ARG SOURCE=https://github.com/plat5dev/operator

LABEL org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# The staff audit log store: ghcr.io/plat5dev/operator-audit. Contract: docs/audit.md.
FROM base AS operator-audit

LABEL org.opencontainers.image.title="operator-audit" \
      org.opencontainers.image.description="Plat5 operator staff audit log"

COPY --from=builder /out/operator-audit /usr/local/bin/operator-audit

ENV ADDR=:5005
ENV INTERNAL_PORT=8005
EXPOSE 5005 8005
USER 10001:65534
ENTRYPOINT ["/usr/local/bin/operator-audit"]
CMD ["serve"]

# The gateway: ghcr.io/plat5dev/operator. Last, so it is the default target.
FROM base AS prod

LABEL org.opencontainers.image.title="operator" \
      org.opencontainers.image.description="Plat5 headless staff gateway"

COPY --from=builder /out/operator /usr/local/bin/operator
COPY routes.yml /routes.yml

ENV ADDR=:5004
ENV INTERNAL_PORT=8004
ENV ROUTES_FILE=/routes.yml
EXPOSE 5004 8004
USER 10001:65534
ENTRYPOINT ["/usr/local/bin/operator"]
