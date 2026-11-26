package deliver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckDiscordBotFindsOwnerInviteAndChannels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/oauth2/applications/@me":
			fmt.Fprint(w, `{"id":"app1","name":"tempoBrief","owner":{"id":"u42","username":"reader"},"bot":{"username":"tempoBrief"}}`)
		case "/users/@me/guilds":
			fmt.Fprint(w, `[{"id":"g1","name":"Hjemme"}]`)
		case "/guilds/g1/channels":
			// A category (4) and a voice channel (2) are no place for a brief.
			fmt.Fprint(w, `[{"id":"c0","name":"Text","type":4},{"id":"c1","name":"nyheter","type":0},
				{"id":"c2","name":"Lounge","type":2},{"id":"c3","name":"announcements","type":5}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	DiscordAPI = srv.URL
	defer func() { DiscordAPI = "https://discord.com/api/v10" }()

	bot, err := CheckDiscordBot(context.Background(), "good")
	if err != nil {
		t.Fatal(err)
	}
	if bot.OwnerID != "u42" || bot.OwnerName != "reader" || bot.Name != "tempoBrief" {
		t.Errorf("bot: %+v", bot)
	}
	if !strings.Contains(bot.InviteURL, "client_id=app1") || !strings.Contains(bot.InviteURL, "permissions=3072") {
		t.Errorf("invite: %s", bot.InviteURL)
	}
	if len(bot.Channels) != 2 || bot.Channels[0] != (DiscordChannel{ID: "c1", Name: "nyheter", Guild: "Hjemme"}) {
		t.Errorf("channels: %+v", bot.Channels)
	}
	if _, err := CheckDiscordBot(context.Background(), "bad"); !errors.Is(err, ErrDiscordToken) {
		t.Errorf("bad token: %v", err)
	}
}
