# Roadmap

One milestone per branch and PR, in order. Each ends deployable.

| Milestone | Result | Release |
|---|---|---|
| M0 Core | Pipeline, API, deliveries, learning, Docker image | |
| M1 Web app | Brief, stories, learning, feeds, settings, first run (done) | v0.1.0 |
| M2 Hardening | API tests, budgets measured, backups, more feeds tested | |
| M3 Open source | Public README, screenshots, demo data | v1.0.0 |

## M0 Core

Status: in progress, 2026-11-25.

- [x] Go module, layout, Makefile, Dockerfile (default and `-cli`), CI, tygo.
- [x] Store: schema, migrations, queries.
- [x] Feeds: conditional fetch, backoff, Google News publishers, OPML.
- [x] Local embeddings, grouping with average linkage (no centroid drift).
- [x] Providers: Claude API, OpenAI-compatible, Claude Code CLI, Codex CLI.
- [x] Scoring with interests, policy, examples and taste.
- [x] Brief schedules with duplicate check; alerts.
- [x] Delivery: Discord with vote buttons, email, ntfy with vote actions, webhook.
- [x] Tracked links, signed vote links, ignore detection.
- [x] Policy learning with history, edit and restore.
- [x] API for everything above; `/brief.json` and `/feed.xml` with a read token.
- [x] Tests: pipeline end to end with fakes, schedule, taste, feeds, schema, settings, links.
- [x] End-to-end run against 43 real feeds with the Claude CLI.
- [ ] Measure idle memory and API latency; fill the budgets table.
- [ ] Run daily on a real install for a week.

## M1 Web app

Direction A ("Column") picked 2026-11-25, built phone first: one reading
column, a bottom tab bar on phones, 44 px tap targets, 16 px inputs (no iOS
zoom), safe-area insets. JS 22 KB gzipped.

- [x] Shell, login, first-run checklist from `/api/v1/status`.
- [x] Brief view (latest and history), votes, "why" lines, "Send now".
- [x] Stories: everything scored, filters, search.
- [x] Learning: current policy, what changed, history with restore, edit,
      relearn, recent reactions.
- [x] Feeds: list with health, add, edit topic, OPML import and export.
- [x] Settings: interests, language and time zone, provider with test,
      schedules, alerts, channels with test, learning switches, feed URLs.
- [ ] Add to Home Screen icon and manifest.
- [x] Public pages: brief and stories readable without logging in, no votes.
- [x] Sign in with GitHub, set up from Settings with GitHub's manifest flow.

More sign-in providers (generic OIDC: Google, Authentik, Pocket ID) later.

## Later

Per-brief interests, an LLM pass on borderline grouping pairs, full-text fetch
for feeds that allow it, multi-user.
