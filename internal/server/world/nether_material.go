package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

var netherMaterials = [...]terrain.Material{terrain.Air, terrain.Lava, terrain.Netherrack, terrain.Bedrock, terrain.Basalt, terrain.Blackstone, terrain.SoulSand, terrain.SoulSoil, terrain.Gravel, terrain.WarpedWartBlock, terrain.WarpedNylium, terrain.NetherWartBlock, terrain.CrimsonNylium}

func (g *NetherGenerator) materialStates() []uint16 {
	return []uint16{airState, g.lavaState, g.netherrackState, g.bedrockState, g.basaltState, g.blackstoneState, g.soulSandState, g.soulSoilState, g.gravelState, g.warpedWartState, g.warpedNyliumState, g.netherWartState, g.crimsonNyliumState}
}

func (g *NetherGenerator) copyMaterials(c *Chunk, chunk *terrain.ProtoChunk) {
	var states [256]uint16
	for i, s := range g.materialStates() {
		states[netherMaterials[i]] = s
	}
	c.fillFromProto(chunk, &states, false)
}

type netherMaterialBiomes struct {
	g        *NetherGenerator
	quarts   *netherQuarts
	qx0, qz0 int32
}

func (b netherMaterialBiomes) Index(path string) int {
	r, _ := registry.Get("minecraft:worldgen/biome")
	return int(r.ID("minecraft:" + path))
}
func (b netherMaterialBiomes) At(x, y, z int32) int {
	px, pz := (x-2)>>2-b.qx0, (z-2)>>2-b.qz0
	q := b.quarts
	if biome := q[px][pz]; biome == q[px+1][pz] && biome == q[px][pz+1] && biome == q[px+1][pz+1] {
		return int(b.g.biomes[biome])
	}
	qx, _, qz := levelgen.ZoomedQuart(b.g.zoomSeed, x, y, z)
	return int(b.g.biomes[q[qx-b.qx0][qz-b.qz0]])
}
func (netherMaterialBiomes) Temperature(_ int, _, _, _, _ int32) float32 { return 2 }
func (g *NetherGenerator) terrainBiomes(q *netherQuarts, cx, cz int32, carve bool) terrain.TerrainBiomes {
	b := netherMaterialBiomes{g, q, cx*4 - 1, cz*4 - 1}
	return terrain.TerrainBiomes{Surface: b, Carving: b, Carvers: func(int32, int32) []terrain.Carver {
		if carve {
			return terrain.NetherCarvers
		}
		return nil
	}}
}

// buildTerrain fills, surfaces and optionally carves a chunk with one noise context.
// vanilla: NoiseBasedChunkGenerator.buildTerrain
func (g *NetherGenerator) buildTerrain(c *Chunk, carve bool) {
	chunk := terrain.NewProtoChunk(0, 256, terrain.Netherrack)
	q := g.quartBiomes(c.x, c.z)
	g.state.BuildTerrain(chunk, c.x, c.z, g.fossilBeardifier(c.x, c.z), g.terrainBiomes(&q, c.x, c.z, carve))
	g.copyMaterials(c, chunk)
	g.fillBiomes(c, &q)
}
