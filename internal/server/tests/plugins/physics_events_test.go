package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// The physics-side block events, black-box: each is asked at the write the
// engine is about to make, a cancel leaves the world as it was, and an edited
// field is what gets written. The main loop is stopped and the test drives the
// passes by hand, so the listeners fire on the goroutine that ticks.

const physicsSeed = int64(11)

// physicsScene is a server with the listeners from listen, one player to keep
// the chunks loaded and the loop stopped.
func physicsScene(t *testing.T, listen func(plugin.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "physics", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		listen(s)
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Join("Walker")
	c.Settle()
	srv.StopTick()
	srv.DriveTicksHere()
	servertest.SeedPhysics(t, srv, physicsSeed)
	return srv, c
}

func stateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s %v", name, props)
	}
	return state
}

func nameAt(srv *server.Server, x, y, z int32) string {
	name, _ := servertest.StateProps(srv.BlockAt(x, y, z))
	return name
}

// tickUntil runs physics one tick at a time until cond or n ticks.
func tickUntil(srv *server.Server, n int, cond func() bool) bool {
	for range n {
		if cond() {
			return true
		}
		srv.TickPhysics(1)
	}
	return cond()
}

func pos(x, y, z int) plugin.BlockPos { return plugin.BlockPos{X: x, Y: y, Z: z} }

// vanilla: FlowingFluid.spreadTo
func TestPluginBlockFromToFiresAndCancels(t *testing.T) {
	t.Parallel()
	var flows []plugin.BlockFromToEvent
	srv, _ := physicsScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockFromToEvent) {
			flows = append(flows, plugin.BlockFromToEvent{From: e.From, To: e.To, Face: e.Face})
			e.SetCancelled(e.To.Pos == pos(5, 64, 0))
		})
	})
	srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:water"))
	srv.TickPhysics(30)
	if len(flows) == 0 {
		t.Fatal("no BlockFromToEvent")
	}
	for _, f := range flows {
		if f.From.Pos == pos(4, 64, 0) && f.To.Pos == pos(5, 64, 0) && f.Face != plugin.East {
			t.Errorf("flow east has face %v", f.Face)
		}
	}
	if nameAt(srv, 5, 64, 0) != "minecraft:air" {
		t.Errorf("the vetoed cell holds %s, want air", nameAt(srv, 5, 64, 0))
	}
	if nameAt(srv, 3, 64, 0) != "minecraft:water" {
		t.Errorf("the free side holds %s, want water", nameAt(srv, 3, 64, 0))
	}
}

// vanilla: FlowingFluid.getSpread, spreadToSides
func TestPluginFluidSpreadKeepsPlannedLevelsAndRereadsTargets(t *testing.T) {
	t.Parallel()
	source, north, east, south := pos(4, 64, 4), pos(4, 64, 3), pos(5, 64, 4), pos(4, 64, 5)
	water := plugin.DefaultState(registry.BlockWater)
	cobweb := plugin.DefaultState(registry.BlockCobweb)
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{
		Name: "fluid", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			plugin.Listen(s, func(e *plugin.BlockFromToEvent) {
				if e.From.Pos != source || e.To.Pos != north {
					return
				}
				// Recomputing the east flow after this edit would make it fall.
				if !e.To.World.SetBlockState(east.Offset(plugin.Up, 1), water, plugin.UpdateAll) {
					t.Error("could not place water over the east target")
				}
				// A stale target state would make the south hook veto the write.
				if !e.To.World.SetBlockState(south, cobweb, plugin.UpdateAll) {
					t.Error("could not place a cobweb at the south target")
				}
			})
			return nil
		}},
	}})
	c := servertest.DialServer(t, srv)
	c.Join("Builder")
	servertest.HoldTicks(t, srv)
	for _, drop := range []plugin.BlockPos{north, east, south} {
		srv.SetBlockAndTell(int32(drop.X), 63, int32(drop.Z), registry.BlockAir.DefaultState())
	}
	srv.SetBlockAndTell(4, 64, 4, uint16(water))
	servertest.RunTicks(t, srv, 5)
	for _, target := range []plugin.BlockPos{north, east, south} {
		name, props := servertest.StateProps(srv.BlockAt(int32(target.X), 64, int32(target.Z)))
		if name != "minecraft:water" || props["level"] != "1" {
			t.Errorf("flow at %v = %s %v, want level-1 water", target, name, props)
		}
	}
}

// vanilla: LiquidBlock.shouldSpreadLiquid
func TestPluginBlockFormLavaHardensCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var forms int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockFormEvent) {
					forms++
					if e.Block.Pos != pos(6, 64, 0) || e.NewState != plugin.DefaultState(registry.BlockObsidian) {
						t.Errorf("form inputs: %v %v", e.Block.Pos, e.NewState)
					}
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.NewState = plugin.DefaultState(registry.BlockGlowstone)
					}
				})
			})
			srv.SetBlockAndTell(7, 64, 0, servertest.BlockState(t, "minecraft:water"))
			srv.SetBlockAndTell(6, 64, 0, stateOf(t, "minecraft:lava", map[string]string{"level": "0"}))
			srv.TickPhysics(5)
			if forms == 0 {
				t.Fatal("no BlockFormEvent")
			}
			got := nameAt(srv, 6, 64, 0)
			if mode == "edit" && got != "minecraft:glowstone" {
				t.Errorf("the lava cell holds %s, want the edited glowstone", got)
			}
			// A refused hardening leaves the lava unhardened: the water beside
			// it then flows over it.
			if mode == "cancel" && (got == "minecraft:obsidian" || got == "minecraft:cobblestone") {
				t.Errorf("the lava cell hardened to %s despite the cancel", got)
			}
		})
	}
}

// vanilla: Level.neighborChanged
func TestPluginBlockPhysicsCancelStopsTheReaction(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var seen []plugin.BlockPhysicsEvent
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.BlockPhysicsEvent) {
				if e.Block.Pos == pos(4, 65, 0) {
					seen = append(seen, plugin.BlockPhysicsEvent{Source: e.Source, State: e.State})
				}
				e.SetCancelled(cancel && e.Block.Pos == pos(4, 65, 0))
			})
		})
		sand := servertest.BlockState(t, "minecraft:sand")
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:stone"))
		srv.SetBlockAndTell(4, 65, 0, sand)
		srv.TickPhysics(5)
		seen = nil
		srv.SetBlockAndTell(4, 64, 0, 0)
		srv.TickPhysics(5)
		if len(seen) == 0 || seen[0].Source.Pos != pos(4, 64, 0) || seen[0].State != plugin.BlockState(sand) {
			t.Fatalf("cancel=%v: physics inputs %v", cancel, seen)
		}
		if standing := srv.BlockAt(4, 65, 0) == sand; standing != cancel {
			t.Errorf("cancel=%v: sand standing=%v", cancel, standing)
		}
	}
}

// vanilla: RedStoneWireBlock.updatePowerStrength
func TestPluginBlockRedstoneEditsPower(t *testing.T) {
	t.Parallel()
	var events []plugin.BlockRedstoneEvent
	srv, _ := physicsScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockRedstoneEvent) {
			events = append(events, plugin.BlockRedstoneEvent{Block: e.Block, Old: e.Old, New: e.New})
			e.New = 3
		})
	})
	srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:redstone_wire"))
	srv.SetBlockAndTell(5, 64, 0, servertest.BlockState(t, "minecraft:redstone_block"))
	srv.TickPhysics(5)
	if len(events) == 0 || events[0].Old != 0 || events[0].New != 15 || events[0].Block.Pos != pos(4, 64, 0) {
		t.Fatalf("redstone inputs: %v", events)
	}
	_, props := servertest.StateProps(srv.BlockAt(4, 64, 0))
	if props["power"] != "3" {
		t.Errorf("the wire reads power %s, want the edited 3", props["power"])
	}
}

// vanilla: PistonBaseBlock.moveBlocks
func TestPluginBlockPistonExtendCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var events []plugin.BlockPistonEvent
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.BlockPistonEvent) {
				events = append(events, plugin.BlockPistonEvent{Piston: e.Piston, Direction: e.Direction, Extending: e.Extending, Blocks: e.Blocks})
				e.SetCancelled(cancel)
			})
		})
		stone := servertest.BlockState(t, "minecraft:stone")
		srv.SetBlockAndTell(4, 64, 0, stateOf(t, "minecraft:piston", map[string]string{"facing": "east", "extended": "false"}))
		srv.SetBlockAndTell(5, 64, 0, stone)
		srv.SetBlockAndTell(4, 64, 1, servertest.BlockState(t, "minecraft:redstone_block"))
		srv.TickPhysics(10)
		if len(events) == 0 || !events[0].Extending || events[0].Direction != plugin.East ||
			len(events[0].Blocks) != 1 || events[0].Blocks[0].Pos != pos(5, 64, 0) {
			t.Fatalf("cancel=%v: piston inputs %+v", cancel, events)
		}
		if pushed := srv.BlockAt(6, 64, 0) == stone; pushed == cancel {
			t.Errorf("cancel=%v: stone pushed=%v", cancel, pushed)
		}
	}
}

// vanilla: CropBlock.randomTick
func TestPluginBlockGrowCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var grows int
			ripe := stateOf(t, "minecraft:wheat", map[string]string{"age": "7"})
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockGrowEvent) {
					grows++
					if e.Block.Pos != pos(6, 64, 0) {
						t.Errorf("grow at %v", e.Block.Pos)
					}
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.NewState = plugin.BlockState(ripe)
					}
				})
			})
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)
			srv.SetBlockAndTell(6, 63, 0, stateOf(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
			young := stateOf(t, "minecraft:wheat", map[string]string{"age": "0"})
			srv.SetBlockAndTell(6, 64, 0, young)
			srv.TickRandomTicks(3000, world.ChunkPos{})
			if grows == 0 {
				t.Fatal("no BlockGrowEvent")
			}
			want := ripe
			if mode == "cancel" {
				want = young
			}
			if got := srv.BlockAt(6, 64, 0); got != want {
				t.Errorf("the crop holds state %d, want %d", got, want)
			}
		})
	}
}

// vanilla: FireBlock.tick, checkBurnOut
func TestPluginFireBurnIgniteSpread(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"burn-cancel", "ignite-cancel", "spread-edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var burns, ignites, spreads int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockBurnEvent) {
					burns++
					if e.Fire == nil {
						t.Error("burn has no fire")
					}
					e.SetCancelled(mode == "burn-cancel")
				})
				plugin.Listen(s, func(e *plugin.BlockIgniteEvent) {
					ignites++
					if e.Cause != plugin.IgniteSpread {
						t.Errorf("ignite cause %v", e.Cause)
					}
					e.SetCancelled(mode == "ignite-cancel")
				})
				plugin.Listen(s, func(e *plugin.BlockSpreadEvent) {
					spreads++
					e.NewState = plugin.DefaultState(registry.BlockStone)
				})
			})
			wool := servertest.BlockState(t, "minecraft:white_wool")
			srv.SetBlockAndTell(4, 63, 0, servertest.BlockState(t, "minecraft:netherrack"))
			srv.SetBlockAndTell(5, 64, 0, wool)
			srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:fire"))
			count := func() (n int) {
				for x := int32(2); x <= 8; x++ {
					for y := int32(64); y <= 68; y++ {
						for z := int32(-3); z <= 3; z++ {
							if nameAt(srv, x, y, z) == "minecraft:fire" {
								n++
							}
						}
					}
				}
				return n
			}
			tickUntil(srv, 1500, func() bool {
				if mode == "burn-cancel" {
					return burns > 0
				}
				return ignites > 0 && burns > 0
			})
			switch mode {
			case "burn-cancel":
				if burns == 0 {
					t.Fatal("no BlockBurnEvent")
				}
				srv.TickPhysics(400)
				if srv.BlockAt(5, 64, 0) != wool {
					t.Error("the cancelled burn still consumed the wool")
				}
			case "ignite-cancel":
				if ignites == 0 {
					t.Fatal("no BlockIgniteEvent")
				}
				if n := count(); n != 1 {
					t.Errorf("%d fires stand, want the original only", n)
				}
			case "spread-edit":
				if spreads == 0 {
					t.Fatal("no BlockSpreadEvent")
				}
				if n := count(); n != 1 {
					t.Errorf("%d fires stand, want the original only (spread rewritten to stone)", n)
				}
			}
		})
	}
}

// vanilla: FarmlandBlock.randomTick
func TestPluginMoistureChangeCancel(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"dry", "wet"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var asked int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.MoistureChangeEvent) {
					asked++
					if e.Block.Pos != pos(6, 63, 0) {
						t.Errorf("moisture at %v", e.Block.Pos)
					}
					e.SetCancelled(true)
				})
			})
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)
			start := "3"
			if mode == "wet" {
				start = "0"
				srv.SetBlockAndTell(6, 63, 2, stateOf(t, "minecraft:water", map[string]string{"level": "0"}))
			}
			farmland := stateOf(t, "minecraft:farmland", map[string]string{"moisture": start})
			srv.SetBlockAndTell(6, 63, 0, farmland)
			srv.TickRandomTicks(3000, world.ChunkPos{})
			if asked == 0 {
				t.Fatal("no MoistureChangeEvent")
			}
			if got := srv.BlockAt(6, 63, 0); got != farmland {
				t.Errorf("a cancelled change left state %d, want %d", got, farmland)
			}
		})
	}
}
