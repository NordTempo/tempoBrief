package store

import (
	"context"
	"net/url"
	"strings"
)

// Missed stories: ones the reader heard about elsewhere and reported, because
// tempoBrief should have shown them. See docs/learning.md#missed.

// SetMissed records a miss on a story; note is the reader's reason, if any.
func (s *Store) SetMissed(ctx context.Context, id int64, note string) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE stories SET missed_at = ?, missed_note = ?, ignored_at = NULL WHERE id = ?",
		s.unix(), strings.TrimSpace(note), id)
	return affected(res, err)
}

// ClearMissed takes a miss back.
func (s *Store) ClearMissed(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE stories SET missed_at = NULL, missed_note = NULL WHERE id = ?", id)
	return affected(res, err)
}

// MissedStory is an article no feed carried, stored so the reader's report
// still teaches taste and policy. It has no items (n_items 0), so it stays out
// of story lists, clustering and scoring.
type MissedStory struct {
	Topic, Title, URL, Snippet, Note string
	Vector                           []float32
}

func (s *Store) AddMissedStory(ctx context.Context, m MissedStory) (int64, error) {
	now := s.unix()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO stories(topic, title, url, snippet, centroid, n_items,
		first_seen, last_seen, scored_at, missed_at, missed_note) VALUES(?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		m.Topic, m.Title, m.URL, m.Snippet, encodeVector(m.Vector), now, now, now, now, strings.TrimSpace(m.Note))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// StoryByURL finds the story an article link belongs to: through its items
// first (a merged story's items moved to the story that kept them), then
// stories' own links, including missed stories no feed had. Links are
// compared without scheme, "www.", query, fragment and trailing slash.
func (s *Store) StoryByURL(ctx context.Context, link string) (Story, error) {
	key := URLKey(link)
	u, err := url.Parse(strings.TrimSpace(link))
	if key == "" || err != nil {
		return Story{}, ErrNotFound
	}
	// Narrow with LIKE on the most specific part, then compare exactly.
	part := strings.TrimSuffix(u.Path, "/")
	if len(part) < 2 {
		part = u.Host
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(part) + "%"
	rows, err := s.DB.QueryContext(ctx, `SELECT story_id, url FROM items WHERE story_id IS NOT NULL AND url LIKE ? ESCAPE '\'
		UNION ALL SELECT id, url FROM stories WHERE (n_items > 0 OR missed_at IS NOT NULL) AND url LIKE ? ESCAPE '\'`, like, like)
	if err != nil {
		return Story{}, err
	}
	defer rows.Close()
	var id int64
	for rows.Next() {
		var sid int64
		var u string
		if err := rows.Scan(&sid, &u); err != nil {
			return Story{}, err
		}
		if URLKey(u) == key {
			id = sid
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Story{}, err
	}
	rows.Close()
	if id == 0 {
		return Story{}, ErrNotFound
	}
	return s.Story(ctx, id)
}

// URLKey is a link reduced to what identifies the article: host without
// "www." and path without a trailing slash. Empty for anything but http(s).
func URLKey(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	return host + strings.TrimSuffix(u.EscapedPath(), "/")
}

// MissedStories lists missed stories, newest first.
func (s *Store) MissedStories(ctx context.Context, limit int) ([]Story, error) {
	return s.queryStories(ctx, "s.missed_at IS NOT NULL ORDER BY s.missed_at DESC LIMIT ?", limit)
}
