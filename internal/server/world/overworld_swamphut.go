package world

import (
	"fmt"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() { overworldStructureTypes["minecraft:swamp_hut"] = swampHutStructure{} }

type swampHutStructure struct{}

// vanilla: SwampHutStructure.findGenerationPoint, onTopOfChunkCenter
func (swampHutStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	return [3]int32{x + 8, ctx.g.firstFreeHeight(x+8, z+8) - 1, z + 8}, func() []structurePiece {
		return []structurePiece{newSwampHut(ctx.rng, x, z)}
	}, true
}

// vanilla: SwampHutPiece
type swampHutPiece struct {
	scatteredPiece
	spawnedWitch, cat bool
}

func newSwampHut(rng levelgen.RandomSource, x, z int32) *swampHutPiece {
	dir := int(rng.NextIntN(4))
	width, depth := int32(7), int32(9)
	if dir&1 != 0 {
		width, depth = depth, width
	}
	return &swampHutPiece{box: jbox{x, 64, z, x + width - 1, 70, z + depth - 1}, dir: dir, heightPosition: -1}
}

func (p *swampHutPiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	tag := p.header("minecraft:tesh", 7, 7, 9)
	tag["Witch"] = nbtBool(p.spawnedWitch)
	tag["Cat"] = nbtBool(p.cat)
	return tag
}

// vanilla: SwampHutPiece.postProcess
func (p *swampHutPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.averageGround(r, chunk) {
		return
	}
	pl := &strongholdPlacer{r: r, st: shStatesOnce(), frame: levelgen.PieceFrame{BB: toBoundingBox(p.box), Dir: p.dir}, dir: p.dir, chunk: chunk, rng: r.rng}
	box := func(state uint16, x0, y0, z0, x1, y1, z1 int32) {
		pl.solid(x0, y0, z0, x1, y1, z1, state)
	}
	sprucePlanks := registry.BlockSprucePlanks.DefaultState()
	oakLog := registry.BlockOakLog.DefaultState()
	oakFence := registry.BlockOakFence.DefaultState()
	box(sprucePlanks, 1, 1, 1, 5, 1, 7)
	box(sprucePlanks, 1, 4, 2, 5, 4, 7)
	box(sprucePlanks, 2, 1, 0, 4, 1, 0)
	box(sprucePlanks, 2, 2, 2, 3, 3, 2)
	box(sprucePlanks, 1, 2, 3, 1, 3, 6)
	box(sprucePlanks, 5, 2, 3, 5, 3, 6)
	box(sprucePlanks, 2, 2, 7, 4, 3, 7)
	box(oakLog, 1, 0, 2, 1, 3, 2)
	box(oakLog, 5, 0, 2, 5, 3, 2)
	box(oakLog, 1, 0, 7, 1, 3, 7)
	box(oakLog, 5, 0, 7, 5, 3, 7)
	pl.put(2, 3, 2, oakFence)
	pl.put(3, 3, 7, oakFence)
	pl.put(1, 3, 4, airState)
	pl.put(5, 3, 4, airState)
	pl.put(5, 3, 5, airState)
	pl.put(1, 3, 5, registry.BlockPottedRedMushroom.DefaultState())
	pl.put(3, 2, 6, registry.BlockCraftingTable.DefaultState())
	pl.put(4, 2, 6, registry.BlockCauldron.DefaultState())
	pl.put(1, 2, 1, oakFence)
	pl.put(5, 2, 1, oakFence)
	stairs := func(dir int, shape string) uint16 {
		state, _ := registry.WithProp(registry.BlockSpruceStairs.DefaultState(), registry.PropFacing, fenceDirNames[dir])
		if shape != "straight" {
			state, _ = registry.WithProp(state, registry.PropShape, shape)
		}
		return pl.localState(state)
	}
	box(stairs(shN, "straight"), 0, 4, 1, 6, 4, 1)
	box(stairs(shE, "straight"), 0, 4, 2, 0, 4, 7)
	box(stairs(shW, "straight"), 6, 4, 2, 6, 4, 7)
	box(stairs(shS, "straight"), 0, 4, 8, 6, 4, 8)
	pl.put(0, 4, 1, stairs(shN, "outer_right"))
	pl.put(6, 4, 1, stairs(shN, "outer_left"))
	pl.put(0, 4, 8, stairs(shS, "outer_left"))
	pl.put(6, 4, 8, stairs(shS, "outer_right"))
	for z := int32(2); z <= 7; z += 5 {
		for x := int32(1); x <= 5; x += 4 {
			pl.fillColumnDown(oakLog, x, -1, z)
		}
	}
	x, y, z := pl.worldPos(2, 2, 5)
	if !p.spawnedWitch && chunk.inside(x, y, z) {
		key := fmt.Sprintf("minecraft:swamp_hut/%d/%d/%d/%d/witch", r.worldSeed, x, y, z)
		r.spawnStructureMob("minecraft:witch", key, x, y, z, false)
		p.spawnedWitch = true
	}
	if !p.cat && chunk.inside(x, y, z) {
		key := fmt.Sprintf("minecraft:swamp_hut/%d/%d/%d/%d/cat", r.worldSeed, x, y, z)
		r.spawnStructureMob("minecraft:cat", key, x, y, z, true)
		p.cat = true
	}
}
