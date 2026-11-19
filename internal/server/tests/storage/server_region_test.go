package storage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAnEntityFileEndingMidSectorLoads checks that an entity region whose
// last chunk ends partway through a sector, as one copied from a running
// vanilla server does, has that chunk's entities read back.
func TestAnEntityFileEndingMidSectorLoads(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")
	srv.SpawnOrb(-14.5, 65, 14.5, 7)
	orb := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	srv.FlushMobs()
	srv.Shutdown()

	pos := world.ChunkPos{X: -1, Z: 0}
	path := servertest.EntityRegionPath(cfg.LevelName, pos)
	servertest.EditChunkRecord(t, path, pos, func(record []byte) []byte { return record })
	if stat, err := os.Stat(path); err != nil || stat.Size()%4096 == 0 {
		t.Fatalf("the entity file does not end mid-sector: %v", err)
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeExperienceOrb: "orb",
	})
	back := servertest.CapturedOr(t, again, shown, "orb", protocol.EntityTypeExperienceOrb)
	servertest.ExpectUUID(t, back.Uuid, orb.Uuid, "orb")
}

// TestAnUnreadableEntityChunkIsNotSavedOver checks that a chunk whose
// entities fail to read keeps its stored bytes through a save, even with a
// new entity standing in it.
func TestAnUnreadableEntityChunkIsNotSavedOver(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")
	srv.SpawnOrb(-14.5, 65, 14.5, 7)
	c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	srv.FlushMobs()
	srv.Shutdown()

	pos := world.ChunkPos{X: -1, Z: 0}
	path := servertest.EntityRegionPath(cfg.LevelName, pos)
	entry, record := servertest.EditChunkRecord(t, path, pos, func(record []byte) []byte {
		copy(record[5:], "not zlib")
		return record
	})
	damaged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := len(damaged) - len(record)

	restarted := *cfg
	againSrv, again := servertest.Join(t, &restarted, "Keeper")
	againSrv.SpawnOrb(-14.5, 65, 14.5, 3)
	again.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	againSrv.FlushMobs()
	againSrv.Shutdown()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after[entry:entry+4], damaged[entry:entry+4]) {
		t.Error("the unreadable chunk's header entry changed")
	}
	if len(after) < len(damaged) || !bytes.Equal(after[at:at+len(record)], record) {
		t.Error("the unreadable chunk's bytes changed")
	}
}

// TestAWorldInTheOldLayoutIsMovedOnStart checks that a world keeping the
// overworld's entities at its root, as worlds before 26.1 and this server's
// own older saves did, is moved into dimensions/minecraft/overworld on start
// with its entities intact.
func TestAWorldInTheOldLayoutIsMovedOnStart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")
	srv.SpawnOrb(-14.5, 65, 14.5, 7)
	orb := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	srv.FlushMobs()
	srv.Shutdown()

	overworld := filepath.Join(cfg.LevelName, "dimensions", "minecraft", "overworld")
	if err := os.Rename(filepath.Join(overworld, "entities"), filepath.Join(cfg.LevelName, "entities")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(cfg.LevelName, "dimensions")); err != nil {
		t.Fatal(err)
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	if _, err := os.Stat(filepath.Join(cfg.LevelName, "entities")); err == nil {
		t.Error("the old entities folder is still at the world's root")
	}
	if _, err := os.Stat(filepath.Join(overworld, "entities")); err != nil {
		t.Errorf("the entities folder was not moved: %v", err)
	}
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeExperienceOrb: "orb",
	})
	back := servertest.CapturedOr(t, again, shown, "orb", protocol.EntityTypeExperienceOrb)
	servertest.ExpectUUID(t, back.Uuid, orb.Uuid, "orb")
}

// TestACorruptChunkSectionIsRefused checks that a chunk whose block_states
// data array is too short does not bring the server down: the chunk is read
// as unreadable, generated in its place, and a player can still join. The
// stored bytes are left alone, since overwriting them would lose whatever the
// file still holds.
func TestACorruptChunkSectionIsRefused(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	// A first run saves chunk (0,0) to its region file.
	srv, _ := servertest.Start(t, cfg)
	srv.SetLoadedBlock(5, 64, 5, registry.BlockStates["minecraft:torch"])
	srv.Shutdown()

	// The section's block_states array is cut down to one long, which cannot
	// hold the section's 4096 entries.
	pos := world.ChunkPos{}
	path := servertest.ChunkRegionPath(cfg.LevelName, pos)
	entry, record := servertest.EditChunkRecord(t, path, pos, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress chunk: %v", err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		_, tag, err := nbt.Decode(raw)
		if err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		root := tag.(nbt.Compound)
		sections, _ := nbt.Get[nbt.List](root, "sections")
		if len(sections.Items) == 0 {
			t.Fatal("the saved chunk has no sections")
		}
		section := sections.Items[0].(nbt.Compound)
		section["block_states"] = nbt.Compound{
			"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String("minecraft:air")},
				nbt.Compound{"Name": nbt.String("minecraft:stone")},
			}},
			"data": nbt.LongArray{1},
		}
		out, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(out); err != nil {
			t.Fatalf("compress chunk: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("close compressor: %v", err)
		}
		newRecord := make([]byte, 5+buf.Len())
		binary.BigEndian.PutUint32(newRecord, uint32(1+buf.Len()))
		newRecord[4] = record[4]
		copy(newRecord[5:], buf.Bytes())
		return newRecord
	})
	damaged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	at := len(damaged) - len(record)

	// A world opened again over the corrupt chunk reads it as unreadable and
	// generates in its place: the join reaches the world and the server is
	// still serving.
	restarted := *cfg
	againSrv, again := servertest.Join(t, &restarted, "Reader")
	if got := againSrv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1: the corrupt chunk brought the server down", got)
	}
	again.Disconnect()
	againSrv.FlushSaves()

	// The stored bytes are untouched: a save must not overwrite what it could
	// not read.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after[entry:entry+4], damaged[entry:entry+4]) {
		t.Error("the unreadable chunk's header entry changed")
	}
	if len(after) < len(damaged) || !bytes.Equal(after[at:at+len(record)], record) {
		t.Error("the unreadable chunk's bytes changed")
	}
}

// TestAnOlderChunkWithChainsAndCreakingHeartsLoads checks that a chunk saved
// before the chain and creaking heart renames loads under the new names, and
// that the chunk is saved afterwards rather than refused.
func TestAnOlderChunkWithChainsAndCreakingHeartsLoads(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	srv.SetLoadedBlock(5, 64, 5, registry.BlockStates["minecraft:torch"])
	srv.Shutdown()

	// Section Y=4 becomes a 1.21.4 (DataVersion 4189) palette of air, a chain
	// at (1, 65, 1) and an active creaking heart at (2, 65, 2), 4 bits per entry.
	pos := world.ChunkPos{}
	path := servertest.ChunkRegionPath(cfg.LevelName, pos)
	servertest.EditChunkRecord(t, path, pos, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress chunk: %v", err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		_, tag, err := nbt.Decode(raw)
		if err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		root := tag.(nbt.Compound)
		root["DataVersion"] = nbt.Int(4189)
		data := make(nbt.LongArray, 256)
		set := func(x, y, z, paletteIdx int) {
			i := (y&15)<<8 | z<<4 | x
			data[i/16] |= int64(paletteIdx) << (4 * (i % 16))
		}
		set(1, 65, 1, 1)
		set(2, 65, 2, 2)
		sections, _ := nbt.Get[nbt.List](root, "sections")
		found := false
		for _, item := range sections.Items {
			section := item.(nbt.Compound)
			if y, _ := nbt.Get[nbt.Byte](section, "Y"); y != 4 {
				continue
			}
			found = true
			section["block_states"] = nbt.Compound{
				"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"Name": nbt.String("minecraft:air")},
					nbt.Compound{"Name": nbt.String("minecraft:chain"), "Properties": nbt.Compound{
						"axis": nbt.String("y"), "waterlogged": nbt.String("false"),
					}},
					nbt.Compound{"Name": nbt.String("minecraft:creaking_heart"), "Properties": nbt.Compound{
						"active": nbt.String("true"), "axis": nbt.String("y"), "natural": nbt.String("true"),
					}},
				}},
				"data": data,
			}
		}
		if !found {
			t.Fatal("the saved chunk has no section Y=4")
		}
		out, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(out); err != nil {
			t.Fatalf("compress chunk: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("close compressor: %v", err)
		}
		newRecord := make([]byte, 5+buf.Len())
		binary.BigEndian.PutUint32(newRecord, uint32(1+buf.Len()))
		newRecord[4] = record[4]
		copy(newRecord[5:], buf.Bytes())
		return newRecord
	})

	again, _ := servertest.Start(t, cfg)
	chain, _ := registry.FindState("minecraft:iron_chain", map[string]string{"axis": "y", "waterlogged": "false"})
	heart, _ := registry.FindState("minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "awake", "natural": "true"})
	if got := again.LoadBlockAt(1, 65, 1); got != chain {
		t.Errorf("block at (1, 65, 1) = %d, want iron_chain[axis=y,waterlogged=false] (%d)", got, chain)
	}
	if got := again.LoadBlockAt(2, 65, 2); got != heart {
		t.Errorf("block at (2, 65, 2) = %d, want creaking_heart[axis=y,creaking_heart_state=awake,natural=true] (%d)", got, heart)
	}
	stone := registry.BlockStates["minecraft:stone"]
	again.SetLoadedBlock(3, 65, 3, stone)
	again.Shutdown()

	third, _ := servertest.Start(t, cfg)
	if got := third.LoadBlockAt(3, 65, 3); got != stone {
		t.Errorf("block at (3, 65, 3) after a restart = %d, want stone (%d): the fixed chunk was not saved", got, stone)
	}
	if got := third.LoadBlockAt(1, 65, 1); got != chain {
		t.Errorf("block at (1, 65, 1) after a restart = %d, want iron_chain (%d)", got, chain)
	}
}
