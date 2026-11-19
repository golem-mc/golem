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
