package pipeline

import (
	"testing"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
)

func TestSlotDue(t *testing.T) {
	oslo, _ := time.LoadLocation("Europe/Oslo")
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, oslo) }
	morning := settings.BriefSchedule{Enabled: true, Time: "07:00"}
	weekdays := settings.BriefSchedule{Enabled: true, Time: "07:00", Days: []int{1, 2, 3, 4, 5}}
	installed := at(1, 0, 0)

	tests := []struct {
		name      string
		b         settings.BriefSchedule
		now, last time.Time
		installed time.Time
		want      bool
	}{
		{"before the slot", morning, at(25, 6, 59), time.Time{}, installed, false},
		{"at the slot", morning, at(25, 7, 0), time.Time{}, installed, true},
		{"already sent today", morning, at(25, 7, 30), at(25, 7, 1), installed, false},
		{"sent yesterday", morning, at(25, 7, 30), at(24, 7, 1), installed, true},
		{"too late after downtime", morning, at(25, 10, 1), time.Time{}, installed, false},
		{"installed after the slot", morning, at(25, 7, 30), time.Time{}, at(25, 7, 10), false},
		{"weekend skipped", weekdays, at(29, 7, 5), time.Time{}, installed, false}, // Saturday
		{"weekday sent", weekdays, at(27, 7, 5), time.Time{}, installed, true},     // Thursday
		{"disabled", settings.BriefSchedule{Time: "07:00"}, at(25, 7, 5), time.Time{}, installed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got := SlotDue(tt.b, tt.now, oslo, tt.last, tt.installed); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaste(t *testing.T) {
	liked := store.Signal{Kind: store.SignalLiked, Vector: []float32{1, 0}}
	disliked := store.Signal{Kind: store.SignalDisliked, Vector: []float32{0, 1}}
	ignored := store.Signal{Kind: store.SignalIgnored, Vector: []float32{1, 0}}

	if r := Taste([]float32{1, 0}, nil); r.Neighbors != 0 || r.Adjustment != 0 {
		t.Fatalf("no signals should mean no taste, got %+v", r)
	}
	if r := Taste([]float32{1, 0}, []store.Signal{disliked}); r.Neighbors != 0 {
		t.Fatalf("dissimilar signals should be ignored, got %+v", r)
	}
	one := Taste([]float32{1, 0}, []store.Signal{liked})
	three := Taste([]float32{1, 0}, []store.Signal{liked, liked, liked})
	if one.Adjustment <= 0 || three.Adjustment <= one.Adjustment || three.Adjustment != tasteWeight {
		t.Fatalf("more agreeing neighbours should raise confidence up to the full weight: one %+v, three %+v", one, three)
	}
	mixed := Taste([]float32{1, 0}, []store.Signal{liked, ignored})
	if mixed.Value >= 1 || mixed.Value <= 0 {
		t.Fatalf("an ignore should pull a like down a little, got %+v", mixed)
	}
	if got := FinalScore(10, three); got != 10 {
		t.Fatalf("final score is capped at 10, got %v", got)
	}
}

func TestSectionsOrderTopicsByBestStory(t *testing.T) {
	score := func(f float64) *float64 { return &f }
	list := []store.Story{
		{ID: 1, Topic: "Tech", Score: score(9)},
		{ID: 2, Topic: "Norway", Score: score(8)},
		{ID: 3, Topic: "Tech", Score: score(7)},
		{ID: 4, Topic: "Tech", Score: score(6)},
	}
	capped := capPerTopic(list, 2)
	if len(capped) != 3 {
		t.Fatalf("want 3 after capping Tech at 2, got %d", len(capped))
	}
	secs := sections(capped)
	if secs[0].topic != "Tech" || len(secs[0].stories) != 2 || secs[1].topic != "Norway" {
		t.Fatalf("unexpected sections %+v", secs)
	}
}

func TestEmbedTextGivesShortTitlesContext(t *testing.T) {
	if got := embedText(store.PendingItem{Title: "v2.1.332", Source: "Claude Code releases"}); got != "Claude Code releases: v2.1.332" {
		t.Fatalf("got %q", got)
	}
	if got := embedText(store.PendingItem{Title: "Norges Bank holder renten uendret", Source: "NRK"}); got != "Norges Bank holder renten uendret" {
		t.Fatalf("got %q", got)
	}
}
