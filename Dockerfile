# syntax=docker/dockerfile:1
# Multi-stage build: web app (Node) -> static Go binary -> distroless (default)
# or Debian with the Claude Code and Codex CLIs (target "cli").
# The web and Go stages run on the build machine's platform and cross-compile
# (CGO is off), so multi-arch builds do not emulate the compiler.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS go
ARG VERSION=dev TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/app ./web/dist/app
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/tempobrief ./cmd/tempobrief \
 && mkdir -p /out/data

# Subscription CLIs. Log in once with `docker exec -it <container> claude setup-token`
# or set CLAUDE_CODE_OAUTH_TOKEN; for Codex, mount a logged-in CODEX_HOME.
FROM node:24-bookworm-slim AS cli
RUN npm install -g @anthropic-ai/claude-code @openai/codex && npm cache clean --force \
 && useradd --uid 65532 --create-home --home-dir /home/nonroot nonroot
COPY --from=go /out/tempobrief /usr/local/bin/tempobrief
COPY --from=go --chown=65532:65532 /out/data /data
USER 65532
ENV GOMEMLIMIT=1GiB TEMPOBRIEF_DATA=/data HOME=/home/nonroot
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/usr/local/bin/tempobrief", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/tempobrief"]

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/tempobrief /usr/local/bin/tempobrief
# Named volumes inherit this ownership, so the nonroot user can write /data.
COPY --from=go --chown=65532:65532 /out/data /data
# Embedding peaks at about 850 MB for a few seconds per batch; the limit sits
# above that so the collector does not thrash, and idle memory stays small.
ENV GOMEMLIMIT=1GiB \
    TEMPOBRIEF_DATA=/data
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/usr/local/bin/tempobrief", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/tempobrief"]
