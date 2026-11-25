package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func newService(t *testing.T, password string) *Service {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Config{Auth: config.AuthPassword, CookieSecure: config.CookieSecureAuto, Password: password, PasswordEnv: password != ""}
	s, err := New(context.Background(), cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func req() *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/login", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	return r
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse", h); !ok {
		t.Fatal("right password rejected")
	}
	if ok, _ := VerifyPassword("wrong horse", h); ok {
		t.Fatal("wrong password accepted")
	}
}

func TestLoginAndSession(t *testing.T) {
	s := newService(t, "hunter2hunter2")
	ctx := context.Background()
	if _, err := s.Login(ctx, req(), "nope"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("want ErrBadPassword, got %v", err)
	}
	token, err := s.Login(ctx, req(), "hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/status", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if !s.Authenticated(r) {
		t.Fatal("valid session rejected")
	}
	s.Logout(r)
	if s.Authenticated(r) {
		t.Fatal("session survived logout")
	}
}

func TestExpiredSession(t *testing.T) {
	s := newService(t, "hunter2hunter2")
	token, _ := s.Login(context.Background(), req(), "hunter2hunter2")
	s.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if s.Authenticated(r) {
		t.Fatal("expired session accepted")
	}
}

func TestBackoff(t *testing.T) {
	s := newService(t, "hunter2hunter2")
	ctx := context.Background()
	for range freeFailures {
		s.Login(ctx, req(), "nope")
	}
	_, err := s.Login(ctx, req(), "hunter2hunter2")
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("want rate limit after %d failures, got %v", freeFailures, err)
	}
	other := req()
	other.RemoteAddr = "192.0.2.2:1"
	if _, err := s.Login(ctx, other, "hunter2hunter2"); err != nil {
		t.Fatalf("other client blocked: %v", err)
	}
}

func TestGeneratedPasswordIsStored(t *testing.T) {
	s := newService(t, "")
	var stored string
	if err := s.db.Setting(context.Background(), settingHash, &stored); err != nil || stored == "" {
		t.Fatalf("no stored hash: %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	s := newService(t, "")
	s.hash, _ = HashPassword("oldpassword")
	ctx := context.Background()
	if err := s.ChangePassword(ctx, req(), "wrong", "newpassword"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("got %v", err)
	}
	if err := s.ChangePassword(ctx, req(), "oldpassword", "newpassword"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, req(), "newpassword"); err != nil {
		t.Fatal(err)
	}
}
