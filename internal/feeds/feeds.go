// Package feeds fetches RSS, Atom and JSON Feed documents politely (conditional
// GET, backoff on errors) and turns their entries into items. It also reads
// and writes OPML. See docs/pipeline.md#fetching.
package feeds

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mmcdole/gofeed"

	"github.com/NordTempo/tempoBrief/internal/store"
)

const maxBody = 10 << 20

// Fetcher downloads one feed at a time; the pipeline runs a few in parallel.
type Fetcher struct {
	http      *http.Client
	userAgent string
}

func NewFetcher(version string) *Fetcher {
	return &Fetcher{
		http:      &http.Client{Timeout: 30 * time.Second},
		userAgent: "tempoBrief/" + version + " (+https://github.com/NordTempo/tempoBrief)",
	}
}

// Result is a fetched feed document.
type Result struct {
	NotModified  bool
	ETag         string
	LastModified string
	Title        string
	SiteURL      string
	Items        []store.NewItem
}

// Fetch downloads and parses a feed, sending the validators from the last
// successful fetch.
func (f *Fetcher) Fetch(ctx context.Context, feed store.Feed) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, */*;q=0.8")
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		req.Header.Set("If-Modified-Since", feed.LastModified)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return Result{NotModified: true, ETag: feed.ETag, LastModified: feed.LastModified}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{}, err
	}
	parsed, err := gofeed.NewParser().ParseString(string(body))
	if err != nil {
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
			return Result{}, errors.New("the URL returned a web page, not a feed")
		}
		return Result{}, fmt.Errorf("not a feed: %w", err)
	}
	return Result{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		Title:        strings.TrimSpace(parsed.Title),
		SiteURL:      parsed.Link,
		Items:        Items(parsed, feed.Title, time.Now()),
	}, nil
}

// Items converts parsed entries. Entries without a link or title are skipped.
// The outlet name is feedTitle (what the user calls the feed), or the
// document's own title when that is empty.
func Items(parsed *gofeed.Feed, feedTitle string, now time.Time) []store.NewItem {
	source := strings.TrimSpace(feedTitle)
	if source == "" {
		source = strings.TrimSpace(parsed.Title)
	}
	var out []store.NewItem
	for _, e := range parsed.Items {
		title := cleanText(e.Title)
		link := strings.TrimSpace(e.Link)
		if title == "" || link == "" {
			continue
		}
		guid := strings.TrimSpace(e.GUID)
		if guid == "" {
			guid = link
		}
		published := now
		if e.PublishedParsed != nil {
			published = *e.PublishedParsed
		} else if e.UpdatedParsed != nil {
			published = *e.UpdatedParsed
		}
		if published.After(now) {
			published = now
		}
		body := e.Description
		if len(e.Content) > len(body) {
			body = e.Content
		}
		item := store.NewItem{
			GUID: guid, URL: link, Title: title, Snippet: Snippet(body, 600),
			Source: source, PublishedAt: published,
		}
		if pub, rest, ok := aggregatorPublisher(link, title); ok {
			item.Source, item.Title = pub, rest
		}
		if item.Snippet == item.Title {
			item.Snippet = ""
		}
		out = append(out, item)
	}
	return out
}

// IsAggregator reports links that point at an aggregator rather than the article.
func IsAggregator(link string) bool {
	return strings.HasPrefix(link, "https://news.google.com/")
}

var publisherSuffix = regexp.MustCompile(`^(.+) - ([^-]+)$`)

// aggregatorPublisher handles Google News search feeds: the feed title says
// nothing about the outlet, and item titles end in " - Publisher". The
// publisher becomes the source, so outlet counts stay honest.
func aggregatorPublisher(link, title string) (publisher, rest string, ok bool) {
	if !IsAggregator(link) {
		return "", "", false
	}
	m := publisherSuffix.FindStringSubmatch(title)
	if m == nil {
		return "", "", false
	}
	return strings.TrimSpace(m[2]), strings.TrimSpace(m[1]), true
}

var (
	tags       = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	whitespace = regexp.MustCompile(`\s+`)
)

// Snippet strips markup and returns at most max characters of plain text,
// cut at a word boundary.
func Snippet(s string, max int) string {
	text := cleanText(tags.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)[:max]
	cut := string(runes)
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func cleanText(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(html.UnescapeString(s), " "))
}

// NextFetch spaces out fetches: the poll interval when healthy, doubling per
// consecutive error up to six hours.
func NextFetch(now time.Time, poll time.Duration, errorCount int) time.Time {
	if errorCount <= 0 {
		return now.Add(poll)
	}
	wait := poll << min(errorCount, 10)
	return now.Add(min(wait, 6*time.Hour))
}
