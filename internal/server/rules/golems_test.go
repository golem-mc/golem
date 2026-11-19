package rules

import (
	"maps"
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

// TestAnIronTAlsoStandsItsArmsTheOtherWay checks the same T turned ninety
// degrees, its arms along Z: the shape matches in the other plane to the
// one the ordinary build lies in.
func TestAnIronTAlsoStandsItsArmsTheOtherWay(t *testing.T) {
	w := testWorld{}
	iron := defaultState(t, "minecraft:iron_block")
	pumpkin := carvedPumpkin(t)
	for _, cell := range [][3]int32{{2, 65, 2}, {2, 65, 3}, {2, 65, 4}, {2, 64, 3}} {
		w.set(cell[0], cell[1], cell[2], iron)
	}
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the T with its arms along Z")
	}
	if spawn.Feet != [3]int32{2, 64, 3} {
		t.Errorf("the turned T's golem stands at %v, want the legs' cell (2, 64, 3)", spawn.Feet)
	}
}

// TestThePatternsCountPointingAnyWay checks the orientations vanilla's
// BlockPattern.find turns the shapes through — checked against the
// reference server: the snow column standing over the head, and running
// away sideways, build as the ordinary one does, standing the golem two
// rows from the head the way the rows run.
func TestThePatternsCountPointingAnyWay(t *testing.T) {
	pumpkin := carvedPumpkin(t)
	snow := defaultState(t, "minecraft:snow_block")
	for _, tc := range []struct {
		name  string
		setup func(w testWorld)
		feet  [3]int32
	}{
		{name: "snow above the head", feet: [3]int32{2, 68, 3}, setup: func(w testWorld) {
			w.set(2, 68, 3, snow)
			w.set(2, 67, 3, snow)
			w.set(2, 66, 3, pumpkin)
		}},
		{name: "snow running east of the head", feet: [3]int32{4, 66, 3}, setup: func(w testWorld) {
			w.set(3, 66, 3, snow)
			w.set(4, 66, 3, snow)
			w.set(2, 66, 3, pumpkin)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld{}
			tc.setup(w)
			spawn, ok := GolemSpawnOf(w, 2, 66, 3)
			if !ok {
				t.Fatalf("GolemSpawnOf said nothing of %s", tc.name)
			}
			if spawn.Kind != SnowGolem {
				t.Errorf("%s builds %s, want the snow golem", tc.name, spawn.Kind)
			}
			if spawn.Feet != tc.feet {
				t.Errorf("%s stands its golem at %v, want %v", tc.name, spawn.Feet, tc.feet)
			}
		})
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

// TestTheSnowShapeOutranksTheIron builds both patterns at once — snow
// standing over the head, iron lying under it — and checks that the snow
// golem is the one vanilla's ask order builds.
func TestTheSnowShapeOutranksTheIron(t *testing.T) {
	w := testWorld{}
	pumpkin := carvedPumpkin(t)
	snow := defaultState(t, "minecraft:snow_block")
	iron := defaultState(t, "minecraft:iron_block")
	w.set(2, 68, 3, snow)
	w.set(2, 67, 3, snow)
	w.set(1, 65, 3, iron)
	w.set(2, 65, 3, iron)
	w.set(3, 65, 3, iron)
	w.set(2, 64, 3, iron)
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the double build")
	}
	if spawn.Kind != SnowGolem {
		t.Errorf("the double build = %s, want the snow golem of vanilla's ask order", spawn.Kind)
	}
}

// TestAJackOLanternHeadBuilds checks the lit head: the pattern reads either
// face of the pumpkin the same CarvedPumpkinBlock places.
// vanilla: CarvedPumpkinBlock.PUMPKINS_PREDICATE
func TestAJackOLanternHeadBuilds(t *testing.T) {
	w := testWorld{}
	snow := defaultState(t, "minecraft:snow_block")
	lantern := jackOLantern(t)
	w.set(2, 64, 3, snow)
	w.set(2, 65, 3, snow)
	w.set(2, 66, 3, lantern)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the snow column under a jack o'lantern")
	}
	if spawn.Kind != SnowGolem {
		t.Errorf("the jack o'lantern build = %s, want the snow golem", spawn.Kind)
	}
}

// TestTheIronCornersTakeEveryAir checks the shape's '~' corners against
// the three blocks vanilla's isAir answers yes to: cave air and void air
// stand in for air as readily as air itself.
// vanilla: BlockBehaviour.BlockStateBase.isAir
func TestTheIronCornersTakeEveryAir(t *testing.T) {
	iron := defaultState(t, "minecraft:iron_block")
	pumpkin := carvedPumpkin(t)
	for _, air := range []string{"minecraft:air", "minecraft:cave_air", "minecraft:void_air"} {
		t.Run(air, func(t *testing.T) {
			w := testWorld{}
			for _, cell := range [][3]int32{{1, 65, 3}, {2, 65, 3}, {3, 65, 3}, {2, 64, 3}} {
				w.set(cell[0], cell[1], cell[2], iron)
			}
			for _, corner := range [][3]int32{{1, 66, 3}, {3, 66, 3}, {1, 64, 3}, {3, 64, 3}} {
				w.set(corner[0], corner[1], corner[2], defaultState(t, air))
			}
			w.set(2, 66, 3, pumpkin)

			spawn, ok := GolemSpawnOf(w, 2, 66, 3)
			if !ok {
				t.Fatalf("GolemSpawnOf said nothing of the T with %s corners", air)
			}
			if spawn.Kind != IronGolem {
				t.Errorf("the T with %s corners builds %s, want the iron golem", air, spawn.Kind)
			}
		})
	}
}

// TestTheSnowShapePrefersFindOrder checks which pattern builds when two are
// complete at once: a column below the head and one to +Z, the axis find
// meets first.
// vanilla: BlockPattern.find
func TestTheSnowShapePrefersFindOrder(t *testing.T) {
	w := testWorld{}
	pumpkin := carvedPumpkin(t)
	snow := defaultState(t, "minecraft:snow_block")
	w.set(2, 65, 3, snow)
	w.set(2, 64, 3, snow)
	w.set(2, 66, 4, snow)
	w.set(2, 66, 5, snow)
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the two snow columns")
	}
	if spawn.Kind != SnowGolem {
		t.Fatalf("the two columns build %s, want the snow golem", spawn.Kind)
	}
	if spawn.Feet != [3]int32{2, 66, 5} {
		t.Errorf("the two columns' golem stands at %v, want the +Z column's foot (2, 66, 5)", spawn.Feet)
	}
}

// TestTheIronShapePrefersFindOrder checks the same precedence between two
// T's complete at once: one pointing down the head and one pointing +Z,
// the plane find's anchor walk — offset along the arm axis — meets first.
// vanilla: BlockPattern.find
func TestTheIronShapePrefersFindOrder(t *testing.T) {
	w := testWorld{}
	pumpkin := carvedPumpkin(t)
	iron := defaultState(t, "minecraft:iron_block")
	for _, cell := range [][3]int32{{1, 65, 3}, {2, 65, 3}, {3, 65, 3}, {2, 64, 3}} {
		w.set(cell[0], cell[1], cell[2], iron)
	}
	for _, cell := range [][3]int32{{1, 66, 4}, {2, 66, 4}, {3, 66, 4}, {2, 66, 5}} {
		w.set(cell[0], cell[1], cell[2], iron)
	}
	w.set(2, 66, 3, pumpkin)

	spawn, ok := GolemSpawnOf(w, 2, 66, 3)
	if !ok {
		t.Fatal("GolemSpawnOf said nothing of the two T's")
	}
	if spawn.Kind != IronGolem {
		t.Fatalf("the two T's build %s, want the iron golem", spawn.Kind)
	}
	if spawn.Feet != [3]int32{2, 66, 5} {
		t.Errorf("the two T's golem stands at %v, want the +Z T's foot (2, 66, 5)", spawn.Feet)
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

// TestAWaxedCopperBlockBuildsAWaxedChestAtItsStage checks the chest names of
// every copper block, and that the other builds keep precedence.
func TestAWaxedCopperBlockBuildsAWaxedChestAtItsStage(t *testing.T) {
	for block, chest := range map[string]registry.Block{
		"minecraft:copper_block":          registry.BlockCopperChest,
		"minecraft:waxed_copper_block":    registry.BlockWaxedCopperChest,
		"minecraft:waxed_oxidized_copper": registry.BlockWaxedOxidizedCopperChest,
		"minecraft:exposed_copper":        registry.BlockExposedCopperChest,
	} {
		w := testWorld{}
		w.set(2, 66, 3, carvedPumpkin(t))
		w.set(2, 65, 3, defaultState(t, block))
		spawn, ok := GolemSpawnOf(w, 2, 66, 3)
		if !ok || spawn.Chest != chest {
			t.Errorf("%s gives chest %d (matched %v), want %d", block, spawn.Chest, ok, chest)
		}
	}
	w := testWorld{}
	w.set(2, 66, 3, carvedPumpkin(t))
	w.set(2, 65, 3, defaultState(t, "minecraft:snow_block"))
	if _, ok := GolemSpawnOf(w, 2, 66, 3); ok {
		t.Error("a pumpkin on one snow block built a golem")
	}
}

// TestGolemBaseNear checks canSpawnGolem's pattern search over its cube.
func TestGolemBaseNear(t *testing.T) {
	snow := defaultState(t, "minecraft:snow_block")
	copper := defaultState(t, "minecraft:copper_block")
	iron := defaultState(t, "minecraft:iron_block")
	stone := defaultState(t, "minecraft:stone")
	ironT := map[[3]int32]uint16{{-1, -1, 0}: iron, {0, -1, 0}: iron, {1, -1, 0}: iron, {0, -2, 0}: iron}
	ironTBlocked := map[[3]int32]uint16{{-1, -2, 0}: stone}
	maps.Copy(ironTBlocked, ironT)
	cases := []struct {
		name  string
		set   map[[3]int32]uint16
		found bool
	}{
		{"iron T", ironT, true},
		{"iron T with a corner blocked", ironTBlocked, false},
		{"two snow below", map[[3]int32]uint16{{0, -1, 0}: snow, {0, -2, 0}: snow}, true},
		{"one snow", map[[3]int32]uint16{{0, -1, 0}: snow}, false},
		{"copper diagonal", map[[3]int32]uint16{{1, -1, 0}: copper}, true},
		{"copper beside", map[[3]int32]uint16{{-1, 0, 0}: copper}, true},
		{"copper too far", map[[3]int32]uint16{{3, 0, 0}: copper}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		w := testWorld{}
		for p, s := range c.set {
			w.set(p[0], p[1], p[2], s)
		}
		if got := GolemBaseNear(w, 0, 0, 0); got != c.found {
			t.Errorf("%s: GolemBaseNear = %v, want %v", c.name, got, c.found)
		}
	}
}
