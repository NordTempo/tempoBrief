package feeds

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Page is what an article's HTML head says about it.
type Page struct {
	Title       string
	Description string
	Site        string
	// Feeds the page links to (<link rel="alternate">), usually the site's own.
	Feeds []FeedLink
}

type FeedLink struct {
	URL   string
	Title string
}

var feedTypes = map[string]bool{
	"application/rss+xml": true, "application/atom+xml": true, "application/feed+json": true,
}

// Page fetches an article and reads its head: Open Graph title and
// description before the plain ones, and any feed links.
func (f *Fetcher) Page(ctx context.Context, link string) (Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return Page{}, err
	}
	// Some news sites only serve a full head to browsers.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; "+f.userAgent+")")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	resp, err := f.http.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return readHead(io.LimitReader(resp.Body, 2<<20), resp.Request.URL), nil
}

func readHead(r io.Reader, base *url.URL) Page {
	var p Page
	meta := map[string]string{}
	z := html.NewTokenizer(r)
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return p.fill(meta)
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head":
				return p.fill(meta)
			case "title":
				inTitle = false
			}
		case html.TextToken:
			if inTitle && p.Title == "" {
				p.Title = strings.TrimSpace(html.UnescapeString(string(z.Text())))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
			}
			switch string(name) {
			case "title":
				inTitle = true
			case "body":
				return p.fill(meta)
			case "meta":
				key := strings.ToLower(attrs["property"] + attrs["name"])
				if key != "" && meta[key] == "" {
					meta[key] = strings.TrimSpace(attrs["content"])
				}
			case "link":
				if !strings.Contains(strings.ToLower(attrs["rel"]), "alternate") || !feedTypes[strings.ToLower(attrs["type"])] {
					continue
				}
				if u, err := base.Parse(attrs["href"]); err == nil && attrs["href"] != "" {
					p.Feeds = append(p.Feeds, FeedLink{URL: u.String(), Title: strings.TrimSpace(attrs["title"])})
				}
			}
		}
	}
}

func (p Page) fill(meta map[string]string) Page {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := meta[k]; v != "" {
				return v
			}
		}
		return ""
	}
	if t := first("og:title", "twitter:title"); t != "" {
		p.Title = t
	} else {
		p.Title = withoutSiteName(p.Title)
	}
	p.Description = first("og:description", "description", "twitter:description")
	p.Site = first("og:site_name")
	return p
}

// withoutSiteName drops a trailing site name from a <title> ("A new Go API
// for JSON - The Go Programming Language"), when what is left still reads as
// a headline of four words or more.
func withoutSiteName(title string) string {
	cut := -1
	for _, sep := range []string{" | ", " - ", " – ", " — "} {
		cut = max(cut, strings.LastIndex(title, sep))
	}
	if cut > 0 && len(strings.Fields(title[:cut])) >= 4 {
		return strings.TrimSpace(title[:cut])
	}
	return title
}
