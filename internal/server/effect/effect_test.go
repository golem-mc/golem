package effect

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestStrongerShorterHidesTheWeaker stacks a short strong effect over a long
// weak one: the strong one shows, the weak one takes over when it runs out
// with the time it had left (vanilla: MobEffectInstance.update).
func TestStrongerShorterHidesTheWeaker(t *testing.T) {
	var s Set
	if ch, ok := s.Add(New(hunger, 600, 0, false, true, true)); !ok || ch.Kind != Added {
		t.Fatalf("first add = %+v, %v; want added", ch, ok)
	}
	if ch, ok := s.Add(New(hunger, 100, 2, false, true, true)); !ok || ch.Kind != Updated || ch.Instance.Amplifier != 2 {
		t.Fatalf("stronger add = %+v, %v; want updated to amplifier 2", ch, ok)
	}
	// A longer weak one changes nothing shown: it is hidden behind.
	if _, ok := s.Add(New(hunger, 900, 1, false, true, true)); ok {
		t.Error("a longer weaker add changed what is shown")
	}
	var changes []Change
	for range 100 {
		changes = append(changes, s.Tick(0, func(*Instance) bool { return true })...)
	}
	in, _ := s.Get(hunger)
	if in.Amplifier != 1 || in.Duration != 800 {
		t.Errorf("after the strong one ran out: amplifier %d for %d, want 1 for 800", in.Amplifier, in.Duration)
	}
	if len(changes) != 1 || changes[0].Kind != Updated || changes[0].Instance.Amplifier != 1 {
		t.Errorf("changes = %+v, want the one downgrade", changes)
	}
}

// TestShouldApplyRhythms checks the effects' own intervals, halved per level.
func TestShouldApplyRhythms(t *testing.T) {
	for _, c := range []struct {
		name            string
		tick, amplifier int32
		want            bool
	}{
		{"minecraft:poison", 75, 0, true},
		{"minecraft:poison", 74, 0, false},
		{"minecraft:poison", 24, 1, true}, // every 12
		{"minecraft:poison", 7, 5, true},  // 25 >> 5 is nothing: every tick
		{"minecraft:regeneration", 50, 0, true},
		{"minecraft:regeneration", 25, 0, false},
		{"minecraft:wither", 40, 0, true},
		{"minecraft:hunger", 3, 0, true},
		{"minecraft:instant_health", 1, 0, true},
		{"minecraft:instant_health", 0, 0, false},
		{"minecraft:speed", 20, 0, false},
	} {
		if got := ShouldApply(registry.MustMobEffect(c.name), c.tick, c.amplifier); got != c.want {
			t.Errorf("%s at %d level %d = %v, want %v", c.name, c.tick, c.amplifier, got, c.want)
		}
	}
}

// TestEncodeIsVanillasCodec writes the shape the vanilla codec writes - the
// defaults left out, show_icon always - and reads it back.
func TestEncodeIsVanillasCodec(t *testing.T) {
	var s Set
	s.Add(New(hunger, 600, 0, false, true, true))
	s.Add(New(poison, 100, 1, true, false, false))
	s.Add(New(poison, 400, 0, true, false, false)) // hidden behind
	list := Encode(s.All())
	want := []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:hunger"), "duration": nbt.Int(600), "show_icon": nbt.Byte(1)},
		nbt.Compound{
			"id": nbt.String("minecraft:poison"), "amplifier": nbt.Byte(1), "duration": nbt.Int(100),
			"ambient": nbt.Byte(1), "show_particles": nbt.Byte(0), "show_icon": nbt.Byte(0),
			"hidden_effect": nbt.Compound{
				"duration": nbt.Int(400), "ambient": nbt.Byte(1), "show_particles": nbt.Byte(0), "show_icon": nbt.Byte(0),
			},
		},
	}
	if list == nil || !nbt.Equal(*list, nbt.List{Elem: nbt.TypeCompound, Items: want}) {
		t.Fatalf("encoded = %v, want %v", list, want)
	}
	back := Decode(*list)
	if !reflect.DeepEqual(back.All(), s.All()) {
		t.Errorf("read back %+v, want %+v", back.All(), s.All())
	}
}

// TestParticlesCarryTheEffectColour gives each visible effect a particle in
// its colour, ambient ones nearly transparent.
func TestParticlesCarryTheEffectColour(t *testing.T) {
	particles, ambient := Particles([]Instance{
		New(hunger, 600, 0, false, true, true),
		New(poison, 600, 0, true, true, true),
		New(registry.MustMobEffect("minecraft:speed"), 600, 0, false, false, false),
	})
	want := []Particle{
		{"minecraft:entity_effect", -0x1000000 | 0x587653},
		{"minecraft:entity_effect", 38<<24 | 0x87A363},
	}
	if !reflect.DeepEqual(particles, want) || ambient {
		t.Errorf("particles = %v, %v; want %v, not all ambient", particles, ambient, want)
	}
}
