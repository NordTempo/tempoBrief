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

// The editorial policy is a short, readable list of rules the scorer follows,
// rewritten from reactions every few days. It is what makes the learning
// visible: the reader can read it, edit it, and roll it back.
// See docs/learning.md#policy.
const (
	LearnMinSignals = 10                  // new reactions needed before a rewrite
	learnEvery      = 72 * time.Hour      // at most this often
	learnLookback   = 60 * 24 * time.Hour // reactions the learner reads
	learnMaxSignals = 300
)

type policyAnswer struct {
	Policy  string   `json:"policy" desc:"the full new policy, as a bulleted list"`
	Changes []string `json:"changes" desc:"what changed from the current policy and which reactions caused it, one short line each; empty if nothing changed"`
}

func (p *Pipeline) maybeLearn(ctx context.Context, s settings.Settings) error {
	if !s.Learning.Policy || strings.TrimSpace(s.Interests) == "" {
		return nil
	}
	last := p.DB.MetaTime(ctx, "learned_at")
	if p.Now().Sub(last) < learnEvery {
		return nil
	}
	n, err := p.DB.CountSignals(ctx, last)
	if err != nil || n < LearnMinSignals {
		return err
	}
	_, err = p.learn(ctx, s)
	return err
}

// LearnNow rewrites the policy immediately ("Relearn now").
func (p *Pipeline) LearnNow(ctx context.Context) (store.Policy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, err := settings.Load(ctx, p.DB)
	if err != nil {
		return store.Policy{}, err
	}
	if strings.TrimSpace(s.Interests) == "" {
		return store.Policy{}, ErrNoInterests
	}
	return p.learn(ctx, s)
}

func (p *Pipeline) learn(ctx context.Context, s settings.Settings) (store.Policy, error) {
	model, err := p.model(s)
	if err != nil {
		return store.Policy{}, err
	}
	signals, err := p.DB.Signals(ctx, p.Now().Add(-learnLookback), learnMaxSignals, false)
	if err != nil {
		return store.Policy{}, err
	}
	if len(signals) == 0 {
		return store.Policy{}, errors.New("no reactions yet: vote on a few stories first")
	}
	current := "(none yet)"
	if cur, err := p.DB.CurrentPolicy(ctx); err == nil {
		current = cur.Text
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Policy{}, err
	}

	var lines []string
	for _, sg := range signals {
		line := fmt.Sprintf("- %s [%s] %s", sg.Kind, sg.Topic, sg.Title)
		if sg.Summary != "" {
			line += " | " + sg.Summary
		}
		lines = append(lines, line)
	}
	system := fmt.Sprintf(`You maintain the editorial policy for one reader's personal news brief. A
scoring model reads the policy together with the reader's own description of
their interests, and rates every incoming story with it.

<reader_interests>
%s
</reader_interests>

<current_policy>
%s
</current_policy>

The user message lists the reader's recent reactions, newest first. "liked" and
"disliked" are explicit votes and count most. "clicked" means they opened it
without voting, a mild positive. "ignored" means it was delivered and they
neither opened nor voted, a weak negative: one ignore says little, a pattern of
them says a lot.

Rewrite the policy so the scorer would have ranked these stories the way the
reader reacted. Rules:
- Bulleted list, at most 15 bullets, each at most 25 words, in English.
- Concrete and testable: name topics, kinds of story, sources or angles, and
  say whether to rank them up or down. No vague lines like "prefers quality".
- Keep what still holds, drop what the reactions contradict, add what they
  reveal. Do not invent preferences the reactions do not show.
- Refine the reader's interests, never contradict them. When reactions
  disagree with their stated interests, write the narrower rule the reactions
  support.`, strings.TrimSpace(s.Interests), current)

	answer, err := llm.Generate[policyAnswer](ctx, model, system, strings.Join(lines, "\n"), "medium")
	if err != nil {
		return store.Policy{}, err
	}
	policy := store.Policy{Source: "learned", Text: strings.TrimSpace(answer.Policy), Changes: answer.Changes, Signals: len(signals)}
	if policy.Text == "" {
		return store.Policy{}, errors.New("the model returned an empty policy")
	}
	id, err := p.DB.AddPolicy(ctx, policy)
	if err != nil {
		return store.Policy{}, err
	}
	policy.ID, policy.CreatedAt = id, p.Now()
	p.Log.Info("policy learned", "signals", len(signals), "changes", len(answer.Changes))
	return policy, p.DB.SetMetaTime(ctx, "learned_at", p.Now())
}
