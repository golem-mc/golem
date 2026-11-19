package registry

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// rocketWith is a rocket of the given flight and explosions.
func rocketWith(flight int32, explosions ...protocol.FireworkExplosion) protocol.ItemStack {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:firework_rocket"), Count: 1}
	stack.SetRaw(protocol.FireworksRawComponent(flight, explosions))
	return stack
}

// fireworksEntry is a stored rocket whose fireworks component is value.
func fireworksEntry(value nbt.Tag) nbt.Compound {
	return nbt.Compound{
		"id":         nbt.String("minecraft:firework_rocket"),
		"count":      nbt.Int(1),
		"components": nbt.Compound{"minecraft:fireworks": value},
	}
}

// TestFireworksSurviveTheStackCodec writes a rocket and a star and reads
// them back whole.
// vanilla: Fireworks.CODEC, FireworkExplosion.CODEC
func TestFireworksSurviveTheStackCodec(t *testing.T) {
	burst := protocol.FireworkExplosion{
		Shape: 4, Colors: []int32{1, 2}, FadeColors: []int32{3}, Twinkle: true,
	}
	rocket := rocketWith(3, burst, protocol.FireworkExplosion{Shape: 3})
	star := protocol.ItemStack{ItemID: MustItem("minecraft:firework_star"), Count: 1}
	star.SetRaw(burst.RawComponent())
	for _, stack := range []protocol.ItemStack{rocket, star} {
		got, ok := StoredStack(StackTag(stack))
		if !ok {
			t.Fatalf("the stored reading refused %v", StackTag(stack))
		}
		if !got.Equal(stack) {
			t.Errorf("came back as %+v, want %+v", got, stack)
		}
	}
}

// TestFireworksWriteOnlyWhatDiffersFromTheDefaults leaves out the plain
// rocket's own component, and inside a component every field at its
// default.
// vanilla: PatchedDataComponentMap.set, Items.FIREWORK_ROCKET
func TestFireworksWriteOnlyWhatDiffersFromTheDefaults(t *testing.T) {
	if components := StackComponents(rocketWith(1)); components != nil {
		t.Errorf("the plain rocket's components = %v, want none", components)
	}
	components := StackComponents(rocketWith(0,
		protocol.FireworkExplosion{Shape: 9}, protocol.FireworkExplosion{Shape: -1}))
	plain := nbt.Compound{"shape": nbt.String("small_ball")}
	want := nbt.Compound{"explosions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{plain, plain}}}
	if got := components["minecraft:fireworks"]; !nbt.Equal(got, want) {
		t.Errorf("fireworks = %v, want %v", got, want)
	}
	if components := StackComponents(rocketWith(2)); !nbt.Equal(
		components["minecraft:fireworks"], nbt.Compound{"flight_duration": nbt.Byte(2)}) {
		t.Errorf("the flight-2 rocket's components = %v, want only its duration", components)
	}
}

// TestFireworksLeaveOutADurationTheFileCannotHold drops a component whose
// wire duration is past the file's unsigned byte; a negative one is saved
// as its low byte.
// vanilla: ExtraCodecs.UNSIGNED_BYTE
func TestFireworksLeaveOutADurationTheFileCannotHold(t *testing.T) {
	if components := StackComponents(rocketWith(256)); components != nil {
		t.Errorf("flight 256: components = %v, want none", components)
	}
	boom := protocol.FireworkExplosion{Colors: []int32{0xff0000}}
	got := StackComponents(rocketWith(-1, boom))
	tag, ok := nbt.CompoundView(got["minecraft:fireworks"])
	if !ok {
		t.Fatalf("flight -1: fireworks = %v, want a compound", got["minecraft:fireworks"])
	}
	if tag["flight_duration"] != nbt.Byte(-1) {
		t.Errorf("flight -1: flight_duration = %v, want 255b", tag["flight_duration"])
	}
	if list, _ := tag["explosions"].(nbt.List); len(list.Items) != 1 {
		t.Errorf("flight -1: explosions = %v, want the one explosion kept", tag["explosions"])
	}
	if components := StackComponents(rocketWith(255)); components == nil {
		t.Error("flight 255: the component was left out, want it written")
	}
}

// TestFireworksAreReadFromVanillaNBT reads the shapes the codecs take: the
// colours as an int array too, a flag as any nonzero number, and a
// duration byte read unsigned.
// vanilla: Fireworks.CODEC, NbtOps.getBooleanValue, ExtraCodecs.UNSIGNED_BYTE
func TestFireworksAreReadFromVanillaNBT(t *testing.T) {
	entry := fireworksEntry(nbt.Compound{
		"flight_duration": nbt.Byte(-56),
		"explosions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"shape":       nbt.String("creeper"),
			"colors":      nbt.IntArray{7},
			"has_trail":   nbt.Double(0.5),
			"has_twinkle": nbt.Short(-1),
		}}},
	})
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a vanilla-written rocket")
	}
	flight, explosions, ok := stack.FireworksComponentOf()
	want := []protocol.FireworkExplosion{{Shape: 3, Colors: []int32{7}, Trail: true, Twinkle: true}}
	if !ok || flight != 200 || !reflect.DeepEqual(explosions, want) {
		t.Errorf("fireworks = %d %+v %v, want 200 %+v", flight, explosions, ok, want)
	}
}

// TestStoredFireworksRefuseWhatTheCodecDoesNot refuses a shape it does not
// name, an explosion without one, an unknown field and a list past 256.
// vanilla: Fireworks.MAX_EXPLOSIONS, FireworkExplosion.CODEC
func TestStoredFireworksRefuseWhatTheCodecDoesNot(t *testing.T) {
	explosions := func(items ...nbt.Tag) nbt.Compound {
		return nbt.Compound{"explosions": nbt.List{Elem: nbt.TypeCompound, Items: items}}
	}
	many := make([]nbt.Tag, 257)
	for i := range many {
		many[i] = nbt.Compound{"shape": nbt.String("star")}
	}
	cases := map[string]nbt.Tag{
		"unknown shape":  explosions(nbt.Compound{"shape": nbt.String("square")}),
		"no shape":       explosions(nbt.Compound{"has_trail": nbt.Byte(1)}),
		"257 explosions": explosions(many...),
		"duration 256":   nbt.Compound{"flight_duration": nbt.Int(256)},
		"string colours": explosions(nbt.Compound{"shape": nbt.String("star"), "colors": nbt.String("red")}),
		"not a compound": nbt.Int(1),
		"string flag":    explosions(nbt.Compound{"shape": nbt.String("star"), "has_trail": nbt.String("1")}),
	}
	for name, value := range cases {
		if _, ok := StoredStack(fireworksEntry(value)); ok {
			t.Errorf("%s: the stored reading took it", name)
		}
	}
	if _, ok := StoredStack(fireworksEntry(explosions(many[:256]...))); !ok {
		t.Error("256 explosions: the stored reading refused them")
	}
}

// TestStoredFireworksSkipAnUnknownField checks that a key the codec does not
// know is skipped, in the fireworks and in an explosion.
// vanilla: Fireworks.CODEC, FireworkExplosion.CODEC
func TestStoredFireworksSkipAnUnknownField(t *testing.T) {
	explosion := nbt.Compound{"shape": nbt.String("star"), "bogus": nbt.Byte(1)}
	value := nbt.Compound{
		"flight":     nbt.Byte(1),
		"explosions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{explosion}},
	}
	if _, ok := StoredStack(fireworksEntry(value)); !ok {
		t.Error("the stored reading refused an unknown key")
	}
}
