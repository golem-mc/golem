package protocol

import "testing"

// TestPotionContentsOfReadsTheOptionals decodes a potion_contents value as a
// drink reads it, past the optionals the client draws: the potion, a custom
// colour, one effect carrying a hidden twin and a custom name all come back,
// every effect field as it was written.
func TestPotionContentsOfReadsTheOptionals(t *testing.T) {
	var b Buffer
	b.PutBool(true)
	b.PutVarInt(19) // turtle_master
	b.PutBool(true)
	b.PutI32(-0x010203) // the custom colour, which a drink skips
	b.PutVarInt(1)
	b.PutVarInt(5)   // the effect
	b.PutVarInt(2)   // amplifier
	b.PutVarInt(100) // duration
	b.PutBool(true)  // ambient
	b.PutBool(false) // show particles
	b.PutBool(true)  // show icon
	b.PutBool(true)  // a hidden twin follows, Details without an effect of its own
	b.PutVarInt(0)   // amplifier
	b.PutVarInt(50)  // duration
	b.PutBool(false) // ambient
	b.PutBool(false) // show particles
	b.PutBool(false) // show icon
	b.PutBool(false) // the hidden twin has none
	b.PutBool(true)  // a custom name
	b.PutString("Brew")

	stack := ItemStack{ItemID: 1271, Count: 1, Raw: []RawComponent{
		{Type: DataComponentPotionContents, Data: b.Bytes()},
	}}
	contents, ok := stack.PotionContentsOf()
	if !ok {
		t.Fatal("PotionContentsOf refused the value its codec reads")
	}
	if contents.Potion != 19 {
		t.Errorf("potion = %d, want 19", contents.Potion)
	}
	if contents.CustomColor == nil || *contents.CustomColor != -0x010203 {
		t.Errorf("custom colour = %v, want %#x", contents.CustomColor, -0x010203)
	}
	if contents.CustomName == nil || *contents.CustomName != "Brew" {
		t.Errorf("custom name = %v, want Brew", contents.CustomName)
	}
	if len(contents.CustomEffects) != 1 {
		t.Fatalf("custom effects = %+v, want one", contents.CustomEffects)
	}
	got := contents.CustomEffects[0]
	if got.ID != 5 || got.Amplifier != 2 || got.Duration != 100 || !got.Ambient || got.ShowParticles || !got.ShowIcon {
		t.Errorf("effect = %+v, want id 5, amplifier 2, 100 ticks, ambient, no particles, an icon", got)
	}
	if got.Hidden == nil || got.Hidden.ID != 5 || got.Hidden.Duration != 50 || got.Hidden.Ambient {
		t.Errorf("the hidden twin = %+v, want id 5 for 50 ticks, not ambient", got.Hidden)
	}
}

// TestSetPotionRoundTrips lays a potion on a stack — the shape a brew's
// result carries — and reads it back: what the writer put on the stack is
// what its own codec reads, and it replaces any potion contents the stack
// had.
func TestSetPotionRoundTrips(t *testing.T) {
	stack := ItemStack{ItemID: 1271, Count: 1}
	stack.SetPotion(3) // awkward
	contents, ok := stack.PotionContentsOf()
	if !ok || contents.Potion != 3 || len(contents.CustomEffects) != 0 {
		t.Fatalf("SetPotion read back as %+v (ok %v), want the awkward potion and nothing else",
			contents, ok)
	}

	// A second SetPotion replaces the first rather than riding beside it:
	// the component is one value, whatever landed it there.
	stack.SetPotion(19) // turtle_master
	if contents, _ := stack.PotionContentsOf(); contents.Potion != 19 {
		t.Errorf("a second SetPotion read back as %d, want it to replace the first", contents.Potion)
	}
}

// TestPotionContentsWriteDropsHiddenEffects writes an effect with a hidden
// twin and reads it back without one.
func TestPotionContentsWriteDropsHiddenEffects(t *testing.T) {
	stack := ItemStack{ItemID: 1271, Count: 1}
	stack.SetPotionContents(PotionContents{
		Potion: NoPotion,
		CustomEffects: []PotionEffectSpec{
			{ID: 5, Duration: 100, ShowParticles: true, ShowIcon: true,
				Hidden: &PotionEffectSpec{ID: 5, Duration: 50}},
		},
	})
	contents, ok := stack.PotionContentsOf()
	if !ok || len(contents.CustomEffects) != 1 || contents.CustomEffects[0].Duration != 100 {
		t.Fatalf("contents = %+v, %v; want the effect back", contents, ok)
	}
	if contents.CustomEffects[0].Hidden != nil {
		t.Errorf("hidden = %+v, want none", contents.CustomEffects[0].Hidden)
	}
}
