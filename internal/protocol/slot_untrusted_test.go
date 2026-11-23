package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestUntrustedSlotMatchesVanilla reads the stacks a vanilla client
// sends the Set Creative Mode Slot packet in: the vanilla captures behind
// TestSlotMatchesVanilla, each component's value prefixed by the byte length
// the untrusted shape adds over the trusted one, so the shape is pinned byte
// for byte, component by component.
func TestUntrustedSlotMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  string
		stack ItemStack
	}{
		{
			// The enchanted sword: enchantments, ID 13, sharpness (ID 33)
			// at level 5, its three bytes prefixed with a three.
			name:  "enchanted sword",
			want:  "019a0801000d03012105",
			stack: ItemStack{ItemID: 1050, Count: 1, Enchantments: map[int32]int32{33: 5}},
		},
		{
			// The renamed diamond: custom_name, ID 6, the bare string "Bob".
			name:  "renamed diamond",
			want:  "01f40701000606080003426f62",
			stack: ItemStack{ItemID: 1012, Count: 1, CustomName: &Text{Text: "Bob"}},
		},
		{
			// The enchanted book: stored_enchantments, ID 45.
			name:  "enchanted book",
			want:  "01f30a01002d03012103",
			stack: ItemStack{ItemID: 1395, Count: 1, StoredEnchantments: map[int32]int32{33: 3}},
		},
		{
			// The repaired, glinting diamond: repair_cost, ID 19, and
			// glint_override, ID 21.
			name:  "repaired, glinting diamond",
			want:  "01f4070200130107150101",
			stack: ItemStack{ItemID: 1012, Count: 1, RepairCost: 7, GlintOverride: GlintShown},
		},
		{
			// The worn, enchanted sword: damage, ID 3, then enchantments.
			name:  "worn, enchanted sword",
			want:  "019a08020003010a0d03012105",
			stack: ItemStack{ItemID: 1050, Count: 1, Damage: 10, Enchantments: map[int32]int32{33: 5}},
		},
		{
			// The unbreakable pickaxe: the flag, ID 4, whose value is the
			// unit, so its prefix is a zero.
			name:  "unbreakable pickaxe",
			want:  "019c0801000400",
			stack: ItemStack{ItemID: 1052, Count: 1, Unbreakable: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := NewReader(want).ReadUntrustedSlot()
			if !ok || !got.Equal(tc.stack) {
				t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, tc.stack)
			}
		})
	}
}

// TestUntrustedSlotRefuses pins the boundaries the length prefix draws. The
// trusted shape — the bytes the server itself puts on the wire, with no
// lengths at all — is not the shape this reads: reading a component's length
// out of the value it prefixes refuses every one of the vanilla captures.
func TestUntrustedSlotRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{
			// The length claims ten bytes where three follow.
			name: "length past the packet's end",
			data: "019a0801000d0a012105",
		},
		{
			// Two bytes where the entry needs three: the value ends
			// inside its own component.
			name: "value ends inside its component",
			data: "019a0801000d020121",
		},
		{
			// A custom name of no bytes, where the bare string tag is due.
			name: "empty value where data is due",
			data: "01f40701000600",
		},
		{
			// A length of negative one, which no slice can be.
			name: "negative length",
			data: "019a0801000dffffffff0f012105",
		},
		{
			// The trusted shape: the length this reader reads first is the
			// value's own first byte, and what follows is not what it says.
			name: "trusted shape, untrusted reader",
			data: "019a0801000d012105",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if stack, ok := NewReader(data).ReadUntrustedSlot(); ok {
				t.Errorf("ReadUntrustedSlot = %+v, want the stack refused", stack)
			}
		})
	}
}

// TestUntrustedSlotRemovesComponents checks the component lists the
// untrusted shape carries besides the added values: a removed component is a
// type alone, taken back to its item's default, and a stack may add one and
// take another away in the same packet.
func TestUntrustedSlotRemovesComponents(t *testing.T) {
	data, err := hex.DecodeString("019a08020103010a040003")
	if err != nil {
		t.Fatal(err)
	}
	want := ItemStack{ItemID: 1050, Count: 1, Unbreakable: true}
	want.Remove(DataComponentDamage)
	got, ok := NewReader(data).ReadUntrustedSlot()
	if !ok || !got.Equal(want) {
		t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, want)
	}
}

// TestUntrustedSlotReadsAContainerHoldingRawEntries checks the container
// component whose entries carry what this server holds raw: a shulker box of
// sixty-four diamonds and a potion is the potion_contents inside its
// container's entry, and the entry's components travel bare, so the potion's
// bytes are checked the same way they are at the top of a slot and kept on
// the entry they belong to. The box comes back the box it was, potion and
// all.
func TestUntrustedSlotReadsAContainerHoldingRawEntries(t *testing.T) {
	// The stack a vanilla client sends for a shulker box it filled
	// with a stack of diamonds and a potion: the container, component 77,
	// its value length-prefixed, the entries inside it bare.
	data, err := hex.DecodeString("01900501004d130201f40740000001f709010100350118000000")
	if err != nil {
		t.Fatal(err)
	}
	want := ItemStack{ItemID: 656, Count: 1, Container: []ItemStack{
		{ItemID: 1012, Count: 64},
		{ItemID: 1271, Count: 1, Raw: []RawComponent{
			{Type: DataComponentPotionContents, Data: []byte{0x01, 0x18, 0x00, 0x00, 0x00}},
		}},
	}}
	got, ok := NewReader(data).ReadUntrustedSlot()
	if !ok || !got.Equal(want) {
		t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, want)
	}

	// And the box rides back out both ways, the potion's bytes where they
	// were: bare in the trusted shape the server sends, behind their
	// length in the untrusted one it received.
	var b Buffer
	b.PutSlot(got)
	if wantTrusted := "01900501004d0201f40740000001f709010100350118000000"; hex.EncodeToString(b.Bytes()) != wantTrusted {
		t.Errorf("PutSlot = %s, want %s", hex.EncodeToString(b.Bytes()), wantTrusted)
	}
	b.Reset()
	b.PutUntrustedSlot(got)
	if hex.EncodeToString(b.Bytes()) != hex.EncodeToString(data) {
		t.Errorf("PutUntrustedSlot = %s, want the bytes it arrived as %s",
			hex.EncodeToString(b.Bytes()), hex.EncodeToString(data))
	}
}

// TestPutUntrustedSlotMatchesVanilla writes the stacks a vanilla client
// sends the Set Creative Mode Slot packet in, the captures behind
// TestUntrustedSlotMatchesVanilla, so the writer's bytes are the reader's.
func TestPutUntrustedSlotMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  string
		stack ItemStack
	}{
		{
			name:  "enchanted sword",
			want:  "019a0801000d03012105",
			stack: ItemStack{ItemID: 1050, Count: 1, Enchantments: map[int32]int32{33: 5}},
		},
		{
			name:  "renamed diamond",
			want:  "01f40701000606080003426f62",
			stack: ItemStack{ItemID: 1012, Count: 1, CustomName: &Text{Text: "Bob"}},
		},
		{
			name:  "enchanted book",
			want:  "01f30a01002d03012103",
			stack: ItemStack{ItemID: 1395, Count: 1, StoredEnchantments: map[int32]int32{33: 3}},
		},
		{
			name:  "repaired, glinting diamond",
			want:  "01f4070200130107150101",
			stack: ItemStack{ItemID: 1012, Count: 1, RepairCost: 7, GlintOverride: GlintShown},
		},
		{
			name:  "worn, enchanted sword",
			want:  "019a08020003010a0d03012105",
			stack: ItemStack{ItemID: 1050, Count: 1, Damage: 10, Enchantments: map[int32]int32{33: 5}},
		},
		{
			name:  "unbreakable pickaxe",
			want:  "019c0801000400",
			stack: ItemStack{ItemID: 1052, Count: 1, Unbreakable: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Buffer
			b.PutUntrustedSlot(tc.stack)
			if got := hex.EncodeToString(b.Bytes()); got != tc.want {
				t.Errorf("PutUntrustedSlot = %s, want %s", got, tc.want)
			}
		})
	}
}

// untrustedSlotOf wraps one component's value the untrusted shape carries
// it: one stack of the item, the one component added, its value behind its
// byte length.
func untrustedSlotOf(item, component int32, value []byte) []byte {
	var b Buffer
	b.PutVarInt(1)    // count
	b.PutVarInt(item) // the item
	b.PutVarInt(1)    // components to add
	b.PutVarInt(0)    // none to remove
	b.PutVarInt(component)
	b.PutVarInt(int32(len(value)))
	b.PutRaw(value)
	return b.Bytes()
}

// textValue writes a text component the way the wire carries one.
func textValue(t Text) []byte {
	var b Buffer
	b.PutText(t)
	return b.Bytes()
}

// bookOf writes a written_book_content value: the title, no filtered twin,
// the author, the generation, the pages, each without its filtered twin,
// and the resolved flag.
func bookOf(generation int32, pages ...[]byte) []byte {
	var b Buffer
	b.PutString("T")
	b.PutBool(false)
	b.PutString("A")
	b.PutVarInt(generation)
	b.PutVarInt(int32(len(pages)))
	for _, page := range pages {
		b.PutRaw(page)
		b.PutBool(false)
	}
	b.PutBool(false)
	return b.Bytes()
}

// TestUntrustedSlotRefusesATextTheCodecRefuses pins the text component's
// own check, the one the game's ComponentSerialization runs: a component
// the reading client would refuse to decode refuses the stack, which is
// relayed — and the relay would take the client down with it.
func TestUntrustedSlotRefusesATextTheCodecRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{
			name:  "an empty compound",
			value: []byte{0x0a, 0x00, 0x00, 0x00},
		},
		{
			name:  "a colour the codec does not name",
			value: textValue(Text{Text: "a", Color: "zzz"}),
		},
		{
			name:  "a bare number where a component is due",
			value: []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05},
		},
		{
			name: "an empty sibling list",
			value: []byte{0x0a, 0x00, 0x00, 0x08, 0x00, 0x04, 't', 'e', 'x', 't',
				0x00, 0x01, 'a', 0x09, 0x00, 0x05, 'e', 'x', 't', 'r', 'a',
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name: "a hover event outside the identifiers",
			value: textValue(Text{Text: "a",
				HoverEntity: &HoverEntity{Name: Text{Text: "b"}, ID: "NOT VALID"}}),
		},
		{
			// The anonymous-tag wrapper belongs to a component list's
			// elements, not to a place a component itself is due; there it
			// is a compound saying nothing, refused as the empty one is.
			name:  "a wrapper where a component is due",
			value: []byte{0x0a, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0x00},
		},
		{
			name:  "a wrapper naming text where a component is due",
			value: []byte{0x0a, 0x08, 0x00, 0x00, 0x00, 0x01, 'a', 0x00},
		},
		{
			// Unwrapped, a sibling list's element is a bare number, and
			// only a translation's with-list has those.
			name: "a sibling list's wrapped number",
			value: []byte{0x0a, 0x08, 0x00, 0x04, 't', 'e', 'x', 't',
				0x00, 0x01, 'a', 0x09, 0x00, 0x05, 'e', 'x', 't', 'r', 'a',
				0x0a, 0x00, 0x00, 0x00, 0x01, 0x03, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x05, 0x00, 0x00},
		},
		{
			// The client resolves the id against its own entity type
			// registry while decoding, so a name that registry does not
			// carry takes it down whatever identifier it is.
			name: "a hover event naming no entity type",
			value: textValue(Text{Text: "a",
				HoverEntity: &HoverEntity{Name: Text{Text: "b"}, ID: "minecraft:zzz"}}),
		},
		{
			name: "a click command with the section sign",
			value: textValue(Text{Text: "a",
				ClickCommand: "say §chello"}),
		},
		{
			name: "a click command with a control character",
			value: textValue(Text{Text: "a",
				ClickCommand: "say \x01hello"}),
		},
		{
			name: "a click command with DEL",
			value: textValue(Text{Text: "a",
				ClickCommand: "say \x7f"}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if stack, ok := NewReader(untrustedSlotOf(1012, DataComponentCustomName, tc.value)).ReadUntrustedSlot(); ok {
				t.Errorf("ReadUntrustedSlot = %+v, want the stack refused", stack)
			}
		})
	}
}

// TestUntrustedSlotRefusesABadWrittenBook pins the written_book_content
// check: a generation past the third, and a page the component codec would
// refuse — an empty compound, an unnamed colour, a bare number, an empty
// sibling list, a hover event naming no identifier or no entity type, a
// click carrying a character the chat box refuses, a wrapper where a
// component is due — each refuse the book.
func TestUntrustedSlotRefusesABadWrittenBook(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{
			name:  "a fourth generation",
			value: bookOf(4, textValue(Text{Text: "a"})),
		},
		{
			name:  "an empty page",
			value: bookOf(0, []byte{0x0a, 0x00, 0x00, 0x00}),
		},
		{
			name:  "a page the codec refuses the colour of",
			value: bookOf(0, textValue(Text{Text: "a", Color: "zzz"})),
		},
		{
			name:  "a page that is a bare number",
			value: bookOf(0, []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05}),
		},
		{
			name: "a page with an empty sibling list",
			value: bookOf(0, []byte{0x0a, 0x00, 0x00, 0x08, 0x00, 0x04, 't', 'e', 'x', 't',
				0x00, 0x01, 'a', 0x09, 0x00, 0x05, 'e', 'x', 't', 'r', 'a',
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00}),
		},
		{
			name: "a page whose hover names no identifier",
			value: bookOf(0, textValue(Text{Text: "a",
				HoverEntity: &HoverEntity{Name: Text{Text: "b"}, ID: "NOT VALID"}})),
		},
		{
			name: "a page whose hover names no entity type",
			value: bookOf(0, textValue(Text{Text: "a",
				HoverEntity: &HoverEntity{Name: Text{Text: "b"}, ID: "minecraft:zzz"}})),
		},
		{
			name: "a page whose click carries the section sign",
			value: bookOf(0, textValue(Text{Text: "a",
				ClickCommand: "say §chello"})),
		},
		{
			name:  "a page that wraps a number where a component is due",
			value: bookOf(0, []byte{0x0a, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0x00}),
		},
		{
			name:  "a page that wraps text where a component is due",
			value: bookOf(0, []byte{0x0a, 0x08, 0x00, 0x00, 0x00, 0x01, 'a', 0x00}),
		},
		{
			name: "a page whose sibling list wraps a number",
			value: bookOf(0, []byte{0x0a, 0x08, 0x00, 0x04, 't', 'e', 'x', 't',
				0x00, 0x01, 'a', 0x09, 0x00, 0x05, 'e', 'x', 't', 'r', 'a',
				0x0a, 0x00, 0x00, 0x00, 0x01, 0x03, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x05, 0x00, 0x00}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if stack, ok := NewReader(untrustedSlotOf(1012, DataComponentWrittenBookContent, tc.value)).ReadUntrustedSlot(); ok {
				t.Errorf("ReadUntrustedSlot = %+v, want the stack refused", stack)
			}
		})
	}

	// A book of a hundred-and-one pages is not one of those: the writable
	// book's codec binds its pages to a hundred, the written book's list
	// being unbounded in vanilla, and the book rides whole, the pages the
	// bytes they arrived as.
	var b Buffer
	b.PutString("T")
	b.PutBool(false)
	b.PutString("A")
	b.PutVarInt(0)
	b.PutVarInt(101)
	for range 101 {
		b.PutU8(0x08) // a page that is a one-character string tag
		b.PutI16(1)
		b.PutU8('p')
		b.PutBool(false) // no filtered twin
	}
	b.PutBool(false)
	long := b.Bytes()
	got, ok := NewReader(untrustedSlotOf(1012, DataComponentWrittenBookContent, long)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the hundred-and-one-page book")
	}
	if len(got.Raw) != 1 || !bytes.Equal(got.Raw[0].Data, long) {
		t.Errorf("the book's pages = % X, want the bytes it arrived as % X", got.Raw[0].Data, long)
	}

	// And the book the codecs read rides whole, the pages the bytes they
	// arrived as.
	value := bookOf(1, textValue(Text{Text: "hello"}),
		textValue(Text{Text: "a", Color: "gold", Italic: true}))
	got, ok = NewReader(untrustedSlotOf(1012, DataComponentWrittenBookContent, value)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the book the codecs read")
	}
	if len(got.Raw) != 1 || !bytes.Equal(got.Raw[0].Data, value) {
		t.Errorf("the book's pages = % X, want the bytes it arrived as % X", got.Raw[0].Data, value)
	}
}

// TestUntrustedSlotReadsTheTextTheClientReads pins the acceptance side of
// the component check: a component list keeps one element type by wrapping
// its plain values, and the game unwraps the wrapper there and only there —
// a sibling list's wrapped text is the component it stands for, a
// with-list's the bare number the codec takes there — and a hover naming a
// real entity type, like a click the chat box would take, reads whole.
func TestUntrustedSlotReadsTheTextTheClientReads(t *testing.T) {
	wrappedText := []byte{0x0a, 0x08, 0x00, 0x04, 't', 'e', 'x', 't',
		0x00, 0x01, 'a', 0x09, 0x00, 0x05, 'e', 'x', 't', 'r', 'a',
		0x0a, 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x00, 0x00, 0x01,
		'a', 0x00, 0x00}
	got, ok := NewReader(untrustedSlotOf(1012, DataComponentCustomName, wrappedText)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the wrapped sibling")
	}
	if name := got.CustomName; name == nil || name.Text != "a" ||
		len(name.Extra) != 1 || name.Extra[0].Text != "a" {
		t.Errorf("the name = %+v, want a with the wrapped text as its sibling", got.CustomName)
	}

	five := int64(5)
	got, ok = NewReader(untrustedSlotOf(1012, DataComponentCustomName,
		textValue(Text{Translate: "multiplayer.player.joined",
			With: []Text{{Number: &five}, {Text: "x"}}}))).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the wrapped with-arguments")
	}
	name := got.CustomName
	if name == nil || len(name.With) != 2 ||
		name.With[0].Number == nil || *name.With[0].Number != 5 || name.With[1].Text != "x" {
		t.Errorf("the name = %+v, want the wrapped count and text among its arguments", name)
	}

	got, ok = NewReader(untrustedSlotOf(1012, DataComponentCustomName,
		textValue(Text{Text: "a",
			HoverEntity:  &HoverEntity{Name: Text{Text: "b"}, ID: "minecraft:pig", UUID: [4]uint32{1, 2, 3, 4}},
			ClickCommand: "say hi"}))).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the hover and click the client takes")
	}
	if name := got.CustomName; name == nil || name.HoverEntity == nil ||
		name.HoverEntity.ID != "minecraft:pig" || name.ClickCommand != "say hi" {
		t.Errorf("the name = %+v, want its hover and click read back", got.CustomName)
	}
}

// TestUntrustedSlotRefusesABadBundle pins the bundle_contents check: an
// entry that is air, the item the template's own constructor refuses, and
// an entry that holds nothing.
func TestUntrustedSlotRefusesABadBundle(t *testing.T) {
	var b Buffer
	b.PutVarInt(1) // one entry
	b.PutVarInt(0) // air
	if stack, ok := NewReader(untrustedSlotOf(1012, DataComponentBundleContents, b.Bytes())).ReadUntrustedSlot(); ok {
		t.Errorf("ReadUntrustedSlot = %+v, want the stack refused", stack)
	}

	b.Reset()
	b.PutVarInt(1)
	b.PutVarInt(1) // stone
	b.PutVarInt(0) // a count of nothing
	if stack, ok := NewReader(untrustedSlotOf(1012, DataComponentBundleContents, b.Bytes())).ReadUntrustedSlot(); ok {
		t.Errorf("ReadUntrustedSlot = %+v, want the stack refused", stack)
	}
}

// effectDetails writes one effect instance's details, its hidden twin
// nesting hidden deep.
func effectDetails(b *Buffer, hidden int) {
	b.PutVarInt(0)   // amplifier
	b.PutVarInt(1)   // duration
	b.PutBool(false) // ambient
	b.PutBool(true)  // show particles
	b.PutBool(true)  // show icon
	for range hidden {
		b.PutBool(true) // another hidden twin follows
		b.PutVarInt(0)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutBool(true)
	}
	b.PutBool(false) // the innermost has none
}

// potionOf writes a potion_contents value: the potion, no custom colour,
// the effects, no custom name.
func potionOf(effects ...func(*Buffer)) []byte {
	var b Buffer
	b.PutBool(true)
	b.PutVarInt(5)   // the potion
	b.PutBool(false) // no custom colour
	b.PutVarInt(int32(len(effects)))
	for _, effect := range effects {
		b.PutVarInt(5) // the effect's ID
		effect(&b)
	}
	b.PutBool(false) // no custom name
	return b.Bytes()
}

// bundleHolding writes a bundle_contents value whose single entry carries
// one component bare, the way a patch inside a bundle rides: the item and
// count, the one component added, its value with no length of its own.
func bundleHolding(component int32, writeValue func(*Buffer)) []byte {
	var b Buffer
	b.PutVarInt(1) // one entry
	b.PutVarInt(1) // minecraft:stone
	b.PutVarInt(1) // a count of one
	b.PutVarInt(1) // one component added
	b.PutVarInt(0) // none removed
	b.PutVarInt(component)
	writeValue(&b)
	return b.Bytes()
}

// TestUntrustedSlotReadsAndRefusesTrims pins the trim component's
// acceptance: the material and pattern references the smithing table writes
// read back, the one-past-the-registry references the codec bounds answer
// are refused, and the inline shapes the codec also carries read whole.
// vanilla: ArmorTrim.STREAM_CODEC
func TestUntrustedSlotReadsAndRefusesTrims(t *testing.T) {
	var b Buffer
	b.PutVarInt(5 + 1) // material id 5, the reference one past it
	b.PutVarInt(8 + 1) // pattern id 8, the same
	got, ok := NewReader(untrustedSlotOf(1084, DataComponentTrim, b.Bytes())).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the referenced trim")
	}
	if m, p, ok := got.TrimOf(); !ok || m != 5 || p != 8 {
		t.Errorf("TrimOf = %d/%d (ok %t), want the references 5 and 8", m, p, ok)
	}

	for _, refuted := range []struct {
		name  string
		value []byte
	}{
		{"a material one past the registry", func() []byte {
			b.Reset()
			b.PutVarInt(RegistrySizeTrimMaterial + 1)
			b.PutVarInt(1)
			return b.Bytes()
		}()},
		{"a pattern one past the registry", func() []byte {
			b.Reset()
			b.PutVarInt(1)
			b.PutVarInt(RegistrySizeTrimPattern + 1)
			return b.Bytes()
		}()},
		{"a negative pattern reference", func() []byte {
			// The varint encodes 0x80...: a negative number, the byte
			// the material half would refuse the same way.
			b.Reset()
			b.PutVarInt(1)
			b.PutRaw([]byte{0xff, 0xff, 0xff, 0xff, 0x0f})
			return b.Bytes()
		}()},
	} {
		if stack, ok := NewReader(untrustedSlotOf(1084, DataComponentTrim, refuted.value)).ReadUntrustedSlot(); ok {
			t.Errorf("ReadUntrustedSlot read %s: the stack %+v", refuted.name, stack)
		}
	}

	// The inline halves: a material of a palette and a description, then
	// the pattern of an asset, its description and the not-a-decal flag.
	b.Reset()
	b.PutVarInt(0) // the inline material
	b.PutString("minecraft:copper")
	b.PutText(Text{Text: "the description"})
	b.PutVarInt(0) // the inline pattern
	b.PutString("minecraft:sentry")
	b.PutText(Text{Text: "the pattern"})
	b.PutBool(false)
	got, ok = NewReader(untrustedSlotOf(1084, DataComponentTrim, b.Bytes())).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the inline trim the codecs read")
	}
	if _, _, trimmed := got.TrimOf(); trimmed {
		t.Error("TrimOf read an inline trim as its registry references")
	}
}

// adventureValue writes one can_place_on or can_break component's value with
// a single predicate in it, its blocks and property matcher the caller's and
// the rest empty.
func adventureValue(blocks func(*Buffer), props func(*Buffer), guarded bool) []byte {
	var b Buffer
	b.PutVarInt(1) // one BlockPredicate
	if blocks == nil {
		b.PutBool(false) // no block set: every kind passes that half
	} else {
		b.PutBool(true)
		blocks(&b)
	}
	if props == nil {
		b.PutBool(false) // no property matchers
	} else {
		b.PutBool(true)
		props(&b)
	}
	if guarded {
		b.PutBool(true)
		networkTag, err := nbt.EncodeNetwork(nbt.Compound{})
		if err != nil {
			panic(err)
		}
		b.PutRaw(networkTag)
	} else {
		b.PutBool(false)
	}
	b.PutVarInt(0) // no exact component matchers
	b.PutVarInt(0) // no partial ones
	return b.Bytes()
}

// TestUntrustedSlotReadsAndRefusesAdventurePredicates checks the
// can_place_on and can_break codecs: the value a stack conveys them in is
// kept as its raw bytes and read back predicate-shaped by the accessor, and
// a value that does not decode refuses the stack.
// vanilla: AdventureModePredicate.STREAM_CODEC
func TestUntrustedSlotReadsAndRefusesAdventurePredicates(t *testing.T) {
	// The value vanilla's parser writes for can_place_on=[minecraft:dirt]:
	// the block named inline, by its minecraft:block registry ID.
	inline := adventureValue(func(b *Buffer) {
		b.PutVarInt(2) // one past the count: one inline block
		b.PutVarInt(9) // dirt
	}, nil, false)
	got, ok := NewReader(untrustedSlotOf(1050, DataComponentCanPlaceOn, inline)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the inline can_place_on")
	}
	if len(got.Raw) != 1 || got.Raw[0].Type != DataComponentCanPlaceOn ||
		!bytes.Equal(got.Raw[0].Data, inline) {
		t.Fatalf("the stack carries %+v, want the component whole", got.Raw)
	}
	predicates, ok := got.CanPlaceOn()
	if !ok || len(predicates) != 1 || !predicates[0].Blocks.Present ||
		len(predicates[0].Blocks.Inline) != 1 || predicates[0].Blocks.Inline[0] != 9 ||
		predicates[0].Blocks.Tag != "" || len(predicates[0].Props) != 0 || predicates[0].NBT != nil {
		t.Errorf("CanPlaceOn = %+v (ok %t), want one predicate carrying dirt", predicates, ok)
	}

	// The set's named half: a tag's identifier, and the two components
	// share the shape, as the accessor for can_break reads.
	tagged := adventureValue(func(b *Buffer) {
		b.PutVarInt(0) // the named form: a tag follows
		b.PutString("minecraft:logs")
	}, nil, false)
	got, ok = NewReader(untrustedSlotOf(1050, DataComponentCanBreak, tagged)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the tagged can_break")
	}
	predicates, ok = got.CanBreak()
	if !ok || len(predicates) != 1 || predicates[0].Blocks.Tag != "minecraft:logs" {
		t.Errorf("CanBreak = %+v (ok %t), want one predicate naming the logs tag", predicates, ok)
	}

	// A predicate with only a property matcher, and one whose block-entity
	// NBT guard decodes: both read, the guard remembered as present.
	exactProps := adventureValue(nil, func(b *Buffer) {
		b.PutVarInt(1)
		b.PutString("snowy")
		b.PutBool(true) // the exact form
		b.PutString("true")
	}, false)
	got, ok = NewReader(untrustedSlotOf(1050, DataComponentCanPlaceOn, exactProps)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the property matcher")
	}
	predicates, ok = got.CanPlaceOn()
	if !ok || len(predicates) != 1 || len(predicates[0].Props) != 1 ||
		predicates[0].Props[0].Name != "snowy" || predicates[0].Props[0].Value != "true" ||
		predicates[0].Props[0].Ranged {
		t.Errorf("CanPlaceOn = %+v (ok %t), want the snowy property matcher", predicates, ok)
	}
	guarded := adventureValue(nil, nil, true)
	got, ok = NewReader(untrustedSlotOf(1050, DataComponentCanPlaceOn, guarded)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the guarded predicate")
	}
	if predicates, ok := got.CanPlaceOn(); !ok || len(predicates) != 1 || predicates[0].NBT == nil {
		t.Errorf("CanPlaceOn = %+v (ok %t), want the guard read as present", predicates, ok)
	}

	for _, refuted := range []struct {
		name  string
		value []byte
	}{
		{"a block one past the registry", adventureValue(func(b *Buffer) {
			b.PutVarInt(2)
			b.PutVarInt(RegistrySizeBlock)
		}, nil, false)},
		{"a negative inline block", adventureValue(func(b *Buffer) {
			b.PutVarInt(2)
			b.PutRaw([]byte{0xff, 0xff, 0xff, 0xff, 0x0f})
		}, nil, false)},
		{"a tag name no identifier parser reads", adventureValue(func(b *Buffer) {
			b.PutVarInt(0)
			b.PutString("Not An Identifier")
		}, nil, false)},
		{"an exact component matcher, whose shape is unread here", func() []byte {
			value := adventureValue(nil, nil, false)
			// The last two bytes are the two matcher counts; the first of
			// them claims one exact matcher.
			value[len(value)-2] = 1
			return value
		}()},
		{"a partial component matcher, whose shape is unread here", func() []byte {
			value := adventureValue(nil, nil, false)
			value[len(value)-1] = 1
			return value
		}()},
	} {
		if stack, ok := NewReader(untrustedSlotOf(1050, DataComponentCanPlaceOn, refuted.value)).ReadUntrustedSlot(); ok {
			t.Errorf("ReadUntrustedSlot read %s: the stack %+v", refuted.name, stack)
		}
	}
}
