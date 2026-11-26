# Learning

It is hard to know up front exactly what you want to read. tempoBrief starts
from the reader's own description and then follows how they actually react.
Two mechanisms, both visible.

## Signals

| Signal | Meaning | Weight |
|---|---|---|
| liked | 👍 in the web app, Discord, ntfy or an email link | +1 |
| disliked | 👎 | -1 |
| clicked | Opened through a tracked link, no vote | +0.5 |
| ignored | Delivered in a brief, then neither opened nor voted within `ignoreAfterHours` (48) | -0.25 |
| missed | Reported by the reader as a story they should have got ([Missed](#missed)) | +1.5 |

A miss wins over a vote, a vote over a click and a click over an ignore. Tracked links and vote
links need `TEMPOBRIEF_PUBLIC_URL`; without it, links go straight to the
article and only votes in the web app and Discord count.

## Taste

A per-story adjustment that needs no model call: how the reader reacted to the
most similar stories they reacted to before (180 days).

- Neighbours: past reacted stories with cosine similarity at least 0.88 to this
  story, the closest 8.
- Value: their signal weights averaged with weights `exp((sim - 1) / 0.02)`, so
  the closest dominate, clamped to -1 to 1.
- Confidence: full from 3 neighbours, proportionally less below.
- Adjustment: value × confidence × 1.5, added to the model's 0 to 10 score.

The story's "why" line says when taste moved it ("like 3 you liked
to"). The constants are in `internal/pipeline/taste.go`; they were picked for
e5 title embeddings, where stories on the same subject score about 0.88 and up.

## Policy

A short list of rules the scorer follows, rewritten by the model from
reactions. It is what makes the learning legible.

- When: at most every 72 hours, once at least 10 new reactions have happened
  since the last rewrite. A miss counts as 3. "Relearn now" in the web app runs it at once.
- Input: the reader's interests, the current policy, and up to 300 reactions
  from the last 60 days (kind, topic, title, summary, the reader's note on
  misses), misses first, then newest first.
- Output: the full new policy (at most 15 bullets of at most 25 words, concrete
  and testable) and a list of what changed and why.
- Rules for the model: keep what holds, drop what reactions contradict, add
  what they reveal, never invent, never contradict the reader's interests;
  where reactions disagree with the interests, write the narrower rule.
- Every version is stored. The reader can edit the policy (a new "edited"
  version) or restore any old version. The scorer always uses the newest.

## Missed

When the reader hears about a story elsewhere that tempoBrief should have shown
them, they report its link, with an optional note on why it matters
(`POST /api/v1/missed`). tempoBrief first finds out why they did not get it:

1. It looks the link up among fetched items, ignoring scheme, `www.`, query
   and trailing slash.
2. Otherwise it reads the page (Open Graph title first) and compares the title
   with stories from the last 7 days. Cosine 0.93 or more counts as the same
   story; below that, titles may be different events.
3. Otherwise no feed had it. The article is stored as a story without items,
   so it stays out of lists and scoring but still teaches taste, under the
   topic of the closest story (General below 0.88). tempoBrief then finds a
   feed and adds it under that topic (see below); the reader can remove it.

### Finding a feed

`feeds.Discover` collects candidates, fetches every one, and keeps real feeds:

- feeds the article page and the site's front page link to
  (`<link rel="alternate">`);
- feeds of known platforms: GitHub releases, Medium authors, subreddits;
- common paths (`/feed`, `/rss`, `/rss.xml`, `/feed.xml`, `/atom.xml`,
  `/index.xml`, `/feeds/posts/default`) when nothing links a feed;
- section feeds under the article's first path segment (`/norge/feed`,
  `/norge/toppsaker.rss` and so on);
- last, a Google News search for the site and for its section
  (`site:nrk.no/norge`), for sites without any feed.

They rank by: carries the article (by link or headline), then matches the
section, then a real feed over a search. The best one not already added is
added; the answer says whether it carries the article and about how many items
a day it brings, since every item costs scoring. Discovery gives up after 25
seconds.

For a fetched story, the answer is one of: never scored, scored under the
lowest brief bar (with the model's reason), in line for the next brief, left
out because its topic was full, or sent in a brief. A line about alerts says
when they are off or what bar the story missed.

The miss is recorded in every case. Taste weighs it +1.5. The scoring prompt
lists recent misses with the reader's notes, as stories that deserve 7 or more,
and the policy learner reads them first. A miss can be taken back
(`DELETE /api/v1/stories/{id}/missed`).
