package explosions

import (
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
