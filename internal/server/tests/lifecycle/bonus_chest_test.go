package lifecycle

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// bonusChestWorld starts a fresh world and returns its folder and the chests
// and torches in the spawn chunk.
func bonusChestWorld(t *testing.T, bonus bool) (dir string, chests [][3]int32, torches int, tag nbt.Compound) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.BonusChest = bonus
	srv, _ := servertest.Start(t, cfg)

	spawn := srv.WorldSpawn()
	x0, z0 := spawn.X>>4*16, spawn.Z>>4*16
	// The chunk was released after placing, so a read must hold it (a saved
	// chunk reloads) instead of seeing air for an unloaded one.
	defer srv.HoldChunk(x0, z0)()
	for x := x0; x < x0+16; x++ {
		for z := z0; z < z0+16; z++ {
			for y := int32(-64); y < 128; y++ {
				switch registry.BlockOf(srv.BlockAt(x, y, z)) {
				case registry.BlockChest:
					chests = append(chests, [3]int32{x, y, z})
				case registry.BlockTorch:
					torches++
				}
			}
		}
	}
	if len(chests) == 1 {
		tag, _ = srv.BlockEntityTagAt(chests[0][0], chests[0][1], chests[0][2])
	}
	return cfg.LevelName, chests, torches, tag
}

func worldGenBonusChest(t *testing.T, dir string) int8 {
	t.Helper()
	data := servertest.ReadSavedFile(t, filepath.Join(dir, "data", "minecraft", "world_gen_settings.dat"), "data")
	v, _ := nbt.ByteField(data, "bonus_chest")
	return v
}

// TestBonusChestFlagPlacesSpawnChest checks that --bonusChest gives a new
// world a loot chest in the spawn chunk with a torch beside it.
// vanilla: MinecraftServer.setInitialSpawn, BonusChestFeature.place
func TestBonusChestFlagPlacesSpawnChest(t *testing.T) {
	t.Parallel()
	dir, chests, torches, tag := bonusChestWorld(t, true)
	if len(chests) != 1 {
		t.Fatalf("chests in the spawn chunk = %v, want one", chests)
	}
	if got, _ := nbt.Get[nbt.String](tag, "LootTable"); got != "minecraft:chests/spawn_bonus_chest" {
		t.Errorf("LootTable = %q, want minecraft:chests/spawn_bonus_chest", got)
	}
	if torches == 0 {
		t.Error("no torch in the spawn chunk, want at least one beside the chest")
	}
	if got := worldGenBonusChest(t, dir); got != 1 {
		t.Errorf("world_gen_settings bonus_chest = %d, want 1", got)
	}
}

// TestNoBonusChestWithoutTheFlag checks that a world made without
// --bonusChest has no chest and saves bonus_chest:0b.
func TestNoBonusChestWithoutTheFlag(t *testing.T) {
	t.Parallel()
	dir, chests, _, _ := bonusChestWorld(t, false)
	if len(chests) != 0 {
		t.Errorf("chests in the spawn chunk = %v, want none", chests)
	}
	if got := worldGenBonusChest(t, dir); got != 0 {
		t.Errorf("world_gen_settings bonus_chest = %d, want 0", got)
	}
}
