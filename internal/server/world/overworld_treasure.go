package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type buriedTreasureStructure struct{}

// vanilla: BuriedTreasureStructure.findGenerationPoint
func (buriedTreasureStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	return [3]int32{x + 8, ctx.g.columnHeight(x+8, z+8), z + 8}, func() []structurePiece {
		return []structurePiece{&buriedTreasurePiece{x: x + 9, y: 90, z: z + 9}}
	}, true
}

type buriedTreasurePiece struct {
	mu      sync.Mutex
	x, y, z int32
}

func (p *buriedTreasurePiece) bounds() jbox {
	p.mu.Lock()
	defer p.mu.Unlock()
	return jbox{p.x, p.y, p.z, p.x, p.y, p.z}
}

func (p *buriedTreasurePiece) nbt() nbt.Compound {
	b := p.bounds()
	return nbt.Compound{"id": nbt.String("minecraft:btp"), "GD": nbt.Int(0), "O": nbt.Int(-1), "BB": nbt.IntArray{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ}}
}

var buriedTreasureBase = blockSet(registry.BlockSandstone, registry.BlockStone, registry.BlockAndesite, registry.BlockGranite, registry.BlockDiorite)

// vanilla: BuriedTreasurePiece.postProcess
func (p *buriedTreasurePiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	liquidOrAir := func(s uint16) bool {
		b := registry.BlockOf(s)
		return r.src.isAir(s) || b == registry.BlockWater || b == registry.BlockLava
	}
	x, z := p.x, p.z
	for y := r.heightOf(hmOceanFloorWG, x, z); y > r.minY; y-- {
		current, below := r.get(x, y, z), r.get(x, y-1, z)
		if !buriedTreasureBase[registry.BlockOf(below)] {
			continue
		}
		soft := current
		if liquidOrAir(soft) {
			soft = registry.BlockSand.DefaultState()
		}
		// Direction.values: down, up, north, south, west, east.
		for i, d := range [6]posKey{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			nx, ny, nz := x+d[0], y+d[1], z+d[2]
			if !liquidOrAir(r.get(nx, ny, nz)) {
				continue
			}
			s := soft
			if i != 1 && liquidOrAir(r.get(nx, ny-1, nz)) {
				s = below
			}
			r.set(nx, ny, nz, s)
		}
		p.y = y
		// A north-oriented, one-block frame maps its origin to this world block.
		frame := levelgen.PieceFrame{BB: levelgen.BoundingBox{MinX: int(x), MinY: int(y), MinZ: int(z), MaxX: int(x), MaxY: int(y), MaxZ: int(z)}}
		frame.CreateChest(&strongholdChests{r: r, st: shStatesOnce(), loot: "minecraft:chests/buried_treasure"}, toBoundingBox(chunk), 0, 0, 0)
		return
	}
}
