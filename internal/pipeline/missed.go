package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// A missed story is one the reader heard about elsewhere and reports with its
// link. ReportMissed finds out why they did not get it, then records the miss,
// which taste and the policy learner treat as the strongest positive signal.
// See docs/learning.md#missed.

const (
	// A reported article whose title is this close to a story's is that story.
	// High on purpose: in the 0.85 to 0.93 band titles may be different events.
	missedSameTitle = 0.93
	// How far back a reported article is looked for.
	missedLookback = 7 * 24 * time.Hour
)

// MissedCase is why the reader did not get a story.
type MissedCase string

const (
	MissedNotFetched MissedCase = "not_fetched" // no feed carried it
	MissedNotScored  MissedCase = "not_scored"  // fetched, never scored
	MissedLowScore   MissedCase = "low_score"   // scored under the brief bar
	MissedQueued     MissedCase = "queued"      // good enough, the next brief has it
	MissedLeftOut    MissedCase = "left_out"    // good enough, but its topic was full
	MissedBriefed    MissedCase = "briefed"     // it went out in a brief
)

type MissedReport struct {
	Story store.Story
	Case  MissedCase
	// Lines explain the case in plain words, for the reader.
	Lines []string
	// AddedFeed is the feed added so stories like this one arrive from now
	// on (not_fetched only); FeedID is 0 when none was found.
	AddedFeed feeds.Found
	FeedID    int64
}

// ErrUnreadable means the link matched no story and its page could not be read.
var ErrUnreadable = errors.New("could not read that page")

// ReportMissed records that the reader should have got the article at link.
func (p *Pipeline) ReportMissed(ctx context.Context, link, note string) (MissedReport, error) {
	s, err := settings.Load(ctx, p.DB)
	if err != nil {
		return MissedReport{}, err
	}
	st, err := p.DB.StoryByURL(ctx, link)
	if errors.Is(err, store.ErrNotFound) {
		return p.missedByPage(ctx, s, link, note)
	}
	if err != nil {
		return MissedReport{}, err
	}
	return p.recordMissed(ctx, s, st, note)
}

// missedByPage reads the article, looks for a story with the same title, and
// otherwise stores the article itself as a missed story.
func (p *Pipeline) missedByPage(ctx context.Context, s settings.Settings, link, note string) (MissedReport, error) {
	page, err := p.Fetcher.Page(ctx, link)
	if err == nil && page.Title == "" {
		err = errors.New("no title")
	}
	if err != nil {
		return MissedReport{}, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	vecs, err := p.Embedder.Embed(ctx, []string{page.Title})
	if err != nil {
		return MissedReport{}, err
	}
	near, err := p.DB.OpenCentroids(ctx, p.Now().Add(-missedLookback))
	if err != nil {
		return MissedReport{}, err
	}
	var best store.Centroid
	var bestSim float32
	for _, c := range near {
		if sim := embed.Cosine(vecs[0], c.Vector); sim > bestSim {
			best, bestSim = c, sim
		}
	}
	if bestSim >= missedSameTitle {
		st, err := p.DB.Story(ctx, best.StoryID)
		if err != nil {
			return MissedReport{}, err
		}
		return p.recordMissed(ctx, s, st, note)
	}

	// No feed had it. File it under the topic of the closest story, so it
	// lands where the reader would look for it.
	topic := "General"
	if bestSim >= tasteFloor {
		if st, err := p.DB.Story(ctx, best.StoryID); err == nil {
			topic = st.Topic
		}
	}
	id, err := p.DB.AddMissedStory(ctx, store.MissedStory{Topic: topic, Title: page.Title, URL: link,
		Snippet: page.Description, Note: note, Vector: vecs[0]})
	if err != nil {
		return MissedReport{}, err
	}
	st, err := p.DB.Story(ctx, id)
	if err != nil {
		return MissedReport{}, err
	}
	report := MissedReport{Story: st, Case: MissedNotFetched}
	if report.AddedFeed, report.FeedID, err = p.addFeedFor(ctx, link, page, topic); err != nil {
		return MissedReport{}, err
	}
	site := page.Site
	if site == "" {
		site = strings.TrimPrefix(hostOf(link), "www.")
	}
	f := report.AddedFeed
	volume := fmt.Sprintf("about %d items a day", f.PerDay)
	if f.PerDay == 0 {
		volume = "less than an item a day"
	}
	switch {
	case report.FeedID == 0:
		report.Lines = []string{fmt.Sprintf("None of your feeds had this, and tempoBrief found no feed for %s.", site)}
	case f.HasArticle:
		report.Lines = []string{fmt.Sprintf("None of your feeds had this. Added %s to %s: it carries this article, %s.", f.Title, topic, volume)}
	default:
		report.Lines = []string{fmt.Sprintf("None of your feeds had this. Added %s to %s, the closest feed for %s, %s.", f.Title, topic, site, volume)}
	}
	return report, nil
}

// addFeedFor adds the best feed Discover finds for the article that is not
// added yet, under topic. It returns the feed and its ID, 0 if none.
func (p *Pipeline) addFeedFor(ctx context.Context, link string, page feeds.Page, topic string) (feeds.Found, int64, error) {
	have, err := p.DB.Feeds(ctx)
	if err != nil {
		return feeds.Found{}, 0, err
	}
	added := map[string]bool{}
	for _, f := range have {
		added[f.URL] = true
	}
	for _, f := range p.Fetcher.Discover(ctx, link, page.Title, page) {
		if added[f.URL] {
			continue
		}
		if _, err := p.DB.AddFeeds(ctx, []store.NewFeed{{URL: f.URL, Title: f.Title, Topic: topic}}); err != nil {
			return feeds.Found{}, 0, err
		}
		list, err := p.DB.Feeds(ctx)
		if err != nil {
			return feeds.Found{}, 0, err
		}
		for _, nf := range list {
			if nf.URL == f.URL {
				p.Log.Info("feed added for a missed story", "feed", f.URL, "has_article", f.HasArticle)
				return f, nf.ID, nil
			}
		}
	}
	return feeds.Found{}, 0, nil
}

func hostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	return u.Host
}

func (p *Pipeline) recordMissed(ctx context.Context, s settings.Settings, st store.Story, note string) (MissedReport, error) {
	if err := p.DB.SetMissed(ctx, st.ID, note); err != nil {
		return MissedReport{}, err
	}
	st.Missed, st.MissedNote = true, strings.TrimSpace(note)
	if st.NItems == 0 && st.Score == nil {
		// Reported before, and no feed has had it since.
		return MissedReport{Story: st, Case: MissedNotFetched, Lines: []string{"Already noted: none of your feeds has had this yet."}}, nil
	}
	c, lines := p.diagnose(s, st)
	return MissedReport{Story: st, Case: c, Lines: lines}, nil
}

// diagnose says why a fetched story did not reach the reader.
func (p *Pipeline) diagnose(s settings.Settings, st store.Story) (MissedCase, []string) {
	loc := s.Location()
	seen := st.FirstSeen.In(loc).Format("Mon 2 Jan 15:04")
	if st.Score == nil {
		return MissedNotScored, []string{fmt.Sprintf("Fetched %s, but never scored. Scoring may have been stopped by a model error or rate limit.", seen)}
	}
	score := fmt.Sprintf("%.1f", *st.Score)
	why := ""
	if st.Reason != "" {
		why = fmt.Sprintf(" The model said: %q.", st.Reason)
	}

	// The brief with the lowest bar decides whether it was good enough.
	var brief *settings.BriefSchedule
	for i, b := range s.Briefs {
		if b.Enabled && (brief == nil || b.MinScore < brief.MinScore) {
			brief = &s.Briefs[i]
		}
	}
	var c MissedCase
	var lines []string
	switch {
	case brief == nil:
		c, lines = MissedLowScore, []string{fmt.Sprintf("Scored %s.%s No brief is turned on.", score, why)}
	case *st.Score < float64(brief.MinScore):
		c, lines = MissedLowScore, []string{fmt.Sprintf("Scored %s.%s The %s needs %d.", score, why, strings.ToLower(brief.Name), brief.MinScore)}
	case st.BriefedAt != nil:
		c, lines = MissedBriefed, []string{fmt.Sprintf("Scored %s and went out in a brief on %s.",
			score, time.Unix(*st.BriefedAt, 0).In(loc).Format("Mon 2 Jan"))}
	case p.Now().Sub(st.FirstSeen) < briefWindow:
		c, lines = MissedQueued, []string{fmt.Sprintf("Scored %s, so it is in line for the next %s.", score, strings.ToLower(brief.Name))}
	default:
		c, lines = MissedLeftOut, []string{fmt.Sprintf("Scored %s but was left out: a brief takes at most %d stories per topic, and %s had higher scorers.",
			score, brief.MaxPerTopic, st.Topic)}
	}

	a := s.Alerts
	switch {
	case !a.Enabled:
		lines = append(lines, "Alerts are off.")
	case *st.Score < float64(a.MinScore) || len(st.Sources) < a.MinSources:
		lines = append(lines, fmt.Sprintf("Alerts need %d and %d outlets; it had %s and %d.", a.MinScore, a.MinSources, score, len(st.Sources)))
	}
	return c, lines
}
