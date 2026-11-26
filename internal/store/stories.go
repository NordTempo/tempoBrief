package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Story is one event and everything tempoBrief knows about it.
type Story struct {
	ID        int64
	Topic     string
	Title     string
	URL       string
	Snippet   string
	NItems    int
	FirstSeen time.Time
	LastSeen  time.Time
	LLMScore  *int64
	Taste     *float64
	TasteN    int
	Score     *float64
	Summary   string
	Reason    string
	BriefedAt *int64
	Vote      int // -1, 0 (none) or 1
	Clicked   bool
	Ignored   bool
	// Sources are the distinct outlets that carried the story, first one first.
	Sources []string
}

const storyColumns = `s.id, s.topic, s.title, s.url, s.snippet, s.n_items, s.first_seen, s.last_seen,
	s.llm_score, s.taste, COALESCE(s.taste_n, 0), s.score, COALESCE(s.summary, ''), COALESCE(s.reason, ''),
	s.briefed_at, COALESCE(s.vote, 0), s.clicked_at IS NOT NULL, s.ignored_at IS NOT NULL,
	(SELECT json_group_array(source) FROM (SELECT source FROM items WHERE story_id = s.id GROUP BY source ORDER BY MIN(id)))`

func (s *Store) queryStories(ctx context.Context, where string, args ...any) ([]Story, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+storyColumns+" FROM stories s WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Story
	for rows.Next() {
		var st Story
		var first, last int64
		var llm, briefed sql.NullInt64
		var taste, score sql.NullFloat64
		var sources string
		if err := rows.Scan(&st.ID, &st.Topic, &st.Title, &st.URL, &st.Snippet, &st.NItems, &first, &last,
			&llm, &taste, &st.TasteN, &score, &st.Summary, &st.Reason, &briefed, &st.Vote, &st.Clicked, &st.Ignored,
			&sources); err != nil {
			return nil, err
		}
		st.FirstSeen, st.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		st.LLMScore, st.BriefedAt = nullInt(llm), nullInt(briefed)
		if taste.Valid {
			st.Taste = &taste.Float64
		}
		if score.Valid {
			st.Score = &score.Float64
		}
		if err := json.Unmarshal([]byte(sources), &st.Sources); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) Story(ctx context.Context, id int64) (Story, error) {
	list, err := s.queryStories(ctx, "s.id = ?", id)
	if err != nil {
		return Story{}, err
	}
	if len(list) == 0 {
		return Story{}, ErrNotFound
	}
	return list[0], nil
}

// StoriesByID returns stories in the order given, skipping ids that no longer exist.
func (s *Store) StoriesByID(ctx context.Context, ids []int64) ([]Story, error) {
	b, _ := json.Marshal(ids)
	list, err := s.queryStories(ctx, "s.id IN (SELECT value FROM json_each(?))", string(b))
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Story, len(list))
	for _, st := range list {
		byID[st.ID] = st
	}
	out := make([]Story, 0, len(ids))
	for _, id := range ids {
		if st, ok := byID[id]; ok {
			out = append(out, st)
		}
	}
	return out, nil
}

// StoryFilter selects stories for the API's story list.
type StoryFilter struct {
	Since    time.Time
	MinScore float64
	Topic    string
	Query    string
	Voted    bool // only stories with a vote
	Sort     string // SortBest (default), SortNewest or SortOutlets
	Limit    int
}

// Story list orders. Ties fall back to the best story first.
const (
	SortBest    = "best"
	SortNewest  = "newest"
	SortOutlets = "outlets"
)

var storyOrder = map[string]string{
	SortBest:    "s.score DESC, s.n_items DESC, s.first_seen DESC",
	SortNewest:  "s.first_seen DESC, s.score DESC",
	SortOutlets: "(SELECT COUNT(DISTINCT source) FROM items WHERE story_id = s.id) DESC, s.score DESC",
}

// ListStories returns scored stories in the filter's order.
func (s *Store) ListStories(ctx context.Context, f StoryFilter) ([]Story, error) {
	// n_items is 0 once a story was merged into another.
	where := []string{"s.first_seen >= ?", "s.score IS NOT NULL", "s.score >= ?", "s.n_items > 0"}
	args := []any{f.Since.Unix(), f.MinScore}
	if f.Topic != "" {
		where = append(where, "s.topic = ?")
		args = append(args, f.Topic)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(s.title LIKE ? OR s.summary LIKE ? OR s.snippet LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if f.Voted {
		where = append(where, "s.vote IS NOT NULL")
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	order, ok := storyOrder[f.Sort]
	if !ok {
		order = storyOrder[SortBest]
	}
	args = append(args, limit)
	return s.queryStories(ctx, strings.Join(where, " AND ")+" ORDER BY "+order+" LIMIT ?", args...)
}

// Topics lists topics that have stories since the given time, for filters.
func (s *Store) Topics(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT topic FROM stories WHERE first_seen >= ? AND n_items > 0 ORDER BY topic", since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Clustering.

// PendingItem is an item not yet assigned to a story.
type PendingItem struct {
	ID          int64
	Topic       string
	Title       string
	URL         string
	Snippet     string
	Source      string
	PublishedAt time.Time
}

// UnclusteredItems returns items without a story, oldest first.
func (s *Store) UnclusteredItems(ctx context.Context, limit int) ([]PendingItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, f.topic, i.title, i.url, i.snippet, i.source, i.published_at
		FROM items i JOIN feeds f ON f.id = i.feed_id WHERE i.story_id IS NULL ORDER BY i.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingItem
	for rows.Next() {
		var it PendingItem
		var pub int64
		if err := rows.Scan(&it.ID, &it.Topic, &it.Title, &it.URL, &it.Snippet, &it.Source, &pub); err != nil {
			return nil, err
		}
		it.PublishedAt = time.Unix(pub, 0)
		out = append(out, it)
	}
	return out, rows.Err()
}

// Centroid is an open story's mean title embedding (not unit length).
type Centroid struct {
	StoryID int64
	NItems  int
	Vector  []float32
}

// OpenCentroids returns stories that new items may still join. Merged-away
// stories are left out.
func (s *Store) OpenCentroids(ctx context.Context, since time.Time) ([]Centroid, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, n_items, centroid FROM stories WHERE last_seen >= ? AND n_items > 0", since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Centroid
	for rows.Next() {
		var c Centroid
		var b []byte
		if err := rows.Scan(&c.StoryID, &c.NItems, &b); err != nil {
			return nil, err
		}
		c.Vector = decodeVector(b)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateStory starts a story from its first item.
func (s *Store) CreateStory(ctx context.Context, it PendingItem, centroid []float32) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.unix()
	res, err := tx.ExecContext(ctx, `INSERT INTO stories(topic, title, url, snippet, centroid, first_seen, last_seen)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, it.Topic, it.Title, it.URL, it.Snippet, encodeVector(centroid), now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, "UPDATE items SET story_id = ? WHERE id = ?", id, it.ID); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// AttachItem adds an item to a story. Aggregator items (Google News) only carry
// a redirect link, so the first direct link to arrive replaces it, along with
// its title and snippet.
func (s *Store) AttachItem(ctx context.Context, it PendingItem, storyID int64, centroid []float32, isAggregator func(string) bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var url string
	if err := tx.QueryRowContext(ctx, "SELECT url FROM stories WHERE id = ?", storyID).Scan(&url); err != nil {
		return err
	}
	if isAggregator(url) && !isAggregator(it.URL) {
		if _, err := tx.ExecContext(ctx, "UPDATE stories SET url = ?, title = ?, snippet = ? WHERE id = ?",
			it.URL, it.Title, it.Snippet, storyID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE stories SET centroid = ?, n_items = n_items + 1, last_seen = ? WHERE id = ?",
		encodeVector(centroid), s.unix(), storyID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE items SET story_id = ? WHERE id = ?", storyID, it.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Scoring.

// UnscoredStories returns stories first seen after since that have no score yet.
func (s *Store) UnscoredStories(ctx context.Context, since time.Time, limit int) ([]Story, error) {
	return s.queryStories(ctx, "s.scored_at IS NULL AND s.first_seen >= ? ORDER BY s.id LIMIT ?", since.Unix(), limit)
}

// Scored is one story's result from the scorer.
type Scored struct {
	ID       int64
	LLMScore int
	Taste    *float64
	TasteN   int
	Score    float64
	Summary  string
	Reason   string
}

func (s *Store) SaveScores(ctx context.Context, scores []Scored) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.unix()
	for _, sc := range scores {
		if _, err := tx.ExecContext(ctx, `UPDATE stories SET llm_score = ?, taste = ?, taste_n = ?, score = ?,
			summary = ?, reason = ?, scored_at = ? WHERE id = ?`,
			sc.LLMScore, sc.Taste, sc.TasteN, sc.Score, sc.Summary, sc.Reason, now, sc.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Signal is a past reaction to a story, used for taste and policy learning.
type Signal struct {
	StoryID int64
	Topic   string
	Title   string
	Summary string
	Kind    SignalKind
	At      time.Time
	Vector  []float32
}

type SignalKind string

const (
	SignalLiked    SignalKind = "liked"
	SignalDisliked SignalKind = "disliked"
	SignalClicked  SignalKind = "clicked" // opened, no vote
	SignalIgnored  SignalKind = "ignored" // delivered, neither opened nor voted
)

// Signals returns reactions since the given time, newest first. A vote wins
// over a click, and a click over an ignore.
func (s *Store) Signals(ctx context.Context, since time.Time, limit int, withVectors bool) ([]Signal, error) {
	vec := "NULL"
	if withVectors {
		vec = "centroid"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, topic, title, COALESCE(summary, ''),
		CASE WHEN vote = 1 THEN 'liked' WHEN vote = -1 THEN 'disliked'
		     WHEN clicked_at IS NOT NULL THEN 'clicked' ELSE 'ignored' END,
		COALESCE(voted_at, clicked_at, ignored_at), `+vec+`
		FROM stories
		WHERE COALESCE(voted_at, clicked_at, ignored_at) >= ?
		ORDER BY COALESCE(voted_at, clicked_at, ignored_at) DESC LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Signal
	for rows.Next() {
		var sg Signal
		var at int64
		var b []byte
		if err := rows.Scan(&sg.StoryID, &sg.Topic, &sg.Title, &sg.Summary, &sg.Kind, &at, &b); err != nil {
			return nil, err
		}
		sg.At = time.Unix(at, 0)
		if b != nil {
			sg.Vector = decodeVector(b)
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// CountSignals counts reactions since the given time.
func (s *Store) CountSignals(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM stories WHERE COALESCE(voted_at, clicked_at, ignored_at) >= ?", since.Unix()).Scan(&n)
	return n, err
}

// Reactions.

// SetVote records a vote; 0 clears it.
func (s *Store) SetVote(ctx context.Context, id int64, vote int) error {
	var res sql.Result
	var err error
	if vote == 0 {
		res, err = s.DB.ExecContext(ctx, "UPDATE stories SET vote = NULL, voted_at = NULL WHERE id = ?", id)
	} else {
		res, err = s.DB.ExecContext(ctx, "UPDATE stories SET vote = ?, voted_at = ?, ignored_at = NULL WHERE id = ?", vote, s.unix(), id)
	}
	return affected(res, err)
}

// RecordClick marks a story as opened and returns its URL.
func (s *Store) RecordClick(ctx context.Context, id int64) (string, error) {
	var url string
	err := s.DB.QueryRowContext(ctx, `UPDATE stories SET clicked_at = COALESCE(clicked_at, ?), ignored_at = NULL
		WHERE id = ? RETURNING url`, s.unix(), id).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return url, err
}

// MarkIgnored flags stories delivered before the cutoff that were neither
// opened nor voted on. Returns how many were marked.
func (s *Store) MarkIgnored(ctx context.Context, deliveredBefore time.Time) (int, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE stories SET ignored_at = ?
		WHERE briefed_at IS NOT NULL AND briefed_at < ? AND briefed_at >= ?
		AND vote IS NULL AND clicked_at IS NULL AND ignored_at IS NULL`,
		s.unix(), deliveredBefore.Unix(), deliveredBefore.Add(-7*24*time.Hour).Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MergeStories moves every item of the dropped stories into keep, so keep
// shows all their sources, and marks the dropped stories as briefed. Dropped
// stories stay (without items) so reactions on them are not lost.
func (s *Store) MergeStories(ctx context.Context, keep int64, drop []int64) error {
	if len(drop) == 0 {
		return nil
	}
	b, _ := json.Marshal(drop)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE items SET story_id = ? WHERE story_id IN (SELECT value FROM json_each(?))", keep, string(b)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE stories SET n_items = (SELECT COUNT(*) FROM items WHERE story_id = ?) WHERE id = ?", keep, keep); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE stories SET n_items = 0, briefed_at = COALESCE(briefed_at, ?) WHERE id IN (SELECT value FROM json_each(?))", s.unix(), string(b)); err != nil {
		return err
	}
	return tx.Commit()
}

// Briefs and alerts.

// BriefCandidates are scored, never-briefed stories first seen after since, best first.
func (s *Store) BriefCandidates(ctx context.Context, since time.Time, minScore float64) ([]Story, error) {
	return s.queryStories(ctx, `s.briefed_at IS NULL AND s.score >= ? AND s.first_seen >= ?
		ORDER BY s.score DESC, s.n_items DESC, s.first_seen DESC LIMIT 300`, minScore, since.Unix())
}

// AlertCandidates are fresh, high-scoring stories not alerted yet.
func (s *Store) AlertCandidates(ctx context.Context, since time.Time, minScore float64, minSources int) ([]Story, error) {
	list, err := s.queryStories(ctx, `s.alerted_at IS NULL AND s.n_items > 0 AND s.score >= ? AND s.first_seen >= ?
		ORDER BY s.score DESC LIMIT 50`, minScore, since.Unix())
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, st := range list {
		if len(st.Sources) >= minSources {
			out = append(out, st)
		}
	}
	return out, nil
}

// MarkBriefed records that stories went out (or were dropped as duplicates) in a brief.
func (s *Store) MarkBriefed(ctx context.Context, ids []int64) error {
	return s.markStories(ctx, "briefed_at", ids)
}

func (s *Store) MarkAlerted(ctx context.Context, ids []int64) error {
	return s.markStories(ctx, "alerted_at", ids)
}

func (s *Store) markStories(ctx context.Context, column string, ids []int64) error {
	b, _ := json.Marshal(ids)
	_, err := s.DB.ExecContext(ctx, "UPDATE stories SET "+column+" = ? WHERE id IN (SELECT value FROM json_each(?))", s.unix(), string(b))
	return err
}

// Retention.

// Prune deletes items and stories last seen before the cutoff, except stories
// with a reaction, which the learner keeps using.
func (s *Store) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM stories WHERE last_seen < ?
		AND vote IS NULL AND clicked_at IS NULL AND ignored_at IS NULL`, before.Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM items WHERE seen_at < ? AND (story_id IS NULL OR story_id NOT IN (SELECT id FROM stories))", before.Unix()); err != nil {
		return n, err
	}
	_, err = s.DB.ExecContext(ctx, "PRAGMA incremental_vacuum")
	return n, err
}

// SignalCounts counts reactions since the given time, by kind.
func (s *Store) SignalCounts(ctx context.Context, since time.Time) (map[SignalKind]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT
		CASE WHEN vote = 1 THEN 'liked' WHEN vote = -1 THEN 'disliked'
		     WHEN clicked_at IS NOT NULL THEN 'clicked' ELSE 'ignored' END AS kind, COUNT(*)
		FROM stories WHERE COALESCE(voted_at, clicked_at, ignored_at) >= ? GROUP BY kind`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[SignalKind]int{}
	for rows.Next() {
		var k SignalKind
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// CountScored counts stories scored since the given time.
func (s *Store) CountScored(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM stories WHERE scored_at >= ?", since.Unix()).Scan(&n)
	return n, err
}
