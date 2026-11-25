package world

import (
	"math"
	"math/rand"
	"testing"

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

// TestMotionHeightTracksTheWrittenColumn checks a write refreshes only its own
// cached column: a block raised in one column lifts that column's motion
// height and leaves its neighbour's where it was. Then batches that add,
// remove and replace tops in one column, and finally empty it, keep the cache
// equal to a full scan.
func TestMotionHeightTracksTheWrittenColumn(t *testing.T) {
	t.Parallel()
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "bottom"})

	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
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

	at := func(y int32, s uint16) BlockChange { return BlockChange{X: 0, Y: y, Z: 0, State: s} }
	top := int32(GroundLevel + 10)
	var clear []BlockChange
	for y := int32(worldMinY); y < top+8; y++ {
		clear = append(clear, at(y, airState))
	}
	batches := []struct {
		name string
		set  []BlockChange
	}{
		{"remove the slab", []BlockChange{at(GroundLevel+4, airState)}},
		{"add above", []BlockChange{at(top, stoneState)}},
		{"add and remove the new top", []BlockChange{at(top+3, stoneState), at(top+3, airState)}},
		{"remove top", []BlockChange{at(top, airState)}},
		{"replace top", []BlockChange{at(GroundLevel-1, slab)}},
		{"write below top", []BlockChange{at(GroundLevel-5, airState)}},
		{"raise and lower", []BlockChange{at(top, stoneState), at(top+1, stoneState), at(top+1, airState), at(top, airState)}},
		{"stand two tops", []BlockChange{at(top, stoneState), at(top-4, stoneState)}},
		// the standing top is top-4; the batch's last accepted write is below it
		{"mixed", []BlockChange{at(top+4, stoneState), at(top, airState), at(top-4, slab), at(top+4, airState), at(GroundLevel+2, stoneState)}},
		{"clear column", clear},
	}
	for _, b := range batches {
		w.SetBlocks(b.set)
		w.mu.RLock()
		loaded := w.chunks[ChunkPos{}]
		want := w.motionHeight(loaded.chunk, 0, 0)
		got := loaded.motion[0][0]
		w.mu.RUnlock()
		if got != want {
			t.Errorf("%s: cached height = %d, rescan = %d", b.name, got, want)
		}
		if b.name == "mixed" && got != top-4 {
			t.Errorf("mixed: cached height = %d, want %d", got, top-4)
		}
	}
	if h := w.motionBlockingHeight(0, 0); h != w.dim.minY-1 {
		t.Errorf("cleared column height = %d, want %d", h, w.dim.minY-1)
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
