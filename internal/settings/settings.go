// Package settings holds everything a user edits at runtime: who they are and
// what they want, the model provider, brief schedules and delivery channels.
// It is stored as one JSON document. See docs/deployment.md#settings.
package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/store"
)

const key = "settings"

type Settings struct {
	// Interests is the reader's own description of what they want, in plain
	// sentences. The learned editorial policy refines it; it never replaces it.
	Interests string `json:"interests"`
	// Language that summaries and briefs are written in.
	Language string `json:"language"`
	Timezone string `json:"timezone"`

	Provider Provider        `json:"provider"`
	Briefs   []BriefSchedule `json:"briefs"`
	Alerts   Alerts          `json:"alerts"`
	Channels []Channel       `json:"channels"`
	Learning Learning        `json:"learning"`

	// PublicPages lets anyone read the brief and stories pages without logging
	// in, without the reader's votes or taste. Everything else needs a session.
	PublicPages bool `json:"publicPages"`

	// ReadToken lets unauthenticated readers (Home Assistant, a feed reader)
	// fetch the latest brief and the RSS feed.
	ReadToken string `json:"readToken"`
	// Secret signs vote links in emails and notifications.
	Secret string `json:"secret"`
}

type ProviderKind string

const (
	// ProviderAnthropic is the Claude API with an API key.
	ProviderAnthropic ProviderKind = "anthropic"
	// ProviderOpenAI is any OpenAI-compatible chat completions API: OpenAI,
	// OpenRouter, Ollama, LM Studio, vLLM.
	ProviderOpenAI ProviderKind = "openai"
	// ProviderClaudeCLI runs `claude -p` with whatever login the CLI already has.
	ProviderClaudeCLI ProviderKind = "claude-cli"
	// ProviderCodexCLI runs `codex exec` with whatever login the CLI already has.
	ProviderCodexCLI ProviderKind = "codex-cli"
)

var ProviderKinds = []ProviderKind{ProviderAnthropic, ProviderOpenAI, ProviderClaudeCLI, ProviderCodexCLI}

type Provider struct {
	Kind    ProviderKind `json:"kind"`
	Model   string       `json:"model"`   // empty means the provider's default
	APIKey  string       `json:"apiKey"`  // anthropic, openai; optional for local servers
	BaseURL string       `json:"baseUrl"` // openai: for OpenRouter, Ollama and friends
}

// BriefSchedule is one recurring brief, such as the morning brief.
type BriefSchedule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Time     string `json:"time"` // HH:MM local
	Days     []int  `json:"days"` // ISO weekdays, 1 = Monday; empty means every day
	MinScore int    `json:"minScore"`
	// MaxStories caps the whole brief: its best stories, from any topic.
	// Nothing is added to fill it.
	MaxStories int `json:"maxStories"`
	// Channels the brief goes to, by channel ID. Empty means every channel
	// with briefs enabled.
	Channels []string `json:"channels"`
}

// Alerts push single stories as they break.
type Alerts struct {
	Enabled    bool     `json:"enabled"`
	MinScore   int      `json:"minScore"`
	MinSources int      `json:"minSources"`
	Channels   []string `json:"channels"`
}

type ChannelKind string

const (
	ChannelDiscord ChannelKind = "discord"
	ChannelEmail   ChannelKind = "email"
	ChannelNtfy    ChannelKind = "ntfy"
	ChannelWebhook ChannelKind = "webhook"
)

var ChannelKinds = []ChannelKind{ChannelDiscord, ChannelEmail, ChannelNtfy, ChannelWebhook}

// Channel is one place briefs and alerts are delivered to. Only the fields of
// its kind are used.
type Channel struct {
	ID      string      `json:"id"`
	Kind    ChannelKind `json:"kind"`
	Name    string      `json:"name"`
	Enabled bool        `json:"enabled"`

	// Discord: a bot token, the channel to post in, and optionally the only
	// user allowed to vote with the buttons.
	DiscordToken     string `json:"discordToken,omitempty"`
	DiscordChannelID string `json:"discordChannelId,omitempty"`
	DiscordOwnerID   string `json:"discordOwnerId,omitempty"`

	// Email over SMTP. Port 465 uses implicit TLS, anything else STARTTLS.
	SMTPHost     string `json:"smtpHost,omitempty"`
	SMTPPort     int    `json:"smtpPort,omitempty"`
	SMTPUsername string `json:"smtpUsername,omitempty"`
	SMTPPassword string `json:"smtpPassword,omitempty"`
	EmailFrom    string `json:"emailFrom,omitempty"`
	EmailTo      string `json:"emailTo,omitempty"`

	// ntfy: server (default https://ntfy.sh), topic, optional access token.
	NtfyServer string `json:"ntfyServer,omitempty"`
	NtfyTopic  string `json:"ntfyTopic,omitempty"`
	NtfyToken  string `json:"ntfyToken,omitempty"`

	// Webhook: the brief as JSON, POSTed to this URL.
	WebhookURL string `json:"webhookUrl,omitempty"`
}

// Learning controls how tempoBrief adapts to reactions.
type Learning struct {
	// Policy turns the weekly editorial policy rewrite on.
	Policy bool `json:"policy"`
	// Taste turns the similarity adjustment from past votes on.
	Taste bool `json:"taste"`
	// IgnoreAfterHours: a delivered story nobody opened or voted on within
	// this many hours counts as a weak "not interested". 0 turns it off.
	IgnoreAfterHours int `json:"ignoreAfterHours"`
}

// Default is what a fresh install starts with.
func Default() Settings {
	return Settings{
		Language: "English",
		Timezone: "Europe/Oslo",
		Provider: Provider{Kind: ProviderAnthropic},
		Briefs: []BriefSchedule{{
			ID: "morning", Name: "Morning brief", Enabled: true, Time: "07:00",
			MinScore: 6, MaxStories: defaultMaxStories,
		}},
		Alerts:   Alerts{Enabled: false, MinScore: 9, MinSources: 2},
		Learning: Learning{Policy: true, Taste: true, IgnoreAfterHours: 48},
	}
}

const defaultMaxStories = 10

// Load reads settings, creating defaults (with fresh tokens) on first start.
func Load(ctx context.Context, db *store.Store) (Settings, error) {
	s := Default()
	err := db.Setting(ctx, key, &s)
	if errors.Is(err, store.ErrNotFound) {
		s.ReadToken, s.Secret = randomHex(16), randomHex(32)
		return s.normalized(), db.SetSetting(ctx, key, s.normalized())
	}
	return s.normalized(), err
}

// normalized replaces nil lists with empty ones, so JSON always carries
// arrays (the web app's types say so).
func (s Settings) normalized() Settings {
	if s.Briefs == nil {
		s.Briefs = []BriefSchedule{}
	}
	if s.Channels == nil {
		s.Channels = []Channel{}
	}
	if s.Alerts.Channels == nil {
		s.Alerts.Channels = []string{}
	}
	s.Briefs = slices.Clone(s.Briefs)
	for i := range s.Briefs {
		if s.Briefs[i].Days == nil {
			s.Briefs[i].Days = []int{}
		}
		if s.Briefs[i].Channels == nil {
			s.Briefs[i].Channels = []string{}
		}
		if s.Briefs[i].MaxStories == 0 { // saved before the cap was per brief
			s.Briefs[i].MaxStories = defaultMaxStories
		}
	}
	return s
}

// Save validates and stores settings.
func Save(ctx context.Context, db *store.Store, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return db.SetSetting(ctx, key, s.normalized())
}

// Location is the configured time zone; UTC if it cannot be loaded.
func (s Settings) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

var (
	hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

// Validate returns every problem at once.
func (s Settings) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if strings.TrimSpace(s.Language) == "" {
		fail("language is required")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "" {
		fail("timezone %q is not a known IANA time zone", s.Timezone)
	}
	if !slices.Contains(ProviderKinds, s.Provider.Kind) {
		fail("provider must be one of %v", ProviderKinds)
	}
	if s.Provider.Kind == ProviderOpenAI && s.Provider.Model == "" {
		fail("an OpenAI-compatible provider needs a model name")
	}

	channelIDs := map[string]bool{}
	for i, c := range s.Channels {
		where := fmt.Sprintf("channel %d (%s)", i+1, c.Name)
		if !idRe.MatchString(c.ID) || channelIDs[c.ID] {
			fail("%s: id must be unique, lowercase letters, digits and dashes", where)
		}
		channelIDs[c.ID] = true
		if !slices.Contains(ChannelKinds, c.Kind) {
			fail("%s: kind must be one of %v", where, ChannelKinds)
		}
		if !c.Enabled {
			continue
		}
		switch c.Kind {
		case ChannelDiscord:
			if c.DiscordToken == "" || c.DiscordChannelID == "" {
				fail("%s: Discord needs a bot token and a channel ID", where)
			}
		case ChannelEmail:
			if c.SMTPHost == "" || c.EmailFrom == "" || c.EmailTo == "" {
				fail("%s: email needs an SMTP host, a from and a to address", where)
			}
		case ChannelNtfy:
			if c.NtfyTopic == "" {
				fail("%s: ntfy needs a topic", where)
			}
		case ChannelWebhook:
			if !strings.HasPrefix(c.WebhookURL, "http://") && !strings.HasPrefix(c.WebhookURL, "https://") {
				fail("%s: webhook needs an http(s) URL", where)
			}
		}
	}

	scheduleIDs := map[string]bool{}
	for i, b := range s.Briefs {
		where := fmt.Sprintf("brief %d (%s)", i+1, b.Name)
		if !idRe.MatchString(b.ID) || scheduleIDs[b.ID] {
			fail("%s: id must be unique, lowercase letters, digits and dashes", where)
		}
		scheduleIDs[b.ID] = true
		if !hhmm.MatchString(b.Time) {
			fail("%s: time must be HH:MM, got %q", where, b.Time)
		}
		for _, d := range b.Days {
			if d < 1 || d > 7 {
				fail("%s: days must be 1 (Monday) to 7 (Sunday)", where)
			}
		}
		if b.MinScore < 0 || b.MinScore > 10 {
			fail("%s: minimum score must be 0 to 10", where)
		}
		if b.MaxStories < 1 || b.MaxStories > 50 {
			fail("%s: stories at most must be 1 to 50", where)
		}
		for _, id := range b.Channels {
			if !channelIDs[id] {
				fail("%s: unknown channel %q", where, id)
			}
		}
	}
	for _, id := range s.Alerts.Channels {
		if !channelIDs[id] {
			fail("alerts: unknown channel %q", id)
		}
	}
	if s.Alerts.MinScore < 0 || s.Alerts.MinScore > 10 {
		fail("alerts: minimum score must be 0 to 10")
	}
	if s.Learning.IgnoreAfterHours < 0 || s.Learning.IgnoreAfterHours > 24*14 {
		fail("learning: ignore window must be 0 to 336 hours")
	}
	return errors.Join(errs...)
}

// Channel returns the channel with the given ID.
func (s Settings) Channel(id string) (Channel, bool) {
	for _, c := range s.Channels {
		if c.ID == id {
			return c, true
		}
	}
	return Channel{}, false
}

// ChannelsFor resolves a channel ID list; empty means every enabled channel.
func (s Settings) ChannelsFor(ids []string) []Channel {
	var out []Channel
	for _, c := range s.Channels {
		if c.Enabled && (len(ids) == 0 || slices.Contains(ids, c.ID)) {
			out = append(out, c)
		}
	}
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Secrets.
//
// The API never returns secrets. Redact replaces each set secret with a
// placeholder, and MergeSecrets puts the stored value back wherever an update
// still carries that placeholder, so a form can round-trip settings without
// knowing them.

const Redacted = "••••••••"

func (s Settings) Redact() Settings {
	r := s
	r.Provider.APIKey = mask(s.Provider.APIKey)
	r.Secret = ""
	r.Channels = slices.Clone(s.Channels)
	for i := range r.Channels {
		c := &r.Channels[i]
		c.DiscordToken, c.SMTPPassword, c.NtfyToken = mask(c.DiscordToken), mask(c.SMTPPassword), mask(c.NtfyToken)
	}
	return r
}

// MergeSecrets fills placeholders in next from prev and keeps server-owned fields.
func MergeSecrets(prev, next Settings) Settings {
	next.Secret = prev.Secret
	next.ReadToken = prev.ReadToken
	if next.Provider.APIKey == Redacted {
		next.Provider.APIKey = prev.Provider.APIKey
	}
	for i := range next.Channels {
		c := &next.Channels[i]
		old, _ := prev.Channel(c.ID)
		if c.DiscordToken == Redacted {
			c.DiscordToken = old.DiscordToken
		}
		if c.SMTPPassword == Redacted {
			c.SMTPPassword = old.SMTPPassword
		}
		if c.NtfyToken == Redacted {
			c.NtfyToken = old.NtfyToken
		}
	}
	return next
}

// RotateReadToken issues a new read token, invalidating old feed and HA URLs.
func (s *Settings) RotateReadToken() { s.ReadToken = randomHex(16) }

func mask(v string) string {
	if v == "" {
		return ""
	}
	return Redacted
}
