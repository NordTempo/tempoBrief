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
