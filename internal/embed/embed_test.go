package embed

import (
	"math"
	"testing"
)

// Headline embeddings share a large common direction: unrelated headlines in
// one language still score about 0.85. This builds such vectors: a shared base
// plus a private direction each.
func headlines(n int, shared float64) [][]float32 {
	dims := n + 1
	out := make([][]float32, n)
	for i := range out {
		v := make([]float32, dims)
		v[0] = float32(math.Sqrt(shared))
		v[i+1] = float32(math.Sqrt(1 - shared))
		out[i] = v
	}
	return out
}

func TestMeanDoesNotDriftTowardsEverything(t *testing.T) {
	const threshold = 0.91
	vs := headlines(31, 0.85)
	story, extra := vs[:30], vs[30]
	mean := story[0]
	for i, v := range story[1:] {
		mean = AddToMean(mean, i+1, v)
	}
	// A renormalized centroid of these would score sqrt(0.85) = 0.92 against
	// any new headline and swallow it. The plain mean stays near the 0.85
	// baseline, below the threshold.
	if sim := Similarity(extra, mean); sim >= threshold {
		t.Fatalf("an unrelated headline scores %.3f against a big story", sim)
	}
}

func TestMeanOfNearDuplicatesStillMatches(t *testing.T) {
	vs := headlines(3, 0.95) // three phrasings of one event
	mean := AddToMean(vs[0], 1, vs[1])
	if sim := Similarity(vs[2], mean); sim < 0.91 {
		t.Fatalf("a third phrasing should join, scored %.3f", sim)
	}
}
