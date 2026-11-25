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

A vote wins over a click and a click over an ignore. Tracked links and vote
links need `TEMPOBRIEF_PUBLIC_URL`; without it, links go straight to the
article and only votes in the web app and Discord count.

## Taste

A per-story adjustment that needs no model call: how the reader reacted to the
most similar stories they reacted to before (180 days).

- Neighbours: past reacted stories with cosine similarity at least 0.88 to this
  story, the closest 8.
- Value: their signal weights averaged with weights `exp((sim - 1) / 0.02)`, so
  the closest dominate. Range -1 to 1.
- Confidence: full from 3 neighbours, proportionally less below.
- Adjustment: value × confidence × 1.5, added to the model's 0 to 10 score.

The story's "why" line says when taste moved it ("like 3 you liked
to"). The constants are in `internal/pipeline/taste.go`; they were picked for
e5 title embeddings, where stories on the same subject score about 0.88 and up.

## Policy

A short list of rules the scorer follows, rewritten by the model from
reactions. It is what makes the learning legible.

- When: at most every 72 hours, once at least 10 new reactions have happened
  since the last rewrite. "Relearn now" in the web app runs it at once.
- Input: the reader's interests, the current policy, and up to 300 reactions
  from the last 60 days (kind, topic, title, summary), newest first.
- Output: the full new policy (at most 15 bullets of at most 25 words, concrete
  and testable) and a list of what changed and why.
- Rules for the model: keep what holds, drop what reactions contradict, add
  what they reveal, never invent, never contradict the reader's interests;
  where reactions disagree with the interests, write the narrower rule.
- Every version is stored. The reader can edit the policy (a new "edited"
  version) or restore any old version. The scorer always uses the newest.
