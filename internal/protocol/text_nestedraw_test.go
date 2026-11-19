package protocol

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: ComponentSerialization.CODEC, ListTag.tryUnwrap
func TestPutTextKeepsRawComponentsInsideModelledText(t *testing.T) {
	t.Parallel()
	for _, raw := range []nbt.Tag{
		nbt.Compound{
			"text": nbt.String("Named"),
			"click_event": nbt.Compound{
				"action": nbt.String("show_dialog"),
				"dialog": nbt.Compound{"type": nbt.String("notice"), "title": nbt.String("Title")},
			},
		},
		nbt.String("Named"),
		nbt.NewList([]nbt.Tag{nbt.String("Named"), nbt.Compound{"text": nbt.String(" sibling")}}),
	} {
		encoded, err := nbt.EncodeNetwork(raw)
		if err != nil {
			t.Fatal(err)
		}
		want, _, err := nbt.DecodeNetwork(encoded)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []Text{
			{Italic: true, Extra: []Text{{Raw: raw}}},
			{Text: "Hover", HoverText: &Text{Raw: raw}},
		} {
			var wire Buffer
			wire.PutText(text)
			got, consumed, err := nbt.DecodeNetwork(wire.Bytes())
			if err != nil || consumed != wire.Len() {
				t.Fatalf("decode = %v, consumed %d of %d", err, consumed, wire.Len())
			}
			fields := got.(nbt.Compound)
			var nested nbt.Tag
			if text.HoverText != nil {
				nested = fields["hover_event"].(nbt.Compound)["value"]
			} else {
				nested = nbt.ListElement(fields["extra"].(nbt.List).Items[0])
			}
			if !reflect.DeepEqual(nested, want) {
				t.Errorf("nested component = %#v, want %#v", nested, raw)
			}
		}
	}
}
