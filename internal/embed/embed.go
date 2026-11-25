// Package embed turns headlines into vectors with a local multilingual model,
// so a Norwegian and an English headline about the same event land close
// together. It runs on hugot's pure Go backend, so the binary stays static.
//
// The model takes about 850 MB while a batch runs, which is far more than the
// rest of tempoBrief. It is therefore loaded for each batch of work and released
// right after; idle memory stays small. See docs/pipeline.md#embeddings.
package embed

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

type Embedder struct {
	dir   string // where models are downloaded
	model string // Hugging Face repo, such as Xenova/multilingual-e5-small
	file  string // ONNX file in the repo
	log   *slog.Logger

	mu sync.Mutex // one model in memory at a time
}

func New(dataDir, model, file string, log *slog.Logger) *Embedder {
	return &Embedder{dir: filepath.Join(dataDir, "models"), model: model, file: file, log: log}
}

// Embed returns one unit-length vector per text.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out, err := e.embed(ctx, texts)
	// The model is unreachable once embed returns; hand its memory back to
	// the OS now instead of whenever the runtime gets to it.
	debug.FreeOSMemory()
	return out, err
}

func (e *Embedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	modelPath, err := e.ensureModel(ctx)
	if err != nil {
		return nil, err
	}
	session, err := hugot.NewGoSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("embedding session: %w", err)
	}
	defer session.Destroy()
	pipe, err := session.NewPipeline(hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         "titles",
		OnnxFilename: path.Base(e.file),
		Options:      []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
	})
	if err != nil {
		return nil, fmt.Errorf("embedding model: %w", err)
	}

	prefix := ""
	// e5 models are trained with a task prefix; "query: " suits symmetric similarity.
	if strings.Contains(strings.ToLower(e.model), "e5") {
		prefix = "query: "
	}
	out := make([][]float32, 0, len(texts))
	// hugot's Go backend works best with batches of about 32.
	for start := 0; start < len(texts); start += 32 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := texts[start:min(start+32, len(texts))]
		in := make([]string, len(batch))
		for i, t := range batch {
			in[i] = prefix + t
		}
		res, err := pipe.RunPipeline(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("embedding: %w", err)
		}
		out = append(out, res.Embeddings...)
	}
	return out, nil
}

// ensureModel downloads the model on first use (about 120 MB for the default).
func (e *Embedder) ensureModel(ctx context.Context) (string, error) {
	local := filepath.Join(e.dir, strings.ReplaceAll(e.model, "/", "_"))
	if _, err := os.Stat(filepath.Join(local, path.Base(e.file))); err == nil {
		return local, nil
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return "", err
	}
	e.log.Info("downloading embedding model, once", "model", e.model, "file", e.file)
	opts := hugot.NewDownloadOptions()
	opts.OnnxFilePath = e.file
	p, err := hugot.DownloadModel(ctx, e.model, e.dir, opts)
	if err != nil {
		return "", fmt.Errorf("download embedding model %s: %w", e.model, err)
	}
	return p, nil
}

// Similarity is the dot product. Between a unit vector and a story's mean
// vector it equals the average similarity to the story's members.
func Similarity(a, b []float32) float32 {
	var s float32
	for i := range min(len(a), len(b)) {
		s += a[i] * b[i]
	}
	return s
}

// Cosine is the similarity of two vectors of any length.
func Cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / math.Sqrt(na*nb))
}

// AddToMean returns the mean of n vectors plus one more. It is deliberately
// not renormalized: comparing a headline with the plain mean is average
// linkage, so a story whose members disagree has a short mean and attracts
// nothing. Renormalizing it makes big stories drift towards "an average
// headline" and swallow unrelated items.
func AddToMean(mean []float32, n int, v []float32) []float32 {
	next := make([]float32, len(mean))
	for i := range mean {
		next[i] = (mean[i]*float32(n) + v[i]) / float32(n+1)
	}
	return next
}
