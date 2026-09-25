FROM node:22-bookworm AS ui
WORKDIR /src/console
COPY console/package.json console/package-lock.json ./
RUN npm ci
COPY console/ ./
RUN npm run build

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/operator ./cmd/operator

FROM debian:bookworm-slim
RUN useradd --system --uid 10001 --no-user-group --gid nogroup --home /data --shell /usr/sbin/nologin operator \
    && mkdir -p /data \
    && chown 10001:nogroup /data
COPY --from=build /out/operator /usr/local/bin/operator
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
