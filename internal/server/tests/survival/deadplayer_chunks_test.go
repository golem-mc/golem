package survival

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// ingots counts the iron ingots in a furnace's output slot.
func ingots(t *testing.T, srv *server.Server, x, y, z int32) int {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatalf("no block entity at %d %d %d", x, y, z)
	}
	list, _ := nbt.Get[nbt.List](tag, "Items")
	for _, it := range list.Items {
		item, _ := it.(nbt.Compound)
		if slot, _ := nbt.ByteField(item, "Slot"); slot != 2 {
			continue
		}
		n, _ := nbt.IntField(item, "count")
		return int(n)
	}
	return 0
}

// TestADeadPlayersChunksAreDroppedAndComeBackOnRespawn: twenty ticks after the
// death the player's chunk tickets are gone, so the furnace beside the body
// stops smelting and the chunk unloads; the respawn sends the chunks again and
// the furnace resumes.
// vanilla: LivingEntity.tickDeath, ChunkMap.removeEntity
func TestADeadPlayersChunksAreDroppedAndComeBackOnRespawn(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Victim", servertest.OpsListing(t, "Victim"))
	servertest.HoldTicks(t, srv)
	p := srv.PlayerPosition("Victim")
	x, y, z := int32(p.X)+2, int32(p.Y), int32(p.Z)
	c.ChatCommand(fmt.Sprintf(`setblock %d %d %d furnace{Items:[{Slot:0b,id:"raw_iron",count:8},{Slot:1b,id:"coal",count:2}]}`, x, y, z))
	c.Settle()
	servertest.RunTicks(t, srv, 1)

	c.ChatCommand("kill")
	c.Settle()
	servertest.RunTicks(t, srv, 25)
	// The sender lets the holds go on its own wall-clock beat, so wait on the
	// beats; held ticks run faster than that.
	for range 200 {
		if !srv.ChunkLoadedInDimension(world.DimensionName, x, z) {
			break
		}
		<-srv.ChunkBeat()
	}
	if srv.ChunkLoadedInDimension(world.DimensionName, x, z) {
		t.Fatal("the dead player's chunk is still loaded")
	}
	servertest.RunTicks(t, srv, 375)

	c.PerformRespawn()
	// The sender sends on its own wall-clock beat, so wait on the beats.
	sent := false
	for range 200 {
		if sent = c.ReadTicks(srv, 1, func(id int32, _ []byte) bool { return id == protocol.IDPlayChunkData }); sent {
			break
		}
		<-srv.ChunkBeat()
	}
	if !sent {
		t.Error("no chunk was sent after the respawn")
	}
	// The chunk is reloaded from its save, which holds what the dead player's
	// 400 ticks smelted.
	servertest.UntilTicks(t, srv, 100, "the chunk loads again", func() bool {
		return srv.ChunkLoadedInDimension(world.DimensionName, x, z)
	})
	if got := ingots(t, srv, x, y, z); got > 1 {
		t.Errorf("the furnace smelted %d ingots beside a dead player, want at most 1", got)
	}
	servertest.UntilTicks(t, srv, 400, "the furnace smelts again", func() bool {
		return ingots(t, srv, x, y, z) >= 2
	})
}
