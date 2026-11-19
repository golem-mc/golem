package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func TestASpectatorUnderALowCeilingKeepsTheStandingPose(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Spectator", servertest.OpsListing(t, "Spectator"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("gamemode spectator")
	c.MoveToGround(5.5, 64.84375, 5.5, false)
	c.Settle()
	if srv.PlayerGameMode("Spectator") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator did not apply")
	}
	srv.SetBlock(5, 66, 5, registry.MustState("minecraft:stone", nil))
	servertest.RunTicks(t, srv, 5)
	if got := srv.PlayerPose("Spectator"); got != protocol.PoseStanding {
		t.Fatalf("spectator pose under the ceiling = %d, want standing (%d)", got, protocol.PoseStanding)
	}
}

// TestASpectatorFiresNoArrowFromADrawnBow: a bow drawn in survival is dropped
// when an operator switches the archer to spectator, so the release-use
// Player Action that follows fires nothing and spends no arrow. The draw is
// counted out with the loop stopped, so the switch cannot race a tick.
func TestASpectatorFiresNoArrowFromADrawnBow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Operator"))

	op := connect().Join("Operator")

	archer := connect()
	archer.JoinAs("Archer")
	servertest.GiveBowAndArrows(srv, "Archer")
	archer.MoveTo(0.5, 64, 0.5, -90, 0)

	// The draw counted out in hand-driven ticks, as TestADrawnBowFiresAnArrow
	// does, with the loop stopped so nothing ticks between the switch and the
	// release.
	srv.StopTick()
	servertest.DrawBow(archer, -90, 0)
	archer.Settle()
	for i := 0; srv.BowTicks("Archer") < 15; i++ {
		if i == 15 {
			t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks("Archer"))
		}
		srv.TickSurvival()
	}

	op.ChatCommand("gamemode spectator Archer")
	op.Settle()
	if srv.PlayerGameMode("Archer") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	if got := srv.BowTicks("Archer"); got != 0 {
		t.Errorf("the draw runs %d ticks after the switch, want it dropped", got)
	}

	archer.ReleaseUseItem()
	archer.ExpectNoArrowish()
}

// TestASpectatorDropsAMealInProgress: the meal a hungry survival player
// started is called off the moment an operator switches them to spectator,
// before the next tick would have, with the food untouched.
func TestASpectatorDropsAMealInProgress(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Operator"))

	op := connect().Join("Operator")

	eater := connect().Join("Eater")
	srv.SetFood("Eater", 10)
	bread := servertest.MustItem("minecraft:bread")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: bread, Count: 1})

	srv.StopTick()
	eater.UseItemRightClicks(protocol.HandMain)
	eater.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins with the Use Item packet")
	}

	op.ChatCommand("gamemode spectator Eater")
	op.Settle()
	if srv.PlayerGameMode("Eater") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	if srv.Eating("Eater") {
		t.Error("the meal is still in progress after the switch to spectator")
	}
	if kept := srv.InventorySlot("Eater", player.HotbarSlot); kept.ItemID != bread || kept.Count != 1 {
		t.Errorf("the bread after the switch = %+v, want it still whole in the hand", kept)
	}
}

// TestASpectatorLeavesTheBoatTheyRode: a boat's rider is put off when an
// operator switches them to spectator, so no spectator stays in a seat.
func TestASpectatorLeavesTheBoatTheyRode(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Operator"))

	op := connect().Join("Operator")

	rower := connect().Join("Rower")
	rower.MoveToGround(0.5, 64, 0.5, true)
	rower.Settle()

	boatID := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_boat")
	rower.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(rower, boatID); len(riders) != 1 || riders[0] != rower.PlayerEntityID {
		t.Fatalf("the rider boarded as %v, want the clicker's own %d", riders, rower.PlayerEntityID)
	}

	op.ChatCommand("gamemode spectator Rower")
	op.Settle()
	if _, ridden := srv.BoatRider(boatID); ridden {
		t.Fatal("the switch to spectator puts the rider off the boat")
	}
	if srv.PlayerGameMode("Rower") != protocol.GameModeSpectator {
		t.Errorf("game mode after the switch = %d, want spectator", srv.PlayerGameMode("Rower"))
	}
	if riders := srv.BoatPassengerIDs(boatID); len(riders) != 0 {
		t.Errorf("boat passengers after the switch = %v, want none", riders)
	}
	// The step leaves them on the ground beside the hull, not still at the
	// seat: vanilla's setGameMode rides the same stopRiding as a sneak off.
	if pos := srv.PlayerPosition("Rower"); math.Abs(pos.Y-64) > 0.001 {
		t.Errorf("the spectator-rider stands at y=%v, want the ground's top 64", pos.Y)
	} else if math.Abs(pos.X-2.5) < 1e-9 && math.Abs(pos.Z-0.5) < 1e-9 {
		t.Error("the spectator-rider stands where the hull was, want the step out beside it")
	}
}

// TestASpectatorIsAlwaysInvisibleToOtherPlayers checks the shared flags another
// player draws a spectator with: the invisible bit is set with no effect to
// explain it, clearing invisibility leaves it set, and a switch back to
// survival returns the bit to following that effect.
func TestASpectatorIsAlwaysInvisibleToOtherPlayers(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost"))

	watcher := connect().Join("Watcher")

	ghost := connect().Join("Ghost")
	entityID := watcher.ExpectPlayerAdded("Ghost")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Ghost")

	ghost.ChatCommand("gamemode spectator")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible == 0 {
		t.Errorf("entity flags = %#02x for a spectator with no effects, want the invisible bit", got)
	}

	// Invisibility comes and goes without the bit clearing. The sneak
	// carries the flags byte again, so the bit the spectator keeps rides out
	// with it.
	ghost.ChatCommand("effect give @s minecraft:invisibility 100 0")
	ghost.AwaitMobEffect()
	ghost.ChatCommand("effect clear @s minecraft:invisibility")
	ghost.AwaitRemoveMobEffect()
	ghost.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible == 0 {
		t.Errorf("entity flags = %#02x after clearing invisibility, want the invisible bit still set", got)
	}

	ghost.ChatCommand("gamemode survival")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSurvival {
		t.Fatal("the switch back to survival applies")
	}
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible != 0 {
		t.Errorf("entity flags = %#02x back in survival, want the invisible bit clear", got)
	}
}

// TestASpectatorIsSentWithNoEffectParticles checks that the switch to
// spectator clears the effect particles another player draws on the body,
// whatever effects it still carries.
func TestASpectatorIsSentWithNoEffectParticles(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost"))

	watcher := connect().Join("Watcher")

	ghost := connect().Join("Ghost")
	entityID := watcher.ExpectPlayerAdded("Ghost")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Ghost")

	srv.GiveEffect("Ghost", "minecraft:invisibility", 1200, 0, true)
	ghost.AwaitMobEffect()
	if got := watcher.AwaitEffectParticles(entityID); len(got) == 0 {
		t.Fatalf("particles before the switch = %v, want the invisibility cloud", got)
	}

	ghost.ChatCommand("gamemode spectator")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	if got := watcher.AwaitEffectParticles(entityID); len(got) != 0 {
		t.Errorf("particles as a spectator = %v, want them cleared", got)
	}
}

// TestASpectatorIsInvisibleToALateJoin checks the spawn metadata a spectator
// who joins after another spectator already carries for it: the invisible bit
// is set with no effect to explain it. Only a spectator is shown the body.
func TestASpectatorIsInvisibleToALateJoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "spectator"
	_, connect := servertest.Start(t, cfg)

	ghost := connect().Join("Ghost")
	ghost.Settle()

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	if got := watcher.EnterWorldSeeingPlayerFlags(); got&protocol.EntityFlagInvisible == 0 {
		t.Errorf("spawn flags = %#02x for the late watcher, want the invisible bit", got)
	}
}

// TestASpectatorOpensAMenuBlock checks that a spectator's use of a menu block
// shows its window: vanilla gives a spectator the block's menu and runs none
// of the block's own use, so the crafting table, the chest and the furnace all
// send Open Screen.
// vanilla: ServerPlayerGameMode.useItemOn
func TestASpectatorOpensAMenuBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "spectator"
	srv, w := servertest.OpenWindowed(t, cfg)

	table, ok := registry.FindState("minecraft:crafting_table", nil)
	if !ok {
		t.Fatal("the crafting table has no state")
	}
	srv.SetBlock(2, 64, 0, table)
	if !srv.PlaceContainer(3, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("the furnace would not stand")
	}

	w.UseBlock(2, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 2, 64, 0)

	w.UseBlock(3, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)

	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
}

// TestASpectatorOpensNoWindowWhereTheProviderAnswersNone checks the menu
// blocks whose getMenuProvider leaves a spectator nothing: the ender chest's
// entity names no provider, a chest with a conductor over its lid returns
// none, and a container still holding its unrolled loot table refuses a
// spectator before its menu is built.
// vanilla: ServerPlayerGameMode.useItemOn, EnderChestBlock,
// ChestBlock.getMenuProvider, RandomizableContainerBlockEntity.canOpen
func TestASpectatorOpensNoWindowWhereTheProviderAnswersNone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "spectator"
	srv, w := servertest.OpenWindowedOps(t, cfg)

	if !srv.SetBlock(2, 64, 0, registry.BlockStates["minecraft:ender_chest"]) {
		t.Fatal("the ender chest's chunk is not loaded")
	}
	w.UseBlock(2, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a spectator opened %d windows at an ender chest, want none", opened)
	}

	if !srv.PlaceContainer(3, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	if !srv.SetBlock(3, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}
	w.UseBlock(3, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a spectator opened %d windows at a blocked chest, want none", opened)
	}

	w.ChatCommand(`setblock 4 64 0 minecraft:chest{LootTable:"minecraft:chests/simple_dungeon"}`)
	w.Drain()
	w.UseBlock(4, 64, 0)
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayOpenScreen:
			t.Error("a spectator opened the loot chest, want no window")
		case protocol.IDPlaySystemChat:
			t.Error("the loot chest sent a spectator a message, want none")
		}
	})

	// The table lives on the barrel's own entity, not a chest half.
	w.ChatCommand(`setblock 5 64 0 minecraft:barrel{LootTable:"minecraft:chests/simple_dungeon"}`)
	w.Drain()
	w.UseBlock(5, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a spectator opened %d windows at a loot barrel, want none", opened)
	}
}

// TestASpectatorsOpenPlaysNoSoundOrLidAction checks the lid a spectator's open
// must not move: the spectator counts as no viewer, so neither the opening
// client nor another player hears the chest's sound or receives its lid block
// action.
// vanilla: ChestBlockEntity.startOpen, ChestBlockEntity.stopOpen
func TestASpectatorsOpenPlaysNoSoundOrLidAction(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Ghost"))

	ghost := servertest.WindowedOf(t, "Ghost", connect())
	if !srv.PlaceContainer(3, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	bob := servertest.WindowedOf(t, "Bob", connect())

	ghost.ChatCommand("gamemode spectator")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}

	// The spectator's window still opens ...
	ghost.UseBlock(3, 64, 0)
	ghost.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
	// ... with no sound and no lid action for the spectator ...
	ghost.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect || id == protocol.IDPlayBlockAction {
			t.Errorf("the spectator's open sent packet 0x%02X to the spectator, want none", id)
		}
	})
	// ... and nothing reaching another player either.
	bob.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			t.Error("another player heard a sound for a spectator's open")
		case protocol.IDPlayBlockAction:
			t.Error("another player received a lid action for a spectator's open")
		}
	})
}

// TestASpectatorsOpenDoesNotCountTowardsAChestsLid checks the opener count a
// survival player's open is measured by leaves out a spectator already looking
// in: the survival player still hears the chest open and reads one opener, and
// the lid falls with his close even though the spectator's window stays open.
// vanilla: ChestBlockEntity.startOpen, ChestBlockEntity.stopOpen
func TestASpectatorsOpenDoesNotCountTowardsAChestsLid(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Ghost"))

	ghost := servertest.WindowedOf(t, "Ghost", connect())
	if !srv.PlaceContainer(3, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	bob := servertest.WindowedOf(t, "Bob", connect())

	ghost.ChatCommand("gamemode spectator")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}

	// The spectator looks in first: his open moves no lid and plays no sound.
	ghost.UseBlock(3, 64, 0)
	ghost.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)

	// The survival player opens the same chest. He is the only opener, so the
	// lid creaks and reads one viewer for both of them.
	bob.UseBlock(3, 64, 0)
	if name, _, _, _ := bob.AwaitChestSound(); name != "minecraft:block.chest.open" {
		t.Errorf("the survival player's open played %q, want minecraft:block.chest.open", name)
	}
	bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 3, 64, 0)
	bob.ExpectLid(3, 64, 0, 1)
	ghost.ExpectLid(3, 64, 0, 1)

	// His close falls the lid: the spectator's open window counts as none.
	bob.CloseWindow()
	bob.ExpectWindowZero()
	if name, _, _, _ := bob.AwaitChestSound(); name != "minecraft:block.chest.close" {
		t.Errorf("the survival player's close played %q, want minecraft:block.chest.close", name)
	}
	bob.ExpectLid(3, 64, 0, 0)
}
