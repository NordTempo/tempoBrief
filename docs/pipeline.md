# Pipeline

`internal/pipeline` runs every step on a one-minute tick, one step at a time
(API actions such as "Send now" share the same lock). A failing step records its
error for the status view and does not stop the others.

| Step | Does |
|---|---|
| fetch | Downloads due feeds, stores new entries |
| cluster | Embeds new headlines, groups them into stories |
| score | Rates unscored stories from the last 24 hours |
| briefs | Sends any brief whose time has come |
| alerts | Pushes fresh high-scoring stories, if alerts are on |
| ignores | Marks delivered stories nobody opened or voted on |
| learn | Rewrites the editorial policy when enough has happened |
| prune | Once a day, deletes old stories without reactions |

## Fetching

- Each enabled feed is fetched every `TEMPOBRIEF_POLL` (15 minutes), four at a
  time, with `If-None-Match` / `If-Modified-Since`.
- On error the next fetch backs off: poll interval doubled per consecutive
  error, at most 6 hours. The feed shows as failing with the last error.
- Entries need a title and a link. GUID falls back to the link.
- Entries published more than 36 hours before they are first seen are skipped,
  so adding a feed does not import its backlog.
- Snippets are the longer of description and content, stripped of markup, at
  most 600 characters.
- Google News items (`news.google.com` links) carry the outlet in the title
  (`Headline - Publisher`). The publisher becomes the source, so outlet counts
  stay honest, and the first direct link to join the story replaces the
  redirect link.

## Embeddings

`internal/embed` runs `Xenova/multilingual-e5-small` (quantized ONNX, about
120 MB, downloaded to `/data/models` on first use) on hugot's pure Go backend.
Titles only, with the e5 `query: ` prefix; titles under four words get their
source as context ("Codex releases: 0.174.0"). About 30 ms per headline on one
core. Memory peaks at about 850 MB while a batch of 256 runs and drops back to
about 65 MB after it.

## Grouping

Each new headline joins the open story (last item within 48 hours) whose mean
vector it is most similar to, if that similarity is at least 0.93; otherwise it
starts a story. The similarity of a unit vector with a story's plain mean is its
average similarity to the story's members (average linkage).

The mean is deliberately not renormalized. Unrelated headlines in one language
still score about 0.85 with e5, so a renormalized centroid of many members
drifts towards "an average headline" and attracts everything (this happened in
the prototype tempoBrief replaces). With the plain mean, a story whose members disagree
has a short mean vector and attracts nothing.

Measured with e5-small on live headlines: unrelated 0.80 to 0.87, different
events about the same company or case 0.90 to 0.93, the same event 0.93 to
0.99, the same event in Norwegian and English about 0.93. No threshold
separates the middle band, so grouping only takes the confident part.

The model catches the middle band, without a call of its own:

- **While scoring.** Each scoring call also lists, for every story, up to three
  already scored stories of the last two days with a similarity of 0.85 or
  more. The model answers `sameAs` per story: the id of a listed or earlier
  story about the same event, or 0. Each group is merged into its oldest story
  (items move over, so it lists every outlet), so a duplicate of a story
  already briefed is not briefed again. Measured: three wordings of one event
  scored 0.89 to 0.91 with each other, while a different Rust migration story
  scored 0.92, so embeddings alone cannot decide.
- **Before a brief.** The model groups the brief's candidates by event once
  more, and each group is merged into its best-scored story.

A merged-away story keeps its row with no items; lists, alerts and grouping
skip it.

## Scoring

Batches of 40 stories, scored as soon as 40 are waiting; fewer wait until 45
minutes after the last small batch, so a trickle of new stories does not cost a
request every minute. One line per story: id, topic, outlet count and names, title,
240 characters of snippet. The system prompt carries the reader's interests,
the current policy, and the 12 most recent likes and dislikes. The answer per
story: score 0 to 10, a summary of at most 25 words in the reader's language,
and a reason of at most 10 words. Effort `low` where the provider supports it.

The final score is the model's score plus the taste adjustment, clamped to 0 to
10. See [learning.md](learning.md#taste).

## Briefs

A brief schedule has a name, a local time, optional weekdays, a minimum score, a
maximum number of stories (default 10) and channels. It is due when its time has passed today by less
than 3 hours, no brief of that schedule went out since, and tempoBrief was
installed before the slot (a fresh install never sends an empty brief).

Candidates: scored stories first seen in the last 24 hours, never briefed, at or
above the minimum, best first. The best twice-the-maximum go to the duplicate
merge, then the maximum applies, whatever the topics. Nothing fills a topic or
the brief: a quiet day sends fewer stories. Topics are ordered by their best
story. The brief is stored before
delivery, so the web app and Home Assistant show it even if a channel fails.

Alerts: stories first seen in the last 6 hours, at or above the alert score,
covered by at least the minimum number of outlets, never alerted.

## Data model

SQLite in WAL mode, one file, `/data/tempobrief.db`. Times are unix seconds.
Schema: [migrations/0001_init.sql](../internal/store/migrations/0001_init.sql).

| Table | Holds |
|---|---|
| feeds | URL, title, topic, fetch validators and health |
| items | One feed entry, its source outlet, and its story |
| stories | One event: title, link, mean vector, scores, summary, reason, reactions |
| briefs | Each brief: schedule, title, story ids in order, delivery results |
| policies | Every version of the editorial policy, newest last |
| settings | The settings document |
| meta | Internal state (install time, last learn, provider health) |
| auth_sessions | Login sessions (hashed tokens) |

Stories without reactions are deleted 30 days after their last item; stories
with reactions are kept, because the learner and taste use them.
