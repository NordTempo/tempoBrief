package api

import "github.com/NordTempo/tempoBrief/internal/settings"

// Response and request bodies. tygo turns this file into
// web/src/api/types.gen.ts, so keep every API shape here. Times are RFC 3339
// strings in UTC; the web app formats them in the reader's time zone.

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type AuthMode string

type Me struct {
	AuthMode        AuthMode `json:"authMode"`
	Authenticated   bool     `json:"authenticated"`
	PasswordFromEnv bool     `json:"passwordFromEnv"`
	// GitHubLogin: "Sign in with GitHub" is set up.
	GitHubLogin bool `json:"githubLogin"`
	// PublicPages: the brief and stories can be read without logging in.
	PublicPages bool   `json:"publicPages"`
	Version     string `json:"version"`
}

type LoginRequest struct {
	Password string `json:"password"`
}

// GitHubStatus is the Sign in with GitHub section of Settings.
type GitHubStatus struct {
	// Available is false without a public URL, which GitHub must send the reader back to.
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Login     string `json:"login"`  // the one GitHub account let in
	AppURL    string `json:"appUrl"` // the app's page on GitHub
}

// GitHubSetup is the form the web app posts to GitHub to create the app.
type GitHubSetup struct {
	Action   string `json:"action"`
	Manifest string `json:"manifest"`
}

type PasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// Story is one story as the web app shows it.
type Story struct {
	ID        int64    `json:"id"`
	Topic     string   `json:"topic"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Summary   string   `json:"summary"`
	Snippet   string   `json:"snippet"`
	Sources   []string `json:"sources"`
	FirstSeen string   `json:"firstSeen"`
	// Score is the final rank, 0 to 10; null until scored.
	Score *float64 `json:"score"`
	// LLMScore is the model's own score, before taste.
	LLMScore *int64 `json:"llmScore"`
	// Taste is -1 to 1 from similar past reactions; null when none were similar.
	Taste   *float64 `json:"taste"`
	TasteN  int      `json:"tasteN"`
	Reason  string   `json:"reason"`
	Why     string   `json:"why"`
	Vote    int      `json:"vote"`
	Clicked bool     `json:"clicked"`
	Ignored bool     `json:"ignored"`
	Missed  bool     `json:"missed"`
	// MissedNote is the reader's reason, when they reported it as missed.
	MissedNote string `json:"missedNote"`
	Briefed    bool   `json:"briefed"`
}

type StoriesResponse struct {
	Stories []Story  `json:"stories"`
	Topics  []string `json:"topics"`
}

type VoteRequest struct {
	Vote int `json:"vote"` // -1, 0 (clear) or 1
}

type Delivery struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type BriefSection struct {
	Topic   string  `json:"topic"`
	Stories []Story `json:"stories"`
}

type Brief struct {
	ID         int64          `json:"id"`
	ScheduleID string         `json:"scheduleId"`
	Title      string         `json:"title"`
	CreatedAt  string         `json:"createdAt"`
	Sections   []BriefSection `json:"sections"`
	Deliveries []Delivery     `json:"deliveries"`
}

type BriefSummary struct {
	ID         int64      `json:"id"`
	ScheduleID string     `json:"scheduleId"`
	Title      string     `json:"title"`
	CreatedAt  string     `json:"createdAt"`
	StoryCount int        `json:"storyCount"`
	Deliveries []Delivery `json:"deliveries"`
}

type BriefsResponse struct {
	Briefs []BriefSummary `json:"briefs"`
}

type SendBriefRequest struct {
	ScheduleID string `json:"scheduleId"`
}

type SendBriefResponse struct {
	BriefID int64  `json:"briefId"`
	Error   string `json:"error,omitempty"` // some deliveries failed; the brief is saved anyway
}

type FeedStatus string

const (
	FeedOK       FeedStatus = "ok"
	FeedFailing  FeedStatus = "failing"
	FeedNew      FeedStatus = "new"
	FeedDisabled FeedStatus = "disabled"
)

type Feed struct {
	ID          int64      `json:"id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Topic       string     `json:"topic"`
	SiteURL     string     `json:"siteUrl"`
	Enabled     bool       `json:"enabled"`
	Status      FeedStatus `json:"status"`
	LastFetchAt string     `json:"lastFetchAt"`
	LastOKAt    string     `json:"lastOkAt"`
	LastError   string     `json:"lastError"`
	ErrorCount  int        `json:"errorCount"`
	ItemCount   int        `json:"itemCount"`
}

type FeedsResponse struct {
	Feeds  []Feed   `json:"feeds"`
	Topics []string `json:"topics"`
}

type AddFeedRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Topic string `json:"topic"`
}

type UpdateFeedRequest struct {
	Title   string `json:"title"`
	Topic   string `json:"topic"`
	Enabled bool   `json:"enabled"`
}

type ImportOPMLRequest struct {
	OPML string `json:"opml"`
}

type ImportResponse struct {
	Found int `json:"found"`
	Added int `json:"added"`
}

type Policy struct {
	ID        int64    `json:"id"`
	CreatedAt string   `json:"createdAt"`
	Source    string   `json:"source"` // "learned" or "edited"
	Text      string   `json:"text"`
	Changes   []string `json:"changes"`
	Signals   int      `json:"signals"`
}

type PolicyRequest struct {
	Text string `json:"text"`
}

type SignalCounts struct {
	Liked    int `json:"liked"`
	Disliked int `json:"disliked"`
	Clicked  int `json:"clicked"`
	Ignored  int `json:"ignored"`
	Missed   int `json:"missed"`
}

type Signal struct {
	StoryID int64  `json:"storyId"`
	Kind    string `json:"kind"`
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Note    string `json:"note"`
	At      string `json:"at"`
}

// MissedRequest reports a story the reader should have got: its link, and
// optionally why it matters to them.
type MissedRequest struct {
	URL  string `json:"url"`
	Note string `json:"note"`
}

// MissedResponse says why the reader did not get the story. The miss is
// recorded either way.
type MissedResponse struct {
	Story Story `json:"story"`
	// Case: not_fetched (no feed had it), not_scored, low_score, queued
	// (the next brief has it), left_out (its topic was full) or briefed.
	Case  string   `json:"case" tstype:"'not_fetched' | 'not_scored' | 'low_score' | 'queued' | 'left_out' | 'briefed'"`
	Lines []string `json:"lines"`
	// AddedFeed was added so stories like this arrive from now on
	// (not_fetched only); null when none was found.
	AddedFeed *AddedFeed `json:"addedFeed"`
}

type AddedFeed struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	Topic string `json:"topic"`
	// HasArticle: the feed carries the reported article.
	HasArticle bool `json:"hasArticle"`
	PerDay     int  `json:"perDay"`
}

type MissedList struct {
	Stories []Story `json:"stories"`
}

// Learning is everything the "what it learned" view shows.
type Learning struct {
	Current *Policy  `json:"current"`
	History []Policy `json:"history"`
	// Totals over the last 60 days, the window the learner reads.
	Counts SignalCounts `json:"counts"`
	// Reactions since the last rewrite, and how many trigger the next one.
	SinceLastLearn int      `json:"sinceLastLearn"`
	LearnAfter     int      `json:"learnAfter"`
	LastLearnedAt  string   `json:"lastLearnedAt"`
	Recent         []Signal `json:"recent"`
}

type Step struct {
	Name      string `json:"name"`
	LastRun   string `json:"lastRun"`
	LastOK    string `json:"lastOk"`
	LastError string `json:"lastError"`
}

type Status struct {
	Version   string        `json:"version"`
	Provider  string        `json:"provider"`
	PublicURL string        `json:"publicUrl"`
	Steps     []Step        `json:"steps"`
	Feeds     int           `json:"feeds"`
	Failing   int           `json:"failing"`
	Stories   int           `json:"stories"` // scored in the last 24 hours
	LastBrief *BriefSummary `json:"lastBrief"`
	// Setup lists what is still missing before the first brief, in order.
	Setup []string `json:"setup"`
}

// SettingsResponse carries settings with secrets replaced by a placeholder.
type SettingsResponse struct {
	Settings settings.Settings `json:"settings"`
	// FeedURL and BriefJSONURL include the read token; empty without a public URL.
	FeedURL      string `json:"feedUrl"`
	BriefJSONURL string `json:"briefJsonUrl"`
}

type TestChannelRequest struct {
	ChannelID string `json:"channelId"`
}

// DiscordCheckRequest checks a bot token. An empty or redacted token means
// the saved token of channel ChannelID.
type DiscordCheckRequest struct {
	Token     string `json:"token"`
	ChannelID string `json:"channelId"`
}

// DiscordBot is what the token reveals: the bot, who made it (the one who
// may vote), an invite link and the channels it can post in.
type DiscordBot struct {
	Name      string           `json:"name"`
	OwnerID   string           `json:"ownerId"`
	OwnerName string           `json:"ownerName"`
	InviteURL string           `json:"inviteUrl"`
	Channels  []DiscordChannel `json:"channels"`
}

type DiscordChannel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Guild string `json:"guild"`
}

type TestResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}
