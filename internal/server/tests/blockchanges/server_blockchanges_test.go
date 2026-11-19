package blockchanges

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// told is one packet a flush sent, in wire order: a Block Update or Update
// Section Blocks with the cells it carries, or a Block Entity Data or the
// block ack with none.
type told struct {
	id    int32
	cells []servertest.BlockChange
}

func (p told) section() bool { return p.id == protocol.IDPlaySectionBlocks }

// readTold runs one held tick and returns the packets about blocks that
// arrived up to its barrier.
func readTold(t *testing.T, srv *server.Server, c *servertest.Client) []told {
	t.Helper()
	var out []told
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			out = append(out, told{id: id, cells: servertest.BlockChangesIn(t, id, data)})
		case protocol.IDPlayBlockEntityData, protocol.IDPlayAckBlockChange:
			out = append(out, told{id: id})
		}
		return false
	})
	return out
}

func tellRig(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Warden")
	servertest.HoldTicks(t, srv)
	readTold(t, srv, c)
	return srv, c
}

func TestTwoCellsOfASectionArriveAsOneSectionBlocksUpdate(t *testing.T) {
	t.Parallel()
	srv, c := tellRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(1, 70, 2, stone)
	srv.SetBlock(3, 71, 4, stone)
	srv.TellBlockChanges([3]int32{1, 70, 2}, [3]int32{3, 71, 4})
	got := readTold(t, srv, c)
	if len(got) != 1 || !got[0].section() || len(got[0].cells) != 2 {
		t.Fatalf("got %+v, want one Section Blocks Update of two cells", got)
	}
	for _, cell := range got[0].cells {
		if cell.State != stone {
			t.Errorf("cell %+v carries state %d, want stone %d", cell, cell.State, stone)
		}
	}
}

// A cell queued twice goes out once, with the state it has at the flush.
func TestACellChangedTwiceArrivesOnceWithItsFinalState(t *testing.T) {
	t.Parallel()
	srv, c := tellRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	dirt := servertest.BlockState(t, "minecraft:dirt")
	srv.SetBlock(1, 70, 2, stone)
	srv.TellBlockChanges([3]int32{1, 70, 2}, [3]int32{1, 70, 2})
	srv.SetBlock(1, 70, 2, dirt)
	got := readTold(t, srv, c)
	want := []servertest.BlockChange{{X: 1, Y: 70, Z: 2, State: dirt}}
	if len(got) != 1 || got[0].section() || !slices.Equal(got[0].cells, want) {
		t.Fatalf("got %+v, want one Block Update %+v", got, want)
	}
}

func TestALoneChangedCellArrivesAsABlockUpdate(t *testing.T) {
	t.Parallel()
	srv, c := tellRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(5, 70, 6, stone)
	srv.TellBlockChanges([3]int32{5, 70, 6})
	got := readTold(t, srv, c)
	want := []servertest.BlockChange{{X: 5, Y: 70, Z: 6, State: stone}}
	if len(got) != 1 || got[0].section() || !slices.Equal(got[0].cells, want) {
		t.Fatalf("got %+v, want one Block Update %+v", got, want)
	}
}

// The sections go out bottom-up whatever order they changed in.
func TestChangesInTwoSectionsArriveBottomUp(t *testing.T) {
	t.Parallel()
	srv, c := tellRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(1, 90, 2, stone)
	srv.SetBlock(1, 70, 2, stone)
	srv.TellBlockChanges([3]int32{1, 90, 2}, [3]int32{1, 70, 2})
	got := readTold(t, srv, c)
	if len(got) != 2 || got[0].section() || got[1].section() {
		t.Fatalf("got %+v, want two Block Updates", got)
	}
	if got[0].cells[0].Y != 70 || got[1].cells[0].Y != 90 {
		t.Errorf("got y %d then %d, want 70 then 90", got[0].cells[0].Y, got[1].cells[0].Y)
	}
}

// A block entity with an update packet follows its block's update, in the
// single and the section case alike.
// vanilla: ChunkHolder.broadcastBlockEntityIfNeeded
func TestABlockEntityFollowsItsBlockUpdate(t *testing.T) {
	t.Parallel()
	srv, c := tellRig(t)
	sign := servertest.BlockState(t, "minecraft:oak_sign")
	stone := servertest.BlockState(t, "minecraft:stone")

	srv.SetBlock(1, 70, 2, sign)
	srv.EnsureBlockEntityInDimension("minecraft:overworld", 1, 70, 2, world.KindSign)
	srv.TellBlockChanges([3]int32{1, 70, 2})
	got := readTold(t, srv, c)
	if len(got) != 2 || got[0].id != protocol.IDPlayBlockUpdate || got[1].id != protocol.IDPlayBlockEntityData {
		t.Fatalf("single: got %+v, want a Block Update then Block Entity Data", got)
	}

	srv.SetBlock(1, 70, 2, sign)
	srv.SetBlock(3, 70, 2, stone)
	srv.TellBlockChanges([3]int32{1, 70, 2}, [3]int32{3, 70, 2})
	got = readTold(t, srv, c)
	if len(got) != 2 || got[0].id != protocol.IDPlaySectionBlocks || got[1].id != protocol.IDPlayBlockEntityData {
		t.Fatalf("section: got %+v, want a Section Blocks Update then Block Entity Data", got)
	}
}

// A change a handler makes after the tick's flush goes out ahead of the ack
// the same tick sends.
// vanilla: ServerGamePacketListenerImpl.tick, MinecraftServer.tickChildren
func TestALateChangeArrivesBeforeTheAck(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Warden")
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.StopTick()
	srv.SetBlock(2, 64, 0, stone)
	srv.SetBeforeAckHook(func() {
		srv.SetBlock(5, 70, 6, stone)
		srv.TellBlockChanges([3]int32{5, 70, 6})
	})
	servertest.HoldTicks(t, srv)
	readTold(t, srv, c)

	c.BreakBlock(2, 64, 0)
	c.Settle()
	got := readTold(t, srv, c)
	late := slices.IndexFunc(got, func(p told) bool {
		return p.id == protocol.IDPlayBlockUpdate && len(p.cells) == 1 && p.cells[0].X == 5
	})
	ack := slices.IndexFunc(got, func(p told) bool { return p.id == protocol.IDPlayAckBlockChange })
	if late < 0 || ack < 0 || late > ack {
		t.Fatalf("got %+v, want the late update (at %d) before the ack (at %d)", got, late, ack)
	}
}
