package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

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
