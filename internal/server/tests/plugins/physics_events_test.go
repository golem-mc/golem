package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// vanilla: LeavesBlock.randomTick
func TestPluginLeavesDecayCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var decays int
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.LeavesDecayEvent) {
				decays++
				e.SetCancelled(cancel)
			})
		})
		srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)
		leaf := stateOf(t, "minecraft:oak_leaves", map[string]string{"persistent": "false", "distance": "7", "waterlogged": "false"})
		srv.SetBlockAndTell(4, 70, 0, leaf)
		srv.TickRandomTicks(3000, world.ChunkPos{})
		if decays == 0 {
			t.Fatalf("cancel=%v: no LeavesDecayEvent", cancel)
		}
		if gone := srv.BlockAt(4, 70, 0) != leaf; gone == cancel {
			t.Errorf("cancel=%v: leaf gone=%v", cancel, gone)
		}
	}
}

// vanilla: SpongeBlock.removeWaterBreadthFirstSearch
func TestPluginSpongeAbsorbCancelAndTrim(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"none", "cancel", "trim"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var cells int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.SpongeAbsorbEvent) {
					cells = len(e.Blocks)
					switch mode {
					case "cancel":
						e.SetCancelled(true)
					case "trim":
						e.Blocks = e.Blocks[:1]
					}
				})
			})
			water := servertest.BlockState(t, "minecraft:water")
			srv.SetBlockAndTell(6, 64, 0, water)
			srv.SetBlockAndTell(7, 64, 0, water)
			srv.SetBlockAndTell(5, 64, 0, servertest.BlockState(t, "minecraft:sponge"))
			if cells != 2 {
				t.Fatalf("the event listed %d cells, want 2", cells)
			}
			wantSponge, wantFar := "minecraft:wet_sponge", "minecraft:air"
			switch mode {
			case "cancel":
				wantSponge, wantFar = "minecraft:sponge", "minecraft:water"
			case "trim":
				wantFar = "minecraft:water"
			}
			if got := nameAt(srv, 5, 64, 0); got != wantSponge {
				t.Errorf("sponge cell holds %s, want %s", got, wantSponge)
			}
			if got := nameAt(srv, 7, 64, 0); got != wantFar {
				t.Errorf("far water cell holds %s, want %s", got, wantFar)
			}
		})
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

// vanilla: NoteBlock.playNote
func TestPluginNotePlayCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var plays []plugin.NotePlayEvent
		srv, c := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.NotePlayEvent) {
				plays = append(plays, plugin.NotePlayEvent{Block: e.Block, Instrument: e.Instrument, Note: e.Note})
				e.SetCancelled(cancel)
			})
		})
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:note_block"))
		c.Settle()
		c.SettleAndCollect(func(int32, []byte) {})
		srv.SetBlockAndTell(5, 64, 0, servertest.BlockState(t, "minecraft:redstone_block"))
		srv.TickPhysics(3)
		var actions int
		c.SettleAndCollect(func(id int32, _ []byte) {
			if id == protocol.IDPlayBlockAction {
				actions++
			}
		})
		if len(plays) != 1 || plays[0].Block.Pos != pos(4, 64, 0) || plays[0].Instrument == "" {
			t.Fatalf("cancel=%v: note inputs %+v", cancel, plays)
		}
		if (actions != 0) != !cancel {
			t.Errorf("cancel=%v: %d block actions reached the client", cancel, actions)
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

// vanilla: SpreadingSnowyDirtBlock.randomTick
func TestPluginGrassFadeAndSpread(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fade", "fade-cancel", "spread-cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var fades, spreads int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockFadeEvent) {
					fades++
					e.SetCancelled(mode == "fade-cancel")
				})
				plugin.Listen(s, func(e *plugin.BlockSpreadEvent) {
					spreads++
					e.SetCancelled(true)
				})
			})
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)
			grass := servertest.BlockStateWith(t, "minecraft:grass_block", "snowy=false")
			dirt := servertest.BlockState(t, "minecraft:dirt")
			if mode == "spread-cancel" {
				srv.SetBlockAndTell(6, 63, 0, grass)
				srv.SetBlockAndTell(7, 63, 0, dirt)
				srv.TickRandomTicks(2000, world.ChunkPos{})
				if spreads == 0 || srv.BlockAt(7, 63, 0) != dirt {
					t.Errorf("spreads=%d, beside block %s", spreads, nameAt(srv, 7, 63, 0))
				}
				return
			}
			srv.SetBlockAndTell(6, 63, 0, grass)
			srv.SetBlockAndTell(6, 64, 0, servertest.BlockState(t, "minecraft:stone"))
			srv.TickRandomTicks(2000, world.ChunkPos{})
			if fades == 0 {
				t.Fatal("no BlockFadeEvent")
			}
			if dead := srv.BlockAt(6, 63, 0) == dirt; dead == (mode == "fade-cancel") {
				t.Errorf("%s: grass dead=%v", mode, dead)
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

// vanilla: LightningBolt.spawnFire
func TestPluginLightningIgniteCancel(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"allow", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var seen []plugin.BlockIgniteEvent
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockIgniteEvent) {
					seen = append(seen, plugin.BlockIgniteEvent{Block: e.Block, Cause: e.Cause})
					e.SetCancelled(mode == "cancel")
				})
			})
			srv.SetDifficulty(3)
			srv.SetBlockAndTell(8, 63, 0, servertest.BlockState(t, "minecraft:stone"))
			srv.SetBlockAndTell(8, 64, 0, 0)
			srv.StrikeLightning(8.5, 64, 0.5)
			srv.TickBolts(4)
			if len(seen) == 0 || seen[0].Cause != plugin.IgniteLightning || seen[0].Block.Pos != pos(8, 64, 0) {
				t.Fatalf("ignite events: %+v", seen)
			}
			if got := nameAt(srv, 8, 64, 0) == "minecraft:fire"; got == (mode == "cancel") {
				t.Errorf("fire at strike cell = %v in mode %s", got, mode)
			}
		})
	}
}
