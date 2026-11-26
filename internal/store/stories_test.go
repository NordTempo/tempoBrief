package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListStoriesSortsByScoreAgeOrOutlets(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.ExecContext(ctx, "INSERT INTO feeds(id, url, created_at) VALUES(1, 'https://example.com/feed', 0)"); err != nil {
		t.Fatal(err)
	}

	// best: high score, seen 3 h ago, 1 outlet. fresh: seen just now.
	// covered: 3 outlets.
	now := time.Now()
	seed := []struct {
		title   string
		score   float64
		age     time.Duration
		outlets []string
	}{
		{"best", 9, 3 * time.Hour, []string{"A"}},
		{"fresh", 6, 0, []string{"A"}},
		{"covered", 7, 2 * time.Hour, []string{"A", "B", "C"}},
	}
	for i, s := range seed {
		id, err := db.CreateStory(ctx, PendingItem{Topic: "Tech", Title: s.title, URL: "https://example.com/" + s.title}, []float32{1})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SaveScores(ctx, []Scored{{ID: id, LLMScore: int(s.score), Score: s.score}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.ExecContext(ctx, "UPDATE stories SET first_seen = ? WHERE id = ?", now.Add(-s.age).Unix(), id); err != nil {
			t.Fatal(err)
		}
		for j, src := range s.outlets {
			if _, err := db.DB.ExecContext(ctx, `INSERT INTO items(feed_id, guid, url, title, snippet, source, published_at, seen_at, story_id)
				VALUES(1, ?, '', '', '', ?, 0, 0, ?)`, i*10+j, src, id); err != nil {
				t.Fatal(err)
			}
		}
	}

	for sort, want := range map[string][]string{
		"":          {"best", "covered", "fresh"},
		SortNewest:  {"fresh", "covered", "best"},
		SortOutlets: {"covered", "best", "fresh"},
	} {
		list, err := db.ListStories(ctx, StoryFilter{Since: now.Add(-24 * time.Hour), Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range list {
			got = append(got, s.Title)
		}
		if !slices.Equal(got, want) {
			t.Errorf("sort %q: got %v, want %v", sort, got, want)
		}
	}
}
