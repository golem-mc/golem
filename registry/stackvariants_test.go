package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestVariantComponentsSurviveASave round-trips each trim, holder, enum and
// potion_duration_scale component through StackTag and StoredStack, checks
// the NBT shape written, and that an inline holder or unknown name refuses.
// vanilla: ArmorTrim.CODEC, InstrumentComponent.CODEC, PaintingVariant.CODEC,
// Axolotl.Variant.CODEC, Salmon.Variant.CODEC, TropicalFish.Pattern.CODEC,
// DyeColor.CODEC, Rabbit.Variant.CODEC, ExtraCodecs.NON_NEGATIVE_FLOAT
func TestVariantComponentsSurviveASave(t *testing.T) {
	stone, _ := ItemID("minecraft:stone")
	cases := []struct {
		name string
		set  func(*protocol.ItemStack)
		want nbt.Tag
	}{
		{"minecraft:trim", func(s *protocol.ItemStack) {
			s.SetTrim(MustID("minecraft:trim_material", "minecraft:gold"), MustID("minecraft:trim_pattern", "minecraft:coast"))
		}, nbt.Compound{"material": nbt.String("minecraft:gold"), "pattern": nbt.String("minecraft:coast")}},
		{"minecraft:instrument", func(s *protocol.ItemStack) {
			s.SetInstrument(MustID("minecraft:instrument", "minecraft:dream_goat_horn"))
		}, nbt.String("minecraft:dream_goat_horn")},
		{"minecraft:painting/variant", func(s *protocol.ItemStack) {
			s.SetPaintingVariant(MustID("minecraft:painting_variant", "minecraft:kebab"))
		}, nbt.String("minecraft:kebab")},
		{"minecraft:axolotl/variant", func(s *protocol.ItemStack) { s.SetAxolotlVariant(4) }, nbt.String("blue")},
		{"minecraft:salmon/size", func(s *protocol.ItemStack) { s.SetSalmonSize(2) }, nbt.String("large")},
		{"minecraft:tropical_fish/pattern", func(s *protocol.ItemStack) {
			s.SetEnum(protocol.DataComponentTropicalFishPattern, 1|4<<8)
		}, nbt.String("betty")},
		{"minecraft:tropical_fish/base_color", func(s *protocol.ItemStack) {
			s.SetEnum(protocol.DataComponentTropicalFishBaseColor, 5)
		}, nbt.String("lime")},
		{"minecraft:tropical_fish/pattern_color", func(s *protocol.ItemStack) {
			s.SetEnum(protocol.DataComponentTropicalFishPatternColor, 11)
		}, nbt.String("blue")},
		{"minecraft:rabbit/variant", func(s *protocol.ItemStack) {
			s.SetEnum(protocol.DataComponentRabbitVariant, 99)
		}, nbt.String("evil")},
		{"minecraft:potion_duration_scale", func(s *protocol.ItemStack) { s.SetPotionDurationScale(0.5) }, nbt.Float(0.5)},
	}
	for _, tc := range cases {
		stack := protocol.ItemStack{ItemID: stone, Count: 1}
		tc.set(&stack)
		tag := StackTag(stack).Entries()
		components, _ := nbt.CompoundView(tag["components"])
		if got := components[tc.name]; !nbt.Equal(got, tc.want) {
			t.Errorf("%s saved as %v, want %v", tc.name, got, tc.want)
		}
		got, ok := StoredStack(tag)
		if !ok || !got.Equal(stack) {
			t.Errorf("%s read back %+v (%v), want %+v", tc.name, got, ok, stack)
		}
	}

	// An id the stream codec maps onto another constant is saved as that one.
	clamped := protocol.ItemStack{ItemID: stone, Count: 1}
	clamped.SetSalmonSize(7)
	if got := StackComponents(clamped)["minecraft:salmon/size"]; got != nbt.String("large") {
		t.Errorf("salmon/size 7 saved as %v, want large", got)
	}

	inline := protocol.ItemStack{ItemID: stone, Count: 1}
	inline.SetRaw(protocol.RawComponent{Type: protocol.DataComponentInstrument, Data: []byte{0}})
	if components := StackComponents(inline); components["minecraft:instrument"] != nil {
		t.Errorf("an inline instrument was saved as %v", components["minecraft:instrument"])
	}

	for _, refused := range []struct {
		name  string
		value nbt.Tag
	}{
		{"minecraft:instrument", nbt.Compound{"sound_event": nbt.String("minecraft:item.goat_horn.sound.0")}},
		{"minecraft:trim", nbt.Compound{"material": nbt.String("minecraft:gold")}},
		{"minecraft:painting/variant", nbt.String("minecraft:nope")},
		{"minecraft:axolotl/variant", nbt.String("Blue")},
		{"minecraft:salmon/size", nbt.Int(2)},
		{"minecraft:potion_duration_scale", nbt.Float(-1)},
	} {
		if !ComponentRefused(refused.name, refused.value) {
			t.Errorf("%s = %v was taken, want refused", refused.name, refused.value)
		}
	}
	// The registry codecs default a bare name's namespace.
	var bare protocol.ItemStack
	if !ReadComponent(&bare, "minecraft:instrument", nbt.String("sing_goat_horn")) {
		t.Error("instrument sing_goat_horn was refused")
	}
}

// TestMobVariantComponentsRoundTrip round-trips each of the 23 mob variant
// components through SNBT read, NBT write and wire bytes, with one valid and
// one refused value.
// vanilla: DataComponents.WOLF_VARIANT, Fox.Variant.CODEC, DyeColor.CODEC
func TestMobVariantComponentsRoundTrip(t *testing.T) {
	stone, _ := ItemID("minecraft:stone")
	cases := []struct {
		name, valid, refused string
		wire                 int32
	}{
		{"minecraft:fox/variant", "snow", "blue", 1},
		{"minecraft:parrot/variant", "gray", "red", 4},
		{"minecraft:mooshroom/variant", "brown", "blue", 1},
		{"minecraft:horse/variant", "dark_brown", "red", 6},
		{"minecraft:llama/variant", "gray", "red", 3},
		{"minecraft:wolf/collar", "red", "Red", 14},
		{"minecraft:cat/collar", "red", "Red", 14},
		{"minecraft:sheep/color", "red", "Red", 14},
		{"minecraft:shulker/color", "red", "Red", 14},
		{"minecraft:cushion/color", "red", "Red", 14},
		{"minecraft:villager/variant", "minecraft:snow", "minecraft:nope", 4},
		{"minecraft:wolf/variant", "minecraft:black", "minecraft:nope", -1},
		{"minecraft:wolf/sound_variant", "minecraft:big", "minecraft:nope", -1},
		{"minecraft:pig/variant", "minecraft:cold", "minecraft:nope", -1},
		{"minecraft:pig/sound_variant", "minecraft:mini", "minecraft:nope", -1},
		{"minecraft:cow/variant", "minecraft:cold", "minecraft:nope", -1},
		{"minecraft:cow/sound_variant", "minecraft:moody", "minecraft:nope", -1},
		{"minecraft:chicken/variant", "minecraft:cold", "minecraft:nope", -1},
		{"minecraft:chicken/sound_variant", "minecraft:picky", "minecraft:nope", -1},
		{"minecraft:zombie_nautilus/variant", "minecraft:warm", "minecraft:nope", -1},
		{"minecraft:frog/variant", "minecraft:cold", "minecraft:nope", -1},
		{"minecraft:cat/variant", "minecraft:black", "minecraft:nope", -1},
		{"minecraft:cat/sound_variant", "minecraft:classic", "minecraft:nope", -1},
	}
	if len(cases) != 23 {
		t.Fatalf("%d cases, want 23", len(cases))
	}
	for _, tc := range cases {
		typ, ok := ItemComponentIDs[tc.name]
		if !ok {
			t.Fatalf("%s has no component ID", tc.name)
		}
		var stack protocol.ItemStack
		if !ReadComponent(&stack, tc.name, nbt.String(tc.valid)) {
			t.Errorf("%s = %q was refused", tc.name, tc.valid)
			continue
		}
		id, ok := stack.EnumOf(typ)
		if !ok || (tc.wire >= 0 && id != tc.wire) {
			t.Errorf("%s wire id = %d (%v), want %d", tc.name, id, ok, tc.wire)
		}
		stack.ItemID, stack.Count = stone, 1
		components, _ := nbt.CompoundView(StackTag(stack).Entries()["components"])
		if got := components[tc.name]; !nbt.Equal(got, nbt.String(tc.valid)) {
			t.Errorf("%s saved as %v, want %q", tc.name, got, tc.valid)
		}
		var buf protocol.Buffer
		buf.PutVarInt(id)
		if wire := (protocol.RawComponent{Type: typ, Data: buf.Bytes()}); !protocol.RawComponentHeld(wire.Type) {
			t.Errorf("%s is not held as a raw component", tc.name)
		}
		if !ComponentRefused(tc.name, nbt.String(tc.refused)) {
			t.Errorf("%s = %q was taken, want refused", tc.name, tc.refused)
		}
	}
}
