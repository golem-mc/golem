package world

import (
	"fmt"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() { overworldStructureTypes["minecraft:monument"] = monumentStructure{} }

type monumentStructure struct{}

// vanilla: OceanMonumentStructure.findGenerationPoint, BiomeSource.getBiomesWithin
func (monumentStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16+9, ctx.chunkZ*16+9
	for qz := (z - 29) >> 2; qz <= (z+29)>>2; qz++ {
		for qx := (x - 29) >> 2; qx <= (x+29)>>2; qx++ {
			for qy := int32((levelgen.OverworldSeaLevel - 29) >> 2); qy <= (levelgen.OverworldSeaLevel+29)>>2; qy++ {
				b := ctx.g.biomeSource.At(qx, qy, qz)
				if !registry.Biome(ctx.g.biomeIDs[b]).InTag(registry.BiomeTagIDRequiredOceanMonumentSurrounding) {
					return [3]int32{}, nil, false
				}
			}
		}
	}
	x, z = ctx.chunkX*16+8, ctx.chunkZ*16+8
	return [3]int32{x, ctx.g.columnHeight(x, z), z}, func() []structurePiece {
		// Direction.Plane.HORIZONTAL is north, east, south, west.
		dir := int(ctx.rng.NextIntN(4))
		return []structurePiece{&monumentBuilding{levelgen.GenerateMonument(ctx.rng, int(ctx.chunkX*16-29), int(ctx.chunkZ*16-29), dir)}}
	}, true
}

// Only the building is saved; its children are reconstructed from the world's
// large-feature seed when a fresh generator reaches an unfinished chunk.
// vanilla: OceanMonumentStructure.regeneratePiecesAfterLoad
type monumentBuilding struct{ *levelgen.MonumentLayout }

func (p *monumentBuilding) bounds() jbox { return monumentBox(p.BB) }

func monumentBox(b levelgen.BoundingBox) jbox {
	return jbox{int32(b.MinX), int32(b.MinY), int32(b.MinZ), int32(b.MaxX), int32(b.MaxY), int32(b.MaxZ)}
}

func (p *monumentBuilding) nbt() nbt.Compound {
	b := p.bounds()
	return nbt.Compound{
		"id": nbt.String("minecraft:omb"), "GD": nbt.Int(0), "O": nbt.Int((p.Dir + 2) & 3),
		"BB": nbt.IntArray{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ},
	}
}

// vanilla: OceanMonumentPieces.MonumentBuilding.postProcess
func (p *monumentBuilding) place(r *decorRegion, chunk jbox, _ [3]int32) {
	pl := newMonumentPlacer(r, chunk, levelgen.PieceFrame{BB: p.BB, Dir: p.Dir}, levelgen.OverworldSeaLevel)
	pl.rng = r.rng
	pl.exterior()
	for _, child := range p.Pieces {
		if monumentBox(child.BB).intersects(chunk) {
			pl.frame = levelgen.PieceFrame{BB: child.BB, Dir: p.Dir}
			pl.room(child)
		}
	}
}

// vanilla: OceanMonumentPiece.spawnElder, ElderGuardian.<init>
func (p *monumentPlacer) spawnElder(x, y, z int32) {
	x, y, z = p.worldPos(x, y, z)
	if !p.chunk.inside(x, y, z) {
		return
	}
	p.r.spawnStructureMob("minecraft:elder_guardian", fmt.Sprintf("monument/%d/%d/%d/%d", p.r.worldSeed, x, y, z), x, y, z, false)
}
