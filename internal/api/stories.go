package api

import (
	"crypto/subtle"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func storyView(st store.Story) Story {
	sources := st.Sources
	if sources == nil {
		sources = []string{}
	}
	return Story{
		ID: st.ID, Topic: st.Topic, Title: st.Title, URL: st.URL, Summary: st.Summary, Snippet: st.Snippet,
		Sources: sources, FirstSeen: timeString(st.FirstSeen), Score: st.Score, LLMScore: st.LLMScore,
		Taste: st.Taste, TasteN: st.TasteN, Reason: st.Reason, Why: pipeline.Why(st), Vote: st.Vote,
		Clicked: st.Clicked, Ignored: st.Ignored, Briefed: st.BriefedAt != nil,
	}
}

// GET /api/v1/stories?hours=24&min=0&topic=&q=&voted=true
func (s *Server) stories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours, err := strconv.Atoi(q.Get("hours"))
	if err != nil || hours <= 0 || hours > 24*90 {
		hours = 24
	}
	minScore, _ := strconv.ParseFloat(q.Get("min"), 64)
	since := s.now().Add(-time.Duration(hours) * time.Hour)
	list, err := s.DB.ListStories(r.Context(), store.StoryFilter{
		Since: since, MinScore: minScore, Topic: q.Get("topic"), Query: q.Get("q"), Voted: q.Get("voted") == "true",
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	topics, err := s.DB.Topics(r.Context(), since)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := StoriesResponse{Stories: make([]Story, 0, len(list)), Topics: topics}
	if out.Topics == nil {
		out.Topics = []string{}
	}
	for _, st := range list {
		out.Stories = append(out.Stories, storyView(st))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) vote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body VoteRequest
	if !readJSON(w, r, &body) {
		return
	}
	if body.Vote < -1 || body.Vote > 1 {
		writeFieldError(w, "vote", "vote must be -1, 0 or 1")
		return
	}
	if err := s.DB.SetVote(r.Context(), id, body.Vote); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such story")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	st, err := s.DB.Story(r.Context(), id)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storyView(st))
}

// click records that a delivered story was opened, then redirects to it.
func (s *Server) click(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	target, err := s.DB.RecordClick(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "This story is no longer stored.", http.StatusNotFound)
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// voteFromPath parses and verifies /v/{id}/{dir}/{sig}.
func (s *Server) voteFromPath(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return 0, 0, false
	}
	vote := map[string]int{"up": 1, "down": -1}[r.PathValue("dir")]
	st, ok := s.settings(w, r)
	if !ok {
		return 0, 0, false
	}
	if vote == 0 || !s.links(st).Verify(id, vote, r.PathValue("sig")) {
		http.Error(w, "This vote link is not valid.", http.StatusForbidden)
		return 0, 0, false
	}
	return id, vote, true
}

var votePage = template.Must(template.New("vote").Parse(`<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>tempoBrief</title></head>
<body style="background:#000;color:#f2f2f0;font:15px -apple-system,Segoe UI,Helvetica,Arial,sans-serif;padding:32px;max-width:560px">
{{if .Done}}<p>{{if eq .Vote 1}}👍{{else}}👎{{end}} Saved for <b>{{.Title}}</b>.</p>
{{else}}<p style="margin-bottom:20px"><b>{{.Title}}</b></p>
<form method="post"><button style="font:inherit;padding:10px 18px;background:#6e9bff;color:#000;border:0;border-radius:6px">Save {{if eq .Vote 1}}👍{{else}}👎{{end}}</button></form>{{end}}
</body></html>`))

// voteLinkPage asks for a confirming tap: mail scanners open every link in a
// message, so a GET must not count as a vote.
func (s *Server) voteLinkPage(w http.ResponseWriter, r *http.Request) {
	id, vote, ok := s.voteFromPath(w, r)
	if !ok {
		return
	}
	st, err := s.DB.Story(r.Context(), id)
	if err != nil {
		http.Error(w, "This story is no longer stored.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page has an inline style and a self-posting form; nothing else.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
	votePage.Execute(w, map[string]any{"Title": st.Title, "Vote": vote, "Done": false})
}

func (s *Server) voteLink(w http.ResponseWriter, r *http.Request) {
	id, vote, ok := s.voteFromPath(w, r)
	if !ok {
		return
	}
	if err := s.DB.SetVote(r.Context(), id, vote); err != nil {
		http.Error(w, "This story is no longer stored.", http.StatusNotFound)
		return
	}
	// ntfy's http action ignores the body; a browser gets the confirmation page.
	st, _ := s.DB.Story(r.Context(), id)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	votePage.Execute(w, map[string]any{"Title": st.Title, "Vote": vote, "Done": true})
}

func briefSummary(b store.Brief) BriefSummary {
	return BriefSummary{
		ID: b.ID, ScheduleID: b.ScheduleID, Title: b.Title, CreatedAt: timeString(b.CreatedAt),
		StoryCount: len(b.StoryIDs), Deliveries: deliveries(b.Deliveries),
	}
}

func deliveries(d []store.Delivery) []Delivery {
	out := make([]Delivery, 0, len(d))
	for _, x := range d {
		out = append(out, Delivery{Channel: x.Channel, OK: x.OK, Error: x.Error})
	}
	return out
}

func (s *Server) briefs(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Briefs(r.Context(), 60)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := BriefsResponse{Briefs: make([]BriefSummary, 0, len(list))}
	for _, b := range list {
		out.Briefs = append(out.Briefs, briefSummary(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) brief(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := s.DB.Brief(r.Context(), id)
	s.writeBrief(w, r, b, err)
}

func (s *Server) latestBrief(w http.ResponseWriter, r *http.Request) {
	b, err := s.DB.LatestBrief(r.Context())
	s.writeBrief(w, r, b, err)
}

func (s *Server) writeBrief(w http.ResponseWriter, r *http.Request, b store.Brief, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no brief yet")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	stories, err := s.DB.StoriesByID(r.Context(), b.StoryIDs)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := Brief{
		ID: b.ID, ScheduleID: b.ScheduleID, Title: b.Title, CreatedAt: timeString(b.CreatedAt),
		Sections: []BriefSection{}, Deliveries: deliveries(b.Deliveries),
	}
	index := map[string]int{}
	for _, st := range stories {
		i, ok := index[st.Topic]
		if !ok {
			i = len(out.Sections)
			index[st.Topic] = i
			out.Sections = append(out.Sections, BriefSection{Topic: st.Topic})
		}
		out.Sections[i].Stories = append(out.Sections[i].Stories, storyView(st))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) sendBrief(w http.ResponseWriter, r *http.Request) {
	var body SendBriefRequest
	if !readJSON(w, r, &body) {
		return
	}
	id, err := s.Pipeline.SendBriefNow(r.Context(), body.ScheduleID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such brief schedule")
	case id == 0 && err != nil:
		s.internal(w, err)
	case err != nil:
		writeJSON(w, http.StatusOK, SendBriefResponse{BriefID: id, Error: err.Error()})
	default:
		writeJSON(w, http.StatusOK, SendBriefResponse{BriefID: id})
	}
}

// readToken checks ?token= against the read token.
func (s *Server) readToken(w http.ResponseWriter, r *http.Request) (settings.Settings, bool) {
	st, ok := s.settings(w, r)
	if !ok {
		return st, false
	}
	token := r.URL.Query().Get("token")
	if st.ReadToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(st.ReadToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "bad_token", "a valid ?token= is required")
		return st, false
	}
	return st, true
}

// briefJSON is the latest brief as a flat, small JSON object, shaped for a
// Home Assistant REST sensor (object at the top, small enough for its 16 KB
// attribute limit).
func (s *Server) briefJSON(w http.ResponseWriter, r *http.Request) {
	st, ok := s.readToken(w, r)
	if !ok {
		return
	}
	b, err := s.DB.LatestBrief(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, deliver.Payload(deliver.Message{Kind: deliver.KindBrief, Title: "No brief yet"}))
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	msg, err := s.Pipeline.MessageForBrief(r.Context(), st, b)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deliver.Payload(msg))
}

type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Category    string `xml:"category"`
	PubDate     string `xml:"pubDate"`
}

// feedXML is an RSS feed of the best stories of the last two days, for
// reading the curated stream in any feed reader.
func (s *Server) feedXML(w http.ResponseWriter, r *http.Request) {
	st, ok := s.readToken(w, r)
	if !ok {
		return
	}
	minScore := 6.0
	if len(st.Briefs) > 0 {
		minScore = float64(st.Briefs[0].MinScore)
	}
	list, err := s.DB.ListStories(r.Context(), store.StoryFilter{Since: s.now().Add(-48 * time.Hour), MinScore: minScore, Limit: 100})
	if err != nil {
		s.internal(w, err)
		return
	}
	links := s.links(st)
	doc := rss{Version: "2.0", Channel: rssChannel{
		Title: "tempoBrief", Link: s.Config.PublicURL, Description: "Stories picked for you by tempoBrief",
	}}
	for _, story := range list {
		doc.Channel.Items = append(doc.Channel.Items, rssItem{
			Title: story.Title, Link: links.Click(story.ID, story.URL), GUID: fmt.Sprintf("tempobrief-%d", story.ID),
			Description: story.Summary, Category: story.Topic, PubDate: story.FirstSeen.UTC().Format(time.RFC1123Z),
		})
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(doc)
}
