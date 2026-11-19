package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The slime's own spawn rule, the tests of it driven against the dice the
// pass hands the checks. The ground of every arrangement is stone with air
// standing over it; the moon and the seed are set per test.

// worldCSeed is the seed of the fresh flat world the vanilla reference
// server ran for the slime-chunk census, whose twenty-one slime chunks
// inside the eight-chunk square the census took are the ground truth the
// dice test matches.
const worldCSeed int64 = -5646140197244274597

// worldCSlimeChunks are the chunk positions of that census whose slime-chunk
// dice roll their one-in-ten.
var worldCSlimeChunks = [][2]int32{
	{-7, -5}, {-7, -2}, {-7, 6}, {-7, 7}, {-6, -8}, {-5, -2}, {-4, -5},
	{-4, -1}, {-4, 6}, {-1, -7}, {1, -5}, {1, 3}, {2, -2}, {2, 2}, {2, 3},
	{3, 3}, {4, -8}, {4, -6}, {4, 8}, {5, 1}, {8, 3},
}

// spawnTestWorld is a testWorld carrying what the spawn pass reads beyond a
// mob's own: one biome everywhere, one surface height, the world's seed and
// the moon's face, and a place where every block read can be counted.
type spawnTestWorld struct {
	testWorld
	surface     int32
	biomeName   string
	blockLights map[[3]int32]uint8
	loaded      bool
	guarded     bool
	thunder     bool
	dimension   string
	seed        int64
	moon        int
	allStone    bool
	ceiling     bool
	stoneState  uint16
	asked       map[[3]int32]int
}

func newSpawnTestWorld() spawnTestWorld {
	return spawnTestWorld{
		testWorld:   newTestWorld(),
		surface:     -2,
		biomeName:   "minecraft:plains",
		blockLights: make(map[[3]int32]uint8),
		loaded:      true,
	}
}

// BlockAt answers the block the test set, or the all-stone the roll test
// asks for, and counts the read.
func (w spawnTestWorld) BlockAt(x, y, z int32) uint16 {
	if w.asked != nil {
		w.asked[[3]int32{x, y, z}]++
	}
	if w.allStone {
		return w.stoneState
	}
	return w.testWorld.BlockAt(x, y, z)
}

// BlocksIn reads the box through the counted BlockAt.
func (w spawnTestWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}

// BlockLightAt is the block light the test set at the cell, none elsewhere.
func (w spawnTestWorld) BlockLightAt(x, y, z int32) uint8 {
	return w.blockLights[[3]int32{x, y, z}]
}

// IsThundering reports the storm the test set.
func (w spawnTestWorld) IsThundering() bool { return w.thunder }

// Biome is the one biome the test world grows.
func (w spawnTestWorld) Biome(x, y, z int32) registry.Biome { return testBiome(w.biomeName) }
func (w spawnTestWorld) NoiseBiome(x, y, z int32) registry.Biome {
	return testBiome(w.biomeName)
}

// testBiome resolves a biome name, NoBiome for "".
func testBiome(name string) registry.Biome {
	b, _ := registry.BiomeByName(name)
	return b
}

// SurfaceHeight is the surface height the test set, everywhere the same.
func (w spawnTestWorld) SurfaceHeight(x, z int32) int32 { return w.surface }

// noLeavesStubTop is the top of the overworld columns the stub's no-leaves
// scan starts from: one below the build limit.
const noLeavesStubTop = 319

// NoLeavesHeight scans the stub's own blocks for the highest one the
// MOTION_BLOCKING_NO_LEAVES heightmap counts, the leaves and the pass-through
// shapes left out, so a test can roof a pasture and still read the grass.
func (w spawnTestWorld) NoLeavesHeight(x, z int32) int32 { return w.motionHeight(x, z, false) }

// MotionBlockingHeight is NoLeavesHeight with the leaves counted.
func (w spawnTestWorld) MotionBlockingHeight(x, z int32) int32 { return w.motionHeight(x, z, true) }

func (w spawnTestWorld) motionHeight(x, z int32, leaves bool) int32 {
	for y := int32(noLeavesStubTop); y >= w.MinY(); y-- {
		state := w.BlockAt(x, y, z)
		if state == 0 {
			continue
		}
		name, _, ok := registry.BlockStateName(state)
		if !ok || (!leaves && registry.BlockOf(state).IsLeaves()) {
			continue
		}
		if name == "minecraft:cobweb" || name == "minecraft:bamboo_sapling" {
			continue
		}
		if registry.IsSolid(state) || registry.FluidStates[state] {
			return y
		}
	}
	return w.MinY() - 1
}

// ChunkLoadedAt reports whether the test world's chunks are loaded, which
// every test's are unless it says otherwise.
func (w spawnTestWorld) ChunkLoadedAt(x, z int32) bool { return w.loaded }

// SpawnPoint is no spawn at all unless a test guards one, so the spawn
// point's own twenty-four blocks stand out of the way.
func (w spawnTestWorld) SpawnPoint() (int32, int32, int32, bool) {
	return 0, 0, 0, w.guarded
}

// SkyDarken is the darkening the test set, the same one the raw light runs
// on.
func (w spawnTestWorld) HasCeiling() bool { return w.ceiling }

func (w spawnTestWorld) SkyDarken() int { return w.darken }

// SeedValue is the world seed the test set.
func (w spawnTestWorld) SeedValue() int64 { return w.seed }

// MoonPhase is the moon's face the test set.
func (w spawnTestWorld) MoonPhase() int { return w.moon }

// SeaLevel is the overworld's sixty-three, the one dimension whose spawn
// lists this stub grows.
func (w spawnTestWorld) SeaLevel() int32 { return 63 }
func (w spawnTestWorld) DimensionName() string {
	if w.dimension != "" {
		return w.dimension
	}
	return "minecraft:overworld"
}

// ground places the stone a spawn position stands on at the y below it.
func (w spawnTestWorld) groundAt(t *testing.T, x, y, z int32) {
	t.Helper()
	w.set(x, y, z, defaultState(t, "minecraft:stone"))
}

// rngOpeningGate scans the seeds in order for one whose first Intn(10) rolls
// zero, the opening of the slime-chunk branch's world-side gate, and hands
// back fresh dice so seeded. The scan is the same every run.
func rngOpeningGate() *rand.Rand {
	for seed := int64(1); ; seed++ {
		r := rand.New(rand.NewSource(seed))
		if r.Intn(10) == 0 {
			return rand.New(rand.NewSource(seed))
		}
	}
}

// rngUnderHalf scans the seeds in order for one whose first Float32 rolls
// under a half and whose first Intn(32) rolls under four — the first the
// surface branch's chance die needs, the second a darkness die the surface
// branch does not spend.
func rngUnderHalf() *rand.Rand {
	for seed := int64(1); ; seed++ {
		f := rand.New(rand.NewSource(seed))
		if f.Float32() >= 0.5 {
			continue
		}
		i := rand.New(rand.NewSource(seed))
		if i.Intn(32) >= 4 {
			continue
		}
		return rand.New(rand.NewSource(seed))
	}
}

// atChunk is a block position inside the chunk named, off the chunk border.
func atChunk(cx, cz int32) (int32, int32) { return cx*16 + 3, cz*16 + 3 }

// slimeValidate runs the pass's own check for a slime at the position, with
// the distance to the nearest player fixed mid-band and no players standing.
func slimeValidate(w SpawnWorld, rng *rand.Rand, x, y, z int32) bool {
	return validSpawnPositionForType(w, rng, registry.EntityTypeSlime, x, y, z, 100, CategoryMonster)
}

// TestSlimeChunkDiceMatchVanillaWorldC rolls the slime-chunk dice of every
// chunk position the vanilla census took and demands the twenty-one slime
// chunks roll true and the rest do not.
func TestSlimeChunkDiceMatchVanillaWorldC(t *testing.T) {
	known := make(map[[2]int32]bool)
	for _, c := range worldCSlimeChunks {
		known[c] = true
	}
	for cx := int32(-8); cx <= 8; cx++ {
		for cz := int32(-8); cz <= 8; cz++ {
			got := seedSlimeChunk(cx, cz, worldCSeed).nextInt(10) == 0
			want := known[[2]int32{cx, cz}]
			if got != want {
				t.Errorf("chunk (%d,%d): slime-chunk dice say %v, the census says %v", cx, cz, got, want)
			}
		}
	}
}

// TestSlimeSpawnsInSlimeChunkWhateverTheLight stands a slime chunk's ground
// under a full night sky and demands the rule stand the slime up, then the
// same ground in a chunk whose dice say no and demands it does not.
func TestSlimeSpawnsInSlimeChunkWhateverTheLight(t *testing.T) {
	w := newSpawnTestWorld()
	w.seed = worldCSeed
	w.darken = 11
	w.sky = 15
	x, z := atChunk(-7, -5)
	w.groundAt(t, x, 29, z)
	if !slimeValidate(w, rngOpeningGate(), x, 30, z) {
		t.Fatal("the slime chunk's own dice opened and the rule refused the slime")
	}
	ox, oz := atChunk(0, 0)
	w.groundAt(t, ox, 29, oz)
	if slimeValidate(w, rngOpeningGate(), ox, 30, oz) {
		t.Fatal("a chunk the slime-chunk dice refuse stood a slime up")
	}
}

// TestSlimeNeedsUnderFortyInSlimeChunks walks the fortieth block: a slime
// chunk stands its slimes under it and nowhere at or above it.
func TestSlimeNeedsUnderFortyInSlimeChunks(t *testing.T) {
	w := newSpawnTestWorld()
	w.seed = worldCSeed
	w.darken = 11
	w.sky = 15
	x, z := atChunk(-7, -5)
	for _, y := range []int32{29, 39} {
		w.groundAt(t, x, y-1, z)
		if !slimeValidate(w, rngOpeningGate(), x, y, z) {
			t.Errorf("y=%d in a slime chunk: the rule refused", y)
		}
	}
	for _, y := range []int32{40, 45} {
		w.groundAt(t, x, y-1, z)
		if slimeValidate(w, rngOpeningGate(), x, y, z) {
			t.Errorf("y=%d in a slime chunk: the rule stood a slime up", y)
		}
	}
}

// TestSlimeSurfaceBranchByTheMoon walks the surface branch: a swamp at the
// heights between the fiftieth and the seventieth stands a slime up when the
// moon's chance die opens and the cell is dark enough for an eight-sided die
// to spare, and falls through to the slime-chunk branch — which the heights
// above the fortieth close for good — the moment any of that fails.
func TestSlimeSurfaceBranchByTheMoon(t *testing.T) {
	w := newSpawnTestWorld()
	w.seed = worldCSeed
	w.darken = 11
	w.sky = 4
	w.biomeName = "minecraft:swamp"
	x, z := atChunk(0, 0)

	// The branch stands a slime up under a full moon in a chunk whose own
	// dice say no: the surface branch never asks them.
	w.moon = 0
	w.groundAt(t, x, 59, z)
	if !slimeValidate(w, rngUnderHalf(), x, 60, z) {
		t.Fatal("a full-moon swamp in the surface band refused its slime")
	}

	// The band's edges are shut: the fiftieth itself and the seventieth
	// fall through to the chunk branch, which the height closes.
	for _, y := range []int32{50, 70} {
		w.groundAt(t, x, y-1, z)
		if slimeValidate(w, rngUnderHalf(), x, y, z) {
			t.Errorf("y=%d at the band's edge stood a surface slime up", y)
		}
	}

	// The new moon's chance is nothing, so the branch falls through and
	// the height closes the chunk branch behind it — in a slime chunk
	// too, at this height.
	w.moon = 4
	w.biomeName = "minecraft:swamp"
	sx, sz := atChunk(-7, -5)
	w.groundAt(t, sx, 59, sz)
	if slimeValidate(w, rngUnderHalf(), sx, 60, sz) {
		t.Fatal("a new-moon swamp stood a surface slime up")
	}

	// A bright cell — raw light fifteen against the eight-sided die —
	// falls through the same way.
	w.moon = 0
	w.darken = 0
	w.sky = 15
	if slimeValidate(w, rngUnderHalf(), x, 60, z) {
		t.Fatal("a daylight-bright swamp stood a surface slime up")
	}

	// The branch is the swamps' alone: a plains at the same height and
	// hour falls through to the closed chunk branch.
	w.biomeName = "minecraft:plains"
	w.darken = 11
	w.sky = 4
	if slimeValidate(w, rngUnderHalf(), x, 60, z) {
		t.Fatal("a plains stood a surface slime up")
	}
}

// TestSlimePeacefulRefused demands the peaceful refusal stand ahead of both
// branches of the slime's rule, as SpawnPlacements.checkSpawnRules runs it.
func TestSlimePeacefulRefused(t *testing.T) {
	w := newSpawnTestWorld()
	w.seed = worldCSeed
	w.darken = 11
	w.sky = 15
	w.diff = uint8(rules.DifficultyPeaceful)
	w.diffUp = true
	x, z := atChunk(-7, -5)
	w.groundAt(t, x, 29, z)
	if slimeValidate(w, rngOpeningGate(), x, 30, z) {
		t.Fatal("a peaceful world stood a slime up in a slime chunk")
	}
}

// Every queried block is a conductor, so each read records the initial roll.
// vanilla: NaturalSpawner.getRandomPosWithin
func TestSpawnRollStopsAtFirstAir(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for _, tc := range []struct {
		name    string
		surface int32
		top     int32
	}{
		{"our flat ground at sixty-three", 63, 64},
		{"highest block at sixty-four", 64, 65},
		{"vanilla flat ground at minus sixty-one", -61, -60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSpawnTestWorld()
			w.allStone = true
			w.stoneState = stone
			w.surface = tc.surface
			w.asked = make(map[[3]int32]int)
			seen := make(map[int32]bool)
			for seed := range int64(2000) {
				spawnCategoryForChunk(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)),
					ChunkPos{X: 0, Z: 0}, nil, nil, &SpawnPotential{}, &SpawnCounts{}, CategoryMonster, func(SpawnRequest) {})
			}
			for at := range w.asked {
				seen[at[1]] = true
			}
			top := int32(-1 << 30)
			for y := range seen {
				if y > top {
					top = y
				}
			}
			if top != tc.top {
				t.Errorf("the dice rolled as high as y=%d, want %d (read heights: %v)", top, tc.top, seen)
			}
		})
	}
}
