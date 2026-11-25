package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Feed struct {
	ID           int64
	URL          string
	Title        string
	Topic        string
	SiteURL      string
	Enabled      bool
	ETag         string
	LastModified string
	NextFetchAt  time.Time
	LastFetchAt  time.Time
	LastOKAt     time.Time
	LastError    string
	ErrorCount   int
	ItemCount    int
}

const feedColumns = `id, url, title, topic, site_url, enabled, etag, last_modified,
	next_fetch_at, last_fetch_at, last_ok_at, last_error, error_count, item_count`

func scanFeed(row interface{ Scan(...any) error }) (Feed, error) {
	var f Feed
	var next, last, ok int64
	err := row.Scan(&f.ID, &f.URL, &f.Title, &f.Topic, &f.SiteURL, &f.Enabled, &f.ETag, &f.LastModified,
		&next, &last, &ok, &f.LastError, &f.ErrorCount, &f.ItemCount)
	f.NextFetchAt, f.LastFetchAt, f.LastOKAt = unixOrZero(next), unixOrZero(last), unixOrZero(ok)
	return f, err
}

func unixOrZero(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func (s *Store) Feeds(ctx context.Context) ([]Feed, error) {
	return s.queryFeeds(ctx, "SELECT "+feedColumns+" FROM feeds ORDER BY topic, title, url")
}

// DueFeeds are enabled feeds whose next fetch time has passed.
func (s *Store) DueFeeds(ctx context.Context, now time.Time) ([]Feed, error) {
	return s.queryFeeds(ctx, "SELECT "+feedColumns+" FROM feeds WHERE enabled = 1 AND next_fetch_at <= ? ORDER BY next_fetch_at", now.Unix())
}

func (s *Store) queryFeeds(ctx context.Context, query string, args ...any) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Feed(ctx context.Context, id int64) (Feed, error) {
	f, err := scanFeed(s.DB.QueryRowContext(ctx, "SELECT "+feedColumns+" FROM feeds WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// NewFeed is what a user (or an OPML import) adds.
type NewFeed struct {
	URL   string
	Title string
	Topic string
}

// AddFeeds inserts feeds, skipping URLs that already exist. Returns how many were added.
func (s *Store) AddFeeds(ctx context.Context, feeds []NewFeed) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, f := range feeds {
		topic := strings.TrimSpace(f.Topic)
		if topic == "" {
			topic = "General"
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO feeds(url, title, topic, created_at) VALUES(?, ?, ?, ?) ON CONFLICT(url) DO NOTHING",
			strings.TrimSpace(f.URL), strings.TrimSpace(f.Title), topic, s.unix())
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return added, tx.Commit()
}

// FeedUpdate is the user-editable part of a feed.
type FeedUpdate struct {
	Title   string
	Topic   string
	Enabled bool
}

func (s *Store) UpdateFeed(ctx context.Context, id int64, u FeedUpdate) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE feeds SET title = ?, topic = ?, enabled = ? WHERE id = ?",
		strings.TrimSpace(u.Title), strings.TrimSpace(u.Topic), u.Enabled, id)
	return affected(res, err)
}

func (s *Store) DeleteFeed(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM feeds WHERE id = ?", id)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FetchResult records one fetch attempt.
type FetchResult struct {
	At           time.Time
	Next         time.Time
	OK           bool
	NotModified  bool
	Error        string
	ETag         string
	LastModified string
	Title        string // feed title from the document; only fills an empty title
	SiteURL      string
}

func (s *Store) RecordFetch(ctx context.Context, id int64, r FetchResult) error {
	if !r.OK {
		_, err := s.DB.ExecContext(ctx,
			"UPDATE feeds SET last_fetch_at = ?, next_fetch_at = ?, last_error = ?, error_count = error_count + 1 WHERE id = ?",
			r.At.Unix(), r.Next.Unix(), r.Error, id)
		return err
	}
	if r.NotModified {
		_, err := s.DB.ExecContext(ctx,
			"UPDATE feeds SET last_fetch_at = ?, next_fetch_at = ?, last_ok_at = ?, last_error = '', error_count = 0 WHERE id = ?",
			r.At.Unix(), r.Next.Unix(), r.At.Unix(), id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE feeds SET last_fetch_at = ?, next_fetch_at = ?, last_ok_at = ?,
		last_error = '', error_count = 0, etag = ?, last_modified = ?,
		title = CASE WHEN title = '' THEN ? ELSE title END,
		site_url = CASE WHEN ? != '' THEN ? ELSE site_url END
		WHERE id = ?`,
		r.At.Unix(), r.Next.Unix(), r.At.Unix(), r.ETag, r.LastModified, r.Title, r.SiteURL, r.SiteURL, id)
	return err
}

// NewItem is one parsed feed entry.
type NewItem struct {
	GUID        string
	URL         string
	Title       string
	Snippet     string
	Source      string
	PublishedAt time.Time
}

// AddItems stores entries not seen before for this feed. Returns how many were new.
func (s *Store) AddItems(ctx context.Context, feedID int64, items []NewItem) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO items(feed_id, guid, url, title, snippet, source, published_at, seen_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(feed_id, guid) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := 0
	now := s.unix()
	for _, it := range items {
		res, err := stmt.ExecContext(ctx, feedID, it.GUID, it.URL, it.Title, it.Snippet, it.Source, it.PublishedAt.Unix(), now)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	if added > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE feeds SET item_count = item_count + ? WHERE id = ?", added, feedID); err != nil {
			return 0, err
		}
	}
	return added, tx.Commit()
}
