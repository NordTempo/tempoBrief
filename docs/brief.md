# Product brief

Decided 2026-11-25. tempoBrief grew out of a smaller personal news bot, after a
survey of what already exists found nothing that does all of this: learning from reactions, local plus international news in
several languages, grouping the same event across outlets, delivery to the
reader's own channels, and the reader's own model.

## Who it is for

One reader, self-hosting. A developer or homelab person who wants the news that
matters to them each morning without reading five front pages and three link
aggregators, and who already pays for a model (an API key or a Claude or
ChatGPT subscription). Multi-user is out of scope for v1.

## What it does

- **Interests in plain sentences.** The reader writes what they want and what
  they do not, the way they would brief a human editor.
- **Their own sources.** RSS, Atom, JSON Feed, OPML import, Google News search
  feeds. No crawling, no scraping.
- **One story per event.** A local multilingual embedding model groups
  headlines from different outlets and languages. Outlet count is a signal.
- **Scored by their model.** Each story gets a 0 to 10 score, a one-line summary
  in the reader's language, and a short reason.
- **Briefs on a schedule.** One or more named briefs (morning, evening,
  weekdays only), each with its own bar and channels. Optional breaking alerts.
- **Learns, visibly.** Votes, opens and skips feed two mechanisms: a taste
  adjustment per story and an editorial policy the reader can read, edit and
  roll back. Every story says why it ranked where it did. See
  [learning.md](learning.md).
- **Delivered where they already look.** Discord (with vote buttons), email,
  ntfy (with vote actions), webhooks, Home Assistant, RSS out.

## Decisions

| Decision | Why |
|---|---|
| Go, one static binary, SQLite | Same as the other NordTempo self-hosted tools. Runs next to anything, backs up as one file. |
| Embeddings run locally (hugot, pure Go) | Grouping and taste need no model calls, so they cost nothing and work with any provider, including CLIs without an embeddings API. |
| Model loaded per batch, then released | The model needs about 850 MB while a batch runs; idle memory stays near 65 MB. |
| Bring your own model, four kinds | API keys for people who have them; CLIs for people who only have a subscription. See [providers.md](providers.md). |
| Interests outrank the learned policy | Learning should narrow and sharpen what the reader asked for, never replace it. |
| Policy is text, not weights | Readable, editable, diffable. A reader trusts what they can see. |
| Opens and skips count, weakly | Most people never vote. An ignored story says a little; a pattern of them says a lot. |
| No full-text fetching | Many outlets paywall; feeds give a title and teaser. Grouping across outlets recovers context instead. |
| Settings in the database, deployment in env | Users change interests and channels in the UI; operators set the port, data dir and public URL. |

## Not in v1

Multi-user, hosted SaaS, full-text extraction, a reading app (tempoBrief links
out), mobile apps (ntfy and email cover phones), training a ranking model.
