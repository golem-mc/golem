package storage

import (
	"bytes"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// dimensionRegionFile is the path of a chunk's region file in a dimension of
// the world under level: the 26.1 layout keeps every dimension's region/
// under dimensions/<namespace>/<name>.
func dimensionRegionFile(level, name string, pos world.ChunkPos) string {
	dim := strings.TrimPrefix(name, "minecraft:")
	rx, rz := strconv.Itoa(int(pos.X>>5)), strconv.Itoa(int(pos.Z>>5))
	return filepath.Join(level, "dimensions", "minecraft", dim, "region", "r."+rx+"."+rz+".mca")
}

// everyRecordIsLz4 walks a region file's offsets and checks every chunk
// record in it names the lz4 compression and carries the LZ4Block framing,
// the shape a server configured for region-file-compression=lz4 writes.
func everyRecordIsLz4(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	present := 0
	for i := range 1024 {
		offset := binary.BigEndian.Uint32(raw[i*4:])
		if offset == 0 {
			continue
		}
		start := int(offset>>8) * 4096
		if start+5 > len(raw) {
			t.Fatalf("%s: entry %d points past the end of the file", path, i)
		}
		if binary.BigEndian.Uint32(raw[start:]) == 0 {
			continue // allocated, never written
		}
		present++
		if raw[start+4] != 4 {
			t.Errorf("%s: record %d has compression %d, want 4 (lz4)", path, i, raw[start+4])
		}
		if !bytes.HasPrefix(raw[start+5:], []byte("LZ4Block")) {
			t.Errorf("%s: record %d does not start with the LZ4Block magic", path, i)
		}
	}
	if present == 0 {
		t.Fatalf("%s holds no chunk records, so nothing was saved", path)
	}
}

// TestRegionFileCompressionCoversEveryDimension checks the property end to
// end: a server configured for region-file-compression=lz4 writes its chunk
// records as lz4 ones in every dimension, and a restart of it reads them back
// rather than regenerating over them — the loss a chunk it could not decode
// would suffer.
func TestRegionFileCompressionCoversEveryDimension(t *testing.T) {
	// Not parallel: the arrivals generate a dimension's worth of chunks.
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.RegionFileCompression = "lz4"

	srv, c := servertest.Join(t, cfg, "Testificate")
	const x, y, z = int32(0), int32(63), int32(0)
	c.BreakBlock(x, y, z)
	c.ExpectBlockChange()

	gold, ok := registry.FindState("minecraft:gold_block", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:gold_block")
	}
	// A teleport into each of the other dimensions loads the chunks around
	// the arrival, and a gold block takes the change the save needs: on the
	// end's arrival platform itself, whose coordinates the platform
	// settles; in the nether, on the ground at the arrival.
	arrival := world.EndArrival()
	for _, tc := range []struct {
		dim    string
		block  [3]int32
		arrive player.Position
	}{
		{world.NetherName,
			[3]int32{4, 70, 4},
			player.Position{X: 4.5, Y: 70, Z: 4.5}},
		{world.EndName,
			[3]int32{arrival.X, arrival.Y, arrival.Z},
			player.Position{X: float64(arrival.X) + 0.5, Y: float64(arrival.Y), Z: float64(arrival.Z) + 0.5}},
	} {
		srv.MovePlayerToDimension("Testificate", tc.dim, tc.arrive.X, tc.arrive.Y, tc.arrive.Z)
		c.Settle()
		set := false
		// The chunk loads on the sender's wall clock, not the tick's.
		servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
			set = srv.SetBlockInDimension(tc.dim, tc.block[0], tc.block[1], tc.block[2], gold)
			return set
		})
		if !set {
			t.Fatalf("setting the %s gold block failed, so its chunk would not save", tc.dim)
		}
	}
	srv.Shutdown()

	everyRecordIsLz4(t, dimensionRegionFile(cfg.LevelName, world.DimensionName, world.ChunkPos{X: 0, Z: 0}))
	everyRecordIsLz4(t, dimensionRegionFile(cfg.LevelName, world.NetherName, world.ChunkPos{X: 0, Z: 0}))
	endChunk := world.ChunkPos{
		X: world.ChunkOf(float64(arrival.X)),
		Z: world.ChunkOf(float64(arrival.Z)),
	}
	everyRecordIsLz4(t, dimensionRegionFile(cfg.LevelName, world.EndName, endChunk))

	// The restart reads its own lz4 records back: the chunks it loads are
	// the ones that were saved, not regenerated ones, which is what a chunk
	// the server could not decode would appear as.
	restarted := *cfg
	again, err := server.New(&restarted)
	if err != nil {
		t.Fatalf("recreate server: %v", err)
	}
	defer again.Shutdown()
	if got := again.LoadBlockAt(x, y, z); got != 0 {
		t.Errorf("the broken block is state %d after the restart, want the air it was broken to", got)
	}
	if got := again.LoadBlockInDimension(world.NetherName, 4, 70, 4); got != gold {
		t.Errorf("the nether block is state %d after the restart, want the gold block %d", got, gold)
	}
	if got := again.LoadBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z); got != gold {
		t.Errorf("the end block is state %d after the restart, want the gold block %d", got, gold)
	}
}
