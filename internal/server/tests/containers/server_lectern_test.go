package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LecternBlock.useItemOn, LecternBlock.signalPageChange,
// LecternMenu.clickMenuButton, LecternBlockEntity.getRedstoneSignal

const (
	lecternPrevPage = 1
	lecternNextPage = 2
	lecternTakeBook = 3
	lecternJump     = 100
)

// lecternServer is a step-clocked server with the player listed as an
// operator and a comparator reading the lectern at (4, 64, 0) over a wire.
func lecternServer(t *testing.T) (*server.Server, *servertest.WindowedClient, *servertest.StepClock) {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	w := servertest.WindowedOf(t, "Testificate", connect())
	servertest.LayComparatorScene(t, w.Client, srv, 5, 64, 0)
	return srv, w, clock
}

// writableBookOf is a writable book of n pages.
func writableBookOf(t *testing.T, pages int) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutVarInt(int32(pages))
	for range pages {
		value.PutString("page")
		value.PutBool(false)
	}
	stack := servertest.ItemStack(t, "minecraft:writable_book", 1)
	stack.Raw = []protocol.RawComponent{{Type: protocol.DataComponentWritableBookContent, Data: value.Bytes()}}
	return stack
}

// putBookOnLectern lays a lectern at (4, 64, 0) and puts a book of n pages
// on it with a click.
func putBookOnLectern(t *testing.T, srv *server.Server, w *servertest.WindowedClient, clock *servertest.StepClock, pages int) {
	t.Helper()
	servertest.Setblock(t, w.Client, 4, 64, 0, "minecraft:lectern[facing=north,has_book=false,powered=false]")
	srv.GiveSlot("Testificate", player.HotbarSlot, writableBookOf(t, pages))
	w.Settle()
	w.UseBlock(4, 64, 0)
	w.Settle()
	servertest.StepTicks(t, srv, clock, 4)
}

func lecternProps(t *testing.T, srv *server.Server) map[string]string {
	t.Helper()
	name, props := servertest.StatePropsOf(t, srv, 4, 64, 0)
	if name != "minecraft:lectern" {
		t.Fatalf("the block at the lectern is %s", name)
	}
	return props
}

// TestALecternTakesABookAndTurnsPages places a book, opens the window, turns
// pages and checks the powered pulse, the wire a comparator drives and the
// page the block entity keeps.
func TestALecternTakesABookAndTurnsPages(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	putBookOnLectern(t, srv, w, clock, 11)

	if props := lecternProps(t, srv); props["has_book"] != "true" || props["powered"] != "false" {
		t.Fatalf("the lectern after the book went on = %v, want has_book=true powered=false", props)
	}
	// The first page of eleven reads 1.
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")

	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	if len(slots) != 1 || slots[0].ItemID != servertest.ItemID(t, "minecraft:writable_book") {
		t.Fatalf("the lectern window holds %+v, want the book alone", slots)
	}
	w.AwaitWindowProperty(0, 0)

	w.ClickButton(lecternNextPage)
	w.AwaitWindowProperty(0, 1)
	if props := lecternProps(t, srv); props["powered"] != "true" {
		t.Errorf("the lectern after a page turn = %v, want powered=true", props)
	}
	servertest.StepTicks(t, srv, clock, 3)
	if props := lecternProps(t, srv); props["powered"] != "false" {
		t.Errorf("the lectern after the pulse = %v, want powered=false", props)
	}
	// Page 1 of 0..10 is floor(1/10*14) + 1.
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "2")

	w.ClickButton(lecternJump + 10)
	w.AwaitWindowProperty(0, 10)
	servertest.StepTicks(t, srv, clock, 3)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "15")

	// Past the last page nothing moves and nothing pulses.
	w.ClickButton(lecternNextPage)
	w.Settle()
	if props := lecternProps(t, srv); props["powered"] != "false" {
		t.Errorf("the lectern after a turn past the end = %v, want powered=false", props)
	}
	if tag, ok := srv.BlockEntityTagAt(4, 64, 0); !ok || tag["Page"] != nbt.Int(10) {
		t.Errorf("the lectern's saved page = %v, want 10", tag["Page"])
	}

	w.ClickButton(lecternPrevPage)
	w.AwaitWindowProperty(0, 9)
}

// TestALecternPowersItsNeighboursWhileThePageTurns reads the pulse off a wire
// beside the lectern.
func TestALecternPowersItsNeighboursWhileThePageTurns(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	stone, _ := registry.FindState("minecraft:stone", nil)
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	srv.SetBlock(4, 63, 1, stone)
	srv.SetBlock(4, 64, 1, wire)
	putBookOnLectern(t, srv, w, clock, 3)
	if got := servertest.WirePowerAt(srv, 4, 64, 1); got != "0" {
		t.Fatalf("the wire beside a resting lectern carries %q, want 0", got)
	}

	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	w.AwaitWindowProperty(0, 0)
	w.ClickButton(lecternNextPage)
	w.AwaitWindowProperty(0, 1)
	if got := servertest.WirePowerAt(srv, 4, 64, 1); got != "15" {
		t.Errorf("the wire during the pulse carries %q, want 15", got)
	}
	servertest.StepTicks(t, srv, clock, 3)
	servertest.ExpectWirePower(t, srv, 4, 64, 1, "0")
}

// TestALecternGivesTheBookBack presses the take button: the book returns to
// the inventory, has_book clears and the window closes.
func TestALecternGivesTheBookBack(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	putBookOnLectern(t, srv, w, clock, 3)
	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{})
	w.Settle()

	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	w.ClickButton(lecternTakeBook)
	w.Settle()
	servertest.StepTicks(t, srv, clock, 2)

	if props := lecternProps(t, srv); props["has_book"] != "false" {
		t.Errorf("the lectern after the take = %v, want has_book=false", props)
	}
	book := writableBookOf(t, 3)
	found := false
	for slot := range player.MainInvSlots {
		if got := srv.InventorySlot("Testificate", player.MainSlot+slot); got.Equal(book) {
			found = true
		}
	}
	if !found {
		t.Error("the taken book is not in the inventory")
	}
	// An empty lectern powers nothing: the comparator reads zero.
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")
}

// TestABrokenLecternThrowsItsBook breaks a lectern that holds a book: the
// book lies on the ground, shifted toward the side the lectern faced.
func TestABrokenLecternThrowsItsBook(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	putBookOnLectern(t, srv, w, clock, 2)

	w.ChatCommand("setblock 4 64 0 minecraft:air destroy")
	w.Settle()
	var book *server.TestItem
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:writable_book" {
			it := it
			book = &it
		}
	}
	if book == nil {
		t.Fatal("the broken lectern left no book")
	}
	// Facing north: the book is nudged a quarter block toward -z.
	if book.Z > 64 || book.Z < -1 {
		t.Errorf("the book lies at z=%.2f, want it toward the lectern's north side", book.Z)
	}
}

// TestALecternBookSurvivesTheSave checks the block entity's file form: the
// book and the page it was left on.
func TestALecternBookSurvivesTheSave(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	putBookOnLectern(t, srv, w, clock, 5)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	w.AwaitWindowProperty(0, 0)
	w.ClickButton(lecternJump + 3)
	w.AwaitWindowProperty(0, 3)

	tag, ok := srv.BlockEntityTagAt(4, 64, 0)
	if !ok {
		t.Fatal("the lectern has no block entity")
	}
	if tag["Page"] != nbt.Int(3) {
		t.Errorf("Page = %v, want 3", tag["Page"])
	}
	book, ok := nbt.Get[nbt.Compound](tag, "Book")
	if !ok || book["id"] != nbt.String("minecraft:writable_book") {
		t.Errorf("Book = %v, want the writable book", tag["Book"])
	}
}

// openLectern opens the window of the lectern at (4, 64, 0).
func openLectern(t *testing.T, w *servertest.WindowedClient) {
	t.Helper()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuLectern, false, 4, 64, 0)
	w.AwaitWindowProperty(0, 0)
}

// TestAnAdventurePlayerTurnsPagesButCannotTakeTheBook checks the take button
// is refused for a body that may not build.
func TestAnAdventurePlayerTurnsPagesButCannotTakeTheBook(t *testing.T) {
	t.Parallel()
	srv, w, clock := lecternServer(t)
	putBookOnLectern(t, srv, w, clock, 4)
	w.ChatCommand("gamemode adventure")
	w.Settle()
	openLectern(t, w)
	w.ClickButton(lecternTakeBook)
	w.Settle()
	if props := lecternProps(t, srv); props["has_book"] != "true" {
		t.Errorf("the lectern after an adventure take = %v, want the book kept", props)
	}
	w.ClickButton(lecternNextPage)
	w.AwaitWindowProperty(0, 1)
}
