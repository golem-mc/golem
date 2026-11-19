package blockchanges

import (
	"math/bits"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// edgeRig joins a builder and a watcher whose view has chunk (0, 0) on its
// border, and holds the tick.
func edgeRig(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), setup...)
	builder := connect().Join("Builder")
	edge := servertest.JoinAtViewEdge(t, connect, "Edge")
	servertest.HoldTicks(t, srv)
	readTold(t, srv, builder)
	readPackets(t, srv, edge)
	return srv, builder, edge
}

// readPackets runs one held tick and returns the ids of the light and block
// packets that reached c, in wire order, with the cells a block packet carries.
func readPackets(t *testing.T, srv *server.Server, c *servertest.Client) []told {
	t.Helper()
	var out []told
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayUpdateLight:
			out = append(out, told{id: id})
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			out = append(out, told{id: id, cells: servertest.BlockChangesIn(t, id, data)})
		}
		return false
	})
	return out
}

func countID(got []told, id int32) int {
	n := 0
	for _, p := range got {
		if p.id == id {
			n++
		}
	}
	return n
}

// Two engine writes in one tick write two cells of a section: they go out once, as
// one Section Blocks Update.
// vanilla: ChunkHolder.broadcastChanges
func TestTwoEngineWritesOfASectionArriveAsOneSectionBlocksUpdate(t *testing.T) {
	t.Parallel()
	srv, builder, _ := edgeRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlockAndTell(1, 70, 2, stone)
	srv.SetBlockAndTell(3, 71, 4, stone)
	got := readTold(t, srv, builder)
	if len(got) != 1 || !got[0].section() || len(got[0].cells) != 2 {
		t.Fatalf("got %+v, want one Section Blocks Update of two cells", got)
	}
	for _, cell := range got[0].cells {
		if cell.State != stone {
			t.Errorf("cell %+v carries state %d, want stone %d", cell, cell.State, stone)
		}
	}
}

// A cell the engine writes twice in a tick goes out once with its last state.
func TestACellWrittenTwiceByTheEngineArrivesOnceWithItsFinalState(t *testing.T) {
	t.Parallel()
	srv, builder, _ := edgeRig(t)
	dirt := servertest.BlockState(t, "minecraft:dirt")
	srv.SetBlockAndTell(1, 70, 2, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlockAndTell(1, 70, 2, dirt)
	got := readTold(t, srv, builder)
	want := []servertest.BlockChange{{X: 1, Y: 70, Z: 2, State: dirt}}
	if len(got) != 1 || got[0].section() || !slices.Equal(got[0].cells, want) {
		t.Fatalf("got %+v, want one Block Update %+v", got, want)
	}
}

// The light a change's relight computes goes out at the flush, before the
// block packets of that flush.
// vanilla: ChunkHolder.broadcastChanges
func TestLightArrivesBeforeTheBlockPacketsOfItsFlush(t *testing.T) {
	t.Parallel()
	srv, _, edge := edgeRig(t)
	srv.SetBlockAndTell(1, 70, 2, servertest.BlockState(t, "minecraft:stone"))
	srv.WaitRelights()
	got := readPackets(t, srv, edge)
	if len(got) != 2 || got[0].id != protocol.IDPlayUpdateLight || got[1].id != protocol.IDPlayBlockUpdate {
		t.Fatalf("got %+v, want an Update Light then a Block Update", got)
	}
}

// Relights that finish within one tick are told once per chunk.
func TestRelightsOfOneTickSendOneLightUpdatePerChunk(t *testing.T) {
	t.Parallel()
	srv, _, edge := edgeRig(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlockAndTell(1, 70, 2, stone)
	srv.WaitRelights()
	srv.SetBlockAndTell(6, 70, 7, stone)
	srv.WaitRelights()
	got := readPackets(t, srv, edge)
	if n := countID(got, protocol.IDPlayUpdateLight); n != 1 {
		t.Fatalf("got %d Update Light packets in %+v, want 1", n, got)
	}
	if got[0].id != protocol.IDPlayUpdateLight {
		t.Errorf("got %+v, want the Update Light first", got)
	}
	if n := countID(got, protocol.IDPlaySectionBlocks); n != 1 {
		t.Errorf("got %d Section Blocks Updates in %+v, want 1", n, got)
	}
}

// lightMasks reads an Update Light payload's chunk position and four masks.
func lightMasks(t *testing.T, data []byte) (x, z int32, sky, block, emptySky, emptyBlock uint64) {
	t.Helper()
	r := protocol.NewReader(data)
	x, z = r.ReadVarInt(), r.ReadVarInt()
	set := r.ReadBitSet
	return x, z, set(), set(), set(), set()
}

// Placing a torch tells only the sections the torch lit: no sky section, and
// a few block sections of the 26.
// vanilla: ClientboundLightUpdatePacketData(ChunkPos, LevelLightEngine, BitSet, BitSet)
func TestATorchSendsLightOfTheNearbySectionsOnly(t *testing.T) {
	t.Parallel()
	srv, _, edge := edgeRig(t)
	srv.SetBlockAndTell(1, 70, 2, servertest.BlockState(t, "minecraft:torch"))
	srv.WaitRelights()
	var sky, block, emptySky, emptyBlock uint64
	lights := 0
	edge.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id == protocol.IDPlayUpdateLight {
			var x, z int32
			x, z, sky, block, emptySky, emptyBlock = lightMasks(t, data)
			if x == 0 && z == 0 {
				lights++
			}
		}
		return false
	})
	if lights != 1 {
		t.Fatalf("got %d Update Light packets, want 1", lights)
	}
	if sky|emptySky != 0 {
		t.Errorf("sky masks %#b / %#b, want none: a torch changes no sky light", sky, emptySky)
	}
	if block == 0 || bits.OnesCount64(block|emptyBlock) > 4 {
		t.Errorf("block masks %#b / %#b, want a few sections of the 26", block, emptyBlock)
	}
}

// A bucket pour is told at the flush, and two pours in one section in one
// tick arrive as one Update Section Blocks.
// vanilla: ChunkHolder.broadcastChanges
func TestTwoBucketPoursOfASectionArriveAsOneSectionBlocksUpdate(t *testing.T) {
	t.Parallel()
	srv, builder, edge := edgeRig(t)
	water := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1}
	builder.MoveStep(1.5, 64, 2.5, true)
	// Straight down, then 45 degrees south: two cells of one section. Zero
	// ticks run between the uses, only the wait for the server to handle each.
	for _, pitch := range []float32{90, 45} {
		srv.GiveSlot("Builder", player.HotbarSlot, water)
		servertest.UseHeld(builder, 0, pitch)
		builder.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	}
	got := readPackets(t, srv, edge)
	if n := countID(got, protocol.IDPlayBlockUpdate); n != 0 {
		t.Errorf("got %d Block Updates in %+v, want the pours in one Section Blocks Update", n, got)
	}
	if n := countID(got, protocol.IDPlaySectionBlocks); n != 1 {
		t.Fatalf("got %d Section Blocks Updates in %+v, want 1", n, got)
	}
}

// A bed taken by a sleeper is told at the flush: both halves, one section.
// vanilla: ChunkHolder.broadcastChanges
func TestAnOccupiedBedArrivesAtTheFlush(t *testing.T) {
	t.Parallel()
	srv, builder, edge := edgeRig(t)
	servertest.StandSleepBed(t, srv)
	servertest.SetNight(t, srv)
	builder.MoveTo(6.5, 64, 0.5, 0, 0)
	builder.UseBlock(servertest.SleepBedHeadX, servertest.SleepBedHeadY, servertest.SleepBedHeadZ)
	// The watcher's barrier cannot wait for the builder's click handler:
	// its tick could flush between the head and foot writes. Settle the
	// builder before the watcher advances the held tick.
	builder.Settle()
	got := readPackets(t, srv, edge)
	if n := countID(got, protocol.IDPlaySectionBlocks); n != 1 {
		t.Fatalf("got %+v, want one Section Blocks Update for the bed", got)
	}
}

// A /setblock with a block entity tag arrives as the Block Update, then the
// block entity's data, as the flush sends them.
// vanilla: ChunkHolder.broadcastChanges
func TestSetblockWithATagSendsTheBlockThenItsEntityData(t *testing.T) {
	t.Parallel()
	srv, builder, edge := edgeRig(t, servertest.OpsListing(t, "Builder"))
	builder.ChatCommand("setblock 1 70 2 minecraft:oak_sign{is_waxed:1b}")
	// The watcher's barrier cannot acknowledge the builder's command.
	builder.Settle()
	var ids []int32
	edge.ReadTicks(srv, 1, func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayBlockEntityData:
			ids = append(ids, id)
		}
		return false
	})
	if len(ids) != 2 || ids[0] != protocol.IDPlayBlockUpdate || ids[1] != protocol.IDPlayBlockEntityData {
		t.Fatalf("got packets %v, want a Block Update then Block Entity Data", ids)
	}
}

// A chunk sent before its outer neighbour loaded is told the light that
// neighbour's load spills into it, though it is no longer on the border of the
// view: the client cannot work that light out from any block update.
// vanilla: ChunkHolder.sectionLightChanged, ChunkHolder.broadcastChanges
func TestALoadingNeighbourSendsTheLightItSpillsIntoASentChunk(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect().Join("Walker")
	c.MoveTo(8.5, 64, 8.5, 0, 0)
	servertest.AwaitChunksSettled(t, srv)
	servertest.HoldTicks(t, srv)
	// The air cell (79, 200, 8) is closed on every side but east, toward the
	// unloaded chunk (5, 0).
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, p := range [][3]int32{{79, 201, 8}, {79, 199, 8}, {78, 200, 8}, {79, 200, 7}, {79, 200, 9}} {
		srv.SetBlockAndTell(p[0], p[1], p[2], stone)
	}
	srv.WaitRelights()
	readPackets(t, srv, c)
	if got := srv.SkyLightAt(79, 200, 8); got != 0 {
		t.Fatalf("sky light in the closed cell is %d before chunk (5, 0) loads, want 0", got)
	}

	c.MoveTo(24.5, 64, 8.5, 0, 0)
	const section = (200+64)/16 + 1
	told := false
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateLight {
			return false
		}
		x, z, sky, _, _, _ := lightMasks(t, data)
		told = x == 4 && z == 0 && sky&(1<<section) != 0
		return told
	})
	if !told {
		t.Fatalf("no Update Light for chunk (4, 0) with sky section %d within 40 ticks", section)
	}
	if got := srv.SkyLightAt(79, 200, 8); got != 14 {
		t.Errorf("sky light in the cell open toward chunk (5, 0) is %d, want 14", got)
	}
}
