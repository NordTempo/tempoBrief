// Package deliver sends briefs and alerts to the user's channels (Discord,
// email, ntfy, webhooks) and turns reactions in those channels back into
// signals. See docs/delivery.md.
package deliver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

// Message is a brief or an alert, ready to render for any channel.
type Message struct {
	Kind     Kind
	Title    string
	Date     string // local date, such as 2026-11-25
	BriefURL string // the brief in the web app; empty without a public URL
	Sections []Section
}

type Kind string

const (
	KindBrief Kind = "brief"
	KindAlert Kind = "alert"
)

type Section struct {
	Topic   string
	Stories []Story
}

// Story is one story as delivered.
type Story struct {
	ID        int64
	Title     string
	Summary   string
	URL       string // tracked link when a public URL is set, else the article
	DirectURL string
	Score     float64
	Sources   []string
	Why       string
	Vote      int
	UpURL     string // vote links; empty without a public URL
	DownURL   string
}

// Stories flattens a message's sections.
func (m Message) Stories() []Story {
	var out []Story
	for _, s := range m.Sections {
		out = append(out, s.Stories...)
	}
	return out
}

// VoteFunc records a vote coming from a channel.
type VoteFunc func(ctx context.Context, storyID int64, vote int) error

// Deliverer sends messages. Discord keeps a gateway connection per bot token
// for its vote buttons; the other channels are plain requests.
type Deliverer struct {
	log     *slog.Logger
	http    *http.Client
	discord *discordHub
}

func New(log *slog.Logger, vote VoteFunc) *Deliverer {
	return &Deliverer{
		log:     log,
		http:    &http.Client{Timeout: 30 * time.Second},
		discord: newDiscordHub(log, vote),
	}
}

// Configure (re)connects Discord bots for the enabled Discord channels. Call
// it at start and whenever settings change.
func (d *Deliverer) Configure(s settings.Settings) {
	d.discord.configure(s.Channels)
}

// Close disconnects Discord.
func (d *Deliverer) Close() { d.discord.close() }

// Send delivers a message to one channel.
func (d *Deliverer) Send(ctx context.Context, c settings.Channel, m Message) error {
	switch c.Kind {
	case settings.ChannelDiscord:
		return d.discord.send(ctx, c, m)
	case settings.ChannelEmail:
		return sendEmail(ctx, c, m)
	case settings.ChannelNtfy:
		return d.sendNtfy(ctx, c, m)
	case settings.ChannelWebhook:
		return d.sendWebhook(ctx, c, m)
	}
	return fmt.Errorf("unknown channel kind %q", c.Kind)
}

// Links builds the public URLs that deliveries point at: tracked article links
// (so opens count as a signal) and signed vote links.
type Links struct {
	Base   string // TEMPOBRIEF_PUBLIC_URL; empty disables tracking and vote links
	Secret string
}

func (l Links) Click(id int64, direct string) string {
	if l.Base == "" {
		return direct
	}
	return l.Base + "/r/" + strconv.FormatInt(id, 10)
}

func (l Links) Vote(id int64, vote int) string {
	if l.Base == "" {
		return ""
	}
	dir := "up"
	if vote < 0 {
		dir = "down"
	}
	return fmt.Sprintf("%s/v/%d/%s/%s", l.Base, id, dir, l.Sign(id, vote))
}

func (l Links) Brief(id int64) string {
	if l.Base == "" {
		return ""
	}
	return l.Base + "/briefs/" + strconv.FormatInt(id, 10)
}

// Sign returns the vote link signature for a story and vote.
func (l Links) Sign(id int64, vote int) string {
	mac := hmac.New(sha256.New, []byte(l.Secret))
	fmt.Fprintf(mac, "vote:%d:%d", id, vote)
	return hex.EncodeToString(mac.Sum(nil)[:12])
}

// Verify checks a vote link signature.
func (l Links) Verify(id int64, vote int, sig string) bool {
	return l.Secret != "" && hmac.Equal([]byte(sig), []byte(l.Sign(id, vote)))
}

// sourcesLabel renders "NRK +2".
func sourcesLabel(sources []string) string {
	switch len(sources) {
	case 0:
		return ""
	case 1:
		return sources[0]
	}
	return fmt.Sprintf("%s +%d", sources[0], len(sources)-1)
}

func scoreLabel(f float64) string {
	return strconv.FormatFloat(f, 'f', 1, 64)
}
