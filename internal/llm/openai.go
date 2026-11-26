package llm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

const defaultOpenAIBase = "https://api.openai.com/v1"

// openAIModel speaks the chat completions protocol with JSON Schema output,
// which OpenAI, OpenRouter, Ollama, LM Studio and vLLM all implement. Plain
// HTTP rather than an SDK, because most users of this path are not on OpenAI.
type openAIModel struct {
	base, key, model string
	http             *http.Client
}

func newOpenAI(p settings.Provider) (Model, error) {
	return openAIModel{
		base:  strings.TrimRight(cmp.Or(p.BaseURL, defaultOpenAIBase), "/"),
		key:   p.APIKey,
		model: p.Model,
		http:  &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (m openAIModel) Name() string { return "openai/" + m.model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m openAIModel) JSON(ctx context.Context, req Request) ([]byte, error) {
	body := map[string]any{
		"model": m.model,
		"messages": []chatMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.Prompt},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "answer", "strict": true, "schema": req.Schema},
		},
	}
	// Only OpenAI's own API is known to accept this; local servers may reject it.
	if req.Effort != "" && m.base == defaultOpenAIBase {
		body["reasoning_effort"] = req.Effort
	}
	// OpenRouter routes a model to several hosts; only use the ones that honour
	// the JSON schema, instead of getting free text back from the others.
	if strings.Contains(m.base, "openrouter.ai") {
		body["provider"] = map[string]any{"require_parameters": true}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if m.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+m.key)
	}
	resp, err := m.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %.500s", m.Name(), resp.StatusCode, raw)
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: unexpected response: %w", m.Name(), err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices in response", m.Name())
	}
	c := out.Choices[0]
	if c.Message.Refusal != "" {
		return nil, fmt.Errorf("%s: the model declined: %s", m.Name(), c.Message.Refusal)
	}
	if c.FinishReason == "length" {
		return nil, fmt.Errorf("%s: the answer hit the output limit", m.Name())
	}
	return []byte(c.Message.Content), nil
}
