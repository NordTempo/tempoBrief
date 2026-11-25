// Package config reads tempoBrief's deployment settings from environment
// variables. Everything a user edits at runtime (interests, feeds, provider,
// schedules, channels) lives in the database instead. See docs/deployment.md.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type AuthMode string

const (
	AuthPassword AuthMode = "password"
	AuthOff      AuthMode = "off"
)

type CookieSecure string

const (
	CookieSecureAuto  CookieSecure = "auto"
	CookieSecureTrue  CookieSecure = "true"
	CookieSecureFalse CookieSecure = "false"
)

type Config struct {
	Listen       string
	DataDir      string
	PublicURL    string // base for links in deliveries (click tracking, votes); empty means direct links
	Auth         AuthMode
	Password     string // from env or file; empty means stored or generated
	PasswordEnv  bool   // set by env, so it cannot be changed in the UI
	CookieSecure CookieSecure
	Poll         time.Duration // how often each feed is fetched
	EmbedModel   string        // Hugging Face repo with an ONNX sentence embedding model
	EmbedFile    string        // ONNX file inside that repo
	LogLevel     string
}

// Load reads the config from getenv (os.Getenv in production) and returns every
// problem at once, one per line.
func Load(getenv func(string) string) (Config, error) {
	p := parser{getenv: getenv}
	c := Config{
		Listen:       p.str("TEMPOBRIEF_LISTEN", ":8080"),
		DataDir:      p.str("TEMPOBRIEF_DATA", "/data"),
		PublicURL:    strings.TrimRight(p.str("TEMPOBRIEF_PUBLIC_URL", ""), "/"),
		Auth:         oneOf(&p, "TEMPOBRIEF_AUTH", AuthPassword, AuthOff),
		CookieSecure: oneOf(&p, "TEMPOBRIEF_COOKIE_SECURE", CookieSecureAuto, CookieSecureTrue, CookieSecureFalse),
		Poll:         p.duration("TEMPOBRIEF_POLL", 15*time.Minute, 5*time.Minute),
		EmbedModel:   p.str("TEMPOBRIEF_EMBED_MODEL", "Xenova/multilingual-e5-small"),
		EmbedFile:    p.str("TEMPOBRIEF_EMBED_FILE", "onnx/model_quantized.onnx"),
		LogLevel:     oneOf(&p, "TEMPOBRIEF_LOG_LEVEL", "info", "debug", "warn", "error"),
	}

	if file := getenv("TEMPOBRIEF_PASSWORD_FILE"); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			p.fail("TEMPOBRIEF_PASSWORD_FILE", "cannot read %s: %v", file, err)
		} else {
			c.Password = strings.TrimRight(string(b), "\r\n")
		}
	} else {
		c.Password = getenv("TEMPOBRIEF_PASSWORD")
	}
	c.PasswordEnv = c.Password != ""
	if c.PasswordEnv && len(c.Password) < 8 {
		p.fail("TEMPOBRIEF_PASSWORD", "must be at least 8 characters")
	}
	if c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			p.fail("TEMPOBRIEF_PUBLIC_URL", "must be an http(s) URL like https://brief.example.com, got %q", c.PublicURL)
		}
	}
	return c, errors.Join(p.errs...)
}

type parser struct {
	getenv func(string) string
	errs   []error
}

func (p *parser) fail(key, format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...)))
}

func (p *parser) str(key, def string) string {
	if v := strings.TrimSpace(p.getenv(key)); v != "" {
		return v
	}
	return def
}

func (p *parser) duration(key string, def, min time.Duration) time.Duration {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < min {
		p.fail(key, "must be a duration of at least %s, like 15m, got %q", min, v)
		return def
	}
	return d
}

func oneOf[T ~string](p *parser, key string, def T, others ...T) T {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	allowed := append([]T{def}, others...)
	for _, a := range allowed {
		if string(a) == v {
			return a
		}
	}
	p.fail(key, "must be one of %v, got %q", allowed, v)
	return def
}
