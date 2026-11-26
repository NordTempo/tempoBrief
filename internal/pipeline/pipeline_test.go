package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// fakeEmbedder maps each title to a vector by keyword, so tests control which
// items count as the same story.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 4)
		switch {
		case strings.Contains(t, "rate"), strings.Contains(t, "renten"):
			v[0] = 1
		case strings.Contains(t, "Claude"):
			v[1] = 1
		case strings.Contains(t, "football"):
			v[2] = 1
		default:
			v[3] = 1
		}
		out[i] = v
	}
	return out, nil
}

// fakeModel answers each kind of prompt the pipeline sends.
type fakeModel struct {
	mu      sync.Mutex
	prompts []string
}

var (
	idRe      = regexp.MustCompile(`id=(\d+) \| \S+ \| \d+ outlet\(s\): [^|]* \| ([^|]+) \|`)
	earlierRe = regexp.MustCompile(`earlier id=(\d+) \| ([^|]+) \|`)
)

func (m *fakeModel) Name() string { return "fake/model" }

func (m *fakeModel) JSON(_ context.Context, req llm.Request) ([]byte, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, req.System)
	m.mu.Unlock()
	switch {
	case strings.Contains(req.System, "pick the news"):
		type entry struct {
			ID      int64  `json:"id"`
			Score   int    `json:"score"`
			Summary string `json:"summary"`
			Reason  string `json:"reason"`
			SameAs  int64  `json:"sameAs"`
		}
		var out struct {
			Stories []entry `json:"stories"`
		}
		// Stories about "Rust" are one event: each points at the oldest one listed.
		var rust int64
		for _, m := range append(earlierRe.FindAllStringSubmatch(req.Prompt, -1), idRe.FindAllStringSubmatch(req.Prompt, -1)...) {
			var id int64
			fmt.Sscan(m[1], &id)
			if strings.Contains(m[2], "Rust") && (rust == 0 || id < rust) {
				rust = id
			}
		}
		for _, match := range idRe.FindAllStringSubmatch(req.Prompt, -1) {
			var id int64
			fmt.Sscan(match[1], &id)
			score := 3
			if strings.Contains(match[2], "rate") || strings.Contains(match[2], "renten") || strings.Contains(match[2], "Claude") {
				score = 8
			}
			var same int64
			if strings.Contains(match[2], "Rust") && rust != id {
				same = rust
			}
			out.Stories = append(out.Stories, entry{id, score, "Summary of " + strings.TrimSpace(match[2]), "fits", same})
		}
		return json.Marshal(out)
	case strings.Contains(req.System, "same event"):
		// Stories mentioning "Norse" are one event, whatever their wording.
		var group []int64
		for _, line := range strings.Split(req.Prompt, "\n") {
			var id int64
			if strings.Contains(line, "Norse") && func() bool { _, err := fmt.Sscanf(line, "id=%d", &id); return err == nil }() {
				group = append(group, id)
			}
		}
		if len(group) < 2 {
			return []byte(`{"groups":[]}`), nil
		}
		return json.Marshal(map[string][][]int64{"groups": {group}})
	case strings.Contains(req.System, "editorial policy"):
		return []byte(`{"policy":"- Rank AI coding tools up","changes":["Added AI coding tools"]}`), nil
	}
	return nil, fmt.Errorf("unexpected prompt")
}

type fakeSender struct {
	mu   sync.Mutex
	sent []deliver.Message
}

func (s *fakeSender) Send(_ context.Context, _ settings.Channel, m deliver.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

const rssDoc = `<?xml version="1.0"?><rss version="2.0"><channel><title>%s</title>%s</channel></rss>`

func item(title, link string, pub time.Time) string {
	return fmt.Sprintf(`<item><title>%s</title><link>%s</link><guid>%s</guid><pubDate>%s</pubDate><description>About %s</description></item>`,
		title, link, link, pub.Format(time.RFC1123Z), title)
}

type harness struct {
	p      *Pipeline
	db     *store.Store
	model  *fakeModel
	sender *fakeSender
	now    time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	// 06:00 in Oslo: the morning brief at 07:00 is still ahead.
	now := time.Date(2026, 11, 25, 5, 0, 0, 0, time.UTC)
	nrk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, rssDoc, "NRK",
			item("Norges Bank holder renten", "https://nrk.no/1", now.Add(-time.Hour))+
				item("Lokal football result", "https://nrk.no/2", now.Add(-time.Hour))+
				item("Old news", "https://nrk.no/old", now.Add(-72*time.Hour)))
	}))
	t.Cleanup(nrk.Close)
	hn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, rssDoc, "Hacker News",
			item("Central bank keeps rate", "https://e24.no/1", now.Add(-2*time.Hour))+
				item("Claude Code ships background agents", "https://hn/1", now.Add(-time.Hour)))
	}))
	t.Cleanup(hn.Close)

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetClock(func() time.Time { return now })
	if _, err := db.AddFeeds(ctx, []store.NewFeed{{URL: nrk.URL, Topic: "Norway"}, {URL: hn.URL, Topic: "Tech"}}); err != nil {
		t.Fatal(err)
	}
	s, err := settings.Load(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	s.Interests = "Norwegian economy and AI coding tools."
	s.Channels = []settings.Channel{{ID: "hook", Kind: settings.ChannelWebhook, Name: "Hook", Enabled: true, WebhookURL: "https://example.com"}}
	if err := settings.Save(ctx, db, s); err != nil {
		t.Fatal(err)
	}

	h := &harness{db: db, model: &fakeModel{}, sender: &fakeSender{}, now: now}
	h.p = New(Deps{
		Config:   config.Config{Poll: 15 * time.Minute, PublicURL: "https://brief.test"},
		DB:       db,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Fetcher:  feeds.NewFetcher("test"),
		Embedder: fakeEmbedder{},
		Sender:   h.sender,
		NewModel: func(settings.Provider) (llm.Model, error) { return h.model, nil },
		Now:      func() time.Time { return h.now },
	})
	h.p.scoreEvery = 0 // most tests add a story and expect the next tick to score it
	db.SetMetaTime(ctx, "installed_at", now.Add(-time.Hour))
	return h
}

func (h *harness) at(t time.Time) {
	h.now = t
	h.db.SetClock(func() time.Time { return t })
}

func TestTickFetchesClustersScoresAndBriefs(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	h.p.Tick(ctx)
	for _, s := range h.p.Status().Steps {
		if s.LastError != "" {
			t.Fatalf("step %s failed: %s", s.Name, s.LastError)
		}
	}
	stories, err := h.db.ListStories(ctx, store.StoryFilter{Since: h.now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// Four fresh items: the two rate headlines are one story, the old item is dropped.
	if len(stories) != 3 {
		t.Fatalf("want 3 stories, got %d: %+v", len(stories), stories)
	}
	top := stories[0]
	if !strings.Contains(top.Title, "renten") && !strings.Contains(top.Title, "rate") {
		t.Fatalf("top story should be the interest rate, got %q", top.Title)
	}
	if top.NItems != 2 || len(top.Sources) != 2 {
		t.Fatalf("rate story should merge two outlets, got %d items, sources %v", top.NItems, top.Sources)
	}
	if len(h.sender.sent) != 0 {
		t.Fatal("no brief before 07:00")
	}

	// 07:01 Oslo time: the morning brief goes out with the two stories above 6.
	h.at(time.Date(2026, 11, 25, 6, 1, 0, 0, time.UTC))
	h.p.Tick(ctx)
	if len(h.sender.sent) != 1 {
		t.Fatalf("want one brief, got %d", len(h.sender.sent))
	}
	msg := h.sender.sent[0]
	if got := len(msg.Stories()); got != 2 {
		t.Fatalf("want 2 stories in the brief, got %d", got)
	}
	if !strings.HasPrefix(msg.Stories()[0].URL, "https://brief.test/r/") || msg.Stories()[0].UpURL == "" {
		t.Fatalf("deliveries should use tracked and vote links, got %+v", msg.Stories()[0])
	}
	if msg.BriefURL == "" {
		t.Fatal("brief should link to the web app")
	}

	// A second tick the same morning sends nothing more.
	h.at(h.now.Add(time.Minute))
	h.p.Tick(ctx)
	if len(h.sender.sent) != 1 {
		t.Fatalf("brief sent twice")
	}
}

func TestTasteMovesSimilarStories(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.Tick(ctx)
	stories, _ := h.db.ListStories(ctx, store.StoryFilter{Since: h.now.Add(-24 * time.Hour)})
	var claude store.Story
	for _, st := range stories {
		if strings.Contains(st.Title, "Claude") {
			claude = st
		}
	}
	if err := h.db.SetVote(ctx, claude.ID, -1); err != nil {
		t.Fatal(err)
	}

	// A new story with the same vector as the disliked one scores lower than the model's 8.
	id, err := h.db.CreateStory(ctx, store.PendingItem{Topic: "Tech", Title: "Claude adds memory", URL: "https://x/1"}, []float32{0, 1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	h.p.Tick(ctx)
	st, err := h.db.Story(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Score == nil || st.LLMScore == nil || *st.Score >= float64(*st.LLMScore) {
		t.Fatalf("taste should lower the score: llm %v, final %v", st.LLMScore, st.Score)
	}
	if !strings.Contains(Why(st), "passed on") {
		t.Fatalf("why should mention the taste, got %q", Why(st))
	}
}

func TestLearnWritesPolicyUsedByScoring(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.Tick(ctx)
	stories, _ := h.db.ListStories(ctx, store.StoryFilter{Since: h.now.Add(-24 * time.Hour)})
	h.db.SetVote(ctx, stories[0].ID, 1)

	p, err := h.p.LearnNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Text == "" || p.Signals != 1 {
		t.Fatalf("unexpected policy %+v", p)
	}
	prompt, err := h.p.scoringPrompt(ctx, mustSettings(t, h.db))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Rank AI coding tools up") {
		t.Fatal("scoring prompt should include the learned policy")
	}
}

func TestBriefMergesDuplicateStories(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.Tick(ctx)
	// Two stories about one event that grouping left apart.
	a, _ := h.db.CreateStory(ctx, store.PendingItem{Topic: "Norway", Title: "Norse rate crisis", URL: "https://nrk.no/n", Source: "NRK"}, []float32{0.6, 0.8, 0, 0})
	b, _ := h.db.CreateStory(ctx, store.PendingItem{Topic: "Norway", Title: "Norse rate warning", URL: "https://e24.no/n", Source: "E24"}, []float32{0.8, 0.6, 0, 0})
	h.p.Tick(ctx) // score them
	h.db.DB.Exec("INSERT INTO items(feed_id, guid, url, title, snippet, source, published_at, seen_at, story_id) VALUES(1, 'n1', 'https://nrk.no/n', 'x', '', 'NRK', 0, 0, ?), (1, 'e1', 'https://e24.no/n', 'x', '', 'E24', 0, 0, ?)", a, b)

	id, err := h.p.SendBriefNow(ctx, "morning")
	if err != nil {
		t.Fatal(err)
	}
	brief, _ := h.db.Brief(ctx, id)
	stories, _ := h.db.StoriesByID(ctx, brief.StoryIDs)
	var norse []store.Story
	for _, st := range stories {
		if strings.Contains(st.Title, "Norse") {
			norse = append(norse, st)
		}
	}
	if len(norse) != 1 || len(norse[0].Sources) != 2 {
		t.Fatalf("want one merged Norse story with both outlets, got %+v", norse)
	}
}

func TestScoringMergesALaterStoryAboutTheSameEvent(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.Tick(ctx)
	withItem := func(title, source string, vec []float32) int64 {
		id, _ := h.db.CreateStory(ctx, store.PendingItem{Topic: "Tech", Title: title, URL: "https://x/" + source, Source: source}, vec)
		h.db.DB.Exec("INSERT INTO items(feed_id, guid, url, title, snippet, source, published_at, seen_at, story_id) VALUES(1, ?, ?, ?, '', ?, 0, 0, ?)",
			source, "https://x/"+source, title, source, id)
		return id
	}
	// Close enough to be shown to the model, too far apart for grouping to join them.
	first := withItem("Migrating our Messages API to Rust", "Anthropic", []float32{0, 0, 0.6, 0.8})
	h.p.Tick(ctx)
	later := withItem("Anthropic moves its Claude API to Rust", "InfoWorld", []float32{0, 0, 0.5, 0.866})
	h.p.Tick(ctx)

	stories, _ := h.db.ListStories(ctx, store.StoryFilter{Since: h.now.Add(-24 * time.Hour)})
	var rust []store.Story
	for _, st := range stories {
		if strings.Contains(st.Title, "Rust") {
			rust = append(rust, st)
		}
	}
	if len(rust) != 1 || rust[0].ID != first || len(rust[0].Sources) != 2 {
		t.Fatalf("want story %d alone, with both outlets; got %+v (later was %d)", first, rust, later)
	}
}

func mustSettings(t *testing.T, db *store.Store) settings.Settings {
	t.Helper()
	s, err := settings.Load(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// limitedModel is a provider out of requests until a set time.
type limitedModel struct {
	calls int
	until time.Time
}

func (m *limitedModel) Name() string { return "fake/free" }
func (m *limitedModel) JSON(context.Context, llm.Request) ([]byte, error) {
	m.calls++
	return nil, &llm.RateLimited{Model: m.Name(), Until: m.until}
}

func TestOutOfRequestsPausesTheModelUntilReset(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	limited := &limitedModel{until: h.now.Add(3 * time.Hour)}
	h.p.NewModel = func(settings.Provider) (llm.Model, error) { return limited, nil }

	h.p.Tick(ctx)
	for range 5 {
		h.at(h.now.Add(time.Minute))
		h.p.Tick(ctx)
	}
	if limited.calls != 1 {
		t.Fatalf("want one call until the reset, got %d", limited.calls)
	}
	for _, s := range h.p.Status().Steps {
		if s.Name == "score" && !strings.Contains(s.LastError, "out of requests until") {
			t.Fatalf("score status should say it is paused, got %q", s.LastError)
		}
	}

	h.p.NewModel = func(settings.Provider) (llm.Model, error) { return h.model, nil }
	h.at(limited.until.Add(time.Minute))
	h.p.Tick(ctx)
	if n, err := h.db.CountScored(ctx, h.now.Add(-24*time.Hour)); err != nil || n == 0 {
		t.Fatalf("scoring should resume after the reset, scored %d, err %v", n, err)
	}
}

func TestSmallBatchesWaitForTheNextScoringRound(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.p.scoreEvery = 30 * time.Minute
	h.p.Tick(ctx) // the first small batch goes right away

	id, err := h.db.CreateStory(ctx, store.PendingItem{Topic: "Tech", Title: "Claude adds memory", URL: "https://x/1"}, []float32{0, 1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	h.at(h.now.Add(time.Minute))
	h.p.Tick(ctx)
	if st, _ := h.db.Story(ctx, id); st.Score != nil {
		t.Fatal("one new story should wait for the next round")
	}
	h.at(h.now.Add(30 * time.Minute))
	h.p.Tick(ctx)
	if st, _ := h.db.Story(ctx, id); st.Score == nil {
		t.Fatal("the story should be scored after scoreEvery")
	}
}
