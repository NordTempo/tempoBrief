# HTTP API

JSON over HTTP. Request and response types live in
[internal/api/types.go](../internal/api/types.go) and are generated into
`web/src/api/types.gen.ts`. Errors are `{"error": {"code", "message", "fields"?}}`.
Times are RFC 3339 UTC strings. Everything under `/api/v1` except `me`, `login`
and `logout` needs a session. With public pages on, the four brief and stories
reads also answer without one, minus the reader's reactions
([deployment.md](deployment.md#auth)).

| Method and path | Does |
|---|---|
| `GET /healthz` | `ok` when the database is writable |
| `GET /api/v1/me` | Auth mode, logged in or not, GitHub sign-in on, public pages on, version |
| `POST /api/v1/login` · `POST /api/v1/logout` · `PUT /api/v1/password` | Session handling |
| `GET /api/v1/github` · `POST /api/v1/github/setup` · `DELETE /api/v1/github` | Sign in with GitHub: status, the manifest form to post to GitHub, turn off |
| `GET /auth/github/login` · `/auth/github/callback` · `/auth/github/setup` | Browser redirects of the GitHub flows ([deployment.md](deployment.md#sign-in-with-github)) |
| `GET /api/v1/status` | Step health, feed counts, stories scored today, last brief, setup checklist |
| `GET /api/v1/briefs` · `GET /api/v1/briefs/latest` · `GET /api/v1/briefs/{id}` | Brief list and briefs with stories grouped by topic |
| `POST /api/v1/briefs/send` `{scheduleId}` | Build and send that brief now |
| `GET /api/v1/stories?hours=&min=&topic=&q=&voted=` | Scored stories, best first, plus topics |
| `PUT /api/v1/stories/{id}/vote` `{vote}` | -1, 0 (clear) or 1; returns the story |
| `GET /api/v1/feeds` · `POST` · `PUT /{id}` · `DELETE /{id}` | Feeds; adding one fetches it at once |
| `POST /api/v1/feeds/{id}/fetch` | Fetch now |
| `POST /api/v1/feeds/import` `{opml}` · `GET /api/v1/feeds/export` | OPML in and out |
| `GET /api/v1/learning` | Current policy, history, signal counts, recent reactions |
| `PUT /api/v1/policy` `{text}` · `POST /api/v1/policy/{id}/restore` · `POST /api/v1/policy/learn` | Edit, restore, relearn now |
| `GET /api/v1/settings` · `PUT /api/v1/settings` | Settings (secrets redacted) and feed URLs |
| `POST /api/v1/settings/read-token` | Rotate the read token |
| `POST /api/v1/settings/test-provider` · `POST /api/v1/settings/test-channel` `{channelId}` | Checks |

Public link routes are described in [delivery.md](delivery.md#links).
