package terrain

import "github.com/golem-mc/golem/internal/server/world/levelgen"

// CarveChunk is a chunk the terrain build fills, covers with its surface and
// carves.
type CarveChunk interface {
	SurfaceChunk
	// Get is the material at x, y, z; x and z wrap into the chunk.
	Get(x, y, z int32) Material
}

// PostProcessingChunk receives the fluid positions that must be updated when
// generation finishes. Coordinates are world block coordinates.
type PostProcessingChunk interface {
	MarkPosForPostProcessing(x, y, z int32)
}

// TerrainBiomes is what the biomes decide while a chunk's terrain is built.
type TerrainBiomes struct {
	// Surface is read by the surface build, with PossibleBiomes the ids it can
	// sample (nil disables the pruning).
	Surface        Biomes
	PossibleBiomes []int
	// Carving is read for the top material under a carved grass block.
	Carving Biomes
	// Carvers is the carver list of the biome at the min corner of a chunk.
	// vanilla: NoiseBasedChunkGenerator.getBiomeGenerationSettingsForCarver
	Carvers func(chunkX, chunkZ int32) []Carver
}

// uncarvable reports the blocks in the uncarvable block tag.
func (m Material) uncarvable() bool { return m == Bedrock }

// substanceMaterial is the block a substance places; ok is false for the
// default block.
func substanceMaterial(s Substance) (m Material, ok bool) {
	switch s {
	case SubstanceAir:
		return Air, true
	case SubstanceWater:
		return Water, true
	case SubstanceLava:
		return Lava, true
	}
	return Air, false
}

// BuildTerrain fills the chunk from the final density, builds its surface with
// the material rule and carves it, without a blender. It returns the mask of
// blocks reached by carvers, including blocks the aquifer leaves unchanged.
// A chunk implementing PostProcessingChunk receives the deferred fluid updates.
// vanilla: NoiseBasedChunkGenerator.buildTerrain
func (rs *RandomState) BuildTerrain(chunk CarveChunk, chunkX, chunkZ int32, b *Beardifier, biomes TerrainBiomes) *CarvingMask {
	nc := NewNoiseChunk(rs, b, rs.ChunkVolume(chunkX, chunkZ))
	defer nc.Recycle()
	post, _ := chunk.(PostProcessingChunk)
	density := nc.SampleDensity()
	nc.DoFill(density, func(x, y, z int32, s Substance) {
		m, ok := substanceMaterial(s)
		if !ok {
			m = rs.Materials.defaultMat
		}
		if m != Air {
			chunk.Set(x, y, z, m)
			if post != nil && m.IsFluid() && nc.aquifer.ShouldScheduleFluidUpdate() {
				post.MarkPosForPostProcessing(x, y, z)
			}
		}
	})
	nc.ctx.release(density)
	rs.Materials.BuildSurface(chunk, chunkX, chunkZ, nc, biomes.Surface, biomes.PossibleBiomes)
	mask := rs.CarvingMask(chunkX, chunkZ, biomes.Carvers)
	if !mask.Empty() {
		rs.applyCarvingMask(chunk, chunkX, chunkZ, mask, nc, biomes.Carving, post)
	}
	return mask
}

// CarvingMask runs the nearby chunks' carvers without sampling terrain.
// carvers returns each source chunk's biome's ordered carver list.
// vanilla: NoiseBasedChunkGenerator.generateCarvers
func (rs *RandomState) CarvingMask(chunkX, chunkZ int32, carvers func(int32, int32) []Carver) *CarvingMask {
	g := carveRange{rs.MinY, rs.Height}
	mask := NewCarvingMask(g.minY+1, g.minY+g.depth-1-carverProtectedTop)
	r := levelgen.NewLegacyRandom(0)
	for dx := int32(-carverRange); dx <= carverRange; dx++ {
		for dz := int32(-carverRange); dz <= carverRange; dz++ {
			sx, sz := chunkX+dx, chunkZ+dz
			for i, c := range carvers(sx, sz) {
				r.SetLargeFeatureSeed(rs.Seed+int64(i), sx, sz)
				if c.isStartChunk(r) {
					c.carve(g, r, chunkX, chunkZ, sx, sz, mask)
				}
			}
		}
	}
	return mask
}

// applyCarvingMask replaces the marked blocks with what the aquifer says at
// zero density, and turns the dirt under a carved grass block into the biome's
// top material. vanilla: NoiseBasedChunkGenerator.applyCarvingMask
func (rs *RandomState) applyCarvingMask(chunk CarveChunk, chunkX, chunkZ int32, mask *CarvingMask, nc *NoiseChunk, biomes Biomes, post PostProcessingChunk) {
	var ctx *materialContext
	var rule ruleFn
	defer func() {
		if ctx != nil {
			ctx.close()
		}
	}()
	mask.Visit(func(x, z, bottomY, topY int32) {
		hasGrass := false
		wx, wz := chunkX*16+x, chunkZ*16+z
		for y := topY; y >= bottomY; y-- {
			block := chunk.Get(wx, y, wz)
			if block.uncarvable() {
				continue
			}
			if block == GrassBlock || block == Mycelium {
				hasGrass = true
			}
			m, ok := substanceMaterial(nc.aquifer.ComputeSubstance(wx, y, wz, 0))
			if !ok {
				continue
			}
			chunk.Set(wx, y, wz, m)
			if post != nil && m.IsFluid() && nc.aquifer.ShouldScheduleFluidUpdate() {
				post.MarkPosForPostProcessing(wx, y, wz)
			}
			if !hasGrass || chunk.Get(wx, y-1, wz) != Dirt {
				continue
			}
			if ctx == nil {
				ctx = newMaterialContext(rs.Materials, nc, nc.volume, biomes, nil)
				ctx.sampleBandsAtPoint = true
				rule = rs.Materials.rule(ctx)
			}
			if top, ok := ctx.topMaterial(rule, chunk, wx, y-1, wz, m.IsFluid()); ok {
				chunk.Set(wx, y-1, wz, top)
				if post != nil && top.IsFluid() {
					post.MarkPosForPostProcessing(wx, y-1, wz)
				}
			}
		}
	})
}
