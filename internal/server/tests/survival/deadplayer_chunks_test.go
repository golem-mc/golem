package survival

import (
	"fmt"
	"slices"
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

// TestADeadPlayersChunkGetsNoRandomTicks: the dead player's chunk leaves the
// random tick pass while a living player far away keeps theirs.
// vanilla: ChunkMap.removeEntity, ServerChunkCache.tickChunks
func TestADeadPlayersChunkGetsNoRandomTicks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Victim", "Other"))
	victim := connect()
	victim.JoinAs("Victim")
	other := connect()
	other.Join("Other")
	other.Settle()
	servertest.HoldTicks(t, srv)

	const farX, farZ = 640, 0
	other.ChatCommand(fmt.Sprintf("tp @s %d.5 65 %d.5", farX, farZ))
	for range 2000 {
		if srv.ChunkLoadedInDimension(world.DimensionName, farX, farZ) {
			break
		}
		other.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
		victim.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
		<-srv.ChunkBeat()
	}
	p := srv.PlayerPosition("Victim")
	own := world.ChunkPos{X: world.ChunkOf(p.X), Z: world.ChunkOf(p.Z)}
	far := world.ChunkPos{X: farX / 16, Z: farZ / 16}
	if got := srv.RandomTickChunkPositions(); !slices.Contains(got, own) || !slices.Contains(got, far) {
		t.Fatalf("before the death own %v and far %v should both tick, got %d chunks", own, far, len(got))
	}

	victim.ChatCommand("kill")
	victim.Settle()
	servertest.RunTicks(t, srv, 25)
	got := srv.RandomTickChunkPositions()
	if slices.Contains(got, own) {
		t.Errorf("the dead player's chunk %v still gets random ticks", own)
	}
	if !slices.Contains(got, far) {
		t.Errorf("the living player's chunk %v lost its random ticks", far)
	}
}
