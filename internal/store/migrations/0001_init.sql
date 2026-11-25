-- tempoBrief schema. Times are unix seconds (UTC). See docs/pipeline.md#data-model.

CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- User-editable settings as JSON documents, one row per key.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE auth_sessions (
  token_hash BLOB PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL
);

CREATE TABLE feeds (
  id            INTEGER PRIMARY KEY,
  url           TEXT NOT NULL UNIQUE,
  title         TEXT NOT NULL DEFAULT '',
  topic         TEXT NOT NULL DEFAULT 'General',
  site_url      TEXT NOT NULL DEFAULT '',
  enabled       INTEGER NOT NULL DEFAULT 1,
  etag          TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  next_fetch_at INTEGER NOT NULL DEFAULT 0,
  last_fetch_at INTEGER NOT NULL DEFAULT 0,
  last_ok_at    INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT NOT NULL DEFAULT '',
  error_count   INTEGER NOT NULL DEFAULT 0,
  item_count    INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

-- A story is one event, covered by one or more items from any feeds.
CREATE TABLE stories (
  id          INTEGER PRIMARY KEY,
  topic       TEXT NOT NULL,
  title       TEXT NOT NULL,
  url         TEXT NOT NULL,
  snippet     TEXT NOT NULL,
  centroid    BLOB NOT NULL, -- mean title embedding, float32 little endian
  n_items     INTEGER NOT NULL DEFAULT 1,
  first_seen  INTEGER NOT NULL,
  last_seen   INTEGER NOT NULL,
  llm_score   INTEGER,       -- 0 to 10 from the model
  taste       REAL,          -- -1 to 1 from similar past signals
  taste_n     INTEGER,       -- how many similar signals taste used
  score       REAL,          -- final rank: llm_score adjusted by taste
  summary     TEXT,
  reason      TEXT,
  scored_at   INTEGER,
  briefed_at  INTEGER,
  alerted_at  INTEGER,
  vote        INTEGER,       -- -1 or 1
  voted_at    INTEGER,
  clicked_at  INTEGER,
  ignored_at  INTEGER        -- delivered, then neither clicked nor voted in time
);
CREATE INDEX stories_last_seen ON stories(last_seen);
CREATE INDEX stories_first_seen ON stories(first_seen);
CREATE INDEX stories_unscored ON stories(scored_at) WHERE scored_at IS NULL;
CREATE INDEX stories_voted ON stories(voted_at) WHERE vote IS NOT NULL;

CREATE TABLE items (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid         TEXT NOT NULL,
  url          TEXT NOT NULL,
  title        TEXT NOT NULL,
  snippet      TEXT NOT NULL,
  source       TEXT NOT NULL, -- outlet name; the publisher for aggregator items
  published_at INTEGER NOT NULL,
  seen_at      INTEGER NOT NULL,
  story_id     INTEGER REFERENCES stories(id) ON DELETE SET NULL,
  UNIQUE (feed_id, guid)
);
CREATE INDEX items_story ON items(story_id);
CREATE INDEX items_unclustered ON items(seen_at) WHERE story_id IS NULL;

CREATE TABLE briefs (
  id          INTEGER PRIMARY KEY,
  schedule_id TEXT NOT NULL,
  title       TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  story_ids   TEXT NOT NULL, -- JSON array, in brief order
  deliveries  TEXT NOT NULL DEFAULT '[]' -- JSON array of {channel, ok, error}
);
CREATE INDEX briefs_schedule ON briefs(schedule_id, created_at);

-- Every version of the editorial policy, newest last. The newest row is the one
-- the scorer uses.
CREATE TABLE policies (
  id         INTEGER PRIMARY KEY,
  created_at INTEGER NOT NULL,
  source     TEXT NOT NULL, -- 'learned' or 'edited'
  text       TEXT NOT NULL,
  changes    TEXT NOT NULL DEFAULT '[]', -- JSON array of short strings
  signals    INTEGER NOT NULL DEFAULT 0  -- signals the learner saw
);
