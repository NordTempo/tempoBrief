package deliver

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

// ntfy limits a message body to 4096 bytes.
const ntfyMaxBody = 4000

type ntfyAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Method string `json:"method,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
}

type ntfyMessage struct {
	Topic    string       `json:"topic"`
	Title    string       `json:"title"`
	Message  string       `json:"message"`
	Markdown bool         `json:"markdown"`
	Click    string       `json:"click,omitempty"`
	Priority int          `json:"priority,omitempty"`
	Tags     []string     `json:"tags,omitempty"`
	Actions  []ntfyAction `json:"actions,omitempty"`
}

// sendNtfy pushes a brief as one notification (tap to open it), and each
// alert as its own notification with open and vote buttons.
func (d *Deliverer) sendNtfy(ctx context.Context, c settings.Channel, m Message) error {
	if m.Kind == KindAlert {
		for _, st := range m.Stories() {
			msg := ntfyMessage{
				Topic: c.NtfyTopic, Title: st.Title, Markdown: true, Click: st.URL, Priority: 4,
				Message: fmt.Sprintf("%s\n\n%s/10 · %s", st.Summary, scoreLabel(st.Score), sourcesLabel(st.Sources)),
				Actions: []ntfyAction{{Action: "view", Label: "Open", URL: st.URL}},
			}
			if st.UpURL != "" {
				msg.Actions = append(msg.Actions,
					ntfyAction{Action: "http", Label: "👍", URL: st.UpURL, Method: "POST", Clear: true},
					ntfyAction{Action: "http", Label: "👎", URL: st.DownURL, Method: "POST", Clear: true})
			}
			if err := d.postNtfy(ctx, c, msg); err != nil {
				return err
			}
		}
		return nil
	}
	var body strings.Builder
	for _, sec := range m.Sections {
		section := "**" + sec.Topic + "**\n"
		for _, st := range sec.Stories {
			section += fmt.Sprintf("- [%s](%s) · %s\n", st.Title, st.URL, sourcesLabel(st.Sources))
		}
		if body.Len()+len(section) > ntfyMaxBody {
			body.WriteString("…")
			break
		}
		body.WriteString(section + "\n")
	}
	if len(m.Sections) == 0 {
		body.WriteString("Nothing cleared the bar today.")
	}
	return d.postNtfy(ctx, c, ntfyMessage{
		Topic: c.NtfyTopic, Title: m.Title, Message: strings.TrimSpace(body.String()), Markdown: true,
		Click: m.BriefURL, Tags: []string{"newspaper"},
	})
}

func (d *Deliverer) postNtfy(ctx context.Context, c settings.Channel, msg ntfyMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cmp.Or(c.NtfyServer, "https://ntfy.sh"), "/"), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.NtfyToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.NtfyToken)
	}
	return d.do(req)
}

// WebhookPayload is what webhook channels receive. It is also the shape of
// GET /brief.json, so one consumer (Home Assistant, n8n) can use either.
type WebhookPayload struct {
	Kind    Kind           `json:"kind"`
	Title   string         `json:"title"`
	Date    string         `json:"date"`
	URL     string         `json:"url,omitempty"`
	Count   int            `json:"count"`
	Stories []WebhookStory `json:"stories"`
}

type WebhookStory struct {
	ID      int64    `json:"id"`
	Topic   string   `json:"topic"`
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	Summary string   `json:"summary"`
	Score   float64  `json:"score"`
	Sources []string `json:"sources"`
}

// Payload flattens a message for webhooks and the JSON endpoint.
func Payload(m Message) WebhookPayload {
	p := WebhookPayload{Kind: m.Kind, Title: m.Title, Date: m.Date, URL: m.BriefURL, Stories: []WebhookStory{}}
	for _, sec := range m.Sections {
		for _, st := range sec.Stories {
			p.Stories = append(p.Stories, WebhookStory{
				ID: st.ID, Topic: sec.Topic, Title: st.Title, URL: st.URL, Summary: st.Summary,
				Score: st.Score, Sources: st.Sources,
			})
		}
	}
	p.Count = len(p.Stories)
	return p
}

func (d *Deliverer) sendWebhook(ctx context.Context, c settings.Channel, m Message) error {
	b, err := json.Marshal(Payload(m))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return d.do(req)
}

func (d *Deliverer) do(req *http.Request) error {
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}
