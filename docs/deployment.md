# Deployment

## Environment

Deployment settings only. Everything a reader edits lives in the database.

| Variable | Default | Meaning |
|---|---|---|
| `TEMPOBRIEF_LISTEN` | `:8080` | HTTP listen address |
| `TEMPOBRIEF_DATA` | `/data` | Database and downloaded models |
| `TEMPOBRIEF_PUBLIC_URL` | empty | Base URL readers reach tempoBrief at. Enables tracked links, vote links, brief links and the feed URLs. |
| `TEMPOBRIEF_PASSWORD` / `_FILE` | generated | Login password. Unset: generated on first start and logged once, changeable in Settings. |
| `TEMPOBRIEF_AUTH` | `password` | `off` only behind a proxy that authenticates |
| `TEMPOBRIEF_COOKIE_SECURE` | `auto` | `auto` follows TLS or `X-Forwarded-Proto` |
| `TEMPOBRIEF_POLL` | `15m` | Feed fetch interval, at least 5m |
| `TEMPOBRIEF_EMBED_MODEL` | `Xenova/multilingual-e5-small` | Hugging Face repo of an ONNX sentence embedding model |
| `TEMPOBRIEF_EMBED_FILE` | `onnx/model_quantized.onnx` | ONNX file in that repo |
| `TEMPOBRIEF_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Changing the embedding model invalidates stored story vectors: start with a
fresh database, or accept a few days of weaker grouping and taste.

## Auth

One password (argon2id), cookie sessions for 30 days, login rate limiting,
SameSite=Strict cookies plus a same-origin check on mutations. Unauthenticated
routes: `/healthz`, login, the `/auth/github/*` redirects, `/r/{id}` (redirect to a stored URL), signed vote
links, and the read-token outputs `/brief.json` and `/feed.xml`.

**Public pages** (Settings, off by default) let anyone read the brief and
stories pages without logging in. Visitors get the same stories without the
reader's votes, opens, taste notes or delivery results, no vote buttons, and
plain article links that do not count as opens. Everything else still needs a
session; "Log in" sits at the right of the header. Every response carries
`X-Robots-Tag: noindex`, so public pages stay out of search engines.

### Sign in with GitHub

Optional, set up from Settings, and needs `TEMPOBRIEF_PUBLIC_URL`. "Set up
with GitHub" posts a prefilled GitHub App to GitHub's
[manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest):
a private app with no permissions and no webhook, whose callback is
`/auth/github/callback`. The reader clicks Create; GitHub sends them back to
`/auth/github/setup` with a code that tempoBrief exchanges for the app's client
ID and secret. The account that created the app is the only one let in,
matched by its numeric GitHub user ID.

Sign-in is the web flow with PKCE. Every flow has a one-time state, valid ten
minutes and bound to the browser that started it with a cookie. The client
secret and the allowed account are stored apart from the settings document,
so only this flow changes them; "Turn off" in Settings forgets them (delete
the app on GitHub too). The password keeps working.

## Settings

One JSON document (`internal/settings`): interests, language, time zone,
provider, brief schedules, alerts, channels, learning switches, public pages, the read token
and the link-signing secret. The API returns it with secrets replaced by a
placeholder; sending the placeholder back keeps the stored value.

## Docker

```bash
docker run -d -p 8080:8080 -v tempobrief:/data \
  -e TEMPOBRIEF_PUBLIC_URL=https://brief.example.com \
  ghcr.io/nordtempo/tempobrief:latest
```

- `:latest`: distroless, static binary, nonroot. For the `anthropic` and
  `openai` providers.
- `:latest-cli`: Debian slim with Node, Claude Code and Codex. For the CLI
  providers. Log in once: `docker exec -it tempobrief claude setup-token`, or
  pass `CLAUDE_CODE_OAUTH_TOKEN`; for Codex, mount a logged-in `CODEX_HOME`.

`GOMEMLIMIT` is 1 GiB: embedding peaks at about 850 MB for a few seconds per
batch, and idle memory is about 65 MB.

## Tempo Dock

`dock.toml` describes the build and health check. Set `TEMPOBRIEF_PUBLIC_URL`
to the app's domain and mount a volume at `/data`.

## The marketing page

brief.nordtempo.com is `site/`: one static page served by Caddy. It runs on
Tempo Dock as `tempobrief/production/site`, connected through Dock's GitHub App
to `main` with `--dir site`, so every push to `main` that changes `site/`
deploys it. Nothing in `site/` is part of the app image.
