package api

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func feedView(f store.Feed) Feed {
	status := FeedOK
	switch {
	case !f.Enabled:
		status = FeedDisabled
	case f.LastError != "":
		status = FeedFailing
	case f.LastFetchAt.IsZero():
		status = FeedNew
	}
	return Feed{
		ID: f.ID, URL: f.URL, Title: f.Title, Topic: f.Topic, SiteURL: f.SiteURL, Enabled: f.Enabled,
		Status: status, LastFetchAt: timeString(f.LastFetchAt), LastOKAt: timeString(f.LastOKAt),
		LastError: f.LastError, ErrorCount: f.ErrorCount, ItemCount: f.ItemCount,
	}
}

func (s *Server) feeds(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Feeds(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := FeedsResponse{Feeds: make([]Feed, 0, len(list)), Topics: []string{}}
	for _, f := range list {
		out.Feeds = append(out.Feeds, feedView(f))
		if !slices.Contains(out.Topics, f.Topic) {
			out.Topics = append(out.Topics, f.Topic)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func validFeedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// addFeed stores a feed and fetches it once right away, so the user sees
// at once whether the URL works.
func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	var body AddFeedRequest
	if !readJSON(w, r, &body) {
		return
	}
	if !validFeedURL(body.URL) {
		writeFieldError(w, "url", "enter an http(s) feed URL")
		return
	}
	n, err := s.DB.AddFeeds(r.Context(), []store.NewFeed{{URL: body.URL, Title: body.Title, Topic: body.Topic}})
	if err != nil {
		s.internal(w, err)
		return
	}
	if n == 0 {
		writeFieldError(w, "url", "this feed is already added")
		return
	}
	list, err := s.DB.Feeds(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	for _, f := range list {
		if f.URL == strings.TrimSpace(body.URL) {
			s.Pipeline.FetchFeed(r.Context(), f)
			f, _ = s.DB.Feed(r.Context(), f.ID)
			writeJSON(w, http.StatusCreated, feedView(f))
			return
		}
	}
	s.internal(w, errors.New("added feed not found"))
}

func (s *Server) updateFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body UpdateFeedRequest
	if !readJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Topic) == "" {
		writeFieldError(w, "topic", "a feed needs a topic")
		return
	}
	err := s.DB.UpdateFeed(r.Context(), id, store.FeedUpdate{Title: body.Title, Topic: body.Topic, Enabled: body.Enabled})
	s.writeFeed(w, r, id, err)
}

func (s *Server) deleteFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.DB.DeleteFeed(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such feed")
	} else if err != nil {
		s.internal(w, err)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) fetchFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, err := s.DB.Feed(r.Context(), id)
	if err == nil {
		s.Pipeline.FetchFeed(r.Context(), f)
	}
	s.writeFeed(w, r, id, err)
}

func (s *Server) writeFeed(w http.ResponseWriter, r *http.Request, id int64, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such feed")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	f, err := s.DB.Feed(r.Context(), id)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, feedView(f))
}

func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	var body ImportOPMLRequest
	if !readJSON(w, r, &body) {
		return
	}
	list, err := feeds.ParseOPML(strings.NewReader(body.OPML))
	if err != nil {
		writeFieldError(w, "opml", err.Error())
		return
	}
	valid := list[:0]
	for _, f := range list {
		if validFeedURL(f.URL) {
			valid = append(valid, f)
		}
	}
	added, err := s.DB.AddFeeds(r.Context(), valid)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ImportResponse{Found: len(valid), Added: added})
}

func (s *Server) exportOPML(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Feeds(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tempobrief-feeds.opml"`)
	feeds.WriteOPML(w, list)
}
