package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math/bits"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestChunkPacketCarriesHeightmaps joins a flat world with one torch standing
// on the ground and reads the three heightmaps off the chunk packet: types
// WORLD_SURFACE, MOTION_BLOCKING and MOTION_BLOCKING_NO_LEAVES. The torch's
// column counts only for WORLD_SURFACE, being a block that does not block
// motion, so the two motion maps still read the stone below it. The same
// packet's light is checked to send only the sky sections that are neither
// uniformly lit nor uniformly dark.
func TestChunkPacketCarriesHeightmaps(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)
	// The torch is placed before the join, so the chunk that reaches the
	// client is the one read back from disk with it.
	srv.SetLoadedBlock(5, 64, 5, registry.BlockStates["minecraft:torch"])

	c := connect()
	c.Login("Testificate")
	c.Configure()

	packet := chunkAt(t, c, 0, 0)
	maps, r := parseHeightmaps(t, packet)

	// The three types vanilla sends, no more and no fewer.
	for _, typ := range []int32{1, 4, 5} {
		if _, ok := maps[typ]; !ok {
			t.Errorf("chunk packet carries no heightmap of type %d", typ)
		}
	}
	if len(maps) != 3 {
		t.Fatalf("chunk packet carries %d heightmaps, want 3", len(maps))
	}

	// The flat ground tops out at y=63, so a plain column reads 63 - (-64) + 1
	// = 128 in every map.
	for _, i := range []int{0, 16, 255} {
		for _, typ := range []int32{1, 4, 5} {
			if got := maps[typ][i]; got != 128 {
				t.Errorf("heightmap %d column %d = %d, want the ground at 128", typ, i, got)
			}
		}
	}
	// The torch's column (5, 5), index z*16+x=85: the torch is the highest
	// block, but does not block motion, so only WORLD_SURFACE sees it.
	torch := 5*16 + 5
	if got := maps[1][torch]; got != 129 {
		t.Errorf("WORLD_SURFACE over the torch = %d, want 129", got)
	}
	for _, typ := range []int32{4, 5} {
		if got := maps[typ][torch]; got != 128 {
			t.Errorf("motion heightmap %d over the torch = %d, want the stone at 128", typ, got)
		}
	}

	checkLightMasksConsistent(t, r)
}

// chunkAt reads through the join and returns the Chunk Data packet for the
// chunk at (cx, cz).
func chunkAt(t *testing.T, c *servertest.Client, cx, cz int32) []byte {
	t.Helper()
	var found []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id != protocol.IDPlayChunkData || found != nil {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() == cx && r.ReadI32() == cz {
			found = data
		}
	})
	if found == nil {
		t.Fatalf("no chunk data for (%d, %d) in the join", cx, cz)
	}
	return found
}

// parseHeightmaps reads the prefixed heightmap array at the head of a chunk
// packet and returns each type's 256 column values, leaving r after them.
func parseHeightmaps(t *testing.T, packet []byte) (map[int32][]int32, *protocol.Reader) {
	t.Helper()
	r := protocol.NewReader(packet)
	r.ReadI32() // chunk x
	r.ReadI32() // chunk z
	count := r.ReadVarInt()
	if count != 3 {
		t.Fatalf("heightmap count = %d, want 3", count)
	}
	bitsPerEntry := bits.Len(uint(384)) // the overworld's 384 blocks
	perLong := 64 / bitsPerEntry
	maps := make(map[int32][]int32, 3)
	for range count {
		typ := r.ReadVarInt()
		longs := r.ReadVarInt()
		if want := int32((256 + perLong - 1) / perLong); longs != want {
			t.Errorf("heightmap %d long count = %d, want %d", typ, longs, want)
		}
		packed := make([]uint64, longs)
		for i := range packed {
			packed[i] = uint64(r.ReadI64())
		}
		if err := r.Err(); err != nil {
			t.Fatalf("heightmap %d: %v", typ, err)
		}
		mask := uint64(1)<<bitsPerEntry - 1
		values := make([]int32, 256)
		for i := range values {
			values[i] = int32(packed[i/perLong] >> ((i % perLong) * bitsPerEntry) & mask)
		}
		maps[typ] = values
	}
	return maps, r
}

// checkLightMasksConsistent walks the light section at the tail of the chunk
// packet (empty block entities) and checks the masks vanilla's rules ask for:
// no section in both a data mask and its empty mask, an array for every bit
// set, and no uniformly lit sky section among them.
func checkLightMasksConsistent(t *testing.T, r *protocol.Reader) {
	t.Helper()
	r.ReadBytes(1 << 24) // the chunk's sections
	if n := r.ReadVarInt(); n != 0 {
		t.Fatalf("chunk carries %d block entities, none expected", n)
	}
	skyMask := readTestBitSet(t, r)
	blockMask := readTestBitSet(t, r)
	emptySky := readTestBitSet(t, r)
	emptyBlock := readTestBitSet(t, r)

	if skyMask&emptySky != 0 {
		t.Error("a section is in both the sky and the empty sky masks")
	}
	if blockMask&emptyBlock != 0 {
		t.Error("a section is in both the block and the empty block masks")
	}
	// The above-world section is uniformly lit: in neither sky mask.
	if skyMask&(1<<25) != 0 || emptySky&(1<<25) != 0 {
		t.Error("uniformly lit above-world section is in a sky mask")
	}

	nSky := r.ReadVarInt()
	if got := int(bits.OnesCount64(skyMask)); int(nSky) != got {
		t.Errorf("sky array count = %d, want %d for the mask", nSky, got)
	}
	if nSky > 4 {
		t.Errorf("flat chunk sent %d sky arrays, want only the mixed sections", nSky)
	}
	for range nSky {
		if arr := r.ReadBytes(2048); len(arr) != 2048 {
			t.Fatalf("sky array length = %d, want 2048", len(arr))
		}
	}
	nBlock := r.ReadVarInt()
	if got := int(bits.OnesCount64(blockMask)); int(nBlock) != got {
		t.Errorf("block array count = %d, want %d for the mask", nBlock, got)
	}
	for range nBlock {
		if arr := r.ReadBytes(2048); len(arr) != 2048 {
			t.Fatalf("block array length = %d, want 2048", len(arr))
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("light section: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("light section has %d bytes left over", r.Remaining())
	}
}

// readTestBitSet reads a BitSet field and returns its low 64 bits.
func readTestBitSet(t *testing.T, r *protocol.Reader) uint64 {
	t.Helper()
	v := r.ReadBitSet()
	if err := r.Err(); err != nil {
		t.Fatalf("BitSet: %v", err)
	}
	return v
}
