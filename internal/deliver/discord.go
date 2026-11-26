package deliver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

// discordHub keeps one gateway session per bot token. The bot needs no
// privileged intents: it only posts and handles its own vote buttons.
type discordHub struct {
	log  *slog.Logger
	vote VoteFunc

	mu       sync.Mutex
	sessions map[string]*discordgo.Session // by token
	owners   map[string]string             // Discord channel ID -> only user allowed to vote
}

func newDiscordHub(log *slog.Logger, vote VoteFunc) *discordHub {
	return &discordHub{log: log, vote: vote, sessions: map[string]*discordgo.Session{}, owners: map[string]string{}}
}

func (h *discordHub) configure(channels []settings.Channel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	want := map[string]bool{}
	owners := map[string]string{}
	for _, c := range channels {
		if c.Kind == settings.ChannelDiscord && c.Enabled && c.DiscordToken != "" {
			want[c.DiscordToken] = true
			owners[c.DiscordChannelID] = c.DiscordOwnerID
		}
	}
	h.owners = owners
	for token, s := range h.sessions {
		if !want[token] {
			s.Close()
			delete(h.sessions, token)
		}
	}
	for token := range want {
		if _, ok := h.sessions[token]; ok {
			continue
		}
		s, err := discordgo.New("Bot " + token)
		if err != nil {
			h.log.Error("discord: bad token", "err", err)
			continue
		}
		s.Identify.Intents = discordgo.IntentsGuilds
		s.AddHandler(h.onInteraction)
		if err := s.Open(); err != nil {
			h.log.Error("discord: cannot connect", "err", err)
			continue
		}
		h.log.Info("discord: connected", "bot", s.State.User.Username)
		h.sessions[token] = s
	}
}

func (h *discordHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, s := range h.sessions {
		s.Close()
		delete(h.sessions, token)
	}
}

var voteID = regexp.MustCompile(`^vote:(\d+):(-?1)$`)

func (h *discordHub) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}
	m := voteID.FindStringSubmatch(i.MessageComponentData().CustomID)
	if m == nil {
		return
	}
	user := i.User
	if i.Member != nil {
		user = i.Member.User
	}
	h.mu.Lock()
	owner := h.owners[i.ChannelID]
	h.mu.Unlock()
	userID := ""
	if user != nil {
		userID = user.ID
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	vote, _ := strconv.Atoi(m[2])
	s.InteractionRespond(i.Interaction, h.answerVote(id, vote, userID, owner))
}

// answerVote records a vote from a button and says what Discord shows. It
// always answers: without one, Discord tells the reader the app did not
// respond. Notes only the voter sees are ephemeral.
func (h *discordHub) answerVote(id int64, vote int, userID, owner string) *discordgo.InteractionResponse {
	note := func(text string) *discordgo.InteractionResponse {
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: text, Flags: discordgo.MessageFlagsEphemeral},
		}
	}
	if owner != "" && userID != owner {
		return note("Only the owner can vote.")
	}
	if id == TestStoryID {
		return note("The buttons work. Votes on real stories teach tempoBrief what you like.")
	}
	if err := h.vote(context.Background(), id, vote); err != nil {
		h.log.Error("discord: vote failed", "story", id, "err", err)
		if errors.Is(err, store.ErrNotFound) {
			return note("That story is no longer in tempoBrief, so the vote was not saved.")
		}
		return note("Could not save the vote. Try again.")
	}
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Components: voteRow(id, vote)},
	}
}

func (h *discordHub) send(ctx context.Context, c settings.Channel, m Message) error {
	h.mu.Lock()
	s := h.sessions[c.DiscordToken]
	h.mu.Unlock()
	if s == nil {
		return fmt.Errorf("discord bot for %q is not connected", c.Name)
	}
	post := func(content string, story *Story) error {
		msg := &discordgo.MessageSend{Content: content, Flags: discordgo.MessageFlagsSuppressEmbeds}
		if story != nil {
			msg.Components = voteRow(story.ID, story.Vote)
		}
		_, err := s.ChannelMessageSendComplex(c.DiscordChannelID, msg, discordgo.WithContext(ctx))
		return err
	}
	if m.Kind == KindAlert {
		for _, st := range m.Stories() {
			if err := post(formatDiscordStory(st), &st); err != nil {
				return err
			}
		}
		return nil
	}
	header := "## " + m.Title
	if m.BriefURL != "" {
		header += fmt.Sprintf("\n-# [Open in tempoBrief](<%s>)", m.BriefURL)
	}
	if len(m.Sections) == 0 {
		header += "\nNothing cleared the bar today."
	}
	if err := post(header, nil); err != nil {
		return err
	}
	for _, sec := range m.Sections {
		if err := post("### "+sec.Topic, nil); err != nil {
			return err
		}
		for _, st := range sec.Stories {
			if err := post(formatDiscordStory(st), &st); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatDiscordStory(st Story) string {
	// Brackets in titles would break the masked link; <> keeps odd URLs intact.
	lines := []string{fmt.Sprintf("**[%s](<%s>)**", strings.NewReplacer("[", "", "]", "").Replace(st.Title), st.URL)}
	if st.Summary != "" {
		lines = append(lines, st.Summary)
	}
	meta := scoreLabel(st.Score) + "/10 · " + sourcesLabel(st.Sources)
	if st.Why != "" {
		meta += " · " + st.Why
	}
	lines = append(lines, "-# "+meta)
	return strings.Join(lines, "\n")
}

// voteRow renders 👍/👎; the chosen one is highlighted and can still be changed.
func voteRow(id int64, vote int) []discordgo.MessageComponent {
	button := func(value int, emoji string, chosen discordgo.ButtonStyle) discordgo.Button {
		style := discordgo.SecondaryButton
		if vote == value {
			style = chosen
		}
		return discordgo.Button{
			CustomID: fmt.Sprintf("vote:%d:%d", id, value),
			Emoji:    &discordgo.ComponentEmoji{Name: emoji},
			Style:    style,
		}
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		button(1, "👍", discordgo.SuccessButton),
		button(-1, "👎", discordgo.DangerButton),
	}}}
}
