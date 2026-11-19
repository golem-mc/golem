package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// The block-entity side of the block events: a furnace burning fuel and
// finishing a smelt, a campfire's cook, a brew, a cauldron, primed TNT and a
// bed's blast. Each is asked before the write, so a cancel leaves the
// container as it was.

func furnaceScene(t *testing.T, listen func(plugin.Server)) *server.Server {
	t.Helper()
	srv, _ := physicsScene(t, listen)
	srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:furnace"))
	srv.PutBlockEntityStack(world.KindFurnace, 4, 64, 0, 0, servertest.ItemStack(t, "minecraft:raw_iron", 2))
	srv.PutBlockEntityStack(world.KindFurnace, 4, 64, 0, 1, servertest.ItemStack(t, "minecraft:coal", 3))
	return srv
}

// vanilla: AbstractFurnaceBlockEntity.serverTick
func TestPluginFurnaceBurnCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var fuels []plugin.FurnaceBurnEvent
			srv := furnaceScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.FurnaceBurnEvent) {
					fuels = append(fuels, plugin.FurnaceBurnEvent{Block: e.Block, Fuel: e.Fuel, BurnTime: e.BurnTime})
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.BurnTime = 7
					}
				})
			})
			servertest.TickBlockEntitiesByHand(srv, 1)
			if len(fuels) == 0 || fuels[0].Block.Pos != pos(4, 64, 0) || fuels[0].Fuel.Item != registry.ItemCoal || fuels[0].BurnTime != 1600 {
				t.Fatalf("burn inputs: %+v", fuels)
			}
			_, _, lit, _, _ := srv.FurnaceAt(4, 64, 0)
			coal := srv.BlockEntityStack(4, 64, 0, 1).Count
			if mode == "cancel" && (lit != 0 || coal != 3) {
				t.Errorf("cancelled burn: lit %d, coal %d, want 0 and 3", lit, coal)
			}
			if mode == "edit" && (lit != 7 || coal != 2) {
				t.Errorf("edited burn: lit %d, coal %d, want 7 and 2", lit, coal)
			}
		})
	}
}

// vanilla: AbstractFurnaceBlockEntity.burn
func TestPluginBlockCookCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var cooks []plugin.BlockCookEvent
			srv := furnaceScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockCookEvent) {
					cooks = append(cooks, plugin.BlockCookEvent{Block: e.Block, Source: e.Source, Result: e.Result})
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.Result = plugin.ItemStack{Item: registry.ItemGoldIngot, Count: 1}
					}
				})
			})
			servertest.TickBlockEntitiesByHand(srv, 1)
			srv.SetCookingAt(4, 64, 0, 199, 200)
			servertest.TickBlockEntitiesByHand(srv, 1)
			if len(cooks) != 1 || cooks[0].Source.Item != registry.ItemRawIron || cooks[0].Result.Item != registry.ItemIronIngot {
				t.Fatalf("cook inputs: %+v", cooks)
			}
			out, in := srv.BlockEntityStack(4, 64, 0, 2), srv.BlockEntityStack(4, 64, 0, 0)
			spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0)
			if mode == "cancel" && (!out.Empty() || in.Count != 2 || spent != 0) {
				t.Errorf("cancelled cook: output %+v, input %d, progress %d", out, in.Count, spent)
			}
			if mode == "edit" && (registry.Item(out.ItemID) != registry.ItemGoldIngot || in.Count != 1) {
				t.Errorf("edited cook: output %+v, input %d", out, in.Count)
			}
		})
	}
}

// vanilla: CampfireBlockEntity.cookTick
func TestPluginCampfireCookCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var cooks int
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.BlockCookEvent) {
				cooks++
				e.SetCancelled(cancel)
			})
		})
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:campfire"))
		srv.PutBlockEntityStack(world.KindCampfire, 4, 64, 0, 0, servertest.ItemStack(t, "minecraft:beef", 1))
		servertest.TickBlockEntitiesByHand(srv, 2)
		if cooks == 0 {
			t.Fatalf("cancel=%v: no BlockCookEvent", cancel)
		}
		if kept := !srv.BlockEntityStack(4, 64, 0, 0).Empty(); kept != cancel {
			t.Errorf("cancel=%v: the food stayed in its slot: %v", cancel, kept)
		}
	}
}

// vanilla: BrewingStandBlockEntity.doBrew
func TestPluginBrewCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var brews int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BrewEvent) {
					brews++
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.Results[0] = plugin.ItemStack{Item: registry.ItemDiamond, Count: 1}
					}
				})
			})
			srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:brewing_stand"))
			srv.PutBlockEntityStack(world.KindBrewingStand, 4, 64, 0, 0, servertest.WaterBottle(t))
			srv.PutBlockEntityStack(world.KindBrewingStand, 4, 64, 0, 3, servertest.ItemStack(t, "minecraft:nether_wart", 1))
			srv.PutBlockEntityStack(world.KindBrewingStand, 4, 64, 0, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))
			servertest.TickBlockEntitiesByHand(srv, 1)
			srv.SetBrewingTime(4, 64, 0, 1)
			servertest.TickBlockEntitiesByHand(srv, 1)
			if brews != 1 {
				t.Fatalf("%d BrewEvents, want 1", brews)
			}
			ingredient, bottle := srv.BlockEntityStack(4, 64, 0, 3), srv.BlockEntityStack(4, 64, 0, 0)
			if mode == "cancel" && (ingredient.Count != 1 || bottle.Empty()) {
				t.Errorf("cancelled brew spent the ingredient: %+v", ingredient)
			}
			if mode == "edit" && (!ingredient.Empty() || registry.Item(bottle.ItemID) != registry.ItemDiamond) {
				t.Errorf("edited brew: ingredient %+v, bottle %+v", ingredient, bottle)
			}
		})
	}
}

// vanilla: TntBlock.prime
func TestPluginTNTPrimeCancel(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var primes int
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.TNTPrimeEvent) {
				primes++
				if e.Block.Pos != pos(4, 64, 0) {
					t.Errorf("prime at %v", e.Block.Pos)
				}
				e.SetCancelled(cancel)
			})
		})
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:air"))
		primed := srv.PrimeTNTAt(4, 64, 0)
		if primes != 1 || primed == cancel {
			t.Errorf("cancel=%v: %d events, primed %v", cancel, primes, primed)
		}
		if got := nameAt(srv, 4, 64, 0); (got == "minecraft:tnt") != cancel {
			t.Errorf("cancel=%v: the cell holds %s", cancel, got)
		}
	}
}

// vanilla: ServerExplosion.explode
func TestPluginBlockExplodeCancelAndTrim(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "trim"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var blasts int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.BlockExplodeEvent) {
					blasts++
					if len(e.Blocks) == 0 {
						t.Error("the blast reached no block")
					}
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.Blocks = nil
					}
				})
			})
			dirt := servertest.BlockState(t, "minecraft:dirt")
			srv.SetBlockAndTell(4, 63, 0, dirt)
			srv.SetBlockAndTell(5, 63, 0, dirt)
			srv.BlockExplosion(4, 64, 0, servertest.BlockState(t, "minecraft:red_bed"))
			if blasts != 1 {
				t.Fatalf("%d BlockExplodeEvents, want 1", blasts)
			}
			if nameAt(srv, 5, 63, 0) != "minecraft:dirt" {
				t.Errorf("the blast removed a block it was told to keep: %s", nameAt(srv, 5, 63, 0))
			}
		})
	}
}

// vanilla: LayeredCauldronBlock.lowerFillLevel
func TestPluginCauldronLevelChangeCancelAndEdit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "edit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var changes int
			srv, _ := physicsScene(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.CauldronLevelChangeEvent) {
					changes++
					if mode == "cancel" {
						e.SetCancelled(true)
					} else {
						e.NewState = plugin.DefaultState(registry.BlockLavaCauldron)
					}
				})
			})
			srv.SetBlockAndTell(4, 64, 0, stateOf(t, "minecraft:water_cauldron", map[string]string{"level": "2"}))
			srv.DouseCauldronAt(4, 64, 0)
			if changes != 1 {
				t.Fatalf("%d events, want 1", changes)
			}
			want := "minecraft:water_cauldron"
			if mode == "edit" {
				want = "minecraft:lava_cauldron"
			}
			if got := nameAt(srv, 4, 64, 0); got != want {
				t.Errorf("the cauldron is %s, want %s", got, want)
			}
		})
	}
}
