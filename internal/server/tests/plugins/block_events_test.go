package plugins_test

import (
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// blockScene is a survival player holding held in the hotbar, with stone at
// the dig target when stone is set, ticks held. listen runs in the plugin's
// enable.
func blockScene(t *testing.T, held string, stone bool, listen func(plugin.Server)) (*server.Server, plugin.Server, *servertest.Client) {
	t.Helper()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(), []plugin.Registration{{Name: "blocks", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		listen(s)
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Join("Miner")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem(held), Count: 1})
	d := servertest.DigTarget
	srv.SetBlock(d[0], d[1]-1, d[2], servertest.BlockState(t, "minecraft:stone"))
	if stone {
		srv.SetBlock(d[0], d[1], d[2], servertest.BlockState(t, "minecraft:stone"))
	}
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, host, c
}

// fired runs the ticks the held loop needs for a packet's listeners to run, then
// waits for the connection to finish handling it.
func fired(t *testing.T, srv *server.Server, c *servertest.Client, what string, n *int, want int) (ids []int32) {
	t.Helper()
	// The packet may not have been read yet, so the ticks are paced in real
	// time until its listeners have run.
	for i := 0; *n < want; i++ {
		if i == 400 {
			t.Fatalf("no %s", what)
		}
		servertest.RunTicks(t, srv, 1)
		time.Sleep(5 * time.Millisecond)
	}
	c.SettleAndCollect(func(id int32, _ []byte) { ids = append(ids, id) })
	return ids
}

func resent(t *testing.T, ids []int32) {
	t.Helper()
	if !slices.Contains(ids, protocol.IDPlayBlockUpdate) {
		t.Error("the cancelled action's block was not resent")
	}
}

// vanilla: ServerPlayerGameMode.handleBlockBreakAction
func TestPluginBlockDamageCancelInstaBreakAndAbort(t *testing.T) {
	var damaged, aborted int
	var cancel, insta bool
	srv, _, c := blockScene(t, "minecraft:diamond_pickaxe", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockDamageEvent) {
			damaged++
			if e.Player.Name() != "Miner" || e.Item.Item != registry.ItemDiamondPickaxe || e.InstaBreak {
				t.Errorf("damage inputs: %v %v %v", e.Player.Name(), e.Item.Item, e.InstaBreak)
			}
			e.SetCancelled(cancel)
			e.InstaBreak = insta
		})
		plugin.Listen(s, func(e *plugin.BlockDamageAbortEvent) { aborted++ })
	})
	d := servertest.DigTarget
	stone := servertest.BlockState(t, "minecraft:stone")

	c.StartDigging(d[0], d[1], d[2])
	fired(t, srv, c, "damage event", &damaged, 1)
	c.CancelDigging(d[0], d[1], d[2])
	fired(t, srv, c, "abort event", &aborted, 1)
	if damaged != 1 || aborted != 1 {
		t.Fatalf("damage=%d abort=%d, want 1 and 1", damaged, aborted)
	}

	cancel = true
	c.StartDigging(d[0], d[1], d[2])
	resent(t, fired(t, srv, c, "cancelled damage event", &damaged, 2))
	if damaged != 2 || srv.BlockAt(d[0], d[1], d[2]) != stone {
		t.Fatalf("cancelled dig: damage=%d, block %d", damaged, srv.BlockAt(d[0], d[1], d[2]))
	}

	cancel, insta = false, true
	c.StartDigging(d[0], d[1], d[2])
	fired(t, srv, c, "instant damage event", &damaged, 3)
	servertest.ExpectAir(t, srv, d)
}

// vanilla: ServerPlayerGameMode.destroyBlock
func TestPluginBlockBreakCancelAndLoot(t *testing.T) {
	var broke int
	var cancel, noDrops bool
	xp := -1
	srv, _, c := blockScene(t, "minecraft:diamond_pickaxe", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockBreakEvent) {
			broke++
			if e.Player.Name() != "Miner" || e.Block.Pos != (plugin.BlockPos{X: 2, Y: 64}) ||
				e.State != plugin.DefaultState(registry.BlockStone) || !e.DropItems {
				t.Errorf("break inputs: %v %v %v", e.Block.Pos, e.State, e.DropItems)
			}
			e.SetCancelled(cancel)
			e.DropItems = !noDrops
			if xp >= 0 {
				e.Experience = xp
			}
		})
	})
	d := servertest.DigTarget
	stone := servertest.BlockState(t, "minecraft:stone")
	dig := func() {
		c.StartDigging(d[0], d[1], d[2])
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		c.StopDigging(d[0], d[1], d[2])
		c.Settle()
		servertest.RunTicks(t, srv, 12)
	}

	cancel = true
	dig()
	if broke != 1 || srv.BlockAt(d[0], d[1], d[2]) != stone || srv.ItemCount() != 0 {
		t.Fatalf("cancelled break: events=%d block=%d items=%d", broke, srv.BlockAt(d[0], d[1], d[2]), srv.ItemCount())
	}

	cancel, noDrops, xp = false, true, 4
	c.StartDigging(d[0], d[1], d[2])
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.StopDigging(d[0], d[1], d[2])
	c.Settle()
	// The orb is the player's within a few ticks, so look as the block goes.
	servertest.UntilTicks(t, srv, 20, "the block to break", func() bool { return srv.BlockAt(d[0], d[1], d[2]) == 0 })
	if srv.ItemCount() != 0 || srv.OrbCount() == 0 {
		t.Fatalf("edited break: items=%d orbs=%d, want no items and an orb", srv.ItemCount(), srv.OrbCount())
	}
}

// Without a listener the break drops the block's loot as before.
func TestPluginBlockBreakKeepsVanillaLoot(t *testing.T) {
	var broke int
	srv, _, c := blockScene(t, "minecraft:diamond_pickaxe", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockBreakEvent) { broke++ })
	})
	d := servertest.DigTarget
	c.StartDigging(d[0], d[1], d[2])
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.StopDigging(d[0], d[1], d[2])
	c.Settle()
	servertest.RunTicks(t, srv, 12)
	servertest.ExpectAir(t, srv, d)
	if broke != 1 || srv.ItemCount() != 1 {
		t.Fatalf("events=%d items=%d, want 1 and 1", broke, srv.ItemCount())
	}
}

// vanilla: BlockItem.place
func TestPluginBlockPlaceCancelAndInputs(t *testing.T) {
	var placed int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:cobblestone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockPlaceEvent) {
			placed++
			if e.Player.Name() != "Miner" || e.Block.Pos != (plugin.BlockPos{X: 2, Y: 64}) ||
				e.Placed != plugin.DefaultState(registry.BlockCobblestone) || e.Replaced != plugin.DefaultState(registry.BlockAir) ||
				e.Against.Pos != (plugin.BlockPos{X: 2, Y: 63}) || e.Hand != plugin.MainHand ||
				e.Item.Item != registry.ItemCobblestone || len(e.Blocks) != 1 {
				t.Errorf("place inputs: %+v", e)
			}
			e.SetCancelled(cancel)
		})
	})
	d := servertest.DigTarget
	cancel = true
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	resent(t, fired(t, srv, c, "cancelled place", &placed, 1))
	if placed != 1 || srv.BlockAt(d[0], d[1], d[2]) != 0 || srv.InventorySlot("Miner", player.HotbarSlot).Count != 1 {
		t.Fatalf("cancelled place: events=%d block=%d", placed, srv.BlockAt(d[0], d[1], d[2]))
	}

	cancel = false
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	fired(t, srv, c, "place", &placed, 2)
	if placed != 2 || srv.BlockAt(d[0], d[1], d[2]) != servertest.BlockState(t, "minecraft:cobblestone") {
		t.Fatalf("place: events=%d block=%d", placed, srv.BlockAt(d[0], d[1], d[2]))
	}
}

// vanilla: ServerGamePacketListenerImpl.updateSignText
func TestPluginSignChangeEdit(t *testing.T) {
	var changed int
	var cancel bool
	srv, host, c := blockScene(t, "minecraft:oak_sign", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.SignChangeEvent) {
			changed++
			if e.Player.Name() != "Miner" || !e.Front || e.Lines[0].PlainString() != "typed" {
				t.Errorf("sign inputs: %v %v", e.Front, e.Lines)
			}
			e.SetCancelled(cancel)
			e.Lines[1] = plugin.Plain("edited")
		})
	})
	d := servertest.DigTarget
	text := func() [4]plugin.Text {
		var lines [4]plugin.Text
		host.Scheduler().RunTask(func() {
			be, _ := host.Overworld().BlockEntity(plugin.BlockPos{X: int(d[0]), Y: int(d[1]), Z: int(d[2])})
			lines = be.(plugin.Sign).Lines(true)
		})
		servertest.RunTicks(t, srv, 1)
		return lines
	}
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	c.Settle()
	c.SendSignUpdate(d[0], d[1], d[2], true, [4]string{"typed", "", "", ""})
	fired(t, srv, c, "sign change", &changed, 1)
	if lines := text(); changed != 1 || lines[0].PlainString() != "typed" || lines[1].PlainString() != "edited" {
		t.Fatalf("edited sign: events=%d lines=%v", changed, lines)
	}
}

// vanilla: DispenserBlock.dispenseFrom
func TestPluginBlockDispenseCancelAndEdit(t *testing.T) {
	var fires int
	var cancel, toArrow bool
	srv, host, _ := blockScene(t, "minecraft:stick", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockDispenseEvent) {
			fires++
			if e.Block.Pos != (plugin.BlockPos{X: 5, Y: 64, Z: 4}) || e.Item.Item != registry.ItemDirt || e.Velocity != (plugin.Vec3{Z: 1}) {
				t.Errorf("dispense inputs: %v %v %v", e.Block.Pos, e.Item.Item, e.Velocity)
			}
			e.SetCancelled(cancel)
			if toArrow {
				e.Item = plugin.ItemStack{Item: registry.ItemArrow, Count: 1}
			}
		})
	})
	host.Scheduler().RunTask(func() {
		host.Overworld().SetBlockState(plugin.BlockPos{X: 5, Y: 64, Z: 4}, plugin.BlockState(facing(t, registry.BlockDispenser, "south")), plugin.UpdateAll)
	})
	servertest.RunTicks(t, srv, 1)
	dispense := func() {
		if !srv.SetInventoryAt(5, 64, 4, 0, servertest.ItemStack(t, "minecraft:dirt", 2)) {
			t.Fatal("missing dispenser")
		}
		srv.Dispense(5, 64, 4, protocol.FaceSouth)
	}

	cancel = true
	dispense()
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Count != 2 || srv.ItemCount() != 0 {
		t.Fatalf("cancelled: slot=%+v items=%d, want the dirt back and nothing dropped", got, srv.ItemCount())
	}

	cancel, toArrow = false, true
	dispense()
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Count != 1 || srv.ArrowCount() != 1 || srv.ItemCount() != 0 {
		t.Fatalf("edited: slot=%+v arrows=%d items=%d, want one dirt taken and an arrow shot", got, srv.ArrowCount(), srv.ItemCount())
	}
	if fires != 2 {
		t.Fatalf("fires=%d, want 2", fires)
	}
}

// vanilla: BlockBehaviour.onDestroy drops through Block.dropResources
func TestPluginBlockDropItemEdit(t *testing.T) {
	var drops int
	srv, _, c := blockScene(t, "minecraft:diamond_pickaxe", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockDropItemEvent) {
			drops++
			if e.Player.Name() != "Miner" || e.State != plugin.DefaultState(registry.BlockStone) || len(e.Items) != 1 || e.Items[0].Item != registry.ItemCobblestone {
				t.Errorf("drop inputs: %v %v", e.State, e.Items)
			}
			e.Items = append(e.Items, plugin.ItemStack{Item: registry.ItemDiamond, Count: 1})
		})
	})
	d := servertest.DigTarget
	c.StartDigging(d[0], d[1], d[2])
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.StopDigging(d[0], d[1], d[2])
	c.Settle()
	servertest.RunTicks(t, srv, 12)
	if drops != 1 || srv.ItemCount() != 2 {
		t.Fatalf("drops=%d items=%d, want 1 and 2", drops, srv.ItemCount())
	}
}

// vanilla: BellBlock.useWithoutItem, BellBlock.attemptToRing
func TestPluginBellRingCancel(t *testing.T) {
	var rings int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stick", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BellRingEvent) {
			rings++
			if e.Block.Pos != (plugin.BlockPos{X: 2, Y: 64}) || e.Entity == nil {
				t.Errorf("bell inputs: %v %v", e.Block.Pos, e.Entity)
			}
			e.SetCancelled(cancel)
		})
	})
	d := servertest.DigTarget
	srv.SetBlock(d[0], d[1], d[2], facing(t, registry.BlockBell, "north"))
	for i, cancel2 := range []bool{true, false} {
		cancel = cancel2
		c.PlaceBlock(d[0], d[1], d[2], protocol.FaceNorth)
		ids := fired(t, srv, c, "bell ring", &rings, i+1)
		if slices.Contains(ids, protocol.IDPlayBlockAction) == cancel2 {
			t.Errorf("cancel=%v: block action sent=%v", cancel2, !cancel2)
		}
	}
}

// vanilla: FlintAndSteelItem.useOn
func TestPluginBlockIgniteCancel(t *testing.T) {
	var ignites int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:flint_and_steel", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.BlockIgniteEvent) {
			ignites++
			if e.Cause != plugin.IgniteFlintAndSteel || e.Player == nil || e.Block.Pos != (plugin.BlockPos{X: 2, Y: 64}) {
				t.Errorf("ignite inputs: %v %v %v", e.Cause, e.Player, e.Block.Pos)
			}
			e.SetCancelled(cancel)
		})
	})
	d := servertest.DigTarget
	cancel = true
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	fired(t, srv, c, "cancelled ignite", &ignites, 1)
	if srv.BlockAt(d[0], d[1], d[2]) != 0 || srv.InventorySlot("Miner", player.HotbarSlot).Damage != 0 {
		t.Fatal("cancelled ignite still lit or wore the tool")
	}
	cancel = false
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	fired(t, srv, c, "ignite", &ignites, 2)
	if srv.BlockAt(d[0], d[1], d[2]) == 0 {
		t.Fatal("fire was not placed")
	}
}

func facing(t *testing.T, b registry.Block, dir string) uint16 {
	t.Helper()
	state, ok := registry.WithProp(b.DefaultState(), registry.PropFacing, dir)
	if !ok {
		t.Fatalf("%v has no facing", b)
	}
	return state
}
