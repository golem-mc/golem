package world

import (
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The overworld's mineshafts: a room that grows corridors, crossings and
// stairs outwards, placed underground below sea level, or around the surface
// in badlands. vanilla: MineshaftStructure, MineshaftPieces

func init() {
	overworldStructureTypes["minecraft:mineshaft"] = mineshaftStructure{}
	overworldStructureTypes["minecraft:mineshaft_mesa"] = mineshaftStructure{mesa: true}
}

// Piece directions, in Direction.Plane.HORIZONTAL order.
const (
	msNorth = iota
	msEast
	msSouth
	msWest
)

// msData2D is Direction.get2DDataValue by direction.
var msData2D = [4]int32{2, 3, 0, 1}

const (
	msMaxDepth    = 8
	msMaxDistance = 80
	msPillarReach = 20
	msChainReach  = 50
	msRoomStartY  = 50
	msSeaOffset   = 10
)

type mineshaftKind uint8

const (
	msRoom mineshaftKind = iota
	msCorridor
	msCrossing
	msStairs
)

var msPieceIDs = [...]string{msRoom: "minecraft:msroom", msCorridor: "minecraft:mscorridor", msCrossing: "minecraft:mscrossing", msStairs: "minecraft:msstairs"}

// mineshaftStates are the block states a mineshaft places, per wood type
// (normal, mesa).
type mineshaftStates struct {
	air, cobweb, chain, rail, spawner uint16
	planks, wood, fence               [2]uint16
	planksBlock, woodBlock            [2]registry.Block
	fenceBlock                        [2]registry.Block
	// fenceWest and fenceEast are the fence with its west or east side
	// connected, once mirrored and rotated for a piece's direction.
	fenceWest, fenceEast [2][4]uint16
	// torchSouth and torchNorth are the wall torch facing south or north
	// before the piece's mirror and rotation; railNS and railEW likewise.
	torchSouth, torchNorth [4]uint16
	railNS, railEW         [4]uint16
}

var mineshaftBlocks = sync.OnceValue(func() *mineshaftStates {
	find := func(name string, props map[string]string) uint16 {
		state, ok := registry.FindState(name, props)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	s := &mineshaftStates{
		air:     find("minecraft:cave_air", nil),
		cobweb:  find("minecraft:cobweb", nil),
		chain:   find("minecraft:iron_chain", nil),
		rail:    find("minecraft:rail", map[string]string{"shape": "north_south"}),
		spawner: find("minecraft:spawner", nil),
	}
	for t, wood := range [2]string{"oak", "dark_oak"} {
		s.planks[t] = find("minecraft:"+wood+"_planks", nil)
		s.wood[t] = find("minecraft:"+wood+"_log", nil)
		s.fence[t] = find("minecraft:"+wood+"_fence", nil)
		s.planksBlock[t] = registry.BlockOf(s.planks[t])
		s.woodBlock[t] = registry.BlockOf(s.wood[t])
		s.fenceBlock[t] = registry.BlockOf(s.fence[t])
		// Mirror.LEFT_RIGHT swaps north and south; Rotation.CLOCKWISE_90 turns
		// west into north and east into south.
		for d := range 4 {
			west, east := registry.PropWest, registry.PropEast
			if d == msWest || d == msEast {
				west, east = registry.PropNorth, registry.PropSouth
			}
			s.fenceWest[t][d], _ = registry.WithBool(s.fence[t], west, true)
			s.fenceEast[t][d], _ = registry.WithBool(s.fence[t], east, true)
		}
	}
	torch := func(facing string) uint16 {
		return find("minecraft:wall_torch", map[string]string{"facing": facing})
	}
	rail := func(shape string) uint16 { return find("minecraft:rail", map[string]string{"shape": shape}) }
	// By piece direction: south and west mirror, west and east rotate.
	torchS := [4]string{"south", "west", "north", "east"}
	torchN := [4]string{"north", "east", "south", "west"}
	for d := range 4 {
		s.torchSouth[d] = torch(torchS[d])
		s.torchNorth[d] = torch(torchN[d])
		if d == msEast || d == msWest {
			s.railNS[d], s.railEW[d] = rail("east_west"), rail("north_south")
		} else {
			s.railNS[d], s.railEW[d] = rail("north_south"), rail("east_west")
		}
	}
	return s
})

var (
	mineshaftLiquids = blocksNamed("minecraft:water", "minecraft:lava")
	// mineshaftFalling is FallingBlock: sand, gravel, concrete powder, anvils
	// and the dragon egg.
	mineshaftFalling = registry.BlocksOf(func(b registry.Block) bool {
		switch b.Type() {
		case registry.BlockTypeSand, registry.BlockTypeColoredFalling, registry.BlockTypeConcretePowder, registry.BlockTypeAnvil, registry.BlockTypeDragonEgg:
			return true
		}
		return false
	})
	mineshaftReplaceable = blocksNamed("minecraft:glow_lichen", "minecraft:seagrass", "minecraft:tall_seagrass")
)

// mineshaftPiece is one piece of a mineshaft. vanilla: MineshaftPieces.MineShaftPiece
type mineshaftPiece struct {
	kind  mineshaftKind
	box   jbox
	depth int32
	// dir is the piece's direction; hasDir is false for a room and a
	// crossing, whose blocks are addressed in world coordinates.
	dir    int
	hasDir bool
	mesa   bool
	// corridor
	hasRails, spiderCorridor bool
	sections                 int32
	placedSpider             atomic.Bool
	// crossing
	twoFloored bool
	// room: the child entrances, as boxes.
	entrances []jbox
}

func (p *mineshaftPiece) bounds() jbox { return p.box }

func (p *mineshaftPiece) typeIndex() int {
	if p.mesa {
		return 1
	}
	return 0
}

func (p *mineshaftPiece) nbt() nbt.Compound {
	o := int32(-1)
	if p.hasDir {
		o = msData2D[p.dir]
	}
	c := nbt.Compound{
		"id":  nbt.String(msPieceIDs[p.kind]),
		"BB":  nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"O":   nbt.Int(o),
		"GD":  nbt.Int(p.depth),
		"MST": nbt.Int(p.typeIndex()),
	}
	switch p.kind {
	case msCrossing:
		c["tf"] = nbtBool(p.twoFloored)
		c["D"] = nbt.Byte(msData2D[p.dir])
	case msCorridor:
		c["hr"] = nbtBool(p.hasRails)
		c["sc"] = nbtBool(p.spiderCorridor)
		c["hps"] = nbtBool(p.placedSpider.Load())
		c["Num"] = nbt.Int(p.sections)
	case msRoom:
		items := make([]nbt.Tag, len(p.entrances))
		for i, b := range p.entrances {
			items[i] = nbt.IntArray{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ}
		}
		c["Entrances"] = nbt.List{Elem: nbt.TypeIntArray, Items: items}
	}
	return c
}

func nbtBool(b bool) nbt.Byte {
	if b {
		return 1
	}
	return 0
}

// place writes the piece's blocks inside the chunk.
func (p *mineshaftPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	w := &mineshaftWriter{r: r, chunk: chunk, p: p, st: mineshaftBlocks(), rng: r.rng}
	w.g = r.src.(*OverworldGenerator)
	if w.invalidLocation() {
		return
	}
	switch p.kind {
	case msRoom:
		w.postRoom()
	case msCorridor:
		w.postCorridor()
	case msCrossing:
		w.postCrossing()
	case msStairs:
		w.postStairs()
	}
}

// mineshaftStructure grows the normal or the badlands mineshaft.
// vanilla: MineshaftStructure
type mineshaftStructure struct{ mesa bool }

func (m mineshaftStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	rng := ctx.rng
	rng.NextDouble()
	gen := &mineshaftGen{rng: rng, mesa: m.mesa}
	room := &mineshaftPiece{kind: msRoom, mesa: m.mesa}
	west, north := ctx.chunkX*16+2, ctx.chunkZ*16+2
	// Arguments are drawn in order: x, y, z extent.
	maxX := west + 7 + rng.NextIntN(6)
	maxY := int32(54) + rng.NextIntN(6)
	maxZ := north + 7 + rng.NextIntN(6)
	room.box = jbox{west, msRoomStartY, north, maxX, maxY, maxZ}
	gen.add(room)
	gen.children(room, room)
	var dy int32
	if m.mesa {
		b := gen.bounds()
		cx, cy, cz := b.minX+(b.maxX-b.minX+1)/2, b.minY+(b.maxY-b.minY+1)/2, b.minZ+(b.maxZ-b.minZ+1)/2
		// vanilla: ChunkGenerator.getBaseHeight, WORLD_SURFACE_WG
		seaLevel := ctx.g.SeaLevel()
		surface := ctx.g.height(cx, cz) + 1
		target := seaLevel
		if surface > seaLevel {
			target = rng.NextIntN(surface-seaLevel+1) + seaLevel
		}
		dy = target - cy
		gen.offset(dy)
	} else {
		// vanilla: StructurePiecesBuilder.moveBelowSeaLevel
		b := gen.bounds()
		maxY := ctx.g.SeaLevel() - msSeaOffset
		y1 := b.maxY - b.minY + 1 + ctx.g.minY() + 1
		if y1 < maxY {
			y1 += rng.NextIntN(maxY - y1)
		}
		dy = y1 - b.maxY
		gen.offset(dy)
	}
	pos := [3]int32{ctx.chunkX*16 + 8, msRoomStartY + dy, ctx.chunkZ * 16}
	return pos, func() []structurePiece {
		out := make([]structurePiece, len(gen.pieces))
		for i, p := range gen.pieces {
			out[i] = p
		}
		return out
	}, true
}

// mineshaftGen grows a mineshaft's pieces. vanilla: StructurePiecesBuilder
type mineshaftGen struct {
	rng    levelgen.RandomSource
	mesa   bool
	pieces []*mineshaftPiece
}

func (g *mineshaftGen) add(p *mineshaftPiece) { g.pieces = append(g.pieces, p) }

func (g *mineshaftGen) bounds() jbox {
	b := g.pieces[0].box
	for _, p := range g.pieces[1:] {
		b = b.union(p.box)
	}
	return b
}

func (g *mineshaftGen) offset(dy int32) {
	for _, p := range g.pieces {
		p.box = p.box.moved(0, dy, 0)
		for i := range p.entrances {
			p.entrances[i] = p.entrances[i].moved(0, dy, 0)
		}
	}
}

func (g *mineshaftGen) collides(b jbox) bool {
	for _, p := range g.pieces {
		if p.box.intersects(b) {
			return true
		}
	}
	return false
}

// shaft grows the piece at a foot position, or nil.
// vanilla: MineshaftPieces.generateAndAddPiece
func (g *mineshaftGen) shaft(start *mineshaftPiece, x, y, z int32, dir int, depth int32) *mineshaftPiece {
	if depth > msMaxDepth || absInt32(x-start.box.minX) > msMaxDistance || absInt32(z-start.box.minZ) > msMaxDistance {
		return nil
	}
	p := g.randomShaft(x, y, z, dir, depth+1)
	if p != nil {
		g.add(p)
		g.children(start, p)
	}
	return p
}

// randomShaft is vanilla's createRandomShaftPiece.
func (g *mineshaftGen) randomShaft(x, y, z int32, dir int, depth int32) *mineshaftPiece {
	rng := g.rng
	sel := rng.NextIntN(100)
	switch {
	case sel >= 80:
		y1 := int32(2)
		if rng.NextIntN(4) == 0 {
			y1 = 6
		}
		box := [4]jbox{{-1, 0, -4, 3, y1, 0}, {0, 0, -1, 4, y1, 3}, {-1, 0, 0, 3, y1, 4}, {-4, 0, -1, 0, y1, 3}}[dir].moved(x, y, z)
		if g.collides(box) {
			return nil
		}
		return &mineshaftPiece{kind: msCrossing, box: box, depth: depth, dir: dir, mesa: g.mesa, twoFloored: box.maxY-box.minY+1 > 3}
	case sel >= 70:
		box := [4]jbox{{0, -5, -8, 2, 2, 0}, {0, -5, 0, 8, 2, 2}, {0, -5, 0, 2, 2, 8}, {-8, -5, 0, 0, 2, 2}}[dir].moved(x, y, z)
		if g.collides(box) {
			return nil
		}
		return &mineshaftPiece{kind: msStairs, box: box, depth: depth, dir: dir, hasDir: true, mesa: g.mesa}
	}
	for length := rng.NextIntN(3) + 2; length > 0; length-- {
		n := length * 5
		box := [4]jbox{{0, 0, -(n - 1), 2, 2, 0}, {0, 0, 0, n - 1, 2, 2}, {0, 0, 0, 2, 2, n - 1}, {-(n - 1), 0, 0, 0, 2, 2}}[dir].moved(x, y, z)
		if g.collides(box) {
			continue
		}
		p := &mineshaftPiece{kind: msCorridor, box: box, depth: depth, dir: dir, hasDir: true, mesa: g.mesa}
		p.hasRails = rng.NextIntN(3) == 0
		p.spiderCorridor = !p.hasRails && rng.NextIntN(23) == 0
		if dir == msNorth || dir == msSouth {
			p.sections = (box.maxZ - box.minZ + 1) / 5
		} else {
			p.sections = (box.maxX - box.minX + 1) / 5
		}
		return p
	}
	return nil
}

// children grows the pieces that leave p. vanilla: MineShaftPiece.addChildren
func (g *mineshaftGen) children(start, p *mineshaftPiece) {
	switch p.kind {
	case msRoom:
		g.roomChildren(start, p)
	case msCorridor:
		g.corridorChildren(start, p)
	case msCrossing:
		g.crossingChildren(start, p)
	case msStairs:
		b := p.box
		switch p.dir {
		case msNorth:
			g.shaft(start, b.minX, b.minY, b.minZ-1, msNorth, p.depth)
		case msSouth:
			g.shaft(start, b.minX, b.minY, b.maxZ+1, msSouth, p.depth)
		case msWest:
			g.shaft(start, b.minX-1, b.minY, b.minZ, msWest, p.depth)
		case msEast:
			g.shaft(start, b.maxX+1, b.minY, b.minZ, msEast, p.depth)
		}
	}
}

func (g *mineshaftGen) roomChildren(start, p *mineshaftPiece) {
	b := p.box
	rng := g.rng
	heightSpace := b.maxY - b.minY + 1 - 3 - 1
	if heightSpace <= 0 {
		heightSpace = 1
	}
	xs, zs := b.maxX-b.minX+1, b.maxZ-b.minZ+1
	for pos := int32(0); pos < xs; pos += 4 {
		pos += rng.NextIntN(xs)
		if pos+3 > xs {
			break
		}
		if c := g.shaft(start, b.minX+pos, b.minY+rng.NextIntN(heightSpace)+1, b.minZ-1, msNorth, p.depth); c != nil {
			p.entrances = append(p.entrances, jbox{c.box.minX, c.box.minY, b.minZ, c.box.maxX, c.box.maxY, b.minZ + 1})
		}
	}
	for pos := int32(0); pos < xs; pos += 4 {
		pos += rng.NextIntN(xs)
		if pos+3 > xs {
			break
		}
		if c := g.shaft(start, b.minX+pos, b.minY+rng.NextIntN(heightSpace)+1, b.maxZ+1, msSouth, p.depth); c != nil {
			p.entrances = append(p.entrances, jbox{c.box.minX, c.box.minY, b.maxZ - 1, c.box.maxX, c.box.maxY, b.maxZ})
		}
	}
	for pos := int32(0); pos < zs; pos += 4 {
		pos += rng.NextIntN(zs)
		if pos+3 > zs {
			break
		}
		if c := g.shaft(start, b.minX-1, b.minY+rng.NextIntN(heightSpace)+1, b.minZ+pos, msWest, p.depth); c != nil {
			p.entrances = append(p.entrances, jbox{b.minX, c.box.minY, c.box.minZ, b.minX + 1, c.box.maxY, c.box.maxZ})
		}
	}
	for pos := int32(0); pos < zs; pos += 4 {
		pos += rng.NextIntN(zs)
		if pos+3 > zs {
			break
		}
		if c := g.shaft(start, b.maxX+1, b.minY+rng.NextIntN(heightSpace)+1, b.minZ+pos, msEast, p.depth); c != nil {
			p.entrances = append(p.entrances, jbox{b.maxX - 1, c.box.minY, c.box.minZ, b.maxX, c.box.maxY, c.box.maxZ})
		}
	}
}

func (g *mineshaftGen) crossingChildren(start, p *mineshaftPiece) {
	b, d := p.box, p.depth
	switch p.dir {
	case msSouth:
		g.shaft(start, b.minX+1, b.minY, b.maxZ+1, msSouth, d)
		g.shaft(start, b.minX-1, b.minY, b.minZ+1, msWest, d)
		g.shaft(start, b.maxX+1, b.minY, b.minZ+1, msEast, d)
	case msWest:
		g.shaft(start, b.minX+1, b.minY, b.minZ-1, msNorth, d)
		g.shaft(start, b.minX+1, b.minY, b.maxZ+1, msSouth, d)
		g.shaft(start, b.minX-1, b.minY, b.minZ+1, msWest, d)
	case msEast:
		g.shaft(start, b.minX+1, b.minY, b.minZ-1, msNorth, d)
		g.shaft(start, b.minX+1, b.minY, b.maxZ+1, msSouth, d)
		g.shaft(start, b.maxX+1, b.minY, b.minZ+1, msEast, d)
	default:
		g.shaft(start, b.minX+1, b.minY, b.minZ-1, msNorth, d)
		g.shaft(start, b.minX-1, b.minY, b.minZ+1, msWest, d)
		g.shaft(start, b.maxX+1, b.minY, b.minZ+1, msEast, d)
	}
	if !p.twoFloored {
		return
	}
	y := b.minY + 3 + 1
	if g.rng.NextBoolean() {
		g.shaft(start, b.minX+1, y, b.minZ-1, msNorth, d)
	}
	if g.rng.NextBoolean() {
		g.shaft(start, b.minX-1, y, b.minZ+1, msWest, d)
	}
	if g.rng.NextBoolean() {
		g.shaft(start, b.maxX+1, y, b.minZ+1, msEast, d)
	}
	if g.rng.NextBoolean() {
		g.shaft(start, b.minX+1, y, b.maxZ+1, msSouth, d)
	}
}

func (g *mineshaftGen) corridorChildren(start, p *mineshaftPiece) {
	b, d, rng := p.box, p.depth, g.rng
	sel := rng.NextIntN(4)
	// y is drawn when the call is made, after the end selection.
	y := func() int32 { return b.minY - 1 + rng.NextIntN(3) }
	switch p.dir {
	case msSouth:
		switch {
		case sel <= 1:
			g.shaft(start, b.minX, y(), b.maxZ+1, msSouth, d)
		case sel == 2:
			g.shaft(start, b.minX-1, y(), b.maxZ-3, msWest, d)
		default:
			g.shaft(start, b.maxX+1, y(), b.maxZ-3, msEast, d)
		}
	case msWest:
		switch {
		case sel <= 1:
			g.shaft(start, b.minX-1, y(), b.minZ, msWest, d)
		case sel == 2:
			g.shaft(start, b.minX, y(), b.minZ-1, msNorth, d)
		default:
			g.shaft(start, b.minX, y(), b.maxZ+1, msSouth, d)
		}
	case msEast:
		switch {
		case sel <= 1:
			g.shaft(start, b.maxX+1, y(), b.minZ, msEast, d)
		case sel == 2:
			g.shaft(start, b.maxX-3, y(), b.minZ-1, msNorth, d)
		default:
			g.shaft(start, b.maxX-3, y(), b.maxZ+1, msSouth, d)
		}
	default:
		switch {
		case sel <= 1:
			g.shaft(start, b.minX, y(), b.minZ-1, msNorth, d)
		case sel == 2:
			g.shaft(start, b.minX-1, y(), b.minZ, msWest, d)
		default:
			g.shaft(start, b.maxX+1, y(), b.minZ, msEast, d)
		}
	}
	if d >= msMaxDepth {
		return
	}
	if p.dir == msNorth || p.dir == msSouth {
		for z := b.minZ + 3; z+3 <= b.maxZ; z += 5 {
			switch rng.NextIntN(5) {
			case 0:
				g.shaft(start, b.minX-1, b.minY, z, msWest, d+1)
			case 1:
				g.shaft(start, b.maxX+1, b.minY, z, msEast, d+1)
			}
		}
		return
	}
	for x := b.minX + 3; x+3 <= b.maxX; x += 5 {
		switch rng.NextIntN(5) {
		case 0:
			g.shaft(start, x, b.minY, b.minZ-1, msNorth, d+1)
		case 1:
			g.shaft(start, x, b.minY, b.maxZ+1, msSouth, d+1)
		}
	}
}

// mineshaftWriter places one piece into the chunk being decorated.
// vanilla: StructurePiece's block helpers
type mineshaftWriter struct {
	r     *decorRegion
	g     *OverworldGenerator
	chunk jbox
	p     *mineshaftPiece
	st    *mineshaftStates
	rng   levelgen.RandomSource
}

// pos is StructurePiece.getWorldPos.
func (w *mineshaftWriter) pos(x, y, z int32) (int32, int32, int32) {
	p := w.p
	if !p.hasDir {
		return x, y, z
	}
	b := p.box
	var wx, wz int32
	switch p.dir {
	case msNorth:
		wx, wz = b.minX+x, b.maxZ-z
	case msSouth:
		wx, wz = b.minX+x, b.minZ+z
	case msWest:
		wx, wz = b.maxX-z, b.minZ+x
	default:
		wx, wz = b.minX+z, b.minZ+x
	}
	return wx, y + b.minY, wz
}

// block is StructurePiece.getBlock: air outside the chunk.
func (w *mineshaftWriter) block(x, y, z int32) uint16 {
	wx, wy, wz := w.pos(x, y, z)
	if !w.chunk.inside(wx, wy, wz) {
		return airState
	}
	return w.r.get(wx, wy, wz)
}

func (w *mineshaftWriter) isAir(state uint16) bool { return w.g.isAir(state) }

// interior is StructurePiece.isInterior: the block above lies under the
// OCEAN_FLOOR_WG surface.
func (w *mineshaftWriter) interior(x, y, z int32) bool {
	wx, wy, wz := w.pos(x, y+1, z)
	if !w.chunk.inside(wx, wy, wz) {
		return false
	}
	return w.r.oceanFloorReaches(wx, wz, wy+1)
}

func (w *mineshaftWriter) canBeReplaced(x, y, z int32) bool {
	b := registry.BlockOf(w.block(x, y, z))
	t := w.p.typeIndex()
	return b != w.st.planksBlock[t] && b != w.st.woodBlock[t] && b != w.st.fenceBlock[t] && b != registry.BlockIronChain
}

// place is StructurePiece.placeBlock; the state is already mirrored and
// rotated for the piece.
func (w *mineshaftWriter) place(state uint16, x, y, z int32) {
	wx, wy, wz := w.pos(x, y, z)
	if !w.chunk.inside(wx, wy, wz) || !w.canBeReplaced(x, y, z) {
		return
	}
	w.r.set(wx, wy, wz, state)
	w.r.markShape(wx, wy, wz)
}

// box is StructurePiece.generateBox.
func (w *mineshaftWriter) box(x0, y0, z0, x1, y1, z1 int32, edge, fill uint16) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					w.place(edge, x, y, z)
				} else {
					w.place(fill, x, y, z)
				}
			}
		}
	}
}

// maybeBox is StructurePiece.generateMaybeBox; every cell draws.
func (w *mineshaftWriter) maybeBox(prob float32, x0, y0, z0, x1, y1, z1 int32, edge, fill uint16, insideOnly bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if w.rng.NextFloat() > prob || insideOnly && !w.interior(x, y, z) {
					continue
				}
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					w.place(edge, x, y, z)
				} else {
					w.place(fill, x, y, z)
				}
			}
		}
	}
}

func (w *mineshaftWriter) maybeBlock(prob float32, x, y, z int32, state uint16) {
	if w.rng.NextFloat() < prob {
		w.place(state, x, y, z)
	}
}

// setPlanks is MineShaftPiece.setPlanksBlock.
func (w *mineshaftWriter) setPlanks(x, y, z int32) {
	if !w.interior(x, y, z) {
		return
	}
	wx, wy, wz := w.pos(x, y, z)
	if !registry.FaceSturdy(w.r.get(wx, wy, wz), dirUp, registry.SupportFull) {
		w.r.set(wx, wy, wz, w.st.planks[w.p.typeIndex()])
	}
}

func (w *mineshaftWriter) liquid(x, y, z int32) bool {
	return mineshaftLiquids[registry.BlockOf(w.r.get(x, y, z))]
}

// invalidLocation is MineShaftPiece.isInInvalidLocation: a blocking biome
// at the centre of the piece's part in the chunk, or liquid on its shell.
func (w *mineshaftWriter) invalidLocation() bool {
	b, c := w.p.box, w.chunk
	x0, y0, z0 := max(b.minX-1, c.minX), max(b.minY-1, c.minY), max(b.minZ-1, c.minZ)
	x1, y1, z1 := min(b.maxX+1, c.maxX), min(b.maxY+1, c.maxY), min(b.maxZ+1, c.maxZ)
	cx, cy, cz := (x0+x1)/2, (y0+y1)/2, (z0+z1)/2
	if registry.Biome(w.g.biomeIDs[w.g.biomeIndex(w.r, cx, cy, cz)]).InTag(registry.BiomeTagIDMineshaftBlocking) {
		return true
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if w.liquid(x, y0, z) || w.liquid(x, y1, z) {
				return true
			}
		}
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			if w.liquid(x, y, z0) || w.liquid(x, y, z1) {
				return true
			}
		}
	}
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			if w.liquid(x0, y, z) || w.liquid(x1, y, z) {
				return true
			}
		}
	}
	return false
}

// upperHalfSphere is StructurePiece.generateUpperHalfSphere.
func (w *mineshaftWriter) upperHalfSphere(x0, y0, z0, x1, y1, z1 int32, fill uint16) {
	diagX, diagY, diagZ := float32(x1-x0+1), float32(y1-y0+1), float32(z1-z0+1)
	cx, cz := float32(x0)+diagX/2, float32(z0)+diagZ/2
	for y := y0; y <= y1; y++ {
		ny := float32(y-y0) / diagY
		for x := x0; x <= x1; x++ {
			nx := (float32(x) - cx) / (diagX * 0.5)
			for z := z0; z <= z1; z++ {
				nz := (float32(z) - cz) / (diagZ * 0.5)
				// float32 conversions keep the products unfused, as Java's are.
				if float32(float32(nx*nx)+float32(ny*ny))+float32(nz*nz) <= 1.05 {
					w.place(fill, x, y, z)
				}
			}
		}
	}
}

func (w *mineshaftWriter) postRoom() {
	b, air := w.p.box, w.st.air
	w.box(b.minX, b.minY+1, b.minZ, b.maxX, min(b.minY+3, b.maxY), b.maxZ, air, air)
	for _, e := range w.p.entrances {
		w.box(e.minX, e.maxY-2, e.minZ, e.maxX, e.maxY, e.maxZ, air, air)
	}
	w.upperHalfSphere(b.minX, b.minY+4, b.minZ, b.maxX, b.maxY, b.maxZ, air)
}

func (w *mineshaftWriter) postStairs() {
	air := w.st.air
	w.box(0, 5, 0, 2, 7, 1, air, air)
	w.box(0, 0, 7, 2, 2, 8, air, air)
	for i := range int32(5) {
		low := 5 - i
		if i < 4 {
			low--
		}
		w.box(0, low, 2+i, 2, 7-i, 2+i, air, air)
	}
}

func (w *mineshaftWriter) postCrossing() {
	b, air := w.p.box, w.st.air
	if w.p.twoFloored {
		w.box(b.minX+1, b.minY, b.minZ, b.maxX-1, b.minY+3-1, b.maxZ, air, air)
		w.box(b.minX, b.minY, b.minZ+1, b.maxX, b.minY+3-1, b.maxZ-1, air, air)
		w.box(b.minX+1, b.maxY-2, b.minZ, b.maxX-1, b.maxY, b.maxZ, air, air)
		w.box(b.minX, b.maxY-2, b.minZ+1, b.maxX, b.maxY, b.maxZ-1, air, air)
		w.box(b.minX+1, b.minY+3, b.minZ+1, b.maxX-1, b.minY+3, b.maxZ-1, air, air)
	} else {
		w.box(b.minX+1, b.minY, b.minZ, b.maxX-1, b.maxY, b.maxZ, air, air)
		w.box(b.minX, b.minY, b.minZ+1, b.maxX, b.maxY, b.maxZ-1, air, air)
	}
	w.pillar(b.minX+1, b.minY, b.minZ+1, b.maxY)
	w.pillar(b.minX+1, b.minY, b.maxZ-1, b.maxY)
	w.pillar(b.maxX-1, b.minY, b.minZ+1, b.maxY)
	w.pillar(b.maxX-1, b.minY, b.maxZ-1, b.maxY)
	y := b.minY - 1
	for x := b.minX; x <= b.maxX; x++ {
		for z := b.minZ; z <= b.maxZ; z++ {
			w.setPlanks(x, y, z)
		}
	}
}

// pillar is MineShaftCrossing.placeSupportPillar.
func (w *mineshaftWriter) pillar(x, y0, z, y1 int32) {
	if !w.isAir(w.block(x, y1+1, z)) {
		w.box(x, y0, z, x, y1, z, w.st.planks[w.p.typeIndex()], w.st.air)
	}
}

func (w *mineshaftWriter) replaceable(state uint16) bool {
	b := registry.BlockOf(state)
	return w.isAir(state) || mineshaftLiquids[b] || mineshaftReplaceable[b]
}

func (w *mineshaftWriter) postCorridor() {
	p, st := w.p, w.st
	air := st.air
	length := p.sections*5 - 1
	w.box(0, 0, 0, 2, 1, length, air, air)
	w.maybeBox(0.8, 0, 2, 0, 2, 2, length, air, air, false)
	if p.spiderCorridor {
		w.maybeBox(0.6, 0, 0, 0, 2, 1, length, st.cobweb, air, true)
	}
	for section := int32(0); section < p.sections; section++ {
		z := 2 + section*5
		w.support(0, 0, z, 2, 2)
		w.maybeCobweb(0.1, 0, 2, z-1)
		w.maybeCobweb(0.1, 2, 2, z-1)
		w.maybeCobweb(0.1, 0, 2, z+1)
		w.maybeCobweb(0.1, 2, 2, z+1)
		w.maybeCobweb(0.05, 0, 2, z-2)
		w.maybeCobweb(0.05, 2, 2, z-2)
		w.maybeCobweb(0.05, 0, 2, z+2)
		w.maybeCobweb(0.05, 2, 2, z+2)
		if w.rng.NextIntN(100) == 0 {
			w.chestMinecart(2, 0, z-1)
		}
		if w.rng.NextIntN(100) == 0 {
			w.chestMinecart(0, 0, z+1)
		}
		if !p.spiderCorridor || p.placedSpider.Load() {
			continue
		}
		newZ := z - 1 + w.rng.NextIntN(3)
		wx, wy, wz := w.pos(1, 0, newZ)
		if !w.chunk.inside(wx, wy, wz) || !w.interior(1, 0, newZ) {
			continue
		}
		p.placedSpider.Store(true)
		w.r.set(wx, wy, wz, st.spawner)
		w.r.placeBlockEntity(wx, wy, wz, st.spawner, nbt.Compound{
			"id":        nbt.String(KindSpawner.Name()),
			"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cave_spider")}},
		})
	}
	for x := int32(0); x <= 2; x++ {
		for z := int32(0); z <= length; z++ {
			w.setPlanks(x, -1, z)
		}
	}
	w.doubleSupport(0, -1, 2)
	if p.sections > 1 {
		w.doubleSupport(0, -1, length-2)
	}
	if p.hasRails {
		for z := int32(0); z <= length; z++ {
			floor := w.block(1, -1, z)
			if w.isAir(floor) || !registry.IsSolidRender(floor) {
				continue
			}
			prob := float32(0.9)
			if w.interior(1, 0, z) {
				prob = 0.7
			}
			w.maybeBlock(prob, 1, 0, z, st.railNS[p.dir])
		}
	}
}

// chestMinecart is MineShaftCorridor.createChest: the rail, then a chest
// minecart on it, kept in the chunk's proto until the chunk is finished.
func (w *mineshaftWriter) chestMinecart(x, y, z int32) {
	wx, wy, wz := w.pos(x, y, z)
	if !w.chunk.inside(wx, wy, wz) || !w.isAir(w.r.get(wx, wy, wz)) || w.isAir(w.r.get(wx, wy-1, wz)) {
		return
	}
	state := w.st.railEW[w.p.dir]
	if w.rng.NextBoolean() {
		state = w.st.railNS[w.p.dir]
	}
	w.place(state, x, y, z)
	seed := w.rng.NextLong()
	proto := w.r.protoAt(wx, wz)
	proto.entities = append(proto.entities, protoEntity{
		x: wx & 15, y: wy - w.r.minY, z: wz & 15,
		tag: nbt.Compound{
			"id":            nbt.String("minecraft:chest_minecart"),
			"LootTable":     nbt.String("minecraft:chests/abandoned_mineshaft"),
			"LootTableSeed": nbt.Long(seed),
			// vanilla: AbstractMinecart.addAdditionalSaveData
			"Rotation":        nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(0), nbt.Float(0)}},
			"FlippedRotation": nbt.Byte(0),
		},
	})
}

func (w *mineshaftWriter) doubleSupport(x, y, z int32) {
	t := w.p.typeIndex()
	if registry.BlockOf(w.block(x, y, z)) == w.st.planksBlock[t] {
		w.pillarDownOrChainUp(x, y, z)
	}
	if registry.BlockOf(w.block(x+2, y, z)) == w.st.planksBlock[t] {
		w.pillarDownOrChainUp(x+2, y, z)
	}
}

// pillarDownOrChainUp is MineShaftCorridor.fillPillarDownOrChainUp: a log
// column down to solid ground, or a fence and chain up to a ceiling.
func (w *mineshaftWriter) pillarDownOrChainUp(x, y, z int32) {
	st, t, r := w.st, w.p.typeIndex(), w.r
	wx, worldY, wz := w.pos(x, y, z)
	if !w.chunk.inside(wx, worldY, wz) {
		return
	}
	checkBelow, checkAbove := true, true
	for d := int32(1); checkBelow || checkAbove; d++ {
		if checkBelow {
			below := r.get(wx, worldY-d, wz)
			empty := w.replaceable(below) && registry.BlockOf(below) != registry.BlockLava
			if !empty && registry.FaceSturdy(below, dirUp, registry.SupportFull) {
				for py := worldY - d + 1; py < worldY; py++ {
					r.set(wx, py, wz, st.wood[t])
				}
				return
			}
			checkBelow = d <= msPillarReach && empty && worldY-d > r.minY+1
		}
		if checkAbove {
			above := r.get(wx, worldY+d, wz)
			empty := w.replaceable(above)
			if !empty && canSupportCenter(above, dirDown) && !mineshaftFalling[registry.BlockOf(above)] {
				r.set(wx, worldY+1, wz, st.fence[t])
				for py := worldY + 2; py < worldY+d; py++ {
					r.set(wx, py, wz, st.chain)
				}
				return
			}
			checkAbove = d <= msChainReach && empty && worldY+d < r.minY+r.height-1
		}
	}
}

// support is MineShaftCorridor.placeSupport.
func (w *mineshaftWriter) support(x0, y0, z, y1, x1 int32) {
	for x := x0; x <= x1; x++ {
		if w.isAir(w.block(x, y1+1, z)) {
			return
		}
	}
	st, t, dir := w.st, w.p.typeIndex(), w.p.dir
	w.box(x0, y0, z, x0, y1-1, z, st.fenceWest[t][dir], st.air)
	w.box(x1, y0, z, x1, y1-1, z, st.fenceEast[t][dir], st.air)
	if w.rng.NextIntN(4) == 0 {
		w.box(x0, y1, z, x0, y1, z, st.planks[t], st.air)
		w.box(x1, y1, z, x1, y1, z, st.planks[t], st.air)
		return
	}
	w.box(x0, y1, z, x1, y1, z, st.planks[t], st.air)
	w.maybeBlock(0.05, x0+1, y1, z-1, st.torchSouth[dir])
	w.maybeBlock(0.05, x0+1, y1, z+1, st.torchNorth[dir])
}

// maybeCobweb is MineShaftCorridor.maybePlaceCobWeb.
func (w *mineshaftWriter) maybeCobweb(prob float32, x, y, z int32) {
	if w.interior(x, y, z) && w.rng.NextFloat() < prob && w.sturdyNeighbours(x, y, z, 2) {
		w.place(w.st.cobweb, x, y, z)
	}
}

// sturdyNeighbours is MineShaftCorridor.hasSturdyNeighbours.
func (w *mineshaftWriter) sturdyNeighbours(x, y, z int32, count int) bool {
	wx, wy, wz := w.pos(x, y, z)
	n := 0
	for d := range 6 {
		nx, ny, nz := wx+dirDelta[d][0], wy+dirDelta[d][1], wz+dirDelta[d][2]
		if w.chunk.inside(nx, ny, nz) && registry.FaceSturdy(w.r.get(nx, ny, nz), blocklogic.Opposite(d), registry.SupportFull) {
			n++
			if n >= count {
				return true
			}
		}
	}
	return false
}
