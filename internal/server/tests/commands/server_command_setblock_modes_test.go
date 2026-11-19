package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// setblockActor joins an operator in creative, the scene the /setblock mode
// tests lay out.
func setblockActor(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	return srv, c
}

// TestSetblockDestroyBreaksTheBlockItReplaces checks the destroy mode: the
// block the command replaces breaks the way a player's break does, so its
// break effect plays and its loot lands on the ground even though the
// command then leaves the cell air.
func TestSetblockDestroyBreaksTheBlockItReplaces(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	cobble, ok := registry.ItemID("minecraft:cobblestone")
	if !ok {
		t.Fatal("the item registry has no cobblestone")
	}

	c.ChatCommand("setblock 5 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, stone)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 air destroy")
	got := c.ReadBreak(5, 64, 3, func(b servertest.BreakWire) bool {
		return b.Air && len(b.Drops) == 1
	})
	if got.Events[protocol.WorldEventBlockBreak] == 0 {
		t.Error("the destroy played no break effect")
	}
	if len(got.Drops) != 1 || got.Drops[0].ItemID != cobble {
		t.Errorf("the destroy dropped %+v, want one cobblestone", got.Drops)
	}
	if block := srv.BlockAt(5, 64, 3); block != 0 {
		t.Errorf("block the destroy broke = %d, want 0 (air)", block)
	}
	c.Disconnect()
}

// TestSetblockAirDestroyAnswersSuccess checks a destroy that leaves the
// break's own air: the command's block is air and the break left air, so the
// placement is skipped and the command answers success rather than failing on
// a write that changed nothing.
func TestSetblockAirDestroyAnswersSuccess(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}

	c.ChatCommand("setblock 5 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, stone)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 air destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "64", "3")
	if block := srv.BlockAt(5, 64, 3); block != 0 {
		t.Errorf("block the destroy left = %d, want 0 (air)", block)
	}
	c.Disconnect()
}

// TestSetblockDestroySpillsABrokenContainersContents checks that destroy's
// break of a container cell spills what the chest held as well as dropping its
// own loot, before the command's block takes the cell.
func TestSetblockDestroySpillsABrokenContainersContents(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	if !srv.PlaceContainer(5, 70, 5, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.SetInventoryAt(5, 70, 5, 0, servertest.ItemStack(t, "minecraft:diamond", 3)) {
		t.Fatal("the chest would not take the diamonds")
	}

	c.ChatCommand("setblock 5 70 5 stone destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "70", "5")

	if got := fillDropTotal(srv, "minecraft:chest"); got != 1 {
		t.Errorf("the destroy dropped %d chests, want the one it broke", got)
	}
	if got := fillDropTotal(srv, "minecraft:diamond"); got != 3 {
		t.Errorf("the destroy dropped %d diamonds, want the 3 the chest held", got)
	}
	if got := srv.BlockAt(5, 70, 5); got != stone {
		t.Errorf("block the destroy placed = %d, want %d (stone)", got, stone)
	}
	c.Disconnect()
}

// TestSetblockDestroyPlacesTheNewBlock checks the other half of destroy: the
// break's loot lands and the command's own block then takes the cell the
// break left, answering success.
func TestSetblockDestroyPlacesTheNewBlock(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the registry has no dirt")
	}

	c.ChatCommand("setblock 5 64 3 dirt")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, dirt)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 stone destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "64", "3")
	if got := srv.BlockAt(5, 64, 3); got != stone {
		t.Errorf("block the destroy placed = %d, want %d (stone)", got, stone)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:dirt")
	c.Disconnect()
}
