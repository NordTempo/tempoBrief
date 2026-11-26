package deliver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/NordTempo/tempoBrief/internal/store"
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

// Discord shows "the application didn't respond" unless every click gets an
// answer, also when the vote cannot be saved.
func TestEveryVoteClickGetsAnAnswer(t *testing.T) {
	saved := map[int64]int{}
	h := newDiscordHub(slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, id int64, vote int) error {
		if id == 404 {
			return store.ErrNotFound
		}
		saved[id] = vote
		return nil
	})
	for _, c := range []struct {
		id          int64
		user, owner string
		want        string // ephemeral note, or "" for the updated buttons
	}{
		{7, "u1", "", ""},
		{7, "u2", "u1", "Only the owner can vote."},
		{TestStoryID, "u1", "u1", "The buttons work."},
		{404, "u1", "", "no longer in tempoBrief"},
	} {
		r := h.answerVote(c.id, 1, c.user, c.owner, nil)
		if r == nil || r.Data == nil {
			t.Fatalf("story %d: no answer", c.id)
		}
		if c.want == "" && r.Type != discordgo.InteractionResponseUpdateMessage {
			t.Errorf("story %d: want the buttons updated, got %q", c.id, r.Data.Content)
		}
		if c.want != "" && !strings.Contains(r.Data.Content, c.want) {
			t.Errorf("story %d: got %q, want %q", c.id, r.Data.Content, c.want)
		}
	}
	if len(saved) != 1 || saved[7] != 1 {
		t.Errorf("saved votes: %v", saved)
	}
}
