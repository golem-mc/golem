package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func propOf(srv interface{ BlockAt(x, y, z int32) uint16 }, x, y, z int32, key string) string {
	_, props := servertest.StateProps(srv.BlockAt(x, y, z))
	return props[key]
}

func TestReshapingKeepsUnrelatedProperties(t *testing.T) {
	for _, tc := range []struct {
		name, placed, neighbor string
		dx, dz                 int32
		want                   map[string]string
	}{
		{"fence", "oak_fence[waterlogged=true,north=true]", "stone", 1, 0,
			map[string]string{"waterlogged": "true", "north": "true", "east": "true"}},
		{"bars", "iron_bars[waterlogged=true,north=true]", "stone", 1, 0,
			map[string]string{"waterlogged": "true", "north": "true", "east": "true"}},
		{"wall", "cobblestone_wall[waterlogged=true,north=low]", "stone", 1, 0,
			map[string]string{"waterlogged": "true", "north": "low", "east": "low", "up": "true"}},
		{"gate", "oak_fence_gate[facing=north,open=true,powered=false]", "cobblestone_wall", 1, 0,
			map[string]string{"facing": "north", "open": "true", "powered": "false", "in_wall": "true"}},
		{"stairs", "oak_stairs[facing=north,half=top,waterlogged=true]", "oak_stairs[facing=west,half=top]", 0, -1,
			map[string]string{"facing": "north", "half": "top", "waterlogged": "true", "shape": "outer_left"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.HoldTicks(t, srv)
			servertest.Setblock(t, c, 5, 65, 5, tc.placed)
			servertest.Setblock(t, c, 5+tc.dx, 65, 5+tc.dz, tc.neighbor)
			state := srv.BlockAt(5, 65, 5)
			for key, want := range tc.want {
				prop, _ := registry.PropNamed(key)
				if got := registry.StateValue(state, prop); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// TestFenceNeighbourChangeKeepsTheOtherSides replaces the west neighbour of a
// fence in a /fill row and requires the side spelled in the fill to survive.
// vanilla: FenceBlock.updateShape
func TestFenceNeighbourChangeKeepsTheOtherSides(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ChatCommand("fill 70 64 5 72 64 5 oak_fence[north=true]")
	c.Settle()
	servertest.Setblock(t, c, 69, 64, 5, "stone")
	c.Settle()
	if got := propOf(srv, 70, 64, 5, "west"); got != "true" {
		t.Errorf("west = %q, want true after stone beside it", got)
	}
	if got := propOf(srv, 70, 64, 5, "north"); got != "true" {
		t.Errorf("north = %q, want the spelled true", got)
	}
}

// TestWallNeighbourChangeKeepsTheOtherSides is the wall counterpart.
// vanilla: WallBlock.updateShape
func TestWallNeighbourChangeKeepsTheOtherSides(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ChatCommand("fill 70 64 9 72 64 9 cobblestone_wall[north=low]")
	c.Settle()
	servertest.Setblock(t, c, 69, 64, 9, "stone")
	c.Settle()
	if got := propOf(srv, 70, 64, 9, "west"); got == "none" {
		t.Errorf("west = none, want a connection to the stone")
	}
	if got := propOf(srv, 70, 64, 9, "north"); got != "low" {
		t.Errorf("north = %q, want the spelled low", got)
	}
}
