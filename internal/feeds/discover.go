package feeds

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NordTempo/tempoBrief/internal/store"
)

// Found is a working feed for an article's site, as Discover ranks it.
type Found struct {
	URL   string
	Title string
	// HasArticle: the feed carries the article right now, the best evidence
	// that it would have caught it.
	HasArticle bool
	// Section: the feed is for the article's section (nrk.no/norge/...).
	Section bool
	// Search: a Google News search for the site, for sites without a feed.
	Search bool
	// PerDay is how many items the feed publishes a day, over its last week.
	PerDay int
}

func (f Found) rank() int {
	r := 0
	if f.HasArticle {
		r += 4
	}
	if f.Section {
		r += 2
	}
	if !f.Search {
		r++
	}
	return r
}

// commonPaths are where sites without a <link rel="alternate"> usually keep
// their feed (WordPress, Substack, Ghost, Hugo, Jekyll, Blogger).
var commonPaths = []string{"/feed", "/rss", "/rss.xml", "/feed.xml", "/atom.xml", "/index.xml", "/feeds/posts/default"}

// sectionPaths are tried under the article's section ("/norge" + path):
// news sites often have a feed per section that their pages do not link.
var sectionPaths = []string{"/feed", "/rss", "/rss.xml", "/toppsaker.rss"}

// Discover looks for feeds that carry the article at link, best first: the
// feeds its page links to, its site's front page links to, common feed paths
// and known platforms, and last a Google News search for the site. Every
// candidate is fetched; only real feeds are returned. page is the article's
// own head, already read.
func (f *Fetcher) Discover(ctx context.Context, link, title string, page Page) []Found {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	art, err := url.Parse(link)
	if err != nil {
		return nil
	}
	home := &url.URL{Scheme: art.Scheme, Host: art.Host, Path: "/"}

	cands := map[string]FeedLink{}
	add := func(links ...FeedLink) {
		for _, l := range links {
			if _, ok := cands[l.URL]; !ok {
				cands[l.URL] = l
			}
		}
	}
	add(page.Feeds...)
	add(platformFeeds(art)...)
	if front, err := f.Page(ctx, home.String()); err == nil {
		add(front.Feeds...)
	}
	if len(cands) == 0 {
		for _, p := range commonPaths {
			add(FeedLink{URL: home.ResolveReference(&url.URL{Path: p}).String()})
		}
	}
	section := sectionOf(art)
	if section != "" {
		for _, p := range sectionPaths {
			add(FeedLink{URL: home.ResolveReference(&url.URL{Path: "/" + section + p}).String()})
		}
	}
	searches := map[string]bool{}
	if f.NewsSearch != "" {
		site := strings.TrimPrefix(strings.ToLower(art.Host), "www.")
		for _, scope := range []string{site, site + "/" + section} {
			if strings.HasSuffix(scope, "/") {
				continue
			}
			u := f.newsSearch(art.Host, scope)
			searches[u] = true
			add(FeedLink{URL: u, Title: "Google News: " + scope})
		}
	}

	key, words := store.URLKey(link), titleWords(title)
	var mu sync.Mutex
	var out []Found
	var wg sync.WaitGroup
	for _, c := range cands {
		wg.Go(func() {
			res, err := f.Fetch(ctx, store.Feed{URL: c.URL})
			if err != nil || len(res.Items) == 0 {
				return
			}
			found := Found{URL: c.URL, Title: c.Title, Search: searches[c.URL], PerDay: perDay(res.Items)}
			if found.Title == "" {
				found.Title = res.Title
			}
			for _, it := range res.Items {
				if store.URLKey(it.URL) == key || (words != "" && titleWords(it.Title) == words) {
					found.HasArticle = true
					break
				}
			}
			found.Section = section != "" &&
				(strings.Contains(strings.ToLower(c.URL), section) || strings.Contains(strings.ToLower(found.Title), section))
			mu.Lock()
			out = append(out, found)
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.SortFunc(out, func(a, b Found) int {
		if a.rank() != b.rank() {
			return b.rank() - a.rank()
		}
		return strings.Compare(a.URL, b.URL)
	})
	return out
}

// platformFeeds knows where a few platforms keep feeds that their pages do
// not always link to.
func platformFeeds(u *url.URL) []FeedLink {
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == "github.com" && len(parts) >= 2:
		return []FeedLink{{URL: "https://github.com/" + parts[0] + "/" + parts[1] + "/releases.atom", Title: parts[1] + " releases"}}
	case host == "medium.com" && strings.HasPrefix(parts[0], "@"):
		return []FeedLink{{URL: "https://medium.com/feed/" + parts[0], Title: parts[0]}}
	case strings.HasSuffix(host, "reddit.com") && len(parts) >= 2 && parts[0] == "r":
		return []FeedLink{{URL: "https://www.reddit.com/r/" + parts[1] + "/.rss", Title: "r/" + parts[1]}}
	}
	return nil
}

// newsSearch is a Google News feed of the articles under scope, a site or a
// site's section. Norwegian sites get the Norwegian edition, so their articles
// are included.
func (f *Fetcher) newsSearch(host, scope string) string {
	edition := "hl=en-US&gl=US&ceid=US:en"
	if strings.HasSuffix(strings.ToLower(host), ".no") {
		edition = "hl=no&gl=NO&ceid=NO:no"
	}
	return f.NewsSearch + "/rss/search?q=" + url.QueryEscape("site:"+scope) + "&" + edition
}

// sectionOf is the article's first path segment when it reads like a section
// name ("norge" in nrk.no/norge/...), lowercased; otherwise empty.
func sectionOf(u *url.URL) string {
	first, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/")
	first = strings.ToLower(first)
	if len(first) < 3 || len(first) > 20 || !strings.Contains(strings.Trim(u.Path, "/"), "/") {
		return ""
	}
	for _, r := range first {
		if !unicode.IsLetter(r) && r != '-' {
			return ""
		}
	}
	return first
}

// titleWords is a title reduced to lowercase letters and digits, for matching
// a headline across feeds that punctuate it differently.
func titleWords(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// perDay estimates items a day from the newest week of items.
func perDay(items []store.NewItem) int {
	weekAgo := time.Now().Add(-7 * 24 * time.Hour)
	var n int
	oldest := time.Now()
	for _, it := range items {
		if it.PublishedAt.After(weekAgo) {
			n++
			if it.PublishedAt.Before(oldest) {
				oldest = it.PublishedAt
			}
		}
	}
	days := max(1, time.Since(oldest).Hours()/24)
	return int(float64(n)/days + 0.5)
}
