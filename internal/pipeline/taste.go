package pipeline

import (
	"math"
	"sort"

	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// Taste is the second opinion next to the model's score: how this reader
// reacted to the stories most similar to this one. It needs no model call, so
// it also catches what a short reader profile cannot say.
//
// Constants were picked for multilingual-e5-small title embeddings, where
// unrelated headlines still score about 0.80 to 0.87 and stories on the same
// subject about 0.88 and up (the same event 0.93 and up). Taste wants the
// subject level. See docs/learning.md#taste.
const (
	tasteFloor     = 0.88 // neighbours below this similarity say nothing
	tasteNeighbors = 8
	tasteSharpness = 0.02 // smaller: the closest neighbours dominate more
	tasteWeight    = 1.5  // most the taste can move a 0 to 10 score
	tasteFullAt    = 3    // neighbours needed for the full weight
)

// signalValue is how much each kind of reaction says about the reader.
var signalValue = map[store.SignalKind]float64{
	store.SignalLiked:    1,
	store.SignalDisliked: -1,
	store.SignalClicked:  0.5,
	store.SignalIgnored:  -0.25,
	// Reported as missed: the reader went looking for it, so it says more than a like.
	store.SignalMissed: 1.5,
}

// TasteResult is the taste estimate for one story.
type TasteResult struct {
	Value     float64 // -1 (disliked kind of story) to 1 (liked kind)
	Neighbors int     // similar past reactions it is based on
	// Adjustment is what taste adds to the model's score.
	Adjustment float64
}

type neighbor struct {
	sim   float64
	value float64
}

// Taste compares a story vector with past reactions (which must carry vectors).
func Taste(v []float32, signals []store.Signal) TasteResult {
	var near []neighbor
	for _, s := range signals {
		if s.Vector == nil {
			continue
		}
		// Story vectors are means, not unit length, so compare directions.
		sim := float64(embed.Cosine(v, s.Vector))
		if sim >= tasteFloor {
			near = append(near, neighbor{sim, signalValue[s.Kind]})
		}
	}
	if len(near) == 0 {
		return TasteResult{}
	}
	sort.Slice(near, func(i, j int) bool { return near[i].sim > near[j].sim })
	near = near[:min(len(near), tasteNeighbors)]

	var sum, weights float64
	for _, n := range near {
		w := math.Exp((n.sim - 1) / tasteSharpness)
		sum += w * n.value
		weights += w
	}
	value := math.Max(-1, math.Min(1, sum/weights))
	confidence := math.Min(1, float64(len(near))/tasteFullAt)
	return TasteResult{
		Value:      round2(value),
		Neighbors:  len(near),
		Adjustment: round2(value * confidence * tasteWeight),
	}
}

// FinalScore combines the model's score and the taste adjustment, on 0 to 10.
func FinalScore(llmScore int, t TasteResult) float64 {
	return round2(math.Max(0, math.Min(10, float64(llmScore)+t.Adjustment)))
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
