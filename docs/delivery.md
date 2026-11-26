# Delivery

A channel is one destination with its own kind and settings. Brief schedules and
alerts list the channels they go to; an empty list means every enabled channel.
"Test" in Settings sends a sample alert to one channel.

| Kind | Brief | Alert | Votes |
|---|---|---|---|
| Discord | Header, a heading per topic, one message per story | One message per story | 👍 👎 buttons on every story, optionally owner only |
| Email (SMTP) | One HTML + text email | One email per story | 👍 👎 links (open a confirm page) |
| ntfy | One notification listing stories; tap opens the brief | One notification per story | 👍 👎 action buttons |
| Webhook | `POST` JSON (`WebhookPayload`) | Same, `kind: "alert"` | None |

Discord needs its own bot (no privileged intents). tempoBrief keeps one gateway
connection per bot token and reconnects when settings change.

## Discord

Settings walks through four steps, and the token is the only thing copied:

1. **Create the bot.** A link to discord.com/developers/applications and three
   lines: New Application, Bot, Reset Token. Discord has no way to prefill a
   new application.
2. **Paste its token.** `POST /api/v1/discord/check` reads the application
   behind it (`/oauth2/applications/@me`): the bot's name and its owner (a
   team's owner for team apps). The owner becomes the only one who may vote,
   a switch away from off.
3. **Add it to your server.** An invite link with `scope=bot` and only View
   Channel and Send Messages (messages suppress embeds; buttons need no
   permission). Coming back to the tab checks again.
4. **Pick the channel** from the text and announcement channels of every
   server the bot is in, grouped by server.

"Send a test" then posts a sample story with vote buttons.

## Links

With `TEMPOBRIEF_PUBLIC_URL` set, deliveries use:

- `/r/{id}`: records an open, then redirects to the stored article URL. Never an
  open redirect: the target always comes from the database.
- `/v/{id}/{up|down}/{sig}`: vote links signed with HMAC-SHA256 over story and
  vote, using a secret generated at first start. `GET` shows a confirm button,
  because mail scanners open every link; `POST` records the vote (ntfy's action
  buttons post directly).
- `/briefs/{id}`: the brief in the web app.

## Read-only outputs

Both need `?token=` with the read token from Settings (rotatable).

- `/brief.json`: the latest brief as one small JSON object, the same shape as
  the webhook payload. Fits a Home Assistant REST sensor:

  ```yaml
  rest:
    - resource: https://brief.example.com/brief.json?token=…
      scan_interval: 900
      sensor:
        - name: News brief
          value_template: "{{ value_json.count }}"
          json_attributes: [title, date, url, stories]
  ```

- `/feed.xml`: RSS of the last 48 hours' stories at or above the first brief's
  minimum score, for any feed reader.
