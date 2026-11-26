// Package api serves the JSON API, the public link endpoints (tracked clicks,
// vote links, the read-token feeds) and the embedded web app on one handler.
// See docs/api.md.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/auth"
	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

type Deps struct {
	Config   config.Config
	DB       *store.Store
	Auth     *auth.Service
	Log      *slog.Logger
	Assets   fs.FS
	Version  string
	Pipeline *pipeline.Pipeline
	// Sender delivers channel tests; Configure applies new settings (Discord bots).
	Sender    pipeline.Sender
	Configure func(settings.Settings)
}

type Server struct {
	Deps
	now    func() time.Time
	oauth  oauthStates
	github githubEndpoints
}

func New(d Deps) *Server {
	if d.Configure == nil {
		d.Configure = func(settings.Settings) {}
	}
	return &Server{Deps: d, now: time.Now, github: realGitHub}
}

// Handler wires every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)

	// Public: the web app needs these before login.
	mux.HandleFunc("GET /api/v1/me", s.me)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/logout", s.logout)

	// Sign in with GitHub: GitHub sends the browser back to these. Each checks
	// a one-time state bound to the browser that started the flow.
	mux.HandleFunc("GET /auth/github/login", s.githubLogin)
	mux.HandleFunc("GET /auth/github/callback", s.githubCallback)
	mux.HandleFunc("GET /auth/github/setup", s.githubSetupDone)

	// Public links that deliveries point at. Clicks only redirect to a stored
	// story URL (never an open redirect); votes need a signature.
	mux.HandleFunc("GET /r/{id}", s.click)
	mux.HandleFunc("GET /v/{id}/{dir}/{sig}", s.voteLinkPage)
	mux.HandleFunc("POST /v/{id}/{dir}/{sig}", s.voteLink)

	// Read-only feeds for Home Assistant and feed readers, with the read token.
	mux.HandleFunc("GET /brief.json", s.briefJSON)
	mux.HandleFunc("GET /feed.xml", s.feedXML)

	// Reading briefs and stories needs a session, or public pages turned on.
	mux.Handle("GET /api/v1/briefs", s.readable(s.briefs))
	mux.Handle("GET /api/v1/briefs/latest", s.readable(s.latestBrief))
	mux.Handle("GET /api/v1/briefs/{id}", s.readable(s.brief))
	mux.Handle("GET /api/v1/stories", s.readable(s.stories))

	// Everything else under /api/v1 needs a session.
	private := http.NewServeMux()
	private.HandleFunc("GET /api/v1/status", s.status)
	private.HandleFunc("PUT /api/v1/password", s.changePassword)
	private.HandleFunc("GET /api/v1/github", s.githubStatus)
	private.HandleFunc("POST /api/v1/github/setup", s.githubSetup)
	private.HandleFunc("DELETE /api/v1/github", s.githubRemove)

	private.HandleFunc("POST /api/v1/briefs/send", s.sendBrief)
	private.HandleFunc("PUT /api/v1/stories/{id}/vote", s.vote)

	private.HandleFunc("GET /api/v1/feeds", s.feeds)
	private.HandleFunc("POST /api/v1/feeds", s.addFeed)
	private.HandleFunc("PUT /api/v1/feeds/{id}", s.updateFeed)
	private.HandleFunc("DELETE /api/v1/feeds/{id}", s.deleteFeed)
	private.HandleFunc("POST /api/v1/feeds/{id}/fetch", s.fetchFeed)
	private.HandleFunc("POST /api/v1/feeds/import", s.importOPML)
	private.HandleFunc("GET /api/v1/feeds/export", s.exportOPML)

	private.HandleFunc("GET /api/v1/learning", s.learning)
	private.HandleFunc("PUT /api/v1/policy", s.editPolicy)
	private.HandleFunc("POST /api/v1/policy/learn", s.learnNow)
	private.HandleFunc("POST /api/v1/policy/{id}/restore", s.restorePolicy)

	private.HandleFunc("GET /api/v1/settings", s.getSettings)
	private.HandleFunc("PUT /api/v1/settings", s.putSettings)
	private.HandleFunc("POST /api/v1/settings/read-token", s.rotateReadToken)
	private.HandleFunc("POST /api/v1/settings/test-provider", s.testProvider)
	private.HandleFunc("POST /api/v1/settings/test-channel", s.testChannel)

	private.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	mux.Handle("/api/v1/", s.requireSession(private))

	mux.Handle("/", s.static())

	return securityHeaders(sameOriginMutations(mux))
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Auth.Authenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "log in first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// readable serves a read route to the logged-in reader, and to anyone when the
// reader turned public pages on. Handlers call s.public(r) to strip what only
// the reader should see.
func (s *Server) readable(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Auth.Authenticated(r) {
			st, ok := s.settings(w, r)
			if !ok {
				return
			}
			if !st.PublicPages {
				writeError(w, http.StatusUnauthorized, "unauthorized", "log in first")
				return
			}
		}
		h(w, r)
	})
}

// public reports whether a read route is serving a visitor rather than the reader.
func (s *Server) public(r *http.Request) bool { return !s.Auth.Authenticated(r) }

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(r.Context()); err != nil {
		http.Error(w, "database not writable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	gh, err := s.loadGitHub(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Me{
		GitHubLogin:     gh.ready(),
		AuthMode:        AuthMode(s.Auth.Mode()),
		Authenticated:   s.Auth.Authenticated(r),
		PasswordFromEnv: s.Auth.PasswordFromEnv(),
		PublicPages:     st.PublicPages,
		Version:         s.Version,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body LoginRequest
	if !readJSON(w, r, &body) {
		return
	}
	token, err := s.Auth.Login(r.Context(), r, body.Password)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		w.Header().Set("Retry-After", strconv.Itoa(int(rl.RetryAfter.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
	case errors.Is(err, auth.ErrBadPassword):
		writeError(w, http.StatusUnauthorized, "bad_password", "wrong password")
	case err != nil:
		s.internal(w, err)
	default:
		s.Auth.SetCookie(w, r, token, auth.SessionMaxAge())
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r)
	s.Auth.SetCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body PasswordRequest
	if !readJSON(w, r, &body) {
		return
	}
	err := s.Auth.ChangePassword(r.Context(), r, body.Current, body.New)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		w.Header().Set("Retry-After", strconv.Itoa(int(rl.RetryAfter.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
	case errors.Is(err, auth.ErrPasswordEnv):
		writeError(w, http.StatusConflict, "password_from_env", "the password is set by TEMPOBRIEF_PASSWORD")
	case errors.Is(err, auth.ErrWeakPassword):
		writeFieldError(w, "new", err.Error())
	case errors.Is(err, auth.ErrBadPassword):
		writeFieldError(w, "current", "wrong password")
	case err != nil:
		s.internal(w, err)
	default:
		s.Auth.SetCookie(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
	}
}

// settings loads the current settings, writing a 500 on failure.
func (s *Server) settings(w http.ResponseWriter, r *http.Request) (settings.Settings, bool) {
	st, err := settings.Load(r.Context(), s.DB)
	if err != nil {
		s.internal(w, err)
		return st, false
	}
	return st, true
}

func (s *Server) links(st settings.Settings) deliver.Links {
	return deliver.Links{Base: s.Config.PublicURL, Secret: st.Secret}
}

// static serves the embedded web app. Unknown paths get index.html so client
// routes work on reload. Hashed assets are cached forever.
func (s *Server) static() http.Handler {
	files := http.FileServerFS(s.Assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "index.html" {
			s.serveIndex(w)
			return
		}
		if _, err := fs.Stat(s.Assets, p); err != nil {
			s.serveIndex(w)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	b, err := fs.ReadFile(s.Assets, "index.html")
	if err != nil {
		http.Error(w, "web app not built, run make web", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return // the client went away
	}
	s.Log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://github.com")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		// A personal instance, public pages included, stays out of search engines.
		h.Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

// sameOriginMutations rejects state-changing requests that are not JSON (or
// empty) or come from another origin. Together with SameSite=Strict cookies
// this is the CSRF defence. Vote links are exempt from the origin check: they
// are signed, and ntfy posts them from the phone app.
func sameOriginMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v/") {
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request refused")
				return
			}
		}
		if r.ContentLength != 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "json_required", "send application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

func writeFieldError(w http.ResponseWriter, field, message string) {
	writeJSON(w, http.StatusUnprocessableEntity, ErrorBody{Error: ErrorDetail{
		Code: "invalid", Message: message, Fields: map[string]string{field: message},
	}})
}

// readJSON decodes a JSON body of up to 4 MB (OPML imports), writing a 400 on failure.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such id")
		return 0, false
	}
	return id, true
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
