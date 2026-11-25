package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/llm"
	"github.com/NordTempo/tempoBrief/internal/settings"
)

func (s *Server) settingsResponse(st settings.Settings) SettingsResponse {
	out := SettingsResponse{Settings: st.Redact()}
	if base := s.Config.PublicURL; base != "" {
		out.FeedURL = base + "/feed.xml?token=" + st.ReadToken
		out.BriefJSONURL = base + "/brief.json?token=" + st.ReadToken
	}
	return out
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.settingsResponse(st))
}

// putSettings replaces settings. Secrets sent back as the placeholder keep
// their stored value.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	prev, ok := s.settings(w, r)
	if !ok {
		return
	}
	var next settings.Settings
	if !readJSON(w, r, &next) {
		return
	}
	next = settings.MergeSecrets(prev, next)
	if err := settings.Save(r.Context(), s.DB, next); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorBody{Error: ErrorDetail{Code: "invalid", Message: err.Error()}})
		return
	}
	s.Configure(next)
	writeJSON(w, http.StatusOK, s.settingsResponse(next))
}

func (s *Server) rotateReadToken(w http.ResponseWriter, r *http.Request) {
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	st.RotateReadToken()
	if err := settings.Save(r.Context(), s.DB, st); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsResponse(st))
}

type pingAnswer struct {
	Reply string `json:"reply"`
}

// testProvider makes one tiny structured call with the saved provider.
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	model, err := llm.New(st.Provider)
	if err != nil {
		writeJSON(w, http.StatusOK, TestResult{OK: false, Detail: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	started := time.Now()
	answer, err := llm.Generate[pingAnswer](ctx, model, "You answer health checks.", `Reply with the word "ready".`, "low")
	if err != nil {
		writeJSON(w, http.StatusOK, TestResult{OK: false, Detail: err.Error()})
		return
	}
	s.DB.SetMeta(r.Context(), "provider_ok", model.Name())
	writeJSON(w, http.StatusOK, TestResult{OK: true, Detail: model.Name() + " answered " + strings.TrimSpace(answer.Reply) +
		" in " + time.Since(started).Round(100*time.Millisecond).String()})
}

// testChannel sends a sample alert to one saved channel.
func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	var body TestChannelRequest
	if !readJSON(w, r, &body) {
		return
	}
	st, ok := s.settings(w, r)
	if !ok {
		return
	}
	c, found := st.Channel(body.ChannelID)
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "save the channel first")
		return
	}
	msg := deliver.Message{Kind: deliver.KindAlert, Title: "tempoBrief test", Sections: []deliver.Section{{
		Topic: "Test", Stories: []deliver.Story{{
			Title: "tempoBrief can reach this channel", Summary: "This is a test message from Settings.",
			URL: "https://github.com/NordTempo/tempoBrief", Score: 10, Sources: []string{"tempoBrief"},
		}},
	}}}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.Sender.Send(ctx, c, msg); err != nil {
		writeJSON(w, http.StatusOK, TestResult{OK: false, Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, TestResult{OK: true, Detail: "sent"})
}
