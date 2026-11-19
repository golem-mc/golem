package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

const clayBandCount = 192

// MaterialSystem builds a chunk's surface and ore veins with the dimension's
// material rule. vanilla: MaterialSystem
type MaterialSystem struct {
	rs          *RandomState
	rule        materialRule
	defaultMat  Material
	preliminary Sampler
	bands       [clayBandCount]Material

	surface, surfaceSecondary                           *NoiseStack
	clayBandsOffset                                     *NoiseStack
	badlandsPillar, badlandsPillarRoof, badlandsSurface *NoiseStack
	icebergPillar, icebergPillarRoof, icebergSurface    *NoiseStack
}

// newMaterialSystem is vanilla: MaterialSystem.<init>.
func newMaterialSystem(rs *RandomState, rule materialRule, defaultMat Material) *MaterialSystem {
	n := func(name string) *NoiseStack { return rs.Noise("minecraft:" + name) }
	s := &MaterialSystem{
		rs: rs, rule: rule, defaultMat: defaultMat, preliminary: rs.Router.ChunkSurfaceLevel,
		clayBandsOffset: n("clay_bands_offset"),
		surface:         n("surface"), surfaceSecondary: n("surface_secondary"),
		badlandsPillar: n("badlands_pillar"), badlandsPillarRoof: n("badlands_pillar_roof"), badlandsSurface: n("badlands_surface"),
		icebergPillar: n("iceberg_pillar"), icebergPillarRoof: n("iceberg_pillar_roof"), icebergSurface: n("iceberg_surface"),
	}
	s.bands = generateBands(rs.random.FromHashOf("minecraft:clay_bands"))
	return s
}

// surfaceDepth is vanilla: MaterialSystem.getSurfaceDepth.
func (s *MaterialSystem) surfaceDepth(x, z int32, rnd levelgen.RandomSource) int32 {
	v := float64(s.surface.Get3D(float64(x), 0, float64(z)))
	s.rs.random.SeedAt(rnd, x, 0, z)
	return int32(float64(v*2.75) + 3.0 + float64(rnd.NextDouble()*0.25))
}

// band is vanilla: MaterialSystem.getBand.
func (s *MaterialSystem) band(x, y, z int32) Material {
	offset := int32(math.Floor(float64(float32(s.clayBandsOffset.Get3D(float64(x), 0, float64(z))*4.0)) + 0.5))
	return s.bands[(y+offset+clayBandCount)%clayBandCount]
}

// generateBands is vanilla: MaterialSystem.generateBands.
func generateBands(r levelgen.RandomSource) [clayBandCount]Material {
	var bands [clayBandCount]Material
	for i := range bands {
		bands[i] = Terracotta
	}
	for i := 0; i < clayBandCount; i++ {
		i += int(r.NextIntN(5)) + 1
		if i >= clayBandCount {
			continue
		}
		bands[i] = OrangeTerracotta
	}
	makeBands(r, &bands, 1, YellowTerracotta)
	makeBands(r, &bands, 2, BrownTerracotta)
	makeBands(r, &bands, 1, RedTerracotta)
	whiteBands := int(r.NextIntN(7)) + 9
	for i, start := 0, 0; i < whiteBands && start < clayBandCount; i, start = i+1, start+int(r.NextIntN(16))+4 {
		bands[start] = WhiteTerracotta
		if start-1 > 0 && r.NextBoolean() {
			bands[start-1] = LightGrayTerracotta
		}
		if start+1 >= clayBandCount || !r.NextBoolean() {
			continue
		}
		bands[start+1] = LightGrayTerracotta
	}
	return bands
}

// makeBands is vanilla: MaterialSystem.makeBands.
func makeBands(r levelgen.RandomSource, bands *[clayBandCount]Material, baseWidth int, m Material) {
	count := int(r.NextIntN(10)) + 6
	for range count {
		width := baseWidth + int(r.NextIntN(3))
		start := int(r.NextIntN(clayBandCount))
		for p := 0; start+p < clayBandCount && p < width; p++ {
			bands[start+p] = m
		}
	}
}

// surfaceColumn reads and writes one column of a chunk.
type surfaceColumn struct {
	chunk      SurfaceChunk
	x, z       int32
	minY, maxY int32
	defaultMat Material
}

func (c *surfaceColumn) block(y int32) ColumnBlock {
	if y < c.minY || y > c.maxY {
		return ColumnAir
	}
	return c.chunk.Block(c.x, y, c.z)
}

func (c *surfaceColumn) set(y int32, m Material) {
	if y >= c.minY && y <= c.maxY {
		c.chunk.Set(c.x, y, c.z, m)
	}
}

// isStone is vanilla: MaterialSystem.isStone.
func isStone(b ColumnBlock) bool { return b == ColumnDefault || b == ColumnOther }

// BuildSurface runs the material rule over every column of the chunk,
// replacing the blocks the fill placed. possibleBiomes contains every biome the
// chunk can sample, including across its borders; nil disables biome pruning.
// vanilla: MaterialSystem.buildSurface
func (s *MaterialSystem) BuildSurface(chunk SurfaceChunk, chunkX, chunkZ int32, nc *NoiseChunk, biomes Biomes, possibleBiomes []int) {
	minBlockX, minBlockZ := chunkX*16, chunkZ*16
	minY, maxY := chunk.MinY(), chunk.MaxY()
	full := nc.volume
	narrowed := NewVolume(full.SizeX, max(chunk.TopFilledY()-full.MinY+1, 1), full.SizeZ, full.MinX, full.MinY, full.MinZ)
	ctx := newMaterialContext(s, nc, narrowed, biomes, possibleBiomes)
	defer ctx.close()
	rule := s.rule(ctx)
	eroded, frozen, deepFrozen := biomes.Index("eroded_badlands"), biomes.Index("frozen_ocean"), biomes.Index("deep_frozen_ocean")
	col := &surfaceColumn{chunk: chunk, minY: minY, maxY: maxY, defaultMat: s.defaultMat}
	for x := range int32(16) {
		for z := range int32(16) {
			blockX, blockZ := minBlockX+x, minBlockZ+z
			col.x, col.z = blockX, blockZ
			startingHeight := chunk.Height(x, z) + 1
			surfaceBiome := biomes.At(blockX, startingHeight, blockZ)
			if surfaceBiome == eroded {
				s.erodedBadlandsExtension(col, blockX, blockZ, startingHeight)
			}
			height := chunk.Height(x, z) + 1
			gradX, gradZ := surfaceGradients(chunk, x, z)
			ctx.updateXZ(blockX, blockZ, gradX, gradZ)
			stoneAbove, waterHeight, nextCeilingStoneY := int32(0), int32(noWater), int32(math.MaxInt32)
			for y := height; y >= minY; y-- {
				switch col.block(y) {
				case ColumnAir:
					stoneAbove, waterHeight = 0, noWater
					continue
				case ColumnFluid:
					if waterHeight == noWater {
						waterHeight = y + 1
					}
					continue
				}
				if nextCeilingStoneY >= y {
					nextCeilingStoneY = wayBelowMinY
					for lookahead := y - 1; lookahead >= minY-1; lookahead-- {
						if !isStone(col.block(lookahead)) {
							nextCeilingStoneY = lookahead + 1
							break
						}
					}
				}
				stoneAbove++
				ctx.updateY(stoneAbove, y-nextCeilingStoneY+1, waterHeight, y)
				if y < minY || y > maxY {
					continue
				}
				if m, ok := rule(blockX, y, blockZ); ok {
					col.set(y, m)
				}
			}
			if surfaceBiome == frozen || surfaceBiome == deepFrozen {
				s.frozenOceanExtension(ctx.minSurfaceLevel(), biomes, surfaceBiome, col, blockX, blockZ, startingHeight)
			}
		}
	}
}

// surfaceGradients is the height difference across the column in x and in z.
// vanilla: MaterialSystem.getSurfaceGradientX, getSurfaceGradientZ
func surfaceGradients(chunk SurfaceChunk, x, z int32) (gradX, gradZ int32) {
	return chunk.Height(min(x+1, 15), z) - chunk.Height(max(x-1, 0), z),
		chunk.Height(x, min(z+1, 15)) - chunk.Height(x, max(z-1, 0))
}

// topMaterial is the block the rule puts at the top of a column at x, y, z,
// with ok false where it places none. vanilla: MaterialSystem.topMaterial
func (s *MaterialSystem) topMaterial(chunk SurfaceChunk, nc *NoiseChunk, biomes Biomes, x, y, z int32, underFluid bool) (m Material, ok bool) {
	ctx := newMaterialContext(s, nc, NewVolume(1, 1, 1, x, y, z), biomes, nil)
	defer ctx.close()
	rule := s.rule(ctx)
	gradX, gradZ := surfaceGradients(chunk, x&15, z&15)
	ctx.updateXZ(x, z, gradX, gradZ)
	water := int32(noWater)
	if underFluid {
		water = y + 1
	}
	ctx.updateY(1, 1, water, y)
	return rule(x, y, z)
}

// erodedBadlandsExtension is vanilla: MaterialSystem.erodedBadlandsExtension.
func (s *MaterialSystem) erodedBadlandsExtension(col *surfaceColumn, blockX, blockZ, height int32) {
	bx, bz := float64(blockX), float64(blockZ)
	pillarBuffer := math.Min(
		math.Abs(float64(s.badlandsSurface.Get3D(bx, 0, bz))*8.25),
		float64(float32(s.badlandsPillar.Get3D(bx*0.2, 0, bz*0.2)*15.0)))
	if pillarBuffer <= 0 {
		return
	}
	pillarFloor := math.Abs(float64(s.badlandsPillarRoof.Get3D(bx*0.75, 0, bz*0.75)) * 1.5)
	extensionTop := 64.0 + math.Min(float64(pillarBuffer*pillarBuffer*2.5), math.Ceil(float64(pillarFloor*50.0))+24.0)
	startY := floorInt(extensionTop)
	if height > startY {
		return
	}
	for y := startY; y >= col.minY; y-- {
		b := col.block(y)
		if b == ColumnDefault {
			break
		}
		if b == ColumnFluid {
			return
		}
	}
	for y := startY; y >= col.minY && col.block(y) == ColumnAir; y-- {
		col.set(y, s.defaultMat)
	}
}

// frozenOceanExtension is vanilla: MaterialSystem.frozenOceanExtension.
func (s *MaterialSystem) frozenOceanExtension(minSurfaceLevel int32, biomes Biomes, biome int, col *surfaceColumn, blockX, blockZ, height int32) {
	sea := s.rs.SeaLevel
	bx, bz := float64(blockX), float64(blockZ)
	iceberg := math.Min(
		math.Abs(float64(s.icebergSurface.Get3D(bx, 0, bz))*8.25),
		float64(float32(s.icebergPillar.Get3D(bx*1.28, 0, bz*1.28)*15.0)))
	if iceberg <= 1.8 {
		return
	}
	icebergRoof := math.Abs(float64(s.icebergPillarRoof.Get3D(bx*1.17, 0, bz*1.17)) * 1.5)
	top := math.Min(float64(iceberg*iceberg*1.2), math.Ceil(float64(icebergRoof*40.0))+14.0)
	if biomes.Temperature(biome, blockX, sea, blockZ, sea) > 0.1 {
		top -= 2.0
	}
	if top <= 2.0 {
		return
	}
	extensionBottom := float64(sea) - top - 7.0
	top += float64(sea)
	extensionTop := top
	r := s.rs.random.At(blockX, 0, blockZ)
	maxSnowDepth := 2 + r.NextIntN(4)
	minSnowHeight := sea + 18 + r.NextIntN(10)
	snowDepth := int32(0)
	for y := max(height, int32(extensionTop)+1); y >= minSurfaceLevel; y-- {
		b := col.block(y)
		air := b == ColumnAir && float64(y) < float64(int32(extensionTop)) && r.NextDouble() > 0.01
		if !air {
			// vanilla tests water by the block, which here is any fluid of an ocean column
			water := b == ColumnFluid && float64(y) > float64(int32(extensionBottom)) && y < sea && r.NextDouble() > 0.15
			if !water {
				continue
			}
		}
		if snowDepth <= maxSnowDepth && y > minSnowHeight {
			col.set(y, SnowBlock)
			snowDepth++
			continue
		}
		col.set(y, PackedIce)
	}
}
