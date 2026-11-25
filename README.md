# tempoBrief

A self-hosted news brief that learns what you care about.

You describe your interests in a few sentences and add the feeds you trust.
tempoBrief reads them all day, groups the same event across outlets and
languages into one story, scores every story with the model you connect, and
sends a short brief at the times you choose: to Discord, email, ntfy, a webhook
or Home Assistant.

Every 👍, 👎, open and skip teaches it. Those reactions become an **editorial
policy**, a short list of rules you can read, edit and roll back. Every story
also shows **why it ranked where it did**.

One Go binary with an embedded web app and SQLite. Bring your own model: the
Claude API, any OpenAI-compatible API (OpenAI, OpenRouter, Ollama, LM Studio),
or a Claude Code or Codex CLI that is already logged in.

> Status: early. The pipeline, API, deliveries and web app work; not yet
> released.

## How it works

```
feeds ──fetch──▶ items ──embed + group──▶ stories ──score──▶ brief ──▶ Discord / email / ntfy / webhook / HA
                                                   ▲                         │
                         interests + policy + taste │      👍 👎 opens skips  │
                                                   └──── learning ◀──────────┘
```

1. **Fetch.** Every feed every 15 minutes, with conditional requests and backoff.
2. **Group.** A local multilingual model embeds each headline, so "Norges Bank
   holder renten" and "Norway's central bank holds rates" become one story
   with two sources. No model call, no API cost.
3. **Score.** Your model rates each story 0 to 10 against your interests and
   the learned policy, writes a one-line summary in your language and says why.
   A **taste** adjustment then moves the score up to 1.5 points based on how
   you reacted to the most similar past stories.
4. **Brief.** At each scheduled time, the best unsent stories per topic, with
   same-event duplicates removed.
5. **Learn.** Every few days, after enough new reactions, the model rewrites the
   editorial policy from them. You see what changed and why.

## Run it

```bash
docker run -d --name tempobrief -p 8080:8080 -v tempobrief:/data \
  -e TEMPOBRIEF_PUBLIC_URL=https://brief.example.com \
  ghcr.io/nordtempo/tempobrief:latest
docker logs tempobrief   # prints the generated password once
```

Open the web app, then: write your interests, import an OPML file or add feeds,
connect a model and add a channel. The first brief goes out at the next
scheduled time; "Send now" sends one immediately.

To use a Claude or ChatGPT subscription through its CLI instead of an API key,
run the `-cli` image and log the CLI in inside the container. See
[docs/providers.md](docs/providers.md).

## Develop

```bash
make dev    # server on :8080 and the web app on :5173
make test   # go test -race ./...
make lint   # go vet + staticcheck
make build  # static binary in bin/tempobrief
```

Read [AGENTS.md](AGENTS.md) first.

## License

[MIT](LICENSE). The bundled fonts are under the SIL Open Font License 1.1.
