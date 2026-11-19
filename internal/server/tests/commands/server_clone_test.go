package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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
