package protocol

import "testing"

func TestEquippableOfReadsWireSlotIDs(t *testing.T) {
	for _, tc := range []struct {
		id   int32
		name string
	}{{0, "mainhand"}, {1, "feet"}, {3, "chest"}, {4, "head"}, {5, "offhand"}, {6, "body"}, {7, "saddle"}} {
		var b Buffer
		b.PutVarInt(tc.id)
		b.PutVarInt(1) // equip sound: registry ID 0
		b.PutBool(false)
		b.PutBool(false)
		b.PutBool(false)
		for range 5 {
			b.PutBool(true)
		}
		b.PutVarInt(1)
		var s ItemStack
		s.SetRaw(RawComponent{Type: DataComponentEquippable, Data: b.Bytes()})
		if got, ok := s.EquippableOf(); !ok || got.Slot != tc.name {
			t.Errorf("slot id %d read as %q, %v; want %q", tc.id, got.Slot, ok, tc.name)
		}
	}
	var bad Buffer
	bad.PutVarInt(8)
	var s ItemStack
	s.SetRaw(RawComponent{Type: DataComponentEquippable, Data: bad.Bytes()})
	if _, ok := s.EquippableOf(); ok {
		t.Error("slot id 8 read as a slot")
	}
}

func equippableBytes(slot, sound int32, trailing bool) []byte {
	var b Buffer
	b.PutVarInt(slot)
	b.PutVarInt(sound)
	b.PutBool(false)
	b.PutBool(false)
	b.PutBool(false)
	for range 5 {
		b.PutBool(true)
	}
	b.PutVarInt(1)
	if trailing {
		b.PutBool(true)
	}
	return b.Bytes()
}

func TestEquippableOfRefusesMalformedValues(t *testing.T) {
	for name, data := range map[string][]byte{
		"valid":            equippableBytes(3, 1, false),
		"unknown sound id": equippableBytes(3, int32(RegistrySizeSoundEvent)+2, false),
		"trailing bytes":   equippableBytes(3, 1, true),
		"truncated":        equippableBytes(3, 1, false)[:4],
		"empty":            nil,
	} {
		var s ItemStack
		s.SetRaw(RawComponent{Type: DataComponentEquippable, Data: data})
		_, ok := s.EquippableOf()
		if ok != (name == "valid") {
			t.Errorf("%s: accepted = %v", name, ok)
		}
	}
}
