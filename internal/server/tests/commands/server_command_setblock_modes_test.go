package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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

// TestSetblockAirDestroyOnAirAnswersSuccess checks the same skip with nothing
// to break: an air block destroyed into an air cell places nothing and still
// answers success.
func TestSetblockAirDestroyOnAirAnswersSuccess(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}

	c.ChatCommand("setblock 5 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, stone)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 air")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, 0)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 air destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "64", "3")
	if block := srv.BlockAt(5, 64, 3); block != 0 {
		t.Errorf("block the destroy left = %d, want 0 (air)", block)
	}
	c.Disconnect()
}

// TestSetblockAirVariantDestroyLeavesPlainAir checks that a destroy whose
// command block is cave air (or void air) over a broken block leaves the plain
// air the break left, not the variant the command named.
func TestSetblockAirVariantDestroyLeavesPlainAir(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	caveAir, ok := registry.FindState("minecraft:cave_air", nil)
	if !ok {
		t.Fatal("the registry has no cave air")
	}

	c.ChatCommand("setblock 5 64 3 stone")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 3, stone)
	drainPendingPackets(t, srv, c)

	c.ChatCommand("setblock 5 64 3 cave_air destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "64", "3")
	if block := srv.BlockAt(5, 64, 3); block != 0 {
		t.Errorf("block the destroy left = %d, want 0 (plain air), not %d (cave air)", block, caveAir)
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

// TestSetblockStrictLeavesTheHangingBlock checks the strict mode: a block the
// command puts in the world is not woken, so sand left hanging over air stays
// where the command wrote it and the cell sends no block update past the
// write's own.
func TestSetblockStrictLeavesTheHangingBlock(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	c.ChatCommand("setblock 5 66 3 sand strict")
	servertest.AwaitBlockUpdateSoon(t, c, 5, 66, 3, sand)
	c.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayBlockUpdate))
	if got := srv.BlockAt(5, 66, 3); got != sand {
		t.Errorf("strict setblock = %d, want %d (still hanging)", got, sand)
	}
	if got := srv.BlockAt(5, 64, 3); got == sand {
		t.Error("the strict sand fell to the ground")
	}
	c.Disconnect()
}

// TestSetblockAirDestroyWakesTheNeighbours checks the wake the destroy's skip
// still owes: the command's block is air over the air the break left, so the
// placement is skipped and the write changed nothing, but a non-strict set
// wakes the neighbours regardless — the lit lamp beside the cell has no signal
// of its own and goes dark only because the command's wake reaches it.
func TestSetblockAirDestroyWakesTheNeighbours(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	// The lamp stands lit and strict, so nothing but the destroy's wake can
	// turn it off before its four-tick delay runs out.
	servertest.Setblock(t, c, 5, 64, 6, "redstone_lamp[lit=true] strict")

	c.ChatCommand("setblock 6 64 6 air destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "6", "64", "6")

	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	servertest.ExpectBlockNamed(t, srv, 5, 64, 6, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestSetblockAirDestroySkipsTheNeighbourShapes checks that the skip's wake is
// the command's neighbour update alone, without the shape update the placement
// would have run: a fence keeps the east side it was placed with, because the
// removed cell is a neighbourChanged wake over air and a fence answers that by
// keeping its shape, not by dropping the side that pointed at the break.
// vanilla: SetBlockCommand.setBlock runs updateNeighboursOnBlockSet, not
// updateNeighbourShapes, when the place is skipped.
func TestSetblockAirDestroySkipsTheNeighbourShapes(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)

	servertest.Setblock(t, c, 5, 64, 6, "oak_fence[east=true] strict")

	c.ChatCommand("setblock 6 64 6 air destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "6", "64", "6")

	servertest.ExpectBlockNamed(t, srv, 5, 64, 6, "minecraft:oak_fence", map[string]string{"east": "true"})
	c.Disconnect()
}

// TestSpilledItemsStandWithTheirFeetInsideTheBlock breaks a chest of 27
// single items: every item's box centre sits half an item above a foot
// height in [70, 70.75), so no item starts inside the block below.
func TestSpilledItemsStandWithTheirFeetInsideTheBlock(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)
	srv.StopTick()

	if !srv.PlaceContainer(5, 70, 5, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	for slot := range 27 {
		if !srv.SetInventoryAt(5, 70, 5, slot, servertest.ItemStack(t, "minecraft:diamond", 1)) {
			t.Fatalf("the chest would not take slot %d", slot)
		}
	}

	c.ChatCommand("setblock 5 70 5 stone destroy")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "5", "70", "5")

	n := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item != "minecraft:diamond" {
			continue
		}
		n++
		if feet := it.Y - entity.ItemHalfSize; feet < 70 || feet >= 70.75 {
			t.Errorf("item feet at y=%v, want [70, 70.75)", feet)
		}
	}
	if n != 27 {
		t.Errorf("%d diamonds spilled, want 27", n)
	}
	c.Disconnect()
}
