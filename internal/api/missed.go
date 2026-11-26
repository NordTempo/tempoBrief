package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// POST /api/v1/missed: the reader should have got the article at url.
func (s *Server) reportMissed(w http.ResponseWriter, r *http.Request) {
	var body MissedRequest
	if !readJSON(w, r, &body) {
		return
	}
	if store.URLKey(body.URL) == "" {
		writeFieldError(w, "url", "paste the article's http(s) link")
		return
	}
	if len(body.Note) > 500 {
		writeFieldError(w, "note", "keep it under 500 characters")
		return
	}
	rep, err := s.Pipeline.ReportMissed(r.Context(), strings.TrimSpace(body.URL), body.Note)
	if errors.Is(err, pipeline.ErrUnreadable) {
		writeFieldError(w, "url", "tempoBrief could not read that page. Check the link, or try the article's own page rather than a share link.")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	out := MissedResponse{Story: storyView(rep.Story), Case: string(rep.Case), Lines: rep.Lines}
	if f := rep.AddedFeed; rep.FeedID != 0 {
		out.AddedFeed = &AddedFeed{ID: rep.FeedID, URL: f.URL, Title: f.Title, Topic: rep.Story.Topic,
			HasArticle: f.HasArticle, PerDay: f.PerDay}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/missed lists recent misses, newest first.
func (s *Server) missedList(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.MissedStories(r.Context(), 30)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := MissedList{Stories: make([]Story, 0, len(list))}
	for _, st := range list {
		out.Stories = append(out.Stories, storyView(st))
	}
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/v1/stories/{id}/missed takes a miss back.
func (s *Server) clearMissed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.DB.ClearMissed(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such story")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
