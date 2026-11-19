package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestATurtleEggHatchChanceIsOneInTheWindowAndRareOutside: a die on a
// sand-backed egg always advances it inside the pre-dawn window and almost
// never outside it.
// vanilla: TurtleEggBlock.shouldUpdateHatchLevel
func TestATurtleEggHatchChanceIsOneInTheWindowAndRareOutside(t *testing.T) {
	w, p := newPhysicsWorld(t)
	setBlock(t, w, p, 0, GroundLevel-1, 0, registry.MustState("minecraft:sand", nil))
	egg := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "1", "hatch": "0"})
	setBlock(t, w, p, 0, GroundLevel, 0, egg)

	advanced := func(clock int64, tries int) int {
		w.SetClockTime(OverworldClock, clock)
		n := 0
		for range tries {
			if w.BlockAt(0, GroundLevel, 0) != egg {
				setBlock(t, w, p, 0, GroundLevel, 0, egg)
			}
			var changes []BlockChange
			p.turtleEggRandomTick(0, GroundLevel, 0, egg, &changes)
			if w.BlockAt(0, GroundLevel, 0) != egg {
				n++
			}
		}
		return n
	}
	for _, clock := range []int64{21062, 21500, 21904} {
		if n := advanced(clock, 50); n != 50 {
			t.Errorf("clock %d: %d of 50 dice advanced the egg, want all", clock, n)
		}
	}
	for _, clock := range []int64{21061, 21905, 6000} {
		if n := advanced(clock, 500); n > 20 {
			t.Errorf("clock %d: %d of 500 dice advanced the egg, want about 1", clock, n)
		}
	}
}

// TestATurtleEggHatchNeedsSand: the hatch stage waits for sand under the egg.
func TestATurtleEggHatchNeedsSand(t *testing.T) {
	w, p := newPhysicsWorld(t)
	w.SetClockTime(OverworldClock, 21500)
	egg := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "1", "hatch": "2"})
	setBlock(t, w, p, 0, GroundLevel, 0, egg)
	var changes []BlockChange
	p.turtleEggRandomTick(0, GroundLevel, 0, egg, &changes)
	if w.BlockAt(0, GroundLevel, 0) != egg || len(p.TakeTurtleHatches()) != 0 {
		t.Fatal("an egg on stone hatched")
	}
	setBlock(t, w, p, 0, GroundLevel-1, 0, registry.MustState("minecraft:red_sand", nil))
	p.turtleEggRandomTick(0, GroundLevel, 0, egg, &changes)
	if hatches := p.TakeTurtleHatches(); len(hatches) != 1 || hatches[0].Eggs != 1 {
		t.Fatalf("hatches on red sand = %v, want one", hatches)
	}
}

// TestATurtleEggPlaysTheTurtleSoundEvents: cracking and hatching use the
// entity.turtle sound events, with block_change or block_destroy.
// vanilla: SoundEvents.TURTLE_EGG_CRACK, TURTLE_EGG_HATCH
func TestATurtleEggPlaysTheTurtleSoundEvents(t *testing.T) {
	w, p := newPhysicsWorld(t)
	w.SetClockTime(OverworldClock, 21500)
	setBlock(t, w, p, 0, GroundLevel-1, 0, registry.MustState("minecraft:sand", nil))
	for _, tc := range []struct{ hatch, sound, event string }{
		{"0", "minecraft:entity.turtle.egg_crack", "minecraft:block_change"},
		{"2", "minecraft:entity.turtle.egg_hatch", "minecraft:block_destroy"},
	} {
		egg := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "1", "hatch": tc.hatch})
		setBlock(t, w, p, 0, GroundLevel, 0, egg)
		p.randomEffects = nil
		var changes []BlockChange
		p.turtleEggRandomTick(0, GroundLevel, 0, egg, &changes)
		found := false
		for _, e := range p.randomEffects {
			found = found || (e.SoundID.Name() == tc.sound && e.GameEvent.Name() == tc.event)
		}
		if !found {
			t.Errorf("hatch %s: effects %v lack sound %s with game event %s", tc.hatch, p.randomEffects, tc.sound, tc.event)
		}
	}
}

// TestTurtleEggsInFollowsTheSectionsEggs: the box test sees an egg set in
// its section, not one in a section beside the box, and forgets it once the
// egg is gone.
func TestTurtleEggsInFollowsTheSectionsEggs(t *testing.T) {
	w, p := newPhysicsWorld(t)
	y := int32(GroundLevel)
	box := func() bool { return w.TurtleEggsIn(-4, y-3, -4, 4, y+3, 4) }
	beside := func() bool { return w.TurtleEggsIn(16, y-3, 0, 31, y+3, 15) }
	if box() || beside() {
		t.Fatalf("flat world: TurtleEggsIn = %v, %v, want false", box(), beside())
	}
	setBlock(t, w, p, 2, y, 2, registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "1", "hatch": "0"}))
	if !box() {
		t.Fatalf("egg in the box: TurtleEggsIn = false, want true")
	}
	if beside() {
		t.Fatalf("egg beside the box: TurtleEggsIn = true, want false")
	}
	setBlock(t, w, p, 2, y, 2, registry.MustState("minecraft:air", nil))
	if box() {
		t.Fatalf("egg replaced by air: TurtleEggsIn = true, want false")
	}
}
