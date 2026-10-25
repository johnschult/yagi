# syntax=docker/dockerfile:1

# 1. Build the web UI, which the Go binary embeds.
FROM --platform=$BUILDPLATFORM node:25-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# The release notes the page shows after an update are bundled from CHANGELOG.md (web/src/hooks/use-whats-new.ts).
COPY CHANGELOG.md /src/CHANGELOG.md
RUN mkdir -p /src/internal/ui/dist && npm run build

# 2. Build the server, with the UI inside it. It is cross-compiled on the build machine's
#    own architecture, so building an arm64 image doesn't mean emulating a compiler.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG VERSION=dev
# TAGS=tsnet builds the image with the built-in Tailscale node (the -tailscale image); empty is the plain one.
ARG TAGS=
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/ui/dist ./internal/ui/dist
# The Tailscale build also says which version it is. Without this the library reports "-ERR-BuildInfo" in the
# Tailscale admin console (there is no .git here to stamp it from), so it is given the library's own version, and
# this release after it, the way Tailscale's build script stamps its own.
RUN set -eu; \
    ldflags="-s -w -X github.com/johnschult/yagi/internal/version.Version=${VERSION}"; \
    case ",${TAGS}," in *,tsnet,*) \
        ts=$(go list -m -f '{{.Version}}' tailscale.com); ts=${ts#v}; \
        ldflags="$ldflags -X tailscale.com/version.shortStamp=${ts} -X tailscale.com/version.longStamp=${ts}-yagi${VERSION}";; \
    esac; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags "${TAGS}" -ldflags="$ldflags" -o /out/yagi ./cmd/yagi

# 3. A small image to run it.
FROM alpine:3
LABEL org.opencontainers.image.source="https://github.com/johnschult/yagi" \
      org.opencontainers.image.description="Share an HDHomeRun tuner with Plex, Jellyfin or Emby servers on other networks" \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/yagi /usr/local/bin/yagi
# Run from /config, so the default settings file (yagi.json) lands in the volume
# you mount there.
WORKDIR /config
EXPOSE 9244 5004
ENTRYPOINT ["/usr/local/bin/yagi"]
