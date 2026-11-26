package deliver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestBriefPacksIntoFewDiscordMessages(t *testing.T) {
	m := Message{Kind: KindBrief, Title: "Morning brief", BriefURL: "https://brief.test/briefs/1"}
	for _, topic := range []string{"AI", "Dev", "Norge"} {
		sec := Section{Topic: topic}
		for i := range 4 {
			sec.Stories = append(sec.Stories, Story{ID: int64(len(m.Sections)*10 + i + 1),
				Title: fmt.Sprintf("%s story %d", topic, i), URL: "https://example.com", Summary: "What happened.", Score: 7})
		}
		m.Sections = append(m.Sections, sec)
	}
	msgs := briefMessages(m)
	// 12 stories at 4 components each, plus the header: 9 fit in the first message.
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	for i, msg := range msgs {
		n, chars := 0, 0
		for _, c := range msg {
			n++
			if row, ok := c.(discordgo.ActionsRow); ok {
				n += len(row.Components)
			}
			if td, ok := c.(discordgo.TextDisplay); ok {
				chars += len(td.Content)
			}
		}
		if n > discordMaxComponents || chars > discordMaxText {
			t.Errorf("message %d: %d components, %d characters", i, n, chars)
		}
	}
	// Norge starts in the first message and goes on in the second, under its heading again.
	second := msgs[1][0].(discordgo.TextDisplay).Content
	if !strings.HasPrefix(second, "### Norge\n") {
		t.Errorf("continued topic lost its heading: %q", second)
	}
}

// A vote changes only that story's buttons, in a message as Discord sends it back.
func TestVoteMarksOnlyItsStory(t *testing.T) {
	m := Message{Kind: KindBrief, Title: "Brief", Sections: []Section{{Topic: "AI", Stories: []Story{
		{ID: 1, Title: "One", URL: "https://a"}, {ID: 2, Title: "Two", URL: "https://b"},
	}}}}
	b, _ := json.Marshal(map[string]any{"components": briefMessages(m)[0]})
	var sent discordgo.Message
	if err := json.Unmarshal(b, &sent); err != nil {
		t.Fatal(err)
	}
	marked := markVote(sent.Components, 2, 1)
	if len(marked) != len(sent.Components) {
		t.Fatalf("layout changed size: %d to %d", len(sent.Components), len(marked))
	}
	style := func(c discordgo.MessageComponent) discordgo.ButtonStyle {
		switch r := c.(type) {
		case discordgo.ActionsRow:
			return r.Components[0].(discordgo.Button).Style
		case *discordgo.ActionsRow:
			return r.Components[0].(*discordgo.Button).Style
		}
		return 0
	}
	// Header, story 1 text, story 1 row, story 2 text, story 2 row.
	if style(marked[2]) != discordgo.SecondaryButton || style(marked[4]) != discordgo.SuccessButton {
		t.Errorf("want only story 2 liked: %v %v", style(marked[2]), style(marked[4]))
	}
}
