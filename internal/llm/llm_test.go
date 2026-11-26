package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

type answer struct {
	Items []struct {
		ID    int64  `json:"id"`
		Label string `json:"label" desc:"short label"`
	} `json:"items"`
	Done bool `json:"done"`
}

func TestSchemaForIsStrict(t *testing.T) {
	b, _ := json.Marshal(SchemaFor[answer]())
	got := string(b)
	for _, want := range []string{
		`"additionalProperties":false`,
		`"required":["items","done"]`,
		`"required":["id","label"]`,
		`"description":"short label"`,
		`"id":{"type":"integer"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("schema lacks %s: %s", want, got)
		}
	}
}

type staticModel string

func (m staticModel) Name() string { return "static" }
func (m staticModel) JSON(context.Context, Request) ([]byte, error) {
	return []byte(m), nil
}

func TestGenerateRejectsAnswersOffSchema(t *testing.T) {
	if _, err := Generate[answer](context.Background(), staticModel(`{"items":[],"done":true,"extra":1}`), "", "", ""); err == nil {
		t.Fatal("unknown fields should be an error")
	}
	got, err := Generate[answer](context.Background(), staticModel(`{"items":[{"id":3,"label":"x"}],"done":true}`), "", "", "")
	if err != nil || len(got.Items) != 1 || got.Items[0].ID != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOpenRouterDailyCapIsRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded: free-models-per-day.","code":429,"metadata":{"headers":{"X-RateLimit-Limit":"50","X-RateLimit-Remaining":"0","X-RateLimit-Reset":"4102444800000"}}}}`))
	}))
	defer srv.Close()
	m, err := New(settings.Provider{Kind: settings.ProviderOpenAI, BaseURL: srv.URL, Model: "free"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.JSON(context.Background(), Request{})
	var rl *RateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("want RateLimited, got %v", err)
	}
	// The reset is years away, so it is capped to a day.
	if d := time.Until(rl.Until); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("want the reset capped to a day, got %s", d)
	}
}

func TestRetryAt(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		header http.Header
		body   string
		want   time.Time
	}{
		{"retry-after seconds", http.Header{"Retry-After": {"30"}}, "", now.Add(30 * time.Second)},
		{"reset header in seconds", http.Header{"X-Ratelimit-Reset": {strconv.FormatInt(midnight.Unix(), 10)}}, "", midnight},
		{"openrouter body in ms", http.Header{}, `{"error":{"metadata":{"headers":{"X-RateLimit-Reset":"` + strconv.FormatInt(midnight.UnixMilli(), 10) + `"}}}}`, midnight},
		{"nothing given", http.Header{}, `{}`, now.Add(rateLimitDefault)},
	} {
		if got := retryAt(tt.header, []byte(tt.body), now); !got.Equal(tt.want) {
			t.Errorf("%s: want %s, got %s", tt.name, tt.want, got)
		}
	}
}
