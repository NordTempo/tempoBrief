package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NordTempo/tempoBrief/internal/auth"
	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store, settings.Settings) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Auth: config.AuthPassword, CookieSecure: config.CookieSecureFalse,
		Password: "test-password", PasswordEnv: true, PublicURL: "https://brief.test", Poll: 15 * time.Minute}
	a, err := auth.New(ctx, cfg, db, log)
	if err != nil {
		t.Fatal(err)
	}
	st, err := settings.Load(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	pipe := pipeline.New(pipeline.Deps{Config: cfg, DB: db, Log: log})
	srv := httptest.NewServer(New(Deps{Config: cfg, DB: db, Auth: a, Log: log, Assets: fstest.MapFS{},
		Pipeline: pipe}).Handler())
	t.Cleanup(srv.Close)
	return srv, db, st
}

func addStory(t *testing.T, db *store.Store) int64 {
	t.Helper()
	id, err := db.CreateStory(context.Background(), store.PendingItem{Topic: "Tech", Title: "A story", URL: "https://example.com/a"}, []float32{1})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func TestVoteLinkNeedsAConfirmingPost(t *testing.T) {
	srv, db, st := newTestServer(t)
	id := addStory(t, db)
	link := deliver.Links{Base: srv.URL, Secret: st.Secret}.Vote(id, 1)

	// A mail scanner opening the link must not vote.
	resp, err := http.Get(link)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: %v %v", resp.StatusCode, err)
	}
	if s, _ := db.Story(context.Background(), id); s.Vote != 0 {
		t.Fatal("GET must not record a vote")
	}
	resp, err = http.Post(link, "", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST: %v %v", resp.StatusCode, err)
	}
	if s, _ := db.Story(context.Background(), id); s.Vote != 1 {
		t.Fatalf("POST should record the vote, got %d", s.Vote)
	}
	// A down link signed for up is refused.
	bad := strings.Replace(link, "/up/", "/down/", 1)
	if resp, _ := http.Post(bad, "", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tampered link: got %d", resp.StatusCode)
	}
}

func TestClickRecordsAndRedirects(t *testing.T) {
	srv, db, _ := newTestServer(t)
	id := addStory(t, db)
	client := &http.Client{CheckRedirect: noRedirect}
	resp, err := client.Get(srv.URL + "/r/" + strconv.FormatInt(id, 10))
	if err != nil || resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://example.com/a" {
		t.Fatalf("got %v %v %q", resp.StatusCode, err, resp.Header.Get("Location"))
	}
	if s, _ := db.Story(context.Background(), id); !s.Clicked {
		t.Fatal("click not recorded")
	}
}

func TestPrivateRoutesNeedLoginAndReadRoutesNeedToken(t *testing.T) {
	srv, _, st := newTestServer(t)
	for path, want := range map[string]int{
		"/api/v1/settings":                  http.StatusUnauthorized,
		"/brief.json":                       http.StatusUnauthorized,
		"/brief.json?token=" + st.ReadToken: http.StatusOK,
		"/feed.xml?token=" + st.ReadToken:   http.StatusOK,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil || resp.StatusCode != want {
			t.Errorf("%s: got %v %v, want %d", path, resp.StatusCode, err, want)
		}
	}
}

func TestPublicPagesShowStoriesWithoutTheReadersReactions(t *testing.T) {
	srv, db, st := newTestServer(t)
	ctx := context.Background()
	id := addStory(t, db)
	taste := 0.8
	if err := db.SaveScores(ctx, []store.Scored{{ID: id, LLMScore: 7, Taste: &taste, TasteN: 3, Score: 8, Reason: "matches"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetVote(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *http.Response {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Off by default: reading needs a session.
	if resp := get("/api/v1/stories?min=0"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public pages off: got %d", resp.StatusCode)
	}

	st.PublicPages = true
	if err := settings.Save(ctx, db, st); err != nil {
		t.Fatal(err)
	}
	resp := get("/api/v1/stories?min=0")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"title":"A story"`) {
		t.Fatalf("public stories: %d %s", resp.StatusCode, body)
	}
	for _, want := range []string{`"vote":0`, `"taste":null`, `"tasteN":0`, `"why":"matches"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("a visitor must not see the reader's reactions: want %s in %s", want, body)
		}
	}

	// Everything else still needs a session.
	for _, path := range []string{"/api/v1/settings", "/api/v1/learning", "/api/v1/feeds", "/api/v1/status"} {
		if resp := get(path); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: got %d", path, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/stories/"+strconv.FormatInt(id, 10)+"/vote", strings.NewReader(`{"vote":-1}`))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous vote: %v %v", resp.StatusCode, err)
	}
}
