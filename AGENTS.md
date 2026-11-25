tempoBrief is a self-hosted news brief that learns what its reader cares about:
feeds in, stories grouped across outlets and languages, scored by the reader's
own model, briefs out to Discord, email, ntfy, webhooks and Home Assistant, and
reactions back in as an editorial policy the reader can read and edit.

One Go binary with an embedded web app and an embedded SQLite database, shipped
as one Docker image (plus a `-cli` variant with the Claude Code and Codex CLIs).

## Read before building

1. [docs/brief.md](docs/brief.md): the product, every decision and why.
2. [docs/roadmap.md](docs/roadmap.md): milestones in build order. Work milestone
   by milestone.
3. [docs/stack.md](docs/stack.md): languages, allowed dependencies, layout,
   commands, budgets.
4. The spec for what you touch:
   - [docs/pipeline.md](docs/pipeline.md): fetching, embeddings, grouping,
     scoring, briefs, alerts, data model.
   - [docs/learning.md](docs/learning.md): signals, taste, the editorial policy.
   - [docs/providers.md](docs/providers.md): model providers.
   - [docs/delivery.md](docs/delivery.md): channels, tracked links, vote links.
   - [docs/api.md](docs/api.md): HTTP API.
   - [docs/deployment.md](docs/deployment.md): config, auth, settings, Docker.
5. [design/README.md](design/README.md) for the look (direction A, phone first).

## Rules

- Lightweight is a feature. Stay inside the dependency allowlist in
  [docs/stack.md](docs/stack.md); a new dependency needs a one-line
  justification there in the same change.
- Learning must stay visible. Anything that changes how a story is ranked shows
  up in the story's "why" line or in the policy history. No hidden weights.
- The reader's own interests outrank anything learned. The policy refines them
  and never contradicts them.
- Never send the reader's data anywhere except their configured model and
  channels. No telemetry.
- Secrets never leave the server: the API returns them redacted.
- No continuously repainting CSS animations. Static loading states only.
- No em dashes in UI copy or docs.
- Keep docs in sync: a change in behaviour updates the matching spec in the same
  commit. Tick roadmap items as they land.

## Commands

```bash
make dev        # server on :8080 + Vite on :5173, password tempobrief-dev
make test       # go test -race ./... and web tests
make lint       # go vet, staticcheck, biome, tsc
make types      # regenerate web/src/api/*.gen.ts from Go structs (tygo)
make build      # web build, then a static binary in ./bin/tempobrief
make image      # Docker image tempobrief:dev; make image-cli for the CLI variant
```

Run `make lint test` before declaring any task done. After changing a Go type
the API returns (`internal/api/types.go` or `internal/settings`), run
`make types` and commit the generated files.

## Conventions

- Go: standard library first, small packages under `internal/`, plain functions
  over interfaces until there are two implementations, table-driven tests,
  `log/slog`.
- Model calls go through `llm.Generate[T]`: the answer type is the schema.
- TypeScript: `strict`, no `any`, API types only from the generated files.
- Commits: Conventional Commits, for example `feat(learn): show policy diffs`.
