package rules

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The golem patterns as ordinary unit tests, on the pumpkin, snow and iron
// arrangements a testWorld holds. The black-box tests in
// internal/server/server_golems_test.go remain the proof of the build itself — the
// clear, the mob, the packets around these decisions; these are the checks
// of the shapes and the precedence the decisions run on.

// TestASnowColumnBuildsASnowGolem checks the ordinary column: two snow
// blocks under the head, the golem standing in the lower one, the clear
// walking the head out of the world first.
func TestASnowColumnBuildsASnowGolem(t *testing.T) {
	w := testWorld{}
	snow := defaultState(t, "minecraft:snow_block")
	pumpkin := carvedPumpkin(t)
	w.set(2, 64, 3, snow)
	w.set(2, 65, 3, snow)
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of two snow blocks and a head")
	}
	if spawn.Kind != SnowGolem {
		t.Errorf("the column builds %s, want the snow golem", spawn.Kind)
	}
	want := [][3]int32{{2, 66, 3}, {2, 65, 3}, {2, 64, 3}}
	if !slices.Equal(spawn.Clear, want) {
		t.Errorf("the snow clear = %v, want the head-first walk %v", spawn.Clear, want)
	}
	if spawn.Feet != [3]int32{2, 64, 3} {
		t.Errorf("the snow golem stands at %v, want the column's foot (2, 64, 3)", spawn.Feet)
	}
}

// TestAnIronTBuildsAnIronGolem checks the ordinary T: the body under the
// head with an arm either side and the legs two below, the corners the
// shape demands empty, the golem standing in the legs' cell and the +axis
// arm leaving the world first.
func TestAnIronTBuildsAnIronGolem(t *testing.T) {
	w := testWorld{}
	iron := defaultState(t, "minecraft:iron_block")
	pumpkin := carvedPumpkin(t)
	for _, cell := range [][3]int32{{1, 65, 3}, {2, 65, 3}, {3, 65, 3}, {2, 64, 3}} {
		w.set(cell[0], cell[1], cell[2], iron)
	}
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the T of iron blocks")
	}
	if spawn.Kind != IronGolem {
		t.Errorf("the T builds %s, want the iron golem", spawn.Kind)
	}
	want := [][3]int32{{3, 65, 3}, {2, 66, 3}, {2, 65, 3}, {2, 64, 3}, {1, 65, 3}}
	if !slices.Equal(spawn.Clear, want) {
		t.Errorf("the iron clear = %v, want the +axis-arm-first walk %v", spawn.Clear, want)
	}
	if spawn.Feet != [3]int32{2, 64, 3} {
		t.Errorf("the iron golem stands at %v, want the legs' cell (2, 64, 3)", spawn.Feet)
	}
}

// TestABrokenPatternBuildsNothing checks the forms vanilla's match refuses:
// an iron corner filled, the column one snow short or made of the wrong
// block, and a head that is no golem's head at all.
func TestABrokenPatternBuildsNothing(t *testing.T) {
	pumpkin, snow, stone := carvedPumpkin(t),
		defaultState(t, "minecraft:snow_block"), defaultState(t, "minecraft:stone")
	iron := defaultState(t, "minecraft:iron_block")
	for _, tc := range []struct {
		name  string
		setup func(w testWorld)
	}{
		{name: "an iron corner filled", setup: func(w testWorld) {
			// The whole T with its legs' +x corner filled: no plane the
			// shape lies in takes it.
			w.set(1, 65, 3, iron)
			w.set(2, 65, 3, iron)
			w.set(3, 65, 3, iron)
			w.set(2, 64, 3, iron)
			w.set(3, 64, 3, stone)
			w.set(2, 66, 3, pumpkin)
		}},
		{name: "a column one snow short", setup: func(w testWorld) {
			w.set(2, 64, 3, snow)
			w.set(2, 66, 3, pumpkin)
		}},
		{name: "a column made of the wrong block", setup: func(w testWorld) {
			w.set(2, 65, 3, stone)
			w.set(2, 64, 3, snow)
			w.set(2, 66, 3, pumpkin)
		}},
		{name: "no head at all", setup: func(w testWorld) {
			w.set(2, 65, 3, snow)
			w.set(2, 64, 3, snow)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld{}
			tc.setup(w)
			if _, ok := GolemSpawnOf(w, 2, 66, 3); ok {
				t.Fatalf("GolemSpawnOf matched %s, want no golem of it", tc.name)
			}
		})
	}
}

// carvedPumpkin is the carved pumpkin's default state, one of the four
// facings the pattern's head cell reads the same.
func carvedPumpkin(t *testing.T) uint16 {
	t.Helper()
	return defaultState(t, "minecraft:carved_pumpkin")
}

// jackOLantern is the lit pumpkin's default state.
func jackOLantern(t *testing.T) uint16 {
	t.Helper()
	return defaultState(t, "minecraft:jack_o_lantern")
}

// TestACopperColumnBuildsACopperGolemInAnyOrientation checks the pumpkin and
// one copper block in each of the six directions: the golem stands in the
// head's cell, the pair is cleared head first, and the chest is the block's
// stage.
func TestACopperColumnBuildsACopperGolemInAnyOrientation(t *testing.T) {
	pumpkin := carvedPumpkin(t)
	for _, d := range [][3]int32{{0, 0, 1}, {0, 0, -1}, {1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}} {
		w := testWorld{}
		w.set(2, 66, 3, pumpkin)
		w.set(2+d[0], 66+d[1], 3+d[2], defaultState(t, "minecraft:weathered_copper"))
		spawn, ok := GolemSpawnOf(w, 2, 66, 3)
		if !ok {
			t.Fatalf("GolemSpawnOf said nothing of copper at %v from the head", d)
		}
		if spawn.Kind != CopperGolem || spawn.Weather != 2 || spawn.Chest != registry.BlockWeatheredCopperChest {
			t.Errorf("copper at %v builds %s, weather %d, chest %d", d, spawn.Kind, spawn.Weather, spawn.Chest)
		}
		want := [][3]int32{{2, 66, 3}, {2 + d[0], 66 + d[1], 3 + d[2]}}
		if !slices.Equal(spawn.Clear, want) || spawn.Feet != [3]int32{2, 66, 3} {
			t.Errorf("copper at %v clears %v and stands at %v, want %v at the head", d, spawn.Clear, spawn.Feet, want)
		}
	}
}
