package entity

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
				var bodies []SpawnBody
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

func TestTrialChambersAndAncientCitiesSpawnNothing(t *testing.T) {
	for _, id := range []string{"minecraft:trial_chambers", "minecraft:ancient_city"} {
		w := structureSpawnTestWorld{spawnTestWorld: newSpawnTestWorld(), id: id}
		for _, category := range []SpawnCategory{CategoryMonster, CategoryCreature, CategoryAmbient, CategoryAxolotls,
			CategoryUndergroundWater, CategoryWaterCreature, CategoryWaterAmbient} {
			if got := spawnListAt(w, 0, 50, 0, category, registry.BiomePlains); len(got) != 0 {
				t.Errorf("%s category %d kept %d spawns", id, category, len(got))
			}
		}
		if len(spawnListAt(w, 21, 50, 0, CategoryMonster, registry.BiomePlains)) == 0 {
			t.Errorf("%s: outside the box lost the biome list", id)
		}
	}
}

func TestMonumentSuppressesOtherWaterSpawns(t *testing.T) {
	w := structureSpawnTestWorld{spawnTestWorld: newSpawnTestWorld(), id: "minecraft:monument"}
	for _, category := range []SpawnCategory{CategoryAxolotls, CategoryUndergroundWater} {
		if got := spawnListAt(w, 0, 50, 0, category, registry.BiomeLushCaves); len(got) != 0 {
			t.Fatal("monument retained water spawns")
		}
		if got := spawnListAt(w, 21, 50, 0, category, registry.BiomeLushCaves); len(got) == 0 {
			t.Fatal("outside monument lost biome spawns")
		}
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

func TestHutCatSpawnerReplenishesOnlyAnEmptyHut(t *testing.T) {
	for _, tc := range []struct {
		cats    int
		village bool
		want    int
	}{{0, false, 1}, {1, false, 0}, {0, true, 0}} {
		w := structureSpawnTestWorld{spawnTestWorld: newSpawnTestWorld(), id: "minecraft:swamp_hut", cats: tc.cats, village: tc.village}
		// The seeded player offsets are between 8 and 31; centre the hut on the roll.
		rng := rand.New(rand.NewSource(4))
		rng.Int63() // Consume the player selection draw.
		x := int32(catSpawnNear + rng.Intn(catSpawnSpan))
		if rng.Intn(2) == 1 {
			x = -x
		}
		z := int32(catSpawnNear + rng.Intn(catSpawnSpan))
		if rng.Intn(2) == 1 {
			z = -z
		}
		w.set(0, 49, 0, registry.BlockSprucePlanks.DefaultState())
		var requests []SpawnRequest
		var spawner CatSpawner
		spawner.Tick(w, rand.New(rand.NewSource(4)), []SpawnPlayer{{X: float64(-x), Y: 50, Z: float64(-z)}}, func(r SpawnRequest) { requests = append(requests, r) })
		if len(requests) != tc.want {
			t.Fatalf("cats %d village %v: got %d requests, want %d", tc.cats, tc.village, len(requests), tc.want)
		}
		if len(requests) > 0 && !requests[0].Persistent {
			t.Fatal("hut cat is not persistent")
		}
	}
}

func TestStructurePiglinKeepsTemplateAgeAndWeapon(t *testing.T) {
	armored := 0
	for seed := int64(1); seed <= 128; seed++ {
		for _, baby := range []bool{false, true} {
			m := NewMob(TypePiglin, 1, 0, 64, 0, seed)
			m.SetBaby(baby)
			m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
			m.FinalizeStructureSpawn(m.SpawnDice(), testBiome("minecraft:nether_wastes"), false, rules.LocalDifficulty{Special: 1})
			if m.IsBaby() != baby || registry.Item(m.MainHand.ItemID) != registry.ItemCrossbow {
				t.Fatalf("seed %d: template age or weapon changed", seed)
			}
			for _, stack := range m.Worn {
				if !stack.Empty() {
					if baby {
						t.Fatal("baby received armor")
					}
					armored++
				}
			}
		}
	}
	if armored == 0 {
		t.Fatal("structure piglins never rolled armor")
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

func TestNaturalSpawnOverrideDispatch(t *testing.T) {
	base := structureDispatchWorld{newSpawnTestWorld()}
	base.biomeName = "minecraft:swamp"
	referenced := &referencedDispatchWorld{base, true}
	for _, w := range []SpawnWorld{base, referenced} {
		c := naturalSpawnContext{w: w}
		for _, loaded := range []bool{true, false, true} {
			referenced.loaded = loaded
			for _, x := range []int32{-1, 0, 15, 16, 31, 32, 47, 48, 64, 0} {
				for cat := range SpawnCategory(SpawnCategoryCount) {
					want := spawnListFor(cat, registry.BiomeSwamp)
					for _, o := range overworldSpawnOverrides {
						if !o.all && o.category != cat {
							continue
						}
						if rw, ok := w.(structureReferenceWorld); ok && !slices.Contains(rw.StructureReferences(x, 0), o.id) {
							continue
						}
						if w.(structureSpawnWorld).StructureContains(x, 50, 0, o.id, o.piece) {
							want = o.spawns
							break
						}
					}
					got := c.listAt(x, 50, 0, cat, registry.BiomeSwamp)
					if !reflect.DeepEqual(got.entries, want) {
						t.Fatalf("%T loaded=%t x=%d cat=%d: %v, want %v", w, loaded, x, cat, got.entries, want)
					}
					for seed := int64(1); seed <= 30; seed++ {
						a, b := rand.New(rand.NewSource(seed)), rand.New(rand.NewSource(seed))
						if !reflect.DeepEqual(got.pick(a), pickFromList(want, GoDice{b})) || a.Int63() != b.Int63() {
							t.Fatal("override result or RNG tail changed")
						}
					}
					// Membership is by entry value even across distinct descriptors.
					other := describeSpawnList(slices.Clone(got.entries))
					for _, e := range other.entries {
						if !canSpawnMobAt(w, x, 50, 0, cat, &e) {
							t.Fatal("descriptor membership")
						}
					}
				}
			}
		}
	}
}

type spawnSinkGenerator struct{ *world.FlatGenerator }

func (g spawnSinkGenerator) Generate(c *world.Chunk) {
	g.FlatGenerator.Generate(c)
	c.Fill(68, 70, registry.BlockStone.DefaultState())
}

func TestNaturalSpawnSinkBiomeEdit(t *testing.T) {
	w := world.NewDimensionWorld(t.TempDir(), true, spawnSinkGenerator{world.NewFlatGenerator()})
	defer w.Close()
	for x := int32(9); x <= 11; x++ {
		for z := int32(9); z <= 11; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			defer w.Release(pos)
		}
	}
	w.SetClockTime("minecraft:overworld", 18000)
	held, _ := w.HoldSpawnChunk(world.ChunkPos{X: 10, Z: 10})
	defer held.Close()
	c := naturalSpawnContext{w: &held, chunk: &held}
	for seed := int64(1); seed <= 30; seed++ {
		var bodies []SpawnBody
		var last SpawnRequest
		spawned := false
		c.spawnCategoryForPosition(readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{X: 10, Z: 10}, 64, 168, 168,
			[]SpawnPlayer{{EntityID: 1, X: 230, Y: 64, Z: 168}}, &bodies, &SpawnPotential{}, &SpawnCounts{}, CategoryMonster, func(r SpawnRequest) {
				last, spawned = r, true
				_, _, ok := w.FillBiomes(world.BiomeBox{MinX: 144, MaxX: 191, MinY: 48, MaxY: 80, MinZ: 144, MaxZ: 191}, int32(registry.BiomeDesert), nil)
				if !ok {
					t.Fatal("sink fill failed")
				}
			})
		if spawned {
			if got := held.Biome(int32(last.X), int32(last.Y), int32(last.Z)); got != registry.BiomeDesert {
				t.Fatalf("sink retained biome %v", got)
			}
			return
		}
	}
	t.Fatal("no synchronous sink fixture")
}
