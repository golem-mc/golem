package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestBlockFriction checks Block.getFriction's table: six tenths for the
// ordinary blocks, the ice family's higher slick and the slime block's.
// vanilla: Block.getFriction
func TestBlockFriction(t *testing.T) {
	cases := map[string]float64{
		"minecraft:stone":       0.6,
		"minecraft:dirt":        0.6,
		"minecraft:ice":         0.98,
		"minecraft:packed_ice":  0.98,
		"minecraft:blue_ice":    0.989,
		"minecraft:slime_block": 0.8,
	}
	for name, want := range cases {
		if got := BlockFriction(defaultState(t, name)); math.Abs(got-want) > 1e-9 {
			t.Errorf("BlockFriction(%s) = %v, want %v", name, got, want)
		}
	}
}

// TestReplaceableBlocksByState checks what a placement may overwrite: air,
// the fluids and the replaceable vegetation say yes; a block standing in the
// way says no.
func TestReplaceableBlocksByState(t *testing.T) {
	yes := map[string]uint16{
		"air":         airState,
		"water":       defaultState(t, "minecraft:water"),
		"lava":        defaultState(t, "minecraft:lava"),
		"fire":        defaultState(t, "minecraft:fire"),
		"short grass": defaultState(t, "minecraft:short_grass"),
		"snow layer":  mustStateFor(t, "minecraft:snow", map[string]string{"layers": "1"}),
	}
	for what, state := range yes {
		if !IsReplaceableState(state) {
			t.Errorf("IsReplaceableState said no to %s (state %d)", what, state)
		}
	}
	no := map[string]uint16{
		"stone":               defaultState(t, "minecraft:stone"),
		"furnace":             defaultState(t, "minecraft:furnace"),
		"a flower":            defaultState(t, "minecraft:dandelion"),
		"cobble wall default": defaultState(t, "minecraft:cobblestone_wall"),
		// A waterlogged block is full of water and still a block standing
		// in the way: a bucket cannot pour into it any more than a
		// placement can.
		"waterlogged": mustStateFor(t, "minecraft:oak_fence", map[string]string{"waterlogged": "true"}),
	}
	for what, state := range no {
		if IsReplaceableState(state) {
			t.Errorf("IsReplaceableState said yes to %s (state %d)", what, state)
		}
	}
}

// mustStateFor resolves one state of a named block by overrides, so the tests
// name the exact state they mean.
func mustStateFor(t *testing.T, name string, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, overrides)
	if !ok {
		t.Fatalf("the block registry has no %s with %v", name, overrides)
	}
	return state
}

// TestClimbableByState checks the climb question the fall asks of a body's
// feet: the climbable tag's blocks say yes wherever their facing, an open
// trapdoor says yes when a ladder of its own facing sits under it, and
// everything else — a closed trapdoor, a mismatched one, the cobweb that
// catches a fall by other means — says no.
func TestClimbableByState(t *testing.T) {
	eastLadder := mustStateFor(t, "minecraft:ladder", map[string]string{"facing": "east"})
	southLadder := mustStateFor(t, "minecraft:ladder", map[string]string{"facing": "south"})
	openEast := mustStateFor(t, "minecraft:oak_trapdoor",
		map[string]string{"facing": "east", "open": "true"})
	shutEast := mustStateFor(t, "minecraft:oak_trapdoor",
		map[string]string{"facing": "east", "open": "false"})

	yes := map[string][2]uint16{
		"ladder":      {eastLadder, airState},
		"vine":        {defaultState(t, "minecraft:vine"), airState},
		"scaffolding": {defaultState(t, "minecraft:scaffolding"), airState},
		"open trapdoor over a ladder of its own facing": {openEast, eastLadder},
	}
	for what, cells := range yes {
		if !IsClimbable(cells[0], cells[1]) {
			t.Errorf("IsClimbable said no to %s", what)
		}
	}
	no := map[string][2]uint16{
		"stone":                 {defaultState(t, "minecraft:stone"), airState},
		"shut trapdoor":         {shutEast, eastLadder},
		"trapdoor over no rung": {openEast, airState},
		"trapdoor off its rung": {openEast, southLadder},
		"trapdoor over a vine":  {openEast, defaultState(t, "minecraft:vine")},
		"cobweb":                {defaultState(t, "minecraft:cobweb"), airState},
		"air":                   {airState, airState},
	}
	for what, cells := range no {
		if IsClimbable(cells[0], cells[1]) {
			t.Errorf("IsClimbable said yes to %s", what)
		}
	}
}

// TestWaterByState checks the swimming question: water and every waterlogged
// state are water, the plants whose getFluidState is a water source are too,
// lava is the fluid it is not, and nothing dry answers yes.
func TestWaterByState(t *testing.T) {
	if !IsWater(defaultState(t, "minecraft:water")) {
		t.Error("IsWater said no to water itself")
	}
	if !IsWater(mustStateFor(t, "minecraft:oak_fence", map[string]string{"waterlogged": "true"})) {
		t.Error("IsWater said no to a waterlogged block")
	}
	for _, name := range []string{"minecraft:kelp", "minecraft:kelp_plant", "minecraft:seagrass", "minecraft:tall_seagrass"} {
		if !IsWater(defaultState(t, name)) {
			t.Errorf("IsWater said no to %s, want the water its own cell holds", name)
		}
	}
	if IsWater(defaultState(t, "minecraft:lava")) {
		t.Error("IsWater said yes to lava, want it not to count for swimming")
	}
	if IsWater(defaultState(t, "minecraft:stone")) {
		t.Error("IsWater said yes to stone")
	}
	if IsWater(airState) {
		t.Error("IsWater said yes to air")
	}
}

func TestBlockBounceRestitution(t *testing.T) {
	slime, _ := registry.FindState("minecraft:slime_block", nil)
	bed, _ := registry.FindState("minecraft:red_bed", nil)
	honey, _ := registry.FindState("minecraft:honey_block", nil)
	for _, tc := range []struct {
		name   string
		state  uint16
		living bool
		want   float64
	}{
		{"slime living", slime, true, 1},
		{"bed living", bed, true, 0.75},
		{"slime item", slime, false, float64(float32(0.8))},
		{"bed item", bed, false, float64(float32(float32(0.75) * 0.8))},
		{"honey", honey, true, 0},
	} {
		if got := BlockBounceRestitution(tc.state, tc.living); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}
