package world

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/golem-mc/golem/nbt"
)

// MaxSavedData bounds a saved data file's decompressed size, on write as on
// read, so every file the server writes it can read back. Vanilla reads without
// a byte quota; the bound only stops a crafted file exhausting memory, and is
// vanilla's uncompressed quota (100 MiB), far beyond what forced chunks, a
// timer queue or a scoreboard reach.
// vanilla: SavedDataStorage.readTagFromDisk, NbtAccounter.uncompressedQuota
const MaxSavedData = 100 << 20

// errNoSavedData is readSavedData's answer for a file that does not exist.
var errNoSavedData = errors.New("no such saved data file")

// readSavedData reads the data compound of a gzipped {DataVersion, data} saved
// data file under dir.
func readSavedData(dir, name string) (nbt.Compound, error) {
	return readSavedDataWithin(dir, name, MaxSavedData)
}

// readSavedDataWithin is readSavedData with the decompression cap of the
// caller's choosing.
func readSavedDataWithin(dir, name string, limit int) (nbt.Compound, error) {
	return readSavedDataTags(dir, name, limit, false)
}

func readSavedDataTags(dir, name string, limit int, ordered bool) (nbt.Compound, error) {
	data, _, err := readSavedDataVersion(dir, name, limit, ordered)
	return data, err
}

// readSavedDataVersion also returns the file's DataVersion, 0 without one.
func readSavedDataVersion(dir, name string, limit int, ordered bool) (nbt.Compound, int32, error) {
	path := filepath.Join(dir, name)
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, errNoSavedData
	} else if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, 0, fmt.Errorf("decompress %s: %w", path, err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(io.LimitReader(gz, int64(limit)+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > limit {
		return nil, 0, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	decode := nbt.Decode
	if ordered {
		decode = nbt.DecodeOrdered
	}
	_, tag, err := decode(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	root, ok := tag.(nbt.CompoundTag)
	if !ok {
		return nil, 0, fmt.Errorf("%s root is a %s, want a compound", path, tag.Type())
	}
	data, ok := nbt.Get[nbt.CompoundTag](root.Entries(), "data")
	if !ok {
		return nil, 0, fmt.Errorf("%s has no data compound", path)
	}
	version, _ := nbt.Get[nbt.Int](root.Entries(), "DataVersion")
	return data.Entries(), int32(version), nil
}

// writeSavedData writes data as the saved data file name under dir. A tree past
// MaxSavedData is refused and the existing file kept, rather than writing one
// the next start would refuse.
// vanilla: LevelStorageSource.writeSavedData
func writeSavedData(dir, name string, data nbt.Compound) error {
	encoded, err := nbt.Encode("", nbt.Compound{"data": data, "DataVersion": nbt.Int(dataVersion)})
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	if len(encoded) > MaxSavedData {
		return fmt.Errorf("encode %s: %d bytes is more than the %d a read takes", name, len(encoded), MaxSavedData)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		return fmt.Errorf("compress %s: %w", name, err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("compress %s: %w", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return writeFileSynced(path, compressed.Bytes())
}

// writeFileSynced writes data via path.tmp, synced before renaming over the old
// file: a crash mid-write leaves the old file, and a power loss after the
// rename cannot leave an empty one.
// vanilla: NbtIo.writeCompressed uses StandardOpenOption.SYNC
func writeFileSynced(path string, data []byte) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ReadSavedData reads the saved data file name under dir, decompressed to at
// most limit bytes; ok is false without the file. Every file the server writes
// fits MaxSavedData.
func ReadSavedData(dir, name string, limit int) (data nbt.Compound, ok bool, err error) {
	data, _, ok, err = ReadSavedDataVersion(dir, name, limit)
	return data, ok, err
}

// ReadSavedDataVersion is ReadSavedData that also returns the file's
// DataVersion, 0 when it has none.
func ReadSavedDataVersion(dir, name string, limit int) (data nbt.Compound, version int32, ok bool, err error) {
	data, version, err = readSavedDataVersion(dir, name, limit, false)
	if errors.Is(err, errNoSavedData) {
		return nil, 0, false, nil
	}
	return data, version, err == nil, err
}

// WriteSavedData writes data as the saved data file name under dir.
func WriteSavedData(dir, name string, data nbt.Compound) error {
	return writeSavedData(dir, name, data)
}
