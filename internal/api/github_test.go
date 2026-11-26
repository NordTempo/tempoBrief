package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NordTempo/tempoBrief/internal/auth"
	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// fakeGitHub answers the three calls the flow makes. The app is owned by user
// 42; the code "owner" signs in as 42 and "stranger" as 7.
func fakeGitHub(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"client_id": "cid", "client_secret": "csecret",
			"html_url": "https://github.com/apps/x", "owner": map[string]any{"id": 42, "login": "reader"}})
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_secret") != "csecret" || r.Form.Get("code_verifier") == "" {
			t.Errorf("token exchange: %v", r.Form)
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok-" + r.Form.Get("code")})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		id := map[string]int{"Bearer tok-owner": 42, "Bearer tok-stranger": 7}[r.Header.Get("Authorization")]
		json.NewEncoder(w).Encode(map[string]int{"id": id})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSignInWithGitHubLetsOnlyTheAppOwnerIn(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Auth: config.AuthPassword, CookieSecure: config.CookieSecureFalse,
		Password: "test-password", PasswordEnv: true, Poll: 15 * time.Minute}
	a, err := auth.New(ctx, cfg, db, log)
	if err != nil {
		t.Fatal(err)
	}
	gh := fakeGitHub(t)
	s := New(Deps{Config: cfg, DB: db, Auth: a, Log: log, Assets: fstest.MapFS{}})
	s.github = githubEndpoints{Web: gh.URL, API: gh.URL}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	s.Config.PublicURL = srv.URL

	browser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, CheckRedirect: noRedirect}
	}
	send := func(c *http.Client, method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	location := func(resp *http.Response) string { return resp.Header.Get("Location") }
	stateOf := func(raw string) string {
		u, _ := url.Parse(raw)
		return u.Query().Get("state")
	}

	// Setup needs a session; the reader logs in with the password first.
	reader := browser()
	if resp := send(reader, "POST", "/api/v1/github/setup", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("setup without a session: %d", resp.StatusCode)
	}
	send(reader, "POST", "/api/v1/login", `{"password":"test-password"}`)
	var setup GitHubSetup
	json.NewDecoder(send(reader, "POST", "/api/v1/github/setup", "").Body).Decode(&setup)
	if !strings.Contains(setup.Manifest, srv.URL+"/auth/github/callback") {
		t.Fatalf("manifest: %s", setup.Manifest)
	}
	// GitHub sends the reader back with a code; the app's owner becomes the allowed account.
	done := send(reader, "GET", "/auth/github/setup?code=abc&state="+stateOf(setup.Action), "")
	if location(done) != "/settings?github=ready#signin" {
		t.Fatalf("setup done: %s", location(done))
	}

	signIn := func(code string) (*http.Client, string) {
		c := browser()
		var me Me
		json.NewDecoder(send(c, "GET", "/api/v1/me", "").Body).Decode(&me)
		if !me.GitHubLogin {
			t.Fatal("login page should offer GitHub")
		}
		start := send(c, "GET", "/auth/github/login", "")
		return c, location(send(c, "GET", "/auth/github/callback?code="+code+"&state="+stateOf(location(start)), ""))
	}
	owner, to := signIn("owner")
	if to != "/" || send(owner, "GET", "/api/v1/settings", "").StatusCode != http.StatusOK {
		t.Fatalf("owner sign-in: redirected to %s", to)
	}
	stranger, to := signIn("stranger")
	if to != "/login?github=denied" || send(stranger, "GET", "/api/v1/settings", "").StatusCode != http.StatusUnauthorized {
		t.Fatalf("stranger sign-in: redirected to %s", to)
	}

	// A callback from another browser, or a replayed state, is refused.
	c := browser()
	start := send(c, "GET", "/auth/github/login", "")
	other := browser()
	if to := location(send(other, "GET", "/auth/github/callback?code=owner&state="+stateOf(location(start)), "")); to != "/login?github=expired" {
		t.Fatalf("callback in another browser: %s", to)
	}
}
