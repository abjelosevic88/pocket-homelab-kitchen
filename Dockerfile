# syntax=docker/dockerfile:1.7
# ---------- 1. build the web UI ----------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- 2. build the Go binary ----------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/pocket-kitchen ./cmd/pocket-kitchen

# ---------- 3. runtime ----------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl \
 && addgroup -g 1000 app && adduser -u 1000 -G app -D -h /data app
COPY --from=build /out/pocket-kitchen /usr/local/bin/pocket-kitchen
ENV DATA_DIR=/data PORT=8080 HOST=0.0.0.0
VOLUME ["/data"]
EXPOSE 8080
USER app
WORKDIR /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -fsS http://localhost:8080/healthz || exit 1
LABEL org.opencontainers.image.title="Pocket Kitchen" \
      org.opencontainers.image.description="Self-hosted recipes, meal plan, shopping lists and pantry — the pantry lives in Pocket Inventory, groceries go to Pocket Split, cooking marks a Pocket Habits habit." \
      org.opencontainers.image.source="https://github.com/abjelosevic88/pocket-homelab-kitchen" \
      org.opencontainers.image.licenses="MIT"
ENTRYPOINT ["pocket-kitchen"]
