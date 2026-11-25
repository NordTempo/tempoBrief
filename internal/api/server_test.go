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
