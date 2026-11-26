package feeds

import (
	"net/url"
	"testing"
)

func TestDiscoverGuessesSectionsAndPlatformFeeds(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.nrk.no/norge/renten-1.234": "norge",
		"https://e24.no/boers-og-finans/i/abc":  "boers-og-finans",
		"https://blog.example.com/2026/11/post": "", // a date, not a section
		"https://example.com/post-title":        "", // no section at all
	} {
		u, _ := url.Parse(link)
		if got := sectionOf(u); got != want {
			t.Errorf("sectionOf(%s) = %q, want %q", link, got, want)
		}
	}
	for link, want := range map[string]string{
		"https://github.com/oven-sh/bun/releases/tag/v2": "https://github.com/oven-sh/bun/releases.atom",
		"https://medium.com/@someone/a-post-123":         "https://medium.com/feed/@someone",
		"https://www.reddit.com/r/selfhosted/comments/x": "https://www.reddit.com/r/selfhosted/.rss",
		"https://example.com/a/b":                        "",
	} {
		u, _ := url.Parse(link)
		got := ""
		if f := platformFeeds(u); len(f) > 0 {
			got = f[0].URL
		}
		if got != want {
			t.Errorf("platformFeeds(%s) = %q, want %q", link, got, want)
		}
	}
}

func TestPageTitlesLoseTheSiteName(t *testing.T) {
	for in, want := range map[string]string{
		"A new Go API for JSON - The Go Programming Language": "A new Go API for JSON",
		"Vite 8 - what's new":                     "Vite 8 - what's new",
		"Norges Bank holder renten uendret | NRK": "Norges Bank holder renten uendret",
	} {
		if got := withoutSiteName(in); got != want {
			t.Errorf("withoutSiteName(%q) = %q, want %q", in, got, want)
		}
	}
}
