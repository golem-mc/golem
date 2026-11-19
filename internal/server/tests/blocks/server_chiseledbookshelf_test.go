package blocks

// vanilla: ChiseledBookShelfBlock.useItemOn, useWithoutItem,
// getAnalogOutputSignal, ChiseledBookShelfBlockEntity.updateState,
// HopperBlockEntity.getBlockContainer

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// useOnBookshelfFace right-clicks the block at the position on the given face
// with the given hand and cursor, the way a client aims at one cell of a
// shelf's front, and answers the sounds the click played before the change
// acknowledgement that ends it.
func useOnBookshelfFace(t *testing.T, c *servertest.Client, hand, x, y, z, face int32, cursorX, cursorY, cursorZ float32) []servertest.PlaySoundEffect {
	t.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(cursorX)
		b.PutF32(cursorY)
		b.PutF32(cursorZ)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
	var sounds []servertest.PlaySoundEffect
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayAckBlockChange:
			return sounds
		case protocol.IDPlaySoundEffect:
			sounds = append(sounds, servertest.DecodePlaySoundEffect(t, data))
		}
	}
	t.Fatalf("the click on (%d, %d, %d) was never acknowledged", x, y, z)
	return nil
}

// bookshelfSlotSound checks one of the shelf's click sounds among a click
// played: the named one, in the block category, at the block's centre and at
// vanilla's volume and pitch.
func bookshelfSlotSound(t *testing.T, sounds []servertest.PlaySoundEffect, want string, x, y, z int32) {
	t.Helper()
	id, ok := registry.SoundEventID(want)
	if !ok {
		t.Fatalf("the sound registry has no %s", want)
	}
	for _, sound := range sounds {
		if sound.RegistryID != id {
			continue
		}
		if sound.Category != protocol.SoundCategoryBlock {
			t.Errorf("the %s category = %d, want %d (blocks)", want, sound.Category, protocol.SoundCategoryBlock)
		}
		// The coordinates travel as eighths of a block; the block's centre
		// is what a block's sound is played at.
		if wx, wy, wz := (x*8)+4, (y*8)+4, (z*8)+4; sound.X != wx || sound.Y != wy || sound.Z != wz {
			t.Errorf("the %s at (%d %d %d), want the block's centre (%d %d %d)",
				want, sound.X, sound.Y, sound.Z, wx, wy, wz)
		}
		if sound.Volume != 1 || sound.Pitch != 1 {
			t.Errorf("the %s volume, pitch = %v, %v, want 1, 1", want, sound.Volume, sound.Pitch)
		}
		return
	}
	t.Errorf("the click played no %s among %+v", want, sounds)
}

// bookshelfLastSlot reads the last_interacted_slot the block entity at a
// position keeps. vanilla: ChiseledBookShelfBlockEntity.lastInteractedSlot
func bookshelfLastSlot(t *testing.T, srv *server.Server, x, y, z int32) int {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatalf("no block entity at (%d, %d, %d)", x, y, z)
	}
	slot, ok := nbt.IntValue(tag["last_interacted_slot"])
	if !ok {
		t.Fatalf("the tag at (%d, %d, %d) names no last_interacted_slot: %v", x, y, z, tag)
	}
	return int(slot)
}

// TestAChiseledBookshelfTakesABookAndGivesItBack is the feature: a right-click
// on one cell of a shelf's front with a book in the main hand puts the book in
// that cell's slot and nowhere else — one item leaving the hand in survival —
// and the same cell clicked with the hand empty takes it back, the occupied
// bit following the book both ways, the cell remembered as the last one
// either way.
// vanilla: ChiseledBookShelfBlock.addBook, removeBook,
// ChiseledBookShelfBlockEntity.updateState
func TestAChiseledBookshelfTakesABookAndGivesItBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:chiseled_bookshelf"])
	book := servertest.ItemStack(t, "minecraft:book", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, book)
	c.Settle()

	// The shelf faces north, so the front is its north face: with the cursor
	// at x=0.1 the cell is the top row's rightmost one, slot 2 of the
	// vanilla 3x2 hit grid, y=0.75 naming the top row.
	sounds := useOnBookshelfFace(t, c, protocol.HandMain, 3, 64, 0, protocol.FaceNorth, 0.1, 0.75, 0)
	bookshelfSlotSound(t, sounds, "minecraft:block.chiseled_bookshelf.insert", 3, 64, 0)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 6 || !got[2].Equal(book) {
		t.Errorf("the shelf after the insert holds %+v, want the book in slot 2", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["slot_2_occupied"] != "true" || props["slot_0_occupied"] != "false" {
		t.Errorf("the shelf after the insert = %v, want slot_2_occupied alone", props)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after a survival insert = %+v, want the book spent", got)
	}
	if got := bookshelfLastSlot(t, srv, 3, 64, 0); got != 2 {
		t.Errorf("the last interacted slot after the insert = %d, want 2", got)
	}

	// The same cell with the now-empty hand: the book comes back to the
	// player and the bit falls, while the last cell touched stays this one.
	sounds = useOnBookshelfFace(t, c, protocol.HandMain, 3, 64, 0, protocol.FaceNorth, 0.1, 0.75, 0)
	bookshelfSlotSound(t, sounds, "minecraft:block.chiseled_bookshelf.pickup", 3, 64, 0)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 6 || !got[2].Empty() {
		t.Errorf("the shelf after the take-back holds %+v, want slot 2 empty", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(book) {
		t.Errorf("the player after the take-back holds %+v in the hotbar, want the book back", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["slot_2_occupied"] != "false" {
		t.Errorf("the shelf after the take-back = %v, want slot_2_occupied=false", props)
	}
	if got := bookshelfLastSlot(t, srv, 3, 64, 0); got != 2 {
		t.Errorf("the last interacted slot after the take-back = %d, want it still 2", got)
	}
}

// TestAComparatorReadsTheChiseledBookshelfsLastInteractedSlot lays a shelf
// before a comparator: it answers with the last cell a book went into or came
// out of, from any side, one past that cell's number — 0 before anything has
// touched it - and each touch is a block change that wakes the comparator to
// read again.
// vanilla: ChiseledBookShelfBlock.getAnalogOutputSignal
func TestAComparatorReadsTheChiseledBookshelfsLastInteractedSlot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:chiseled_bookshelf")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	book := servertest.ItemStack(t, "minecraft:book", 1)
	c.SetCreativeSlot(player.HotbarSlot, book)
	// The bottom-left cell as the shelf's front reads, slot 3.
	useOnBookshelfFace(t, c, protocol.HandMain, 4, 64, 0, protocol.FaceNorth, 0.75, 0.25, 0)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "4")

	// Taking the book back leaves the cell last touched, so the shelf still
	// answers with it.
	useOnBookshelfFace(t, c, protocol.HandMain, 4, 64, 0, protocol.FaceNorth, 0.75, 0.25, 0)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "4")

	// A book in another cell — the bottom-right one, slot 5 - moves the
	// answer to it. The creative hand kept its book through both inserts,
	// the one the take handed back stacking onto it as vanilla's
	// inventory add does.
	useOnBookshelfFace(t, c, protocol.HandMain, 4, 64, 0, protocol.FaceNorth, 0.1, 0.25, 0)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "6")
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Equal(servertest.ItemStack(t, "minecraft:book", 2)) {
		t.Errorf("the creative hand holds %+v after the inserts, want its book kept with the taken one stacked on", got)
	}
}

// TestAChiseledBookshelfLoadedFromNBT lays a shelf with the NBT a save would
// carry: the books come back in their slots, the last touched cell with them,
// and the shelf hands a book to the first click of an empty hand as if the
// world had never gone.
// vanilla: ChiseledBookShelfBlockEntity.loadAdditional
func TestAChiseledBookshelfLoadedFromNBT(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 3, 64, 0,
		`minecraft:chiseled_bookshelf[facing=north,slot_1_occupied=true]`+
			`{Items:[{Slot:1,id:"minecraft:book",count:1}],last_interacted_slot:1}`)
	c.Settle()

	book := servertest.ItemStack(t, "minecraft:book", 1)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 6 || !got[1].Equal(book) {
		t.Errorf("the loaded shelf holds %+v, want the book in slot 1", got)
	}
	if got := bookshelfLastSlot(t, srv, 3, 64, 0); got != 1 {
		t.Errorf("the loaded last interacted slot = %d, want 1", got)
	}
	useOnBookshelfFace(t, c, protocol.HandMain, 3, 64, 0, protocol.FaceNorth, 0.4, 0.75, 0)
	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Equal(book) {
		t.Errorf("the loaded shelf handed %+v to the click, want its book back", got)
	}
}

// TestAHopperFeedsAChiseledBookshelf holds a book in a hopper above a shelf:
// the book lands in the shelf's first empty slot and shows on the block, and
// a hopper holding dirt keeps it, the shelf taking only the books an insert
// accepts.
// vanilla: ChiseledBookShelfBlockEntity.acceptsItemType,
// ListBackedContainer.canPlaceItem, HopperBlockEntity.getBlockContainer
func TestAHopperFeedsAChiseledBookshelf(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 4, 63, 0, "minecraft:chiseled_bookshelf")
	servertest.PlaceHopperOnTheGround(t, srv, w, 4, 64, 0)
	srv.StopTick()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:book", 1)) {
		t.Fatal("the hopper above has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)

	if got := srv.InventoryAt(4, 63, 0); len(got) != 6 || !got[0].Equal(servertest.ItemStack(t, "minecraft:book", 1)) {
		t.Errorf("the fed shelf holds %+v, want the book in its first slot", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 63, 0); props["slot_0_occupied"] != "true" {
		t.Errorf("the fed shelf = %v, want slot_0_occupied=true", props)
	}
	if got := bookshelfLastSlot(t, srv, 4, 63, 0); got != 0 {
		t.Errorf("the fed shelf's last interacted slot = %d, want 0", got)
	}

	// Dirt stays: the shelf answers only the books the insert accepts.
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper's block entity went with the book")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 5 || !got[0].Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Errorf("the hopper after the dirt's turn holds %+v, want the dirt kept", got)
	}
	if got := srv.InventoryAt(4, 63, 0); len(got) != 6 || !got[0].Equal(servertest.ItemStack(t, "minecraft:book", 1)) || !servertest.NothingIn(got[1:]) {
		t.Errorf("the shelf after the dirt's turn holds %+v, want its book alone", got)
	}
}
