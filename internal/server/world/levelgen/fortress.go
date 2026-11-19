package levelgen

import "sync/atomic"

// The nether fortress: the piece tree vanilla grows from the start piece, and
// the blocks each piece writes into one chunk. vanilla: NetherFortressStructure,
// NetherFortressPieces

// BoundingBox is an inclusive block box. vanilla: BoundingBox
type BoundingBox struct{ MinX, MinY, MinZ, MaxX, MaxY, MaxZ int }

func (b BoundingBox) intersects(o BoundingBox) bool {
	return b.MaxX >= o.MinX && b.MinX <= o.MaxX && b.MaxZ >= o.MinZ && b.MinZ <= o.MaxZ &&
		b.MaxY >= o.MinY && b.MinY <= o.MaxY
}

func (b BoundingBox) inside(x, y, z int) bool {
	return x >= b.MinX && x <= b.MaxX && z >= b.MinZ && z <= b.MaxZ && y >= b.MinY && y <= b.MaxY
}

func (b BoundingBox) union(o BoundingBox) BoundingBox {
	return BoundingBox{min(b.MinX, o.MinX), min(b.MinY, o.MinY), min(b.MinZ, o.MinZ),
		max(b.MaxX, o.MaxX), max(b.MaxY, o.MaxY), max(b.MaxZ, o.MaxZ)}
}

// orientBox is vanilla: BoundingBox.orientBox.
func orientBox(footX, footY, footZ, offX, offY, offZ, width, height, depth int, d fdir) BoundingBox {
	switch d {
	case dN:
		return BoundingBox{footX + offX, footY + offY, footZ - depth + 1 + offZ,
			footX + width - 1 + offX, footY + height - 1 + offY, footZ + offZ}
	case dW:
		return BoundingBox{footX - depth + 1 + offZ, footY + offY, footZ + offX,
			footX + offZ, footY + height - 1 + offY, footZ + width - 1 + offX}
	case dE:
		return BoundingBox{footX + offZ, footY + offY, footZ + offX,
			footX + depth - 1 + offZ, footY + height - 1 + offY, footZ + width - 1 + offX}
	}
	return BoundingBox{footX + offX, footY + offY, footZ + offZ,
		footX + width - 1 + offX, footY + height - 1 + offY, footZ + depth - 1 + offZ}
}

// SetLargeFeatureWithSalt seeds the stream for a structure placement grid
// cell. vanilla: WorldgenRandom.setLargeFeatureWithSalt
func (r *LegacyRandomSource) SetLargeFeatureWithSalt(seed int64, x, z, salt int32) {
	r.SetSeed(int64(x)*341873128712 + int64(z)*132897987541 + seed + int64(salt))
}

// RandomSpreadChunk is the chunk a random_spread placement puts a structure
// in within the grid cell around (chunkX, chunkZ), spread linearly.
// vanilla: RandomSpreadStructurePlacement.getPotentialStructureChunk
func RandomSpreadChunk(seed int64, chunkX, chunkZ, spacing, separation, salt int32) (int32, int32) {
	p := StructurePlacement{Spacing: spacing, Separation: separation, Salt: salt}
	return p.PotentialChunk(seed, chunkX, chunkZ)
}

// The nether_complexes structure set's placement.
const (
	NetherComplexSpacing    = 27
	NetherComplexSeparation = 4
	NetherComplexSalt       = 30084232
	// NetherComplexStep and NetherFortressIndex place the fortress among the
	// structures of the underground_decoration step, whose index seeds the
	// placement random. vanilla: ChunkGenerator.applyBiomeDecoration
	NetherComplexStep   = 7
	NetherFortressIndex = 1
	fortressStructure   = "minecraft:fortress"
)

// bastionStarts are the sizes (x, y, z) of the templates in the
// bastion/starts pool, which the bastion's start piece is drawn from with
// equal weights. vanilla: data/minecraft/worldgen/template_pool/bastion/starts.json
var bastionStarts = [4][3]int32{{46, 24, 46}, {30, 24, 48}, {38, 48, 38}, {16, 32, 32}}

// BastionStartCentre returns the block column the bastion remnant's biome is
// read at: the centre of its start piece, whose rotation and template are the
// first two draws of the start's random. The y read is 33 (start_height).
// vanilla: JigsawPlacement.addPieces
func BastionStartCentre(seed int64, chunkX, chunkZ int32) (x, z int32) {
	r := NewLegacyRandom(0)
	r.SetLargeFeatureSeed(seed, chunkX, chunkZ)
	rotation := r.NextIntN(4)
	size := bastionStarts[r.NextIntN(4)]
	// The template box spans (0,0,0) to size-1 before the rotation about the
	// origin. vanilla: StructureTemplate.getBoundingBox
	dx, dz := size[0]-1, size[2]-1
	var minX, maxX, minZ, maxZ int32
	switch rotation {
	case 0:
		minX, maxX, minZ, maxZ = 0, dx, 0, dz
	case 1:
		minX, maxX, minZ, maxZ = -dz, 0, 0, dx
	case 2:
		minX, maxX, minZ, maxZ = -dx, 0, -dz, 0
	default:
		minX, maxX, minZ, maxZ = 0, dz, -dx, 0
	}
	// Java integer division truncates toward zero, as Go's does.
	return chunkX*16 + (minX+maxX)/2, chunkZ*16 + (minZ+maxZ)/2
}

// NetherComplexIsFortress reports whether the nether_complexes start in its
// placement chunk (chunkX, chunkZ) is a fortress: the weighted pick takes it,
// or takes the bastion remnant and finds its biome invalid. bastionBiomeOK is
// whether the bastion's start point (BastionStartCentre) is in an allowed
// biome; a fortress may generate in every nether biome.
// vanilla: ChunkGenerator.createStructures
func NetherComplexIsFortress(seed int64, chunkX, chunkZ int32, bastionBiomeOK bool) bool {
	fortress := false
	StructureSetNamed("minecraft:nether_complexes").TryEntries(seed, chunkX, chunkZ, func(e StructureSetEntry) bool {
		fortress = e.Structure == fortressStructure
		return fortress || bastionBiomeOK
	})
	return fortress
}

type fdir uint8

const (
	dN fdir = iota
	dE
	dS
	dW
)

func (d fdir) opposite() fdir         { return (d + 2) & 3 }
func (d fdir) clockwise() fdir        { return (d + 1) & 3 }
func (d fdir) counterClockwise() fdir { return (d + 3) & 3 }

// Fence connection bits, in fdir order.
const (
	fN = 1 << iota
	fE
	fS
	fW
)

type fkind uint8

const (
	kAir fkind = iota
	kBricks
	kFence
	kStairs
	kSoulSand
	kWart
	kLava
)

// fblock is a block as a piece spells it before the piece's mirror and
// rotation apply: a kind and, for a fence the connection bits, for stairs the
// facing.
type fblock struct {
	kind fkind
	arg  uint8
}

var (
	air      = fblock{kind: kAir}
	bricks   = fblock{kind: kBricks}
	soulSand = fblock{kind: kSoulSand}
	wart     = fblock{kind: kWart}
	lava     = fblock{kind: kLava}
)

func fenceB(mask uint8) fblock { return fblock{kFence, mask} }
func stairsB(d fdir) fblock    { return fblock{kStairs, uint8(d)} }

func (b fblock) with(mask uint8) fblock { return fblock{b.kind, b.arg | mask} }

// FortressStates are the block states a fortress writes.
type FortressStates struct {
	Air, NetherBricks, SoulSand, NetherWart, Lava uint16
	// Fence is indexed by the connection bits north 1, east 2, south 4, west 8.
	Fence [16]uint16
	// Stairs is indexed by facing north, east, south, west.
	Stairs [4]uint16
}

// FortressWorld is the level a fortress writes into and reads back.
type FortressWorld interface {
	ChestWorld
	SetBlock(x, y, z int, state uint16)
	// ReplaceableByStructures is air or a liquid. vanilla: StructurePiece.isReplaceableByStructures
	ReplaceableByStructures(state uint16) bool
	// PlaceSpawner sets a blaze spawner.
	PlaceSpawner(x, y, z int)
	// FenceAt notes a fence the world must reconnect once the chunk is
	// decorated. vanilla: StructurePiece.placeBlock, markPosForPostProcessing
	FenceAt(x, y, z int)
	// ScheduleFluid schedules the tick of a lava source placed at a block.
	ScheduleFluid(x, y, z int)
}

type fortressCtx struct {
	w     FortressWorld
	st    *FortressStates
	rng   RandomSource
	chunk BoundingBox
}

func (c *fortressCtx) state(b fblock) uint16 {
	switch b.kind {
	case kBricks:
		return c.st.NetherBricks
	case kFence:
		return c.st.Fence[b.arg]
	case kStairs:
		return c.st.Stairs[b.arg]
	case kSoulSand:
		return c.st.SoulSand
	case kWart:
		return c.st.NetherWart
	case kLava:
		return c.st.Lava
	}
	return c.st.Air
}

type pieceKind uint8

const (
	pBridgeStraight pieceKind = iota
	pBridgeCrossing
	pRoomCrossing
	pStairsRoom
	pMonsterThrone
	pCastleEntrance
	pSmallCorridor
	pSmallCorridorCrossing
	pRightTurn
	pLeftTurn
	pCorridorStairs
	pTBalcony
	pStalkRoom
	pEndFiller
)

type fortressPiece struct {
	kind       pieceKind
	bb         BoundingBox
	dir        fdir
	depth      int
	needsChest bool
	selfSeed   int32
	// placed is set once the chunk holding the throne's spawner or the turn's
	// chest was written, which vanilla saves as the spawner placed or the
	// chest no longer needed.
	placed atomic.Bool
}

func (p *fortressPiece) frame() PieceFrame { return PieceFrame{BB: p.bb, Dir: int(p.dir)} }

func (p *fortressPiece) mapDir(d fdir) fdir { return fdir(p.frame().MapDir(int(d))) }

func (p *fortressPiece) worldPos(x, y, z int) (int, int, int) { return p.frame().WorldPos(x, y, z) }

// box is vanilla: StructurePiece.generateBox.
func (p *fortressPiece) box(c *fortressCtx, x0, y0, z0, x1, y1, z1 int, edge, fill fblock) {
	GenerateBox(x0, y0, z0, x1, y1, z1, func(x, y, z int, shell bool) {
		if shell {
			p.place(c, edge, x, y, z)
		} else {
			p.place(c, fill, x, y, z)
		}
	})
}

// chest is vanilla: StructurePiece.createChest.
func (p *fortressPiece) chest(c *fortressCtx, x, y, z int) {
	if p.frame().CreateChest(c.w, c.chunk, x, y, z) {
		p.placed.Store(true)
	}
}

func (p *fortressPiece) transform(b fblock) fblock {
	switch b.kind {
	case kStairs:
		return fblock{kStairs, uint8(p.mapDir(fdir(b.arg)))}
	case kFence:
		var m uint8
		for d := range fdir(4) {
			if b.arg&(1<<d) != 0 {
				m |= 1 << p.mapDir(d)
			}
		}
		return fblock{kFence, m}
	}
	return b
}

// place is vanilla: StructurePiece.placeBlock.
func (p *fortressPiece) place(c *fortressCtx, b fblock, x, y, z int) {
	wx, wy, wz := p.worldPos(x, y, z)
	if !c.chunk.inside(wx, wy, wz) {
		return
	}
	b = p.transform(b)
	c.w.SetBlock(wx, wy, wz, c.state(b))
	switch b.kind {
	case kLava:
		c.w.ScheduleFluid(wx, wy, wz)
	case kFence:
		c.w.FenceAt(wx, wy, wz)
	}
}

// fillColumnDown is vanilla: StructurePiece.fillColumnDown, for nether bricks.
func (p *fortressPiece) fillColumnDown(c *fortressCtx, x, startY, z int) {
	wx, wy, wz := p.worldPos(x, startY, z)
	if !c.chunk.inside(wx, wy, wz) {
		return
	}
	for c.w.ReplaceableByStructures(c.w.Block(wx, wy, wz)) && wy > fortressMinY+1 {
		c.w.SetBlock(wx, wy, wz, c.st.NetherBricks)
		wy--
	}
}

// fortressMinY is the nether's lowest block; the writable area starts above it.
const fortressMinY = 0

// fortressMaxY is the nether's highest block.
const fortressMaxY = 255

// spawner places the throne's blaze spawner at a piece-local block.
func (p *fortressPiece) spawner(c *fortressCtx, x, y, z int) {
	wx, wy, wz := p.worldPos(x, y, z)
	if c.chunk.inside(wx, wy, wz) {
		p.placed.Store(true)
		c.w.PlaceSpawner(wx, wy, wz)
	}
}

type pieceWeight struct {
	kind       pieceKind
	weight     int
	placeCount int
	maxCount   int
	allowInRow bool
}

func (w *pieceWeight) valid() bool { return w.maxCount == 0 || w.placeCount < w.maxCount }

type fortressGen struct {
	rng      RandomSource
	pieces   []*fortressPiece
	start    *fortressPiece
	previous *pieceWeight
	bridge   []*pieceWeight
	castle   []*pieceWeight
	pending  []*fortressPiece
}

const (
	fortressMaxDepth     = 30
	fortressRange        = 112
	fortressLowestY      = 10
	fortressStartY       = 64
	fortressMinStartY    = 48
	fortressMaxStartY    = 70
	fortressBridgeBudget = 5
)

func (g *fortressGen) collides(box BoundingBox) bool {
	_, hit := findCollision(g.pieces, box, func(p *fortressPiece) BoundingBox { return p.bb })
	return hit
}

// findCollision is the first piece whose box meets box.
// vanilla: StructurePiece.findCollisionPiece
func findCollision[P any](pieces []P, box BoundingBox, bb func(P) BoundingBox) (P, bool) {
	for _, p := range pieces {
		if bb(p).intersects(box) {
			return p, true
		}
	}
	var none P
	return none, false
}

// unionOf is the box holding every piece's box; pieces is not empty.
func unionOf[P any](pieces []P, bb func(P) BoundingBox) BoundingBox {
	u := bb(pieces[0])
	for _, p := range pieces[1:] {
		u = u.union(bb(p))
	}
	return u
}

type pieceShape struct{ offX, offY, offZ, width, height, depth int }

var pieceShapes = [...]pieceShape{
	pBridgeStraight:        {-1, -3, 0, 5, 10, 19},
	pBridgeCrossing:        {-8, -3, 0, 19, 10, 19},
	pRoomCrossing:          {-2, 0, 0, 7, 9, 7},
	pStairsRoom:            {-2, 0, 0, 7, 11, 7},
	pMonsterThrone:         {-2, 0, 0, 7, 8, 9},
	pCastleEntrance:        {-5, -3, 0, 13, 14, 13},
	pSmallCorridor:         {-1, 0, 0, 5, 7, 5},
	pSmallCorridorCrossing: {-1, 0, 0, 5, 7, 5},
	pRightTurn:             {-1, 0, 0, 5, 7, 5},
	pLeftTurn:              {-1, 0, 0, 5, 7, 5},
	pCorridorStairs:        {-1, -7, 0, 5, 14, 10},
	pTBalcony:              {-3, 0, 0, 9, 7, 9},
	pStalkRoom:             {-5, -3, 0, 13, 14, 13},
	pEndFiller:             {-1, -3, 0, 5, 10, 8},
}

// create is each piece's createPiece: nil when the box leaves the lowest
// allowed height or hits a piece, else the piece, drawing from the random
// where its constructor does.
func (g *fortressGen) create(kind pieceKind, footX, footY, footZ int, d fdir, depth int) *fortressPiece {
	s := pieceShapes[kind]
	box := orientBox(footX, footY, footZ, s.offX, s.offY, s.offZ, s.width, s.height, s.depth, d)
	if box.MinY <= fortressLowestY || g.collides(box) {
		return nil
	}
	p := &fortressPiece{kind: kind, bb: box, dir: d, depth: depth}
	switch kind {
	case pRightTurn, pLeftTurn:
		p.needsChest = g.rng.NextIntN(3) == 0
	case pEndFiller:
		p.selfSeed = g.rng.NextInt()
	}
	return p
}

var bridgeWeights = [...]pieceWeight{
	{pBridgeStraight, 30, 0, 0, true}, {pBridgeCrossing, 10, 0, 4, false}, {pRoomCrossing, 10, 0, 4, false},
	{pStairsRoom, 10, 0, 3, false}, {pMonsterThrone, 5, 0, 2, false}, {pCastleEntrance, 5, 0, 1, false},
}

var castleWeights = [...]pieceWeight{
	{pSmallCorridor, 25, 0, 0, true}, {pSmallCorridorCrossing, 15, 0, 5, false}, {pRightTurn, 5, 0, 10, false},
	{pLeftTurn, 5, 0, 10, false}, {pCorridorStairs, 10, 0, 3, true}, {pTBalcony, 7, 0, 2, false},
	{pStalkRoom, 5, 0, 2, false},
}

// updatePieceWeight returns the total weight of the pieces still available,
// or -1 when none has a limit left. vanilla: NetherBridgePiece.updatePieceWeight
func updatePieceWeight(list []*pieceWeight) int {
	any := false
	total := 0
	for _, w := range list {
		if w.maxCount > 0 && w.placeCount < w.maxCount {
			any = true
		}
		total += w.weight
	}
	if any {
		return total
	}
	return -1
}

// generatePiece draws a weighted piece for a foot position, falling back to a
// bridge end filler. A piece that cannot be made leaves the attempt's draw
// to the next piece in the list. vanilla: NetherBridgePiece.generatePiece
func (g *fortressGen) generatePiece(list *[]*pieceWeight, footX, footY, footZ int, d fdir, depth int) *fortressPiece {
	total := updatePieceWeight(*list)
	if total > 0 && depth <= fortressMaxDepth {
	attempt:
		for range fortressBridgeBudget {
			sel := int(g.rng.NextIntN(int32(total)))
			for i, w := range *list {
				sel -= w.weight
				if sel >= 0 {
					continue
				}
				if !w.valid() || w == g.previous && !w.allowInRow {
					continue attempt
				}
				p := g.create(w.kind, footX, footY, footZ, d, depth)
				if p == nil {
					continue
				}
				w.placeCount++
				g.previous = w
				if !w.valid() {
					*list = append((*list)[:i:i], (*list)[i+1:]...)
				}
				return p
			}
		}
	}
	return g.create(pEndFiller, footX, footY, footZ, d, depth)
}

// generateAndAddPiece adds a piece at a foot position; a foot beyond the
// fortress's range yields an end filler that is not kept.
// vanilla: NetherBridgePiece.generateAndAddPiece
func (g *fortressGen) generateAndAddPiece(footX, footY, footZ int, d fdir, depth int, castle bool) {
	if abs(footX-g.start.bb.MinX) > fortressRange || abs(footZ-g.start.bb.MinZ) > fortressRange {
		g.create(pEndFiller, footX, footY, footZ, d, depth)
		return
	}
	list := &g.bridge
	if castle {
		list = &g.castle
	}
	if p := g.generatePiece(list, footX, footY, footZ, d, depth+1); p != nil {
		g.pieces = append(g.pieces, p)
		g.pending = append(g.pending, p)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// childSide is which way a piece's child leaves it.
type childSide uint8

const (
	sideForward childSide = iota
	sideLeft
	sideRight
)

// childFoot is the foot and direction of the child that leaves box b, which
// faces d, on a side. off is the offset along the piece's width for a forward
// child and along its length for a left or right one.
// vanilla: StructurePiece.generateChildForward, generateChildLeft, generateChildRight
func childFoot(b BoundingBox, d fdir, side childSide, yOff, off int) (x, y, z int, dir fdir) {
	y = b.MinY + yOff
	alongZ := d == dN || d == dS
	switch side {
	case sideForward:
		switch d {
		case dN:
			return b.MinX + off, y, b.MinZ - 1, d
		case dS:
			return b.MinX + off, y, b.MaxZ + 1, d
		case dW:
			return b.MinX - 1, y, b.MinZ + off, d
		}
		return b.MaxX + 1, y, b.MinZ + off, d
	case sideLeft:
		if alongZ {
			return b.MinX - 1, y, b.MinZ + off, dW
		}
		return b.MinX + off, y, b.MinZ - 1, dN
	}
	if alongZ {
		return b.MaxX + 1, y, b.MinZ + off, dE
	}
	return b.MinX + off, y, b.MaxZ + 1, dS
}

func (g *fortressGen) childForward(p *fortressPiece, xOff, yOff int, castle bool) {
	x, y, z, d := childFoot(p.bb, p.dir, sideForward, yOff, xOff)
	g.generateAndAddPiece(x, y, z, d, p.depth, castle)
}

func (g *fortressGen) childLeft(p *fortressPiece, yOff, zOff int, castle bool) {
	x, y, z, d := childFoot(p.bb, p.dir, sideLeft, yOff, zOff)
	g.generateAndAddPiece(x, y, z, d, p.depth, castle)
}

func (g *fortressGen) childRight(p *fortressPiece, yOff, zOff int, castle bool) {
	x, y, z, d := childFoot(p.bb, p.dir, sideRight, yOff, zOff)
	g.generateAndAddPiece(x, y, z, d, p.depth, castle)
}

// addChildren grows a piece's neighbours. vanilla: StructurePiece.addChildren
func (g *fortressGen) addChildren(p *fortressPiece) {
	switch p.kind {
	case pBridgeStraight:
		g.childForward(p, 1, 3, false)
	case pBridgeCrossing:
		g.childForward(p, 8, 3, false)
		g.childLeft(p, 3, 8, false)
		g.childRight(p, 3, 8, false)
	case pRoomCrossing:
		g.childForward(p, 2, 0, false)
		g.childLeft(p, 0, 2, false)
		g.childRight(p, 0, 2, false)
	case pStairsRoom:
		g.childRight(p, 6, 2, false)
	case pCastleEntrance:
		g.childForward(p, 5, 3, true)
	case pSmallCorridor, pCorridorStairs:
		g.childForward(p, 1, 0, true)
	case pRightTurn:
		g.childRight(p, 0, 1, true)
	case pLeftTurn:
		g.childLeft(p, 0, 1, true)
	case pTBalcony:
		zOff := 1
		if p.dir == dW || p.dir == dN {
			zOff = 5
		}
		left := g.rng.NextIntN(8) > 0
		g.childLeft(p, 0, zOff, left)
		right := g.rng.NextIntN(8) > 0
		g.childRight(p, 0, zOff, right)
	case pSmallCorridorCrossing:
		g.childForward(p, 1, 0, true)
		g.childLeft(p, 0, 1, true)
		g.childRight(p, 0, 1, true)
	case pStalkRoom:
		g.childForward(p, 5, 3, true)
		g.childForward(p, 5, 11, true)
	}
}

// FortressStart is a fortress's pieces, placed as they were first grown.
type FortressStart struct {
	ChunkX, ChunkZ int32
	Bounds         BoundingBox
	pieces         []*fortressPiece
}

// NewFortressStart grows the fortress that starts in a chunk. vanilla: NetherFortressStructure.generatePieces
func NewFortressStart(seed int64, chunkX, chunkZ int32) *FortressStart {
	rng := NewLegacyRandom(0)
	rng.SetLargeFeatureSeed(seed, chunkX, chunkZ)
	g := &fortressGen{rng: rng}
	for i := range bridgeWeights {
		w := bridgeWeights[i]
		g.bridge = append(g.bridge, &w)
	}
	for i := range castleWeights {
		w := castleWeights[i]
		g.castle = append(g.castle, &w)
	}
	west, north := int(chunkX)*16+2, int(chunkZ)*16+2
	start := &fortressPiece{kind: pBridgeCrossing, dir: fdir(rng.NextIntN(4))}
	// vanilla: StructurePiece.makeBoundingBox; the start's 19x19 footprint
	// reads the same for every orientation.
	start.bb = BoundingBox{west, fortressStartY, north, west + 18, fortressStartY + 9, north + 18}
	g.start = start
	g.pieces = append(g.pieces, start)
	g.addChildren(start)
	for len(g.pending) > 0 {
		i := int(rng.NextIntN(int32(len(g.pending))))
		p := g.pending[i]
		g.pending = append(g.pending[:i], g.pending[i+1:]...)
		g.addChildren(p)
	}
	// vanilla: StructurePiecesBuilder.moveInsideHeights
	bounds := unionOf(g.pieces, func(p *fortressPiece) BoundingBox { return p.bb })
	span := fortressMaxStartY - fortressMinStartY + 1 - (bounds.MaxY - bounds.MinY + 1)
	y0 := fortressMinStartY
	if span > 1 {
		y0 += int(rng.NextIntN(int32(span)))
	}
	dy := y0 - bounds.MinY
	for _, p := range g.pieces {
		p.bb.MinY += dy
		p.bb.MaxY += dy
	}
	bounds.MinY += dy
	bounds.MaxY += dy
	return &FortressStart{ChunkX: chunkX, ChunkZ: chunkZ, Bounds: bounds, pieces: g.pieces}
}

// Intersects reports whether any block of the chunk (cx, cz) lies in the
// fortress's bounding box.
func (s *FortressStart) Intersects(cx, cz int32) bool {
	x, z := int(cx)*16, int(cz)*16
	b := s.Bounds
	return b.MaxX >= x && b.MinX <= x+15 && b.MaxZ >= z && b.MinZ <= z+15
}

// Contains reports whether the block lies in the fortress's bounding box and
// whether it lies in one of its pieces' boxes.
// vanilla: StructureManager.getStructureAt, StructureManager.structureHasPieceAt
func (s *FortressStart) Contains(x, y, z int) (inBounds, inPiece bool) {
	if !s.Bounds.inside(x, y, z) {
		return false, false
	}
	for _, p := range s.pieces {
		if p.bb.inside(x, y, z) {
			return true, true
		}
	}
	return true, false
}

// PlaceInChunk writes the part of every piece that lies in chunk (cx, cz).
// rng is the chunk's decoration random, seeded for the fortress's step and
// index. vanilla: StructureStart.placeInChunk
func (s *FortressStart) PlaceInChunk(w FortressWorld, st *FortressStates, rng RandomSource, cx, cz int32) {
	x, z := int(cx)*16, int(cz)*16
	c := &fortressCtx{w: w, st: st, rng: rng,
		chunk: BoundingBox{x, fortressMinY + 1, z, x + 15, fortressMaxY, z + 15}}
	for _, p := range s.pieces {
		if p.bb.intersects(c.chunk) {
			p.postProcess(c)
		}
	}
}

func (p *fortressPiece) postProcess(c *fortressCtx) {
	switch p.kind {
	case pBridgeStraight:
		p.postBridgeStraight(c)
	case pBridgeCrossing:
		p.postBridgeCrossing(c)
	case pRoomCrossing:
		p.postRoomCrossing(c)
	case pStairsRoom:
		p.postStairsRoom(c)
	case pMonsterThrone:
		p.postMonsterThrone(c)
	case pCastleEntrance:
		p.postCastleEntrance(c)
	case pSmallCorridor:
		p.postCastleSmallCorridor(c)
	case pSmallCorridorCrossing:
		p.postCastleSmallCorridorCrossing(c)
	case pRightTurn:
		p.postCastleSmallCorridorTurn(c, false)
	case pLeftTurn:
		p.postCastleSmallCorridorTurn(c, true)
	case pCorridorStairs:
		p.postCastleCorridorStairs(c)
	case pTBalcony:
		p.postCastleCorridorTBalcony(c)
	case pStalkRoom:
		p.postCastleStalkRoom(c)
	case pEndFiller:
		p.postBridgeEndFiller(c)
	}
}

func (p *fortressPiece) postBridgeStraight(w *fortressCtx) {
	p.box(w, 0, 3, 0, 4, 4, 18, bricks, bricks)
	p.box(w, 1, 5, 0, 3, 7, 18, air, air)
	p.box(w, 0, 5, 0, 0, 5, 18, bricks, bricks)
	p.box(w, 4, 5, 0, 4, 5, 18, bricks, bricks)
	p.box(w, 0, 2, 0, 4, 2, 5, bricks, bricks)
	p.box(w, 0, 2, 13, 4, 2, 18, bricks, bricks)
	p.box(w, 0, 0, 0, 4, 1, 3, bricks, bricks)
	p.box(w, 0, 0, 15, 4, 1, 18, bricks, bricks)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 2; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, x, -1, 18-z)
		}
	}
	nsFence := fenceB(fN | fS)
	nseFence := nsFence.with(fE)
	nswFence := nsFence.with(fW)
	p.box(w, 0, 1, 1, 0, 4, 1, nseFence, nseFence)
	p.box(w, 0, 3, 4, 0, 4, 4, nseFence, nseFence)
	p.box(w, 0, 3, 14, 0, 4, 14, nseFence, nseFence)
	p.box(w, 0, 1, 17, 0, 4, 17, nseFence, nseFence)
	p.box(w, 4, 1, 1, 4, 4, 1, nswFence, nswFence)
	p.box(w, 4, 3, 4, 4, 4, 4, nswFence, nswFence)
	p.box(w, 4, 3, 14, 4, 4, 14, nswFence, nswFence)
	p.box(w, 4, 1, 17, 4, 4, 17, nswFence, nswFence)
}

func (p *fortressPiece) postBridgeCrossing(w *fortressCtx) {
	var z int
	var x int
	p.box(w, 7, 3, 0, 11, 4, 18, bricks, bricks)
	p.box(w, 0, 3, 7, 18, 4, 11, bricks, bricks)
	p.box(w, 8, 5, 0, 10, 7, 18, air, air)
	p.box(w, 0, 5, 8, 18, 7, 10, air, air)
	p.box(w, 7, 5, 0, 7, 5, 7, bricks, bricks)
	p.box(w, 7, 5, 11, 7, 5, 18, bricks, bricks)
	p.box(w, 11, 5, 0, 11, 5, 7, bricks, bricks)
	p.box(w, 11, 5, 11, 11, 5, 18, bricks, bricks)
	p.box(w, 0, 5, 7, 7, 5, 7, bricks, bricks)
	p.box(w, 11, 5, 7, 18, 5, 7, bricks, bricks)
	p.box(w, 0, 5, 11, 7, 5, 11, bricks, bricks)
	p.box(w, 11, 5, 11, 18, 5, 11, bricks, bricks)
	p.box(w, 7, 2, 0, 11, 2, 5, bricks, bricks)
	p.box(w, 7, 2, 13, 11, 2, 18, bricks, bricks)
	p.box(w, 7, 0, 0, 11, 1, 3, bricks, bricks)
	p.box(w, 7, 0, 15, 11, 1, 18, bricks, bricks)
	for x = 7; x <= 11; x++ {
		for z = 0; z <= 2; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, x, -1, 18-z)
		}
	}
	p.box(w, 0, 2, 7, 5, 2, 11, bricks, bricks)
	p.box(w, 13, 2, 7, 18, 2, 11, bricks, bricks)
	p.box(w, 0, 0, 7, 3, 1, 11, bricks, bricks)
	p.box(w, 15, 0, 7, 18, 1, 11, bricks, bricks)
	for x = 0; x <= 2; x++ {
		for z = 7; z <= 11; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, 18-x, -1, z)
		}
	}
}

func (p *fortressPiece) postRoomCrossing(w *fortressCtx) {
	p.box(w, 0, 0, 0, 6, 1, 6, bricks, bricks)
	p.box(w, 0, 2, 0, 6, 7, 6, air, air)
	p.box(w, 0, 2, 0, 1, 6, 0, bricks, bricks)
	p.box(w, 0, 2, 6, 1, 6, 6, bricks, bricks)
	p.box(w, 5, 2, 0, 6, 6, 0, bricks, bricks)
	p.box(w, 5, 2, 6, 6, 6, 6, bricks, bricks)
	p.box(w, 0, 2, 0, 0, 6, 1, bricks, bricks)
	p.box(w, 0, 2, 5, 0, 6, 6, bricks, bricks)
	p.box(w, 6, 2, 0, 6, 6, 1, bricks, bricks)
	p.box(w, 6, 2, 5, 6, 6, 6, bricks, bricks)
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	p.box(w, 2, 6, 0, 4, 6, 0, bricks, bricks)
	p.box(w, 2, 5, 0, 4, 5, 0, weFence, weFence)
	p.box(w, 2, 6, 6, 4, 6, 6, bricks, bricks)
	p.box(w, 2, 5, 6, 4, 5, 6, weFence, weFence)
	p.box(w, 0, 6, 2, 0, 6, 4, bricks, bricks)
	p.box(w, 0, 5, 2, 0, 5, 4, nsFence, nsFence)
	p.box(w, 6, 6, 2, 6, 6, 4, bricks, bricks)
	p.box(w, 6, 5, 2, 6, 5, 4, nsFence, nsFence)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postStairsRoom(w *fortressCtx) {
	p.box(w, 0, 0, 0, 6, 1, 6, bricks, bricks)
	p.box(w, 0, 2, 0, 6, 10, 6, air, air)
	p.box(w, 0, 2, 0, 1, 8, 0, bricks, bricks)
	p.box(w, 5, 2, 0, 6, 8, 0, bricks, bricks)
	p.box(w, 0, 2, 1, 0, 8, 6, bricks, bricks)
	p.box(w, 6, 2, 1, 6, 8, 6, bricks, bricks)
	p.box(w, 1, 2, 6, 5, 8, 6, bricks, bricks)
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	p.box(w, 0, 3, 2, 0, 5, 4, nsFence, nsFence)
	p.box(w, 6, 3, 2, 6, 5, 2, nsFence, nsFence)
	p.box(w, 6, 3, 4, 6, 5, 4, nsFence, nsFence)
	p.place(w, bricks, 5, 2, 5)
	p.box(w, 4, 2, 5, 4, 3, 5, bricks, bricks)
	p.box(w, 3, 2, 5, 3, 4, 5, bricks, bricks)
	p.box(w, 2, 2, 5, 2, 5, 5, bricks, bricks)
	p.box(w, 1, 2, 5, 1, 6, 5, bricks, bricks)
	p.box(w, 1, 7, 1, 5, 7, 4, bricks, bricks)
	p.box(w, 6, 8, 2, 6, 8, 4, air, air)
	p.box(w, 2, 6, 0, 4, 8, 0, bricks, bricks)
	p.box(w, 2, 5, 0, 4, 5, 0, weFence, weFence)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postMonsterThrone(w *fortressCtx) {
	p.box(w, 0, 2, 0, 6, 7, 7, air, air)
	p.box(w, 1, 0, 0, 5, 1, 7, bricks, bricks)
	p.box(w, 1, 2, 1, 5, 2, 7, bricks, bricks)
	p.box(w, 1, 3, 2, 5, 3, 7, bricks, bricks)
	p.box(w, 1, 4, 3, 5, 4, 7, bricks, bricks)
	p.box(w, 1, 2, 0, 1, 4, 2, bricks, bricks)
	p.box(w, 5, 2, 0, 5, 4, 2, bricks, bricks)
	p.box(w, 1, 5, 2, 1, 5, 3, bricks, bricks)
	p.box(w, 5, 5, 2, 5, 5, 3, bricks, bricks)
	p.box(w, 0, 5, 3, 0, 5, 8, bricks, bricks)
	p.box(w, 6, 5, 3, 6, 5, 8, bricks, bricks)
	p.box(w, 1, 5, 8, 5, 5, 8, bricks, bricks)
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	p.place(w, fenceB(fW), 1, 6, 3)
	p.place(w, fenceB(fE), 5, 6, 3)
	p.place(w, fenceB(fE|fN), 0, 6, 3)
	p.place(w, fenceB(fW|fN), 6, 6, 3)
	p.box(w, 0, 6, 4, 0, 6, 7, nsFence, nsFence)
	p.box(w, 6, 6, 4, 6, 6, 7, nsFence, nsFence)
	p.place(w, fenceB(fE|fS), 0, 6, 8)
	p.place(w, fenceB(fW|fS), 6, 6, 8)
	p.box(w, 1, 6, 8, 5, 6, 8, weFence, weFence)
	p.place(w, fenceB(fE), 1, 7, 8)
	p.box(w, 2, 7, 8, 4, 7, 8, weFence, weFence)
	p.place(w, fenceB(fW), 5, 7, 8)
	p.place(w, fenceB(fE), 2, 8, 8)
	p.place(w, weFence, 3, 8, 8)
	p.place(w, fenceB(fW), 4, 8, 8)
	p.spawner(w, 3, 5, 5)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleEntrance(w *fortressCtx) {
	var z int
	var x int
	p.box(w, 0, 3, 0, 12, 4, 12, bricks, bricks)
	p.box(w, 0, 5, 0, 12, 13, 12, air, air)
	p.box(w, 0, 5, 0, 1, 12, 12, bricks, bricks)
	p.box(w, 11, 5, 0, 12, 12, 12, bricks, bricks)
	p.box(w, 2, 5, 11, 4, 12, 12, bricks, bricks)
	p.box(w, 8, 5, 11, 10, 12, 12, bricks, bricks)
	p.box(w, 5, 9, 11, 7, 12, 12, bricks, bricks)
	p.box(w, 2, 5, 0, 4, 12, 1, bricks, bricks)
	p.box(w, 8, 5, 0, 10, 12, 1, bricks, bricks)
	p.box(w, 5, 9, 0, 7, 12, 1, bricks, bricks)
	p.box(w, 2, 11, 2, 10, 12, 10, bricks, bricks)
	p.box(w, 5, 8, 0, 7, 8, 0, fenceB(0), fenceB(0))
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	for i := 1; i <= 11; i += 2 {
		p.box(w, i, 10, 0, i, 11, 0, weFence, weFence)
		p.box(w, i, 10, 12, i, 11, 12, weFence, weFence)
		p.box(w, 0, 10, i, 0, 11, i, nsFence, nsFence)
		p.box(w, 12, 10, i, 12, 11, i, nsFence, nsFence)
		p.place(w, bricks, i, 13, 0)
		p.place(w, bricks, i, 13, 12)
		p.place(w, bricks, 0, 13, i)
		p.place(w, bricks, 12, 13, i)
		if i == 11 {
			continue
		}
		p.place(w, weFence, i+1, 13, 0)
		p.place(w, weFence, i+1, 13, 12)
		p.place(w, nsFence, 0, 13, i+1)
		p.place(w, nsFence, 12, 13, i+1)
	}
	p.place(w, fenceB(fN|fE), 0, 13, 0)
	p.place(w, fenceB(fS|fE), 0, 13, 12)
	p.place(w, fenceB(fS|fW), 12, 13, 12)
	p.place(w, fenceB(fN|fW), 12, 13, 0)
	for z2 := 3; z2 <= 9; z2 += 2 {
		p.box(w, 1, 7, z2, 1, 8, z2, nsFence.with(fW), nsFence.with(fW))
		p.box(w, 11, 7, z2, 11, 8, z2, nsFence.with(fE), nsFence.with(fE))
	}
	p.box(w, 4, 2, 0, 8, 2, 12, bricks, bricks)
	p.box(w, 0, 2, 4, 12, 2, 8, bricks, bricks)
	p.box(w, 4, 0, 0, 8, 1, 3, bricks, bricks)
	p.box(w, 4, 0, 9, 8, 1, 12, bricks, bricks)
	p.box(w, 0, 0, 4, 3, 1, 8, bricks, bricks)
	p.box(w, 9, 0, 4, 12, 1, 8, bricks, bricks)
	for x = 4; x <= 8; x++ {
		for z = 0; z <= 2; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, x, -1, 12-z)
		}
	}
	for x = 0; x <= 2; x++ {
		for z = 4; z <= 8; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, 12-x, -1, z)
		}
	}
	p.box(w, 5, 5, 5, 7, 5, 7, bricks, bricks)
	p.box(w, 6, 1, 6, 6, 4, 6, air, air)
	p.place(w, bricks, 6, 0, 6)
	p.place(w, lava, 6, 5, 6)
}

func (p *fortressPiece) postCastleSmallCorridor(w *fortressCtx) {
	p.box(w, 0, 0, 0, 4, 1, 4, bricks, bricks)
	p.box(w, 0, 2, 0, 4, 5, 4, air, air)
	nsFence := fenceB(fN | fS)
	p.box(w, 0, 2, 0, 0, 5, 4, bricks, bricks)
	p.box(w, 4, 2, 0, 4, 5, 4, bricks, bricks)
	p.box(w, 0, 3, 1, 0, 4, 1, nsFence, nsFence)
	p.box(w, 0, 3, 3, 0, 4, 3, nsFence, nsFence)
	p.box(w, 4, 3, 1, 4, 4, 1, nsFence, nsFence)
	p.box(w, 4, 3, 3, 4, 4, 3, nsFence, nsFence)
	p.box(w, 0, 6, 0, 4, 6, 4, bricks, bricks)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 4; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleSmallCorridorTurn(w *fortressCtx, left bool) {
	wall, front, backMin, backMax, chest := 0, 4, 1, 4, 1
	if left {
		wall, front, backMin, backMax, chest = 4, 0, 0, 3, 3
	}
	p.box(w, 0, 0, 0, 4, 1, 4, bricks, bricks)
	p.box(w, 0, 2, 0, 4, 5, 4, air, air)
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	p.box(w, wall, 2, 0, wall, 5, 4, bricks, bricks)
	p.box(w, wall, 3, 1, wall, 4, 1, nsFence, nsFence)
	p.box(w, wall, 3, 3, wall, 4, 3, nsFence, nsFence)
	p.box(w, front, 2, 0, front, 5, 0, bricks, bricks)
	p.box(w, backMin, 2, 4, backMax, 5, 4, bricks, bricks)
	p.box(w, 1, 3, 4, 1, 4, 4, weFence, weFence)
	p.box(w, 3, 3, 4, 3, 4, 4, weFence, weFence)
	if p.needsChest {
		p.chest(w, chest, 2, 3)
	}
	p.box(w, 0, 6, 0, 4, 6, 4, bricks, bricks)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 4; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleCorridorStairs(w *fortressCtx) {
	stairs := stairsB(dS)
	nsFence := fenceB(fN | fS)
	for step := 0; step <= 9; step++ {
		floor := max(1, 7-step)
		roof := min(max(floor+5, 14-step), 13)
		z := step
		p.box(w, 0, 0, z, 4, floor, z, bricks, bricks)
		p.box(w, 1, floor+1, z, 3, roof-1, z, air, air)
		if step <= 6 {
			p.place(w, stairs, 1, floor+1, z)
			p.place(w, stairs, 2, floor+1, z)
			p.place(w, stairs, 3, floor+1, z)
		}
		p.box(w, 0, roof, z, 4, roof, z, bricks, bricks)
		p.box(w, 0, floor+1, z, 0, roof-1, z, bricks, bricks)
		p.box(w, 4, floor+1, z, 4, roof-1, z, bricks, bricks)
		if (step & 1) == 0 {
			p.box(w, 0, floor+2, z, 0, floor+3, z, nsFence, nsFence)
			p.box(w, 4, floor+2, z, 4, floor+3, z, nsFence, nsFence)
		}
		for x := 0; x <= 4; x++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleCorridorTBalcony(w *fortressCtx) {
	nsFence := fenceB(fN | fS)
	weFence := fenceB(fW | fE)
	p.box(w, 0, 0, 0, 8, 1, 8, bricks, bricks)
	p.box(w, 0, 2, 0, 8, 5, 8, air, air)
	p.box(w, 0, 6, 0, 8, 6, 5, bricks, bricks)
	p.box(w, 0, 2, 0, 2, 5, 0, bricks, bricks)
	p.box(w, 6, 2, 0, 8, 5, 0, bricks, bricks)
	p.box(w, 1, 3, 0, 1, 4, 0, weFence, weFence)
	p.box(w, 7, 3, 0, 7, 4, 0, weFence, weFence)
	p.box(w, 0, 2, 4, 8, 2, 8, bricks, bricks)
	p.box(w, 1, 1, 4, 2, 2, 4, air, air)
	p.box(w, 6, 1, 4, 7, 2, 4, air, air)
	p.box(w, 1, 3, 8, 7, 3, 8, weFence, weFence)
	p.place(w, fenceB(fE|fS), 0, 3, 8)
	p.place(w, fenceB(fW|fS), 8, 3, 8)
	p.box(w, 0, 3, 6, 0, 3, 7, nsFence, nsFence)
	p.box(w, 8, 3, 6, 8, 3, 7, nsFence, nsFence)
	p.box(w, 0, 3, 4, 0, 5, 5, bricks, bricks)
	p.box(w, 8, 3, 4, 8, 5, 5, bricks, bricks)
	p.box(w, 1, 3, 5, 2, 5, 5, bricks, bricks)
	p.box(w, 6, 3, 5, 7, 5, 5, bricks, bricks)
	p.box(w, 1, 4, 5, 1, 5, 5, weFence, weFence)
	p.box(w, 7, 4, 5, 7, 5, 5, weFence, weFence)
	for z := 0; z <= 5; z++ {
		for x := 0; x <= 8; x++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleSmallCorridorCrossing(w *fortressCtx) {
	p.box(w, 0, 0, 0, 4, 1, 4, bricks, bricks)
	p.box(w, 0, 2, 0, 4, 5, 4, air, air)
	p.box(w, 0, 2, 0, 0, 5, 0, bricks, bricks)
	p.box(w, 4, 2, 0, 4, 5, 0, bricks, bricks)
	p.box(w, 0, 2, 4, 0, 5, 4, bricks, bricks)
	p.box(w, 4, 2, 4, 4, 5, 4, bricks, bricks)
	p.box(w, 0, 6, 0, 4, 6, 4, bricks, bricks)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 4; z++ {
			p.fillColumnDown(w, x, -1, z)
		}
	}
}

func (p *fortressPiece) postCastleStalkRoom(w *fortressCtx) {
	var z int
	var x int
	p.box(w, 0, 3, 0, 12, 4, 12, bricks, bricks)
	p.box(w, 0, 5, 0, 12, 13, 12, air, air)
	p.box(w, 0, 5, 0, 1, 12, 12, bricks, bricks)
	p.box(w, 11, 5, 0, 12, 12, 12, bricks, bricks)
	p.box(w, 2, 5, 11, 4, 12, 12, bricks, bricks)
	p.box(w, 8, 5, 11, 10, 12, 12, bricks, bricks)
	p.box(w, 5, 9, 11, 7, 12, 12, bricks, bricks)
	p.box(w, 2, 5, 0, 4, 12, 1, bricks, bricks)
	p.box(w, 8, 5, 0, 10, 12, 1, bricks, bricks)
	p.box(w, 5, 9, 0, 7, 12, 1, bricks, bricks)
	p.box(w, 2, 11, 2, 10, 12, 10, bricks, bricks)
	weFence := fenceB(fW | fE)
	nsFence := fenceB(fN | fS)
	nswFence := nsFence.with(fW)
	nseFence := nsFence.with(fE)
	for i := 1; i <= 11; i += 2 {
		p.box(w, i, 10, 0, i, 11, 0, weFence, weFence)
		p.box(w, i, 10, 12, i, 11, 12, weFence, weFence)
		p.box(w, 0, 10, i, 0, 11, i, nsFence, nsFence)
		p.box(w, 12, 10, i, 12, 11, i, nsFence, nsFence)
		p.place(w, bricks, i, 13, 0)
		p.place(w, bricks, i, 13, 12)
		p.place(w, bricks, 0, 13, i)
		p.place(w, bricks, 12, 13, i)
		if i == 11 {
			continue
		}
		p.place(w, weFence, i+1, 13, 0)
		p.place(w, weFence, i+1, 13, 12)
		p.place(w, nsFence, 0, 13, i+1)
		p.place(w, nsFence, 12, 13, i+1)
	}
	p.place(w, fenceB(fN|fE), 0, 13, 0)
	p.place(w, fenceB(fS|fE), 0, 13, 12)
	p.place(w, fenceB(fS|fW), 12, 13, 12)
	p.place(w, fenceB(fN|fW), 12, 13, 0)
	for z2 := 3; z2 <= 9; z2 += 2 {
		p.box(w, 1, 7, z2, 1, 8, z2, nswFence, nswFence)
		p.box(w, 11, 7, z2, 11, 8, z2, nseFence, nseFence)
	}
	stairs := stairsB(dN)
	for i := 0; i <= 6; i++ {
		z3 := i + 4
		for x = 5; x <= 7; x++ {
			p.place(w, stairs, x, 5+i, z3)
		}
		if z3 >= 5 && z3 <= 8 {
			p.box(w, 5, 5, z3, 7, i+4, z3, bricks, bricks)
		} else if z3 >= 9 && z3 <= 10 {
			p.box(w, 5, 8, z3, 7, i+4, z3, bricks, bricks)
		}
		if i < 1 {
			continue
		}
		p.box(w, 5, 6+i, z3, 7, 9+i, z3, air, air)
	}
	for x2 := 5; x2 <= 7; x2++ {
		p.place(w, stairs, x2, 12, 11)
	}
	p.box(w, 5, 6, 7, 5, 7, 7, nseFence, nseFence)
	p.box(w, 7, 6, 7, 7, 7, 7, nswFence, nswFence)
	p.box(w, 5, 13, 12, 7, 13, 12, air, air)
	p.box(w, 2, 5, 2, 3, 5, 3, bricks, bricks)
	p.box(w, 2, 5, 9, 3, 5, 10, bricks, bricks)
	p.box(w, 2, 5, 4, 2, 5, 8, bricks, bricks)
	p.box(w, 9, 5, 2, 10, 5, 3, bricks, bricks)
	p.box(w, 9, 5, 9, 10, 5, 10, bricks, bricks)
	p.box(w, 10, 5, 4, 10, 5, 8, bricks, bricks)
	eastStairs := stairsB(dE)
	westStairs := stairsB(dW)
	p.place(w, westStairs, 4, 5, 2)
	p.place(w, westStairs, 4, 5, 3)
	p.place(w, westStairs, 4, 5, 9)
	p.place(w, westStairs, 4, 5, 10)
	p.place(w, eastStairs, 8, 5, 2)
	p.place(w, eastStairs, 8, 5, 3)
	p.place(w, eastStairs, 8, 5, 9)
	p.place(w, eastStairs, 8, 5, 10)
	p.box(w, 3, 4, 4, 4, 4, 8, soulSand, soulSand)
	p.box(w, 8, 4, 4, 9, 4, 8, soulSand, soulSand)
	p.box(w, 3, 5, 4, 4, 5, 8, wart, wart)
	p.box(w, 8, 5, 4, 9, 5, 8, wart, wart)
	p.box(w, 4, 2, 0, 8, 2, 12, bricks, bricks)
	p.box(w, 0, 2, 4, 12, 2, 8, bricks, bricks)
	p.box(w, 4, 0, 0, 8, 1, 3, bricks, bricks)
	p.box(w, 4, 0, 9, 8, 1, 12, bricks, bricks)
	p.box(w, 0, 0, 4, 3, 1, 8, bricks, bricks)
	p.box(w, 9, 0, 4, 12, 1, 8, bricks, bricks)
	for x = 4; x <= 8; x++ {
		for z = 0; z <= 2; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, x, -1, 12-z)
		}
	}
	for x = 0; x <= 2; x++ {
		for z = 4; z <= 8; z++ {
			p.fillColumnDown(w, x, -1, z)
			p.fillColumnDown(w, 12-x, -1, z)
		}
	}
}

func (p *fortressPiece) postBridgeEndFiller(w *fortressCtx) {
	var x, y int
	self := NewLegacyRandom(int64(p.selfSeed))
	for x = 0; x <= 4; x++ {
		for y = 3; y <= 4; y++ {
			z := int(self.NextIntN(8))
			p.box(w, x, y, 0, x, y, z, bricks, bricks)
		}
	}
	z2 := int(self.NextIntN(8))
	p.box(w, 0, 5, 0, 0, 5, z2, bricks, bricks)
	z2 = int(self.NextIntN(8))
	p.box(w, 4, 5, 0, 4, 5, z2, bricks, bricks)
	for x = 0; x <= 4; x++ {
		z3 := int(self.NextIntN(5))
		p.box(w, x, 2, 0, x, 2, z3, bricks, bricks)
	}
	for x = 0; x <= 4; x++ {
		for y = 0; y <= 1; y++ {
			z := int(self.NextIntN(3))
			p.box(w, x, y, 0, x, y, z, bricks, bricks)
		}
	}
}
