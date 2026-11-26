// Package auth implements the dashboard's single-password login and cookie
// sessions. See docs/deployment.md#auth.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/store"
)

const (
	CookieName      = "tb_session"
	sessionLifetime = 30 * 24 * time.Hour
	slideEvery      = time.Hour
	settingHash     = "password_hash"
)

var (
	ErrBadPassword  = errors.New("wrong password")
	ErrPasswordEnv  = errors.New("password is set by environment")
	ErrWeakPassword = errors.New("password must be at least 8 characters")
)

// RateLimitedError carries how long the client must wait.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string { return "too many attempts" }

type Service struct {
	mode         config.AuthMode
	cookieSecure config.CookieSecure
	passwordEnv  bool
	db           *store.Store
	limiter      *limiter
	now          func() time.Time

	mu   sync.RWMutex
	hash string
}

// New prepares the password: from env, from the stored hash, or generated on
// first start and logged once.
func New(ctx context.Context, cfg config.Config, db *store.Store, log *slog.Logger) (*Service, error) {
	s := &Service{
		mode:         cfg.Auth,
		cookieSecure: cfg.CookieSecure,
		passwordEnv:  cfg.PasswordEnv,
		db:           db,
		limiter:      newLimiter(time.Now),
		now:          time.Now,
	}
	if s.mode == config.AuthOff {
		log.Warn("auth is off: make sure a proxy authenticates in front of tempoBrief")
		return s, nil
	}
	if cfg.PasswordEnv {
		h, err := HashPassword(cfg.Password)
		if err != nil {
			return nil, err
		}
		s.hash = h
		return s, nil
	}
	var stored string
	err := db.Setting(ctx, settingHash, &stored)
	switch {
	case err == nil:
		s.hash = stored
	case errors.Is(err, store.ErrNotFound):
		pw := randomPassword(20)
		h, err := HashPassword(pw)
		if err != nil {
			return nil, err
		}
		if err := db.SetSetting(ctx, settingHash, h); err != nil {
			return nil, err
		}
		s.hash = h
		log.Warn("generated dashboard password, change it in Settings", "password", pw)
	default:
		return nil, err
	}
	return s, nil
}

func (s *Service) Mode() config.AuthMode { return s.mode }

// PasswordFromEnv reports whether the password can be changed in the UI.
func (s *Service) PasswordFromEnv() bool { return s.passwordEnv }

// Login checks the password and returns a new session token.
func (s *Service) Login(ctx context.Context, r *http.Request, password string) (string, error) {
	client := clientKey(r)
	if ok, wait := s.limiter.allow(client); !ok {
		return "", &RateLimitedError{RetryAfter: wait}
	}
	s.mu.RLock()
	hash := s.hash
	s.mu.RUnlock()
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return "", err
	}
	if !ok {
		s.limiter.fail(client)
		return "", ErrBadPassword
	}
	s.limiter.succeed(client)
	return s.StartSession(ctx)
}

// StartSession creates a session and returns its token, for a login the caller
// has already verified (the password above, or Sign in with GitHub).
func (s *Service) StartSession(ctx context.Context) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	now := s.now().Unix()
	_, err := s.db.DB.ExecContext(ctx,
		"INSERT INTO auth_sessions(token_hash, created_at, expires_at, last_seen) VALUES(?, ?, ?, ?)",
		tokenHash(token), now, now+int64(sessionLifetime.Seconds()), now)
	if err != nil {
		return "", err
	}
	// Opportunistic cleanup keeps the table tiny without a job.
	s.db.DB.ExecContext(ctx, "DELETE FROM auth_sessions WHERE expires_at < ?", now)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Authenticated reports whether the request carries a valid session, and slides
// its expiry at most once an hour.
func (s *Service) Authenticated(r *http.Request) bool {
	if s.mode == config.AuthOff {
		return true
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	token, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(token) != 32 {
		return false
	}
	h := tokenHash(token)
	var expires, lastSeen int64
	err = s.db.DB.QueryRowContext(r.Context(),
		"SELECT expires_at, last_seen FROM auth_sessions WHERE token_hash = ?", h).Scan(&expires, &lastSeen)
	now := s.now().Unix()
	if errors.Is(err, sql.ErrNoRows) || err != nil || expires < now {
		return false
	}
	if now-lastSeen > int64(slideEvery.Seconds()) {
		s.db.DB.ExecContext(r.Context(), "UPDATE auth_sessions SET last_seen = ?, expires_at = ? WHERE token_hash = ?",
			now, now+int64(sessionLifetime.Seconds()), h)
	}
	return true
}

// Logout deletes the request's session, if any.
func (s *Service) Logout(r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return
	}
	if token, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil {
		s.db.DB.ExecContext(r.Context(), "DELETE FROM auth_sessions WHERE token_hash = ?", tokenHash(token))
	}
}

// ChangePassword replaces the stored password and ends every session.
func (s *Service) ChangePassword(ctx context.Context, r *http.Request, current, next string) error {
	if s.passwordEnv {
		return ErrPasswordEnv
	}
	if len(next) < 8 {
		return ErrWeakPassword
	}
	client := clientKey(r)
	if ok, wait := s.limiter.allow(client); !ok {
		return &RateLimitedError{RetryAfter: wait}
	}
	s.mu.RLock()
	hash := s.hash
	s.mu.RUnlock()
	if ok, err := VerifyPassword(current, hash); err != nil || !ok {
		s.limiter.fail(client)
		return ErrBadPassword
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(ctx, settingHash, h); err != nil {
		return err
	}
	s.mu.Lock()
	s.hash = h
	s.mu.Unlock()
	_, err = s.db.DB.ExecContext(ctx, "DELETE FROM auth_sessions")
	return err
}

// SetCookie writes the session cookie; maxAge < 0 clears it.
func (s *Service) SetCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	secure := s.cookieSecure == config.CookieSecureTrue ||
		(s.cookieSecure == config.CookieSecureAuto && (r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"))
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

// SessionMaxAge is the cookie lifetime in seconds.
func SessionMaxAge() int { return int(sessionLifetime.Seconds()) }

func tokenHash(token []byte) []byte {
	h := sha256.Sum256(token)
	return h[:]
}

// clientKey identifies a client for rate limiting. Behind Caddy the last
// X-Forwarded-For entry is the one Caddy itself appended.
func clientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
