package world

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// newRainWorld is a flat world of the given biome with the rain at full
// strength, its columns loaded so the biome, sky light and heightmap reads
// have something to answer from.
func newRainWorld(t *testing.T, biome string) *World {
	t.Helper()
	w := newTestWorld(t, newFlatGenerator(biome, []flatLayer{{state: stoneState, height: GroundLevel - worldMinY}}))
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	setRain(w)
	return w
}

// TestRainingAtFollowsTheBiomePrecipitation checks the biome's word on the
// rain: the dry biomes stay dry through the rain, a cold one drops snow
// rather than rain, and a temperate one takes the rain.
func TestRainingAtFollowsTheBiomePrecipitation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		biome string
		want  bool
	}{
		{"minecraft:plains", true},
		{"minecraft:desert", false},
		{"minecraft:savanna", false},
		{"minecraft:badlands", false},
		{"minecraft:snowy_plains", false},
	} {
		w := newRainWorld(t, tc.biome)
		if got := w.RainingAt(0, GroundLevel, 0); got != tc.want {
			t.Errorf("raining in the %s = %v, want %v", tc.biome, got, tc.want)
		}
	}
}

// TestRainingAtStopsAtTheMotionBlockingHeightmap checks the heightmap half:
// a glass roof lets the sky light through at full strength yet still stops
// the rain, and a leaf canopy the light dims stops it too.
func TestRainingAtStopsAtTheMotionBlockingHeightmap(t *testing.T) {
	t.Parallel()
	glass := registry.MustState("minecraft:glass", nil)
	leaves := registry.MustState("minecraft:oak_leaves", nil)

	w := newRainWorld(t, "minecraft:plains")
	w.SetBlock(0, GroundLevel+2, 0, glass)
	w.Relight(0, 0)
	if sky := w.SkyLightAt(0, GroundLevel, 0); sky != 15 {
		t.Fatalf("the sky light under glass = %d, want 15", sky)
	}
	if w.RainingAt(0, GroundLevel, 0) {
		t.Error("the rain fell through a glass roof")
	}

	w.SetBlock(0, GroundLevel+2, 0, leaves)
	w.Relight(0, 0)
	if w.RainingAt(0, GroundLevel, 0) {
		t.Error("the rain fell through a leaf canopy")
	}
}

// TestRainDoesNotDouseFireInADesert checks the reader the burn-out asks: the
// same rain and dice leave a fire burning in a desert while it douses the
// same fire in the plains.
func TestRainDoesNotDouseFireInADesert(t *testing.T) {
	t.Parallel()
	const seed = 2
	desert, desertPhysics := newRainFireWorld(t, "minecraft:desert", seed)
	plains, plainsPhysics := newRainFireWorld(t, "minecraft:plains", seed)

	setBlock(t, desert, desertPhysics, 0, GroundLevel, 0, blocklogic.Fire)
	setBlock(t, plains, plainsPhysics, 0, GroundLevel, 0, blocklogic.Fire)

	for clock := int64(1); clock <= 100; clock++ {
		desertPhysics.Tick(clock)
		plainsPhysics.Tick(clock)
	}
	if name, _, _ := registry.BlockStateName(desert.BlockAt(0, GroundLevel, 0)); name != "minecraft:fire" {
		t.Errorf("the desert fire after the rain = %s, want fire", blockLabel(desert.BlockAt(0, GroundLevel, 0)))
	}
	if got := plains.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("the plains fire after the rain = %s, want air", blockLabel(got))
	}
}

// newRainFireWorld is a physics world of the given biome, its dice pinned to
// the seed given, with the rain at full strength.
func newRainFireWorld(t *testing.T, biome string, seed int64) (*World, *Physics) {
	t.Helper()
	w := newRainWorld(t, biome)
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(seed))
	w.SetDifficulty(DifficultyHard)
	return w, p
}

// TestRainingAtSnowsAboveTheSnowLine checks the height correction: a meadow is
// warm enough to rain at its ground but too cold to by the top of the world.
func TestRainingAtSnowsAboveTheSnowLine(t *testing.T) {
	t.Parallel()
	w := newRainWorld(t, "minecraft:meadow")
	if got := w.precipitationAt(0, GroundLevel, 0); got != precipRain {
		t.Errorf("the meadow at ground level drops %v, want rain", got)
	}
	if got := w.precipitationAt(0, 300, 0); got != precipSnow {
		t.Errorf("the meadow high above the snow line drops %v, want snow", got)
	}
}

// TestRainingAtIsDryAtTheHeightmapTop checks the heightmap boundary: a bottom
// slab leaves the sky light at full strength in its own cell yet is the top
// motion-blocking block, so the cell at that y is dry and only the one above
// it takes the rain.
func TestRainingAtIsDryAtTheHeightmapTop(t *testing.T) {
	t.Parallel()
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "bottom"})

	w := newRainWorld(t, "minecraft:plains")
	w.SetBlock(0, GroundLevel+2, 0, slab)
	w.Relight(0, 0)
	if sky := w.SkyLightAt(0, GroundLevel+2, 0); sky != 15 {
		t.Fatalf("the sky light at the slab = %d, want 15", sky)
	}
	if w.RainingAt(0, GroundLevel+2, 0) {
		t.Error("the rain fell at the top motion-blocking block's own y")
	}
	if !w.RainingAt(0, GroundLevel+3, 0) {
		t.Error("the rain did not fall just above the heightmap top")
	}
}

// TestRainingAtUsesTheFrozenTemperatureModifier checks the frozen oceans'
// temperature_modifier through the rain: its cold patches read 0.2, warm
// enough to rain, while the ocean outside them stays below freezing. The
// ground sits below the snow line, so the modifier's temperature stands
// unadjusted.
func TestRainingAtUsesTheFrozenTemperatureModifier(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, newFlatGenerator("minecraft:frozen_ocean", []flatLayer{{state: stoneState, height: 1}}))
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	setRain(w)
	if !w.RainingAt(0, worldMinY+1, 0) {
		t.Error("the frozen ocean patch at 0,0 = snow, want rain")
	}
	if w.RainingAt(16, worldMinY+1, 16) {
		t.Error("the frozen ocean outside a patch = rain, want snow")
	}
}

// TestBiomeTemperatureMatchesTheJar pins the noise port against rows read from
// the vanilla jar, both the frozen modifiers' noise and the height correction's,
// as raw float32 bits so a rounding change cannot slip through.
func TestBiomeTemperatureMatchesTheJar(t *testing.T) {
	t.Parallel()
	w := newRainWorld(t, "minecraft:frozen_ocean")
	frozen := registry.BiomeFrozenOcean.Climate()
	plains := registry.BiomePlains.Climate()
	for _, tc := range []struct {
		climate registry.BiomeClimate
		x, y, z int32
		want    uint32
	}{
		{frozen, 0, 64, 0, 1031798784},
		{frozen, 16, 64, 16, 3188175078},
		{frozen, -7, 64, 13, 1030693832},
		{frozen, 100, 200, -250, 3197771331},
		{frozen, 37, 80, 91, 1028136216},
		{frozen, 512, -46, 512, 0},
		{frozen, -4096, 120, 2048, 3158663776},
		// the frozen noise alone stands above the 0.3 patch threshold at
		// this cell; the biome-info edge term is what brings it under.
		{frozen, 16, -60, -15, 1045220557},
		{plains, 0, 64, 0, 1059690906},
		{plains, 100, 200, -250, 1056930647},
		{plains, 8, 300, 8, 1052818224},
	} {
		if got := math.Float32bits(w.temperatureAt(tc.x, tc.y, tc.z, tc.climate)); got != tc.want {
			t.Errorf("temperature at %d,%d,%d = %d, want the jar's %d", tc.x, tc.y, tc.z, got, tc.want)
		}
	}
}

// TestMotionBlockingCountsWaterloggedFluids checks the heightmap reads a
// block's own fluid: a waterlogged ladder blocks motion, its dry twin does
// not.
func TestMotionBlockingCountsWaterloggedFluids(t *testing.T) {
	t.Parallel()
	dry := registry.MustState("minecraft:ladder", map[string]string{"waterlogged": "false"})
	wet := registry.MustState("minecraft:ladder", map[string]string{"waterlogged": "true"})
	if blocksMotionInHeightmap(dry) {
		t.Error("a dry ladder blocks motion")
	}
	if !blocksMotionInHeightmap(wet) {
		t.Error("a waterlogged ladder does not block motion")
	}
}

// TestMotionHeightTracksTheWrittenColumn checks a write refreshes only its own
// cached column: a block raised in one column lifts that column's motion
// height and leaves its neighbour's where it was.
func TestMotionHeightTracksTheWrittenColumn(t *testing.T) {
	t.Parallel()
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "bottom"})

	w := newRainWorld(t, "minecraft:plains")
	if h := w.motionBlockingHeight(0, 0); h != GroundLevel-1 {
		t.Fatalf("the flat column's motion height = %d, want %d", h, GroundLevel-1)
	}
	w.motionBlockingHeight(1, 0)
	w.SetBlock(0, GroundLevel+4, 0, slab)
	if h := w.motionBlockingHeight(0, 0); h != GroundLevel+4 {
		t.Errorf("the written column's motion height = %d, want %d", h, GroundLevel+4)
	}
	if h := w.motionBlockingHeight(1, 0); h != GroundLevel-1 {
		t.Errorf("the neighbour column's motion height = %d, want %d", h, GroundLevel-1)
	}
}

// TestMotionHeightTracksTheWrittenColumnThroughSetBlocks checks the batched
// write /fill and explosions take: SetBlocks refreshes each written column's
// cached motion height and leaves an untouched column's where it was.
func TestMotionHeightTracksTheWrittenColumnThroughSetBlocks(t *testing.T) {
	t.Parallel()
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "bottom"})

	w := newRainWorld(t, "minecraft:plains")
	for _, col := range [][2]int32{{0, 0}, {1, 0}, {2, 2}} {
		w.motionBlockingHeight(col[0], col[1])
	}
	applied := w.SetBlocks([]BlockChange{
		{X: 0, Y: GroundLevel + 4, Z: 0, State: slab},
		{X: 2, Y: GroundLevel + 6, Z: 2, State: slab},
	})
	if len(applied) != 2 {
		t.Fatalf("SetBlocks applied %d of 2 writes", len(applied))
	}
	if h := w.motionBlockingHeight(0, 0); h != GroundLevel+4 {
		t.Errorf("the first written column's motion height = %d, want %d", h, GroundLevel+4)
	}
	if h := w.motionBlockingHeight(2, 2); h != GroundLevel+6 {
		t.Errorf("the second written column's motion height = %d, want %d", h, GroundLevel+6)
	}
	if h := w.motionBlockingHeight(1, 0); h != GroundLevel-1 {
		t.Errorf("the neighbour column's motion height = %d, want %d", h, GroundLevel-1)
	}
}

// motionHeightPrimed reports whether the chunk at pos already carries its
// MOTION_BLOCKING heightmap.
func motionHeightPrimed(w *World, pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	return ok && loaded.motion != nil
}

// TestAChunkArrivesWithItsMotionHeightmap checks the heightmap the rain reads
// is built by whoever prepares the chunk — the chunk worker for the pool, the
// caller for the synchronous load — rather than lazily under the write lock on
// the tick goroutine's first rain check.
func TestAChunkArrivesWithItsMotionHeightmap(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, newFlatGenerator("minecraft:plains", []flatLayer{{state: stoneState, height: GroundLevel - worldMinY}}))
	t.Cleanup(func() { w.Close() })

	w.Acquire(ChunkPos{X: 0, Z: 0})
	if !motionHeightPrimed(w, ChunkPos{X: 0, Z: 0}) {
		t.Error("the synchronously acquired chunk has no motion heightmap")
	}
	<-w.AcquireAsync(ChunkPos{X: 1, Z: 0})
	if !motionHeightPrimed(w, ChunkPos{X: 1, Z: 0}) {
		t.Error("the worker-acquired chunk has no motion heightmap")
	}
}

// TestMotionHeightCoversTheDimensions drives the primed heightmap against a
// fresh scan in each dimension, whose chunk memory has a different band of
// blocks: the nether and the end fill only part of the column the overworld
// shape spans, so the scan has air sections above and below to skip.
func TestMotionHeightCoversTheDimensions(t *testing.T) {
	t.Parallel()
	dimensions := []struct {
		name  string
		world func(t *testing.T) *World
		pos   ChunkPos
	}{
		{"overworld", func(t *testing.T) *World {
			w := newRainWorld(t, "minecraft:plains")
			return w
		}, ChunkPos{X: 0, Z: 0}},
		{"nether", func(t *testing.T) *World { w, _ := newDimensionWorld(t, true); return w }, ChunkPos{X: 0, Z: 0}},
		{"end", func(t *testing.T) *World { w, _ := newDimensionWorld(t, false); return w }, ChunkPos{X: 0, Z: 0}},
	}
	for _, d := range dimensions {
		w := d.world(t)
		w.mu.RLock()
		loaded := w.chunks[d.pos]
		primed := loaded != nil && loaded.motion != nil
		var got [16][16]int32
		if primed {
			got = *loaded.motion
		}
		chunk := (*Chunk)(nil)
		if loaded != nil {
			chunk = loaded.chunk
		}
		w.mu.RUnlock()
		if !primed {
			t.Errorf("the %s chunk has no motion heightmap", d.name)
			continue
		}
		for lx := range int32(16) {
			for lz := range int32(16) {
				want := int32(w.dim.minY - 1)
				for y := int32(worldMinY + worldHeight - 1); y >= worldMinY; y-- {
					if blocksMotionInHeightmap(chunk.BlockAt(lx, y, lz)) {
						want = y
						break
					}
				}
				if got[lx][lz] != want {
					t.Errorf("the %s column (%d, %d) motion height = %d, want %d", d.name, lx, lz, got[lx][lz], want)
				}
			}
		}
	}
}

// newPrecipitationWorld is a flat physics world of the given biome with the
// dice pinned to the seed, dry until the test drives the rain itself: the
// sky's own work is what these tests are about, so the rain does not come
// with the world the way newRainWorld's does.
func newPrecipitationWorld(t *testing.T, biome string, seed int64) (*World, *Physics) {
	t.Helper()
	w := newTestWorld(t, newFlatGenerator(biome, []flatLayer{{state: stoneState, height: GroundLevel - worldMinY}}))
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 2; cx++ {
		for cz := int32(-1); cz <= 2; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(seed))
	return w, p
}

// TestPrecipitationFreezesOpenWaterToIce drives the sky's work over a
// column whose top holds water in a biome too cold for rain: the water
// freezes to ice on the visit, winter or no.
func TestPrecipitationFreezesOpenWaterToIce(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 1)
	w.SetBlock(0, GroundLevel, 0, waterByLevel[0])

	var changes []BlockChange
	p.tickPrecipitation(0, 0, 1, &changes)

	if got, want := w.BlockAt(0, GroundLevel, 0), iceState; got != want {
		t.Fatalf("the visited water = %s, want %s", blockLabel(got), blockLabel(want))
	}
	if len(changes) != 1 || changes[0].X != 0 || changes[0].Y != GroundLevel || changes[0].Z != 0 ||
		changes[0].State != iceState {
		t.Errorf("the freeze's changes = %+v, want the one ice at (0, %d, 0)", changes, GroundLevel)
	}
}

// TestPrecipitationLeavesDeepWaterStanding checks the surrounded half of
// Biome.shouldFreeze: a cell with water on all four sides keeps its water
// while deep in the pond, and freezes once the rim about it has gone to
// ice. The pond's rim itself freezes on its own visits — every rim cell
// faces the stone shore.
func TestPrecipitationLeavesDeepWaterStanding(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 2)
	for dx := int32(0); dx <= 2; dx++ {
		for dz := int32(0); dz <= 2; dz++ {
			w.SetBlock(dx, GroundLevel-1, dz, waterByLevel[0])
		}
	}
	var changes []BlockChange

	// Deep in the pond, surrounded by water on every side: the visit
	// leaves the cell standing.
	p.tickPrecipitation(1, 1, 1, &changes)
	if got := w.BlockAt(1, GroundLevel-1, 1); got != waterByLevel[0] {
		t.Errorf("the pond's centre = %s after its first visit, want water", blockLabel(got))
	}

	// The rim freezes as its columns are visited.
	for dx := int32(0); dx <= 2; dx++ {
		for dz := int32(0); dz <= 2; dz++ {
			if dx == 1 && dz == 1 {
				continue
			}
			p.tickPrecipitation(dx, dz, 1, &changes)
			if got := w.BlockAt(dx, GroundLevel-1, dz); got != iceState {
				t.Errorf("pond rim at (%d, %d) = %s after its visit, want ice", dx, dz, blockLabel(got))
			}
		}
	}
	if got := w.BlockAt(1, GroundLevel-1, 1); got != waterByLevel[0] {
		t.Errorf("the pond's centre = %s after the rim froze, want water", blockLabel(got))
	}

	// With the rim ice, the centre is surrounded no more.
	p.tickPrecipitation(1, 1, 1, &changes)
	if got := w.BlockAt(1, GroundLevel-1, 1); got != iceState {
		t.Errorf("the pond's centre = %s after a second visit, want ice", blockLabel(got))
	}
}

// TestPrecipitationReadsWaterloggedFluids checks the fluid face of the
// freeze's two readings: a waterlogged block as the column top holds water
// yet is no water block, so it does not freeze, while its water does count
// as one of the four sides that keep a cell from freezing.
func TestPrecipitationReadsWaterloggedFluids(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 21)
	wetSlab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"})

	// A waterlogged slab is the column's own top block: the freeze reads
	// the slab, not the water its fluid state holds, and leaves it.
	w.SetBlock(0, GroundLevel, 0, wetSlab)
	var changes []BlockChange
	p.tickPrecipitation(0, 0, 1, &changes)
	if got := w.BlockAt(0, GroundLevel, 0); got != wetSlab {
		t.Errorf("the visited waterlogged slab = %s, want it left standing", blockLabel(got))
	}

	// A water cell walled in by waterlogged blocks on all four sides: the
	// four-sides check reads their fluid too, so the cell keeps its water.
	w.SetBlock(1, GroundLevel-1, 1, waterByLevel[0])
	for _, d := range [][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		w.SetBlock(1+d[0], GroundLevel-1, 1+d[1], wetSlab)
	}
	p.tickPrecipitation(1, 1, 1, &changes)
	if got := w.BlockAt(1, GroundLevel-1, 1); got != waterByLevel[0] {
		t.Errorf("the water walled in by waterlogged blocks = %s, want water", blockLabel(got))
	}
}

// TestPrecipitationSnowGrowsOneColumnToTheRuleHeight checks the snowfall's
// work: a visit lays the first layer at the column's first free cell, the
// next grows the pile a layer at a time, the growth lifts the bodies above
// it (Block.pushEntitiesUp), and the max_snow_accumulation_height rule is
// the depth the pile stops at — the rule's default of one among them.
func TestPrecipitationSnowGrowsOneColumnToTheRuleHeight(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 3)
	setRain(w)

	var changes []BlockChange
	for visit := 1; visit <= 2; visit++ {
		p.tickPrecipitation(0, 0, w.IntRule(RuleMaxSnowHeight), &changes)
		if got, want := w.BlockAt(0, GroundLevel, 0), snowByLayers[1]; got != want {
			t.Fatalf("after %d visits the pile = %s, want the default rule's one layer", visit, blockLabel(got))
		}
	}

	w.SetIntRule(RuleMaxSnowHeight, 3)
	for visit := 2; visit < 3+5; visit++ {
		p.tickPrecipitation(0, 0, w.IntRule(RuleMaxSnowHeight), &changes)
		want := snowByLayers[min(visit, 3)]
		if got := w.BlockAt(0, GroundLevel, 0); got != want {
			t.Fatalf("after %d visits the pile = %s, want %s", visit, blockLabel(got), blockLabel(want))
		}
		if got := w.BlockAt(0, GroundLevel+1, 0); got != airState {
			t.Fatalf("after %d visits the pile grew a second column at (%d, %d, %d)", visit, 0, GroundLevel+1, 0)
		}
	}
	if len(changes) == 0 || changes[len(changes)-1].PushUp != 0.125 {
		t.Errorf("the last growth's push = %v, want the eighth of a block a layer adds", changes[len(changes)-1].PushUp)
	}

	// The rule at zero lays nothing at all.
	w.SetIntRule(RuleMaxSnowHeight, 0)
	p.tickPrecipitation(8, 0, w.IntRule(RuleMaxSnowHeight), &changes)
	if got := w.BlockAt(8, GroundLevel, 0); got != airState {
		t.Errorf("a bare column under a zero rule = %s, want air", blockLabel(got))
	}
}

// TestPrecipitationSnowNeedsTheColdAndTheRain checks the gates: a warm
// biome takes no snow from its rain, and a cold one lays neither snow nor
// a cauldron's fill while its sky is dry — though the freeze, which asks
// no rain, still takes the water.
func TestPrecipitationSnowNeedsTheColdAndTheRain(t *testing.T) {
	t.Parallel()
	warm, warmPhysics := newPrecipitationWorld(t, "minecraft:plains", 4)
	setRain(warm)

	var changes []BlockChange
	warmPhysics.tickPrecipitation(0, 0, 8, &changes)
	if got := warm.BlockAt(0, GroundLevel, 0); got != airState {
		t.Errorf("the plains column's first free cell = %s, want air: the rain falls as rain, not snow", blockLabel(got))
	}

	cold, coldPhysics := newPrecipitationWorld(t, "minecraft:snowy_plains", 5)
	cold.SetBlock(0, GroundLevel, 0, waterByLevel[0])
	cold.SetBlock(2, GroundLevel, 0, registry.BlockStates["minecraft:cauldron"])

	for range 100 {
		coldPhysics.tickPrecipitation(0, 0, 8, &changes)
		coldPhysics.tickPrecipitation(2, 0, 8, &changes)
	}
	if got := cold.BlockAt(0, GroundLevel, 0); got != iceState {
		t.Errorf("the dry cold's water = %s, want ice: the freeze asks no rain", blockLabel(got))
	}
	if got := cold.BlockAt(2, GroundLevel, 0); got != registry.BlockStates["minecraft:cauldron"] {
		t.Errorf("the dry cold's cauldron = %s, want the empty cauldron", blockLabel(got))
	}
	if got := cold.BlockAt(3, GroundLevel, 0); got != airState {
		t.Errorf("the dry cold's bare column = %s, want no snow laid", blockLabel(got))
	}
}

// TestPrecipitationBalksAtBrightLight checks the light gates of the freeze
// and the snowfall: under a lantern's ten and more of block light the water
// stands and the sky lays nothing, while the same visit in the dark takes
// both. The lantern's light reaches the water through itself and the snow
// cells from the sides, the way block light does.
func TestPrecipitationBalksAtBrightLight(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 6)
	setRain(w)

	// A lantern set into the ground at (1, y, 0), water beside it at one
	// height and open columns either way; the same scene again twelve
	// blocks east, out of the light.
	w.SetBlock(1, GroundLevel-1, 0, registry.BlockStates["minecraft:lantern"])
	w.SetBlock(0, GroundLevel-1, 0, waterByLevel[0])
	w.SetBlock(0, GroundLevel-1, 12, waterByLevel[0])
	w.Relight(0, 0)
	for _, cell := range [][3]int32{
		{0, GroundLevel - 1, 0}, {1, GroundLevel, 1},
	} {
		if light := w.BlockLightAt(cell[0], cell[1], cell[2]); light < 10 {
			t.Fatalf("the lantern leaves (%d, %d, %d) at block light %d, want at least ten", cell[0], cell[1], cell[2], light)
		}
	}
	for _, cell := range [][3]int32{
		{0, GroundLevel - 1, 12}, {1, GroundLevel, 11},
	} {
		if light := w.BlockLightAt(cell[0], cell[1], cell[2]); light >= 10 {
			t.Fatalf("the dark east leaves (%d, %d, %d) at block light %d, want under ten", cell[0], cell[1], cell[2], light)
		}
	}

	var changes []BlockChange
	for range 10 {
		p.tickPrecipitation(0, 0, 1, &changes)
		p.tickPrecipitation(1, 1, 1, &changes)
		p.tickPrecipitation(0, 12, 1, &changes)
		p.tickPrecipitation(1, 11, 1, &changes)
	}
	if got := w.BlockAt(0, GroundLevel-1, 0); got != waterByLevel[0] {
		t.Errorf("the water beside the lantern = %s, want water: the light keeps the freeze off", blockLabel(got))
	}
	if got := w.BlockAt(1, GroundLevel, 1); got != airState {
		t.Errorf("the column beside the lantern = %s, want air: the light keeps the snow off", blockLabel(got))
	}
	if got := w.BlockAt(0, GroundLevel-1, 12); got != iceState {
		t.Errorf("the water in the dark = %s, want ice", blockLabel(got))
	}
	if got, want := w.BlockAt(1, GroundLevel, 11), snowByLayers[1]; got != want {
		t.Errorf("the column in the dark = %s, want %s", blockLabel(got), blockLabel(want))
	}
}

// TestPrecipitationFillsCauldronsOneLevelAtATime checks the cauldrons' half
// of the work (CauldronBlock.handlePrecipitation): an empty cauldron takes
// the first level — water in the rain, powder snow in the snowfall — a
// filling one rises until it is full and stops there, the cauldron with a
// different fill in it than the sky is dropping takes no level, and the
// lava cauldron never fills at all.
func TestPrecipitationFillsCauldronsOneLevelAtATime(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 7)
	setRain(w)
	empty := registry.BlockStates["minecraft:cauldron"]
	w.SetBlock(0, GroundLevel, 0, empty)
	w.SetBlock(2, GroundLevel, 0, registry.MustState("minecraft:water_cauldron", map[string]string{"level": "2"}))
	w.SetBlock(4, GroundLevel, 0, lavaCauldronState())

	var changes []BlockChange
	for range 800 {
		for _, col := range [][2]int32{{0, 0}, {2, 0}, {4, 0}} {
			p.tickPrecipitation(col[0], col[1], 8, &changes)
		}
	}
	if got, want := w.BlockAt(0, GroundLevel, 0), powderSnowCauldronByLevel[cauldronFullLevel]; got != want {
		t.Errorf("the empty cauldron in the snowfall = %s, want the full %s", blockLabel(got), blockLabel(want))
	}
	if got, want := w.BlockAt(2, GroundLevel, 0), waterCauldronByLevel[2]; got != want {
		t.Errorf("the water cauldron in the snowfall = %s, want the %s it started as", blockLabel(got), blockLabel(want))
	}
	if got, want := w.BlockAt(4, GroundLevel, 0), lavaCauldronState(); got != want {
		t.Errorf("the lava cauldron = %s, want the %s it started as", blockLabel(got), blockLabel(want))
	}

	raining, rainingPhysics := newPrecipitationWorld(t, "minecraft:plains", 8)
	setRain(raining)
	raining.SetBlock(0, GroundLevel, 0, empty)
	raining.SetBlock(2, GroundLevel, 0, powderSnowCauldronByLevel[1])
	for range 800 {
		for _, col := range [][2]int32{{0, 0}, {2, 0}} {
			rainingPhysics.tickPrecipitation(col[0], col[1], 8, &changes)
		}
	}
	if got, want := raining.BlockAt(0, GroundLevel, 0), waterCauldronByLevel[cauldronFullLevel]; got != want {
		t.Errorf("the empty cauldron in the rain = %s, want the full %s", blockLabel(got), blockLabel(want))
	}
	if got, want := raining.BlockAt(2, GroundLevel, 0), powderSnowCauldronByLevel[1]; got != want {
		t.Errorf("the powder-snow cauldron in the rain = %s, want the %s it started as", blockLabel(got), blockLabel(want))
	}
}

// TestTheCauldronFillDiceLandAtVanillasRates pins the cauldron's two rolls
// (CauldronBlock.shouldHandlePrecipitation): the rain fills on one visit in
// twenty, the snow on one in ten. A rate doubled or halved lands outside the
// bounds many seeded visits draw around the expected count.
func TestTheCauldronFillDiceLandAtVanillasRates(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 1)
	const visits = 4000
	empty := registry.BlockStates["minecraft:cauldron"]

	fills := func(precip precipitation) int {
		p.rng = rand.New(rand.NewSource(1))
		count := 0
		changes := make([]BlockChange, 0, 1)
		for range visits {
			w.SetBlock(0, GroundLevel, 0, empty)
			changes = changes[:0]
			p.blockTakesPrecipitation(0, GroundLevel, 0, precip, &changes)
			if len(changes) > 0 {
				count++
			}
		}
		return count
	}
	if got := fills(precipRain); got < 150 || got > 260 {
		t.Errorf("the rain filled the cauldron on %d of %d visits, want about one in twenty (150..260)", got, visits)
	}
	if got := fills(precipSnow); got < 320 || got > 480 {
		t.Errorf("the snow filled the cauldron on %d of %d visits, want about one in ten (320..480)", got, visits)
	}
}

// TestTheSkyDieLandsOnAColumnOnceInFortyEight pins the share of the chunk's
// dice the sky's work waits behind: one face in forty-eight hands the pass
// its column. Each seeded pass throws a fixed number of dice at a lone water
// column, so the passes that froze it track the share; a die of one in eight
// or of one in ninety-six lands outside the bounds the passes draw.
func TestTheSkyDieLandsOnAColumnOnceInFortyEight(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 1)
	const (
		passes = 400
		dice   = 12288 // forty-eight times the chunk's 256 columns
	)
	frozen := 0
	for seed := int64(1); seed <= passes; seed++ {
		w.SetBlock(0, GroundLevel, 0, waterByLevel[0])
		p.rng = rand.New(rand.NewSource(seed))
		p.RandomTick(seed, []ChunkPos{{0, 0}}, dice, 1)
		if w.BlockAt(0, GroundLevel, 0) == iceState {
			frozen++
		}
	}
	if frozen < 220 || frozen > 285 {
		t.Errorf("the sky froze the lone column on %d of %d passes, want the one-in-forty-eight share's count (220..285)", frozen, passes)
	}
}

// TestRandomTickPassCarriesTheSkyWork drives the pass itself — not the
// column visitor — over a snowbound chunk: the forty-eight die finds the
// columns, and every one of them gets its pond cell frozen and its open
// stone laid with the snow the rule's height allows.
func TestRandomTickPassCarriesTheSkyWork(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 9)
	setRain(w)
	w.SetIntRule(RuleMaxSnowHeight, 2)
	for dx := int32(0); dx <= 2; dx++ {
		for dz := int32(0); dz <= 2; dz++ {
			w.SetBlock(dx, GroundLevel-1, dz, waterByLevel[0])
		}
	}

	for pass := 1; pass <= 160; pass++ {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3000, int(w.IntRule(RuleMaxSnowHeight)))
	}

	for dx := int32(0); dx <= 15; dx++ {
		for dz := int32(0); dz <= 15; dz++ {
			if dx <= 2 && dz <= 2 {
				if surface := w.BlockAt(dx, GroundLevel-1, dz); surface != iceState {
					t.Errorf("pond cell (%d, %d) = %s, want ice", dx, dz, blockLabel(surface))
				}
				continue
			}
			if got, want := w.BlockAt(dx, GroundLevel, dz), snowByLayers[2]; got != want {
				t.Errorf("column (%d, %d) = %s, want %s", dx, dz, blockLabel(got), blockLabel(want))
			}
		}
	}
}

// newDimensionWorld builds one of the two worlds that hang off the
// overworld's — the nether or the end — around its own origin chunk, for
// the dimension side of the sky's work.
func newDimensionWorld(t *testing.T, nether bool) (*World, *Physics) {
	t.Helper()
	gen := Generator(NewEndGenerator(42))
	if nether {
		gen = NewNetherGenerator(42)
	}
	w := NewDimensionWorld(t.TempDir(), nether, gen)
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{X: 0, Z: 0})
	p := NewPhysics(w)
	p.rng = rand.New(rand.NewSource(11))
	return w, p
}

// TestTheSkyLeavesTheNetherAndTheEndAlone checks the two dimensions without
// weather of their own: even with the rain level forced up in their skies,
// their biomes answer every precipitation question with none, so no water
// freezes, no snow is laid and no cauldron fills.
func TestTheSkyLeavesTheNetherAndTheEndAlone(t *testing.T) {
	t.Parallel()
	for _, nether := range []bool{true, false} {
		w, p := newDimensionWorld(t, nether)
		setRain(w) // a state no weather cycle of theirs reaches; the
		// biomes must keep them dry anyway

		// A scene high above whatever the dimension grew: stone pedestals
		// with water, an open cauldron and a bare stone top each.
		base := int32(130)
		if !nether {
			base = 100
		}
		empty := registry.BlockStates["minecraft:cauldron"]
		for _, col := range [][2]int32{{0, 0}, {2, 0}, {4, 0}} {
			w.SetBlock(col[0], base, col[1], stoneState)
		}
		w.SetBlock(0, base+1, 0, waterByLevel[0])
		w.SetBlock(2, base+1, 0, empty)

		for pass := 1; pass <= 40; pass++ {
			p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 600, 8)
		}

		if got := w.BlockAt(0, base+1, 0); got != waterByLevel[0] {
			t.Errorf("the %s water = %s, want the water it started as", w.Name(), blockLabel(got))
		}
		if got := w.BlockAt(2, base+1, 0); got != empty {
			t.Errorf("the %s cauldron = %s, want the empty cauldron it started as", w.Name(), blockLabel(got))
		}
		if got := w.BlockAt(4, base+1, 0); got != airState {
			t.Errorf("the %s bare column top = %s, want air: no snow lands there", w.Name(), blockLabel(got))
		}
	}
}

// TestIceMeltsInBrightLightOverThePass checks the other half of the ice the
// freeze lays: the plain ice random-ticks now, and a block-lit ice melts —
// to water in the overworld, to thin air in the nether — while the same ice
// in the dark stands.
func TestIceMeltsInBrightLightOverThePass(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 10)
	lantern := registry.BlockStates["minecraft:lantern"]
	w.SetBlock(1, GroundLevel-1, 0, lantern)
	w.SetBlock(0, GroundLevel-1, 0, iceState)
	w.SetBlock(0, GroundLevel-1, 12, iceState)
	w.Relight(0, 0)
	if light := w.BlockLightAt(0, GroundLevel-1, 0); light <= 10 {
		t.Fatalf("the ice beside the lantern = block light %d, want over ten", light)
	}
	if light := w.BlockLightAt(0, GroundLevel-1, 12); light > 10 {
		t.Fatalf("the dark ice = block light %d, want ten or under", light)
	}

	for pass := 1; pass <= 60; pass++ {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3000, 1)
	}
	if got, want := w.BlockAt(0, GroundLevel-1, 0), waterByLevel[0]; got != want {
		t.Errorf("the lit ice = %s, want the %s it melted into", blockLabel(got), blockLabel(want))
	}
	if got := w.BlockAt(0, GroundLevel-1, 12); got != iceState {
		t.Errorf("the dark ice = %s, want the ice it started as", blockLabel(got))
	}

	hell, hellPhysics := newDimensionWorld(t, true)
	hell.SetBlock(0, 130, 0, stoneState)
	hell.SetBlock(0, 131, 0, iceState)
	hell.SetBlock(2, 131, 0, lantern)
	hell.Relight(0, 0)
	if light := hell.BlockLightAt(0, 131, 0); light <= 10 {
		t.Fatalf("the nether ice beside the lantern = block light %d, want over ten", light)
	}
	for pass := 1; pass <= 60; pass++ {
		hellPhysics.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3000, 1)
	}
	if got := hell.BlockAt(0, 131, 0); got != airState {
		t.Errorf("the nether's lit ice = %s, want the air the water evaporated into", blockLabel(got))
	}
}

// lavaCauldronState is the lava cauldron, which has no item of its own and
// so no BlockStates row: its state is resolved by name.
func lavaCauldronState() uint16 {
	return registry.MustState("minecraft:lava_cauldron", nil)
}

// TestPrecipitationFreezesFrozenOceanWater checks the freeze follows the
// frozen ocean biome the generator now gives a cold sea.
func TestPrecipitationFreezesFrozenOceanWater(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:frozen_ocean", 1)
	w.SetBlock(0, GroundLevel, 0, waterByLevel[0])
	var changes []BlockChange
	p.tickPrecipitation(0, 0, 1, &changes)
	if got := w.BlockAt(0, GroundLevel, 0); got != iceState {
		t.Errorf("frozen ocean water = %s after a visit, want ice", blockLabel(got))
	}
}

// TestFrostedIceIgnoresRandomTicks checks that lit frosted ice is never melted
// by a random tick: only its scheduled tick touches it.
func TestFrostedIceIgnoresRandomTicks(t *testing.T) {
	t.Parallel()
	w, p := newPrecipitationWorld(t, "minecraft:snowy_plains", 10)
	w.SetBlock(1, GroundLevel-1, 0, registry.BlockStates["minecraft:lantern"])
	w.SetBlock(0, GroundLevel-1, 0, frostedIceStateByAge[0])
	w.Relight(0, 0)
	if light := w.BlockLightAt(0, GroundLevel-1, 0); light <= 10 {
		t.Fatalf("the frosted ice beside the lantern = block light %d, want over ten", light)
	}
	for pass := 1; pass <= 60; pass++ {
		p.RandomTick(int64(pass), []ChunkPos{{0, 0}}, 3000, 1)
	}
	if got, want := w.BlockAt(0, GroundLevel-1, 0), frostedIceStateByAge[0]; got != want {
		t.Errorf("the frosted ice = %s, want %s", blockLabel(got), blockLabel(want))
	}
}
