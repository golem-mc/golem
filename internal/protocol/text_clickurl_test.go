package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestClickURLSurvivesEveryForm checks that an open_url click is kept by the
// JSON form, the tag form and TextEqual.
func TestClickURLSurvivesEveryForm(t *testing.T) {
	t.Parallel()
	text := Text{Text: "x", ClickURL: "https://a"}
	if got, want := JSONText(text), `{"click_event":{"action":"open_url","url":"https://a"},"text":"x"}`; got != want {
		t.Errorf("JSONText = %s, want %s", got, want)
	}
	cmd := Text{Text: "x", ClickCommand: "/c"}
	if got, want := JSONText(cmd), `{"click_event":{"action":"suggest_command","command":"/c"},"text":"x"}`; got != want {
		t.Errorf("JSONText = %s, want %s", got, want)
	}
	tag, ok := nbt.CompoundView(TextTag(text))
	if !ok {
		t.Fatalf("TextTag = %T, want a compound", TextTag(text))
	}
	click, _ := nbt.CompoundView(tag["click_event"])
	if click["action"] != nbt.String("open_url") || click["url"] != nbt.String("https://a") {
		t.Errorf("click_event = %v", click)
	}
	if TextEqual(text, Text{Text: "x"}) {
		t.Error("TextEqual ignores ClickURL")
	}
	if !TextEqual(text, Text{Text: "x", ClickURL: "https://a"}) {
		t.Error("TextEqual differs for equal ClickURL")
	}
}
