package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestShowEntityHoverUUIDForms checks that show_entity takes its uuid as a
// string or as four ints, keeps the identity, and refuses a malformed one.
// vanilla: HoverEvent.EntityTooltipInfo.CODEC
func TestShowEntityHoverUUIDForms(t *testing.T) {
	t.Parallel()
	hover := func(id nbt.Tag) nbt.Tag {
		event := nbt.Compound{}
		event.Set("action", nbt.String("show_entity"))
		event.Set("id", nbt.String("minecraft:player"))
		event.Set("name", nbt.String("Subject"))
		event.Set("uuid", id)
		c := nbt.Compound{}
		c.Set("text", nbt.String("a"))
		c.Set("hover_event", event)
		return c
	}
	for name, parse := range map[string]func(nbt.Tag) (Text, bool){
		"TextOf": TextOf,
		"ReadText": func(tag nbt.Tag) (Text, bool) {
			data, err := nbt.EncodeNetwork(tag)
			if err != nil {
				t.Fatal(err)
			}
			return ReadText(NewReader(data))
		},
	} {
		t.Run(name, func(t *testing.T) { checkHoverUUIDs(t, hover, parse) })
	}
}

func checkHoverUUIDs(t *testing.T, hover func(nbt.Tag) nbt.Tag, parse func(nbt.Tag) (Text, bool)) {
	const str = "32c1e9d0-a046-34fb-8bc1-3986ea745dc4"
	want := [4]uint32{0x32c1e9d0, 0xa04634fb, 0x8bc13986, 0xea745dc4}
	for name, tag := range map[string]nbt.Tag{
		"string": nbt.String(str),
		"ints":   nbt.IntArray{0x32c1e9d0, int32(-0x5fb9cb05), int32(-0x743ec67a), int32(-0x158ba23c)},
	} {
		got, ok := parse(hover(tag))
		if !ok || got.HoverEntity == nil || got.HoverEntity.UUID != want {
			t.Errorf("%s: ok=%v got=%+v, want uuid %x", name, ok, got.HoverEntity, want)
		}
	}
	short, ok := parse(hover(nbt.String("+1-1-1-1-1")))
	if !ok || short.HoverEntity == nil || short.HoverEntity.UUID != [4]uint32{1, 0x10001, 0x10000, 1} {
		t.Errorf("leading plus: ok=%v got=%+v", ok, short.HoverEntity)
	}
	for _, bad := range []nbt.Tag{
		nbt.String("nope"), nbt.String(str + "-1"), nbt.String("g2c1e9d0-a046-34fb-8bc1-3986ea745dc4"),
		nbt.String(""), nbt.String(str + "0"), nbt.String("8000000000000000-0-0-0-0"), nbt.IntArray{1, 2, 3}, nbt.Int(5),
	} {
		if _, ok := parse(hover(bad)); ok {
			t.Errorf("accepted malformed uuid %v", bad)
		}
	}
}
