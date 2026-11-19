package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The shelf's powered state: POWERED follows the neighbour signal, a rise
// sounding activate and a fall sounding deactivate; the placement reads the
// power at the cell. A rise links the powered shelves beside it into a side
// chain at most three long — part left, center or right naming the chain end
// the shelf is — and a fall or a removal unlinks. A click on the front swaps
// a whole held stack with one cell, or, when powered, the chain's cells with
// the hotbar.
// vanilla: ShelfBlock.neighborChanged, ShelfBlock.getStateForPlacement

// expectShelfSound fails unless the shelf's given sound plays to the client
// at the block, in the block category at volume and pitch one.
func expectShelfSound(t *testing.T, c *servertest.Client, sound string, x, y, z int32) {
	t.Helper()
	soundID, ok := registry.SoundEventID(sound)
	if !ok {
		t.Fatalf("the sound registry has no %s", sound)
	}
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != soundID+1 || r.ReadVarInt() != protocol.SoundCategoryBlock {
			return false
		}
		if r.ReadI32() != x*8+4 || r.ReadI32() != y*8+4 || r.ReadI32() != z*8+4 {
			return false
		}
		volume, pitch := r.ReadF32(), r.ReadF32()
		return volume == 1 && pitch == 1
	}) {
		t.Fatalf("the shelf's %s never played", sound)
	}
}

// TestAShelfPowersUpWhenARedstoneBlockLandsBesideIt is the rising edge: the
// block set beside an unpowered shelf turns it powered with the activate
// sound. The /setblock that stood the shelf wrote the state it was given;
// the arriving redstone block is the neighbour visit that reads the power.
// vanilla: ShelfBlock.neighborChanged
func TestAShelfPowersUpWhenARedstoneBlockLandsBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 4, "oak_shelf[facing=north]")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "false", "side_chain": "unconnected"})

	c.ChatCommand("setblock 5 64 5 redstone_block")
	expectShelfSound(t, c, "minecraft:block.shelf.activate", 5, 64, 4)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf", map[string]string{"powered": "true"})
}

// TestAShelfDropsItsSideChainWhenTheSignalGoes is the falling edge: a
// powered shelf whose side chain a command had left linked drops the link
// with its power when the signal goes, with the deactivate sound — and an
// arriving signal that matches POWERED writes and sounds nothing.
// vanilla: ShelfBlock.neighborChanged
func TestAShelfDropsItsSideChainWhenTheSignalGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 4, "oak_shelf[facing=north,powered=true,side_chain=left]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "true", "side_chain": "left"})

	c.ChatCommand("setblock 5 64 5 air")
	expectShelfSound(t, c, "minecraft:block.shelf.deactivate", 5, 64, 4)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "false", "side_chain": "unconnected"})
}

// TestAShelfSetDownBesideAnAlreadyPoweredBlockStartsPowered is the placement
// half: the shelf a player sets down beside a live redstone block arrives
// powered, and one set down out of any signal arrives unpowered.
// vanilla: ShelfBlock.getStateForPlacement
func TestAShelfSetDownBesideAnAlreadyPoweredBlockStartsPowered(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_block")
	servertest.PlaceFrom(c, "minecraft:oak_shelf", 5, 3, 270, 5, 63, 4, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "true"})

	servertest.PlaceFrom(c, "minecraft:oak_shelf", 8, 3, 270, 8, 63, 4, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 8, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "false"})
}

// TestAShelfSwapsAWholeHeldStackWithOneCellInSurvival is the unpowered
// feature: a right-click on one cell of the front puts the whole held stack
// into that cell and empties the hand, a second click swaps the hand's stack
// with the cell's, a click with an empty hand takes the cell's stack whole,
// and a click on an empty cell with an empty hand passes, sounding and
// moving nothing. Each moving click plays its sound at the block.
// vanilla: ShelfBlock.swapSingleItem
func TestAShelfSwapsAWholeHeldStackWithOneCellInSurvival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	servertest.Setblock(t, c, 3, 64, 0, "oak_shelf[facing=north]")
	apples := servertest.ItemStack(t, "minecraft:apple", 3)
	srv.GiveSlot("Testificate", player.HotbarSlot, apples)
	c.Settle()

	// The shelf faces north, so the front is its north face; the cursor at
	// the middle of it names the middle cell, slot 1 of the vanilla 3x1 hit
	// grid.
	click := func() []servertest.PlaySoundEffect {
		return useOnBookshelfFace(t, c, protocol.HandMain, 3, 64, 0, protocol.FaceNorth, 0.5, 0.5, 0.5)
	}

	sounds := click()
	bookshelfSlotSound(t, sounds, "minecraft:block.shelf.place_item", 3, 64, 0)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 3 || !got[0].Empty() || !got[1].Equal(apples) || !got[2].Empty() {
		t.Errorf("the shelf after the place holds %+v, want the three apples in the middle cell", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after placing a whole stack = %+v, want it spent", got)
	}

	wheat := servertest.ItemStack(t, "minecraft:wheat", 5)
	srv.GiveSlot("Testificate", player.HotbarSlot, wheat)
	c.Settle()
	sounds = click()
	bookshelfSlotSound(t, sounds, "minecraft:block.shelf.single_swap", 3, 64, 0)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 3 || !got[1].Equal(wheat) {
		t.Errorf("the shelf after the swap holds %+v, want the five wheat in the middle cell", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(apples) {
		t.Errorf("the hand after the swap = %+v, want the shelf's apples back", got)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	sounds = click()
	bookshelfSlotSound(t, sounds, "minecraft:block.shelf.take_item", 3, 64, 0)
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(wheat) {
		t.Errorf("the hand after the take = %+v, want the shelf's wheat", got)
	}
	if got := srv.InventoryAt(3, 64, 0); len(got) != 3 || !got[1].Empty() {
		t.Errorf("the shelf after the take holds %+v, want the middle cell empty", got)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	if sounds := click(); len(sounds) != 0 {
		t.Errorf("the click on an empty cell with an empty hand played %+v, want silence", sounds)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after the passed click = %+v, want it empty", got)
	}
}

// TestAShelfSwapRefusesAnEntityRemovedInTheWindow covers the break race: a
// click resolves its block entity before the swap's own lock is taken, so a
// break that lands in between leaves it holding an entity the world no
// longer has. The swap must not trade with it — the broken block's stack is
// the spill's and must not also reach the hand — and the click passes
// through.
// vanilla: ShelfBlock.swapSingleItem
func TestAShelfSwapRefusesAnEntityRemovedInTheWindow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 0, "oak_shelf[facing=north]")
	apples := servertest.ItemStack(t, "minecraft:apple", 3)
	if !srv.SetInventoryAt(3, 64, 0, 1, apples) {
		t.Fatal("the shelf took no apples")
	}
	wheat := servertest.ItemStack(t, "minecraft:wheat", 5)
	srv.GiveSlot("Actor", player.HotbarSlot, wheat)
	c.Settle()

	// The break lands between the click's lookup and the swap's lock.
	srv.SetShelfSwapHook(func() { srv.RemoveBlockEntity(3, 64, 0) })
	useOnBookshelfFace(t, c, protocol.HandMain, 3, 64, 0, protocol.FaceNorth, 0.5, 0.5, 0.5)
	c.Settle()

	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Equal(wheat) {
		t.Errorf("the hand after the refused swap = %+v, want the wheat untouched", got)
	}
	if got := srv.InventoryAt(3, 64, 0); got != nil {
		t.Errorf("the shelf after the break holds %+v, want the entity gone", got)
	}
}

// TestAShelfSwapsItsCellsIntoTheFarHotbarWhenPowered is the powered feature:
// a shelf powered on its own is a side chain of one, so its three cells swap
// with the far end of the hotbar, vanilla slots 6 through 8, playing the
// multi swap once at the clicked shelf and leaving the selected slot out of
// it.
// vanilla: ShelfBlock.swapHotbar
func TestAShelfSwapsItsCellsIntoTheFarHotbarWhenPowered(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 4, "oak_shelf[facing=north,powered=true]")
	apples := servertest.ItemStack(t, "minecraft:apple", 3)
	bread := servertest.ItemStack(t, "minecraft:bread", 2)
	wheat := servertest.ItemStack(t, "minecraft:wheat", 7)
	if !srv.SetInventoryAt(5, 64, 4, 0, apples) ||
		!srv.SetInventoryAt(5, 64, 4, 1, bread) ||
		!srv.SetInventoryAt(5, 64, 4, 2, wheat) {
		t.Fatal("the shelf took no items")
	}
	farWheat := servertest.ItemStack(t, "minecraft:wheat", 5)
	farBread := servertest.ItemStack(t, "minecraft:bread", 9)
	farIron := servertest.ItemStack(t, "minecraft:iron_ingot", 4)
	srv.GiveSlot("Actor", 42, farWheat)
	srv.GiveSlot("Actor", 43, farBread)
	srv.GiveSlot("Actor", 44, farIron)
	stick := servertest.ItemStack(t, "minecraft:stick", 1)
	srv.GiveSlot("Actor", player.HotbarSlot, stick)
	c.Settle()

	sounds := useOnBookshelfFace(t, c, protocol.HandMain, 5, 64, 4, protocol.FaceNorth, 0.5, 0.5, 0.5)
	bookshelfSlotSound(t, sounds, "minecraft:block.shelf.multi_swap", 5, 64, 4)
	if len(sounds) != 1 {
		t.Errorf("the hotbar swap played %+v, want the multi swap alone", sounds)
	}
	if got := srv.InventoryAt(5, 64, 4); len(got) != 3 || !got[0].Equal(farWheat) || !got[1].Equal(farBread) || !got[2].Equal(farIron) {
		t.Errorf("the shelf after the swap holds %+v, want the far hotbar's stacks", got)
	}
	if got := srv.InventorySlot("Actor", 42); !got.Equal(apples) {
		t.Errorf("hotbar slot 42 after the swap = %+v, want the shelf's apples", got)
	}
	if got := srv.InventorySlot("Actor", 43); !got.Equal(bread) {
		t.Errorf("hotbar slot 43 after the swap = %+v, want the shelf's bread", got)
	}
	if got := srv.InventorySlot("Actor", 44); !got.Equal(wheat) {
		t.Errorf("hotbar slot 44 after the swap = %+v, want the shelf's wheat", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Equal(stick) {
		t.Errorf("the selected slot after the swap = %+v, want it untouched", got)
	}
}

// TestAShelfSideChainLinksItsPoweredNeighbours is the linking: three shelves
// facing north in a row power up one after another, each rise joining the
// powered ones into a side chain — the easternmost shelf the chain's left
// end, the westernmost the right end, a shelf between them the centre. A
// shelf powered alone stays unconnected.
// vanilla: ShelfBlock.updateSelfAndNeighborsOnPoweringUp
func TestAShelfSideChainLinksItsPoweredNeighbours(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 4, "oak_shelf[facing=north]")
	servertest.Setblock(t, c, 4, 64, 4, "oak_shelf[facing=north]")
	servertest.Setblock(t, c, 5, 64, 4, "oak_shelf[facing=north]")
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "unconnected"})

	servertest.Setblock(t, c, 3, 64, 5, "redstone_block")
	expectShelfSound(t, c, "minecraft:block.shelf.activate", 3, 64, 4)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "unconnected"})

	servertest.Setblock(t, c, 4, 64, 5, "redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "left"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "right"})

	servertest.Setblock(t, c, 5, 64, 5, "redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "left"})
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "center"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "right"})
}

// TestAShelfSideChainUnlinksAsItsPowerGoes is the unlinking: a chain end
// losing its signal unlinks itself alone and the rest stays a shorter chain;
// breaking a middle shelf collapses both ends to unconnected while their
// power stays.
// vanilla: ShelfBlock.updateNeighborsAfterPoweringDown,
// ShelfBlock.affectNeighborsAfterRemoval
func TestAShelfSideChainUnlinksAsItsPowerGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 4, "oak_shelf[facing=north]")
	servertest.Setblock(t, c, 4, 64, 4, "oak_shelf[facing=north]")
	servertest.Setblock(t, c, 5, 64, 4, "oak_shelf[facing=north]")
	servertest.Setblock(t, c, 3, 64, 5, "redstone_block")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_block")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "center"})

	c.ChatCommand("setblock 3 64 5 air")
	expectShelfSound(t, c, "minecraft:block.shelf.deactivate", 3, 64, 4)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "false", "side_chain": "unconnected"})
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "right"})
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "left"})

	servertest.Setblock(t, c, 3, 64, 5, "redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf",
		map[string]string{"powered": "true", "side_chain": "right"})
	servertest.ExpectBlockNamed(t, srv, 4, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "center"})
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "left"})

	c.ChatCommand("setblock 4 64 4 air")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "unconnected"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 4, "minecraft:oak_shelf", map[string]string{"side_chain": "unconnected"})
}

// TestAShelfSwapReachesItsComparator is the container wake: swapping a stack
// into a shelf a comparator reads raises the wire beyond the comparator,
// because the swap marks the shelf's contents changed. Without the mark the
// comparator has nothing to recompute and the wire stays quiet.
// vanilla: ShelfBlockEntity.setChanged, Level.updateNeighbourForOutputSignal
func TestAShelfSwapReachesItsComparator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)

	// The shelf faces west, so the face opposite it — the one a comparator
	// standing to its east reads through — is that comparator's input.
	servertest.Setblock(t, c, 4, 64, 0, "oak_shelf[facing=west]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:apple", 3))
	c.Settle()
	useOnBookshelfFace(t, c, protocol.HandMain, 4, 64, 0, protocol.FaceWest, 0.5, 0.5, 0.5)
	c.Settle()

	// The bit of whichever cell the cursor named, so the test does not
	// depend on the front's hit grid.
	want := 0
	for i, stack := range srv.InventoryAt(4, 64, 0) {
		if !stack.Empty() {
			want |= 1 << i
		}
	}
	if want == 0 {
		t.Fatal("the swap left the shelf empty, so it has no analog output to carry")
	}
	servertest.ExpectWirePower(t, srv, 6, 64, 0, strconv.Itoa(want))
}

// TestAPoweredShelfHotbarSwapReachesItsComparator is the container wake on
// the powered path: the hotbar swap marks every shelf it moved, so a
// comparator reading one raises the wire beyond. The unpowered single swap
// and each shelf of a chain take the same mark.
// vanilla: ShelfBlockEntity.setChanged, Level.updateNeighbourForOutputSignal
func TestAPoweredShelfHotbarSwapReachesItsComparator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)

	servertest.Setblock(t, c, 4, 64, 0, "oak_shelf[facing=west,powered=true]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	srv.GiveSlot("Actor", 42, servertest.ItemStack(t, "minecraft:apple", 3))
	srv.GiveSlot("Actor", 43, servertest.ItemStack(t, "minecraft:bread", 2))
	srv.GiveSlot("Actor", 44, servertest.ItemStack(t, "minecraft:wheat", 7))
	c.Settle()
	useOnBookshelfFace(t, c, protocol.HandMain, 4, 64, 0, protocol.FaceWest, 0.5, 0.5, 0.5)
	c.Settle()

	// The bit of whichever cell the swap filled, so the test does not depend
	// on the front's hit grid.
	want := 0
	for i, stack := range srv.InventoryAt(4, 64, 0) {
		if !stack.Empty() {
			want |= 1 << i
		}
	}
	if want == 0 {
		t.Fatal("the hotbar swap left the shelf empty, so it has no analog output to carry")
	}
	servertest.ExpectWirePower(t, srv, 6, 64, 0, strconv.Itoa(want))
}

// TestAShelfSwapMarksItsChunkDirty is the persistence half: a swap that only
// moved a stack and left the block state alone still marks the chunk for the
// next save, so the moved stack reaches the region file. The loop is stopped
// first, because the inhabited-time pass dirties every chunk a player stands
// near on each tick and would leave no clean chunk to read the swap's own
// mark against.
// vanilla: ShelfBlockEntity.setChanged
func TestAShelfSwapMarksItsChunkDirty(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The shelf stands a chunk over from the player, in chunk (1, 0), so a
	// mark the swap leaves is the shelf's own.
	c.MoveToGround(15.5, 64, 0.5, true)
	c.Settle()
	servertest.Setblock(t, c, 16, 64, 0, "oak_shelf[facing=north]")
	c.Settle()

	// InhabitTime dirties every chunk near a player each tick, so the loop is
	// stopped to hold the chunk clean; the save then clears the shelf's own
	// placement mark.
	srv.StopTick()
	if !srv.SaveAll(false) {
		t.Fatal("the first save failed")
	}
	srv.FlushSaves()
	if srv.ChunkDirtyAt(16, 0) {
		t.Fatal("the chunk is still dirty after a save, so a later mark cannot be told from this one")
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:apple", 3))
	c.Settle()
	// The stopped loop never flushes the ack, so the click is settled without
	// waiting for one and the flush puts it on the wire.
	c.UseBlockSeq(16, 64, 0, protocol.FaceNorth, 1)
	c.SettleKeeping()
	srv.FlushBlockAcks()

	if !srv.ChunkDirtyAt(16, 0) {
		t.Fatal("the swap did not mark the shelf's chunk for the next save")
	}

	if !srv.SaveAll(false) {
		t.Fatal("the second save failed")
	}
	srv.FlushSaves()

	pos := world.ChunkPos{X: 1}
	root := servertest.ReadBlockChunkNBT(t, servertest.ChunkRegionPath(cfg.LevelName, pos), pos)
	entities, ok := nbt.Get[nbt.List](root, "block_entities")
	if !ok {
		t.Fatal("the saved chunk holds no block entities at all")
	}
	found := false
	for _, tag := range entities.Items {
		entry, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		id, _ := nbt.Get[nbt.String](entry, "id")
		x, _ := nbt.Get[nbt.Int](entry, "x")
		y, _ := nbt.Get[nbt.Int](entry, "y")
		z, _ := nbt.Get[nbt.Int](entry, "z")
		if id != "minecraft:shelf" || x != 16 || y != 64 || z != 0 {
			continue
		}
		items, _ := nbt.Get[nbt.List](entry, "Items")
		for _, itemTag := range items.Items {
			item, ok := itemTag.(nbt.Compound)
			if !ok {
				continue
			}
			if name, _ := nbt.Get[nbt.String](item, "id"); name == "minecraft:apple" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the moved stack never reached the shelf's region file: the swap did not mark its chunk dirty")
	}
}
