package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// noStage is the byte a Set Block Destroy Stage carries to clear the crack.
const noStage = 255

type crackPacket struct {
	entity  int32
	x, y, z int32
	stage   int32
}

// cracksOf runs n ticks and returns the Set Block Destroy Stage packets the
// client was sent in them.
func cracksOf(t *testing.T, c *servertest.Client, srv *server.Server, n int) []crackPacket {
	t.Helper()
	var got []crackPacket
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlayBlockDestruction {
			return false
		}
		r := protocol.NewReader(data)
		var p crackPacket
		p.entity = r.ReadVarInt()
		p.x, p.y, p.z = r.ReadPosition()
		p.stage = int32(r.ReadU8())
		if err := r.Err(); err != nil || r.Remaining() != 0 {
			t.Fatalf("block destruction packet % X: err %v, %d bytes left", data, err, r.Remaining())
		}
		got = append(got, p)
		return false
	})
	return got
}

// crackScene joins a survival miner holding a diamond pickaxe beside stone,
// and a watcher beside them, on a held loop.
func crackScene(t *testing.T) (srv *server.Server, miner, watcher *servertest.Client, minerID int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	miner = connect()
	minerID = miner.JoinAs("Miner")
	watcher = connect()
	watcher.JoinAs("Watcher")
	miner.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_pickaxe"), Count: 1})
	d := servertest.DigTarget
	if !srv.SetBlock(d[0], d[1], d[2], servertest.BlockState(t, "minecraft:stone")) {
		t.Fatal("could not stand the stone")
	}
	miner.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	return srv, miner, watcher, minerID
}

// stagesByTick runs n ticks one at a time and returns the stage of each crack
// packet the client was sent in each.
func stagesByTick(t *testing.T, c *servertest.Client, srv *server.Server, n int) [][]int32 {
	t.Helper()
	out := make([][]int32, n)
	for i := range out {
		for _, p := range cracksOf(t, c, srv, 1) {
			out[i] = append(out[i], p.stage)
		}
	}
	return out
}

func sameStages(a, b [][]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

// TestADigShowsRisingCracksToOtherPlayers: the start of a survival dig shows
// the first stage to a player nearby and each tick that raises it shows the
// next, none going out twice; the digger is sent nothing.
// A diamond pickaxe takes 8/1.5/30 of the stone a tick, so the stage after k
// ticks is (int)(10*k*0.1778).
// vanilla: ServerPlayerGameMode.incrementDestroyProgress
func TestADigShowsRisingCracksToOtherPlayers(t *testing.T) {
	t.Parallel()
	srv, miner, watcher, minerID := crackScene(t)
	d := servertest.DigTarget

	miner.StartDigging(d[0], d[1], d[2])
	miner.Settle()
	got := cracksOf(t, watcher, srv, 0)
	if len(got) != 1 || got[0].stage != 1 || got[0].entity != minerID || got[0].x != d[0] || got[0].y != d[1] || got[0].z != d[2] {
		t.Fatalf("the start sent the watcher %+v, want one crack for entity %d at %v", got, minerID, d)
	}
	miner.Settle()
	if own := cracksOf(t, miner, srv, 0); len(own) != 0 {
		t.Errorf("the digger was sent crack stages %+v, want none", own)
	}
	// The start's stage 1 went out above; each tick after it shows the next
	// stage; past 9 the client draws none but the number keeps rising,
	// as the progress does.
	want := [][]int32{{3}, {5}, {7}, {8}, {10}, {12}, {14}}
	if gotTicks := stagesByTick(t, watcher, srv, 7); !sameStages(gotTicks, want) {
		t.Errorf("the stages by tick = %v, want %v", gotTicks, want)
	}
}

// TestACancelledDigClearsTheCrack: letting go of the block sends stage -1.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction ABORT_DESTROY_BLOCK
func TestACancelledDigClearsTheCrack(t *testing.T) {
	t.Parallel()
	srv, miner, watcher, minerID := crackScene(t)
	d := servertest.DigTarget

	miner.StartDigging(d[0], d[1], d[2])
	miner.Settle()
	cracksOf(t, watcher, srv, 1)
	miner.CancelDigging(d[0], d[1], d[2])
	miner.Settle()
	got := cracksOf(t, watcher, srv, 0)
	if len(got) != 1 || got[0].stage != noStage || got[0].entity != minerID || got[0].x != d[0] {
		t.Errorf("after the cancel the watcher was sent %+v, want one stage -1 for the digger", got)
	}
}

// TestAFinishedDigClearsTheCrack: the finishing click that breaks the block
// sends stage -1, and the block goes.
// vanilla: ServerPlayerGameMode.handleBlockBreakAction STOP_DESTROY_BLOCK
func TestAFinishedDigClearsTheCrack(t *testing.T) {
	t.Parallel()
	srv, miner, watcher, _ := crackScene(t)
	d := servertest.DigTarget

	miner.StartDigging(d[0], d[1], d[2])
	miner.Settle()
	// Three ticks in, the progress is 4 ticks of 0.178: past the 0.7 a
	// finishing click breaks at.
	cracksOf(t, watcher, srv, 3)
	miner.StopDigging(d[0], d[1], d[2])
	miner.Settle()
	got := cracksOf(t, watcher, srv, 0)
	if len(got) != 1 || got[0].stage != noStage {
		t.Errorf("after the finishing click the watcher was sent %+v, want one stage -1", got)
	}
	servertest.ExpectAir(t, srv, d)
}

// TestACrackReachesOnlyNearbyPlayersOfTheSameDimension: a player 32 blocks
// from the block or more, and one in another dimension, see no crack.
// vanilla: ServerLevel.destroyBlockProgress
func TestACrackReachesOnlyNearbyPlayersOfTheSameDimension(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	join := func(name string) *servertest.Client {
		c := connect()
		c.JoinAs(name)
		return c
	}
	miner, near, far, nether := join("Miner"), join("Near"), join("Far"), join("Nether")
	miner.MoveToGround(0.5, 64, 0.5, true)
	// Short hops, each under the move check's limit.
	for _, x := range []float64{9.5, 18.5, 27.5, 36.5} {
		far.MoveToGround(x, 64, 0.5, true)
		far.Settle()
	}
	srv.MovePlayerToDimension("Nether", "minecraft:the_nether", 2, 64, 0)
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_pickaxe"), Count: 1})
	d := servertest.DigTarget
	if !srv.SetBlock(d[0], d[1], d[2], servertest.BlockState(t, "minecraft:stone")) {
		t.Fatal("could not stand the stone")
	}
	for _, c := range []*servertest.Client{miner, near, far, nether} {
		c.Settle()
	}
	if p := srv.PlayerPosition("Far"); p.X < 36 {
		t.Fatalf("the far watcher stands at x=%v, want it past 36", p.X)
	}
	servertest.HoldTicks(t, srv)

	miner.StartDigging(d[0], d[1], d[2])
	miner.Settle()
	if got := cracksOf(t, near, srv, 2); len(got) == 0 {
		t.Fatal("the near watcher was sent no crack")
	}
	for name, c := range map[string]*servertest.Client{"far": far, "other-dimension": nether} {
		if got := cracksOf(t, c, srv, 0); len(got) != 0 {
			t.Errorf("the %s watcher was sent %+v, want none", name, got)
		}
	}
}
