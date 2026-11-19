package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The inhabited time a chunk keeps: the ticks players have spent near it,
// vanilla's InhabitedTime root tag, which every save writes and
// DifficultyInstance reads as the local share of the difficulty a spawn's
// rolls make. A chunk near a player gains one tick a tick, the value
// survives a save and a reload, and a chunk a vanilla server left old keeps
// its age.
// vanilla: ServerChunkCache.tickSpawningChunk, ChunkAccess.incrementInhabitedTime,
// ServerLevel.getCurrentDifficultyAt, DifficultyInstance.calculateDifficulty

// seedChunkFile writes a chunk in the shape a vanilla save leaves — a
// full-status column of stone sections under a plains biome — carrying the
// inhabited time the root tag holds, into the region file at path. It is
// how a test hands the server a chunk that has been where players live
// before the first connection arrives.
func seedChunkFile(t *testing.T, path string, pos world.ChunkPos, inhabited int64) {
	t.Helper()
	sections := make([]nbt.Tag, 0, 8)
	for y := int32(-4); y < 4; y++ {
		sections = append(sections, nbt.Compound{
			"Y": nbt.Byte(y),
			"block_states": nbt.Compound{
				"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"Name": nbt.String("minecraft:stone")},
				}},
			},
			"biomes": nbt.Compound{
				"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
					nbt.String("minecraft:plains"),
				}},
			},
		})
	}
	root := nbt.Compound{
		"DataVersion":   nbt.Int(4903), // the 26.2 world version, an older save than this server stamps
		"xPos":          nbt.Int(pos.X),
		"yPos":          nbt.Int(-4),
		"zPos":          nbt.Int(pos.Z),
		"Status":        nbt.String("minecraft:full"),
		"LastUpdate":    nbt.Long(0),
		"InhabitedTime": nbt.Long(inhabited),
		"sections":      nbt.List{Elem: nbt.TypeCompound, Items: sections},
		"block_ticks":   nbt.List{Elem: nbt.TypeCompound},
		"fluid_ticks":   nbt.List{Elem: nbt.TypeCompound},
		"Heightmaps":    nbt.Compound{},
		"blending_data": nbt.Compound{"min_section": nbt.Int(-4), "max_section": nbt.Int(20)},
	}

	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("compress chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("compress chunk (%d,%d): %v", pos.X, pos.Z, err)
	}

	// The record a region file holds: a length, a compression byte, then
	// the payload, padded out to whole sectors behind the two-table header.
	record := make([]byte, 0, compressed.Len()+5)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(compressed.Len()+1))
	record = append(record, length[:]...)
	record = append(record, 2) // zlib
	record = append(record, compressed.Bytes()...)
	sectors := (len(record) + 4095) / 4096

	file := make([]byte, 8192+sectors*4096)
	entry := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	binary.BigEndian.PutUint32(file[entry:], uint32(2<<8|sectors))
	copy(file[8192:], record)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create region dir: %v", err)
	}
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatalf("write region file: %v", err)
	}
}

// savedInhabitedTime reads the InhabitedTime a saved chunk carries, failing
// where the file holds no chunk or no InhabitedTime at all.
func savedInhabitedTime(t *testing.T, path string, pos world.ChunkPos) int64 {
	t.Helper()
	root := servertest.ReadBlockChunkNBT(t, path, pos)
	got, ok := nbt.Get[nbt.Long](root, "InhabitedTime")
	if !ok {
		t.Fatalf("chunk (%d,%d) in %s carries no InhabitedTime", pos.X, pos.Z, path)
	}
	return int64(got)
}

// TestInhabitedTimeAdvancesAChunkAPlayerStandsIn checks the two halves of
// the age a chunk keeps: a tick of the loop a player stands near it is a
// tick of its InhabitedTime, and the count a save writes is the one the
// next server's join reads back and carries on from. The loop is stopped
// and driven by hand, so both runs' counts are exact. The first save is the
// generated chunk's own; the reloaded chunk is changed in the second run,
// since its age alone does not get it saved.
func TestInhabitedTimeAdvancesAChunkAPlayerStandsIn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg, servertest.VanillaSaves)
	srv.StopTick()
	connect().Join("Prey")

	for range 25 {
		srv.RunTick()
	}
	srv.Shutdown()

	// The chunk beside the spawn's: the spawn search loads the spawn's own
	// chunk and gives it back before the join, which saves it as generated,
	// so the age it gains after that has no change to reach disk with.
	near := world.ChunkPos{X: 1}
	if got := savedInhabitedTime(t, servertest.ChunkRegionPath(cfg.LevelName, near), near); got != 25 {
		t.Errorf("InhabitedTime after the first run = %d, want 25: one tick of the loop a tick of age", got)
	}

	// A world opened again over the same directory: the chunk the first
	// server saved is the one this join loads, and the age it carried is
	// where this run's counting starts from.
	restarted := *cfg
	again, reconnect := servertest.Start(t, &restarted)
	again.StopTick()
	reconnect().Join("Prey")

	for range 15 {
		again.RunTick()
	}
	again.SetBlockInDimension(world.DimensionName, 24, 100, 8, registry.BlockStates["minecraft:stone"])
	again.Shutdown()

	if got := savedInhabitedTime(t, servertest.ChunkRegionPath(restarted.LevelName, near), near); got != 40 {
		t.Errorf("InhabitedTime after the second run = %d, want 40: the 25 the reload read and the 15 the run added", got)
	}
}

// TestAnUnchangedChunksAgeAloneIsNotSaved checks that the age marks nothing
// for saving: a chunk read back from disk and changed by nothing keeps the
// age its file holds, however long a player stands in it.
// vanilla: ChunkAccess.incrementInhabitedTime, ChunkMap.save
func TestAnUnchangedChunksAgeAloneIsNotSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	spawn := world.ChunkPos{}
	seedChunkFile(t, servertest.ChunkRegionPath(cfg.LevelName, spawn), spawn, 500)

	srv, connect := servertest.Start(t, cfg, servertest.VanillaSaves)
	srv.StopTick()
	connect().Join("Prey")

	for range 25 {
		srv.RunTick()
	}
	srv.Shutdown()

	if got := savedInhabitedTime(t, servertest.ChunkRegionPath(cfg.LevelName, spawn), spawn); got != 500 {
		t.Errorf("InhabitedTime on disk = %d, want the 500 the file held", got)
	}
}

// TestALocalDifficultyRisesWithAChunkLongInhabited checks the point of the
// age: the chunk a vanilla server left long-inhabited loads with its
// InhabitedTime, and a husk spawned into it strikes with the local
// difficulty those ticks buy. A fresh chunk on a hard world would give the
// hunger 140 ticks per whole effective difficulty of 2.25 — 280, what the
// variant tests see — while one players have lived in buys the full local
// share, an effective 5.25 and hunger for 700.
// vanilla: DifficultyInstance.calculateDifficulty, Husk.doHurtTarget
func TestALocalDifficultyRisesWithAChunkLongInhabited(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()

	// A chunk four chunks out, so the join's own ground at spawn stays the
	// flat world's, carrying the InhabitedTime of two hundred hours' play:
	// past the clamp, the full local share of the difficulty.
	longLived := world.ChunkPos{X: 4, Z: 4}
	seedChunkFile(t, servertest.ChunkRegionPath(cfg.LevelName, longLived), longLived, 7_200_000)

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Prey"))
	srv.SetDifficulty(3) // hard
	c := connect()
	playerID := c.JoinAs("Prey")
	c.MoveTo(66.5, 64, 66.5, 0, 0)
	c.Settle()

	// The husk four blocks away, both of them inside the long-inhabited
	// chunk, so the difficulty the blow reads from where it stands is the
	// one the chunk's age buys. Seed 1 rolls it empty-handed: a husk holding
	// a weapon gives no hunger (vanilla: Husk.doHurtTarget).
	husk := srv.SpawnMobSeeded("minecraft:husk", 70.5, 64, 70.5, 1)
	if hand := srv.MobMainHand(husk); hand.ItemID != 0 {
		t.Fatalf("the husk spawned holding item %d, want an empty hand", hand.ItemID)
	}

	hunger := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		got := servertest.MobEffectFor(data)
		if got.Entity != playerID {
			return false
		}
		hunger = true
		if got.Effect != registry.MustMobEffect("minecraft:hunger") || got.Amplifier != 0 || got.Duration != 700 {
			t.Errorf("the husk's blow gave %+v, want hunger I for 700 ticks of the chunk's local difficulty", got)
		}
		return true
	})
	if !hunger {
		t.Error("the husk's blow never gave its hunger")
	}
	c.Disconnect()
}
