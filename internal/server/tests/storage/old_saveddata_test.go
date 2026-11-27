package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// writeOldSavedData writes data/name of an old-layout world, saved at version.
func writeOldSavedData(t *testing.T, level, name string, version int32, data nbt.Compound) {
	t.Helper()
	servertest.WritePlayerFile(t, filepath.Join(level, "data", name), nbt.Compound{
		"DataVersion": nbt.Int(version), "data": data,
	})
}

// A world saved by 1.21.4 keeps its forced chunks: Forced is a long array of
// packed chunk positions (x in the low half, z in the high half).
func TestOldWorldForcedChunksSurvive(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	packed := nbt.Long(int64(uint32(3)) | int64(-2)<<32)
	writeOldSavedData(t, cfg.LevelName, "chunks.dat", 3955, nbt.Compound{"Forced": nbt.LongArray{int64(packed)}})

	srv, _ := servertest.Start(t, cfg)
	var out string
	for _, l := range srv.RunLine("forceload query 48 -32") {
		out += l.Plain() + "\n"
	}
	if !strings.Contains(out, "Chunk at [3, -2] in minecraft:overworld is marked for force loading") {
		t.Errorf("forceload query = %q, want chunk [3, -2] marked", out)
	}
}

// A raid running when a 1.21.4 world was saved comes back under the current
// names. No village stands there, so it ends at once as a loss, and the bar
// of the finished raid shows the player.
func TestOldWorldRaidSurvives(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	hero := nbt.IntArray{1, 2, 3, 4}
	writeOldSavedData(t, cfg.LevelName, "raids.dat", 3955, nbt.Compound{
		"NextAvailableID": nbt.Int(2), "Tick": nbt.Int(100),
		"Raids": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"Id": nbt.Int(1), "Started": nbt.Byte(1), "Active": nbt.Byte(1), "TicksActive": nbt.Long(50),
			"BadOmenLevel": nbt.Int(2), "GroupsSpawned": nbt.Int(1), "PreRaidTicks": nbt.Int(0),
			"PostRaidTicks": nbt.Int(0), "TotalHealth": nbt.Float(20), "NumGroups": nbt.Int(5),
			"Status": nbt.String("ongoing"), "CX": nbt.Int(0), "CY": nbt.Int(70), "CZ": nbt.Int(0),
			"HeroesOfTheVillage": nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{hero}},
		}}},
	})

	srv, c := servertest.Join(t, cfg, "Hero")
	raids := srv.Raids()
	if len(raids) != 1 {
		t.Fatalf("raids = %d, want the saved one", len(raids))
	}
	r := raids[0]
	if !r.Started || !r.Active || r.OmenLevel != 2 || r.Groups != 1 || r.X != 0 || r.Y != 70 || r.Z != 0 {
		t.Errorf("raid = %+v, want the saved one", r)
	}
	var added bool
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id == protocol.IDPlayBossEvent {
			rd := protocol.NewReader(data)
			added = added || rd.ReadUUID() == r.BarWire && rd.ReadVarInt() == protocol.BossEventAdd
		}
		return false
	})
	if !added {
		t.Errorf("the player near the raid was not shown its bar")
	}
}

// A ticket file with a long chunk_pos in a current-version file, a short int
// array and a non-forced ticket loads without a panic and keeps the good ticket.
func TestChunkTicketsOddPositionsLoad(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	packed := int64(uint32(5)) | int64(7)<<32
	ticket := func(typ string, pos nbt.Tag) nbt.Compound {
		return nbt.Compound{"type": nbt.String(typ), "level": nbt.Int(31), "ticks_left": nbt.Long(0), "chunk_pos": pos}
	}
	writeOldSavedData(t, filepath.Join(cfg.LevelName, "dimensions/minecraft/overworld"), "minecraft/chunk_tickets.dat", 4400, nbt.Compound{
		"tickets": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			ticket("minecraft:forced", nbt.Long(packed)),
			ticket("minecraft:forced", nbt.IntArray{1}),
			ticket("minecraft:other", nbt.Long(packed)),
		}},
	})

	srv, _ := servertest.Start(t, cfg)
	var out string
	for _, l := range srv.RunLine("forceload query 80 112") {
		out += l.Plain() + "\n"
	}
	if !strings.Contains(out, "Chunk at [5, 7] in minecraft:overworld is marked for force loading") {
		t.Errorf("forceload query = %q, want chunk [5, 7] marked", out)
	}
}
