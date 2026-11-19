package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// expectCloneFeedback checks the count feedback a clone that copied
// something sends, the number the count the command reported.
func expectCloneFeedback(t *testing.T, c *servertest.Client, count int64) {
	t.Helper()
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.clone.success", protocol.Text{Number: &count})
}

// TestCloneCopiesARegionToItsDestination checks the plain clone: a stone
// pair copies onto its destination corner - the blocks land, the feedback
// counts the cells - and a second clone of the same thing counts them all
// again, the repeats vanilla's barrier write makes of identical states.
func TestCloneCopiesARegionToItsDestination(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}
	if !srv.SetBlock(20, 70, 20, glowstone) || !srv.SetBlock(21, 70, 20, glowstone) {
		t.Fatal("the world did not take the glowstone")
	}

	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40")
	expectCloneFeedback(t, c, 2)
	for x := int32(40); x <= 41; x++ {
		if got := srv.BlockAt(x, 70, 40); got != glowstone {
			t.Errorf("block at (%d, 70, 40) = %d, want %d (glowstone)", x, got, glowstone)
		}
	}

	// The same clone again, onto a destination that already holds what it
	// copies, counts every cell once more.
	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40")
	expectCloneFeedback(t, c, 2)
	c.Disconnect()
}

// TestCloneRefusesWhatTheVanillaServerRefuses checks the refusals a clone
// answers with before anything is written: a destination that overlaps its
// source under the normal mode, a region over the cap, and a corner whose
// chunk is not loaded; and the refusal a masked clone that copied nothing
// answers with.
func TestCloneRefusesWhatTheVanillaServerRefuses(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// A destination that overlaps the source is refused unless the mode
	// allows it - force and move both do - whatever the region holds.
	c.ChatCommand("clone 10 70 10 11 70 10 10 70 10")
	servertest.AwaitCommandFailure(t, c, "commands.clone.overlap")

	// Thirty-three cubed is over the cap of 32768, and the refusal names
	// both the cap and the area, the area as a long.
	limit, area := int64(32768), int64(35937)
	c.ChatCommand("clone 0 80 0 32 112 32 60 80 60")
	var toobig protocol.Buffer
	toobig.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{
		Translate: "commands.clone.toobig",
		With:      []protocol.Text{{Number: &limit}, {Long: &area}},
	}}})
	toobig.PutBool(false)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, toobig.Bytes()) {
		t.Errorf("too-large clone error = % X, want % X", got, toobig.Bytes())
	}

	// A corner whose chunk is not loaded is a bare red refusal, and nothing
	// is cloned from the loaded side.
	c.ChatCommand("clone 2000 64 2000 2001 64 2001 0 70 0")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	if got := srv.BlockAt(0, 70, 0); got != 0 {
		t.Errorf("block at the loaded destination = %d, want air", got)
	}

	// A masked clone of an all-air region copies nothing and is refused.
	c.ChatCommand("clone 10 90 10 11 91 11 30 90 30 masked")
	servertest.AwaitCommandFailure(t, c, "commands.clone.failed")
	c.Disconnect()
}

// TestCloneRefusesABoxThatSpansUnloadedChunks checks the whole-box loaded
// check a clone runs once its corners have passed: two players stand far
// apart, both corners are loaded, and the region between them is not - the
// clone is refused with the unloaded position error, not run half-way.
func TestCloneRefusesABoxThatSpansUnloadedChunks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate", "Stander"))
	c := connect().Join("Testificate")

	// A second player keeps the chunks around x=900 loaded while the
	// runner's reach covers those around spawn, leaving the middle empty.
	far := connect().Join("Stander")
	c.ChatCommand("teleport Stander 900 70 0")
	// The far side's chunks load around its player once that client has
	// awoken its sender, which a move report does; the middle between
	// the two players stays unloaded.
	drainPendingPackets(t, srv, far)
	far.MoveTo(900.5, 70, 0.5, 0, 0)
	loaded := func() bool {
		return srv.ChunkLoadedInDimension(world.DimensionName, 890, 10) &&
			srv.ChunkLoadedInDimension(world.DimensionName, 900, 0)
	}
	for range 60 {
		if loaded() {
			break
		}
		// The runner's own connection has to be read too, or its keep-alive
		// exchange times out while this waits on the far side.
		drainPendingPackets(t, srv, c)
		drainPendingPackets(t, srv, far)
	}
	if !loaded() {
		t.Fatal("the far side's chunks never loaded")
	}

	// Both corners sit in loaded ground, the box between them does not.
	c.ChatCommand("clone 10 70 10 890 70 10 30 75 30")
	servertest.AwaitCommandFailure(t, c, "argument.pos.unloaded")
	if got := srv.BlockAt(30, 75, 30); got != 0 {
		t.Errorf("block at the destination = %d, want air: the clone was not run", got)
	}
	c.Disconnect()
	far.Disconnect()
}

// TestCloneModesShapeTheCopy checks the filtration modes: masked copies only
// the cells that hold something, leaving whatever the destination had where
// the source held air; a filtered clone copies only the cells its predicate
// picks - a block name, a property the block carries, or a tag it is in.
func TestCloneModesShapeTheCopy(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	glass, ok := registry.FindState("minecraft:glass", nil)
	if !ok {
		t.Fatal("the registry has no glass")
	}
	srv.SetBlock(20, 70, 20, stone)
	srv.SetBlock(21, 70, 20, stone)
	// The destination is floored with glass at both rows, so what the
	// masked clone leaves alone is visible.
	for x := int32(30); x <= 31; x++ {
		for z := int32(30); z <= 31; z++ {
			for y := int32(70); y <= 71; y++ {
				srv.SetBlock(x, y, z, glass)
			}
		}
	}

	// Masked copies the stone pair and leaves every glass that stood where
	// the source held air.
	c.ChatCommand("clone 20 70 20 21 70 21 30 70 30 masked")
	expectCloneFeedback(t, c, 2)
	for x := int32(30); x <= 31; x++ {
		z := int32(30)
		if want := stone; srv.BlockAt(x, 70, z) != want {
			t.Errorf("block at (%d, 70, 30) = %d, want %d (stone)", x, srv.BlockAt(x, 70, z), want)
		}
		for _, y := range []int32{71, 70} {
			if got := srv.BlockAt(x, y, 31); got != glass {
				t.Errorf("masked copy touched (%d, %d, 31): block = %d, want the glass it stood", x, y, got)
			}
		}
		if got := srv.BlockAt(x, 71, 30); got != glass {
			t.Errorf("masked copy touched (%d, 71, 30): block = %d, want the glass it stood", x, got)
		}
	}

	// A filtered clone of a predicate nothing matches copies nothing and
	// is refused; the stone-only predicate copies the pair.
	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40 filtered minecraft:glass")
	servertest.AwaitCommandFailure(t, c, "commands.clone.failed")
	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40 filtered minecraft:stone")
	expectCloneFeedback(t, c, 2)
	for x := int32(40); x <= 41; x++ {
		if got := srv.BlockAt(x, 70, 40); got != stone {
			t.Errorf("block at (%d, 70, 40) = %d, want %d (stone)", x, got, stone)
		}
	}

	// A property the predicate names picks only the states that carry it:
	// of two oak logs, only the axis=y one is copied.
	logY, ok := registry.FindState("minecraft:oak_log", map[string]string{"axis": "y"})
	if !ok {
		t.Fatal("the registry has no standing oak log")
	}
	logX, ok := registry.FindState("minecraft:oak_log", map[string]string{"axis": "x"})
	if !ok {
		t.Fatal("the registry has no lying oak log")
	}
	srv.SetBlock(20, 72, 20, logY)
	srv.SetBlock(20, 73, 20, logX)
	c.ChatCommand("clone 20 72 20 20 73 20 50 72 50 filtered minecraft:oak_log[axis=y]")
	expectCloneFeedback(t, c, 1)
	if got := srv.BlockAt(50, 72, 50); got != logY {
		t.Errorf("block at (50, 72, 50) = %d, want %d (the standing log)", got, logY)
	}
	if got := srv.BlockAt(50, 73, 50); got != 0 {
		t.Errorf("block at (50, 73, 50) = %d, want air: the lying log was not picked", got)
	}

	// A tag predicate picks every block in the tag: the birch log is
	// copied, the stone beside it is not.
	birch, ok := registry.FindState("minecraft:birch_log", nil)
	if !ok {
		t.Fatal("the registry has no birch log")
	}
	srv.SetBlock(24, 80, 24, birch)
	srv.SetBlock(25, 80, 24, stone)
	c.ChatCommand("clone 24 80 24 25 80 24 60 80 60 filtered #minecraft:logs")
	expectCloneFeedback(t, c, 1)
	if got := srv.BlockAt(60, 80, 60); got != birch {
		t.Errorf("block at (60, 80, 60) = %d, want %d (the birch log)", got, birch)
	}
	if got := srv.BlockAt(61, 80, 60); got != 0 {
		t.Errorf("block at (61, 80, 60) = %d, want air: the stone was not picked", got)
	}
	c.Disconnect()
}

// TestCloneMovesTheSourceAndForceOverlapsIt checks the clone modes: a move
// copies the region and leaves its source as air, force copies onto a
// destination that overlaps the source, and move itself may overlap too -
// what moved onto itself lands back where it stood.
func TestCloneMovesTheSourceAndForceOverlapsIt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	srv.SetBlock(20, 70, 20, stone)
	srv.SetBlock(21, 70, 20, stone)

	// Move onto itself is allowed, and the region lands back where it
	// stood: the source that was cleared is written over by its own copy.
	c.ChatCommand("clone 20 70 20 21 70 20 20 70 20 replace move")
	expectCloneFeedback(t, c, 2)
	for x := int32(20); x <= 21; x++ {
		if got := srv.BlockAt(x, 70, 20); got != stone {
			t.Errorf("move onto itself left (%d, 70, 20) = %d, want %d (stone)", x, got, stone)
		}
	}

	// A move elsewhere copies the pair and clears its source.
	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40 replace move")
	expectCloneFeedback(t, c, 2)
	for x := int32(40); x <= 41; x++ {
		if got := srv.BlockAt(x, 70, 40); got != stone {
			t.Errorf("block at (%d, 70, 40) = %d, want %d (stone)", x, got, stone)
		}
	}
	for x := int32(20); x <= 21; x++ {
		if got := srv.BlockAt(x, 70, 20); got != 0 {
			t.Errorf("the moved-from block at (%d, 70, 20) = %d, want air", x, got)
		}
	}

	// Normal mode refuses the overlapping destination that force accepts.
	c.ChatCommand("clone 40 70 40 41 70 40 41 70 40")
	servertest.AwaitCommandFailure(t, c, "commands.clone.overlap")
	c.ChatCommand("clone 40 70 40 41 70 40 41 70 40 replace force")
	expectCloneFeedback(t, c, 2)
	c.Disconnect()
}

// TestCloneCarriesTheBlockEntitiesOfItsCells checks what a clone does with
// a block's own data: a chest holding items copies with them, a masked move
// of it carries the items and leaves nothing standing behind.
func TestCloneCarriesTheBlockEntitiesOfItsCells(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	if !srv.PlaceContainer(5, 70, 5, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.SetInventoryAt(5, 70, 5, 0, servertest.ItemStack(t, "minecraft:diamond", 3)) {
		t.Fatal("the chest would not take the diamonds")
	}

	diamonds := servertest.ItemStack(t, "minecraft:diamond", 3)

	// A plain clone copies the chest with the diamonds it holds, and the
	// chest it copied keeps its own.
	c.ChatCommand("clone 5 70 5 5 70 5 8 70 8")
	expectCloneFeedback(t, c, 1)
	got := srv.InventoryAt(8, 70, 8)
	if len(got) == 0 || got[0].ItemID != diamonds.ItemID || got[0].Count != 3 {
		t.Errorf("the copied chest holds %+v, want its three diamonds", got)
	}
	if still := srv.InventoryAt(5, 70, 5); len(still) == 0 || still[0].ItemID != diamonds.ItemID || still[0].Count != 3 {
		t.Errorf("the chest that was copied holds %+v, want it untouched", still)
	}

	// A masked move carries the items and clears the source.
	c.ChatCommand("clone 8 70 8 8 70 8 11 70 11 masked move")
	expectCloneFeedback(t, c, 1)
	got = srv.InventoryAt(11, 70, 11)
	if len(got) == 0 || got[0].ItemID != diamonds.ItemID || got[0].Count != 3 {
		t.Errorf("the moved chest holds %+v, want its three diamonds", got)
	}
	if srv.HasBlockEntity(8, 70, 8) {
		t.Error("the moved-from chest kept a block entity")
	}
	if block := srv.BlockAt(8, 70, 8); block != 0 {
		t.Errorf("the moved-from block = %d, want air", block)
	}
	c.Disconnect()
}

// TestCloneStrictLeavesTheHangingCopy checks the strict mode: a copied block
// that nothing supports starts falling the way a placed one does - unless
// the clone was strict, which leaves the copy unwoken.
func TestCloneStrictLeavesTheHangingCopy(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	// The source sits on the ground; the copy lands in the air above it.
	if !srv.SetBlock(24, 64, 24, sand) {
		t.Fatal("the world did not take the sand")
	}

	// A plain clone's hanging copy falls to the ground, the way the block
	// the wake reaches does.
	c.ChatCommand("clone 24 64 24 24 64 24 26 90 26")
	expectCloneFeedback(t, c, 1)
	servertest.AwaitBlockUpdateSoon(t, c, 26, 90, 26, sand)
	servertest.UntilTicks(t, srv, 100, "block at (26, 64, 26)", func() bool { return srv.BlockAt(26, 64, 26) == sand })
	if got := srv.BlockAt(26, 90, 26); got != 0 {
		t.Errorf("block the sand fell from = %d, want air", got)
	}
	drainPendingPackets(t, srv, c)

	// A strict clone's copy hangs where it was written: its write is the
	// one Block Update a single cell sends, and no further one follows —
	// the wake a plain clone's copy gets that the strict one does not.
	c.ChatCommand("clone 24 64 24 24 64 24 28 90 28 strict")
	expectCloneFeedback(t, c, 1)
	servertest.AwaitBlockUpdateSoon(t, c, 28, 90, 28, sand)
	c.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayBlockUpdate))
	if got := srv.BlockAt(28, 90, 28); got != sand {
		t.Errorf("strict copy = %d, want %d (still hanging)", got, sand)
	}

	// A strict move clears the source and leaves the copy hanging too: no
	// further block update follows the one the copy's own write sends.
	c.ChatCommand("clone 24 64 24 24 64 24 30 90 30 strict replace move")
	expectCloneFeedback(t, c, 1)
	servertest.AwaitBlockUpdateSoon(t, c, 30, 90, 30, sand)
	drainPendingPackets(t, srv, c)
	if got := srv.BlockAt(30, 90, 30); got != sand {
		t.Errorf("strict move copy = %d, want %d (still hanging)", got, sand)
	}
	if got := srv.BlockAt(24, 64, 24); got != 0 {
		t.Errorf("moved-from block = %d, want air", got)
	}
	c.Disconnect()
}

// TestCloneCrossesDimensionBorders checks the dimension forms: a clone into
// the nether the to <dimension> form names lands there once a player stands
// in it to keep its chunks around, the from <dimension> form copies back
// out of it, the end takes its copies the same way, and a destination
// corner outside the nether's band of blocks is refused the way a position
// the dimension does not hold is.
func TestCloneCrossesDimensionBorders(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate", "Boltholer"))
	c := connect().Join("Testificate")

	bolt := connect().Join("Boltholer")
	srv.MovePlayerToDimension("Boltholer", world.NetherName, 80.5, 40, -16.5)
	servertest.AwaitViewSent(t, bolt)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.PlayerDimension("Boltholer") == world.NetherName })
	// The view arrives in paced batches, the chunks the clones land in
	// among them.
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.ChunkLoadedInDimension(world.NetherName, 20, 20) })

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	srv.SetBlock(20, 70, 20, stone)
	srv.SetBlock(21, 70, 20, stone)

	// Into the nether: the copy lands around the player standing there.
	c.ChatCommand("clone 20 70 20 21 70 20 to minecraft:the_nether 80 40 -30")
	expectCloneFeedback(t, c, 2)
	for x := int32(80); x <= 81; x++ {
		if got := srv.BlockInDimension(world.NetherName, x, 40, -30); got != stone {
			t.Errorf("nether block at (%d, 40, -30) = %d, want %d (stone)", x, got, stone)
		}
	}

	// And back out of it: the from <dimension> form reads the nether's
	// blocks and lands its copy beside the runner.
	c.ChatCommand("clone from minecraft:the_nether 80 40 -30 81 40 -30 60 70 60")
	expectCloneFeedback(t, c, 2)
	for x := int32(60); x <= 61; x++ {
		if got := srv.BlockAt(x, 70, 60); got != stone {
			t.Errorf("block at (%d, 70, 60) = %d, want %d (stone)", x, got, stone)
		}
	}

	// The nether's band reaches to 256, so a copy at its top lands; a
	// corner over it is outside the band of blocks the dimension holds.
	c.ChatCommand("clone 60 70 60 61 70 60 to minecraft:the_nether 20 200 20")
	expectCloneFeedback(t, c, 2)
	for x := int32(20); x <= 21; x++ {
		if got := srv.BlockInDimension(world.NetherName, x, 200, 20); got != stone {
			t.Errorf("nether block at (%d, 200, 20) = %d, want %d (stone)", x, got, stone)
		}
	}
	c.ChatCommand("clone 20 70 20 20 70 20 to minecraft:the_nether 20 300 20")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")

	// The end takes its copies the same way, the player standing there
	// keeping its chunks around.
	srv.MovePlayerToDimension("Boltholer", world.EndName, 80.5, 80, -16.5)
	// The nether's paced batches may still be queued ahead of the end's.
	bolt.AwaitPacket(protocol.IDPlayRespawn)
	servertest.AwaitViewSent(t, bolt)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.PlayerDimension("Boltholer") == world.EndName })
	c.ChatCommand("clone 20 70 20 21 70 20 to minecraft:the_end 80 80 -30")
	expectCloneFeedback(t, c, 2)
	for x := int32(80); x <= 81; x++ {
		if got := srv.BlockInDimension(world.EndName, x, 80, -30); got != stone {
			t.Errorf("end block at (%d, 80, -30) = %d, want %d (stone)", x, got, stone)
		}
	}
	c.Disconnect()
	bolt.Disconnect()
}

// TestCloneCountsBarrierCellsAsVanillaDoes checks the count a clone reports
// against the barrier vanilla writes into every destination cell first: a
// source cell that already holds that barrier lands but is not counted, so a
// barrier beside a glowstone counts one, and a region only of barriers
// copies nothing and is refused.
func TestCloneCountsBarrierCellsAsVanillaDoes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	barrier, ok := registry.FindState("minecraft:barrier", nil)
	if !ok {
		t.Fatal("the registry has no barrier")
	}
	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}
	srv.SetBlock(20, 70, 20, barrier)
	srv.SetBlock(21, 70, 20, glowstone)

	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40")
	expectCloneFeedback(t, c, 1)
	if got := srv.BlockAt(40, 70, 40); got != barrier {
		t.Errorf("block at (40, 70, 40) = %d, want %d (barrier)", got, barrier)
	}
	if got := srv.BlockAt(41, 70, 40); got != glowstone {
		t.Errorf("block at (41, 70, 40) = %d, want %d (glowstone)", got, glowstone)
	}

	// The barrier lands at the destination too, but a region only of them
	// leaves the count nothing to report and the clone is refused.
	srv.SetBlock(20, 72, 20, barrier)
	srv.SetBlock(21, 72, 20, barrier)
	c.ChatCommand("clone 20 72 20 21 72 20 50 72 50")
	servertest.AwaitCommandFailure(t, c, "commands.clone.failed")
	c.Disconnect()
}

// TestCloneFilteredTagCarriesProperties checks a tag predicate with a
// property test: of two stairs in the tag, only the one whose state carries
// the tested property is picked.
func TestCloneFilteredTagCarriesProperties(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	north, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "north"})
	if !ok {
		t.Fatal("the registry has no north-facing oak stairs")
	}
	south, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "south"})
	if !ok {
		t.Fatal("the registry has no south-facing oak stairs")
	}
	srv.SetBlock(20, 70, 20, north)
	srv.SetBlock(21, 70, 20, south)

	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40 filtered #minecraft:stairs[facing=north]")
	expectCloneFeedback(t, c, 1)
	if got := srv.BlockAt(40, 70, 40); got != north {
		t.Errorf("block at (40, 70, 40) = %d, want %d (north-facing stairs)", got, north)
	}
	if got := srv.BlockAt(41, 70, 40); got != 0 {
		t.Errorf("block at (41, 70, 40) = %d, want air: the south-facing stair was not picked", got)
	}

	// A key the tag's blocks do not carry is read vaguely: it is not
	// refused, and it picks nothing, so the clone copies no cell.
	c.ChatCommand("clone 20 70 20 21 70 20 40 70 40 filtered #minecraft:stairs[bogus=north]")
	servertest.AwaitCommandFailure(t, c, "commands.clone.failed")
	c.Disconnect()
}

// TestCloneHandlesADestinationPastTheBuildLimit checks the destination
// against the overworld's band of blocks: a corner over the band is refused
// as a position the world does not hold, and a box whose own top runs past
// the band copies only the rows the world holds and counts them.
func TestCloneHandlesADestinationPastTheBuildLimit(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}
	srv.SetBlock(10, 70, 10, glowstone)
	srv.SetBlock(10, 71, 10, glowstone)
	srv.SetBlock(10, 72, 10, glowstone)

	// A destination corner at y=320 sits above the overworld's top at 319.
	c.ChatCommand("clone 10 70 10 10 70 10 10 320 10")
	servertest.AwaitCommandFailure(t, c, "argument.pos.outofworld")

	// A three-row copy onto y=318 lands its two rows and drops the third.
	c.ChatCommand("clone 10 70 10 10 72 10 20 318 20")
	expectCloneFeedback(t, c, 2)
	if got := srv.BlockAt(20, 318, 20); got != glowstone {
		t.Errorf("block at (20, 318, 20) = %d, want %d (glowstone)", got, glowstone)
	}
	if got := srv.BlockAt(20, 319, 20); got != glowstone {
		t.Errorf("block at (20, 319, 20) = %d, want %d (glowstone)", got, glowstone)
	}
	c.Disconnect()
}

// TestCloneSendsTheCopiedSpawnerDataAfterTheBlock pins the order a clone
// sends a copied block entity's data in: the block reaches the client
// before its data, which names the copied skeleton, because a client drops
// block entity data for a block it has not been told of.
func TestCloneSendsTheCopiedSpawnerDataAfterTheBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`setblock 5 70 5 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	expectBlockEntityData(t, c) // the source being placed
	c.Drain()

	c.ChatCommand("clone 5 70 5 5 70 5 8 70 8")
	compound := awaitFillEntityData(t, c, 8, 70, 8)
	if got := spawnDataID(compound); got != "minecraft:skeleton" {
		t.Errorf("the copied block entity data names %q, want minecraft:skeleton", got)
	}
	held, ok := srv.BlockEntityTagAt(8, 70, 8)
	if !ok {
		t.Fatal("the copied spawner carries no block entity")
	}
	if got := spawnDataID(held); got != "minecraft:skeleton" {
		t.Errorf("the copied spawner holds %q, want minecraft:skeleton", got)
	}
	c.Disconnect()
}

// TestCloneRunsAnAutoCommandBlockBeforeItsNeighboursPowerIt places an
// always-active command block holding a running command beside a redstone
// block, then copies the pair: the copied block's tag load books its run
// inside the copy, before the copy's neighbour updates power the block, so
// the copy still says its line. A booking behind those updates would read
// the powered flag already on and drop the run.
// vanilla: CloneCommands.clone, CommandBlockEntity.loadAdditional
func TestCloneRunsAnAutoCommandBlockBeforeItsNeighboursPowerIt(t *testing.T) {
	t.Parallel()
	_, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,Command:"say hi"}`)
	c.ExpectSayLine(t, "@", "hi")
	c.Settle()

	// A redstone block beside the auto block, placed strict so its own
	// placement wakes nothing, and the source's powered flag cleared so the
	// tag the copy carries holds a block that is not already powered.
	c.ChatCommand("setblock 3 64 0 redstone_block strict")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "3", "64", "0")
	c.ChatCommand("data merge block 2 64 0 {powered:0b}")
	c.Settle()

	c.ChatCommand("clone 2 64 0 3 64 0 2 64 5")
	expectCloneFeedback(t, c, 2)
	c.ExpectSayLine(t, "@", "hi")
	c.Disconnect()
}

// TestCloneCarriesAPendingRepeaterTick clones a powered repeater whose
// turn-off tick is pending: the copy takes the tick and turns off with its
// source. vanilla: LevelTicks.copyAreaFrom
func TestCloneCarriesAPendingRepeaterTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 5, 64, 0, "repeater[facing=west]")
	servertest.Setblock(t, c, 4, 64, 0, "redstone_block")
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:repeater", map[string]string{"powered": "true"})

	servertest.Setblock(t, c, 4, 64, 0, "air")
	// strict: a waking clone has the copy schedule its own turn-off.
	c.ChatCommand("clone 5 64 0 5 64 0 5 64 5 strict")
	expectCloneFeedback(t, c, 1)
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:repeater", map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:repeater", map[string]string{"powered": "false"})
}

// TestCloneMoveCarriesAPendingRepeaterTick: a move clears its source before
// the copy lands, and the moved repeater still takes its turn-off tick.
// vanilla: LevelTicks.copyAreaFrom
func TestCloneMoveCarriesAPendingRepeaterTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 5, 64, 0, "repeater[facing=west]")
	servertest.Setblock(t, c, 4, 64, 0, "redstone_block")
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:repeater", map[string]string{"powered": "true"})

	servertest.Setblock(t, c, 4, 64, 0, "air")
	c.ChatCommand("clone 5 64 0 5 64 0 5 64 5 strict replace move")
	expectCloneFeedback(t, c, 1)
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:repeater", map[string]string{"powered": "false"})
}
