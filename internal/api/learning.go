package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func policyView(p store.Policy) Policy {
	changes := p.Changes
	if changes == nil {
		changes = []string{}
	}
	return Policy{ID: p.ID, CreatedAt: timeString(p.CreatedAt), Source: p.Source, Text: p.Text, Changes: changes, Signals: p.Signals}
}

func (s *Server) learning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	history, err := s.DB.Policies(ctx, 50)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := Learning{History: []Policy{}, Recent: []Signal{}, LearnAfter: pipeline.LearnMinSignals}
	for i, p := range history {
		v := policyView(p)
		if i == 0 {
			out.Current = &v
		}
		out.History = append(out.History, v)
	}
	counts, err := s.DB.SignalCounts(ctx, s.now().Add(-60*24*time.Hour))
	if err != nil {
		s.internal(w, err)
		return
	}
	out.Counts = SignalCounts{
		Liked: counts[store.SignalLiked], Disliked: counts[store.SignalDisliked],
		Clicked: counts[store.SignalClicked], Ignored: counts[store.SignalIgnored],
		Missed: counts[store.SignalMissed],
	}
	last := s.DB.MetaTime(ctx, "learned_at")
	out.LastLearnedAt = timeString(last)
	if out.SinceLastLearn, err = s.DB.CountSignals(ctx, last); err != nil {
		s.internal(w, err)
		return
	}
	recent, err := s.DB.Signals(ctx, s.now().Add(-60*24*time.Hour), 50, false)
	if err != nil {
		s.internal(w, err)
		return
	}
	for _, sg := range recent {
		out.Recent = append(out.Recent, Signal{StoryID: sg.StoryID, Kind: string(sg.Kind), Topic: sg.Topic, Title: sg.Title, Note: sg.Note, At: timeString(sg.At)})
	}
	writeJSON(w, http.StatusOK, out)
}

// editPolicy saves a hand-edited policy as a new version.
func (s *Server) editPolicy(w http.ResponseWriter, r *http.Request) {
	var body PolicyRequest
	if !readJSON(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeFieldError(w, "text", "the policy cannot be empty")
		return
	}
	if len(text) > 8000 {
		writeFieldError(w, "text", "keep the policy under 8000 characters")
		return
	}
	s.savePolicy(w, r, store.Policy{Source: "edited", Text: text, Changes: []string{"Edited by hand"}})
}

func (s *Server) restorePolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	history, err := s.DB.Policies(r.Context(), 1000)
	if err != nil {
		s.internal(w, err)
		return
	}
	for _, p := range history {
		if p.ID == id {
			s.savePolicy(w, r, store.Policy{Source: "edited", Text: p.Text, Changes: []string{"Restored the version from " + p.CreatedAt.UTC().Format("2 Jan 2006")}})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not_found", "no such policy version")
}

func (s *Server) savePolicy(w http.ResponseWriter, r *http.Request, p store.Policy) {
	id, err := s.DB.AddPolicy(r.Context(), p)
	if err != nil {
		s.internal(w, err)
		return
	}
	p.ID, p.CreatedAt = id, s.now()
	writeJSON(w, http.StatusOK, policyView(p))
}

func (s *Server) learnNow(w http.ResponseWriter, r *http.Request) {
	p, err := s.Pipeline.LearnNow(r.Context())
	if errors.Is(err, pipeline.ErrNoInterests) {
		writeError(w, http.StatusConflict, "no_interests", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "learn_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policyView(p))
}
