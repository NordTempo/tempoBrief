package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RateLimited is returned when the provider refuses a call for now, such as
// OpenRouter's free tier after its daily cap. Until is when to try again.
type RateLimited struct {
	Model  string
	Until  time.Time
	Detail string
}

func (e *RateLimited) Error() string {
	return fmt.Sprintf("%s: rate limited until %s: %.300s", e.Model, e.Until.UTC().Format(time.RFC3339), e.Detail)
}

const (
	// When the provider gives no time, try again after this.
	rateLimitDefault = 5 * time.Minute
	rateLimitMax     = 24 * time.Hour
)

// retryAt reads when a 429 lifts: Retry-After, an X-RateLimit-Reset header,
// or the copy of that header OpenRouter puts in the error body.
func retryAt(h http.Header, body []byte, now time.Time) time.Time {
	until := now.Add(rateLimitDefault)
	if v := h.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil {
			until = now.Add(time.Duration(s) * time.Second)
		} else if t, err := http.ParseTime(v); err == nil {
			until = t
		}
	} else if t, ok := resetTime(h.Get("X-RateLimit-Reset"), now); ok {
		until = t
	} else {
		var b struct {
			Error struct {
				Metadata struct {
					Headers map[string]string `json:"headers"`
				} `json:"metadata"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &b) == nil {
			if t, ok := resetTime(b.Error.Metadata.Headers["X-RateLimit-Reset"], now); ok {
				until = t
			}
		}
	}
	switch {
	case until.Before(now):
		return now.Add(rateLimitDefault)
	case until.After(now.Add(rateLimitMax)):
		return now.Add(rateLimitMax)
	}
	return until
}

// resetTime reads a Unix time in milliseconds or seconds.
func resetTime(v string, now time.Time) (time.Time, bool) {
	n, err := strconv.ParseInt(v, 10, 64)
	switch {
	case err != nil:
		return time.Time{}, false
	case n > 1e12:
		return time.UnixMilli(n), true
	case n > 1e9:
		return time.Unix(n, 0), true
	}
	return time.Time{}, false
}
