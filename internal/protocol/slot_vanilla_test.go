package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestSlotMatchesVanilla encodes the stacks a vanilla server sent and
// compares them, byte for byte, with the slot bytes the probe captured (the
// Set Container Slot of a /give, HEXDUMP=0x14). The bytes pin the whole
// shape at once: the component type IDs of this version, the enchantment
// registry's numbering, the count-and-pairs enchantment encoding, the bare
// string a plain custom name travels as, the byte of a glint override and
// the nothing an unbreakable flag carries.
func TestSlotMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		name string
		slot func(b *Buffer)
		want string
	}{
		{
			// /give @s diamond_sword[enchantments={sharpness:5}]: item 1050,
			// the enchantments (ID 13) carrying sharpness (ID 33) at 5.
			name: "enchanted sword",
			slot: func(b *Buffer) {
				b.PutSlot(ItemStack{ItemID: 1050, Count: 1, Enchantments: map[int32]int32{33: 5}})
			},
			want: "019a0801000d012105",
		},
		{
			// /give @s diamond[custom_name="Bob"]: the bare string tag a
			// plain component travels as.
			name: "renamed diamond",
			slot: func(b *Buffer) {
				name := Text{Text: "Bob"}
				b.PutSlot(ItemStack{ItemID: 1012, Count: 1, CustomName: &name})
			},
			want: "01f407010006080003426f62",
		},
		{
			// /give @s enchanted_book[stored_enchantments={sharpness:3}]:
			// item 1395, stored_enchantments ID 45.
			name: "enchanted book",
			slot: func(b *Buffer) {
				b.PutSlot(ItemStack{ItemID: 1395, Count: 1, StoredEnchantments: map[int32]int32{33: 3}})
			},
			want: "01f30a01002d012103",
		},
		{
			// /give @s diamond[repair_cost=7,enchantment_glint_override=true]:
			// the two components vanilla happens to name in the order this
			// server writes them in.
			name: "repaired, glinting diamond",
			slot: func(b *Buffer) {
				b.PutSlot(ItemStack{ItemID: 1012, Count: 1, RepairCost: 7, GlintOverride: GlintShown})
			},
			want: "01f407020013071501",
		},
		{
			// /give @s diamond_sword[damage=10,enchantments={sharpness:5}],
			// again in the order this server writes its components in.
			name: "worn, enchanted sword",
			slot: func(b *Buffer) {
				b.PutSlot(ItemStack{ItemID: 1050, Count: 1, Damage: 10, Enchantments: map[int32]int32{33: 5}})
			},
			want: "019a080200030a0d012105",
		},
		{
			// /give @s diamond_pickaxe[unbreakable={}]: ID 4, carrying
			// nothing at all.
			name: "unbreakable pickaxe",
			slot: func(b *Buffer) {
				b.PutSlot(ItemStack{ItemID: 1052, Count: 1, Unbreakable: true})
			},
			want: "019c08010004",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Buffer
			tc.slot(&b)
			want, err := hex.DecodeString(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b.Bytes(), want) {
				t.Errorf("PutSlot = % X, want the vanilla server's % X", b.Bytes(), want)
			}
			// And what vanilla sent reads back as the stack it was sent for.
			got, ok := NewReader(want).ReadSlot()
			if !ok {
				t.Fatal("ReadSlot refused the vanilla bytes")
			}
			var roundTrip Buffer
			roundTrip.PutSlot(got)
			if !bytes.Equal(roundTrip.Bytes(), want) {
				t.Errorf("round trip = % X, want the same % X", roundTrip.Bytes(), want)
			}
		})
	}
}

// TestContainerComponentMatchesVanilla encodes the shulker boxes a vanilla
// server sent for two /give commands. The container, ID 77, travels as
// a positional list running to the last filled slot; each present entry is
// the item, the count, and a component patch of the same shape the stack it
// is inside carries.
//
// The one-component box is pinned byte for byte. The two-component one is
// not: the patch of the diamond inside names its components in the order
// vanilla happened to set them in, and this server writes its own canonical
// order — so the vanilla bytes are read and compared as the stack they mean.
func TestContainerComponentMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stack      ItemStack
		want       string
		bytesExact bool
	}{
		{
			name: "gapped shulker box",
			stack: ItemStack{
				ItemID: 656, Count: 1,
				Container: []ItemStack{{ItemID: 1012, Count: 3}, {}, {}, {}, {}, {ItemID: 1022, Count: 2}},
			},
			want:       "01900501004d0601f4070300000000000001fe07020000",
			bytesExact: true,
		},
		{
			name: "enchanted, renamed diamond inside",
			stack: ItemStack{
				ItemID: 656, Count: 1,
				Container: []ItemStack{{ItemID: 1012, Count: 3, Enchantments: map[int32]int32{33: 5},
					CustomName: &Text{Text: "Bob"}}},
			},
			want: "01900501004d0101f4070302000d01210506080003426f62",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Buffer
			b.PutSlot(tc.stack)
			want, err := hex.DecodeString(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if tc.bytesExact && !bytes.Equal(b.Bytes(), want) {
				t.Errorf("PutSlot = % X, want the vanilla server's % X", b.Bytes(), want)
			}
			got, ok := NewReader(want).ReadSlot()
			if !ok {
				t.Fatal("ReadSlot refused the vanilla bytes")
			}
			if !got.Equal(tc.stack) {
				t.Errorf("ReadSlot = %v, want %v", got, tc.stack)
			}
			// And the server's own bytes read back as the stack they were
			// written for.
			roundTrip, ok := NewReader(b.Bytes()).ReadSlot()
			if !ok || !roundTrip.Equal(tc.stack) {
				t.Errorf("the server's bytes read back as %v (read %v)", roundTrip, ok)
			}
		})
	}
}

// TestContainerComponentSemantics pins what the container component makes a
// stack mean: same kind and contents is the same stack whatever the counts,
// an entry's count is part of what the component says, and a gap is not an
// empty slot nor an emptied slot an absent one.
func TestContainerComponentSemantics(t *testing.T) {
	// The names the components hash under come through the hook the
	// registry installs; the test answers for the two items it pins.
	ItemName = func(id int32) (string, bool) {
		switch id {
		case 656:
			return "minecraft:shulker_box", true
		case 1012:
			return "minecraft:diamond", true
		}
		return "", false
	}
	t.Cleanup(func() { ItemName = nil })

	box := func(contents ...ItemStack) ItemStack {
		return ItemStack{ItemID: 656, Count: 1, Container: contents}
	}
	diamonds := ItemStack{ItemID: 1012, Count: 3}
	gold := ItemStack{ItemID: 1022, Count: 2}
	if !box(diamonds, gold).Same(box(diamonds, gold)) {
		t.Error("two boxes of the same contents were not the same stack")
	}
	if box(diamonds, gold).Same(box(diamonds)) {
		t.Error("a box with a gold ingot was the same as one without")
	}
	if box(diamonds, gold).Same(box(diamonds, ItemStack{ItemID: 1022, Count: 1})) {
		t.Error("an entry's count was left out of what the component says")
	}
	if box(diamonds).Same(ItemStack{ItemID: 656, Count: 1}) {
		t.Error("a filled box was the same as a plain one")
	}
	if !box().Same(ItemStack{ItemID: 656, Count: 1}) {
		t.Error("an emptied box was not the same as a plain one")
	}
	if !box(diamonds).Equal(box(diamonds)) {
		t.Error("two filled boxes of the same count were not equal")
	}
	if box(diamonds).Equal(box(diamonds, gold)) {
		t.Error("a filled box was equal to a fuller one")
	}

	// The hashed form carries the container's checksum like any other
	// component's.
	claims := map[int32]int32{}
	if checksum, ok := ComponentChecksumOf(box(diamonds), DataComponentContainer); ok {
		claims[DataComponentContainer] = checksum
	} else {
		t.Fatal("a filled box's checksum was not computed")
	}
	claim := HashedSlot{Stack: ItemStack{ItemID: 656, Count: 1}, hashes: claims}
	if !claim.Matches(box(diamonds)) {
		t.Error("the hashed claim did not match the box it was taken from")
	}
	if claim.Matches(box(diamonds, gold)) {
		t.Error("the hashed claim matched a fuller box")
	}

	// Without the hook the checksum is one this server cannot compute, and
	// the slot is corrected rather than confirmed.
	ItemName = nil
	if _, ok := componentChecksumOf(box(diamonds), DataComponentContainer); ok {
		t.Error("a checksum was computed without a name to hash")
	}
}

// TestContainerComponentRefuses pins the containers ReadSlot turns away: a
// list past the codec's bound, an entry without anything in it, and an entry
// carrying an unmodelled component, whose unreadable tail the whole packet
// has to be dropped for.
func TestContainerComponentRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{
			// The codec binds the list to 256 slots.
			name: "list past the codec's bound",
			data: "01900501004d8102",
		},
		{
			// One present entry whose count is zero: a template has to
			// hold something.
			name: "an entry without anything in it",
			data: "01900501004d0101f407000000",
		},
		{
			// Potion_contents, component 53, rides in the entry's patch:
			// two bytes this server cannot find the end of.
			name: "an entry with an unmodelled component",
			data: "01900501004d0101f4070301003502aabb",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if stack, ok := NewReader(data).ReadSlot(); ok {
				t.Errorf("ReadSlot = %+v, want the stack refused", stack)
			}
		})
	}
}

// TestClaimChecksumsMatchVanilla pins the checksums a hashed claim carries
// for each component, the values the vanilla server confirmed by
// accepting them on a click and refusing one checksum away from each. The
// hashing is the game's own: every value through its codec under CRC32C.
func TestClaimChecksumsMatchVanilla(t *testing.T) {
	// The enchantment names come through the hook the registry installs;
	// the test answers with the one enchantment it pins.
	EnchantmentName = func(id int32) (string, bool) {
		switch id {
		case 33:
			return "minecraft:sharpness", true
		case 'E':
			return "minecraft:efficiency", true
		case 'U':
			return "minecraft:unbreaking", true
		}
		return "", false
	}
	ItemName = func(id int32) (string, bool) {
		if id == 1012 {
			return "minecraft:diamond", true
		}
		return "", false
	}
	t.Cleanup(func() { EnchantmentName, ItemName = nil, nil })

	for _, test := range []struct {
		name      string
		stack     ItemStack
		component int32
		want      int32
	}{
		{"damage ten", ItemStack{ItemID: 1050, Damage: 10}, DataComponentDamage, -0x36C9C23D},
		{"repair cost seven", ItemStack{ItemID: 1050, RepairCost: 7}, DataComponentRepairCost, -0x66EA3A92},
		{"glint shown", ItemStack{ItemID: 1050, GlintOverride: GlintShown}, DataComponentEnchantmentGlintOverride, -0x3CC9313E},
		{"glint hidden", ItemStack{ItemID: 1050, GlintOverride: GlintHidden}, DataComponentEnchantmentGlintOverride, 0x315D4DC1},
		{"unbreakable", ItemStack{ItemID: 1050, Unbreakable: true}, DataComponentUnbreakable, -0x3A8B4B38},
		// Map entries are ordered by unsigned key digest, then value digest.
		{"efficiency and unbreaking three", ItemStack{ItemID: 1050, Enchantments: map[int32]int32{'E': 3, 'U': 3}},
			DataComponentEnchantments, -0x56C7D2E6},
		{"red custom name Bob", ItemStack{ItemID: 1050, CustomName: &Text{Text: "Bob", Color: "red"}},
			DataComponentCustomName, 0x01E386C8},
		{"container of a diamond", ItemStack{ItemID: 1050, Container: []ItemStack{{ItemID: 1012, Count: 1}}},
			DataComponentContainer, -0x36B5D714},
	} {
		got, ok := componentChecksumOf(test.stack, test.component)
		if !ok || int32Le(got) != test.want {
			t.Errorf("%s = %08X (computable %v), want the vanilla %08X", test.name,
				uint32(int32Le(got)), ok, uint32(test.want))
		}
	}

	named := ItemStack{ItemID: 1050, CustomName: &Text{Text: "Bob"}}
	got, ok := componentChecksumOf(named, DataComponentCustomName)
	if !ok || int32Le(got) != int32(0xE970654C-0x100000000) {
		t.Errorf("custom name Bob = %08X (computable %v), want the vanilla E970654C",
			uint32(int32Le(got)), ok)
	}

	enchanted := ItemStack{ItemID: 1050, Enchantments: map[int32]int32{33: 5}}
	got, ok = componentChecksumOf(enchanted, DataComponentEnchantments)
	if !ok || int32Le(got) != 0x66A963EF {
		t.Errorf("sharpness five = %08X (computable %v), want the vanilla 66A963EF",
			uint32(int32Le(got)), ok)
	}

	// An enchantment whose name cannot be resolved is a checksum this
	// server cannot compute.
	strangers := ItemStack{ItemID: 1050, Enchantments: map[int32]int32{99: 1}}
	if _, ok := componentChecksumOf(strangers, DataComponentEnchantments); ok {
		t.Error("an unknown enchantment's checksum was computed")
	}
}
