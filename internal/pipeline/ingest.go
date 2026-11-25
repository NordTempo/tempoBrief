package pipeline

import (
	"context"
	"strings"
	"sync"

	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// fetchWorkers bounds parallel feed downloads.
const fetchWorkers = 4

// fetch downloads every due feed and stores entries that are new and recent.
func (p *Pipeline) fetch(ctx context.Context, _ settings.Settings) error {
	due, err := p.DB.DueFeeds(ctx, p.Now())
	if err != nil {
		return err
	}
	jobs := make(chan store.Feed)
	var wg sync.WaitGroup
	for range min(fetchWorkers, len(due)) {
		wg.Go(func() {
			for f := range jobs {
				p.fetchOne(ctx, f)
			}
		})
	}
	for _, f := range due {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}

// FetchFeed fetches one feed now, for "Add feed" and "Retry" in the UI.
func (p *Pipeline) FetchFeed(ctx context.Context, f store.Feed) {
	p.fetchOne(ctx, f)
}

func (p *Pipeline) fetchOne(ctx context.Context, f store.Feed) {
	now := p.Now()
	res, err := p.Fetcher.Fetch(ctx, f)
	if err != nil {
		p.Log.Warn("feed failed", "feed", f.URL, "err", err)
		p.DB.RecordFetch(ctx, f.ID, store.FetchResult{
			At: now, Next: feeds.NextFetch(now, p.Config.Poll, f.ErrorCount+1), Error: err.Error(),
		})
		return
	}
	var fresh []store.NewItem
	for _, it := range res.Items {
		if now.Sub(it.PublishedAt) <= maxItemAge {
			fresh = append(fresh, it)
		}
	}
	if _, err := p.DB.AddItems(ctx, f.ID, fresh); err != nil {
		p.Log.Error("store items", "feed", f.URL, "err", err)
	}
	p.DB.RecordFetch(ctx, f.ID, store.FetchResult{
		At: now, Next: feeds.NextFetch(now, p.Config.Poll, 0), OK: true, NotModified: res.NotModified,
		ETag: res.ETag, LastModified: res.LastModified, Title: res.Title, SiteURL: res.SiteURL,
	})
}

// clusterThreshold is the average title similarity at which an item joins a
// story. Measured on live NRK, Aftenposten, E24 and Google News headlines with
// e5-small: unrelated headlines score 0.80 to 0.87, different events about the
// same company or case 0.90 to 0.93, the same event 0.93 to 0.99 (the same
// event across Norwegian and English about 0.93). Grouping here only takes the
// confident part; the duplicate check before each brief merges the rest.
const clusterThreshold = 0.93

// cluster assigns each new item to the open story it is most similar to on
// average (its similarity to the story's mean vector), or starts a new one.
// Items are embedded by title only: snippets are noisy, and aggregator items
// have none.
func (p *Pipeline) cluster(ctx context.Context, _ settings.Settings) error {
	for {
		items, err := p.DB.UnclusteredItems(ctx, 256)
		if err != nil || len(items) == 0 {
			return err
		}
		titles := make([]string, len(items))
		for i, it := range items {
			titles[i] = embedText(it)
		}
		vectors, err := p.Embedder.Embed(ctx, titles)
		if err != nil {
			return err
		}
		open, err := p.DB.OpenCentroids(ctx, p.Now().Add(-storyWindow))
		if err != nil {
			return err
		}
		for i, it := range items {
			best, bestSim := -1, float32(clusterThreshold)
			for j, c := range open {
				if sim := embed.Similarity(c.Vector, vectors[i]); sim >= bestSim {
					best, bestSim = j, sim
				}
			}
			if best < 0 {
				id, err := p.DB.CreateStory(ctx, it, vectors[i])
				if err != nil {
					return err
				}
				open = append(open, store.Centroid{StoryID: id, NItems: 1, Vector: vectors[i]})
				continue
			}
			c := &open[best]
			c.Vector = embed.AddToMean(c.Vector, c.NItems, vectors[i])
			c.NItems++
			if err := p.DB.AttachItem(ctx, it, c.StoryID, c.Vector, feeds.IsAggregator); err != nil {
				return err
			}
		}
		if len(items) < 256 {
			return nil
		}
	}
}

// embedText is what a headline is embedded as. Very short titles carry too
// little meaning on their own (release feeds title items "v2.1.332"), so they
// get their source as context.
func embedText(it store.PendingItem) string {
	if len(strings.Fields(it.Title)) < 4 {
		return it.Source + ": " + it.Title
	}
	return it.Title
}
