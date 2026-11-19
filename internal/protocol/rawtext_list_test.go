package protocol_test

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestRawTextDecodedMixedListsRoundTrip(t *testing.T) {
	t.Parallel()
	mixed := nbt.NewList([]nbt.Tag{nbt.String("prefix"), nbt.Compound{"selector": nbt.String("@s")}})
	for name, original := range map[string]nbt.Tag{
		"root":     mixed,
		"siblings": nbt.Compound{"text": nbt.String("base"), "extra": mixed},
		"hover": nbt.Compound{
			"text":        nbt.String("hover"),
			"hover_event": nbt.Compound{"action": nbt.String("show_text"), "value": mixed},
		},
		"arguments": nbt.Compound{
			"translate": nbt.String("chat.type.text"), "bold": nbt.Byte(1),
			"with": nbt.NewList([]nbt.Tag{nbt.Int(7), nbt.String("message")}),
		},
		"nested": nbt.NewList([]nbt.Tag{nbt.String("outer"), mixed}),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := nbt.EncodeNetwork(original)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := nbt.DecodeNetwork(data)
			if err != nil {
				t.Fatal(err)
			}
			want := decoded
			for pass := range 2 {
				text, ok := protocol.RawTextOf(decoded)
				if !ok || text.Raw == nil {
					t.Fatalf("pass %d: RawTextOf = %+v, %v, want raw component", pass, text, ok)
				}
				var wire protocol.Buffer
				wire.PutText(text)
				var consumed int
				decoded, consumed, err = nbt.DecodeNetwork(wire.Bytes())
				if err != nil {
					t.Fatalf("pass %d: DecodeNetwork: %v", pass, err)
				}
				if consumed != len(wire.Bytes()) {
					t.Fatalf("pass %d: consumed %d of %d bytes", pass, consumed, len(wire.Bytes()))
				}
				if !reflect.DeepEqual(decoded, want) {
					t.Fatalf("pass %d: decoded = %#v, want %#v", pass, decoded, want)
				}
			}
		})
	}
}
