package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

const (
	scoreBatch  = 40
	scoreWindow = 24 * time.Hour
	// Reactions older than this no longer shape taste.
	tasteMemory = 180 * 24 * time.Hour
	// Most recent votes shown to the model as examples.
	promptExamples = 12
)

// ErrNoInterests stops scoring until the reader has said what they want.
var ErrNoInterests = errors.New("add your interests in Settings to start scoring")

type scoreAnswer struct {
	Stories []struct {
		ID      int64  `json:"id"`
		Score   int    `json:"score" desc:"0 to 10"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	} `json:"stories"`
}

// score rates every unscored story from the last day.
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
	lines := make([]string, len(batch))
	for i, st := range batch {
		lines[i] = describe(st)
	}
	answer, err := llm.Generate[scoreAnswer](ctx, model, system, strings.Join(lines, "\n"), "low")
	if err != nil {
		return 0, err
	}
	// The setup checklist reads this to know the provider works.
	p.DB.SetMeta(ctx, "provider_ok", model.Name())
	byID := make(map[int64]store.Story, len(batch))
	for _, st := range batch {
		byID[st.ID] = st
	}
	var vectors map[int64][]float32
	if useTaste && len(signals) > 0 {
		if vectors, err = p.storyVectors(ctx, batch); err != nil {
			return 0, err
		}
	}
	var out []store.Scored
	for _, a := range answer.Stories {
		if _, ok := byID[a.ID]; !ok {
			continue
		}
		delete(byID, a.ID)
		llmScore := max(0, min(10, a.Score))
		var t TasteResult
		if v := vectors[a.ID]; v != nil {
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
	return len(out), p.DB.SaveScores(ctx, out)
}

// storyVectors loads centroids for a batch (the story list query skips them).
func (p *Pipeline) storyVectors(ctx context.Context, batch []store.Story) (map[int64][]float32, error) {
	open, err := p.DB.OpenCentroids(ctx, p.Now().Add(-scoreWindow-storyWindow))
	if err != nil {
		return nil, err
	}
	want := make(map[int64]bool, len(batch))
	for _, st := range batch {
		want[st.ID] = true
	}
	out := make(map[int64][]float32, len(batch))
	for _, c := range open {
		if want[c.StoryID] {
			out[c.StoryID] = c.Vector
		}
	}
	return out, nil
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

Return one entry per story:
- score, 0 to 10: how much this reader would want to see it today.
  9-10 they would be annoyed to miss it, 7-8 clearly worth their time,
  5-6 mildly interesting, 0-4 skip. Most stories should score 4 or lower.
  Wide coverage across outlets signals importance for general news,
  but it never overrides the reader's interests.
- summary: one plain sentence, at most 25 words, saying concretely what happened.
  Written in %s. No hype.
- reason: at most 10 words, in %s, on why it does or does not fit this reader.`,
		strings.TrimSpace(s.Interests), policy, liked, disliked, s.Language, s.Language), nil
}

func examples(signals []store.Signal, kind store.SignalKind) string {
	var lines []string
	for _, sg := range signals {
		if sg.Kind == kind {
			lines = append(lines, fmt.Sprintf("- [%s] %s", sg.Topic, sg.Title))
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
