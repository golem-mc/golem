package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
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

// TestSpawnPassRefusesBodiesAlreadyThere runs the pass's own walk against a
// world where a small pack stands up, then seeds bodies at exactly those
// spots and runs the same throw again: no later body may stand inside one
// already there. vanilla: NaturalSpawner.spawnCategoryForPosition, whose
// addFreshEntityWithPassengers puts each body in the world before the next
// roll.
func TestSpawnPassRefusesBodiesAlreadyThere(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 0
	const y = 30
	bx, bz := atChunk(0, 0)
	for dx := int32(-40); dx <= 40; dx++ {
		for dz := int32(-40); dz <= 40; dz++ {
			w.groundAt(t, bx+dx, y-1, bz+dz)
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 100.5, Y: float64(y), Z: float64(bz) + 0.5}}

	for seed := int64(1); seed < 5000; seed++ {
		var bodies []SpawnBody
		var reqs []SpawnRequest
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, y, bx, bz,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryMonster, func(r SpawnRequest) { reqs = append(reqs, r) })
		if len(reqs) == 0 || len(reqs) >= spawnClusterSize {
			// Nothing to seed, or the cluster cap ended the throw before
			// the walk's own dice would have.
			continue
		}
		bodies = nil
		for _, r := range reqs {
			bodies = append(bodies, spawnBox(r.Type, r.X, r.Y, r.Z))
		}
		overlaps := 0
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, y, bx, bz,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryMonster, func(r SpawnRequest) {
				if overlapsBody(bodies, r) {
					overlaps++
				}
			})
		if overlaps != 0 {
			t.Errorf("seed %d stood up %d bodies inside the %d already standing", seed, overlaps, len(reqs))
		}
		return
	}
	t.Fatal("no seed stood up a pack smaller than the cluster size")
}

// TestSpawnPassSeesBodiesItJustStoodUp runs one pass from no bodies at all
// and demands that none of the requests it stands up lands inside an earlier
// request of the same pass: vanilla's addFreshEntityWithPassengers puts each
// body in the world before the next roll, so the pass obstructs itself. The
// emitted requests are compared with the test's own box test, independent of
// the production one, so a body the pass forgot cannot hide an overlap.
// vanilla: NaturalSpawner.spawnCategoryForPosition
func TestSpawnPassSeesBodiesItJustStoodUp(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 0
	const y = 30
	bx, bz := atChunk(0, 0)
	for dx := int32(-40); dx <= 40; dx++ {
		for dz := int32(-40); dz <= 40; dz++ {
			w.groundAt(t, bx+dx, y-1, bz+dz)
		}
	}
	players := []SpawnPlayer{{EntityID: 1, X: float64(bx) + 100.5, Y: float64(y), Z: float64(bz) + 0.5}}

	packs := 0
	for seed := int64(1); seed < 5000; seed++ {
		var bodies []SpawnBody
		var emitted []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, y, bx, bz,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryMonster, func(r SpawnRequest) {
				if overlapsBody(emitted, r) {
					t.Errorf("seed %d stood a body up inside an earlier body of the same pass", seed)
				}
				emitted = append(emitted, spawnRequestBox(r))
			})
		if len(emitted) > 1 {
			packs++
		}
	}
	if packs == 0 {
		t.Fatal("no seed stood up more than one body in a single pass")
	}
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

// TestOriginalMobsHerdDoesNotObstructItself scans seeds for a herd whose
// later member stands inside an earlier one: a WorldGenRegion's getEntities
// is always empty, so isUnobstructed never sees the herd.
// vanilla: WorldGenRegion.getEntities, Mob.checkSpawnObstruction
func TestOriginalMobsHerdDoesNotObstructItself(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 15
	grass := defaultState(t, "minecraft:grass_block")
	for x := range int32(16) {
		for z := range int32(16) {
			w.set(x, 29, z, grass)
		}
	}
	w.surface = 30

	// Few seeds open the gate on a known kind: the first overlap is at 4103.
	for seed := int64(1); seed < 20000; seed++ {
		var bodies []SpawnBody
		overlap := false
		spawnOriginalMobsAt(w, seed, func(r SpawnRequest) {
			overlap = overlap || overlapsBody(bodies, r)
			bodies = append(bodies, spawnBox(r.Type, r.X, r.Y, r.Z))
		})
		if overlap {
			return
		}
	}
	t.Fatal("no seed stood a herd member inside an earlier one")
}

// TestOriginalMobsFinalizeTheHerd: the level seed is the chunk's dice at chunk
// 0,0, and each mob takes an ID from nextID and its request's spot and yaw.
// Finalizing rolls the herd's dice, so only the first mob is compared with a
// bare request stream.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration, Entity.snapTo
func TestOriginalMobsFinalizeTheHerd(t *testing.T) {
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
	next := int32(40)
	mobs := OriginalMobs(w, seedOpeningAHerd(), ChunkPos{}, func() int32 { next++; return next },
		func(float64, float64) rules.LocalDifficulty { return rules.LocalDifficulty{} })
	if len(mobs) != 4 {
		t.Fatalf("%d mobs, want the herd of four", len(mobs))
	}
	first, r := mobs[0], reqs[0]
	if first.Type != r.Type || first.X != r.X || first.Y != r.Y || first.Z != r.Z || first.Yaw != r.Yaw {
		t.Errorf("the first mob is %s at %.2f %.2f %.2f yaw %.2f, want %s at %.2f %.2f %.2f yaw %.2f",
			first.Type.Name(), first.X, first.Y, first.Z, first.Yaw, r.Type.Name(), r.X, r.Y, r.Z, r.Yaw)
	}
	for i, m := range mobs {
		if m.ID != int32(41+i) {
			t.Errorf("mob %d has ID %d, want %d", i, m.ID, 41+i)
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

// TestOriginalMobsStandInsideTheirColumnsVegetation pins the stand the walk
// finds: the first free y over the grass block whichever top the column
// carries, so a bare grass block and one under short grass stand their herds
// at one and the same y — neither inside the grass block's own cell.
func TestOriginalMobsStandInsideTheirColumnsVegetation(t *testing.T) {
	w := pastureStub{spawnTestWorld: newSpawnTestWorld(), grown: make(map[[2]int32]bool)}
	w.sky = 15
	grass := defaultState(t, "minecraft:grass_block")
	shortGrass := defaultState(t, "minecraft:short_grass")
	for x := range int32(16) {
		for z := range int32(16) {
			w.set(x, 29, z, grass)
			if x%2 == 0 {
				w.set(x, 30, z, shortGrass)
				w.grown[[2]int32{x, z}] = true
			}
		}
	}
	w.surface = 29

	var stood int
	spawnOriginalMobsAt(w, seedOpeningAHerd(), func(r SpawnRequest) {
		stood++
		if r.Y != 30 {
			t.Errorf("a %s stands at y %.0f, want the one free cell over either column's grass", r.Type, r.Y)
		}
	})
	if stood == 0 {
		t.Fatal("the pasture stub grew no herd")
	}
}

// TestOriginalMobsGrowNothingWhereTheBiomeNamesNoAnimal holds the pass to
// the biomes no simulatable creature comes from: the gate opens, the rolls
// spend themselves, nothing stands up.
func TestOriginalMobsGrowNothingWhereTheBiomeNamesNoAnimal(t *testing.T) {
	for _, biome := range []string{"minecraft:ocean", "minecraft:beach", "minecraft:snowy_plains"} {
		w := newSpawnTestWorld()
		w.sky = 15
		w.biomeName = biome
		var stood int
		spawnOriginalMobsAt(w, seedOpeningTheGate(), func(SpawnRequest) { stood++ })
		if stood != 0 {
			t.Errorf("a %s chunk stood up %d animals, want the none its list names", biome, stood)
		}
	}
}

// TestOriginalMobsStandUnderTheCanopy holds the herd to the ground under a
// leaf canopy: the stand reads the MOTION_BLOCKING_NO_LEAVES heightmap, so a
// column roofed by oak leaves stands its animals on the grass below, not on
// top of the leaves.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration,
// SpawnPlacements.getHeightmapType
func TestOriginalMobsStandUnderTheCanopy(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 15
	grass := defaultState(t, "minecraft:grass_block")
	leaves := defaultState(t, "minecraft:oak_leaves")
	for x := range int32(16) {
		for z := range int32(16) {
			w.set(x, 29, z, grass)
			w.set(x, 34, z, leaves)
		}
	}
	w.surface = 34

	var reqs []SpawnRequest
	spawnOriginalMobsAt(w, seedOpeningAHerd(), func(r SpawnRequest) { reqs = append(reqs, r) })
	if len(reqs) == 0 {
		t.Fatal("the canopy pasture grew no herd, want the animals the grass under it holds")
	}
	for _, r := range reqs {
		if r.Y != 30 {
			t.Errorf("a %s stands at y %.0f, want the grass under the canopy at y 30", r.Type, r.Y)
		}
	}
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

// TestValidPositionRefusesABrightMonster holds Monster.getWalkTargetValue in
// the walk-target check: a zombified piglin, whose spawn rule reads no light,
// is refused in lava light and taken in the dark; an enderman's value is 0.
// vanilla: PathfinderMob.checkSpawnRules, Monster.getWalkTargetValue,
// Enderman.getWalkTargetValue
func TestValidPositionRefusesABrightMonster(t *testing.T) {
	at := func(kind MobType, light uint8) bool {
		w := newSpawnTestWorld()
		w.dimension = world.NetherName
		w.sky = 0
		w.block = light
		w.set(0, 29, 0, defaultState(t, "minecraft:netherrack"))
		return validPositionAt(w, kind, 0.5, 30, 0.5, 0, nil)
	}
	if at(TypeZombifiedPiglin, 15) {
		t.Error("a zombified piglin at block light 15 passed, want it refused")
	}
	if !at(TypeZombifiedPiglin, 0) {
		t.Error("a zombified piglin at block light 0 was refused, want it passed")
	}
	if !at(TypeEnderman, 15) {
		t.Error("an enderman at block light 15 was refused, want its walk-target value 0")
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

// TestOriginalParrotsStandOnTheCanopy: a parrot reads the MOTION_BLOCKING
// heightmap, so it stands on top of the leaves, not on the grass under them.
// vanilla: SpawnPlacements.getHeightmapType, NaturalSpawner.getTopNonCollidingPos
func TestOriginalParrotsStandOnTheCanopy(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 15
	w.biomeName = "minecraft:jungle"
	grass := defaultState(t, "minecraft:grass_block")
	leaves := defaultState(t, "minecraft:jungle_leaves")
	for x := range int32(16) {
		for z := range int32(16) {
			w.set(x, 29, z, grass)
			w.set(x, 34, z, leaves)
		}
	}
	w.surface = 34

	var reqs []SpawnRequest
	spawnOriginalMobsAt(w, seedOpeningAParrotHerd(), func(r SpawnRequest) { reqs = append(reqs, r) })
	if len(reqs) == 0 {
		t.Fatal("the canopy grew no parrots, want them on top of the leaves")
	}
	for _, r := range reqs {
		if r.Type != TypeParrot || r.Y != 35 {
			t.Errorf("a %s stands at y %.0f, want a parrot on the leaves at y 35", r.Type, r.Y)
		}
	}
}

// TestOriginalMobsStandOnTheBlockCorner pins the horizontal stand: vanilla
// clamps the walked block coordinate to the chunk, so an animal stands on the
// block corner, not the block centre the tick's own pass would put it at.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration
func TestOriginalMobsStandOnTheBlockCorner(t *testing.T) {
	w := newSpawnTestWorld()
	w.sky = 15
	grass := defaultState(t, "minecraft:grass_block")
	// The pasture stays clear of the chunk edge, where the width clamp and
	// not the corner decides the stand.
	for x := int32(3); x < 13; x++ {
		for z := int32(3); z < 13; z++ {
			w.set(x, 29, z, grass)
		}
	}
	w.surface = 29

	var stood int
	spawnOriginalMobsAt(w, seedOpeningAHerd(), func(r SpawnRequest) {
		stood++
		if r.X != math.Floor(r.X) || r.Z != math.Floor(r.Z) {
			t.Errorf("a %s stands at (%.1f, %.1f), want the block corner", r.Type, r.X, r.Z)
		}
	})
	if stood == 0 {
		t.Fatal("the interior pasture grew no herd")
	}
}

// TestOriginalMobsRetryARefusedColumn: a column whose ground passes but whose
// box collides spends the attempt without walking, so every later attempt of
// the herd retries it. A stone in the first stand's box, beside its column,
// leaves the whole herd unplaced.
// vanilla: the continue in NaturalSpawner.spawnMobsForChunkGeneration
func TestOriginalMobsRetryARefusedColumn(t *testing.T) {
	pasture := func() spawnTestWorld {
		w := newSpawnTestWorld()
		w.sky = 15
		grass := defaultState(t, "minecraft:grass_block")
		for x := range int32(16) {
			for z := range int32(16) {
				w.set(x, 29, z, grass)
			}
		}
		w.surface = 30
		return w
	}
	var first *SpawnRequest
	spawnOriginalMobsAt(pasture(), seedOpeningAHerd(), func(r SpawnRequest) {
		if first == nil {
			first = &r
		}
	})
	if first == nil {
		t.Fatal("the open pasture grew no herd")
	}
	// A cell of the box beside the stand column on both axes: a half-width
	// either way reaches the next column.
	beside := func(v float64) int32 {
		col := int32(math.Floor(v))
		if lo := int32(math.Floor(v - 0.4)); lo != col {
			return lo
		}
		return int32(math.Floor(v + 0.4))
	}
	w := pasture()
	w.set(beside(first.X), 30, beside(first.Z), defaultState(t, "minecraft:stone"))
	stood := 0
	spawnOriginalMobsAt(w, seedOpeningAHerd(), func(SpawnRequest) { stood++ })
	if stood != 0 {
		t.Errorf("the herd stood %d animals, want none: each attempt retries the refused column", stood)
	}
}

// TestBiomeCreatureProbabilitiesCoverTheSnowyBiomes pins the 0.07 both snowy
// biomes carry, the override every other biome's 0.1 default stands beside.
// vanilla: BiomeData, OverworldBiomes.plains
func TestBiomeCreatureProbabilitiesCoverTheSnowyBiomes(t *testing.T) {
	for _, biome := range []registry.Biome{registry.BiomeSnowyPlains, registry.BiomeIceSpikes} {
		if got := biomeCreatureProbabilities[biome]; got != 0.07 {
			t.Errorf("the %s creature probability is %v, want the 0.07 its data file carries", biome.Name(), got)
		}
	}
}

// TestBadlandsGateIsClosedWhereThePlainsGateOpens holds that a first spawn
// float in [0.03, 0.1) opens the plains gate but not the badlands one.
// vanilla: OverworldBiomes.badlands, BiomeData
func TestBadlandsGateIsClosedWhereThePlainsGateOpens(t *testing.T) {
	pasture := func(biome string) spawnTestWorld {
		w := newSpawnTestWorld()
		w.sky = 15
		w.biomeName = biome
		grass := defaultState(t, "minecraft:grass_block")
		for x := range int32(16) {
			for z := range int32(16) {
				w.set(x, 29, z, grass)
			}
		}
		w.surface = 30
		return w
	}
	stand := func(biome string, seed int64) int {
		var stood int
		spawnOriginalMobsAt(pasture(biome), seed, func(SpawnRequest) { stood++ })
		return stood
	}
	for seed := int64(1); seed < 10000; seed++ {
		if f, _ := originalMobDice(seed, ChunkPos{}, false); f.NextFloat() < 0.03 {
			continue
		}
		if f, _ := originalMobDice(seed, ChunkPos{}, false); f.NextFloat() >= creatureSpawnProbability {
			continue
		}
		if stand("minecraft:plains", seed) == 0 {
			continue
		}
		for _, biome := range []string{"minecraft:badlands", "minecraft:eroded_badlands"} {
			if got := stand(biome, seed); got != 0 {
				t.Errorf("a %s chunk stood up %d animals, want none under its 0.03 gate", biome, got)
			}
		}
		return
	}
	t.Fatal("no seed opened the plains gate with a first float in [0.03, 0.1)")
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

func TestEmptySpawnBlockRejectsHazards(t *testing.T) {
	for _, tc := range []struct {
		block registry.Block
		empty bool
	}{
		{registry.BlockAir, true}, {registry.BlockCaveAir, true}, {registry.BlockShortGrass, true},
		{registry.BlockStone, false}, {registry.BlockRail, false}, {registry.BlockFire, false},
		{registry.BlockSoulFire, false}, {registry.BlockMagmaBlock, false},
		{registry.BlockWater, false}, {registry.BlockLava, false},
	} {
		if got := emptySpawnBlock(TypeZombie, tc.block.DefaultState()); got != tc.empty {
			t.Errorf("%s: empty = %v, want %v", tc.block.Name(), got, tc.empty)
		}
	}
	if !emptySpawnBlock(TypeZombie, 65535) {
		t.Error("invalid state no longer reads as empty")
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

// TestOriginalMobsDrawTheSpawnDiceInVanillaOrder pins, for level seed 0, the
// spawn stream's opening draws over a pasture against java.util.Random on the
// chunk's decoration seed, deco(0, x, z) as in levelgen's
// TestLegacyRandomDecorationSeed. At chunk 0,0 deco is 0 and the gate shuts
// on the first float; at chunk -2,-3 it opens, the plains list (total weight
// 46) picks the sheep's fixed four, and the first member's yaw follows x/z;
// the gate opens again for later herds.
// vanilla: NoiseBasedChunkGenerator.spawnOriginalMobs,
// NaturalSpawner.spawnMobsForChunkGeneration
func TestOriginalMobsDrawTheSpawnDiceInVanillaOrder(t *testing.T) {
	for _, c := range []struct {
		pos   ChunkPos
		want  []float64
		first SpawnRequest
	}{
		// new Random(0).nextFloat()
		{ChunkPos{}, []float64{0.73096776}, SpawnRequest{}},
		// q = new Random(1167361632575206672L): q.nextFloat(), q.nextInt(46),
		// q.nextInt(16), q.nextInt(16), q.nextFloat()
		{ChunkPos{X: -2, Z: -3}, []float64{0.0059737563, 7, 15, 1, 0.96234995},
			SpawnRequest{Type: registry.EntityTypeSheep, X: -17, Y: 30, Z: -47, Yaw: 346.44598}},
	} {
		w := newSpawnTestWorld()
		w.sky = 15
		grass := defaultState(t, "minecraft:grass_block")
		for x := range int32(16) {
			for z := range int32(16) {
				w.set(c.pos.X*16+x, 29, c.pos.Z*16+z, grass)
			}
		}
		w.surface = 30

		spawn, region := originalMobDice(0, c.pos, false)
		rec := &recordingDice{Dice: spawn}
		var reqs []SpawnRequest
		SpawnOriginalMobs(w, rec, region, c.pos, func(r SpawnRequest) { reqs = append(reqs, r) })
		if len(rec.draws) < len(c.want) {
			t.Fatalf("chunk %v: %d draws %v, want at least %v", c.pos, len(rec.draws), rec.draws, c.want)
		}
		for i, want := range c.want {
			if float32(rec.draws[i]) != float32(want) {
				t.Errorf("chunk %v: draw %d is %v, want java.util.Random's %v", c.pos, i, rec.draws[i], want)
			}
		}
		if c.first.Type == 0 {
			if len(reqs) != 0 || len(rec.draws) != len(c.want) {
				t.Errorf("chunk %v: %d animals over %d draws, want the shut gate's none over one", c.pos, len(reqs), len(rec.draws))
			}
			continue
		}
		if len(reqs) < 4 {
			t.Fatalf("chunk %v: %d animals, want the first herd's four sheep", c.pos, len(reqs))
		}
		for _, r := range reqs[:4] {
			if r.Type != c.first.Type {
				t.Errorf("chunk %v: the first herd stands a %s, want four %s", c.pos, r.Type.Name(), c.first.Type.Name())
			}
		}
		r := reqs[0]
		if r.Type != c.first.Type || r.X != c.first.X || r.Y != c.first.Y || r.Z != c.first.Z || r.Yaw != c.first.Yaw {
			t.Errorf("chunk %v: first is %s at %v %v %v yaw %v, want %s at %v %v %v yaw %v", c.pos,
				r.Type.Name(), r.X, r.Y, r.Z, r.Yaw, c.first.Type.Name(), c.first.X, c.first.Y, c.first.Z, c.first.Yaw)
		}
		if r.Region != region {
			t.Errorf("chunk %v: the request carries region %v, want the chunk's region random", c.pos, r.Region)
		}
	}
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

// TestOriginalMobsCheckWidePlacementAtTheRolledColumn: a horse rolled at
// x = xo stands clamped at xo+1.3965, but its placement is checked at the
// rolled column xo; a rail (no collision, but no spawning inside) decides
// which column was read.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration
func TestOriginalMobsCheckWidePlacementAtTheRolledColumn(t *testing.T) {
	list := CategoryCreature.lists()[registry.BiomePlains]
	pick := int32(-1)
	sum := int32(0)
	for _, e := range list {
		if e.mob == TypeHorse {
			pick = sum
			break
		}
		sum += int32(e.weight)
	}
	if pick < 0 {
		t.Fatal("plains lists no horse")
	}
	wantX, _ := clampedStand(TypeHorse, 0, 8, ChunkPos{})
	for _, c := range []struct {
		railX int32
		want  bool
	}{{railX: 1, want: true}, {railX: 0, want: false}} {
		w := newSpawnTestWorld()
		w.sky = 15
		floor(w.testWorld, 0, 0, 15, 15, 63, defaultState(t, "minecraft:grass_block"))
		w.set(c.railX, 64, 8, defaultState(t, "minecraft:rail"))
		// The gate opens once; the pick lands on the horse, the count on two,
		// the herd's column on x 0, z 8, and every walk step on zero.
		spawn := &scriptedDice{floats: []float32{0}, ints: []int32{pick, 0, 0, 8}}
		var reqs []SpawnRequest
		SpawnOriginalMobs(w, spawn, GoDice{rand.New(rand.NewSource(1))}, ChunkPos{},
			func(r SpawnRequest) { reqs = append(reqs, r) })
		if got := len(reqs) > 0; got != c.want {
			t.Fatalf("rail at x %d: a horse stood = %v, want %v", c.railX, got, c.want)
		}
		if c.want && (reqs[0].Type != TypeHorse || reqs[0].X != wantX || reqs[0].Z != 8) {
			t.Errorf("rail at x %d: %s at %v %v, want a horse at %v 8", c.railX, reqs[0].Type.Name(), reqs[0].X, reqs[0].Z, wantX)
		}
	}
}

func TestSpawnCategoryOfAgreesWithKinds(t *testing.T) {
	// The predicates in SpawnCategoryOf's precedence order.
	preds := []struct {
		cat  SpawnCategory
		kind func(MobType) bool
	}{
		{CategoryMonster, MonsterKind},
		{CategoryCreature, CreatureKind},
		{CategoryAxolotls, AxolotlKind},
		{CategoryUndergroundWater, UndergroundWaterKind},
		{CategoryWaterCreature, WaterCreatureKind},
		{CategoryWaterAmbient, WaterAmbientKind},
		{CategoryAmbient, AmbientKind},
	}
	for ty := range MobType(registry.EntityTypeCount) {
		want, wantOK := SpawnCategory(0), false
		for _, p := range preds {
			if p.kind(ty) {
				want, wantOK = p.cat, true
				break
			}
		}
		if got, ok := SpawnCategoryOf(ty); got != want || ok != wantOK {
			t.Errorf("SpawnCategoryOf(%v) = %v, %v; want %v, %v", ty, got, ok, want, wantOK)
		}
	}
}

// vanilla's recorded first-generation striders (tools/worldgen-parity
// -record-entities, seed 777): the ceiling column search and lava placement.
func TestOriginalMobsGrowNetherStriders(t *testing.T) {
	g := world.NewNetherGenerator(777)
	w := world.NewDimensionWorld(t.TempDir(), true, g)
	defer w.Close()
	type pos [3]float64
	cases := []struct {
		chunk ChunkPos
		want  []pos
	}{
		{ChunkPos{X: 11, Z: -16}, []pos{{179, 31, -241}, {180, 31, -246}, {180, 31, -249}}},
		{ChunkPos{X: 10, Z: -15}, []pos{{166, 31, -238}}},
		{ChunkPos{X: 12, Z: -15}, []pos{{201, 31, -235}, {201, 31, -238}}},
		{ChunkPos{X: 9, Z: -16}, nil},
	}
	for _, c := range cases {
		wp := world.ChunkPos{X: c.chunk.X, Z: c.chunk.Z}
		w.Acquire(wp)
		var next int32
		mobs := OriginalMobs(w, 777, c.chunk, func() int32 { next++; return next },
			func(x, z float64) rules.LocalDifficulty { return rules.LocalDifficulty{} })
		w.Release(wp)
		got := map[pos]bool{}
		for _, m := range mobs {
			if m.Type != TypeStrider {
				t.Errorf("chunk %v grew a %v, want striders only", c.chunk, m.Type)
			}
			got[pos{m.X, m.Y, m.Z}] = true
		}
		if len(got) != len(c.want) {
			t.Errorf("chunk %v grew %v, want %v", c.chunk, got, c.want)
			continue
		}
		for _, p := range c.want {
			if !got[p] {
				t.Errorf("chunk %v grew %v, want %v", c.chunk, got, c.want)
				break
			}
		}
	}
}

func TestSpawnPotentialEnergyChange(t *testing.T) {
	var p SpawnPotential
	p.Add(0, 0, 0, 0)
	if len(p) != 0 {
		t.Fatal("a zero charge was stored")
	}
	p.Add(0, 0, 0, 1)
	p.Add(3, 0, 4, 0.5)
	if got := p.EnergyChange(0, 0, 0, 0); got != 0 {
		t.Fatalf("zero charge change = %v", got)
	}
	if got := p.EnergyChange(0, 0, 0, 1); !math.IsInf(got, 1) {
		t.Fatalf("change at a stored block = %v, want +Inf", got)
	}
	// Distances from (6,0,8): 10 to the origin, 5 to (3,0,4).
	want := 2 * (1.0/10 + 0.5/5)
	if got := p.EnergyChange(6, 0, 8, 2); math.Abs(got-want) > 1e-12 {
		t.Fatalf("change = %v, want %v", got, want)
	}
}

func TestFoxAndPandaSpawnWithoutPlacementCheck(t *testing.T) {
	t.Parallel()
	w := newSpawnTestWorld()
	w.set(0, 62, 0, registry.BlockGrassBlock.DefaultState())
	w.set(0, 63, 0, registry.BlockSpruceLeaves.DefaultState())
	for _, typ := range []MobType{TypeFox, TypePanda} {
		if !originalSpawnPositionOk(w, typ, 0, 63, 0) {
			t.Errorf("%s refused a leaf stand cell", typ.Name())
		}
	}
	if originalSpawnPositionOk(w, TypeCow, 0, 63, 0) {
		t.Error("cow accepted a leaf stand cell")
	}
}

// striderRuleWorld changes only the rule a generated strider's jockey reads.
type striderRuleWorld struct {
	spawnTestWorld
	disabledRule world.GameRuleIndex
}

func (w striderRuleWorld) BoolRule(id world.GameRuleIndex) bool {
	return id != w.disabledRule && w.spawnTestWorld.BoolRule(id)
}

// TestOriginalStriderJockeysRespectMonsterSpawningRules covers the
// WorldGenRegion finalize path, which does not pass through the arrival spawner.
// vanilla: Strider.finalizeSpawn, ServerLevel.isSpawningMonsters
func TestOriginalStriderJockeysRespectMonsterSpawningRules(t *testing.T) {
	w := striderRuleWorld{spawnTestWorld: newSpawnTestWorld()}
	w.ceiling, w.biomeName = true, "minecraft:nether_wastes"
	floor(w.testWorld, -1, -1, 16, 16, 40, defaultState(t, "minecraft:stone"))
	floor(w.testWorld, -1, -1, 16, 16, 31, defaultState(t, "minecraft:lava"))
	for _, rule := range []world.GameRuleIndex{world.RuleUnknown, world.RuleSpawnMobs, world.RuleSpawnMonsters} {
		w.disabledRule = rule
		piglins, babies := 0, 0
		var id int32
		for seed := int64(1); seed <= 600; seed++ {
			mobs := OriginalMobs(w, seed*341873128712, ChunkPos{}, func() int32 { id++; return id },
				func(float64, float64) rules.LocalDifficulty { return rules.LocalDifficulty{} })
			for _, m := range mobs {
				if r := m.Rider; r != nil {
					switch r.Type {
					case TypeZombifiedPiglin:
						piglins++
					case TypeStrider:
						if !r.IsBaby() {
							t.Fatal("a grown strider jockey spawned")
						}
						babies++
					}
				}
			}
		}
		if babies == 0 || (rule == world.RuleUnknown && piglins == 0) || (rule != world.RuleUnknown && piglins != 0) {
			t.Errorf("disabled rule %q: %d piglin and %d baby jockeys", rule, piglins, babies)
		}
	}
}
