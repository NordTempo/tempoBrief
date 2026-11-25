package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Brief is one sent (or attempted) brief.
type Brief struct {
	ID         int64
	ScheduleID string
	Title      string
	CreatedAt  time.Time
	StoryIDs   []int64
	Deliveries []Delivery
}

// Delivery is the outcome of sending a brief to one channel.
type Delivery struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

func (s *Store) AddBrief(ctx context.Context, b Brief) (int64, error) {
	ids, _ := json.Marshal(b.StoryIDs)
	del, _ := json.Marshal(b.Deliveries)
	res, err := s.DB.ExecContext(ctx, "INSERT INTO briefs(schedule_id, title, created_at, story_ids, deliveries) VALUES(?, ?, ?, ?, ?)",
		b.ScheduleID, b.Title, b.CreatedAt.Unix(), string(ids), string(del))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SetDeliveries(ctx context.Context, id int64, d []Delivery) error {
	b, _ := json.Marshal(d)
	_, err := s.DB.ExecContext(ctx, "UPDATE briefs SET deliveries = ? WHERE id = ?", string(b), id)
	return err
}

const briefColumns = "id, schedule_id, title, created_at, story_ids, deliveries"

func scanBrief(row interface{ Scan(...any) error }) (Brief, error) {
	var b Brief
	var at int64
	var ids, del string
	if err := row.Scan(&b.ID, &b.ScheduleID, &b.Title, &at, &ids, &del); err != nil {
		return b, err
	}
	b.CreatedAt = time.Unix(at, 0)
	if err := json.Unmarshal([]byte(ids), &b.StoryIDs); err != nil {
		return b, err
	}
	return b, json.Unmarshal([]byte(del), &b.Deliveries)
}

func (s *Store) Brief(ctx context.Context, id int64) (Brief, error) {
	b, err := scanBrief(s.DB.QueryRowContext(ctx, "SELECT "+briefColumns+" FROM briefs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// LatestBrief is the newest brief of any schedule.
func (s *Store) LatestBrief(ctx context.Context) (Brief, error) {
	b, err := scanBrief(s.DB.QueryRowContext(ctx, "SELECT "+briefColumns+" FROM briefs ORDER BY created_at DESC, id DESC LIMIT 1"))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// Briefs lists recent briefs, newest first.
func (s *Store) Briefs(ctx context.Context, limit int) ([]Brief, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+briefColumns+" FROM briefs ORDER BY created_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Brief
	for rows.Next() {
		b, err := scanBrief(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// LastBriefAt is when a schedule last produced a brief; zero if never.
func (s *Store) LastBriefAt(ctx context.Context, scheduleID string) (time.Time, error) {
	var at sql.NullInt64
	err := s.DB.QueryRowContext(ctx, "SELECT MAX(created_at) FROM briefs WHERE schedule_id = ?", scheduleID).Scan(&at)
	if err != nil || !at.Valid {
		return time.Time{}, err
	}
	return time.Unix(at.Int64, 0), nil
}

// Policy is one version of the editorial policy.
type Policy struct {
	ID        int64
	CreatedAt time.Time
	Source    string // "learned" or "edited"
	Text      string
	Changes   []string
	Signals   int
}

func (s *Store) AddPolicy(ctx context.Context, p Policy) (int64, error) {
	ch, _ := json.Marshal(p.Changes)
	if p.Changes == nil {
		ch = []byte("[]")
	}
	res, err := s.DB.ExecContext(ctx, "INSERT INTO policies(created_at, source, text, changes, signals) VALUES(?, ?, ?, ?, ?)",
		s.unix(), p.Source, p.Text, string(ch), p.Signals)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Policies lists policy versions, newest first.
func (s *Store) Policies(ctx context.Context, limit int) ([]Policy, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, created_at, source, text, changes, signals FROM policies ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Policy
	for rows.Next() {
		var p Policy
		var at int64
		var ch string
		if err := rows.Scan(&p.ID, &at, &p.Source, &p.Text, &ch, &p.Signals); err != nil {
			return nil, err
		}
		p.CreatedAt = time.Unix(at, 0)
		if err := json.Unmarshal([]byte(ch), &p.Changes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CurrentPolicy is the newest version; ErrNotFound before the first one.
func (s *Store) CurrentPolicy(ctx context.Context) (Policy, error) {
	list, err := s.Policies(ctx, 1)
	if err != nil {
		return Policy{}, err
	}
	if len(list) == 0 {
		return Policy{}, ErrNotFound
	}
	return list[0], nil
}
