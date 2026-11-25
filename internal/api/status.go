package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// status powers the health view and the first-run checklist.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	out := Status{Version: s.Version, PublicURL: s.Config.PublicURL, Steps: []Step{}, Setup: []string{}}
	if m, err := llm.New(st.Provider); err == nil {
		out.Provider = m.Name()
	}
	for _, step := range s.Pipeline.Status().Steps {
		out.Steps = append(out.Steps, Step{Name: step.Name, LastRun: timeString(step.LastRun), LastOK: timeString(step.LastOK), LastError: step.LastError})
	}
	feeds, err := s.DB.Feeds(ctx)
	if err != nil {
		s.internal(w, err)
		return
	}
	for _, f := range feeds {
		if f.Enabled {
			out.Feeds++
			if f.LastError != "" {
				out.Failing++
			}
		}
	}
	if out.Stories, err = s.DB.CountScored(ctx, s.now().Add(-24*time.Hour)); err != nil {
		s.internal(w, err)
		return
	}
	if b, err := s.DB.LatestBrief(ctx); err == nil {
		sum := briefSummary(b)
		out.LastBrief = &sum
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internal(w, err)
		return
	}

	// The first-run checklist, in the order a new user should do things.
	if strings.TrimSpace(st.Interests) == "" {
		out.Setup = append(out.Setup, "interests")
	}
	if out.Feeds == 0 {
		out.Setup = append(out.Setup, "feeds")
	}
	if ok, _ := s.DB.Meta(ctx, "provider_ok"); ok == "" || ok != out.Provider {
		out.Setup = append(out.Setup, "provider")
	}
	if len(st.ChannelsFor(nil)) == 0 {
		out.Setup = append(out.Setup, "channel")
	}
	writeJSON(w, http.StatusOK, out)
}
