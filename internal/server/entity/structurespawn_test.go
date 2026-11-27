package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type structureSpawnTestWorld struct {
	spawnTestWorld
	id      string
	village bool
	cats    int
}

func (w structureSpawnTestWorld) StructureContains(x, y, z int32, id string, piece bool) bool {
	return id == w.id && x >= -20 && x <= 20 && z >= -20 && z <= 20 && y >= 40 && y <= 70 &&
		(piece == (id == "minecraft:swamp_hut" || id == "minecraft:trial_chambers"))
}
func (w structureSpawnTestWorld) CloseToVillage(_, _, _ int32, _ int) bool { return w.village }
func (w structureSpawnTestWorld) VillageBeds(_, _, _ int32, _ int) int     { return 0 }
func (w structureSpawnTestWorld) CatsIn(_, _, _ int32, _, _ float64) int   { return w.cats }

func TestStructureOverrideSpawnsWitchesGuardiansAndCats(t *testing.T) {
	for _, tc := range []struct {
		id       string
		mob      MobType
		category SpawnCategory
	}{
		{"minecraft:swamp_hut", TypeWitch, CategoryMonster},
		{"minecraft:swamp_hut", TypeCat, CategoryCreature},
		{"minecraft:monument", TypeGuardian, CategoryMonster},
		{"minecraft:pillager_outpost", TypePillager, CategoryMonster},
	} {
		t.Run(tc.mob.Name(), func(t *testing.T) {
			w := structureSpawnTestWorld{spawnTestWorld: newSpawnTestWorld(), id: tc.id}
			w.biomeName = "minecraft:swamp"
			if tc.mob == TypeCat || tc.mob == TypePillager {
				w.sky = 15
			}
			for x := int32(-30); x <= 30; x++ {
				for z := int32(-30); z <= 30; z++ {
					w.set(x, 49, z, registry.BlockGrassBlock.DefaultState())
					if tc.mob == TypeGuardian {
						for y := int32(49); y <= 52; y++ {
							w.set(x, y, z, registry.BlockWater.DefaultState())
						}
					}
				}
			}
			spawned := 0
			for seed := range int64(30) {
				var bodies SpawnBodies
				counts := SpawnCounts{PerPlayer: make(map[int32]int)}
				spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 50, 0, 0,
					[]SpawnPlayer{{EntityID: 1, X: 80, Y: 50, Z: 0}}, &bodies, &SpawnPotential{}, &counts, tc.category, func(r SpawnRequest) {
						if r.Type != tc.mob {
							t.Errorf("spawned %s, want %s", r.Type, tc.mob)
						}
						spawned++
					})
			}
			if spawned == 0 {
				t.Fatal("no structure mob spawned")
			}
			data := pickSpawnData(w, rand.New(rand.NewSource(1)), 0, 50, 0, tc.category)
			if data == nil || canSpawnMobAt(w, 21, 50, 0, tc.category, data) {
				t.Fatal("structure pack may escape the structure")
			}
		})
	}
}

func TestGuardianNaturalSpawnRules(t *testing.T) {
	w := newSpawnTestWorld()
	water := registry.BlockWater.DefaultState()
	w.set(0, 49, 0, water)
	w.set(0, 50, 0, water)
	ok := func() bool {
		return validSpawnPositionForType(w, rand.New(rand.NewSource(1)), TypeGuardian, 0, 50, 0, 100, CategoryMonster)
	}
	if !ok() {
		t.Fatal("covered water refused a guardian")
	}
	w.diff, w.diffUp = rules.DifficultyPeaceful, true
	if ok() {
		t.Fatal("guardian spawned in peaceful")
	}
	w.diff = rules.DifficultyNormal
	w.set(0, 49, 0, registry.BlockStone.DefaultState())
	if ok() {
		t.Fatal("guardian spawned without water below")
	}
	w.set(0, 49, 0, water)
	w.sky = 15
	passed := 0
	rng := rand.New(rand.NewSource(2))
	for range 2000 {
		if validSpawnPositionForType(w, rng, TypeGuardian, 0, 50, 0, 100, CategoryMonster) {
			passed++
		}
	}
	if passed < 60 || passed > 140 {
		t.Fatalf("open water passed %d/2000, want about 100", passed)
	}
	w.set(0, 60, 0, registry.BlockStone.DefaultState())
	if !ok() {
		t.Fatal("roof below sea level did not shelter guardian")
	}
}

// structureDispatchWorld deliberately has no reference method.
type structureDispatchWorld struct{ spawnTestWorld }

func (w structureDispatchWorld) StructureContains(x, y, z int32, id string, piece bool) bool {
	if y < 40 || y > 70 || z < -20 || z > 20 {
		return false
	}
	switch id {
	case "minecraft:swamp_hut":
		return x < 16 && piece
	case "minecraft:monument":
		return x < 32 && !piece
	case "minecraft:pillager_outpost":
		return x >= 16 && !piece
	case "minecraft:ancient_city":
		return x >= 32 && x < 48 && !piece
	case "minecraft:trial_chambers":
		return x >= 48 && piece
	}
	return false
}

type referencedDispatchWorld struct {
	structureDispatchWorld
	loaded bool
}

func (w *referencedDispatchWorld) StructureReferences(x, z int32) []string {
	if !w.loaded {
		return nil
	}
	switch x >> 4 {
	case -1, 0:
		return []string{"minecraft:monument", "minecraft:swamp_hut", "minecraft:swamp_hut"}
	case 1:
		return []string{"minecraft:pillager_outpost", "minecraft:monument"}
	case 2:
		return []string{"minecraft:ancient_city"}
	default:
		return []string{"minecraft:trial_chambers"}
	}
}

type spawnSinkGenerator struct{ *world.FlatGenerator }

func (g spawnSinkGenerator) Generate(c *world.Chunk) {
	g.FlatGenerator.Generate(c)
	c.Fill(68, 70, registry.BlockStone.DefaultState())
}
