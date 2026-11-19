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
)

// vanilla: ServerGamePacketListenerImpl.handlePlayerInput, handlePlayerCommand
func TestPluginToggleSneakAndSprintCancel(t *testing.T) {
	var sneaks, sprints int
	var lastSneak, lastSprint, cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerToggleSneakEvent) {
			sneaks++
			lastSneak = e.On
			e.SetCancelled(cancel)
		})
		plugin.Listen(s, func(e *plugin.PlayerToggleSprintEvent) {
			sprints++
			lastSprint = e.On
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.SendSneak(true)
	fired(t, srv, c, "sneak event", &sneaks, 1)
	c.SendSprint(0, true)
	fired(t, srv, c, "sprint event", &sprints, 1)
	if !lastSneak || !lastSprint {
		t.Fatalf("On = %v, %v, want true", lastSneak, lastSprint)
	}
	// The cancelled change did not happen, so the same packet asks again.
	c.SendSneak(true)
	fired(t, srv, c, "second sneak event", &sneaks, 2)
	cancel = false
	c.SendSneak(true)
	fired(t, srv, c, "allowed sneak event", &sneaks, 3)
	// Sneaking now, so repeating it is no change and fires nothing.
	c.SendSneak(true)
	c.Settle()
	if sneaks != 3 {
		t.Fatalf("sneak events = %d, want 3", sneaks)
	}
}

// vanilla: ServerGamePacketListenerImpl.handleSetCarriedItem
func TestPluginItemHeldCancelRestoresSlot(t *testing.T) {
	var held int
	var prev, next int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerItemHeldEvent) {
			held++
			prev, next = e.Previous, e.New
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.SwapHotbarSlot(3)
	ids := fired(t, srv, c, "held event", &held, 1)
	if prev != 0 || next != 3 {
		t.Fatalf("previous %d new %d, want 0 and 3", prev, next)
	}
	if !slices.Contains(ids, protocol.IDPlayHeldItem) {
		t.Error("the client's selection was not put back")
	}
	cancel = false
	c.SwapHotbarSlot(3)
	ids = fired(t, srv, c, "allowed held event", &held, 2)
	if slices.Contains(ids, protocol.IDPlayHeldItem) {
		t.Error("an allowed change was answered with a held item")
	}
}

// vanilla: ServerGamePacketListenerImpl.handlePlayerAction SWAP_ITEM_WITH_OFFHAND
func TestPluginSwapHandItemsCancelAndChange(t *testing.T) {
	var swaps int
	var cancel, replace bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerSwapHandItemsEvent) {
			swaps++
			if replace {
				e.OffHand = plugin.NewStack(e.OffHand.Item, 5)
			}
			e.SetCancelled(cancel)
		})
	})
	stone := servertest.MustItem("minecraft:stone")
	cancel = true
	c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	fired(t, srv, c, "swap event", &swaps, 1)
	if srv.InventorySlot("Miner", player.HotbarSlot).ItemID != stone || !srv.InventorySlot("Miner", player.OffHandSlot).Empty() {
		t.Fatal("a cancelled swap moved the items")
	}
	cancel, replace = false, true
	c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	fired(t, srv, c, "second swap event", &swaps, 2)
	if got := srv.InventorySlot("Miner", player.OffHandSlot); got.ItemID != stone || got.Count != 5 {
		t.Fatalf("off hand = %+v, want 5 stone", got)
	}
	if !srv.InventorySlot("Miner", player.HotbarSlot).Empty() {
		t.Fatal("main hand kept its item")
	}
}

// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
func TestPluginInteractRightClickBlockCancelAndDeny(t *testing.T) {
	var clicks int
	var cancel, denyItem, denyBlock bool
	srv, _, c := blockScene(t, "minecraft:cobblestone", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerInteractEvent) {
			clicks++
			if e.Action != plugin.RightClickBlock || e.Block == nil || e.Block.Pos != (plugin.BlockPos{X: 2, Y: 64}) ||
				e.Face != plugin.Up || e.Hand != plugin.MainHand || e.Item.Count != 1 {
				t.Errorf("interact inputs: %v %v %v %v", e.Action, e.Block, e.Face, e.Item)
			}
			e.SetCancelled(cancel)
			e.DenyItem, e.DenyBlock = denyItem, denyBlock
		})
	})
	d := servertest.DigTarget
	above := srv.BlockAt(d[0], d[1]+1, d[2])

	cancel = true
	c.PlaceBlock(d[0], d[1], d[2], protocol.FaceTop)
	ids := fired(t, srv, c, "cancelled click", &clicks, 1)
	resent(t, ids)
	if !slices.Contains(ids, protocol.IDPlayContainerSlot) {
		t.Error("a cancelled click left the client's predicted stack count")
	}
	if srv.BlockAt(d[0], d[1]+1, d[2]) != above {
		t.Fatal("a cancelled click placed a block")
	}

	cancel, denyItem = false, true
	c.PlaceBlock(d[0], d[1], d[2], protocol.FaceTop)
	ids = fired(t, srv, c, "item-denied click", &clicks, 2)
	if !slices.Contains(ids, protocol.IDPlayContainerSlot) {
		t.Error("a denied item use left the client's predicted stack count")
	}
	if srv.BlockAt(d[0], d[1]+1, d[2]) != above {
		t.Fatal("DenyItem still placed a block")
	}

	denyItem = false
	c.PlaceBlock(d[0], d[1], d[2], protocol.FaceTop)
	fired(t, srv, c, "allowed click", &clicks, 3)
	if srv.BlockAt(d[0], d[1]+1, d[2]) == above {
		t.Fatal("an allowed click placed nothing")
	}
}

// vanilla: ServerPlayerGameMode.useItemOn
func TestPluginInteractDenyBlockKeepsItemUse(t *testing.T) {
	var clicks int
	srv, _, c := blockScene(t, "minecraft:cobblestone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerInteractEvent) {
			clicks++
			e.DenyBlock = true
		})
	})
	d := servertest.DigTarget
	srv.SetBlock(d[0], d[1], d[2], servertest.BlockState(t, "minecraft:crafting_table"))
	c.PlaceBlock(d[0], d[1], d[2], protocol.FaceTop)
	fired(t, srv, c, "click", &clicks, 1)
	// The table did not open, so the cobblestone went on top of it.
	if srv.BlockAt(d[0], d[1]+1, d[2]) != servertest.BlockState(t, "minecraft:cobblestone") {
		t.Fatal("DenyBlock did not leave the item's use to place")
	}
}

// vanilla: ServerGamePacketListenerImpl.handleUseItem
func TestPluginInteractRightClickAirCancel(t *testing.T) {
	var clicks int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerInteractEvent) {
			clicks++
			if e.Action != plugin.RightClickAir || e.Block != nil {
				t.Errorf("air click: %v %v", e.Action, e.Block)
			}
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.UseItemRightClicks(protocol.HandMain)
	fired(t, srv, c, "air click", &clicks, 1)
	cancel = false
	c.UseItemRightClicks(protocol.HandMain)
	fired(t, srv, c, "allowed air click", &clicks, 2)
}

// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func TestPluginMoveCancelSendsPlayerBack(t *testing.T) {
	var moves int
	var cancel bool
	var from, to plugin.Vec3
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerMoveEvent) {
			moves++
			from, to = e.From.Pos, e.To.Pos
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.SendPosition(0.7, 64, 0.5, true)
	ids := fired(t, srv, c, "move event", &moves, 1)
	if !slices.Contains(ids, protocol.IDPlaySyncPosition) {
		t.Error("a cancelled move was not corrected")
	}
	if to.X != 0.7 || from.X != 0.5 {
		t.Fatalf("from %v to %v", from, to)
	}
	if p := srv.PlayerPosition("Miner"); p.X != 0.5 {
		t.Fatalf("a cancelled move changed the position to %v", p)
	}
}

// vanilla: Entity.teleportTo
func TestPluginTeleportEventCancelAndRedirect(t *testing.T) {
	var teleports int
	var mode string
	var cause plugin.TeleportCause
	srv, host, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTeleportEvent) {
			teleports++
			cause = e.Cause
			switch mode {
			case "cancel":
				e.SetCancelled(true)
			case "redirect":
				e.To.Pos.X = 5
			}
		})
	})
	p := host.Players()[0]
	to := plugin.Location{World: p.Location().World, Pos: plugin.Vec3{X: 3, Y: 64, Z: 0.5}}
	mode = "cancel"
	onTick(t, srv, host, func() { p.Teleport(to, plugin.TeleportPlugin) })
	if got := srv.PlayerPosition("Miner"); got.X != 0.5 {
		t.Fatalf("cancelled teleport moved to %v", got)
	}
	mode = "redirect"
	onTick(t, srv, host, func() { p.Teleport(to, plugin.TeleportPlugin) })
	if got := srv.PlayerPosition("Miner"); got.X != 5 || cause != plugin.TeleportPlugin || teleports != 2 {
		t.Fatalf("redirected teleport: %v cause %v events %d", got, cause, teleports)
	}
}

// vanilla: ServerPlayer.setGameMode
func TestPluginGameModeChangeCancel(t *testing.T) {
	var changes int
	var cancel bool
	srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(), []plugin.Registration{{Name: "modes", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerGameModeChangeEvent) {
			changes++
			if e.New != plugin.Creative {
				t.Errorf("new mode %v", e.New)
			}
			e.SetCancelled(cancel)
		})
		return nil
	}}}}, servertest.OpsListing(t, "Miner"))
	c := servertest.DialServer(t, srv)
	c.Join("Miner")
	c.Settle()
	servertest.HoldTicks(t, srv)
	cancel = true
	c.SendChangeGameMode(protocol.GameModeCreative)
	fired(t, srv, c, "game mode event", &changes, 1)
	if srv.PlayerGameMode("Miner") != protocol.GameModeSurvival {
		t.Fatal("a cancelled change applied")
	}
	cancel = false
	c.SendChangeGameMode(protocol.GameModeCreative)
	fired(t, srv, c, "second game mode event", &changes, 2)
	if srv.PlayerGameMode("Miner") != protocol.GameModeCreative {
		t.Fatalf("mode %d", srv.PlayerGameMode("Miner"))
	}
}

// vanilla: ServerGamePacketListenerImpl.handleAttack, handleInteract
func TestPluginAttackAndInteractEntityCancel(t *testing.T) {
	var attacks, uses int
	var cancel bool
	var target string
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerAttackEvent) {
			attacks++
			target = e.Target.(plugin.Player).Name()
			e.SetCancelled(cancel)
		})
		plugin.Listen(s, func(e *plugin.PlayerInteractEntityEvent) {
			uses++
			e.SetCancelled(cancel)
		})
	})
	other := servertest.DialServer(t, srv)
	id := other.JoinAs("Victim")
	other.Settle()
	hp := func() float64 { return srv.PlayerHealth("Victim") }
	before := hp()
	cancel = true
	c.Attack(id)
	fired(t, srv, c, "attack event", &attacks, 1)
	c.UseOn(id)
	fired(t, srv, c, "interact event", &uses, 1)
	if target != "Victim" || hp() != before {
		t.Fatalf("target %q, health %v -> %v", target, before, hp())
	}
	cancel = false
	c.Attack(id)
	fired(t, srv, c, "second attack event", &attacks, 2)
}

// onTick runs fn as a plugin task on the next tick.
func onTick(t *testing.T, srv *server.Server, host plugin.Server, fn func()) {
	t.Helper()
	host.Scheduler().RunTask(fn)
	servertest.RunTicks(t, srv, 1)
}

// vanilla: PlayerList.respawn
func TestPluginRespawnEventMovesPlayer(t *testing.T) {
	var respawns int
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerRespawnEvent) {
			respawns++
			if e.Bed || e.Anchor {
				t.Errorf("no spawn point was set: bed %v anchor %v", e.Bed, e.Anchor)
			}
			e.Location.Pos.X = 9.5
		})
	})
	if !srv.Hurt("Miner", 50) {
		t.Fatal("lethal hurt refused")
	}
	c.PerformRespawn()
	// The listener runs on a tick, once the connection has read the packet.
	for i := 0; respawns < 1; i++ {
		if i == 400 {
			t.Fatal("no respawn event")
		}
		servertest.RunTicks(t, srv, 1)
		time.Sleep(5 * time.Millisecond)
	}
	pos, noBlock := c.ExpectRespawnThroughTeleport()
	if noBlock || respawns != 1 || pos.X != 9.5 {
		t.Fatalf("respawn at %v (no-block %v), %d events", pos, noBlock, respawns)
	}
}

// vanilla: ServerGamePacketListenerImpl.handleAnimate
func TestPluginAnimationCancelKeepsSwingOff(t *testing.T) {
	var swings int
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerAnimationEvent) {
			swings++
			e.SetCancelled(true)
		})
	})
	c.Send(protocol.IDPlayPunch, func(*protocol.Buffer) {})
	fired(t, srv, c, "animation event", &swings, 1)
}
