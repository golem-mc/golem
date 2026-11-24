package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Piece-local directions, in the order of fenceDirNames.
const (
	shN = iota
	shE
	shS
	shW
)

// shDirOfOrientation maps a saved orientation (Direction.get2DDataValue) to a
// piece-local direction.
var shDirOfOrientation = [4]int{shS, shW, shN, shE}

const (
	shCorridorLoot = "minecraft:chests/stronghold_corridor"
	shCrossingLoot = "minecraft:chests/stronghold_crossing"
	shLibraryLoot  = "minecraft:chests/stronghold_library"
)

// shStates holds the block states the stronghold places; the directional ones
// are indexed by the world direction they face.
type shStates struct {
	bricks, cracked, mossy, infested, caveAir, water, lava, cobble, planks, bookshelf, cobweb uint16
	torch, smoothSlab, smoothDouble, brickSlab, portal, spawner                               uint16
	wallTorch, cobbleStairs, brickStairs, button, ladder, frame, frameEye, chest              [4]uint16
	bars, fence                                                                               [16]uint16
	// door is indexed by iron, facing, hinge (right), upper half.
	door [2][4][2][2]uint16
}

var shStatesOnce = sync.OnceValue(func() *shStates {
	s := &shStates{}
	find := func(name string, props ...string) uint16 {
		o := map[string]string{}
		for i := 0; i < len(props); i += 2 {
			o[props[i]] = props[i+1]
		}
		st, ok := registry.FindState(name, o)
		if !ok {
			panic("the block registry has no " + name)
		}
		return st
	}
	s.bricks = find("minecraft:stone_bricks")
	s.cracked = find("minecraft:cracked_stone_bricks")
	s.mossy = find("minecraft:mossy_stone_bricks")
	s.infested = find("minecraft:infested_stone_bricks")
	s.caveAir = find("minecraft:cave_air")
	s.water = find("minecraft:water")
	s.lava = find("minecraft:lava")
	s.cobble = find("minecraft:cobblestone")
	s.planks = find("minecraft:oak_planks")
	s.bookshelf = find("minecraft:bookshelf")
	s.cobweb = find("minecraft:cobweb")
	s.torch = find("minecraft:torch")
	s.smoothSlab = find("minecraft:smooth_stone_slab")
	s.smoothDouble = find("minecraft:smooth_stone_slab", "type", "double")
	s.brickSlab = find("minecraft:stone_brick_slab")
	s.portal = find("minecraft:end_portal")
	s.spawner = find("minecraft:spawner")
	for d, name := range fenceDirNames {
		s.wallTorch[d] = find("minecraft:wall_torch", "facing", name)
		s.cobbleStairs[d] = find("minecraft:cobblestone_stairs", "facing", name)
		s.brickStairs[d] = find("minecraft:stone_brick_stairs", "facing", name)
		s.button[d] = find("minecraft:stone_button", "facing", name)
		s.ladder[d] = find("minecraft:ladder", "facing", name)
		s.frame[d] = find("minecraft:end_portal_frame", "facing", name)
		s.frameEye[d] = find("minecraft:end_portal_frame", "facing", name, "eye", "true")
		s.chest[d] = find("minecraft:chest", "facing", name)
		for iron, door := range [2]string{"minecraft:oak_door", "minecraft:iron_door"} {
			for hinge, h := range [2]string{"left", "right"} {
				for upper, half := range [2]string{"lower", "upper"} {
					s.door[iron][d][hinge][upper] = find(door, "facing", name, "hinge", h, "half", half)
				}
			}
		}
	}
	for mask := range s.bars {
		bars := []string{"minecraft:iron_bars"}
		fence := []string{"minecraft:oak_fence"}
		var props []string
		for d, name := range fenceDirNames {
			v := "false"
			if mask&(1<<d) != 0 {
				v = "true"
			}
			props = append(props, name, v)
		}
		s.bars[mask] = find(bars[0], props...)
		s.fence[mask] = find(fence[0], props...)
	}
	return s
})

// strongholdPlacer places one piece into a chunk. vanilla: StructurePiece
type strongholdPlacer struct {
	r     *decorRegion
	st    *shStates
	frame levelgen.PieceFrame
	dir   int
	chunk jbox
	rng   levelgen.RandomSource
}

func (p *strongholdPlacer) mapDir(d int) int { return p.frame.MapDir(d) }

func (p *strongholdPlacer) mapMask(mask int) int {
	var m int
	for d := range 4 {
		if mask&(1<<d) != 0 {
			m |= 1 << p.mapDir(d)
		}
	}
	return m
}

func (p *strongholdPlacer) worldPos(x, y, z int32) (int32, int32, int32) {
	wx, wy, wz := p.frame.WorldPos(int(x), int(y), int(z))
	return int32(wx), int32(wy), int32(wz)
}

// localState applies the piece's mirror and rotation to an untransformed state.
// vanilla: StructurePiece.placeBlock
func (p *strongholdPlacer) localState(state uint16) uint16 {
	if p.dir == shS || p.dir == shW {
		state = MirrorState(state, MirrorLeftRight)
	}
	if p.dir == shE || p.dir == shW {
		state = RotateState(state, RotateClockwise90)
	}
	return state
}

// put is vanilla: StructurePiece.placeBlock.
func (p *strongholdPlacer) put(x, y, z int32, state uint16) {
	wx, wy, wz := p.worldPos(x, y, z)
	if !p.chunk.inside(wx, wy, wz) {
		return
	}
	r := p.r
	r.set(wx, wy, wz, state)
	if state == p.st.water || state == p.st.lava {
		r.scheduleFluidTick(wx, wy, wz)
	}
	r.markShape(wx, wy, wz)
}

func (p *strongholdPlacer) facing(x, y, z int32, table *[4]uint16, local int) {
	p.put(x, y, z, table[p.mapDir(local)])
}

func (p *strongholdPlacer) mask(x, y, z int32, table *[16]uint16, mask int) {
	p.put(x, y, z, table[p.mapMask(mask)])
}

// door places both halves of a door at the lower block; the hinge swaps on a
// mirrored piece. vanilla: DoorBlock.mirror
func (p *strongholdPlacer) door(x, y, z int32, iron int, local int) {
	hinge := 0
	if p.dir == shS || p.dir == shW {
		hinge = 1
	}
	d := p.mapDir(local)
	p.put(x, y, z, p.st.door[iron][d][hinge][0])
	p.put(x, y+1, z, p.st.door[iron][d][hinge][1])
}

// isAir is vanilla: StructurePiece.getBlock(...).isAir(), true outside the chunk.
func (p *strongholdPlacer) isAir(x, y, z int32) bool {
	wx, wy, wz := p.worldPos(x, y, z)
	return !p.chunk.inside(wx, wy, wz) || p.r.isAir(wx, wy, wz)
}

// box is vanilla: StructurePiece.generateBox.
func (p *strongholdPlacer) box(x0, y0, z0, x1, y1, z1 int32, edge, fill uint16) {
	levelgen.GenerateBox(int(x0), int(y0), int(z0), int(x1), int(y1), int(z1), func(x, y, z int, shell bool) {
		if shell {
			p.put(int32(x), int32(y), int32(z), edge)
		} else {
			p.put(int32(x), int32(y), int32(z), fill)
		}
	})
}

// stoneBox is generateBox with the smooth stone selector: random stone brick
// variants on the shell, cave air inside. vanilla: StrongholdPieces.SmoothStoneSelector
func (p *strongholdPlacer) stoneBox(x0, y0, z0, x1, y1, z1 int32, skipAir bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && p.isAir(x, y, z) {
					continue
				}
				if y != y0 && y != y1 && x != x0 && x != x1 && z != z0 && z != z1 {
					p.put(x, y, z, p.st.caveAir)
					continue
				}
				f := p.rng.NextFloat()
				switch {
				case f < 0.2:
					p.put(x, y, z, p.st.cracked)
				case f < 0.5:
					p.put(x, y, z, p.st.mossy)
				case f < 0.55:
					p.put(x, y, z, p.st.infested)
				default:
					p.put(x, y, z, p.st.bricks)
				}
			}
		}
	}
}

func (p *strongholdPlacer) air(x0, y0, z0, x1, y1, z1 int32) {
	p.box(x0, y0, z0, x1, y1, z1, p.st.caveAir, p.st.caveAir)
}

func (p *strongholdPlacer) solid(x0, y0, z0, x1, y1, z1 int32, state uint16) {
	p.box(x0, y0, z0, x1, y1, z1, state, state)
}

// fillColumnDown is vanilla: StructurePiece.fillColumnDown.
func (p *strongholdPlacer) fillColumnDown(state uint16, x, startY, z int32) {
	var y int32
	x, y, z = p.worldPos(x, startY, z)
	if !p.chunk.inside(x, y, z) {
		return
	}
	for y > p.r.minY+1 {
		current := p.r.get(x, y, z)
		if !structureColumnReplaceable[registry.BlockOf(current)] && !p.r.src.isAir(current) {
			return
		}
		// Vanilla writes foundation columns directly, without scheduling fluids.
		p.r.set(x, y, z, state)
		y--
	}
}

// maybe is vanilla: StructurePiece.maybeGenerateBlock.
func (p *strongholdPlacer) maybe(prob float32, x, y, z int32, state uint16) {
	if p.rng.NextFloat() < prob {
		p.put(x, y, z, state)
	}
}

// smallDoor is vanilla: StrongholdPiece.generateSmallDoor.
func (p *strongholdPlacer) smallDoor(kind string, x, y, z int32) {
	s := p.st
	frame := func() {
		for _, o := range [][2]int32{{0, 0}, {0, 1}, {0, 2}, {1, 2}, {2, 2}, {2, 1}, {2, 0}} {
			p.put(x+o[0], y+o[1], z, s.bricks)
		}
	}
	switch kind {
	case "WOOD_DOOR":
		frame()
		p.door(x+1, y, z, 0, shN)
	case "GRATES":
		p.put(x+1, y, z, s.caveAir)
		p.put(x+1, y+1, z, s.caveAir)
		p.mask(x, y, z, &s.bars, 1<<shW)
		p.mask(x, y+1, z, &s.bars, 1<<shW)
		p.mask(x, y+2, z, &s.bars, 1<<shE|1<<shW)
		p.mask(x+1, y+2, z, &s.bars, 1<<shE|1<<shW)
		p.mask(x+2, y+2, z, &s.bars, 1<<shE|1<<shW)
		p.mask(x+2, y+1, z, &s.bars, 1<<shE)
		p.mask(x+2, y, z, &s.bars, 1<<shE)
	case "IRON_DOOR":
		frame()
		p.door(x+1, y, z, 1, shN)
		p.facing(x+2, y+1, z+1, &s.button, shN)
		p.facing(x+2, y+1, z-1, &s.button, shS)
	default:
		p.air(x, y, z, x+2, y+2, z)
	}
}

// chestAt is vanilla: StructurePiece.createChest.
func (p *strongholdPlacer) chestAt(x, y, z int32, loot string) {
	p.frame.CreateChest(&strongholdChests{r: p.r, st: p.st, loot: loot}, toBoundingBox(p.chunk), int(x), int(y), int(z))
}

func toBoundingBox(b jbox) levelgen.BoundingBox {
	return levelgen.BoundingBox{MinX: int(b.minX), MinY: int(b.minY), MinZ: int(b.minZ), MaxX: int(b.maxX), MaxY: int(b.maxY), MaxZ: int(b.maxZ)}
}

// strongholdChests is the region as a chest is placed into it.
type strongholdChests struct {
	r    *decorRegion
	st   *shStates
	loot string
}

func (c *strongholdChests) Block(x, y, z int) uint16 { return c.r.get(int32(x), int32(y), int32(z)) }

func (c *strongholdChests) SolidRender(state uint16) bool { return registry.IsSolidRender(state) }

func (c *strongholdChests) IsChest(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockOf(c.st.chest[0])
}

func (c *strongholdChests) PlaceChest(x, y, z, facing int) {
	state := c.st.chest[facing]
	c.r.set(int32(x), int32(y), int32(z), state)
	c.r.placeBlockEntity(int32(x), int32(y), int32(z), state, nbt.Compound{
		"id":        nbt.String("minecraft:chest"),
		"LootTable": nbt.String(c.loot),
	})
}

// place writes the piece's blocks inside chunk. vanilla: StrongholdPieces.*.postProcess
func (p *strongholdPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	pl := &strongholdPlacer{r: r, st: shStatesOnce(), frame: levelgen.PieceFrame{BB: toBoundingBox(p.bounds()), Dir: shDirOfOrientation[p.saved.Orientation&3]}, dir: shDirOfOrientation[p.saved.Orientation&3], chunk: chunk, rng: r.rng}
	door := p.saved.EntryDoor
	switch p.saved.ID {
	case "minecraft:shs":
		pl.straight(door, p.tag("Left"), p.tag("Right"))
	case "minecraft:shph":
		pl.prisonHall(door)
	case "minecraft:shlt":
		pl.turn(door, pl.dir == shN || pl.dir == shE)
	case "minecraft:shrt":
		pl.turn(door, pl.dir != shN && pl.dir != shE)
	case "minecraft:shrc":
		pl.roomCrossing(door, p.tag("Type"))
	case "minecraft:shssd":
		pl.straightStairsDown(door)
	case "minecraft:shsd", "minecraft:shstart":
		pl.stairsDown(door)
	case "minecraft:sh5c":
		pl.fiveCrossing(door, p.tag("leftLow") != 0, p.tag("leftHigh") != 0, p.tag("rightLow") != 0, p.tag("rightHigh") != 0)
	case "minecraft:shcc":
		pl.chestCorridor(door)
	case "minecraft:shli":
		pl.library(door, p.tag("Tall") != 0)
	case "minecraft:shpr":
		pl.portalRoom()
	case "minecraft:shfc":
		pl.filler(p.tag("Steps"))
	}
}

// tag is a saved field of the piece, 0 when it has none.
func (p *strongholdPiece) tag(name string) int {
	for _, t := range p.saved.Tags {
		if t.Name == name {
			return t.Value
		}
	}
	return 0
}

// nbtSilverfishSpawner is the portal room's spawner as a fresh block entity
// holds it. vanilla: BaseSpawner.setEntityId
func nbtSilverfishSpawner() nbt.Compound {
	return freshSpawnerTag("minecraft:silverfish")
}
