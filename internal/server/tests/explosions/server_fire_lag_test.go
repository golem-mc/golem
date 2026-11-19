package explosions

import (
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The fire spread's cost, black-box: the same wooden site the benchmark
// burns — a 32x32 plank floor with a log pillar and leaf canopy on every
// eighth cell — built through the commands an operator has, lit at all
// sixteen canopies at once, and run on the hand-driven engine with the
// fire's dice pinned. What the test pins is the wire the burn produces:
// the block updates arrive one per changed cell whatever the engine's
// cost, so their count says the burn played out the same, while the
// relight boxes the batch path books against the columns a per-column
// relight would book say how the burn was billed.

// burnWindow is how many engine ticks the driven burn runs: the fires'
// first visits come due thirty-some ticks in and the blaze compounds from
// there, so the window runs from the spark through the busy phase.
const burnWindow = 240

// burnSitePillars is where the site's trees stand: on every cell whose x
// and z are both 4 mod 8 over the 32x32 site.
var burnSitePillars = [][2]int32{
	{4, 4}, {12, 4}, {20, 4}, {28, 4},
	{4, 12}, {12, 12}, {20, 12}, {28, 12},
	{4, 20}, {12, 20}, {20, 20}, {28, 20},
	{4, 28}, {12, 28}, {20, 28}, {28, 28},
}

// fillFeedbackWait is how long a setup fill waits for its success feedback:
// one fill places up to a thousand blocks, and a runner sharing its cores
// with the rest of the packages can spend longer on a fill than the default
// command wait allows.
const fillFeedbackWait = time.Minute

// fillCmd lays a /fill, the way an operator would, and waits for the
// feedback that says it landed. count is the cells the fill was expected
// to change, its feedback's argument; a fill that skips cells for being
// unloaded does not land at all where the test means to build.
func fillCmd(t *testing.T, c *servertest.Client, x0, y0, z0, x1, y1, z1 int32, block, mode string, count int64) {
	t.Helper()
	command := "fill " +
		strconv.FormatInt(int64(x0), 10) + " " + strconv.FormatInt(int64(y0), 10) + " " +
		strconv.FormatInt(int64(z0), 10) + " " +
		strconv.FormatInt(int64(x1), 10) + " " + strconv.FormatInt(int64(y1), 10) + " " +
		strconv.FormatInt(int64(z1), 10) + " " + block
	if mode != "" {
		command += " " + mode
	}
	c.ChatCommand(command)
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fill.success",
		protocol.Text{Number: &count})
}

// buildLagSite writes the burn site through /fill and /setblock: the plank
// floor, then a leaf canopy with the log pillar driven through its middle
// on each of the sixteen positions. The leaves are persistent, the way a
// placeable leaf is: the burn is the fire's own dice on it, not the decay
// a fireless canopy would come apart by.
func buildLagSite(t *testing.T, c *servertest.Client) {
	t.Helper()
	fillCmd(t, c, 0, 64, 0, 31, 64, 31, "minecraft:oak_planks", "", 1024)
	for _, p := range burnSitePillars {
		fillCmd(t, c, p[0]-2, 67, p[1]-2, p[0]+2, 70, p[1]+2,
			"minecraft:oak_leaves[persistent=true]", "", 100)
		fillCmd(t, c, p[0], 66, p[1], p[0], 70, p[1], "minecraft:oak_log", "", 5)
	}
}

// countPackets reads and counts Block Update, Update Section Blocks and
// Update Light packets within n ticks, and reports whether any arrived. Reading
// them out as a burn drives keeps the socket buffer from filling and
// blocking a writer mid-packet, which a whole burn's burst left to the end
// can do under a loaded race run.
func countPackets(t *testing.T, srv *server.Server, c *servertest.Client, n int) (blockUpdates, sectionBlocks, lightUpdates int, saw bool) {
	t.Helper()
	c.ReadTicks(srv, n, func(id int32, _ []byte) bool {
		saw = true
		switch id {
		case protocol.IDPlayBlockUpdate:
			blockUpdates++
		case protocol.IDPlaySectionBlocks:
			sectionBlocks++
		case protocol.IDPlayUpdateLight:
			lightUpdates++
		}
		return false
	})
	return
}

// drainUntilQuiet reads and counts packets until a whole window passes
// with none, and reports how many Block Update, Update Section Blocks and
// Update Light packets the run sent meanwhile. The burn's light work runs
// on the pool behind the ticks, so the drain is what waits for the last
// of it — that is where a backlog the ticks left shows up.
func drainUntilQuiet(t *testing.T, srv *server.Server, c *servertest.Client) (blockUpdates, sectionBlocks, lightUpdates int) {
	t.Helper()
	// The drain counts ticks, so the loop is held for it, and frozen so its
	// ticks run the players alone: the burn moves only on the passes the
	// test drives, so the drain can go quiet. The loop is stopped again
	// after, as the passes driven by hand expect.
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	defer func() {
		srv.StopTick()
		srv.SetTickFrozen(false)
	}()
	for range 200 {
		bu, sb, lu, saw := countPackets(t, srv, c, 8)
		blockUpdates, sectionBlocks, lightUpdates = blockUpdates+bu, sectionBlocks+sb, lightUpdates+lu
		if !saw {
			return blockUpdates, sectionBlocks, lightUpdates
		}
	}
	t.Fatal("packets kept arriving for the whole drain")
	return
}

// TestAFireSpreadKeepsLightBilledPerChunk pins the burn's cost end to end:
// a seeded blaze at its busiest must book a relight per chunk the changes
// span, not one per changed column, while burning the very same blocks,
// which the block update count pins exactly. It fails where every changed
// column is relit on its own; see BenchmarkFireSpread for the same burn's
// before and after.
func TestAFireSpreadKeepsLightBilledPerChunk(t *testing.T) {
	// Not parallel: the burn's packets are timed out by the quiet windows
	// the drain waits in, and the cores beside the rest of a parallel
	// batch are not to be had.
	srv, c := servertest.Join(t, fireConfig(), "Warden", servertest.OpsListing(t, "Warden"))

	// The loop stops before the site is built: the only ticks that run
	// over it are the ones the test drives, so the burn is the engine's
	// own dice on a world nothing else has ticked, the same every run
	// whatever the wall clock spent on the way here.
	srv.StopTick()
	servertest.SeedPhysics(t, srv, burnSeed)
	buildLagSite(t, c)
	for _, p := range burnSitePillars {
		setBlockCmd(t, c, p[0]-1, 68, p[1], "fire")
	}

	// The warmup settles what the site's own wakes book — the leaf
	// distances the fills left at seven recomputed — and carries the
	// fires into their first visits. None of it is counted.
	srv.TickPhysics(40)
	drainUntilQuiet(t, srv, c)

	// What the window's changes are billed: the relight boxes the batch
	// path books against the boxes a relight per changed column would
	// book for the same changes. Counting the bookings, not the wire,
	// keeps this off the light pool's timing: a drained pool sends the
	// same chunks' light however many times a per-column path recomputes
	// it.
	var booked, columns, chunks int
	srv.RecordRelights(func(n int, changes []world.BlockChange) {
		booked += n
		cols := make(map[[2]int32]struct{}, len(changes))
		chs := make(map[[2]int32]struct{}, len(changes))
		for _, ch := range changes {
			cols[[2]int32{ch.X, ch.Z}] = struct{}{}
			chs[[2]int32{world.ChunkOf(float64(ch.X)), world.ChunkOf(float64(ch.Z))}] = struct{}{}
		}
		columns += len(cols)
		chunks += len(chs)
	})

	// The window drives the blaze for real. The burn's light work runs on
	// the pool beside the ticks; reading each tick's packets as it goes
	// keeps the buffer from holding a whole burn, whose burst left to the
	// end can block a writer mid-packet under a loaded race run.
	start := time.Now()
	var blockUpdates, sectionBlocks, lightUpdates int
	for range burnWindow {
		srv.TickPhysics(1)
		bu, sb, lu, _ := countPackets(t, srv, c, 0)
		blockUpdates, sectionBlocks, lightUpdates = blockUpdates+bu, sectionBlocks+sb, lightUpdates+lu
	}
	driveTime := time.Since(start)

	bu, sb, lu := drainUntilQuiet(t, srv, c)
	blockUpdates, sectionBlocks, lightUpdates = blockUpdates+bu, sectionBlocks+sb, lightUpdates+lu
	var fires, leaves, logs, planksLeft int
	for y := 64; y <= 70; y++ {
		for x := range int32(32) {
			for z := range int32(32) {
				name, _, _ := registry.BlockStateName(srv.BlockAt(x, int32(y), z))
				switch name {
				case "minecraft:fire":
					fires++
				case "minecraft:oak_leaves":
					leaves++
				case "minecraft:oak_log":
					logs++
				case "minecraft:oak_planks":
					planksLeft++
				}
			}
		}
	}
	t.Logf("burn: drove %d ticks in %s (%.1f ms/tick), %d block updates, %d section blocks, %d light updates; %d relights booked over %d chunks the changes touched, %d columns a per-column relight would book; site ends with %d fires, %d leaves, %d logs, %d planks",
		burnWindow, driveTime, float64(driveTime.Milliseconds())/float64(burnWindow),
		blockUpdates, sectionBlocks, lightUpdates,
		booked, chunks, columns, fires, leaves, logs, planksLeft)

	// The burn itself is unchanged by how its work is billed: the changes
	// the engine made are the same dice on the same world, one packet per
	// changed section per tick, so their count is a constant of the site and
	// seed.
	if got := blockUpdates + sectionBlocks; got != burnChanges {
		t.Errorf("burn made %d block packets over the wire, want %d — the burn changed with the billing",
			got, burnChanges)
	}
	// The light the burn billed: one relight box per chunk each batch of
	// changes touches, not one per changed column. The count is measured
	// against the per-column baseline the same changes give on this
	// machine, so it is the billing and not the pool's timing.
	if booked != chunks {
		t.Errorf("burn booked %d relights over %d chunks the changes touched, want one per chunk",
			booked, chunks)
	}
	if booked*burnRelightRatio > columns {
		t.Errorf("burn booked %d relights, want well under the %d columns a relight per changed column would book",
			booked, columns)
	}
}

// burnChanges and burnRelightRatio are the seeded burn's own numbers: the
// constant the block packets of the window are (the same site, the same
// seed, the same dice, whatever relights behind them), and the least the
// columns a per-column relight would book outnumber the chunks a per-chunk
// relight does.
const (
	burnChanges      = 364
	burnRelightRatio = 2
)

// burnSeed pins the dice of a burn busy enough to span many chunks per
// tick: some seeds burn out thin, where each tick's changes touch one
// chunk and a per-column relight books little more than a per-chunk one.
const burnSeed = 4
