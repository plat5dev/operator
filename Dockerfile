FROM node:22-bookworm AS ui
WORKDIR /src/console
COPY console/package.json console/package-lock.json ./
RUN npm ci
COPY console/ ./
RUN npm run build

FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /out/operator ./cmd/operator

FROM debian:bookworm-slim AS prod

ARG VERSION=dev
ARG REVISION=
ARG SOURCE=https://github.com/plat5dev/operator

LABEL org.opencontainers.image.title="operator" \
      org.opencontainers.image.description="Plat5 operator gateway and console" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-user-group --gid nogroup --home /data --shell /usr/sbin/nologin operator \
    && mkdir -p /data \
    && chown 10001:nogroup /data

COPY --from=builder /out/operator /usr/local/bin/operator
COPY --from=ui /src/console/dist /console
COPY routes.yml /routes.yml
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh

ENV ADDR=:5004
ENV DB_PATH=/data/operator.db
ENV ROUTES_FILE=/routes.yml
ENV CONSOLE_ASSETS=/console
EXPOSE 5004
ENTRYPOINT ["/entrypoint.sh"]
