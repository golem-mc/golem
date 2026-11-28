package world

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// UpgradeOptions are the command line's --eraseCache and --recreateRegionFiles.
type UpgradeOptions struct {
	// EraseCache also drops the cached heightmaps and light of every chunk.
	EraseCache bool
	// Recreate writes each region to a fresh file that replaces the old one.
	Recreate bool
}

// CurrentDataVersion is the DataVersion a written chunk claims.
const CurrentDataVersion = dataVersion

// UpgradeWorld rewrites every chunk, entity and poi record of every dimension
// under root at the current DataVersion. A record older than this server reads
// is left as stored and logged.
// vanilla: WorldUpgrader.work
func UpgradeWorld(root string, opts UpgradeOptions) error {
	dims, err := filepath.Glob(filepath.Join(root, "dimensions", "*", "*"))
	if err != nil {
		return err
	}
	slog.Info("Starting upgrade", "dimensions", len(dims))
	for _, dim := range dims {
		dimension := filepath.Base(filepath.Dir(dim)) + ":" + filepath.Base(dim)
		for _, kind := range []struct {
			folder string
			fix    func(nbt.Compound, bool) error
		}{
			{"entities", upgradeEntityRecord},
			{"poi", func(r nbt.Compound, _ bool) error { return stampVersion(r) }},
			{"region", func(r nbt.Compound, erase bool) error {
				dropOldBlendingData(r, dimension)
				return upgradeChunkRecord(r, erase)
			}},
		} {
			if err := upgradeFolder(filepath.Join(dim, kind.folder), opts, kind.fix); err != nil {
				return err
			}
		}
	}
	slog.Info("Upgrade finished")
	return nil
}

// upgradeFolder runs fix over every record of every region file in dir.
func upgradeFolder(dir string, opts UpgradeOptions, fix func(nbt.Compound, bool) error) error {
	files, err := filepath.Glob(filepath.Join(dir, "r.*.*.mca"))
	if err != nil {
		return err
	}
	for _, path := range files {
		if err := upgradeRegion(path, opts, fix); err != nil {
			return fmt.Errorf("upgrade %s: %w", path, err)
		}
		slog.Info("Upgraded region file", "file", strings.TrimPrefix(path, filepath.Dir(filepath.Dir(dir))+string(filepath.Separator)))
	}
	return nil
}

// upgradeRegion rewrites one region file, in place or into a fresh file
// renamed over it.
func upgradeRegion(path string, opts UpgradeOptions, fix func(nbt.Compound, bool) error) error {
	var rx, rz int32
	if _, err := fmt.Sscanf(filepath.Base(path), "r.%d.%d.mca", &rx, &rz); err != nil {
		return nil // not a region file name
	}
	src, err := openRegion(path, defaultSaveFormat)
	if err != nil {
		return err
	}
	defer src.close()
	dst, target := src, path
	if opts.Recreate {
		target = path + ".new"
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if dst, err = openRegion(target, defaultSaveFormat); err != nil {
			return err
		}
		defer dst.close()
	}
	for i := range int32(chunksPerRegion) {
		pos := ChunkPos{X: rx<<5 | i&31, Z: rz<<5 | i>>5}
		data, err := src.readChunk(pos)
		if err != nil {
			slog.Warn("Could not read a chunk, leaving it as stored", "chunk", pos, "error", err)
			continue
		}
		if data == nil {
			continue
		}
		root, err := decodeRoot(data)
		if err == nil {
			err = fix(root, opts.EraseCache)
		}
		if err != nil {
			slog.Warn("Could not upgrade a chunk, leaving it as stored", "chunk", pos, "error", err)
			if dst != src {
				err = dst.writeChunk(pos, data)
			}
			if err != nil {
				return err
			}
			continue
		}
		out, err := nbt.Encode("", root)
		if err != nil {
			return err
		}
		if err := dst.writeChunk(pos, out); err != nil {
			return err
		}
	}
	if dst == src {
		return nil
	}
	if err := dst.close(); err != nil {
		return err
	}
	if err := src.close(); err != nil {
		return err
	}
	return os.Rename(target, path)
}

// stampVersion sets the current DataVersion on a record.
func stampVersion(root nbt.Compound) error {
	root["DataVersion"] = nbt.Int(dataVersion)
	return nil
}

// upgradeEntityRecord moves a chunk's stored entities to the current shape.
func upgradeEntityRecord(root nbt.Compound, _ bool) error {
	version, _ := nbt.IntField(root, "DataVersion")
	if version != 0 && version < entityDataVersionFloor {
		return fmt.Errorf("data version %d is below %d", version, entityDataVersionFloor)
	}
	if entities, ok := nbt.Get[nbt.List](root, "Entities"); ok {
		upgradeEntities(entities, int32(version))
	}
	return stampVersion(root)
}

// upgradeChunkRecord applies the fixes decodeChunkNBT applies in memory to a
// chunk's root, stamps the current DataVersion and, with erase, drops the
// cached heightmaps and light.
// vanilla: WorldUpgrader.verifyChunkPosAndEraseCache
func upgradeChunkRecord(root nbt.Compound, erase bool) error {
	if err := checkChunkFormat(root); err != nil {
		return err
	}
	if entities, ok := nbt.Get[nbt.List](root, "entities"); ok && len(entities.Items) > 0 {
		// Only the running server moves these to the entity region.
		return errors.New("chunk still holds its entities in the layout before 1.18")
	}
	fixOldChunkRoot(root)
	version, _ := nbt.IntField(root, "DataVersion")
	sections, _ := nbt.Get[nbt.List](root, "sections")
	for _, item := range sections.Items {
		sec, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		if version != 0 {
			if bs, ok := nbt.Get[nbt.Compound](sec, "block_states"); ok {
				palette, _ := nbt.Get[nbt.List](bs, "palette")
				fixLegacyPalette(palette, int32(version))
			}
		}
		if erase {
			delete(sec, "BlockLight")
			delete(sec, "SkyLight")
		}
	}
	if be, ok := nbt.Get[nbt.List](root, "block_entities"); ok && version != 0 {
		kept := be.Items[:0]
		for _, entry := range be.Items {
			if c, ok := entry.(nbt.CompoundTag); ok {
				if !fixOldBlockEntity(c, int32(version)) {
					continue
				}
				if version < furnaceBurnTimeVersion {
					fixFurnaceBurnTime(c)
				}
			}
			kept = append(kept, entry)
		}
		be.Items = kept
		root["block_entities"] = be
	}
	if erase {
		// LightData and lightCorrect are this server's own light cache.
		for _, key := range []string{"Heightmaps", "isLightOn", "LightData", "lightCorrect"} {
			delete(root, key)
		}
	}
	return stampVersion(root)
}
