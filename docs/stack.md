# Stack

Decided 2026-11-25.

| Part | Choice | Why |
|---|---|---|
| Server | Go, `CGO_ENABLED=0` | Static binary, small idle memory, cheap cross-compiles |
| HTTP | `net/http` with method patterns | No framework needed |
| Database | SQLite via `modernc.org/sqlite`, WAL | One file, no CGO |
| Embeddings | hugot, pure Go backend | Local multilingual embeddings without CGO or a sidecar |
| Web app | Preact 11 + signals + preact-iso, TypeScript (strict), Vite, Tailwind v4, Biome, Vitest; embedded with `go:embed` | Small and fast. 22 KB of JS gzipped |
| API types | tygo from `internal/api` and `internal/settings` | One source of truth |
| Container | distroless static (default), Debian slim with the CLIs (`-cli`) | |

## Dependency allowlist

Go, each with its reason:

- `modernc.org/sqlite`: database.
- `golang.org/x/crypto`: argon2id passwords.
- `github.com/knights-analytics/hugot`: embeddings. Pulls in GoMLX, which is
  most of the binary's 54 MB.
- `github.com/mmcdole/gofeed`: RSS, Atom and JSON Feed parsing with real-world
  quirks handled.
- `github.com/anthropics/anthropic-sdk-go/v2`: the Claude API.
- `github.com/bwmarrin/discordgo`: the Discord gateway for vote buttons.
- Tools only (`go tool`): `honnef.co/go/tools` (staticcheck), `github.com/gzuidhof/tygo`.

Not allowed: web frameworks, ORMs, logging or config libraries, an OpenAI SDK
(the OpenAI-compatible path is plain HTTP because most users of it are not on
OpenAI).

## Budgets

| What | Budget | Measured |
|---|---|---|
| Idle memory | 80 MB RSS | about 65 MB after a scoring run |
| Peak memory while embedding | 1 GB RSS | about 850 MB (batch of 256) |
| Binary | 60 MB | 54 MB |
| Embedding speed | 50 ms per headline on one core | about 30 ms |
| API latency, any read endpoint | p95 100 ms with 30 days of data | to measure |

## Layout

```
cmd/tempobrief/     main: config, wiring, HTTP server, shutdown, healthcheck
internal/config/    env parsing
internal/store/     SQLite schema, migrations, every query
internal/settings/  the settings document, validation, secret redaction
internal/feeds/     fetching, parsing, OPML
internal/embed/     local embeddings, vector math
internal/llm/       providers and Generate[T]
internal/pipeline/  the tick: fetch, cluster, score, briefs, alerts, learn
internal/deliver/   Discord, email, ntfy, webhooks, tracked and vote links
internal/auth/      password, sessions, rate limit
internal/api/       HTTP handlers and API types (tygo source)
web/                web app; web/dist is embedded
site/               brief.nordtempo.com, a static page with no build step
design/             design notes
docs/               specs
```

## Commands

See the Makefile: `dev`, `test`, `lint`, `types`, `build`, `image`,
`image-cli`. Module `github.com/NordTempo/tempoBrief`, binary and image
`tempobrief`.
