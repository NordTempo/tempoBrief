package llm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

// DefaultAnthropicModel is cheap and fast enough to score every story of
// the day. Users pick a bigger model in Settings if they want one.
const DefaultAnthropicModel = "claude-haiku-5-5"

type anthropicModel struct {
	client anthropic.Client
	model  string
}

// newAnthropic uses the key from Settings, or else the SDK's own credential
// chain (ANTHROPIC_API_KEY, an `ant auth login` profile).
func newAnthropic(p settings.Provider) (Model, error) {
	var opts []option.RequestOption
	if p.APIKey != "" {
		opts = append(opts, option.WithAPIKey(p.APIKey))
	}
	if p.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(p.BaseURL))
	}
	return anthropicModel{client: anthropic.NewClient(opts...), model: cmp.Or(p.Model, DefaultAnthropicModel)}, nil
}

func (m anthropicModel) Name() string { return "anthropic/" + m.model }

func (m anthropicModel) JSON(ctx context.Context, req Request) ([]byte, error) {
	params := anthropic.MessageNewParams{
		Model:     m.model,
		MaxTokens: 16000,
		// The system prompt (reader profile, policy, examples) repeats across
		// every scoring batch of a run, so it is worth caching.
		System: []anthropic.TextBlockParam{{Text: req.System, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	if req.Effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Effort)
	}
	resp, err := m.client.Messages.New(ctx, params)
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == http.StatusTooManyRequests && apiErr.Response != nil {
				return nil, &RateLimited{Model: m.Name(), Until: retryAt(apiErr.Response.Header, nil, time.Now()), Detail: apiErr.Error()}
			}
			return nil, fmt.Errorf("%s: HTTP %d: %s", m.Name(), apiErr.StatusCode, apiErr.Error())
		}
		return nil, fmt.Errorf("%s: %w", m.Name(), err)
	}
	switch resp.StopReason {
	case anthropic.StopReasonRefusal:
		return nil, fmt.Errorf("%s: the model declined the request (%s)", m.Name(), resp.StopDetails.Category)
	case anthropic.StopReasonMaxTokens:
		return nil, fmt.Errorf("%s: the answer hit the output limit", m.Name())
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	return []byte(text.String()), nil
}
