package worldgen

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestGeneratedSourceWaterInSeed12345(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := world.NewWorld(dir, world.NewOverworldGenerator(12345))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := world.ChunkPos{X: 1, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)

	// The ruined portal clears the cell, then post-processing refills it from
	// the water above magma before that water becomes a bubble column.
	// vanilla: LevelChunk.postProcessGeneration
	want := registry.BlockWater.DefaultState()
	if got := w.BlockAt(18, 13, 1); got != want {
		t.Fatalf("block at (18, 13, 1) = %s (state %d), want source water (state %d)", registry.BlockOf(got).Name(), got, want)
	}
	if got := registry.BlockOf(w.BlockAt(18, 13, 0)); got != registry.BlockBubbleColumn {
		t.Errorf("block at (18, 13, 0) = %s, want bubble column", got.Name())
	}

	w.Save()
	w.FlushSaves()
	root := servertest.ReadBlockChunkNBT(t, servertest.DimensionRegionPath(dir, "minecraft:overworld", pos), pos)
	ticks, _ := nbt.Get[nbt.List](root, "fluid_ticks")
	for _, item := range ticks.Items {
		tick := item.(nbt.Compound)
		x, _ := nbt.IntField(tick, "x")
		y, _ := nbt.IntField(tick, "y")
		z, _ := nbt.IntField(tick, "z")
		if x != 18 || y != 13 || z != 1 {
			continue
		}
		id, _ := nbt.Get[nbt.String](tick, "i")
		delay, _ := nbt.IntField(tick, "t")
		priority, _ := nbt.IntField(tick, "p")
		if id != "minecraft:water" || delay != 5 || priority != 0 {
			t.Fatalf("unexpected fluid tick at (18, 13, 1): %v", tick)
		}
		return
	}
	t.Error("new water source has no five-tick fluid update")
}

// TestReportedStandingWaterMatchesVanilla pins the frozen vanilla reference run:
// seed -7863699016225221906, chunks (-1,-4)..(0,-2). The reported (10,56,-55)
// is in (0,-4); the southern neighbours also contain waterlogged glow lichen
// whose post-processing leaves five-tick fluid updates.
func TestReportedStandingWaterMatchesVanilla(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	g := world.NewOverworldGenerator(-7863699016225221906)
	w, err := world.NewWorld(dir, g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	physics := world.NewPhysics(w)
	counts := map[world.ChunkPos]int{{X: -1, Z: -4}: 2186, {X: -1, Z: -3}: 612, {X: -1, Z: -2}: 5168, {X: 0, Z: -4}: 143, {X: 0, Z: -3}: 117, {X: 0, Z: -2}: 3998}
	hashes := map[world.ChunkPos]string{
		{X: -1, Z: -4}: "3efc860bff3f38b882073f8c650d5e9a61eabefafc3bc7919090b3c84de9f475",
		{X: -1, Z: -3}: "3a3116ad38196e2e3547c68933a054abcffaa1bee6c7a7bab7cb07b99aa40177",
		{X: -1, Z: -2}: "347d3f5f41e82b42f37a6fbc72a2f4fe7bc99843fa5a35230977f82d3e329eb8",
		{X: 0, Z: -4}:  "d2c3f69cd75b1c63006bfafdf64e273acf7797393c201c0a1bc1cca0ce635816",
		{X: 0, Z: -3}:  "812b39b6ae5bd653a6110e4b1ce465211a3cc5cdd853208486373fcb0adf6657",
		{X: 0, Z: -2}:  "893c1c8529e691e16dbcce09b3691b3a8afc6338750693460126efead2677059",
	}
	for x := int32(-1); x <= 0; x++ {
		for z := int32(-4); z <= -2; z++ {
			w.Acquire(world.ChunkPos{X: x, Z: z})
		}
	}
	// Drain generation's visits without advancing the frozen world's time.
	physics.Tick(0)
	source := registry.BlockWater.DefaultState()
	for pos, want := range counts {
		got := 0
		levels := make([]byte, 0, 384*256)
		for y := int32(-64); y < 320; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					state := w.BlockAt(pos.X*16+x, y, pos.Z*16+z)
					// One byte per block: water level + 1, 17 for a waterlogged
					// block, zero otherwise; vanilla section order is Y, Z, X.
					level := byte(0)
					if registry.StateBool(state, registry.PropWaterlogged) {
						level = 17
					}
					if registry.BlockOf(state) == registry.BlockWater {
						depth, _ := registry.StateInt(state, registry.PropLevel)
						level = byte(depth) + 1
						got++
						if state != source {
							t.Fatalf("flowing water at %d %d %d, want vanilla source", pos.X*16+x, y, pos.Z*16+z)
						}
					}
					levels = append(levels, level)
				}
			}
		}
		if hash := fmt.Sprintf("%x", sha256.Sum256(levels)); hash != hashes[pos] {
			t.Errorf("chunk %v water-state hash = %s, want %s", pos, hash, hashes[pos])
		}
		if got != want {
			t.Errorf("chunk %v water sources = %d, want %d", pos, got, want)
		}
	}
	wantTicks := map[[3]int32]bool{{-8, 27, -31}: true, {-4, 30, -22}: true, {-16, 35, -28}: true, {-5, 40, -32}: true, {9, 28, -22}: true, {1, 37, -19}: true}
	w.Save()
	w.FlushSaves()
	gotTicks := map[[3]int32]bool{}
	for pos := range counts {
		path := servertest.DimensionRegionPath(dir, "minecraft:overworld", pos)
		root := servertest.ReadBlockChunkNBT(t, path, pos)
		list, _ := nbt.Get[nbt.List](root, "fluid_ticks")
		for _, item := range list.Items {
			tick := item.(nbt.Compound)
			x, _ := nbt.IntField(tick, "x")
			y, _ := nbt.IntField(tick, "y")
			z, _ := nbt.IntField(tick, "z")
			delay, _ := nbt.IntField(tick, "t")
			priority, _ := nbt.IntField(tick, "p")
			at := [3]int32{x, y, z}
			if id, _ := nbt.Get[nbt.String](tick, "i"); id != "minecraft:water" || delay != 5 || priority != 0 {
				t.Errorf("unexpected fluid tick: %v", tick)
			}
			gotTicks[at] = true
		}
	}
	if !maps.Equal(gotTicks, wantTicks) {
		t.Errorf("fluid ticks = %v, want %v", gotTicks, wantTicks)
	}
}
