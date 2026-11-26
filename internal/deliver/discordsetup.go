package deliver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Discord setup: from a bot token alone, find the bot, its owner, an invite
// link and the channels it can post in, so the reader never copies an ID.
// See docs/delivery.md#discord.

// DiscordAPI is Discord's REST base URL; tests point it at a fake.
var DiscordAPI = "https://discord.com/api/v10"

// discordPermissions is what the bot needs: View Channel and Send Messages.
// Messages suppress embeds and buttons need no permission.
const discordPermissions = 1<<10 | 1<<11

// ErrDiscordToken means Discord refused the token.
var ErrDiscordToken = errors.New("discord does not accept this token")

type DiscordBot struct {
	Name      string
	AppID     string
	OwnerID   string
	OwnerName string
	// InviteURL adds the bot to a server the reader picks, with the
	// permissions above.
	InviteURL string
	Channels  []DiscordChannel
}

type DiscordChannel struct {
	ID    string
	Name  string
	Guild string
}

// CheckDiscordBot looks the bot up with its token.
func CheckDiscordBot(ctx context.Context, token string) (DiscordBot, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	get := func(path string, out any) error { return discordGet(ctx, token, path, out) }

	var app struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Owner struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"owner"`
		Team *struct {
			Name        string `json:"name"`
			OwnerUserID string `json:"owner_user_id"`
		} `json:"team"`
		Bot struct {
			Username string `json:"username"`
		} `json:"bot"`
	}
	if err := get("/oauth2/applications/@me", &app); err != nil {
		return DiscordBot{}, err
	}
	bot := DiscordBot{Name: app.Bot.Username, AppID: app.ID, OwnerID: app.Owner.ID, OwnerName: app.Owner.Username}
	if bot.Name == "" {
		bot.Name = app.Name
	}
	if app.Team != nil { // a team's app: its owner votes
		bot.OwnerID, bot.OwnerName = app.Team.OwnerUserID, app.Team.Name
	}
	bot.InviteURL = "https://discord.com/oauth2/authorize?" + url.Values{
		"client_id": {app.ID}, "scope": {"bot"}, "permissions": {strconv.Itoa(discordPermissions)},
	}.Encode()

	var guilds []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := get("/users/@me/guilds", &guilds); err != nil {
		return DiscordBot{}, err
	}
	for _, g := range guilds {
		var chans []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type int    `json:"type"`
		}
		if err := get("/guilds/"+g.ID+"/channels", &chans); err != nil {
			return DiscordBot{}, err
		}
		for _, c := range chans {
			if c.Type == 0 || c.Type == 5 { // text and announcement channels
				bot.Channels = append(bot.Channels, DiscordChannel{ID: c.ID, Name: c.Name, Guild: g.Name})
			}
		}
	}
	return bot, nil
}

func discordGet(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DiscordAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrDiscordToken
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("discord %s: HTTP %d %s", path, resp.StatusCode, b)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
