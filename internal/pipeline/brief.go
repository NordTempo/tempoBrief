package pipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

const (
	briefWindow = 24 * time.Hour
	// A brief whose slot passed longer ago than this (the server was down) is
	// skipped rather than sent late.
	briefGrace  = 3 * time.Hour
	alertWindow = 6 * time.Hour
)

// SlotDue reports whether a schedule's brief for today is due at now: the slot
// has passed, by less than the grace period, on an allowed weekday, and no
// brief was sent since the slot (or since install, so a fresh install does not
// send a brief of nothing).
func SlotDue(b settings.BriefSchedule, now time.Time, loc *time.Location, last, installed time.Time) (time.Time, bool) {
	local := now.In(loc)
	hour, _ := strconv.Atoi(b.Time[:2])
	minute, _ := strconv.Atoi(b.Time[3:])
	slot := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !b.Enabled || local.Before(slot) || local.Sub(slot) > briefGrace {
		return slot, false
	}
	weekday := int(local.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	if len(b.Days) > 0 && !slices.Contains(b.Days, weekday) {
		return slot, false
	}
	return slot, last.Before(slot) && !installed.After(slot)
}

func (p *Pipeline) dueBriefs(ctx context.Context, s settings.Settings) error {
	installed := p.DB.MetaTime(ctx, "installed_at")
	var errs []error
	for _, b := range s.Briefs {
		last, err := p.DB.LastBriefAt(ctx, b.ID)
		if err != nil {
			return err
		}
		if _, due := SlotDue(b, p.Now(), s.Location(), last, installed); !due {
			continue
		}
		if _, err := p.sendBrief(ctx, s, b); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b.Name, err))
		}
	}
	return errors.Join(errs...)
}

// SendBriefNow builds and sends a schedule's brief immediately ("Send now").
func (p *Pipeline) SendBriefNow(ctx context.Context, scheduleID string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, err := settings.Load(ctx, p.DB)
	if err != nil {
		return 0, err
	}
	for _, b := range s.Briefs {
		if b.ID == scheduleID {
			return p.sendBrief(ctx, s, b)
		}
	}
	return 0, store.ErrNotFound
}

// sendBrief picks the brief's stories, saves the brief, delivers it to its
// channels and marks the stories as briefed. The brief is saved even when
// every delivery fails, so the web app and Home Assistant still show it.
func (p *Pipeline) sendBrief(ctx context.Context, s settings.Settings, b settings.BriefSchedule) (int64, error) {
	now := p.Now()
	candidates, err := p.DB.BriefCandidates(ctx, now.Add(-briefWindow), float64(b.MinScore))
	if err != nil {
		return 0, err
	}
	// The best stories from any topic, nothing added to fill a topic. Twice
	// the cap go to the duplicate merge, so there is room left after it.
	best := candidates[:min(len(candidates), b.MaxStories*2)]
	kept, dropped := best, []int64(nil)
	if model, err := p.model(s); err == nil {
		kept, dropped = p.mergeDuplicates(ctx, model, best)
	}
	sections := sections(kept[:min(len(kept), b.MaxStories)])
	title := fmt.Sprintf("%s · %s", b.Name, now.In(s.Location()).Format("Monday 2 January"))

	var ids []int64
	for _, sec := range sections {
		for _, st := range sec.stories {
			ids = append(ids, st.ID)
		}
	}
	briefID, err := p.DB.AddBrief(ctx, store.Brief{ScheduleID: b.ID, Title: title, CreatedAt: now, StoryIDs: ids})
	if err != nil {
		return 0, err
	}
	if err := p.DB.MarkBriefed(ctx, append(ids, dropped...)); err != nil {
		return briefID, err
	}

	msg := p.message(deliver.KindBrief, title, now.In(s.Location()).Format("2006-01-02"), briefID, s, sections)
	deliveries := p.deliverAll(ctx, s.ChannelsFor(b.Channels), msg)
	if err := p.DB.SetDeliveries(ctx, briefID, deliveries); err != nil {
		return briefID, err
	}
	p.Log.Info("brief sent", "schedule", b.ID, "stories", len(ids), "channels", len(deliveries))
	var errs []error
	for _, d := range deliveries {
		if !d.OK {
			errs = append(errs, fmt.Errorf("%s: %s", d.Channel, d.Error))
		}
	}
	return briefID, errors.Join(errs...)
}

func (p *Pipeline) deliverAll(ctx context.Context, channels []settings.Channel, msg deliver.Message) []store.Delivery {
	var out []store.Delivery
	for _, c := range channels {
		err := p.Sender.Send(ctx, c, msg)
		d := store.Delivery{Channel: c.ID, OK: err == nil}
		if err != nil {
			d.Error = err.Error()
			p.Log.Error("delivery failed", "channel", c.ID, "err", err)
		}
		out = append(out, d)
	}
	return out
}

// alerts pushes fresh, high-scoring stories that several outlets cover.
func (p *Pipeline) alerts(ctx context.Context, s settings.Settings) error {
	if !s.Alerts.Enabled {
		return nil
	}
	list, err := p.DB.AlertCandidates(ctx, p.Now().Add(-alertWindow), float64(s.Alerts.MinScore), s.Alerts.MinSources)
	if err != nil || len(list) == 0 {
		return err
	}
	channels := s.ChannelsFor(s.Alerts.Channels)
	var errs []error
	for _, st := range list {
		msg := p.message(deliver.KindAlert, st.Title, p.Now().In(s.Location()).Format("2006-01-02"), 0, s,
			[]section{{topic: st.Topic, stories: []store.Story{st}}})
		for _, d := range p.deliverAll(ctx, channels, msg) {
			if !d.OK {
				errs = append(errs, fmt.Errorf("%s: %s", d.Channel, d.Error))
			}
		}
		if err := p.DB.MarkAlerted(ctx, []int64{st.ID}); err != nil {
			return err
		}
	}
	return errors.Join(errs...)
}

type section struct {
	topic   string
	stories []store.Story
}

// sections groups stories by topic; topics are ordered by their best story.
func sections(list []store.Story) []section {
	var out []section
	index := map[string]int{}
	for _, st := range list {
		i, ok := index[st.Topic]
		if !ok {
			i = len(out)
			index[st.Topic] = i
			out = append(out, section{topic: st.Topic})
		}
		out[i].stories = append(out[i].stories, st)
	}
	best := func(s section) float64 { return deref(s.stories[0].Score) }
	sort.SliceStable(out, func(i, j int) bool { return best(out[i]) > best(out[j]) })
	return out
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

type duplicateAnswer struct {
	Groups [][]int64 `json:"groups" desc:"groups of two or more ids that report the same event"`
}

// mergeDuplicates catches same-event stories that grouping left apart (the
// same event in two languages, two phrasings of one accident). Each group is
// merged into its best-scored story, which then lists every outlet; stories
// must be sorted best first. On failure the brief goes out without it.
func (p *Pipeline) mergeDuplicates(ctx context.Context, model llm.Model, list []store.Story) ([]store.Story, []int64) {
	if len(list) < 2 {
		return list, nil
	}
	lines := make([]string, len(list))
	for i, st := range list {
		lines[i] = fmt.Sprintf("id=%d | %s | %s", st.ID, st.Title, st.Summary)
	}
	answer, err := llm.Generate[duplicateAnswer](ctx, model,
		`Group the ids of news stories that report the same event: the same incident,
release, announcement or study, even when written in different languages.
Different events on the same topic are not duplicates. Return only groups of two or more ids.`,
		strings.Join(lines, "\n"), "low")
	if err != nil {
		p.Log.Warn("duplicate check failed, sending without it", "err", err)
		return list, nil
	}
	drop := map[int64]bool{}
	var dropped []int64
	for _, group := range answer.Groups {
		keep := int64(-1)
		for _, st := range list {
			if slices.Contains(group, st.ID) && !drop[st.ID] {
				keep = st.ID
				break
			}
		}
		var merge []int64
		for _, id := range group {
			if keep >= 0 && id != keep && !drop[id] && slices.ContainsFunc(list, func(st store.Story) bool { return st.ID == id }) {
				drop[id] = true
				merge = append(merge, id)
			}
		}
		if err := p.DB.MergeStories(ctx, keep, merge); err != nil {
			p.Log.Warn("merge duplicates", "err", err)
			continue
		}
		dropped = append(dropped, merge...)
	}
	var keptIDs []int64
	for _, st := range list {
		if !drop[st.ID] {
			keptIDs = append(keptIDs, st.ID)
		}
	}
	// Reload, so merged stories carry their new sources.
	kept, err := p.DB.StoriesByID(ctx, keptIDs)
	if err != nil {
		p.Log.Warn("reload merged stories", "err", err)
		return list, nil
	}
	return kept, dropped
}

// message renders stories for delivery, with tracked and vote links when a
// public URL is configured.
func (p *Pipeline) message(kind deliver.Kind, title, date string, briefID int64, s settings.Settings, secs []section) deliver.Message {
	links := deliver.Links{Base: p.Config.PublicURL, Secret: s.Secret}
	m := deliver.Message{Kind: kind, Title: title, Date: date}
	if briefID > 0 {
		m.BriefURL = links.Brief(briefID)
	}
	for _, sec := range secs {
		out := deliver.Section{Topic: sec.topic}
		for _, st := range sec.stories {
			out.Stories = append(out.Stories, deliver.Story{
				ID: st.ID, Title: st.Title, Summary: st.Summary, URL: links.Click(st.ID, st.URL), DirectURL: st.URL,
				Score: deref(st.Score), Sources: st.Sources, Why: Why(st), Vote: st.Vote,
				UpURL: links.Vote(st.ID, 1), DownURL: links.Vote(st.ID, -1),
			})
		}
		m.Sections = append(m.Sections, out)
	}
	return m
}

// MessageForBrief rebuilds a stored brief for display or the JSON endpoint.
func (p *Pipeline) MessageForBrief(ctx context.Context, s settings.Settings, b store.Brief) (deliver.Message, error) {
	stories, err := p.DB.StoriesByID(ctx, b.StoryIDs)
	if err != nil {
		return deliver.Message{}, err
	}
	// Keep the brief's order; sections() orders topics by their best story.
	return p.message(deliver.KindBrief, b.Title, b.CreatedAt.In(s.Location()).Format("2006-01-02"), b.ID, s, sections(stories)), nil
}

// Why is the short "why it ranked here" line: the model's reason, plus what
// taste contributed when it moved the score.
func Why(st store.Story) string {
	parts := []string{}
	if st.Reason != "" {
		parts = append(parts, st.Reason)
	}
	if st.Taste != nil && st.TasteN > 0 && st.LLMScore != nil && st.Score != nil {
		delta := *st.Score - float64(*st.LLMScore)
		switch {
		case delta >= 0.25:
			parts = append(parts, fmt.Sprintf("like %d you liked", st.TasteN))
		case delta <= -0.25:
			parts = append(parts, fmt.Sprintf("like %d you passed on", st.TasteN))
		}
	}
	return strings.Join(parts, "; ")
}
