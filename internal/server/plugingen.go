package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginGenerator adapts a plugin's Generator to world.Generator. It runs on
// the generation workers, so it touches only the chunk it is given and never
// the host's tick-bound API.
type pluginGenerator struct {
	gen        plugin.Generator
	seed       int64
	minY       int
	height     int
	seaLevel   int32
	dimensionN string
}

// SeaLevel is the sea level of the vanilla dimension type the shape is.
// vanilla: NoiseGeneratorSettings.seaLevel
func (g *pluginGenerator) SeaLevel() int32 { return g.seaLevel }

// Generate fills the chunk with the plugin's Generate, then its Populate
// clipped to the chunk: a write outside the chunk is dropped, because chunks
// here have no generation status to order a neighbour's decoration against.
// A panic in the plugin leaves the chunk as written so far.
func (g *pluginGenerator) Generate(c *world.Chunk) {
	pos := plugin.ChunkPos{X: int(c.X()), Z: int(c.Z())}
	g.guard("Generate", pos, func() {
		g.gen.Generate(pluginChunkWriter{c: c, pos: pos, minY: g.minY, height: g.height}, g.random(c))
	})
	g.guard("Populate", pos, func() {
		g.gen.Populate(pluginRegionWriter{c: c}, pos, g.random(c))
	})
}

func (g *pluginGenerator) guard(stage string, pos plugin.ChunkPos, f func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("plugin generator panicked", "stage", stage, "dimension", g.dimensionN, "x", pos.X, "z", pos.Z, "panic", r)
		}
	}()
	f()
}

// random is seeded from the world seed and the chunk position, the mixing
// vanilla's WorldgenRandom.setLargeFeatureSeed does.
func (g *pluginGenerator) random(c *world.Chunk) plugin.Random {
	r := levelgen.NewXoroshiroRandom(g.seed)
	a, b := r.NextLong(), r.NextLong()
	r.SetSeed(int64(c.X())*a ^ int64(c.Z())*b ^ g.seed)
	return pluginRandom{r}
}

type pluginRandom struct {
	r *levelgen.XoroshiroRandomSource
}

func (p pluginRandom) Int(n int) int {
	if n <= 0 {
		return 0
	}
	return int(p.r.NextIntN(int32(min(n, 1<<31-1))))
}
func (p pluginRandom) Int64() int64     { return p.r.NextLong() }
func (p pluginRandom) Float64() float64 { return p.r.NextDouble() }
func (p pluginRandom) Bool() bool       { return p.r.NextBoolean() }

type pluginChunkWriter struct {
	c            *world.Chunk
	pos          plugin.ChunkPos
	minY, height int
}

func (w pluginChunkWriter) Pos() plugin.ChunkPos { return w.pos }
func (w pluginChunkWriter) MinY() int            { return w.minY }
func (w pluginChunkWriter) Height() int          { return w.height }
func (w pluginChunkWriter) Set(x, y, z int, s plugin.BlockState) {
	w.c.SetBlock(int32(x), int32(y), int32(z), uint16(s))
}
func (w pluginChunkWriter) Get(x, y, z int) plugin.BlockState {
	return plugin.BlockState(w.c.BlockAt(int32(x), int32(y), int32(z)))
}
func (w pluginChunkWriter) SetBiome(x, y, z int, b registry.Biome) {
	w.c.SetCellBiome(int32(x), int32(y), int32(z), int32(b))
}

// pluginRegionWriter is the populated chunk in world coordinates.
type pluginRegionWriter struct{ c *world.Chunk }

func (w pluginRegionWriter) local(pos plugin.BlockPos) (x, z int32, ok bool) {
	x, z = int32(pos.X)-w.c.X()<<4, int32(pos.Z)-w.c.Z()<<4
	return x, z, x >= 0 && x < 16 && z >= 0 && z < 16
}
func (w pluginRegionWriter) Set(pos plugin.BlockPos, s plugin.BlockState) {
	if x, z, ok := w.local(pos); ok {
		w.c.SetBlock(x, int32(pos.Y), z, uint16(s))
	}
}
func (w pluginRegionWriter) Get(pos plugin.BlockPos) plugin.BlockState {
	if x, z, ok := w.local(pos); ok {
		return plugin.BlockState(w.c.BlockAt(x, int32(pos.Y), z))
	}
	return plugin.DefaultState(registry.BlockAir)
}
func (w pluginRegionWriter) Biome(pos plugin.BlockPos) registry.Biome {
	x, z, _ := w.local(pos)
	return registry.Biome(w.c.BiomeAt(min(max(x, 0), 15), int32(pos.Y), min(max(z, 0), 15)))
}
