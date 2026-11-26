package pipeline

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

const (
	scoreBatch  = 40
	scoreWindow = 24 * time.Hour
	// Full batches are scored right away; smaller ones at most this often.
	// Each batch is one request: every 45 minutes is 32 a day, which leaves
	// room in a free tier's 50 for full batches, duplicate checks and learning.
	scoreEvery = 45 * time.Minute
	// Reactions older than this no longer shape taste.
	tasteMemory = 180 * 24 * time.Hour
	// Most recent votes shown to the model as examples.
	promptExamples = 12
	// Scored stories at least this similar to a new one are shown to the
	// model as possible duplicates. Low on purpose: in this band embeddings
	// cannot tell the same event from a related one (see docs/pipeline.md), so
	// the model decides. At most sameEventPerStory per story.
	sameEventFloor    = 0.85
	sameEventPerStory = 3
)

// ErrNoInterests stops scoring until the reader has said what they want.
var ErrNoInterests = errors.New("add your interests in Settings to start scoring")

type scoreAnswer struct {
	Stories []struct {
		ID      int64  `json:"id"`
		Score   int    `json:"score" desc:"0 to 10"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
		SameAs  int64  `json:"sameAs" desc:"id of an earlier or listed story about the same event, else 0"`
	} `json:"stories"`
}

// score rates every unscored story from the last day, in batches of
// scoreBatch, holding back a last smaller batch until scoreEvery has passed.
func (p *Pipeline) score(ctx context.Context, s settings.Settings) error {
	if strings.TrimSpace(s.Interests) == "" {
		return ErrNoInterests
	}
	model, err := p.model(s)
	if err != nil {
		return err
	}
	var signals []store.Signal
	if s.Learning.Taste {
		if signals, err = p.DB.Signals(ctx, p.Now().Add(-tasteMemory), 5000, true); err != nil {
			return err
		}
	}
	system, err := p.scoringPrompt(ctx, s)
	if err != nil {
		return err
	}
	for {
		batch, err := p.DB.UnscoredStories(ctx, p.Now().Add(-scoreWindow), scoreBatch)
		if err != nil || len(batch) == 0 {
			return err
		}
		if len(batch) < scoreBatch {
			if p.Now().Sub(p.lastPartial) < p.scoreEvery {
				return nil
			}
			p.lastPartial = p.Now()
		}
		done, err := p.scoreBatch(ctx, model, system, batch, signals, s.Learning.Taste)
		if err != nil {
			return err
		}
		// A batch the model answers nothing for would otherwise loop forever.
		if done == 0 {
			return fmt.Errorf("%s returned no scores for %d stories", model.Name(), len(batch))
		}
	}
}

func (p *Pipeline) scoreBatch(ctx context.Context, model llm.Model, system string, batch []store.Story, signals []store.Signal, useTaste bool) (int, error) {
	vectors, earlier, err := p.sameEventCandidates(ctx, batch)
	if err != nil {
		return 0, err
	}
	lines := make([]string, len(batch))
	for i, st := range batch {
		lines[i] = describe(st)
	}
	prompt := strings.Join(lines, "\n")
	if len(earlier) > 0 {
		prev := make([]string, len(earlier))
		for i, st := range earlier {
			prev[i] = fmt.Sprintf("earlier id=%d | %s | %s", st.ID, st.Title, st.Summary)
		}
		prompt += "\n\nEarlier stories, already scored. Only for sameAs; return no entries for them:\n" + strings.Join(prev, "\n")
	}
	answer, err := llm.Generate[scoreAnswer](ctx, model, system, prompt, "low")
	if err != nil {
		return 0, err
	}
	// The setup checklist reads this to know the provider works.
	p.DB.SetMeta(ctx, "provider_ok", model.Name())
	byID := make(map[int64]store.Story, len(batch))
	for _, st := range batch {
		byID[st.ID] = st
	}
	if !useTaste {
		signals = nil
	}
	var out []store.Scored
	for _, a := range answer.Stories {
		if _, ok := byID[a.ID]; !ok {
			continue
		}
		delete(byID, a.ID)
		llmScore := max(0, min(10, a.Score))
		var t TasteResult
		if v := vectors[a.ID]; v != nil && len(signals) > 0 {
			t = Taste(v, signals)
		}
		sc := store.Scored{
			ID: a.ID, LLMScore: llmScore, Score: FinalScore(llmScore, t),
			Summary: strings.TrimSpace(a.Summary), Reason: strings.TrimSpace(a.Reason),
		}
		if t.Neighbors > 0 {
			sc.Taste, sc.TasteN = &t.Value, t.Neighbors
		}
		out = append(out, sc)
	}
	if err := p.DB.SaveScores(ctx, out); err != nil {
		return 0, err
	}
	p.mergeSameEvent(ctx, answer, batch, earlier)
	return len(out), nil
}

// sameEventCandidates loads the batch's vectors (the story list query skips
// them) and the scored stories most similar to each, for the sameAs check.
func (p *Pipeline) sameEventCandidates(ctx context.Context, batch []store.Story) (map[int64][]float32, []store.Story, error) {
	open, err := p.DB.OpenCentroids(ctx, p.Now().Add(-scoreWindow-storyWindow))
	if err != nil {
		return nil, nil, err
	}
	vectors := make(map[int64][]float32, len(batch))
	for _, st := range batch {
		vectors[st.ID] = nil
	}
	for _, c := range open {
		if _, ok := vectors[c.StoryID]; ok {
			vectors[c.StoryID] = c.Vector
		}
	}
	type near struct {
		id  int64
		sim float32
	}
	picked := map[int64]bool{}
	var ids []int64
	for _, st := range batch {
		v := vectors[st.ID]
		if v == nil {
			continue
		}
		var best []near
		for _, c := range open {
			if _, inBatch := vectors[c.StoryID]; inBatch {
				continue // the model sees the whole batch anyway
			}
			if sim := embed.Similarity(v, c.Vector); sim >= sameEventFloor {
				best = append(best, near{c.StoryID, sim})
			}
		}
		slices.SortFunc(best, func(a, b near) int { return cmp.Compare(b.sim, a.sim) })
		for _, n := range best[:min(len(best), sameEventPerStory)] {
			if !picked[n.id] {
				picked[n.id] = true
				ids = append(ids, n.id)
			}
		}
	}
	if len(ids) == 0 {
		return vectors, nil, nil
	}
	list, err := p.DB.StoriesByID(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	earlier := list[:0]
	for _, st := range list {
		if st.Score != nil { // unscored ones are in a later batch and get their own check
			earlier = append(earlier, st)
		}
	}
	return vectors, earlier, nil
}

// mergeSameEvent merges stories the model marked as the same event. Each group
// keeps its oldest story (the lowest id), which may be one already shown or
// briefed: a later duplicate of a briefed story is then not briefed again.
func (p *Pipeline) mergeSameEvent(ctx context.Context, answer scoreAnswer, batch, earlier []store.Story) {
	valid := map[int64]bool{}
	for _, st := range batch {
		valid[st.ID] = true
	}
	for _, st := range earlier {
		valid[st.ID] = true
	}
	// Union-find whose root is the smallest id, so chains and cycles settle.
	parent := map[int64]int64{}
	var root func(int64) int64
	root = func(id int64) int64 {
		if up, ok := parent[id]; ok && up != id {
			r := root(up)
			parent[id] = r
			return r
		}
		return id
	}
	for _, a := range answer.Stories {
		if a.SameAs == 0 || a.SameAs == a.ID || !valid[a.ID] || !valid[a.SameAs] {
			continue
		}
		ra, rb := root(a.ID), root(a.SameAs)
		if ra != rb {
			parent[max(ra, rb)] = min(ra, rb)
		}
	}
	groups := map[int64][]int64{}
	for id := range parent {
		if r := root(id); r != id {
			groups[r] = append(groups[r], id)
		}
	}
	for keep, drop := range groups {
		if err := p.DB.MergeStories(ctx, keep, drop); err != nil {
			p.Log.Warn("merge same event", "err", err)
			continue
		}
		p.Log.Info("merged same event", "keep", keep, "merged", drop)
	}
}

func describe(st store.Story) string {
	snippet := st.Snippet
	if r := []rune(snippet); len(r) > 240 {
		snippet = string(r[:240])
	}
	return fmt.Sprintf("id=%d | %s | %d outlet(s): %s | %s | %s",
		st.ID, st.Topic, len(st.Sources), strings.Join(st.Sources, ", "), st.Title, snippet)
}

// scoringPrompt is rebuilt every run, so edits to interests and the policy
// apply on the next tick.
func (p *Pipeline) scoringPrompt(ctx context.Context, s settings.Settings) (string, error) {
	policy := "(none yet: it is learned from reactions after the first days)"
	if cur, err := p.DB.CurrentPolicy(ctx); err == nil {
		policy = cur.Text
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	signals, err := p.DB.Signals(ctx, p.Now().Add(-tasteMemory), 400, false)
	if err != nil {
		return "", err
	}
	liked, disliked := examples(signals, store.SignalLiked), examples(signals, store.SignalDisliked)
	missed := examples(signals, store.SignalMissed)

	return fmt.Sprintf(`You pick the news one specific reader wants to see. Each user message lists
candidate stories, one per line: id, topic, how many outlets covered it, title,
and an excerpt.

<reader_interests>
%s
</reader_interests>

The editorial policy below was distilled from how this reader actually reacted
to past stories. Where it is more specific than their interests, follow it.

<editorial_policy>
%s
</editorial_policy>

Recent votes, the most direct evidence of taste:

<liked>
%s
</liked>

<disliked>
%s
</disliked>

Stories the reader found elsewhere and reported as missed: they wanted these
and did not get them, so similar stories deserve 7 or more. Their own words
follow "reader:" where given.

<missed>
%s
</missed>

Return one entry per story:
- score, 0 to 10: how much this reader would want to see it today.
  9-10 they would be annoyed to miss it, 7-8 clearly worth their time,
  5-6 mildly interesting, 0-4 skip. Most stories should score 4 or lower.
  Wide coverage across outlets signals importance for general news,
  but it never overrides the reader's interests.
- summary: one plain sentence, at most 25 words, saying concretely what happened.
  Written in %s. No hype.
- reason: at most 10 words, in %s, on why it does or does not fit this reader.
- sameAs: the id of a story that reports the same event as this one, from the
  earlier stories listed after the candidates or from the candidates
  themselves; otherwise 0. The same event means the same incident, release,
  announcement or study, also when written in another language or from
  another angle. A different event on the same topic is not the same.`,
		strings.TrimSpace(s.Interests), policy, liked, disliked, missed, s.Language, s.Language), nil
}

func examples(signals []store.Signal, kind store.SignalKind) string {
	var lines []string
	for _, sg := range signals {
		if sg.Kind == kind {
			line := fmt.Sprintf("- [%s] %s", sg.Topic, sg.Title)
			if sg.Note != "" {
				line += " | reader: " + sg.Note
			}
			lines = append(lines, line)
			if len(lines) == promptExamples {
				break
			}
		}
	}
	if len(lines) == 0 {
		return "(none yet)"
	}
	return strings.Join(lines, "\n")
}
