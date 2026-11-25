package settings

import (
	"strings"
	"testing"
)

func TestValidateReportsEveryProblem(t *testing.T) {
	s := Default()
	s.Timezone = "Mars/Olympus"
	s.Briefs[0].Time = "7am"
	s.Briefs[0].Channels = []string{"missing"}
	s.Channels = []Channel{{ID: "d", Kind: ChannelDiscord, Name: "Discord", Enabled: true}}
	err := s.Validate()
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"timezone", "HH:MM", `unknown channel "missing"`, "bot token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in: %v", want, err)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
}

func TestSecretsRoundTrip(t *testing.T) {
	stored := Default()
	stored.Secret, stored.ReadToken = "sign", "read"
	stored.Provider.APIKey = "sk-real"
	stored.Channels = []Channel{{ID: "d", Kind: ChannelDiscord, DiscordToken: "bot-real", DiscordChannelID: "1"}}

	shown := stored.Redact()
	if shown.Provider.APIKey != Redacted || shown.Channels[0].DiscordToken != Redacted || shown.Secret != "" {
		t.Fatalf("secrets leaked: %+v", shown)
	}
	if stored.Channels[0].DiscordToken != "bot-real" {
		t.Fatal("Redact must not modify the original")
	}
	shown.Interests = "new"
	merged := MergeSecrets(stored, shown)
	if merged.Provider.APIKey != "sk-real" || merged.Channels[0].DiscordToken != "bot-real" ||
		merged.Secret != "sign" || merged.ReadToken != "read" || merged.Interests != "new" {
		t.Fatalf("merge lost data: %+v", merged)
	}
	shown.Provider.APIKey = "sk-new"
	if MergeSecrets(stored, shown).Provider.APIKey != "sk-new" {
		t.Fatal("a new key must replace the stored one")
	}
}
