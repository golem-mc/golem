package mobs

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: CatSpawner.spawnInHut, Cat.finalizeSpawn
func TestAHutSpawnsAPersistentBlackCat(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	const hut = "minecraft:swamp_hut"
	plantHutChunk(t, cfg.LevelName, world.ChunkPos{}, nbt.Compound{
		"starts": nbt.Compound{hut: nbt.Compound{
			"id": nbt.String(hut), "ChunkX": nbt.Int(0), "ChunkZ": nbt.Int(0),
			"Children": nbt.NewList([]nbt.Tag{nbt.Compound{"BB": nbt.IntArray{0, 64, 2, 6, 70, 10}}}),
		}},
		"References": nbt.Compound{hut: nbt.LongArray{0}},
	})
	srv, c := servertest.Join(t, cfg, "Watcher", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(4 * 24000) // A new moon excludes all_black from the ordinary variant draw.
	c.MoveToGround(24.5, 64, 24.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 1)

	// Seed 1 rolls (-23, -19) from the player, inside the saved hut piece.
	srv.CatSpawnerPass(1)
	cat := c.ExpectMobSpawn(protocol.EntityTypeCat)
	if x, y, z, ok := srv.MobPos(cat.EntityID); !ok || x != 1.5 || y != 64 || z != 5.5 {
		t.Fatalf("hut cat at (%v, %v, %v), present %v, want (1.5, 64, 5.5)", x, y, z, ok)
	}
	if !srv.MobPersistent(cat.EntityID) {
		t.Error("hut spawner's cat is not persistent")
	}
	if cat.Variant != 0 {
		t.Errorf("hut cat variant = %d, want minecraft:all_black (0)", cat.Variant)
	}

	srv.SpawnMobSeeded("minecraft:cat", 7.5, 64, 5.5, 4)
	outside := c.ExpectMobSpawn(protocol.EntityTypeCat)
	if outside.Variant == 0 {
		t.Error("cat outside the hut piece received the all_black override under a new moon")
	}
	if srv.MobPersistent(outside.EntityID) {
		t.Error("ordinary cat outside the hut is persistent")
	}
}

// plantHutChunk writes a minimal full chunk with a floor at y=63 and
// imported structure records, before the server opens its region files.
func plantHutChunk(t *testing.T, level string, pos world.ChunkPos, structures nbt.Compound) {
	t.Helper()
	root := nbt.Compound{
		"DataVersion": nbt.Int(4903), "Status": nbt.String("minecraft:full"),
		"xPos": nbt.Int(pos.X), "zPos": nbt.Int(pos.Z), "structures": structures,
		"sections": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Y": nbt.Byte(3),
			"block_states": nbt.Compound{"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"Name": nbt.String("minecraft:stone")},
			}}},
		}}},
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(servertest.MustEncodeNBT(t, root)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := compressed.Bytes()
	sectors := (5 + len(payload) + 4095) / 4096
	raw := make([]byte, 8192+sectors*4096)
	index := int((pos.X&31)+(pos.Z&31)*32) * 4
	binary.BigEndian.PutUint32(raw[index:], uint32(2<<8|sectors))
	binary.BigEndian.PutUint32(raw[8192:], uint32(len(payload)+1))
	raw[8196] = 2
	copy(raw[8197:], payload)
	path := servertest.ChunkRegionPath(level, pos)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
