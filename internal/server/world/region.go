package world

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/klauspost/compress/zlib"
)

// Region files (.mca, Anvil) hold 32x32 chunks as r.<regionX>.<regionZ>.mca
// under a dimension's region/: two 4 KiB tables (offsets, timestamps), then
// data in 4 KiB sectors. An offset entry is the start sector in the upper 24
// bits and the sector count in the low 8; zero means absent. A record is a
// big-endian int32 length, a compression byte and the compressed NBT. Vanilla
// writes deflate (2), or lz4 (4) per region-file-compression; gzip (1) and none
// (3) are valid too. All are read; writes use the configured one. A record of
// 256 sectors or more cannot fit the 8-bit count, so its payload goes to
// c.<x>.<z>.mcc beside the region file and the entry points at a one-sector
// stub with the external flag.
const (
	sectorSize      = 4096
	chunksPerRegion = 32 * 32
	regionHeaderLen = 2 * sectorSize // offset table + timestamp table

	compressGzip byte = 1
	compressZlib byte = 2
	compressNone byte = 3
	compressLZ4  byte = 4

	// externalFlag marks a record whose payload is in a c.<x>.<z>.mcc file.
	// vanilla: RegionFile.EXTERNAL_STREAM_FLAG
	externalFlag byte = 0x80

	// externalSectorThreshold is the record size that moves to an external
	// file.
	// vanilla: RegionFile.EXTERNAL_CHUNK_THRESHOLD
	externalSectorThreshold = 256
)

// errUnreadable refuses a write to a chunk whose stored data could not be read,
// which would lose what the file still holds.
var errUnreadable = errors.New("its stored data could not be read, so it is not overwritten")

// saveFormat is the compression new records use and whether every write is
// synced: region-file-compression and sync-chunk-writes.
// vanilla: RegionFile's constructor takes both
type saveFormat struct {
	compression byte
	sync        bool
}

// defaultSaveFormat is what a vanilla server writes by default.
var defaultSaveFormat = saveFormat{compression: compressZlib}

// parseSaveFormat reads region-file-compression, which names a compression
// rather than numbering it.
// vanilla: RegionFileVersion.configure
func parseSaveFormat(name string, sync bool) (saveFormat, error) {
	format := saveFormat{compression: compressZlib, sync: sync}
	if !slices.Contains(config.RegionFileCompressions, name) {
		return format, fmt.Errorf("not one of %s: %q", strings.Join(config.RegionFileCompressions, ", "), name)
	}
	switch name {
	case "lz4":
		format.compression = compressLZ4
	case "none":
		format.compression = compressNone
	}
	return format, nil
}

// regionFile is one open .mca file, its tables cached, sector I/O behind one
// mutex so a save never lands inside a load from the same region.
type regionFile struct {
	path   string
	file   *os.File
	format saveFormat

	mu        sync.Mutex
	offsets   [chunksPerRegion]uint32
	timestamp [chunksPerRegion]uint32
	// used marks the sectors the header's entries occupy, so a write never
	// lands on another chunk's data.
	// vanilla: RegionBitmap
	used []bool
	// unreadable marks the chunks whose stored data failed to read or parse.
	unreadable [chunksPerRegion]bool
	// anyUnreadable mirrors whether unreadable has any bit set, so the cache
	// can ask in O(1) whether the file must stay open.
	anyUnreadable bool
}

// regionCoords is the region of a chunk, (x >> 5, z >> 5); Go's arithmetic
// shift rounds toward negative infinity as the format requires.
func regionCoords(pos ChunkPos) (int32, int32) {
	return pos.X >> 5, pos.Z >> 5
}

// regionIndex is a chunk's table slot: the low five bits of each coordinate, x
// fastest; the mask gives 0-31 for negative coordinates too.
func regionIndex(x, z int32) int {
	return int((x & 31) + (z&31)*32)
}

// regionFilePath returns the path of the region file for (rx, rz) under dir.
func regionFilePath(dir string, rx, rz int32) string {
	return filepath.Join(dir, "region", fmt.Sprintf("r.%d.%d.mca", rx, rz))
}

// externalChunkPath is an oversized chunk's payload file beside the region
// file, c.<x>.<z>.mcc by the chunk's coordinates.
// vanilla: RegionFile.getExternalChunkPath
func externalChunkPath(regionPath string, pos ChunkPos) string {
	return filepath.Join(filepath.Dir(regionPath), fmt.Sprintf("c.%d.%d.mcc", pos.X, pos.Z))
}

// openRegion opens or creates a region file and reads its header; a new file
// gets an empty header so the first save has an offset table to write.
func openRegion(path string, format saveFormat) (*regionFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create region directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	rf := &regionFile{path: path, file: file, format: format}
	rf.force(0, regionHeaderLen/sectorSize)
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if stat.Size() < regionHeaderLen {
		// A new or truncated file: write an empty header so both tables exist
		// before any data is appended.
		if err := file.Truncate(regionHeaderLen); err != nil {
			file.Close()
			return nil, fmt.Errorf("init %s: %w", path, err)
		}
		return rf, nil
	}

	header := make([]byte, regionHeaderLen)
	if _, err := file.ReadAt(header, 0); err != nil {
		file.Close()
		return nil, fmt.Errorf("read header of %s: %w", path, err)
	}
	for i := range chunksPerRegion {
		rf.offsets[i] = binary.BigEndian.Uint32(header[i*4:])
		rf.timestamp[i] = binary.BigEndian.Uint32(header[sectorSize+i*4:])
		sector, count := int64(rf.offsets[i]>>8), int(rf.offsets[i]&0xFF)
		if sector*sectorSize > stat.Size() {
			// Nothing of it is in the file. vanilla: RegionFile constructor
			slog.Warn("region file entry points past the end of the file, ignoring it",
				"file", path, "index", i, "sector", sector)
			rf.offsets[i] = 0
			continue
		}
		if sector >= regionHeaderLen/sectorSize {
			rf.force(int(sector), count)
		}
	}
	return rf, nil
}

func (rf *regionFile) force(start, count int) {
	if n := start + count; n > len(rf.used) {
		rf.used = append(rf.used, make([]bool, n-len(rf.used))...)
	}
	for i := start; i < start+count; i++ {
		rf.used[i] = true
	}
}

func (rf *regionFile) free(start, count int) {
	for i := start; i < start+count && i < len(rf.used); i++ {
		rf.used[i] = false
	}
}

// allocate claims the first run of count free sectors; past the last used
// sector all are free.
// vanilla: RegionBitmap.allocate
func (rf *regionFile) allocate(count int) int {
	start := 0
	for {
		for start < len(rf.used) && rf.used[start] {
			start++
		}
		end := start
		for end < len(rf.used) && !rf.used[end] && end-start < count {
			end++
		}
		if end == len(rf.used) || end-start >= count {
			rf.force(start, count)
			return start
		}
		start = end
	}
}

// readChunk returns the chunk's decompressed NBT, or (nil, nil) if absent. It
// reads only the length the record announces: vanilla pads the last sector only
// on close, so a copy taken while it runs ends mid-sector. A failed read marks
// the chunk unreadable and later writes to it are refused.
// vanilla: RegionFile.getChunkDataInputStream
func (rf *regionFile) readChunk(pos ChunkPos) ([]byte, error) {
	index := regionIndex(pos.X, pos.Z)
	compType, payload, err := rf.readRecord(index)
	if err == nil && payload != nil {
		var data []byte
		if data, err = rf.decodeRecord(pos, compType, payload); err == nil {
			return data, nil
		}
	} else if err == nil {
		return nil, nil
	}
	rf.markUnreadable(index)
	return nil, fmt.Errorf("chunk %d in %s: %w", index, rf.path, err)
}

// decodeRecord decompresses a payload. An external record is a stub; the bytes
// are in the c.<x>.<z>.mcc file, compressed per the type byte under the
// external flag.
// vanilla: RegionFile.createChunkInputStream,
// RegionFile.createExternalChunkInputStream
func (rf *regionFile) decodeRecord(pos ChunkPos, compType byte, payload []byte) ([]byte, error) {
	if compType&externalFlag != 0 {
		data, err := os.ReadFile(externalChunkPath(rf.path, pos))
		if err != nil {
			return nil, fmt.Errorf("read its external file: %w", err)
		}
		return decompress(compType&^externalFlag, data)
	}
	return decompress(compType, payload)
}

// markUnreadable marks the chunk at index unusable, for a caller whose parse of
// readChunk's bytes failed.
func (rf *regionFile) markUnreadable(index int) {
	rf.mu.Lock()
	rf.unreadable[index] = true
	rf.anyUnreadable = true
	rf.mu.Unlock()
}

// hasUnreadable reports a chunk in the file that failed to read; such a file is
// not evicted, so the mark survives a reopen.
func (rf *regionFile) hasUnreadable() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.anyUnreadable
}

// readRecord returns the chunk's compression type and compressed payload, nil
// if absent.
func (rf *regionFile) readRecord(index int) (byte, []byte, error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	offset := rf.offsets[index]
	sector, count := int(offset>>8), int(offset&0xFF)
	if offset == 0 || count == 0 {
		return 0, nil, nil // vanilla drops a zero-sector entry as absent
	}
	if sector < regionHeaderLen/sectorSize {
		return 0, nil, errors.New("it starts inside the header")
	}

	var head [5]byte
	if _, err := rf.file.ReadAt(head[:], int64(sector)*sectorSize); err != nil {
		return 0, nil, fmt.Errorf("read its header: %w", err)
	}
	// The length covers the compression type byte and the payload.
	length := int64(binary.BigEndian.Uint32(head[:4]))
	if length == 0 {
		return 0, nil, nil // allocated but never written; vanilla reads it as absent
	}
	if length-1 > int64(count)*sectorSize-5 {
		return 0, nil, fmt.Errorf("its length %d overruns its %d sectors", length, count)
	}
	payload := make([]byte, length-1)
	if _, err := rf.file.ReadAt(payload, int64(sector)*sectorSize+5); err != nil {
		return 0, nil, fmt.Errorf("read %d bytes: %w", len(payload), err)
	}
	return head[4], payload, nil
}

func decompress(compType byte, payload []byte) ([]byte, error) {
	var r io.ReadCloser
	var err error
	switch compType {
	case compressZlib:
		zr, _ := zlibReaders.Get().(io.ReadCloser)
		if zr == nil {
			zr, err = zlib.NewReader(bytes.NewReader(payload))
		} else {
			err = zr.(zlib.Resetter).Reset(bytes.NewReader(payload), nil)
		}
		if err != nil {
			if zr != nil {
				zr.Close()
			}
			return nil, fmt.Errorf("decompress: %w", err)
		}
		defer func() {
			zr.Close()
			zlibReaders.Put(zr)
		}()
		// Chunk NBT usually deflates tenfold; allow headroom to avoid growth.
		out := bytes.NewBuffer(make([]byte, 0, len(payload)*16))
		if _, err := out.ReadFrom(zr); err != nil {
			return nil, fmt.Errorf("decompress: %w", err)
		}
		return out.Bytes(), nil
	case compressGzip:
		r, err = gzip.NewReader(bytes.NewReader(payload))
	case compressNone:
		return payload, nil
	case compressLZ4:
		return lz4DecompressStream(payload)
	default:
		return nil, fmt.Errorf("unknown compression %d", compType)
	}
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return data, nil
}

// zlibReaders retains the inflate tables; each output remains caller-owned.
var zlibReaders sync.Pool

// zlibWriters pools deflate writers: a fresh one allocates and clears about a
// megabyte of tables, and Reset gives the same bytes.
var zlibWriters = sync.Pool{New: func() any { w, _ := zlib.NewWriterLevel(nil, 6); return w }}

// compressPayload compresses the NBT with the type the record header will name.
func compressPayload(compression byte, data []byte) ([]byte, error) {
	switch compression {
	case compressZlib:
		var compressed bytes.Buffer
		writer := zlibWriters.Get().(*zlib.Writer)
		defer zlibWriters.Put(writer)
		writer.Reset(&compressed)
		if _, err := writer.Write(data); err != nil {
			writer.Close()
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return compressed.Bytes(), nil
	case compressLZ4:
		return lz4CompressStream(data), nil
	case compressNone:
		return data, nil
	}
	return nil, fmt.Errorf("unknown compression %d", compression)
}

// writeChunk writes the chunk, then points the header at the new sectors and
// frees the old, so a failed write leaves the old copy. A record of
// externalSectorThreshold sectors or more moves to c.<x>.<z>.mcc with a
// one-sector stub; a chunk that shrinks back removes its external file. A chunk
// marked unreadable is not written (the error wraps errUnreadable).
// vanilla: RegionFile.write
func (rf *regionFile) writeChunk(pos ChunkPos, nbtData []byte) error {
	index := regionIndex(pos.X, pos.Z)
	payload, err := compressPayload(rf.format.compression, nbtData)
	if err != nil {
		return fmt.Errorf("compress chunk %d in %s: %w", index, rf.path, err)
	}

	// The record: a big-endian length (type byte plus payload), the compression
	// type, the payload.
	sectors := (5 + len(payload) + sectorSize - 1) / sectorSize
	external := sectors >= externalSectorThreshold
	if external {
		sectors = 1 // the stub the header points at
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.unreadable[index] {
		return fmt.Errorf("chunk %d in %s: %w", index, rf.path, errUnreadable)
	}

	oldExternal, err := rf.chunkExternal(index)
	if err != nil {
		return err
	}
	sector := rf.allocate(sectors)
	old, oldTime := rf.offsets[index], rf.timestamp[index]

	// An external payload goes to a staged file that the commit below renames
	// to c.<x>.<z>.mcc, and the region gets the stub. An empty staged path
	// means there is no external half to commit.
	var staged string
	if external {
		if staged, err = rf.stageExternalChunk(pos, payload); err == nil {
			err = rf.writeStub(sector)
		}
	} else {
		padded := make([]byte, sectors*sectorSize)
		binary.BigEndian.PutUint32(padded[:4], uint32(1+len(payload)))
		padded[4] = rf.format.compression
		copy(padded[5:], payload)
		if _, err = rf.file.WriteAt(padded, int64(sector)*sectorSize); err == nil && rf.format.sync {
			err = rf.file.Sync()
		}
	}
	if err != nil {
		if staged != "" {
			os.Remove(staged) // the old copy is untouched
		}
		rf.free(sector, sectors)
		return fmt.Errorf("write chunk %d in %s: %w", index, rf.path, err)
	}

	rf.offsets[index] = uint32(sector<<8 | sectors)
	rf.timestamp[index] = uint32(time.Now().Unix())
	if err := rf.writeHeader(); err != nil {
		if staged != "" {
			os.Remove(staged)
		}
		rf.offsets[index], rf.timestamp[index] = old, oldTime
		rf.free(sector, sectors)
		return err
	}
	if staged != "" {
		// The staged payload is renamed only once the header points at the
		// stub, so a crash between leaves the old copy readable.
		// vanilla: RegionFile.write's CommitOp order
		if err := os.Rename(staged, externalChunkPath(rf.path, pos)); err != nil {
			return fmt.Errorf("commit chunk %d in %s: %w", index, rf.path, err)
		}
	} else if oldExternal {
		// A chunk that fits again leaves no external file.
		// vanilla: RegionFile.write's CommitOp
		if err := os.Remove(externalChunkPath(rf.path, pos)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove chunk %d in %s's old external file: %w", index, rf.path, err)
		}
	}
	if oldSector := int(old >> 8); old != 0 && oldSector >= regionHeaderLen/sectorSize {
		rf.free(oldSector, int(old&0xFF))
	}
	return nil
}

// removeChunk frees a chunk's sectors and clears its entry, so an empty chunk
// leaves no entry.
// vanilla: RegionFile.clear
func (rf *regionFile) removeChunk(pos ChunkPos) error {
	index := regionIndex(pos.X, pos.Z)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.unreadable[index] {
		return fmt.Errorf("chunk %d in %s: %w", index, rf.path, errUnreadable)
	}
	old := rf.offsets[index]
	if old == 0 {
		return nil
	}
	wasExternal, err := rf.chunkExternal(index)
	if err != nil {
		return err
	}
	oldTime := rf.timestamp[index]
	rf.offsets[index] = 0
	rf.timestamp[index] = uint32(time.Now().Unix())
	if err := rf.writeHeader(); err != nil {
		rf.offsets[index], rf.timestamp[index] = old, oldTime
		return err
	}
	if sector := int(old >> 8); sector >= regionHeaderLen/sectorSize {
		rf.free(sector, int(old&0xFF))
	}
	if wasExternal {
		if err := os.Remove(externalChunkPath(rf.path, pos)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove chunk %d in %s's external file: %w", index, rf.path, err)
		}
	}
	return nil
}

// writeStub writes an external chunk's record in its one sector: length one and
// the compression or'ed with the external flag.
// vanilla: RegionFile.createExternalStub
func (rf *regionFile) writeStub(sector int) error {
	var stub [sectorSize]byte
	binary.BigEndian.PutUint32(stub[:4], 1)
	stub[4] = rf.format.compression | externalFlag
	if _, err := rf.file.WriteAt(stub[:], int64(sector)*sectorSize); err != nil {
		return err
	}
	if rf.format.sync {
		return rf.file.Sync()
	}
	return nil
}

// stagedExternalPath is where an external payload waits, beside the region file
// so the committing rename stays in one directory.
func (rf *regionFile) stagedExternalPath(pos ChunkPos) string {
	return filepath.Join(filepath.Dir(rf.path), fmt.Sprintf("tmp-%d-%d.mcc", pos.X, pos.Z))
}

// stageExternalChunk writes an external payload to the staged path the commit
// renames, so a half-written .mcc never appears under its real name.
// vanilla: RegionFile.writeToExternalFile's temp file
func (rf *regionFile) stageExternalChunk(pos ChunkPos, payload []byte) (string, error) {
	path := rf.stagedExternalPath(pos)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if _, err := file.Write(payload); err != nil {
		return fail(err)
	}
	if rf.format.sync {
		// The payload is the whole chunk, and sync-chunk-writes asks every
		// write to be durable.
		if err := file.Sync(); err != nil {
			return fail(err)
		}
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// writeHeader writes both tables together, so a crash leaves the old or the new
// header, never half of each.
func (rf *regionFile) writeHeader() error {
	header := make([]byte, regionHeaderLen)
	for i := range chunksPerRegion {
		binary.BigEndian.PutUint32(header[i*4:], rf.offsets[i])
		binary.BigEndian.PutUint32(header[sectorSize+i*4:], rf.timestamp[i])
	}
	if _, err := rf.file.WriteAt(header, 0); err != nil {
		return fmt.Errorf("write header of %s: %w", rf.path, err)
	}
	if rf.format.sync {
		if err := rf.file.Sync(); err != nil {
			return fmt.Errorf("sync header of %s: %w", rf.path, err)
		}
	}
	return nil
}

// close syncs what the OS still holds and releases the handle, so an unclean
// stop loses at most the write in flight. Safe after a failed open.
// vanilla: RegionFile.close
func (rf *regionFile) close() error {
	if rf.file == nil {
		return nil
	}
	err := rf.file.Sync()
	closeErr := rf.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// chunkExternal reads the previous record's flag before its header is replaced.
// The region lock is held; an absent record never needs external cleanup.
func (rf *regionFile) chunkExternal(index int) (bool, error) {
	offset := rf.offsets[index]
	if offset == 0 {
		return false, nil
	}
	var flag [1]byte
	if _, err := rf.file.ReadAt(flag[:], int64(offset>>8)*sectorSize+4); err != nil {
		return false, fmt.Errorf("read chunk %d in %s's external flag: %w", index, rf.path, err)
	}
	return flag[0]&externalFlag != 0, nil
}
