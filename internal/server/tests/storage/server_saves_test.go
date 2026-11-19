package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server/world"
)

// saveTestChunks puts one chunk in each region file: (5,5) in r.0.0.mca,
// (-5,5) in r.-1.0.mca, (5,-5) in r.0.-1.mca, (-5,-5) in r.-1.-1.mca — the
// four region files the chunks around spawn share, so saves a barrier did
// not wait out leave at least one of the four unwritten.
var saveTestChunks = []world.ChunkPos{
	{X: 5, Z: 5}, {X: -5, Z: 5}, {X: 5, Z: -5}, {X: -5, Z: -5},
}

// netherRegionPath is the block region file holding a chunk of the nether.
func netherRegionPath(level string, pos world.ChunkPos) string {
	rx, rz := strconv.Itoa(int(pos.X>>5)), strconv.Itoa(int(pos.Z>>5))
	return filepath.Join(level, "dimensions", "minecraft", "the_nether", "region", "r."+rx+"."+rz+".mca")
}

// queueSaveBacklog releases n held, dirty chunks of regions far from
// spawn, whose saves then occupy a world's I/O pool ahead of a later
// release's. Without it, a barrier that fails to wait could still see the
// later saves land by the disk's own timing; behind it, a barrier that
// does not wait leaves them unwritten for sure. The backlog chunk is
// marked dirty by the block it is handed, TouchChunk being the overworld's
// own form.
func queueSaveBacklog(t *testing.T, hold func(x, z int32) func(), mark func(x, y, z int32), n int) {
	t.Helper()
	var releases []func()
	for i := range int32(n) {
		x, z := (40+i)*16+8, int32(200*16+8)
		releases = append(releases, hold(x, z))
		mark(x, 64, z)
	}
	for _, release := range releases {
		release()
	}
}

// TestALeavingPlayersChunksAreSavedBeforeTheConnectionEnds disconnects a
// player from a fresh world and looks at the level directory the moment
// the server has finished with the connection: the departure's chunk
// releases queue async saves, and the connection's end has to wait them
// out, for a caller that waits a departure out may then read or take the
// level directory away without a region file landing in it behind them.
func TestALeavingPlayersChunksAreSavedBeforeTheConnectionEnds(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Saver", servertest.VanillaSaves)

	// Every chunk around the player was generated, so each is unsaved and
	// its release saves it.
	queueSaveBacklog(t, srv.HoldChunk, func(x, y, z int32) { srv.TouchChunk(x, z) }, 200)
	c.Disconnect()

	for _, pos := range saveTestChunks {
		if _, err := os.Stat(servertest.ChunkRegionPath(cfg.LevelName, pos)); err != nil {
			t.Errorf("the connection has ended but chunk (%d,%d)'s region file is not on disk: %v", pos.X, pos.Z, err)
		}
	}
}
