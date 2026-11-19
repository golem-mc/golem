package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestClickCopyEncodesCopyToClipboard checks a copy click is written as the
// copy_to_clipboard event with a value.
func TestClickCopyEncodesCopyToClipboard(t *testing.T) {
	t.Parallel()
	tag, ok := nbt.CompoundView(TextTag(Text{Text: "1", ClickCopy: "1"}))
	if !ok {
		t.Fatal("component is not a compound")
	}
	event, _ := nbt.Get[nbt.Compound](tag, "click_event")
	action, _ := nbt.Get[nbt.String](event, "action")
	value, _ := nbt.Get[nbt.String](event, "value")
	if action != "copy_to_clipboard" || value != "1" {
		t.Errorf("click_event = %v", event)
	}
}
