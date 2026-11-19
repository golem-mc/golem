package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// progressEventsByTick runs n ticks one at a time and returns the event and
// data of each destroy-progress World Event the client was sent in each.
func progressEventsByTick(t *testing.T, c *servertest.Client, srv *server.Server, n int) [][][2]int32 {
	t.Helper()
	out := make([][][2]int32, n)
	for i := range out {
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			if id != protocol.IDPlayWorldEvent {
				return false
			}
			r := protocol.NewReader(data)
			ev := r.ReadI32()
			if ev == protocol.WorldEventDestroyProgress || ev == protocol.WorldEventDestroyProgressSound {
				r.ReadPosition()
				out[i] = append(out[i], [2]int32{ev, r.ReadI32()})
			}
			return false
		})
	}
	return out
}

// TestAHeldDigBroadcastsProgressEventsToTheDigger: every tick of a held dig
// sends 2019, or 2020 when the ticks spent are a multiple of four, carrying
// the dig's face, and CHANGE_DESTROY_DIRECTION changes the face.
// vanilla: ServerPlayerGameMode.tick
func TestAHeldDigBroadcastsProgressEventsToTheDigger(t *testing.T) {
	t.Parallel()
	srv, miner, _, _ := crackScene(t)
	// A stick digs stone too slowly to finish within the test.
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:stick"), Count: 1})
	d := servertest.DigTarget
	miner.StartDigging(d[0], d[1], d[2])
	miner.Settle()
	got := progressEventsByTick(t, miner, srv, 8)
	for i, evs := range got {
		if len(evs) != 1 {
			t.Fatalf("tick %d sent %v, want one progress event", i, evs)
		}
		want := int32(protocol.WorldEventDestroyProgress)
		if (i+1)%4 == 0 { // the first pass is one tick past the START
			want = protocol.WorldEventDestroyProgressSound
		}
		if evs[0][0] != want || evs[0][1] != 0 {
			t.Errorf("tick %d sent %v, want event %d data 0", i, evs[0], want)
		}
	}

	miner.ChangeDestroyDirection(d[0], d[1], d[2], 3)
	miner.Settle()
	for i, evs := range progressEventsByTick(t, miner, srv, 2) {
		if len(evs) != 1 || evs[0][1] != 3 {
			t.Errorf("tick %d after the re-aim sent %v, want data 3", i, evs)
		}
	}
}
