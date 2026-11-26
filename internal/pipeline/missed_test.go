package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func TestReportingAMissedStorySaysWhyAndTeachesTaste(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.Tick(ctx) // rate story 8, football 3, Claude 8; the brief is at 07:00

	// An outside site whose articles link its feed. "Strike" embeds like
	// nothing tempoBrief has.
	var site *httptest.Server
	site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rss.xml":
			fmt.Fprintf(w, rssDoc, "Avisa", item("Strike closes Oslo harbour", site.URL+"/strike", h.now))
		case "/strike", "/rates":
			title := map[string]string{"/strike": "Strike closes Oslo harbour", "/rates": "Why the rate stayed put"}[r.URL.Path]
			fmt.Fprintf(w, `<html><head><title>ignored</title><meta property="og:title" content="%s">
				<meta property="og:site_name" content="Avisa"><link rel="alternate" type="application/rss+xml" href="/rss.xml" title="Avisa">
				</head><body>text</body></html>`, title)
		default:
			fmt.Fprint(w, `<html><head><title>Avisa</title></head></html>`)
		}
	}))
	t.Cleanup(site.Close)
	// A site with no feed at all; Google News has its articles.
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ferry" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<html><head><title>Ferry strike ends | Bladet</title><meta property="og:title" content="Ferry strike ends"></head></html>`)
	}))
	t.Cleanup(bare.Close)
	news := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("q"), "site:127.0.0.1") {
			t.Errorf("news search: %s", r.URL)
		}
		fmt.Fprintf(w, rssDoc, "Google News", item("Ferry strike ends - Bladet", "https://news.google.com/rss/articles/x", h.now))
	}))
	t.Cleanup(news.Close)
	h.p.Fetcher.NewsSearch = news.URL

	report := func(link, note string) MissedReport {
		t.Helper()
		rep, err := h.p.ReportMissed(ctx, link, note)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if !rep.Story.Missed {
			t.Fatalf("%s: not recorded as missed", link)
		}
		return rep
	}
	for _, c := range []struct {
		link string
		want MissedCase
		line string
	}{
		// Found by link, though written with a trailing slash.
		{"https://nrk.no/2/", MissedLowScore, "Scored 3.0. The model said: \"fits\". The morning brief needs 6."},
		{"https://hn/1", MissedQueued, "in line for the next morning brief"},
		// Not by link, but by its title.
		{site.URL + "/rates", MissedQueued, "Scored 8.0"},
		// No feed had it: tempoBrief adds the site's feed, which carries it.
		{site.URL + "/strike", MissedNotFetched, "None of your feeds had this. Added Avisa to General: it carries this article"},
		// No feed anywhere: a Google News search for the site.
		{bare.URL + "/ferry", MissedNotFetched, "Added Google News: 127.0.0.1"},
	} {
		rep := report(c.link, "")
		if rep.Case != c.want || !strings.Contains(rep.Lines[0], c.line) {
			t.Errorf("%s: got %s %q, want %s %q", c.link, rep.Case, rep.Lines, c.want, c.line)
		}
		if c.want != MissedNotFetched && rep.Lines[len(rep.Lines)-1] != "Alerts are off." {
			t.Errorf("%s: want the alert line, got %q", c.link, rep.Lines)
		}
	}

	// Reported again, it is the same missed story, and the feed is not added twice.
	strike := report(site.URL+"/strike", "Anything that stops Oslo traffic")
	if feeds, _ := h.db.Feeds(ctx); len(feeds) != 4 {
		t.Errorf("want the two feeds plus Avisa and the news search, got %d", len(feeds))
	}

	// After the brief went out, a story in it was "missed" only by the reader.
	h.at(h.now.Add(2 * time.Hour))
	h.p.Tick(ctx)
	if rep := report("https://hn/1", ""); rep.Case != MissedBriefed {
		t.Errorf("after the brief: %s %q", rep.Case, rep.Lines)
	}

	// The miss teaches taste: a new story like the strike article ranks up.
	signals, err := h.db.Signals(ctx, h.now.Add(-time.Hour*24), 100, true)
	if err != nil {
		t.Fatal(err)
	}
	vec, _ := fakeEmbedder{}.Embed(ctx, []string{"Harbour strike spreads"})
	if taste := Taste(vec[0], signals); taste.Adjustment <= 0 {
		t.Errorf("a story like a missed one should rank up, got %+v", taste)
	}
	// The scorer and the learner read the reader's words.
	s, _ := settings.Load(ctx, h.db)
	if prompt, _ := h.p.scoringPrompt(ctx, s); !strings.Contains(prompt, "Strike closes Oslo harbour | reader: Anything that stops Oslo traffic") {
		t.Errorf("scoring prompt lacks the miss:\n%s", prompt)
	}
	// Each miss counts as three reactions toward the next policy rewrite.
	if n, _ := h.db.CountSignals(ctx, time.Time{}); n < 4*3 {
		t.Errorf("want misses to count triple, got %d", n)
	}

	if err := h.db.ClearMissed(ctx, strike.Story.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.db.Story(ctx, strike.Story.ID); st.Missed {
		t.Error("undo left the miss")
	}
	if _, err := h.db.StoryByURL(ctx, "https://nrk.no/nothing"); err != store.ErrNotFound {
		t.Errorf("unknown link: %v", err)
	}
}
