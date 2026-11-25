package feeds

import (
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func TestItemsUsesAggregatorPublisher(t *testing.T) {
	parsed := &gofeed.Feed{Title: "Google News", Items: []*gofeed.Item{
		{Title: "Norges Bank holder renten - E24", Link: "https://news.google.com/rss/articles/abc", GUID: "g1"},
		{Title: "Plain item", Link: "https://nrk.no/a", Description: "<p>Some <b>bold</b> &amp; text</p>"},
		{Title: "", Link: "https://nrk.no/empty"},
	}}
	items := Items(parsed, "", time.Now())
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].Source != "E24" || items[0].Title != "Norges Bank holder renten" {
		t.Fatalf("publisher not split from title: %+v", items[0])
	}
	if items[1].Source != "Google News" || items[1].Snippet != "Some bold & text" || items[1].GUID != "https://nrk.no/a" {
		t.Fatalf("unexpected plain item: %+v", items[1])
	}
}

func TestSnippetCutsAtWord(t *testing.T) {
	got := Snippet(strings.Repeat("word ", 50), 23)
	if got != "word word word word…" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOPMLTopics(t *testing.T) {
	doc := `<opml version="2.0"><body>
	<outline text="Norge"><outline text="NRK" xmlUrl="https://nrk.no/toppsaker.rss"/></outline>
	<outline text="Loose" xmlUrl="https://example.com/feed"/>
	</body></opml>`
	feeds, err := ParseOPML(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 || feeds[0].Topic != "Norge" || feeds[0].Title != "NRK" || feeds[1].Topic != "" {
		t.Fatalf("unexpected feeds %+v", feeds)
	}
}

func TestNextFetchBacksOff(t *testing.T) {
	now := time.Unix(0, 0)
	if got := NextFetch(now, 15*time.Minute, 0).Sub(now); got != 15*time.Minute {
		t.Fatalf("healthy: got %s", got)
	}
	if got := NextFetch(now, 15*time.Minute, 2).Sub(now); got != time.Hour {
		t.Fatalf("two errors: got %s", got)
	}
	if got := NextFetch(now, 15*time.Minute, 9).Sub(now); got != 6*time.Hour {
		t.Fatalf("capped: got %s", got)
	}
}
