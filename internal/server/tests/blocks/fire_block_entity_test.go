package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABurnedLecternDropsItsBook burns a lectern holding a book: the block
// entity goes with the block and the book is thrown out.
// vanilla: LevelChunk.setBlockState, LecternBlockEntity.preRemoveSideEffects
func TestABurnedLecternDropsItsBook(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, 21)

	servertest.Setblock(t, c, 5, 64, 0, `lectern[facing=north,has_book=true]{Book:{id:"minecraft:writable_book",count:1}}`)
	servertest.Setblock(t, c, 5, 65, 0, "fire")
	c.Settle()

	for range 400 {
		if srv.BlockNameAt(5, 64, 0) != "minecraft:lectern" {
			break
		}
		if srv.BlockNameAt(5, 65, 0) == "minecraft:air" {
			servertest.Setblock(t, c, 5, 65, 0, "fire")
		}
		srv.TickPhysics(20)
	}
	if name := srv.BlockNameAt(5, 64, 0); name == "minecraft:lectern" {
		t.Fatal("the lectern never burned")
	}
	if srv.HasBlockEntity(5, 64, 0) {
		t.Error("the burned lectern kept its block entity")
	}
	if n := srv.ItemCount(); n != 1 {
		t.Errorf("item count = %d, want the lectern's book", n)
	}
}
