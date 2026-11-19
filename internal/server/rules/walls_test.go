package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestAWallStateFollowsWhatStandsAroundIt checks the post and arm decisions
// together, through the state they pick for real cobblestone: arms low under
// open air, arms tall under a full cube without a post, and the post raised
// under the post of a wall above or a torch in #wall_post_override.
func TestAWallStateFollowsWhatStandsAroundIt(t *testing.T) {
	const name = "minecraft:cobblestone_wall"

	w := testWorld{}
	// Two walls south and north of the spot, so the arms have something to
	// reach for.
	w.set(0, 0, -1, defaultState(t, name))
	w.set(0, 0, 1, defaultState(t, name))

	// Open air above: both arms low, and the post stays down for a plain
	// straight run.
	state := WallStateFromConnections(registry.BlockCobblestoneWall, true, true, false, false, airState)
	props := stateProps(t, state)
	want := map[string]string{"north": "low", "south": "low", "east": "none", "west": "none", "up": "false"}
	for prop, v := range want {
		if props[prop] != v {
			t.Errorf("wall under air: %s = %q, want %q", prop, props[prop], v)
		}
	}

	// A stone above: the arms reach the full height, and the covered top is
	// the tall arms' own, so the post stays down.
	state = WallStateFromConnections(registry.BlockCobblestoneWall, true, true, false, false, defaultState(t, "minecraft:stone"))
	props = stateProps(t, state)
	if props["north"] != "tall" || props["south"] != "tall" {
		t.Errorf("wall under stone: north = %q, south = %q, want tall tall",
			props["north"], props["south"])
	}
	if props["up"] != "false" {
		t.Errorf("wall under stone: up = %q, want false, the tall arms covering it", props["up"])
	}

	// A wall with a post above: the post of the wall above raises this
	// one's, which is what stacks two walls over the same column.
	postAbove, ok := registry.FindState(name, map[string]string{"up": "true"})
	if !ok {
		t.Fatal("the block registry has no cobblestone wall post state")
	}
	state = WallStateFromConnections(registry.BlockCobblestoneWall, true, true, false, false, postAbove)
	props = stateProps(t, state)
	if props["up"] != "true" {
		t.Errorf("wall under a wall's post: up = %q, want true", props["up"])
	}
	if props["north"] != "low" {
		t.Errorf("wall under a wall's post: north = %q, want low, the wall above covering nothing", props["north"])
	}

	// A torch above sits in #wall_post_override: the post rises even though
	// the run is straight, and the torch covers no arm.
	state = WallStateFromConnections(registry.BlockCobblestoneWall, true, true, false, false, defaultState(t, "minecraft:torch"))
	props = stateProps(t, state)
	if props["up"] != "true" {
		t.Errorf("wall under a torch: up = %q, want true", props["up"])
	}
	if props["north"] != "low" {
		t.Errorf("wall under a torch: north = %q, want low, a torch covering nothing", props["north"])
	}
}

// TestAWallStandingAloneKeepsTheRegistryDefault checks that the rules and the
// registry agree on the wall anyone meets first: an isolated wall holds the
// post state its default is, which is what a /setblock or a fresh placement
// of a wall with nothing around it produces.
func TestAWallStandingAloneKeepsTheRegistryDefault(t *testing.T) {
	const name = "minecraft:cobblestone_wall"
	w := testWorld{}
	state := WallStateWithConnections(w, registry.BlockCobblestoneWall, 0, 0, 0)
	if state != defaultState(t, name) {
		t.Errorf("isolated wall state = %d, want the default %d", state, defaultState(t, name))
	}
	props := stateProps(t, state)
	if props["up"] != "true" {
		t.Errorf("isolated wall up = %q, want the post raised", props["up"])
	}
}

// TestAWallReadsTheDownFaceOfTheBlockAbove checks that the arms follow the
// block above's collision shape rather than its sturdiness: a bottom slab,
// a bottom stair and a carpet fill their down face, so the arms go tall,
// while a top slab and a top stair leave the arms low.
func TestAWallReadsTheDownFaceOfTheBlockAbove(t *testing.T) {
	cases := []struct {
		block string
		props map[string]string
		arm   string
	}{
		{"minecraft:oak_slab", map[string]string{"type": "bottom"}, "tall"},
		{"minecraft:oak_slab", map[string]string{"type": "top"}, "low"},
		{"minecraft:oak_stairs", map[string]string{"half": "bottom"}, "tall"},
		{"minecraft:oak_stairs", map[string]string{"half": "top"}, "low"},
		{"minecraft:white_carpet", nil, "tall"},
		{"minecraft:stone", nil, "tall"},
	}
	for _, c := range cases {
		above, ok := registry.FindState(c.block, c.props)
		if !ok {
			t.Fatalf("no state for %s %v", c.block, c.props)
		}
		props := stateProps(t, WallStateFromConnections(registry.BlockCobblestoneWall, true, true, false, false, above))
		if props["north"] != c.arm || props["south"] != c.arm {
			t.Errorf("wall under %s %v: north = %q, south = %q, want %s", c.block, c.props, props["north"], props["south"], c.arm)
		}
		if props["up"] != "false" {
			t.Errorf("wall under %s %v: up = %q, want false", c.block, c.props, props["up"])
		}
	}
}
