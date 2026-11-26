package api

// Sign in with GitHub, through a GitHub App the reader registers from
// Settings with GitHub's manifest flow: tempoBrief posts a prefilled app to
// GitHub, the reader clicks Create, and GitHub hands back the app's client ID
// and secret. The account that created the app is the only one let in.
// See docs/deployment.md#sign-in-with-github.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/NordTempo/tempoBrief/internal/auth"
	"github.com/NordTempo/tempoBrief/internal/store"
)

const (
	githubSettingKey = "github_login"
	oauthCookie      = "tempobrief_oauth"
	oauthLifetime    = 10 * time.Minute
)

// githubLogin is stored on its own, outside the settings document, so only
// this flow can change who may sign in.
type githubLogin struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	AppURL       string `json:"appUrl"`
	UserID       int64  `json:"userId"`
	Login        string `json:"login"`
}

// GitHub endpoints; tests point them at a fake.
type githubEndpoints struct {
	Web string // https://github.com
	API string // https://api.github.com
}

var realGitHub = githubEndpoints{Web: "https://github.com", API: "https://api.github.com"}

// oauthStates remembers flows in progress: one use each, ten minutes. The
// state is also set in a cookie, so a flow only finishes in the browser that
// started it.
type oauthStates struct {
	mu sync.Mutex
	m  map[string]oauthState
}

type oauthState struct {
	purpose  string // "setup" or "login"
	verifier string // PKCE, for login
	expires  time.Time
}

func (o *oauthStates) put(st oauthState) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.m == nil {
		o.m = map[string]oauthState{}
	}
	now := time.Now()
	for k, v := range o.m {
		if now.After(v.expires) {
			delete(o.m, k)
		}
	}
	key := randomToken()
	st.expires = now.Add(oauthLifetime)
	o.m[key] = st
	return key
}

func (o *oauthStates) take(key, purpose string) (oauthState, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.m[key]
	delete(o.m, key)
	return st, ok && st.purpose == purpose && time.Now().Before(st.expires)
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) loadGitHub(ctx context.Context) (githubLogin, error) {
	var g githubLogin
	err := s.DB.Setting(ctx, githubSettingKey, &g)
	if errors.Is(err, store.ErrNotFound) {
		return githubLogin{}, nil
	}
	return g, err
}

func (g githubLogin) ready() bool { return g.ClientID != "" && g.UserID != 0 }

// setOAuthCookie binds a flow to this browser. Lax, because GitHub sends the
// reader back with a cross-site redirect.
func (s *Server) setOAuthCookie(w http.ResponseWriter, r *http.Request, state string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state, Path: "/auth/github/", MaxAge: maxAge,
		HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode,
	})
}

// checkState takes the flow named by ?state= if this browser started it.
func (s *Server) checkState(w http.ResponseWriter, r *http.Request, purpose string) (oauthState, bool) {
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(oauthCookie)
	s.setOAuthCookie(w, r, "", -1)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		return oauthState{}, false
	}
	return s.oauth.take(state, purpose)
}

// GET /api/v1/github
func (s *Server) githubStatus(w http.ResponseWriter, r *http.Request) {
	g, err := s.loadGitHub(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, GitHubStatus{
		Available: s.Config.PublicURL != "", Enabled: g.ready(), Login: g.Login, AppURL: g.AppURL,
	})
}

// POST /api/v1/github/setup returns the form the web app posts to GitHub.
func (s *Server) githubSetup(w http.ResponseWriter, r *http.Request) {
	base := s.Config.PublicURL
	if base == "" {
		writeError(w, http.StatusConflict, "no_public_url", "set TEMPOBRIEF_PUBLIC_URL first")
		return
	}
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	name := "tempoBrief " + host
	if len(name) > 34 { // GitHub's limit; the reader can rename it on the form
		name = name[:34]
	}
	manifest, _ := json.Marshal(map[string]any{
		"name":          name,
		"url":           base,
		"description":   "Sign in to tempoBrief at " + host,
		"redirect_url":  base + "/auth/github/setup",
		"callback_urls": []string{base + "/auth/github/callback"},
		"public":        false,
		// No webhook and no permissions: the app only identifies the reader.
		"hook_attributes":     map[string]any{"url": base + "/healthz", "active": false},
		"default_permissions": map[string]string{},
	})
	state := s.oauth.put(oauthState{purpose: "setup"})
	s.setOAuthCookie(w, r, state, int(oauthLifetime.Seconds()))
	writeJSON(w, http.StatusOK, GitHubSetup{
		Action:   s.github.Web + "/settings/apps/new?state=" + url.QueryEscape(state),
		Manifest: string(manifest),
	})
}

// DELETE /api/v1/github turns GitHub sign-in off. The app stays on GitHub
// until the reader deletes it there.
func (s *Server) githubRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.SetSetting(r.Context(), githubSettingKey, githubLogin{}); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /auth/github/setup?code=&state= : GitHub created the app. Exchange the
// code for its credentials; the app's owner becomes the allowed account.
func (s *Server) githubSetupDone(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.checkState(w, r, "setup"); !ok {
		http.Redirect(w, r, "/settings/signin?github=expired", http.StatusFound)
		return
	}
	var app struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		HTMLURL      string `json:"html_url"`
		Owner        struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"owner"`
	}
	code := url.PathEscape(r.URL.Query().Get("code"))
	if err := s.githubJSON(r.Context(), http.MethodPost, s.github.API+"/app-manifests/"+code+"/conversions", nil, "", &app); err != nil {
		s.Log.Error("github app setup", "err", err)
		http.Redirect(w, r, "/settings/signin?github=failed", http.StatusFound)
		return
	}
	g := githubLogin{ClientID: app.ClientID, ClientSecret: app.ClientSecret, AppURL: app.HTMLURL,
		UserID: app.Owner.ID, Login: app.Owner.Login}
	if !g.ready() {
		http.Redirect(w, r, "/settings/signin?github=failed", http.StatusFound)
		return
	}
	if err := s.DB.SetSetting(r.Context(), githubSettingKey, g); err != nil {
		s.internal(w, err)
		return
	}
	http.Redirect(w, r, "/settings/signin?github=ready", http.StatusFound)
}

// GET /auth/github/login sends the browser to GitHub to sign in.
func (s *Server) githubLogin(w http.ResponseWriter, r *http.Request) {
	g, err := s.loadGitHub(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if !g.ready() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	verifier := randomToken()
	state := s.oauth.put(oauthState{purpose: "login", verifier: verifier})
	s.setOAuthCookie(w, r, state, int(oauthLifetime.Seconds()))
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {g.ClientID},
		"redirect_uri":          {s.Config.PublicURL + "/auth/github/callback"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"login":                 {g.Login},
		"allow_signup":          {"false"},
	}
	http.Redirect(w, r, s.github.Web+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// GET /auth/github/callback?code=&state= : start a session if the GitHub
// account is the allowed one.
func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	st, ok := s.checkState(w, r, "login")
	if !ok {
		http.Redirect(w, r, "/login?github=expired", http.StatusFound)
		return
	}
	g, err := s.loadGitHub(r.Context())
	if err != nil || !g.ready() {
		http.Redirect(w, r, "/login?github=off", http.StatusFound)
		return
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	form := url.Values{
		"client_id": {g.ClientID}, "client_secret": {g.ClientSecret}, "code": {r.URL.Query().Get("code")},
		"redirect_uri": {s.Config.PublicURL + "/auth/github/callback"}, "code_verifier": {st.verifier},
	}
	err = s.githubJSON(r.Context(), http.MethodPost, s.github.Web+"/login/oauth/access_token", form, "", &tok)
	if err == nil && tok.AccessToken == "" {
		err = fmt.Errorf("no token: %s", tok.Error)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err == nil {
		err = s.githubJSON(r.Context(), http.MethodGet, s.github.API+"/user", nil, tok.AccessToken, &user)
	}
	if err != nil {
		s.Log.Error("github sign-in", "err", err)
		http.Redirect(w, r, "/login?github=failed", http.StatusFound)
		return
	}
	if user.ID != g.UserID {
		http.Redirect(w, r, "/login?github=denied", http.StatusFound)
		return
	}
	token, err := s.Auth.StartSession(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	s.Auth.SetCookie(w, r, token, auth.SessionMaxAge())
	http.Redirect(w, r, "/", http.StatusFound)
}

// githubJSON makes one GitHub call and decodes the JSON answer. A form body
// is sent url-encoded; token, when set, authenticates as the user.
func (s *Server) githubJSON(ctx context.Context, method, endpoint string, form url.Values, token string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %d %s", method, endpoint, resp.StatusCode, b)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
