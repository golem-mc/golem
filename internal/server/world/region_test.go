package world

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestRegionWriteReadRoundTrip checks that a chunk written to a region file
// reads back identically, which is what a save and a load amounts to.
func TestRegionWriteReadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rf, err := openRegion(regionFilePath(dir, 0, 0), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	t.Cleanup(func() { rf.close() })

	data := []byte("hello chunk world")
	if err := rf.writeChunk(ChunkPos{X: 0, Z: 0}, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}

	got, err := rf.readChunk(ChunkPos{X: 0, Z: 0})
	if err != nil {
		t.Fatalf("readChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("readChunk = %q, want %q", got, data)
	}
}

// TestRegionReadsMissingChunkAsNil checks that a chunk that was never written
// reads as nil rather than an error, which is how the world knows to fall back
// to the generator.
func TestRegionReadsMissingChunkAsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rf, err := openRegion(regionFilePath(dir, 0, 0), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	t.Cleanup(func() { rf.close() })

	if got, err := rf.readChunk(ChunkPos{X: 5, Z: 0}); err != nil {
		t.Fatalf("readChunk on empty slot: %v", err)
	} else if got != nil {
		t.Errorf("readChunk on empty slot = %v, want nil", got)
	}
}

// TestRegionOverwritesChunk checks that writing a chunk to a slot that already
// has one replaces it, which is what a block change and a re-save does.
func TestRegionOverwritesChunk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rf, err := openRegion(regionFilePath(dir, 0, 0), defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	t.Cleanup(func() { rf.close() })

	first := []byte("first version, short")
	if err := rf.writeChunk(ChunkPos{X: 3, Z: 0}, first); err != nil {
		t.Fatalf("writeChunk first: %v", err)
	}
	second := []byte("second version, rather longer than the first")
	if err := rf.writeChunk(ChunkPos{X: 3, Z: 0}, second); err != nil {
		t.Fatalf("writeChunk second: %v", err)
	}

	got, err := rf.readChunk(ChunkPos{X: 3, Z: 0})
	if err != nil {
		t.Fatalf("readChunk: %v", err)
	}
	if !bytes.Equal(got, second) {
		t.Errorf("readChunk = %q, want %q", got, second)
	}
}

// TestRegionPersistsAcrossReopen checks that a chunk written to a region file
// is still there after the file is closed and reopened, which is what a server
// restart does.
func TestRegionPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := regionFilePath(dir, -1, 2)

	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatalf("openRegion: %v", err)
	}
	data := []byte("a chunk that outlives the file handle")
	if err := rf.writeChunk(ChunkPos{X: 10, Z: 0}, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	if err := rf.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	rf2, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { rf2.close() })

	got, err := rf2.readChunk(ChunkPos{X: 10, Z: 0})
	if err != nil {
		t.Fatalf("readChunk after reopen: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("readChunk after reopen = %q, want %q", got, data)
	}
}

// TestRegionReadsGzipAndNone checks that a region file written with gzip or no
// compression — which older tools or other implementations may produce — is
// read back correctly, since the vanilla server only writes zlib but the
// format allows all three.
func TestRegionReadsGzipAndNone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := regionFilePath(dir, 0, 0)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	payload := []byte("uncompressed payload")
	for _, tc := range []struct {
		name    string
		comp    byte
		wrapped []byte
	}{
		{"gzip", compressGzip, func() []byte {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			if _, err := gz.Write(payload); err != nil {
				t.Fatalf("gzip write: %v", err)
			}
			gz.Close()
			return buf.Bytes()
		}()},
		{"none", compressNone, payload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			rf, err := openRegion(path, defaultSaveFormat)
			if err != nil {
				t.Fatal(err)
			}
			defer rf.close()

			// Write the chunk manually with the given compression type,
			// bypassing writeChunk (which always uses zlib).
			record := make([]byte, 4+1+len(tc.wrapped))
			record[0], record[1], record[2], record[3] = 0, 0, 0, byte(1+len(tc.wrapped))
			record[4] = tc.comp
			copy(record[5:], tc.wrapped)

			sectors := (len(record) + sectorSize - 1) / sectorSize
			padded := make([]byte, sectors*sectorSize)
			copy(padded, record)
			if _, err := rf.file.WriteAt(padded, int64(2*sectorSize)); err != nil {
				t.Fatal(err)
			}
			rf.offsets[7] = uint32(2<<8 | sectors)
			rf.timestamp[7] = 1
			if err := rf.writeHeader(); err != nil {
				t.Fatal(err)
			}

			got, err := rf.readChunk(ChunkPos{X: 7, Z: 0})
			if err != nil {
				t.Fatalf("readChunk: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("readChunk = %q, want %q", got, payload)
			}
		})
	}
}

// TestRegionZlibIsReadableByVanillaLayout checks that the bytes writeChunk
// produces match the layout the vanilla server reads: a big-endian length, a
// compression type byte, and a zlib-compressed payload, padded to a sector.
func TestRegionZlibIsReadableByVanillaLayout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := regionFilePath(dir, 0, 0)
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.close()

	payload := []byte("the vanilla layout test")
	if err := rf.writeChunk(ChunkPos{X: 0, Z: 0}, payload); err != nil {
		t.Fatal(err)
	}

	// Read the raw bytes from the file and parse them by hand.
	offset := rf.offsets[0]
	sector := int(offset >> 8)
	raw := make([]byte, sectorSize*4) // enough for a small chunk
	n, err := rf.file.ReadAt(raw, int64(sector*sectorSize))
	if err != nil && n == 0 {
		t.Fatalf("ReadAt: %v", err)
	}
	raw = raw[:n]

	// Length includes the compression type byte and the compressed payload.
	length := int(raw[0])<<24 | int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
	if length < 1 || length > len(raw)-4 {
		t.Fatalf("length %d is out of range", length)
	}
	if raw[4] != compressZlib {
		t.Errorf("compression type = %d, want %d (zlib)", raw[4], compressZlib)
	}

	// Decompress the payload and check it matches.
	r, err := zlib.NewReader(bytes.NewReader(raw[5 : 4+length]))
	if err != nil {
		t.Fatalf("zlib reader: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("zlib read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("decompressed = %q, want %q", got, payload)
	}
}

// trimLastSector cuts the file right after the record of the chunk at index,
// simulating a region file that was never closed.
func trimLastSector(t *testing.T, rf *regionFile, index int) {
	t.Helper()
	start := int64(rf.offsets[index]>>8) * sectorSize
	var head [4]byte
	if _, err := rf.file.ReadAt(head[:], start); err != nil {
		t.Fatal(err)
	}
	end := start + 4 + int64(binary.BigEndian.Uint32(head[:]))
	if end%sectorSize == 0 {
		t.Fatal("the record fills its last sector, so there is nothing to trim")
	}
	if err := rf.file.Truncate(end); err != nil {
		t.Fatal(err)
	}
}

// TestRegionDoesNotOverwriteAnUnreadableChunk checks that a chunk whose data
// fails to read keeps its bytes: a write to it is refused, and a write to
// another chunk does not reuse its sectors.
func TestRegionDoesNotOverwriteAnUnreadableChunk(t *testing.T) {
	t.Parallel()
	rf, err := openRegion(regionFilePath(t.TempDir(), 0, 0), defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rf.close() })
	if err := rf.writeChunk(ChunkPos{X: 1, Z: 0}, []byte("a chunk about to be damaged")); err != nil {
		t.Fatal(err)
	}
	start := int64(rf.offsets[1]>>8) * sectorSize
	if _, err := rf.file.WriteAt([]byte{0xde, 0xad, 0xbe, 0xef}, start+5); err != nil {
		t.Fatal(err)
	}
	damaged := make([]byte, sectorSize)
	if _, err := rf.file.ReadAt(damaged, start); err != nil {
		t.Fatal(err)
	}

	if _, err := rf.readChunk(ChunkPos{X: 1, Z: 0}); err == nil {
		t.Fatal("readChunk of a damaged chunk succeeded")
	}
	if err := rf.writeChunk(ChunkPos{X: 1, Z: 0}, []byte("a regenerated chunk")); !errors.Is(err, errUnreadable) {
		t.Errorf("writeChunk over the damaged chunk = %v, want errUnreadable", err)
	}
	for i := range 3 {
		if err := rf.writeChunk(ChunkPos{X: 2, Z: 0}, bytes.Repeat([]byte{byte(i)}, 3*sectorSize)); err != nil {
			t.Fatal(err)
		}
	}

	after := make([]byte, sectorSize)
	if _, err := rf.file.ReadAt(after, start); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, damaged) {
		t.Error("the damaged chunk's sector was overwritten")
	}
	if got := rf.offsets[1] >> 8; int64(got)*sectorSize != start {
		t.Errorf("the damaged chunk's entry moved to sector %d", got)
	}
}

// damageChunk writes a chunk record whose payload is not zlib, returning the
// region file's bytes for comparison after the world has had its go.
func damageChunk(t *testing.T, path string, pos ChunkPos) []byte {
	t.Helper()
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.close()
	if err := rf.writeChunk(pos, []byte("placeholder")); err != nil {
		t.Fatal(err)
	}
	start := int64(rf.offsets[regionIndex(pos.X, pos.Z)]>>8) * sectorSize
	if _, err := rf.file.WriteAt([]byte("not zlib"), start+5); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// hexBytes decodes a fixture recorded as hex, so the reading tests can
// carry bytes the reference server wrote without a binary file beside them.
func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	raw := make([]byte, len(s)/2)
	if _, err := hex.Decode(raw, []byte(s)); err != nil {
		t.Fatalf("fixture hex: %v", err)
	}
	return raw
}

// putRawRecord writes a hand-made record — the length, the compression type
// byte and the payload exactly as given — into a free sector and points the
// header at it, so the reading tests can plant shapes this writer never
// makes.
func putRawRecord(t *testing.T, rf *regionFile, index int, compType byte, payload []byte) {
	t.Helper()
	sectors := (5 + len(payload) + sectorSize - 1) / sectorSize
	sector := rf.allocate(sectors)
	record := make([]byte, sectors*sectorSize)
	binary.BigEndian.PutUint32(record[:4], uint32(1+len(payload)))
	record[4] = compType
	copy(record[5:], payload)
	if _, err := rf.file.WriteAt(record, int64(sector)*sectorSize); err != nil {
		t.Fatal(err)
	}
	rf.offsets[index] = uint32(sector<<8 | sectors)
	if err := rf.writeHeader(); err != nil {
		t.Fatal(err)
	}
}

// lz4FixtureCompressed and lz4FixtureStored are the bytes a vanilla
// server with region-file-compression=lz4 wrote through its own
// LZ4BlockOutputStream, produced once with the reference harness: the first
// wraps a compressible block, the second a short one stored verbatim. Each
// ends with the 21-byte empty terminal envelope.
const lz4FixtureCompressed = "4c5a34426c6f636b2638000000c00f00004ecbf403ff0f74686520706164646564206368756e6b207061796c6f6164207a6f6e65000100ffffffffffffffffffffffffffffff99500000656e644c5a34426c6f636b16000000000000000000000000"
const lz4FixtureStored = "4c5a34426c6f636b16110000001100000092dc790168656c6c6f206368756e6b20776f726c644c5a34426c6f636b16000000000000000000000000"

// TestRegionReadsVanillaLz4Chunks checks that a chunk record a vanilla
// server wrote with region-file-compression=lz4 — compression type 4, the
// "LZ4Block" framing of its lz4-java library — is read, rather than being
// refused as an unknown compression and its chunk regenerated over.
func TestRegionReadsVanillaLz4Chunks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		stream string
		want   []byte
	}{
		{"compressed block", lz4FixtureCompressed, []byte("the padded chunk payload zone" + strings.Repeat("\x00", 4000) + "end")},
		{"stored block", lz4FixtureStored, []byte("hello chunk world")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rf, err := openRegion(regionFilePath(t.TempDir(), 0, 0), defaultSaveFormat)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { rf.close() })
			putRawRecord(t, rf, 7, 4, hexBytes(t, tc.stream))

			got, err := rf.readChunk(ChunkPos{X: 7, Z: 0})
			if err != nil {
				t.Fatalf("readChunk of an lz4 record: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("readChunk = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRegionReadsExternalMccChunks checks that a record with vanilla's
// external flag set — a length of one and the compression with the 128 bit
// or'ed in — has its payload read from the c.<x>.<z>.mcc file beside the
// region file, the shape the vanilla server writes a chunk too big for a
// region entry into.
func TestRegionReadsExternalMccChunks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := regionFilePath(dir, 0, 0)
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rf.close() })

	var wrapped bytes.Buffer
	zw := zlib.NewWriter(&wrapped)
	if _, err := zw.Write([]byte("an external chunk's payload")); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	putRawRecord(t, rf, 7, 0x80|2, nil)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "c.7.0.mcc"), wrapped.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := rf.readChunk(ChunkPos{X: 7, Z: 0})
	if err != nil {
		t.Fatalf("readChunk of an external record: %v", err)
	}
	if string(got) != "an external chunk's payload" {
		t.Errorf("readChunk = %q, want the .mcc file's decompressed bytes", got)
	}

	// The stub's sector is one entry long: a region header never says more
	// of an external chunk than the 5 record bytes themselves.
	entry := rf.offsets[7]
	if sector, count := entry>>8, entry&0xFF; count != 1 || sector < 2 {
		t.Errorf("the external record's entry is %d sectors at %d, want 1 sector past the header", count, sector)
	}
}

// incompressible makes n bytes that survive zlib, so a test can push a
// record past the size an entry can name without generating megabytes of
// real chunk data.
func incompressible(n int) []byte {
	rng := rand.New(rand.NewPCG(944, 1))
	data := make([]byte, 0, n+8)
	for len(data) < n {
		var v [8]byte
		binary.LittleEndian.PutUint64(v[:], rng.Uint64())
		data = append(data, v[:]...)
	}
	return data[:n]
}

// TestRegionWritesOversizedChunksToMcc checks that a chunk whose record
// would not fit a header entry — 256 sectors or more, the mebibyte a base
// of books, shulker boxes or sign text fills — lands in the external
// c.<x>.<z>.mcc file the vanilla server reads, with a one-sector stub
// entry in its place, and reads back.
func TestRegionWritesOversizedChunksToMcc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := regionFilePath(dir, 0, 0)
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rf.close() })

	data := incompressible(1<<20 + sectorSize)
	if err := rf.writeChunk(ChunkPos{X: 9, Z: 0}, data); err != nil {
		t.Fatalf("writeChunk of a chunk too big for an entry: %v", err)
	}

	entry := rf.offsets[9]
	if count := entry & 0xFF; count != 1 {
		t.Fatalf("the oversized chunk's entry spans %d sectors, want the 1 of a stub", count)
	}
	sector := int(entry>>8) * sectorSize
	var stub [5]byte
	if _, err := rf.file.ReadAt(stub[:], int64(sector)); err != nil {
		t.Fatal(err)
	}
	if length := binary.BigEndian.Uint32(stub[:4]); length != 1 {
		t.Errorf("the stub record's length = %d, want 1", length)
	}
	if stub[4] != 0x80|2 {
		t.Errorf("the stub's compression byte = %d, want zlib with the external flag", stub[4])
	}
	mcc, err := os.ReadFile(filepath.Join(filepath.Dir(path), "c.9.0.mcc"))
	if err != nil {
		t.Fatalf("the external chunk file does not hold the payload: %v", err)
	}
	var want bytes.Buffer
	zw := zlib.NewWriter(&want)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	if !bytes.Equal(mcc, want.Bytes()) {
		t.Errorf("the .mcc holds %d bytes, want the record's %d-byte compressed payload", len(mcc), want.Len())
	}

	got, err := rf.readChunk(ChunkPos{X: 9, Z: 0})
	if err != nil {
		t.Fatalf("readChunk of an external chunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("readChunk = the first %d bytes of the chunk, want all %d", len(got), len(data))
	}

	// A chunk rewritten small enough to fit an entry again takes its
	// external file away, as vanilla's write does.
	small := []byte("a chunk that fits an entry again")
	if err := rf.writeChunk(ChunkPos{X: 9, Z: 0}, small); err != nil {
		t.Fatalf("writeChunk after the oversized one: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "c.9.0.mcc")); !os.IsNotExist(err) {
		t.Error("the external file of the chunk survived its internal rewrite")
	}
	again, err := rf.readChunk(ChunkPos{X: 9, Z: 0})
	if err != nil || !bytes.Equal(again, small) {
		t.Errorf("readChunk after the move back = %q, %v; want %q", again, err, small)
	}
}

// TestRegionWritesLz4Chunks checks that a region file opened for lz4 writes
// the records a vanilla server reads: compression type 4, the "LZ4Block"
// framing of its lz4-java library, ending in the empty terminal envelope.
func TestRegionWritesLz4Chunks(t *testing.T) {
	t.Parallel()
	format, err := parseSaveFormat("lz4", false)
	if err != nil {
		t.Fatal(err)
	}
	path := regionFilePath(t.TempDir(), 0, 0)
	rf, err := openRegion(path, format)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rf.close() })

	data := []byte("a chunk saved compressed with lz4")
	if err := rf.writeChunk(ChunkPos{X: 3, Z: 0}, data); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}

	entry := rf.offsets[regionIndex(3, 0)]
	sector := int64(entry>>8) * sectorSize
	var head [5]byte
	if _, err := rf.file.ReadAt(head[:], sector); err != nil {
		t.Fatal(err)
	}
	if head[4] != 4 {
		t.Fatalf("compression type = %d, want 4 (lz4)", head[4])
	}
	length := binary.BigEndian.Uint32(head[:4])
	payload := make([]byte, length-1)
	if _, err := rf.file.ReadAt(payload, sector+5); err != nil {
		t.Fatal(err)
	}
	if want := []byte("LZ4Block"); !bytes.Equal(payload[:len(want)], want) {
		t.Errorf("the record starts with %q, want the LZ4Block magic", payload[:len(want)])
	}
	wantTail := append([]byte("LZ4Block"), 0x16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if !bytes.HasSuffix(payload, wantTail) {
		t.Error("the record does not end in the empty terminal envelope")
	}

	got, err := rf.readChunk(ChunkPos{X: 3, Z: 0})
	if err != nil {
		t.Fatalf("readChunk of our own lz4 record: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("readChunk = %q, want %q", got, data)
	}
}

// TestWorldKeepsChunksItCannotUpgrade checks that a chunk record in an older
// format, or naming a block this server does not know, is left byte for byte
// as stored by a later edit and save instead of being regenerated or read as
// air.
func TestWorldKeepsChunksItCannotUpgrade(t *testing.T) {
	t.Parallel()
	pos := ChunkPos{X: 1, Z: 1}
	chunkRoot := func(version int32, status string, block string, props ...string) nbt.Compound {
		entry := nbt.Compound{"Name": nbt.String(block)}
		if len(props) > 0 {
			entry["Properties"] = nbt.Compound{props[0]: nbt.String(props[1])}
		}
		return nbt.Compound{
			"DataVersion": nbt.Int(version),
			"Status":      nbt.String(status),
			"xPos":        nbt.Int(pos.X),
			"zPos":        nbt.Int(pos.Z),
			"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"Y": nbt.Byte(4),
				"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					entry,
				}}},
			}}},
		}
	}
	for _, tc := range []struct {
		name string
		root nbt.Compound
	}{
		{"Level wrapper below the floor", nbt.Compound{"DataVersion": nbt.Int(levelChunkFloor - 1), "Level": nbt.Compound{"Status": nbt.String("full")}}},
		{"unnamespaced status", chunkRoot(dataVersion, "full", "minecraft:stone")},
		{"data version below the floor", chunkRoot(chunkDataVersionFloor-1, "minecraft:full", "minecraft:stone")},
		{"unknown palette name", chunkRoot(dataVersion, "minecraft:full", "minecraft:grass")},
		{"older property no fix renames", chunkRoot(4189, "minecraft:full", "minecraft:oak_log", "facing", "north")},
		{"unknown property value", chunkRoot(dataVersion, "minecraft:full", "minecraft:oak_log", "axis", "sideways")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			rx, rz := regionCoords(pos)
			path := regionFilePath(DimensionDir(dir, DimensionName), rx, rz)
			encoded, err := nbt.Encode("", tc.root)
			if err != nil {
				t.Fatal(err)
			}
			rf, err := openRegion(path, defaultSaveFormat)
			if err != nil {
				t.Fatal(err)
			}
			if err := rf.writeChunk(pos, encoded); err != nil {
				t.Fatal(err)
			}
			rf.close()
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			w, _ := NewWorld(dir, NewFlatGenerator())
			w.Acquire(pos)
			glass := registry.BlockStates["minecraft:glass"]
			if !w.SetBlock(pos.X*16, 70, pos.Z*16, glass) || w.BlockAt(pos.X*16, 70, pos.Z*16) != glass {
				t.Fatal("the edit did not change the block")
			}
			w.Save()
			w.Release(pos)
			w.FlushSaves()
			w.Close()

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Error("the region file changed after an edit and save")
			}
		})
	}
}

func TestOldDataVersionChunkIsNotGeneratedOver(t *testing.T) {
	// Not parallel: it swaps the global slog default to read the warning.
	dir := t.TempDir()
	pos := ChunkPos{X: 1, Z: 2}
	rx, rz := regionCoords(pos)
	path := regionFilePath(DimensionDir(dir, DimensionName), rx, rz)
	encoded, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(chunkDataVersionFloor - 1),
		"Status":      nbt.String("minecraft:full"),
		"xPos":        nbt.Int(pos.X),
		"zPos":        nbt.Int(pos.Z),
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.writeChunk(pos, encoded); err != nil {
		t.Fatal(err)
	}
	rf.close()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := writeLevelCompound(dir, nbt.Compound{
		"Version": nbt.Compound{"Name": nbt.String("1.17.1")},
	}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	w, _ := NewWorld(dir, NewFlatGenerator())
	w.Acquire(pos)
	w.Acquire(pos)
	if n := strings.Count(logs.String(), "cannot upgrade"); n != 1 || !strings.Contains(logs.String(), "world_version=1.17.1") {
		t.Errorf("want one warning naming the world and version 1.17.1, got %d:\n%s", n, logs.String())
	}
	if got := w.BlockAt(pos.X*16, 1, pos.Z*16); got != 0 {
		t.Errorf("block at the flat ground = %d, want air", got)
	}
	if !w.oldVersionLogged.Load() {
		t.Error("no warning for the unsupported version")
	}
	w.Save()
	w.Release(pos)
	w.FlushSaves()
	w.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("the region file changed")
	}
}

func TestOldDataVersionWarningNamesVersionInNether(t *testing.T) {
	// Not parallel: it swaps the global slog default to read the warning.
	dir := t.TempDir()
	pos := ChunkPos{X: 1, Z: 2}
	rx, rz := regionCoords(pos)
	path := regionFilePath(DimensionDir(dir, NetherName), rx, rz)
	encoded, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(chunkDataVersionFloor - 1),
		"Status":      nbt.String("minecraft:full"),
		"xPos":        nbt.Int(pos.X),
		"zPos":        nbt.Int(pos.Z),
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.writeChunk(pos, encoded); err != nil {
		t.Fatal(err)
	}
	rf.close()
	if err := writeLevelCompound(dir, nbt.Compound{
		"Version": nbt.Compound{"Name": nbt.String("1.17.1")},
	}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	w := NewDimensionWorld(dir, true, NewNetherGenerator(7))
	w.Acquire(pos)
	if n := strings.Count(logs.String(), "cannot upgrade"); n != 1 || !strings.Contains(logs.String(), "world_version=1.17.1") {
		t.Errorf("want one warning naming version 1.17.1, got %d:\n%s", n, logs.String())
	}
}
