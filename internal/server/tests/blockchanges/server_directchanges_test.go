package blockchanges

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// edgeRig joins a builder and a watcher whose view has chunk (0, 0) on its
// border, and holds the tick.
func edgeRig(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), setup...)
	builder := connect().Join("Builder")
	edge := servertest.JoinAtViewEdge(t, srv, connect, "Edge")
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

// lightMasks reads an Update Light payload's chunk position and four masks.
func lightMasks(t *testing.T, data []byte) (x, z int32, sky, block, emptySky, emptyBlock uint64) {
	t.Helper()
	r := protocol.NewReader(data)
	x, z = r.ReadVarInt(), r.ReadVarInt()
	set := r.ReadBitSet
	return x, z, set(), set(), set(), set()
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

// Update Light reaches a player whose view the relit chunk borders, not one
// tracking all eight neighbours of that chunk.
// vanilla: ChunkHolder.broadcastChanges, ChunkMap.isChunkOnTrackedBorder
func TestUpdateLightGoesOnlyToBorderPlayers(t *testing.T) {
	t.Parallel()
	srv, builder, edge := edgeRig(t)
	torch := servertest.BlockState(t, "minecraft:torch")
	srv.SetBlockAndTell(1, 70, 2, torch)
	srv.WaitRelights()

	var edgeLight bool
	edge.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id == protocol.IDPlayUpdateLight {
			if x, z, _, _, _, _ := lightMasks(t, data); x == 0 && z == 0 {
				edgeLight = true
			}
		}
		return false
	})
	if !edgeLight {
		t.Fatal("edge watcher did not receive Update Light for chunk (0, 0)")
	}

	var gotBlock, gotLight bool
	builder.ReadTicks(srv, servertest.QuietTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate:
			for _, cell := range servertest.BlockChangesIn(t, id, data) {
				if cell.X == 1 && cell.Y == 70 && cell.Z == 2 && cell.State == torch {
					gotBlock = true
				}
			}
		case protocol.IDPlayUpdateLight:
			gotLight = true
		}
		return false
	})
	if !gotBlock {
		t.Fatal("middle player did not receive Block Update for the torch")
	}
	if gotLight {
		t.Fatal("middle player received Update Light for an interior chunk")
	}
}
