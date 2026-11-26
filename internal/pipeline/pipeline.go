// Package pipeline runs tempoBrief's work on a one-minute tick: fetch due
// feeds, group new items into stories, score them, send due briefs and
// alerts, and learn from reactions. Each step is also callable on demand from
// the API. See docs/pipeline.md.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// Embedder turns texts into unit vectors. *embed.Embedder in production.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Sender delivers to one channel. *deliver.Deliverer in production.
type Sender interface {
	Send(ctx context.Context, c settings.Channel, m deliver.Message) error
}

type Deps struct {
	Config   config.Config
	DB       *store.Store
	Log      *slog.Logger
	Fetcher  *feeds.Fetcher
	Embedder Embedder
	Sender   Sender
	// NewModel builds the model for the current provider; llm.New in
	// production, a fake in tests.
	NewModel func(settings.Provider) (llm.Model, error)
	Now      func() time.Time
}

type Pipeline struct {
	Deps
	mu     sync.Mutex // one step at a time; the tick and API calls share it
	status statusTracker
	// The provider said it is out of requests until then. Guarded by mu.
	modelPausedUntil time.Time
	// Scoring a batch that is not full waits this long after the last one,
	// so a trickle of stories does not cost a request a minute. Guarded by mu.
	scoreEvery  time.Duration
	lastPartial time.Time
}

func New(d Deps) *Pipeline {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewModel == nil {
		d.NewModel = llm.New
	}
	return &Pipeline{Deps: d, scoreEvery: scoreEvery}
}

const (
	// Entries older than this when first seen are not news any more; this
	// keeps a new feed's backlog out.
	maxItemAge = 36 * time.Hour
	// A story stays open for new items this long after its last item.
	storyWindow = 48 * time.Hour
	// Stories without reactions are deleted after this.
	retention = 30 * 24 * time.Hour
)

// Run ticks every minute until ctx is done.
func (p *Pipeline) Run(ctx context.Context) error {
	if p.DB.MetaTime(ctx, "installed_at").IsZero() {
		p.DB.SetMetaTime(ctx, "installed_at", p.Now())
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		p.Tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Tick runs every step once. Each step logs and records its own failure, so
// one broken step (a provider outage) does not stop the others.
func (p *Pipeline) Tick(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, err := settings.Load(ctx, p.DB)
	if err != nil {
		p.Log.Error("settings", "err", err)
		return
	}
	steps := []struct {
		name string
		run  func(context.Context, settings.Settings) error
	}{
		{"fetch", p.fetch},
		{"cluster", p.cluster},
		{"score", p.score},
		{"briefs", p.dueBriefs},
		{"alerts", p.alerts},
		{"ignores", p.markIgnored},
		{"learn", p.maybeLearn},
		{"prune", p.prune},
	}
	for _, step := range steps {
		if ctx.Err() != nil {
			return
		}
		err := step.run(ctx, s)
		p.status.record(step.name, p.Now(), err)
		// Waiting for the reader to add interests, or for the provider's limit
		// to reset (logged once when it starts), is a state, not a failure.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrNoInterests) && !errors.Is(err, ErrModelPaused) {
			p.Log.Error(step.name, "err", err)
		}
	}
}

// ErrModelPaused means the provider is out of requests for now.
var ErrModelPaused = errors.New("model paused")

// model returns the configured model, or an error explaining what is missing
// or that the provider is out of requests for now.
func (p *Pipeline) model(s settings.Settings) (llm.Model, error) {
	m, err := p.NewModel(s.Provider)
	if err != nil {
		return nil, err
	}
	pm := pausingModel{Model: m, p: p, loc: s.Location()}
	if p.Now().Before(p.modelPausedUntil) {
		return nil, pm.paused()
	}
	return pm, nil
}

// pausingModel stops calling a provider that answered 429 until the time it
// gave, so a daily cap (OpenRouter's free tier allows 50 requests a day)
// does not turn into a failed request and an error line every minute.
type pausingModel struct {
	llm.Model
	p   *Pipeline
	loc *time.Location
}

func (m pausingModel) JSON(ctx context.Context, req llm.Request) ([]byte, error) {
	if m.p.Now().Before(m.p.modelPausedUntil) {
		return nil, m.paused()
	}
	out, err := m.Model.JSON(ctx, req)
	var rl *llm.RateLimited
	if errors.As(err, &rl) {
		m.p.modelPausedUntil = rl.Until
		m.p.Log.Warn("model out of requests, pausing it", "until", rl.Until.In(m.loc).Format(time.DateTime), "err", err)
		return nil, m.paused()
	}
	return out, err
}

func (m pausingModel) paused() error {
	return fmt.Errorf("%w: %s is out of requests until %s", ErrModelPaused, m.Name(),
		m.p.modelPausedUntil.In(m.loc).Format("Mon 15:04"))
}

func (p *Pipeline) prune(ctx context.Context, _ settings.Settings) error {
	last := p.DB.MetaTime(ctx, "pruned_at")
	if p.Now().Sub(last) < 24*time.Hour {
		return nil
	}
	n, err := p.DB.Prune(ctx, p.Now().Add(-retention))
	if err != nil {
		return err
	}
	if n > 0 {
		p.Log.Info("pruned old stories", "stories", n)
	}
	return p.DB.SetMetaTime(ctx, "pruned_at", p.Now())
}

func (p *Pipeline) markIgnored(ctx context.Context, s settings.Settings) error {
	if s.Learning.IgnoreAfterHours == 0 {
		return nil
	}
	_, err := p.DB.MarkIgnored(ctx, p.Now().Add(-time.Duration(s.Learning.IgnoreAfterHours)*time.Hour))
	return err
}

// Status is what the health view shows: when each step last ran and how it went.
type Status struct {
	Steps []StepStatus
}

type StepStatus struct {
	Name      string
	LastRun   time.Time
	LastOK    time.Time
	LastError string
}

func (p *Pipeline) Status() Status { return p.status.snapshot() }

type statusTracker struct {
	mu    sync.Mutex
	steps []StepStatus
}

func (t *statusTracker) record(name string, at time.Time, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := -1
	for j, s := range t.steps {
		if s.Name == name {
			i = j
		}
	}
	if i < 0 {
		t.steps = append(t.steps, StepStatus{Name: name})
		i = len(t.steps) - 1
	}
	t.steps[i].LastRun = at
	if err == nil {
		t.steps[i].LastOK, t.steps[i].LastError = at, ""
	} else {
		t.steps[i].LastError = err.Error()
	}
}

func (t *statusTracker) snapshot() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Status{Steps: append([]StepStatus(nil), t.steps...)}
}

var _ Embedder = (*embed.Embedder)(nil)
var _ Sender = (*deliver.Deliverer)(nil)
