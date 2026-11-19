package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

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
