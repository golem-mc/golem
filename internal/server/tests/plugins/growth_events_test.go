package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// The bone meal and game event hooks. A bone meal growth writes as it decides,
// so a veto is undone before any client hears of it.

// boneMealOnce clicks the bone meal on the dig target and waits for the
// listeners to have run n times.
func boneMealOnce(t *testing.T, srv *server.Server, c *servertest.Client, n *int, want int) {
	t.Helper()
	d := servertest.DigTarget
	c.UseBlock(d[0], d[1], d[2])
	fired(t, srv, c, "bone meal", n, want)
}

// vanilla: BoneMealItem.growCrop, CropBlock.performBonemeal
func TestPluginBlockFertilizeCancelAndEdit(t *testing.T) {
	for _, mode := range []string{"cancel", "edit", "keep"} {
		t.Run(mode, func(t *testing.T) {
			var fertilized int
			ripe := stateOf(t, "minecraft:wheat", map[string]string{"age": "7"})
			young := stateOf(t, "minecraft:wheat", map[string]string{"age": "0"})
			srv, _, c := blockScene(t, "minecraft:bone_meal", false, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockFertilizeEvent) {
					fertilized++
					d := servertest.DigTarget
					if e.Player == nil || e.Player.Name() != "Miner" || e.Block.Pos != (plugin.BlockPos{X: int(d[0]), Y: int(d[1]), Z: int(d[2])}) ||
						len(e.Blocks) != 1 || e.Blocks[0].Pos != e.Block.Pos || e.Blocks[0].State == plugin.BlockState(young) {
						t.Errorf("fertilize inputs: %+v", e)
					}
					switch mode {
					case "cancel":
						e.SetCancelled(true)
					case "edit":
						e.Blocks[0].State = plugin.BlockState(ripe)
					}
				})
			})
			d := servertest.DigTarget
			srv.SetBlock(d[0], d[1]-1, d[2], stateOf(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
			srv.SetBlock(d[0], d[1], d[2], young)
			boneMealOnce(t, srv, c, &fertilized, 1)
			got := srv.BlockAt(d[0], d[1], d[2])
			switch {
			case mode == "cancel" && got != young:
				t.Errorf("a cancelled fertilize left state %d, want the young crop", got)
			case mode == "edit" && got != ripe:
				t.Errorf("the edited fertilize left state %d, want the ripe crop", got)
			case mode == "keep" && (got == young || got == ripe && false):
				t.Errorf("the crop did not grow")
			}
		})
	}
}

// vanilla: SaplingBlock.advanceTree, TreeGrower.growTree
func TestPluginStructureGrowCancelAndEdit(t *testing.T) {
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			var grown int
			srv, _, c := blockScene(t, "minecraft:bone_meal", false, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.StructureGrowEvent) {
					grown++
					d := servertest.DigTarget
					if e.Player == nil || e.Player.Name() != "Miner" || e.Pos != (plugin.BlockPos{X: int(d[0]), Y: int(d[1]), Z: int(d[2])}) || len(e.Blocks) < 5 {
						t.Errorf("structure inputs: pos=%v blocks=%d", e.Pos, len(e.Blocks))
					}
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.Blocks = e.Blocks[:1]
					}
				})
			})
			d := servertest.DigTarget
			srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bone_meal"), Count: 64})
			srv.SetBlock(d[0], d[1]-1, d[2], servertest.BlockState(t, "minecraft:grass_block"))
			sapling := stateOf(t, "minecraft:oak_sapling", map[string]string{"stage": "1"})
			srv.SetBlock(d[0], d[1], d[2], sapling)
			// The 0.45 roll gates the growth: click until the listeners ran.
			for i := 0; grown == 0; i++ {
				if i == 40 {
					t.Fatal("the sapling never grew")
				}
				d := servertest.DigTarget
				c.UseBlock(d[0], d[1], d[2])
				// The click's listeners wait for a tick; pace them in real time.
				for range 10 {
					servertest.RunTicks(t, srv, 1)
					time.Sleep(2 * time.Millisecond)
				}
				c.Settle()
			}
			logs := 0
			for y := d[1]; y < d[1]+8; y++ {
				if name := nameAt(srv, d[0], y, d[2]); name == "minecraft:oak_log" {
					logs++
				}
			}
			switch {
			case mode == "cancel" && srv.BlockAt(d[0], d[1], d[2]) != sapling:
				t.Errorf("a cancelled tree changed the sapling to %s", nameAt(srv, d[0], d[1], d[2]))
			case mode == "cancel" && nameAt(srv, d[0], d[1]+1, d[2]) != "minecraft:air":
				t.Errorf("a cancelled tree left %s above the sapling", nameAt(srv, d[0], d[1]+1, d[2]))
			case mode == "edit" && logs > 1:
				t.Errorf("the trimmed tree stands %d logs, want at most 1", logs)
			}
		})
	}
}

// vanilla: ServerLevel.gameEvent
func TestPluginGenericGameEventCancelAndRadius(t *testing.T) {
	for _, mode := range []string{"hear", "cancel", "radius"} {
		t.Run(mode, func(t *testing.T) {
			var fired int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.GenericGameEvent) {
					if e.Event != plugin.MinecraftKey("block_place") {
						return
					}
					fired++
					if e.Pos != (plugin.Vec3{X: 5.5, Y: 64.5, Z: 0.5}) || e.Entity != nil || e.Radius != 16 {
						t.Errorf("game event inputs: %+v", e)
					}
					switch mode {
					case "cancel":
						e.SetCancelled(true)
					case "radius":
						e.Radius = 1
					}
				})
			})
			srv.SetBlockAndTell(8, 64, 0, stateOf(t, "minecraft:sculk_sensor", nil))
			srv.PostGameEvent("minecraft:block_place", 5.5, 64.5, 0.5)
			heard := tickUntil(srv, 40, func() bool {
				_, props := servertest.StateProps(srv.BlockAt(8, 64, 0))
				return props["sculk_sensor_phase"] != "inactive"
			})
			if fired != 1 {
				t.Errorf("GenericGameEvent fired %d times, want 1", fired)
			}
			if heard != (mode == "hear") {
				t.Errorf("sensor heard it: %v in mode %s", heard, mode)
			}
		})
	}
}
