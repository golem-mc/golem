package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func init() { overworldStructureTypes["minecraft:jungle_pyramid"] = jungleTempleStructure{} }

type jungleTempleStructure struct{}

// vanilla: SinglePieceStructure.findGenerationPoint, Structure.getLowestY
func (jungleTempleStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	if min(ctx.g.firstFreeHeight(x, z), ctx.g.firstFreeHeight(x+12, z), ctx.g.firstFreeHeight(x, z+15), ctx.g.firstFreeHeight(x+12, z+15))-1 < ctx.g.SeaLevel() {
		return [3]int32{}, nil, false
	}
	return [3]int32{x + 8, ctx.g.firstFreeHeight(x+8, z+8) - 1, z + 8}, func() []structurePiece {
		return []structurePiece{newJungleTemple(ctx.rng, x, z)}
	}, true
}

// vanilla: ScatteredFeaturePiece
type jungleTemplePiece struct {
	scatteredPiece
	placedMainChest   bool
	placedHiddenChest bool
	placedTrap1       bool
	placedTrap2       bool
}

func newJungleTemple(rng levelgen.RandomSource, x, z int32) *jungleTemplePiece {
	dir := int(rng.NextIntN(4)) // Direction.Plane.HORIZONTAL: north, east, south, west.
	w, d := int32(12), int32(15)
	if dir&1 != 0 {
		w, d = d, w
	}
	return &jungleTemplePiece{box: jbox{x, 64, z, x + w - 1, 73, z + d - 1}, dir: dir, heightPosition: -1}
}

func (p *jungleTemplePiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	tag := p.header("minecraft:tejp", 12, 10, 15)
	tag["placedMainChest"] = nbtBool(p.placedMainChest)
	tag["placedHiddenChest"] = nbtBool(p.placedHiddenChest)
	tag["placedTrap1"] = nbtBool(p.placedTrap1)
	tag["placedTrap2"] = nbtBool(p.placedTrap2)
	return tag
}

// vanilla: JungleTemplePiece.postProcess
func (p *jungleTemplePiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.averageGround(r, chunk) {
		return
	}
	pl := &strongholdPlacer{r: r, st: shStatesOnce(), frame: levelgen.PieceFrame{BB: toBoundingBox(p.box), Dir: p.dir}, dir: p.dir, chunk: chunk, rng: r.rng}
	put := func(x, y, z int32, state uint16) {
		pl.put(x, y, z, pl.localState(state))
	}
	stone := func(x0, y0, z0, x1, y1, z1 int32) {
		// Draw even outside chunkBB: the selector runs before placeBlock clips.
		levelgen.GenerateBox(int(x0), int(y0), int(z0), int(x1), int(y1), int(z1), func(x, y, z int, _ bool) {
			state := registry.BlockMossyCobblestone.DefaultState()
			if r.rng.NextFloat() < 0.4 {
				state = registry.BlockCobblestone.DefaultState()
			}
			put(int32(x), int32(y), int32(z), state)
		})
	}
	air := func(x0, y0, z0, x1, y1, z1 int32) { pl.solid(x0, y0, z0, x1, y1, z1, airState) }
	prop := func(state uint16, key registry.Prop, value string) uint16 {
		s, _ := registry.WithProp(state, key, value)
		return s
	}
	faceProp, _ := registry.PropNamed("face")
	chest := func(x, y, z int32) bool {
		return pl.frame.CreateChest(&strongholdChests{r: r, st: pl.st, loot: "minecraft:chests/jungle_temple"}, toBoundingBox(chunk), int(x), int(y), int(z))
	}
	dispenser := func(x, y, z int32, dir int) bool {
		wx, wy, wz := pl.worldPos(x, y, z)
		if !chunk.inside(wx, wy, wz) || registry.BlockOf(r.get(wx, wy, wz)) == registry.BlockDispenser {
			return false
		}
		put(x, y, z, prop(registry.BlockDispenser.DefaultState(), registry.PropFacing, fenceDirNames[dir]))
		r.placeBlockEntity(wx, wy, wz, r.get(wx, wy, wz), nbt.Compound{"id": nbt.String("minecraft:dispenser"), "LootTable": nbt.String("minecraft:chests/jungle_temple_dispenser")})
		return true
	}
	stone(0, -4, 0, 11, 0, 14)
	stone(2, 1, 2, 9, 2, 2)
	stone(2, 1, 12, 9, 2, 12)
	stone(2, 1, 3, 2, 2, 11)
	stone(9, 1, 3, 9, 2, 11)
	stone(1, 3, 1, 10, 6, 1)
	stone(1, 3, 13, 10, 6, 13)
	stone(1, 3, 2, 1, 6, 12)
	stone(10, 3, 2, 10, 6, 12)
	stone(2, 3, 2, 9, 3, 12)
	stone(2, 6, 2, 9, 6, 12)
	stone(3, 7, 3, 8, 7, 11)
	stone(4, 8, 4, 7, 8, 10)
	air(3, 1, 3, 8, 2, 11)
	air(4, 3, 6, 7, 3, 9)
	air(2, 4, 2, 9, 5, 12)
	air(4, 6, 5, 7, 6, 9)
	air(5, 7, 6, 6, 7, 8)
	air(5, 1, 2, 6, 2, 2)
	air(5, 2, 12, 6, 2, 12)
	air(5, 5, 1, 6, 5, 1)
	air(5, 5, 13, 6, 5, 13)
	put(1, 5, 5, registry.BlockAir.DefaultState())
	put(10, 5, 5, registry.BlockAir.DefaultState())
	put(1, 5, 9, registry.BlockAir.DefaultState())
	put(10, 5, 9, registry.BlockAir.DefaultState())
	for z2 := int32(0); z2 <= 14; z2 += 14 {
		stone(2, 4, z2, 2, 5, z2)
		stone(4, 4, z2, 4, 5, z2)
		stone(7, 4, z2, 7, 5, z2)
		stone(9, 4, z2, 9, 5, z2)
	}
	stone(5, 6, 0, 6, 6, 0)
	for x := int32(0); x <= 11; x += 11 {
		for z3 := int32(2); z3 <= 12; z3 += 2 {
			stone(x, 4, z3, x, 5, z3)
		}
		stone(x, 6, 5, x, 6, 5)
		stone(x, 6, 9, x, 6, 9)
	}
	stone(2, 7, 2, 2, 9, 2)
	stone(9, 7, 2, 9, 9, 2)
	stone(2, 7, 12, 2, 9, 12)
	stone(9, 7, 12, 9, 9, 12)
	stone(4, 9, 4, 4, 9, 4)
	stone(7, 9, 4, 7, 9, 4)
	stone(4, 9, 10, 4, 9, 10)
	stone(7, 9, 10, 7, 9, 10)
	stone(5, 9, 7, 6, 9, 7)
	eastStairs := prop(registry.BlockCobblestoneStairs.DefaultState(), registry.PropFacing, "east")
	westStairs := prop(registry.BlockCobblestoneStairs.DefaultState(), registry.PropFacing, "west")
	southStairs := prop(registry.BlockCobblestoneStairs.DefaultState(), registry.PropFacing, "south")
	northStairs := prop(registry.BlockCobblestoneStairs.DefaultState(), registry.PropFacing, "north")
	put(5, 9, 6, northStairs)
	put(6, 9, 6, northStairs)
	put(5, 9, 8, southStairs)
	put(6, 9, 8, southStairs)
	put(4, 0, 0, northStairs)
	put(5, 0, 0, northStairs)
	put(6, 0, 0, northStairs)
	put(7, 0, 0, northStairs)
	put(4, 1, 8, northStairs)
	put(4, 2, 9, northStairs)
	put(4, 3, 10, northStairs)
	put(7, 1, 8, northStairs)
	put(7, 2, 9, northStairs)
	put(7, 3, 10, northStairs)
	stone(4, 1, 9, 4, 1, 9)
	stone(7, 1, 9, 7, 1, 9)
	stone(4, 1, 10, 7, 2, 10)
	stone(5, 4, 5, 6, 4, 5)
	put(4, 4, 5, eastStairs)
	put(7, 4, 5, westStairs)
	for i := range int32(4) {
		put(5, 0-i, 6+i, southStairs)
		put(6, 0-i, 6+i, southStairs)
		air(5, 0-i, 7+i, 6, 0-i, 9+i)
	}
	air(1, -3, 12, 10, -1, 13)
	air(1, -3, 1, 3, -1, 13)
	air(1, -3, 1, 9, -1, 5)
	for z := int32(1); z <= 13; z += 2 {
		stone(1, -3, z, 1, -2, z)
	}
	for z := int32(2); z <= 12; z += 2 {
		stone(1, -1, z, 3, -1, z)
	}
	stone(2, -2, 1, 5, -2, 1)
	stone(7, -2, 1, 9, -2, 1)
	stone(6, -3, 1, 6, -3, 1)
	stone(6, -1, 1, 6, -1, 1)
	put(1, -3, 8, prop(prop(registry.BlockTripwireHook.DefaultState(), registry.PropFacing, "east"), registry.PropAttached, "true"))
	put(4, -3, 8, prop(prop(registry.BlockTripwireHook.DefaultState(), registry.PropFacing, "west"), registry.PropAttached, "true"))
	put(2, -3, 8, prop(prop(prop(registry.BlockTripwire.DefaultState(), registry.PropEast, "true"), registry.PropWest, "true"), registry.PropAttached, "true"))
	put(3, -3, 8, prop(prop(prop(registry.BlockTripwire.DefaultState(), registry.PropEast, "true"), registry.PropWest, "true"), registry.PropAttached, "true"))
	redstoneWireNS := prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropNorth, "side"), registry.PropSouth, "side")
	put(5, -3, 7, redstoneWireNS)
	put(5, -3, 6, redstoneWireNS)
	put(5, -3, 5, redstoneWireNS)
	put(5, -3, 4, redstoneWireNS)
	put(5, -3, 3, redstoneWireNS)
	put(5, -3, 2, redstoneWireNS)
	put(5, -3, 1, prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropNorth, "side"), registry.PropWest, "side"))
	put(4, -3, 1, prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropEast, "side"), registry.PropWest, "side"))
	put(3, -3, 1, registry.BlockMossyCobblestone.DefaultState())
	if !p.placedTrap1 {
		p.placedTrap1 = dispenser(3, -2, 1, 0)
	}
	put(3, -2, 2, prop(registry.BlockVine.DefaultState(), registry.PropSouth, "true"))
	put(7, -3, 1, prop(prop(registry.BlockTripwireHook.DefaultState(), registry.PropFacing, "north"), registry.PropAttached, "true"))
	put(7, -3, 5, prop(prop(registry.BlockTripwireHook.DefaultState(), registry.PropFacing, "south"), registry.PropAttached, "true"))
	put(7, -3, 2, prop(prop(prop(registry.BlockTripwire.DefaultState(), registry.PropNorth, "true"), registry.PropSouth, "true"), registry.PropAttached, "true"))
	put(7, -3, 3, prop(prop(prop(registry.BlockTripwire.DefaultState(), registry.PropNorth, "true"), registry.PropSouth, "true"), registry.PropAttached, "true"))
	put(7, -3, 4, prop(prop(prop(registry.BlockTripwire.DefaultState(), registry.PropNorth, "true"), registry.PropSouth, "true"), registry.PropAttached, "true"))
	put(8, -3, 6, prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropEast, "side"), registry.PropWest, "side"))
	put(9, -3, 6, prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropWest, "side"), registry.PropSouth, "side"))
	put(9, -3, 5, prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropNorth, "side"), registry.PropSouth, "up"))
	put(9, -3, 4, registry.BlockMossyCobblestone.DefaultState())
	put(9, -2, 4, redstoneWireNS)
	if !p.placedTrap2 {
		p.placedTrap2 = dispenser(9, -2, 3, 3)
	}
	put(8, -1, 3, prop(registry.BlockVine.DefaultState(), registry.PropEast, "true"))
	put(8, -2, 3, prop(registry.BlockVine.DefaultState(), registry.PropEast, "true"))
	if !p.placedMainChest {
		p.placedMainChest = chest(8, -3, 3)
	}
	put(9, -3, 2, registry.BlockMossyCobblestone.DefaultState())
	put(8, -3, 1, registry.BlockMossyCobblestone.DefaultState())
	put(4, -3, 5, registry.BlockMossyCobblestone.DefaultState())
	put(5, -2, 5, registry.BlockMossyCobblestone.DefaultState())
	put(5, -1, 5, registry.BlockMossyCobblestone.DefaultState())
	put(6, -3, 5, registry.BlockMossyCobblestone.DefaultState())
	put(7, -2, 5, registry.BlockMossyCobblestone.DefaultState())
	put(7, -1, 5, registry.BlockMossyCobblestone.DefaultState())
	put(8, -3, 5, registry.BlockMossyCobblestone.DefaultState())
	stone(9, -1, 1, 9, -1, 5)
	air(8, -3, 8, 10, -1, 10)
	put(8, -2, 11, registry.BlockChiseledStoneBricks.DefaultState())
	put(9, -2, 11, registry.BlockChiseledStoneBricks.DefaultState())
	put(10, -2, 11, registry.BlockChiseledStoneBricks.DefaultState())
	lever := prop(prop(registry.BlockLever.DefaultState(), registry.PropFacing, "north"), faceProp, "wall")
	put(8, -2, 12, lever)
	put(9, -2, 12, lever)
	put(10, -2, 12, lever)
	stone(8, -3, 8, 8, -3, 10)
	stone(10, -3, 8, 10, -3, 10)
	put(10, -2, 9, registry.BlockMossyCobblestone.DefaultState())
	put(8, -2, 9, redstoneWireNS)
	put(8, -2, 10, redstoneWireNS)
	put(10, -1, 9, prop(prop(prop(prop(registry.BlockRedstoneWire.DefaultState(), registry.PropNorth, "side"), registry.PropSouth, "side"), registry.PropEast, "side"), registry.PropWest, "side"))
	put(9, -2, 8, prop(registry.BlockStickyPiston.DefaultState(), registry.PropFacing, "up"))
	put(10, -2, 8, prop(registry.BlockStickyPiston.DefaultState(), registry.PropFacing, "west"))
	put(10, -1, 8, prop(registry.BlockStickyPiston.DefaultState(), registry.PropFacing, "west"))
	put(10, -2, 10, prop(registry.BlockRepeater.DefaultState(), registry.PropFacing, "north"))
	if !p.placedHiddenChest {
		p.placedHiddenChest = chest(9, -3, 10)
	}
}
