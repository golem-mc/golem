package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The spawn's obstruction test, driven against the stub world: the dice are
// out of it, only the bodies handed in decide. Below them the chunk's
// original herd, rolled against the same stub over a pasture, its dice the
// seed scans below hunt.

// TestSpawnRefusesABodyInTheWay checks the entity half of the spawn's
// obstruction test: a mob's box that overlaps an existing blocksBuilding
// body is refused, and the same cell stands one up with no body in it.
// vanilla: NaturalSpawner.isValidPositionForMob, Mob.checkSpawnObstruction
func TestSpawnRefusesABodyInTheWay(t *testing.T) {
	w := newSpawnTestWorld()
	x, z := atChunk(0, 0)
	const y = 30
	cx, cz := float64(x)+0.5, float64(z)+0.5
	bodies := []SpawnBody{{
		MinX: cx - 0.4, MinY: y, MinZ: cz - 0.4,
		MaxX: cx + 0.4, MaxY: y + 1, MaxZ: cz + 0.4,
	}}
	if validPositionForMob(w, registry.EntityTypeZombie, x, y, z, 100, bodies) {
		t.Fatal("a spawn landed inside an existing body's box")
	}
	if !validPositionForMob(w, registry.EntityTypeZombie, x, y, z, 100, nil) {
		t.Fatal("the same cell was refused with no body in the way")
	}
}

// overlapsBody is the test's own box test, independent of the production
// one, so a check defeated in the code under test cannot hide an overlap.
func overlapsBody(list []SpawnBody, r SpawnRequest) bool {
	wid, hgt := KindDims(r.Type)
	minX, maxX := r.X-wid/2, r.X+wid/2
	minY, maxY := r.Y, r.Y+hgt
	minZ, maxZ := r.Z-wid/2, r.Z+wid/2
	for i := range list {
		b := &list[i]
		if b.MinX < maxX && b.MaxX > minX &&
			b.MinY < maxY && b.MaxY > minY &&
			b.MinZ < maxZ && b.MaxZ > minZ {
			return true
		}
	}
	return false
}

// spawnRequestBox is the test's own box of a spawned body, built from
// KindDims so it stays independent of the production spawnBox.
func spawnRequestBox(r SpawnRequest) SpawnBody {
	wid, hgt := KindDims(r.Type)
	return SpawnBody{
		MinX: r.X - wid/2, MinY: r.Y, MinZ: r.Z - wid/2,
		MaxX: r.X + wid/2, MaxY: r.Y + hgt, MaxZ: r.Z + wid/2,
	}
}

// TestOriginalMobsGrowAHerdOfOneKind rolls a chunk's original animals over a
// pasture stub: the biome's list picks the kind, the entry's size span the
// count, and every member carries the one group whose dice the finalize
// threads the herd's babies through — no cap, no player, only the ground.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration
func TestOriginalMobsGrowAHerdOfOneKind(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 15
	grass := defaultState(t, "minecraft:grass_block")
	for x := range int32(16) {
		for z := range int32(16) {
			w.set(x, 29, z, grass)
		}
	}
	w.surface = 30

	var reqs []SpawnRequest
	spawnOriginalMobsAt(w, seedOpeningAHerd(), func(r SpawnRequest) { reqs = append(reqs, r) })
	if len(reqs) != 4 {
		t.Fatalf("the herd stands %d animals, want the four the span of the passive entries rolls", len(reqs))
	}
	for _, r := range reqs {
		if r.Type != reqs[0].Type {
			t.Errorf("the herd stands a %s beside its %s, want the one kind a herd is", r.Type, reqs[0].Type)
		}
		if r.Group != reqs[0].Group {
			t.Error("the herd's animals carry separate groups, want the one pack a herd finalizes in")
		}
		if r.X < 0 || r.X > 16 || r.Z < 0 || r.Z > 16 {
			t.Errorf("a %s stands at (%.1f, %.1f), want the animal inside its chunk", r.Type, r.X, r.Z)
		}
		if r.Y != 30 {
			t.Errorf("a %s stands at y %.0f, want the pasture's y 30", r.Type, r.Y)
		}
	}
}

// pastureStub is a spawnTestWorld whose columns say their own tops: some of
// its pasture grew short grass on its grass blocks, and the heightmap answer
// a column gives is its own top, the stub's one-surface answer aside.
type pastureStub struct {
	spawnTestWorld
	grown map[[2]int32]bool
}

// SurfaceHeight is the top of the column: one over the grass block where
// the short grass grew, the grass block's own y where it did not.
func (w pastureStub) SurfaceHeight(x, z int32) int32 {
	if w.grown[[2]int32{x, z}] {
		return w.surface + 1
	}
	return w.surface
}

// TestValidPositionRefusesADarkWalkTarget holds the walk-target check: in the
// dark an animal's cell scores below 0 off grass and passes over it, and a
// kind that is no PathfinderMob skips the check.
// vanilla: PathfinderMob.checkSpawnRules, Animal.getWalkTargetValue
func TestValidPositionRefusesADarkWalkTarget(t *testing.T) {
	at := func(block string, kind MobType) bool {
		w := newSpawnTestWorld()
		w.sky = 0
		w.set(0, 29, 0, defaultState(t, block))
		return validPositionAt(w, kind, 0.5, 30, 0.5, 0, nil)
	}
	if !at("minecraft:grass_block", TypeCow) {
		t.Error("a cow over grass was refused, want it passed")
	}
	if at("minecraft:snow_block", TypeFox) {
		t.Error("a fox over dark snow_block passed, want it refused")
	}
	if !at("minecraft:snow_block", TypeSlime) {
		t.Error("a slime was refused, want the check skipped")
	}
	if !at("minecraft:snow_block", TypeSquid) {
		t.Error("a squid was refused, want its walk-target value 0")
	}
	if !at("minecraft:snow_block", TypeNautilus) {
		t.Error("a nautilus in dark water was refused, want its walk-target value 0")
	}
	w := newSpawnTestWorld()
	w.sky = 10
	w.set(0, 29, 0, defaultState(t, "minecraft:oak_leaves"))
	if validPositionAt(w, TypeParrot, 0.5, 30, 0.5, 0, nil) {
		t.Error("a parrot over leaves at light 10 passed, want it refused")
	}
}

// seedOpeningAParrotHerd scans the level seeds in order for one whose spawn
// dice at chunk 0,0 open the gate and pick the jungle's parrots.
func seedOpeningAParrotHerd() int64 {
	for seed := int64(1); ; seed++ {
		f, _ := originalMobDice(seed, ChunkPos{}, false)
		if f.NextFloat() >= creatureSpawnProbability {
			continue
		}
		if data := pickFromList(CategoryCreature.lists()[registry.BiomeJungle], f); data != nil && data.mob == TypeParrot {
			return seed
		}
	}
}

// spawnOriginalMobsAt runs SpawnOriginalMobs at chunk 0,0 on the dice
// OriginalMobs builds for a level seed.
func spawnOriginalMobsAt(w SpawnWorld, seed int64, sink func(SpawnRequest)) {
	spawn, region := originalMobDice(seed, ChunkPos{}, false)
	SpawnOriginalMobs(w, spawn, region, ChunkPos{}, sink)
}

// seedOpeningAHerd scans the level seeds in order for one whose spawn dice
// at chunk 0,0 open the gate and pick a simulated plains kind of a fixed four.
func seedOpeningAHerd() int64 {
	for seed := int64(1); ; seed++ {
		f, _ := originalMobDice(seed, ChunkPos{}, false)
		if f.NextFloat() >= creatureSpawnProbability {
			continue
		}
		if data := pickFromList(CategoryCreature.lists()[registry.BiomePlains], f); data == nil || !KnownMobKind(data.mob) || data.minCount != 4 || data.maxCount != 4 {
			continue
		}
		return seed
	}
}

// seedOpeningTheGate scans the level seeds in order for one whose first
// spawn float at chunk 0,0 alone rolls under the creature probability:
// whatever the list then answers, the gate stood open.
func seedOpeningTheGate() int64 {
	for seed := int64(1); ; seed++ {
		if f, _ := originalMobDice(seed, ChunkPos{}, false); f.NextFloat() < creatureSpawnProbability {
			return seed
		}
	}
}

// TestBiomeSpawnListsCoverTheVanillaBiomes pins that biomes beyond the ones
// this server grows still carry their data files' lists.
// vanilla: data/minecraft/worldgen/biome/*.json, "spawners"
func TestBiomeSpawnListsCoverTheVanillaBiomes(t *testing.T) {
	has := func(list []spawnData, mob MobType) bool {
		for _, e := range list {
			if e.mob == mob {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		biome string
		cat   SpawnCategory
		mob   MobType
	}{
		{"minecraft:taiga", CategoryCreature, registry.EntityTypeSheep},
		{"minecraft:taiga", CategoryCreature, registry.EntityTypeWolf},
		{"minecraft:taiga", CategoryMonster, registry.EntityTypeZombie},
		{"minecraft:desert", CategoryCreature, registry.EntityTypeRabbit},
		{"minecraft:desert", CategoryMonster, registry.EntityTypeHusk},
		{"minecraft:swamp", CategoryMonster, registry.EntityTypeSlime},
		{"minecraft:jungle", CategoryCreature, registry.EntityTypeParrot},
		{"minecraft:badlands", CategoryMonster, registry.EntityTypeCreeper},
		{"minecraft:badlands", CategoryUndergroundWater, TypeGlowSquid},
		{"minecraft:taiga", CategoryUndergroundWater, TypeGlowSquid},
		{"minecraft:birch_forest", CategoryUndergroundWater, TypeGlowSquid},
		{"minecraft:dark_forest", CategoryUndergroundWater, TypeGlowSquid},
		{"minecraft:warm_ocean", CategoryWaterCreature, registry.EntityTypeSquid},
	} {
		if !has(c.cat.lists()[testBiome(c.biome)], c.mob) {
			t.Errorf("%s has no %s in category %d, want vanilla's entry", c.biome, c.mob, c.cat)
		}
	}
}

// recordingDice is Dice that logs every draw it hands out.
type recordingDice struct {
	Dice
	draws []float64
}

func (d *recordingDice) NextFloat() float32 {
	v := d.Dice.NextFloat()
	d.draws = append(d.draws, float64(v))
	return v
}

func (d *recordingDice) NextIntN(n int32) int32 {
	v := d.Dice.NextIntN(n)
	d.draws = append(d.draws, float64(v))
	return v
}

// scriptedDice answers each roll from its queue, then 0.99 for a float and 0
// for an int once the queue runs dry.
type scriptedDice struct {
	floats []float32
	ints   []int32
}

func (d *scriptedDice) NextFloat() float32 {
	if len(d.floats) == 0 {
		return 0.99
	}
	f := d.floats[0]
	d.floats = d.floats[1:]
	return f
}

func (d *scriptedDice) NextIntN(n int32) int32 {
	if len(d.ints) == 0 {
		return 0
	}
	v := d.ints[0]
	d.ints = d.ints[1:]
	return v % n
}

func (d *scriptedDice) NextDouble() float64 { return float64(d.NextFloat()) }

func (d *scriptedDice) NextBoolean() bool { return d.NextIntN(2) == 0 }

// striderRuleWorld changes only the rule a generated strider's jockey reads.
type striderRuleWorld struct {
	spawnTestWorld
	disabledRule world.GameRuleIndex
}

func (w striderRuleWorld) BoolRule(id world.GameRuleIndex) bool {
	return id != w.disabledRule && w.spawnTestWorld.BoolRule(id)
}
