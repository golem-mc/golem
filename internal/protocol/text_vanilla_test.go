package protocol

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestTextMatchesVanillaSystemChat encodes the components of three System
// Chat messages and compares the decoded trees with the NBT the vanilla
// server sent for the same messages, captured with the probe's HEXDUMP=0x79.
// The field order differs between the two sides, which NBT leaves free, so
// what is pinned is every tag's name, type and value.
func TestTextMatchesVanillaSystemChat(t *testing.T) {
	zero := int64(0)
	gametime := int64(5577)
	for _, tc := range []struct {
		name    string
		text    Text
		vanilla string
	}{
		{
			// /time query gametime, run by Probe: a single argument, which
			// as longs alone goes out a plain list of them.
			name: "feedback of longs alone",
			text: Text{Translate: "commands.time.query.gametime", With: []Text{
				{Long: &gametime},
			}},
			vanilla: "0a09000477697468040000000100000000000015c90800097472616e736c617465001c636f6d6d616e64732e74696d652e71756572792e67616d6574696d6500",
		},
		{
			// /xp add @s 0, run by Dissector: a count and a player's
			// display name.
			name: "feedback naming a player",
			text: Text{Translate: "commands.experience.add.points.success.single", With: []Text{
				{Number: &zero},
				{
					Text:         "Dissector",
					Insertion:    "Dissector",
					ClickCommand: "/tell Dissector ",
					HoverEntity: &HoverEntity{
						Name: Text{Text: "Dissector"},
						ID:   "minecraft:player",
						UUID: [4]uint32{0xd57e479d, 0x80b03140, 0xaee1e721, 0xb820a04d},
					},
				},
			}},
			vanilla: "0a090004776974680a0000000203000000000000000a000b636c69636b5f6576656e74080006616374696f6e000f737567676573745f636f6d6d616e64080007636f6d6d616e6400102f74656c6c20446973736563746f722000080009696e73657274696f6e0009446973736563746f72080004746578740009446973736563746f720a000b686f7665725f6576656e740800046e616d650009446973736563746f72080006616374696f6e000b73686f775f656e74697479080002696400106d696e6563726166743a706c617965720b00047575696400000004d57e479d80b03140aee1e721b820a04d00000800097472616e736c617465002d636f6d6d616e64732e657870657269656e63652e6164642e706f696e74732e737563636573732e73696e676c6500",
		},
		{
			// /gamemode bogus: the failure wrapper around a translation
			// whose one argument is plain text.
			name: "parse error message",
			text: Text{Color: ColorRed, Extra: []Text{
				{Translate: "argument.gamemode.invalid", With: []Text{{Text: "bogus"}}},
			}},
			vanilla: "0a080005636f6c6f72000372656409000565787472610a000000010900047769746808000000010005626f6775730800097472616e736c6174650019617267756d656e742e67616d656d6f64652e696e76616c69640008000474657874000000",
		},
		{
			// The context line after it: the click that puts the input back,
			// and the elided input with the marker.
			name: "parse error context",
			text: Text{Color: ColorRed, Extra: []Text{{
				ClickCommand: "/gamemode bogus",
				Color:        ColorGray,
				Extra: []Text{
					{Text: "..."},
					{Text: "mode bogus"},
					{Translate: "command.context.here", Color: ColorRed, Italic: true},
				},
			}}},
			vanilla: "0a080005636f6c6f72000372656409000565787472610a000000010a000b636c69636b5f6576656e74080006616374696f6e000f737567676573745f636f6d6d616e64080007636f6d6d616e64000f2f67616d656d6f646520626f67757300080005636f6c6f7200046772617909000565787472610a0000000308000000032e2e2e00080000000a6d6f646520626f67757300080005636f6c6f7200037265640100066974616c6963010800097472616e736c6174650014636f6d6d616e642e636f6e746578742e68657265000800047465787400000008000474657874000000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tc.vanilla)
			if err != nil {
				t.Fatal(err)
			}
			want := decodeNetworkNBT(t, raw)
			var b Buffer
			b.PutText(tc.text)
			got := decodeNetworkNBT(t, b.Bytes())
			if !reflect.DeepEqual(normalizeText(got), normalizeText(want)) {
				t.Errorf("component = %#v\nwant the vanilla server's %#v", got, want)
			}
		})
	}
}

// decodeNetworkNBT decodes a nameless network compound by giving it the empty
// name the file format expects.
func decodeNetworkNBT(t *testing.T, data []byte) nbt.Tag {
	t.Helper()
	named := append([]byte{data[0], 0, 0}, data[1:]...)
	_, tag, err := nbt.Decode(named)
	if err != nil {
		t.Fatalf("decode % X: %v", data, err)
	}
	return tag
}

// normalizeText folds the one difference the two encoders may have that the
// client reads the same: a list element written as {"": s} by vanilla's
// heterogeneous-list wrapper and as {text: s} here.
func normalizeText(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case nbt.Compound:
		out := nbt.Compound{}
		for k, child := range v {
			out[k] = normalizeText(child)
		}
		if s, ok := out[""]; ok && len(out) == 1 {
			if _, isString := s.(nbt.String); isString {
				return nbt.Compound{"text": s}
			}
		}
		return out
	case nbt.List:
		items := make([]nbt.Tag, len(v.Items))
		for i, item := range v.Items {
			// A bare string stands where the game's own list would have
			// carried a one-field text component; the wrapped form the
			// file saves a heterogeneous list in is unwrapped on the way
			// in, so both shapes meet here as a bare string.
			if s, isString := item.(nbt.String); isString {
				item = nbt.Compound{"text": s}
			}
			items[i] = normalizeText(item)
		}
		v.Items = items
		return v
	}
	return tag
}
