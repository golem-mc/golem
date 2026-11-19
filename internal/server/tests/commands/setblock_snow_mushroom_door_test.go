package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SnowyBlock.updateShape, HugeMushroomBlock.updateShape, DoorBlock.updateShape
func TestCommandPlacementSnowMushroomsAndDoors(t *testing.T) {
	t.Parallel()
	type cell struct {
		x, y, z int32
		block   string
	}
	type shapeCase struct {
		name, requested, want string
		setup, neighbors      []cell
	}
	var cases []shapeCase
	for _, dirt := range []string{"grass_block", "podzol", "mycelium"} {
		for _, snow := range []string{"snow", "snow_block"} {
			cases = append(cases, shapeCase{
				name: dirt + "_under_" + snow, requested: dirt, want: dirt + "[snowy=true]",
				setup: []cell{{0, 1, 0, snow}},
			})
		}
		cases = append(cases, shapeCase{
			name: dirt + "_defined", requested: dirt + "[snowy=false]", want: dirt,
			setup: []cell{{0, 1, 0, "snow"}},
		}, shapeCase{
			name: dirt + "_strict", requested: dirt + " strict", want: dirt,
			setup: []cell{{0, 1, 0, "snow_block"}},
		})
	}
	for _, mushroom := range []string{"brown_mushroom_block", "red_mushroom_block", "mushroom_stem"} {
		for _, face := range []struct {
			name, opposite string
			x, y, z        int32
		}{
			{"west", "east", -1, 0, 0}, {"east", "west", 1, 0, 0},
			{"north", "south", 0, 0, -1}, {"south", "north", 0, 0, 1},
			{"down", "up", 0, -1, 0}, {"up", "down", 0, 1, 0},
		} {
			cases = append(cases, shapeCase{
				name: mushroom + "_" + face.name, requested: mushroom, want: mushroom + "[" + face.name + "=false]",
				setup:     []cell{{face.x, face.y, face.z, mushroom}},
				neighbors: []cell{{face.x, face.y, face.z, mushroom + "[" + face.opposite + "=false]"}},
			})
		}
	}
	cases = append(cases,
		shapeCase{"mushroom_different_kind", "brown_mushroom_block", "brown_mushroom_block", []cell{{1, 0, 0, "red_mushroom_block"}}, nil},
		shapeCase{"mushroom_defined", "brown_mushroom_block[east=true]", "brown_mushroom_block", []cell{{1, 0, 0, "brown_mushroom_block"}}, nil},
		shapeCase{"mushroom_strict", "brown_mushroom_block strict", "brown_mushroom_block", []cell{{1, 0, 0, "brown_mushroom_block"}}, nil},
		shapeCase{"door_lower", "oak_door[half=lower]", "oak_door[half=lower,open=true]", []cell{{0, -1, 0, "stone"}, {0, 1, 0, "oak_door[half=upper,open=true]"}}, []cell{{0, 1, 0, "oak_door[half=upper,open=true]"}}},
		shapeCase{"door_lower_properties", "oak_door[half=lower]", "oak_door[half=lower,open=true,facing=west,hinge=right,powered=true]", []cell{{0, -1, 0, "redstone_block"}, {0, 1, 0, "oak_door[half=upper,open=true,facing=west,hinge=right,powered=true]"}}, nil},
		shapeCase{"door_upper", "oak_door[half=upper]", "oak_door[half=upper,open=true,facing=east,hinge=right]", []cell{{0, -2, 0, "stone"}, {0, -1, 0, "oak_door[half=lower,open=true,facing=east,hinge=right]"}}, nil},
		shapeCase{"door_defined", "oak_door[half=lower,open=false]", "oak_door[half=lower]", []cell{{0, -1, 0, "stone"}, {0, 1, 0, "oak_door[half=upper,open=true]"}}, []cell{{0, 1, 0, "oak_door[half=upper]"}}},
		shapeCase{"door_defined_properties", "oak_door[half=lower,facing=south,hinge=left,powered=false]", "oak_door[half=lower,open=true,facing=south]", []cell{{0, -1, 0, "stone"}, {0, 1, 0, "oak_door[half=upper,open=true,facing=west,hinge=right]"}}, nil},
		shapeCase{"door_strict", "oak_door[half=lower] strict", "oak_door[half=lower]", []cell{{0, -1, 0, "stone"}, {0, 1, 0, "oak_door[half=upper,open=true]"}}, []cell{{0, 1, 0, "oak_door[half=upper,open=true]"}}},
		shapeCase{"door_no_partner_keeps_typed", "oak_door[half=lower]", "oak_door[half=lower]", []cell{{0, -1, 0, "stone"}}, nil},
		// DoorBlock accepts any DoorBlock partner, including weathered copper.
		shapeCase{"door_adopts_partner_kind", "copper_door[half=lower]", "weathered_copper_door[half=lower,open=true]", []cell{{0, -1, 0, "stone"}, {0, 1, 0, "weathered_copper_door[half=upper,open=true]"}}, nil},
	)
	state := func(block string) uint16 {
		name, props, _ := strings.Cut(block, "[")
		return registry.MustState("minecraft:"+name, registry.ParseStateProps(strings.TrimSuffix(props, "]")))
	}
	for _, command := range []string{"setblock", "fill"} {
		t.Run(command, func(t *testing.T) {
			srv, c := setblockActor(t)
			servertest.HoldTicks(t, srv)
			srv.SetTickFrozen(true)
			for i, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					x, y, z := int32(3+3*(i%4)), int32(80+5*(i/4)), int32(5)
					for _, s := range tc.setup {
						servertest.Setblock(t, c, x+s.x, y+s.y, z+s.z, s.block+" strict")
					}
					cmd := fmt.Sprintf("setblock %d %d %d %s", x, y, z, tc.requested)
					if command == "fill" {
						cmd = fmt.Sprintf("fill %d %d %d %d %d %d %s", x, y, z, x, y, z, tc.requested)
					}
					c.ChatCommand(cmd)
					var sent uint16
					if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
						for _, ch := range servertest.BlockChangesIn(t, id, data) {
							if ch.X == x && ch.Y == y && ch.Z == z {
								sent = ch.State
								return true
							}
						}
						return false
					}) {
						t.Fatal("no block update arrived")
					}
					want := state(tc.want)
					if sent != want {
						t.Errorf("client state = %d, want %d (%s)", sent, want, tc.want)
					}
					if got := srv.BlockAt(x, y, z); got != want {
						t.Errorf("world state = %d, want %d (%s)", got, want, tc.want)
					}
					for _, n := range tc.neighbors {
						if got, want := srv.BlockAt(x+n.x, y+n.y, z+n.z), state(n.block); got != want {
							t.Errorf("neighbor (%d,%d,%d) state = %d, want %d (%s)", n.x, n.y, n.z, got, want, n.block)
						}
					}
				})
			}
		})
	}
}
