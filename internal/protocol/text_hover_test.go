package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestShowTextHoverRoundTrips checks that a show-text hover survives the wire
// and the NBT form, for a plain and a styled tooltip.
// vanilla: HoverEvent.ShowText
func TestShowTextHoverRoundTrips(t *testing.T) {
	t.Parallel()
	for _, hover := range []Text{{Text: "test:bar"}, {Text: "x", Color: "red"}} {
		text := Text{Text: "a", HoverText: &hover}
		var b Buffer
		b.PutText(text)
		got, ok := ReadText(NewReader(b.Bytes()))
		if !ok || got.HoverText == nil || !namesEqual(*got.HoverText, hover) {
			t.Errorf("wire form of %+v read back as %+v (%v)", text, got, ok)
		}
		fromTag, ok := TextOf(TextTag(text))
		if !ok || fromTag.HoverText == nil || !namesEqual(*fromTag.HoverText, hover) {
			t.Errorf("tag form of %+v read back as %+v (%v)", text, fromTag, ok)
		}
	}
	tag := TextTag(Text{Text: "a", HoverText: &Text{Text: "v"}})
	event, _ := nbt.Get[nbt.Compound](tag.(nbt.CompoundTag).Entries(), "hover_event")
	if action, _ := nbt.Get[nbt.String](event, "action"); action != "show_text" {
		t.Errorf("hover action = %q, want show_text", action)
	}
	if value, _ := nbt.Get[nbt.String](event, "value"); value != "v" {
		t.Errorf("hover value = %q, want v", value)
	}
}
