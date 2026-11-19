package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Block.updateFromNeighbourShapes, BlockInput.place and the blocks' updateShape
func TestCommandPlacementShapes(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"setblock", "fill"} {
		t.Run(command, func(t *testing.T) {
			srv, c := setblockActor(t)
			servertest.HoldTicks(t, srv)
			srv.SetTickFrozen(true)
			type cell struct {
				dy    int32
				block string
			}
			for i, tc := range []struct {
				name, requested, want string
				setup                 []cell
				randomAge             bool
			}{
				{"kelp_body_to_head", "kelp_plant", "kelp", []cell{{-1, "dirt"}}, true},
				{"cave_body_berries", "cave_vines_plant[berries=true]", "cave_vines[berries=true]", []cell{{1, "stone"}}, true},
				{"kelp_head_to_body", "kelp", "kelp_plant", []cell{{-1, "dirt"}, {0, "water"}, {1, "kelp"}}, false},
				{"twisting_head_to_body", "twisting_vines", "twisting_vines_plant", []cell{{-1, "dirt"}, {1, "twisting_vines"}}, false},
				{"cave_head_to_body", "cave_vines", "cave_vines_plant", []cell{{1, "stone"}, {-1, "cave_vines"}}, false},
				{"dripleaf", "big_dripleaf", "big_dripleaf_stem", []cell{{-1, "dirt"}, {1, "big_dripleaf"}}, false},
				{"dripstone", "pointed_dripstone[vertical_direction=down]", "pointed_dripstone[vertical_direction=down,thickness=frustum]", []cell{{1, "stone"}, {-1, "pointed_dripstone[vertical_direction=down]"}}, false},
				{"chorus", "chorus_plant", "chorus_plant[down=true,up=true]", []cell{{-1, "end_stone"}, {1, "chorus_flower"}}, false},
				{"moss", "pale_hanging_moss", "pale_hanging_moss[tip=false]", []cell{{1, "stone"}, {-1, "pale_hanging_moss"}}, false},
				{"kelp_body_stays", "kelp_plant", "kelp_plant", []cell{{-1, "dirt"}, {1, "kelp_plant"}}, false},
				{"strict_body", "kelp_plant strict", "kelp_plant", []cell{{-1, "dirt"}}, false},
				{"strict_head", "kelp strict", "kelp", []cell{{-1, "dirt"}, {1, "kelp"}}, false},
				{"strict_dripleaf", "big_dripleaf strict", "big_dripleaf", []cell{{-1, "dirt"}, {1, "big_dripleaf"}}, false},
				{"defined_moss_tip", "pale_hanging_moss[tip=true]", "pale_hanging_moss", []cell{{1, "stone"}, {-1, "pale_hanging_moss"}}, false},
				{"defined_thickness", "pointed_dripstone[vertical_direction=down,thickness=base]", "pointed_dripstone[vertical_direction=down,thickness=base]", []cell{{1, "stone"}, {-1, "pointed_dripstone[vertical_direction=down]"}}, false},
				{"cave_head_keeps_berries", "cave_vines[berries=true]", "cave_vines_plant[berries=true]", []cell{{1, "stone"}, {-1, "cave_vines"}}, false},
				{"weeping_head", "weeping_vines", "weeping_vines_plant", []cell{{1, "stone"}, {-1, "weeping_vines"}}, false},
				{"weeping_body", "weeping_vines_plant", "weeping_vines", []cell{{1, "stone"}}, true},
				{"twisting_body", "twisting_vines_plant", "twisting_vines", []cell{{-1, "dirt"}}, true},
				{"dripleaf_properties", "big_dripleaf[facing=west,waterlogged=true]", "big_dripleaf_stem[facing=west,waterlogged=true]", []cell{{-1, "dirt"}, {1, "big_dripleaf"}}, false},
				{"dripleaf_removed_tilt", "big_dripleaf[tilt=full,facing=west,waterlogged=true]", "big_dripleaf_stem[facing=west,waterlogged=true]", []cell{{-1, "dirt"}, {1, "big_dripleaf"}}, false},
				{"cave_removed_age", "cave_vines[age=7,berries=true]", "cave_vines_plant[berries=true]", []cell{{1, "stone"}, {-1, "cave_vines"}}, false},
				{"unsupported_dripleaf_keeps_typed", "big_dripleaf", "big_dripleaf", []cell{{1, "big_dripleaf"}}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					x, z := int32(3+2*(i%5)), int32(3+2*(i/5))
					const y = int32(80)
					for _, s := range tc.setup {
						servertest.Setblock(t, c, x, y+s.dy, z, s.block+" strict")
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
					name, props, _ := strings.Cut(tc.want, "[")
					want := registry.MustState("minecraft:"+name, registry.ParseStateProps(strings.TrimSuffix(props, "]")))
					got := srv.BlockAt(x, y, z)
					if got != sent {
						t.Errorf("world state %d differs from client state %d", got, sent)
					}
					if tc.randomAge {
						age, ok := registry.StateInt(got, registry.PropAge)
						if !ok || age < 0 || age >= 25 {
							t.Errorf("head age = %d, present=%v; want [0,25)", age, ok)
						}
						got, _ = registry.WithInt(got, registry.PropAge, 0)
					}
					if got != want {
						t.Errorf("placed state = %d (%s), want %d (%s)", got, registry.BlockOf(got).Name(), want, tc.want)
					}
				})
			}
		})
	}
}
