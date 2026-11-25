package terrain

import "testing"

func TestApplyCarvingMaskReusesTopMaterialRule(t *testing.T) {
	rs := NewRandomState(Overworld, 12345)
	biomes := &goldenBiomeSource{names: []string{"plains"}}
	compiles := 0
	rule := sequence(ifTrue(water(0, 0, false), block(GrassBlock)), block(Gravel))
	rs.Materials.rule = func(c *materialContext) ruleFn {
		compiles++
		return rule(c)
	}

	const chunkX, chunkZ = -2, 3
	volume := rs.ChunkVolume(chunkX, chunkZ)
	nc := NewNoiseChunk(rs, nil, volume)
	defer nc.Recycle()
	nc.aquifer = NewDisabledAquifer(NewFluidPicker(rs.SeaLevel, FluidWater))
	chunk := NewProtoChunk(rs.MinY, rs.Height, Stone)
	mask := NewCarvingMask(50, 80)

	chunk.Set(0, 75, 0, Stone)
	chunk.Set(0, 74, 0, Dirt)
	mask.Carve(0, 75, 0)
	rs.applyCarvingMask(chunk, chunkX, chunkZ, mask, nc, biomes, nil)
	if compiles != 0 {
		t.Fatalf("compiled the rule %d times without a grass or mycelium repair", compiles)
	}

	chunk.Set(1, 75, 0, GrassBlock)
	chunk.Set(1, 74, 0, Dirt)
	chunk.Set(1, 73, 0, Dirt)
	mask.Carve(1, 75, 0)
	mask.Carve(1, 74, 0)
	chunk.Set(2, 60, 0, Mycelium)
	chunk.Set(2, 59, 0, Dirt)
	mask.Carve(2, 60, 0)

	rs.applyCarvingMask(chunk, chunkX, chunkZ, mask, nc, biomes, nil)
	for _, want := range []struct {
		x, y int32
		mat  Material
	}{
		{1, 75, Air}, {1, 74, Air}, {1, 73, GrassBlock},
		{2, 60, Water}, {2, 59, Gravel},
	} {
		if got := chunk.Get(want.x, want.y, 0); got != want.mat {
			t.Errorf("block at (%d, %d, 0) = %v, want %v", want.x, want.y, got, want.mat)
		}
	}
	if compiles != 1 {
		t.Errorf("compiled the rule %d times for one chunk, want once", compiles)
	}
}
