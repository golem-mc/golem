package levelgen

// The stronghold's piece tree, grown from its start staircase.
// vanilla: StrongholdStructure, StrongholdPieces

const (
	strongholdMaxDepth = 50
	// strongholdReach is how far, in blocks, a piece's foot may lie from the
	// start piece along either axis.
	strongholdReach = 112
	// strongholdLowestY is the lowest y a piece's box may start at.
	strongholdLowestY = 10
	strongholdStartY  = 64
	// strongholdBelowSea is how far under the sea level the tree's top is kept.
	strongholdBelowSea = 10
	// strongholdNoKind marks no imposed kind.
	strongholdNoKind = shFiller
)

type shKind uint8

const (
	shStraight shKind = iota
	shPrisonHall
	shLeftTurn
	shRightTurn
	shRoomCrossing
	shStraightStairsDown
	shStairsDown
	shFiveCrossing
	shChestCorridor
	shLibrary
	shPortalRoom
	shStart
	shFiller
)

var shIDs = [...]string{
	shStraight: "minecraft:shs", shPrisonHall: "minecraft:shph", shLeftTurn: "minecraft:shlt",
	shRightTurn: "minecraft:shrt", shRoomCrossing: "minecraft:shrc", shStraightStairsDown: "minecraft:shssd",
	shStairsDown: "minecraft:shsd", shFiveCrossing: "minecraft:sh5c", shChestCorridor: "minecraft:shcc",
	shLibrary: "minecraft:shli", shPortalRoom: "minecraft:shpr", shStart: "minecraft:shstart",
	shFiller: "minecraft:shfc",
}

// shBoxes holds, per kind, the offset and the size of the box a piece takes
// at its foot.
var shBoxes = [...]pieceShape{
	shStraight:           {-1, -1, 0, 5, 5, 7},
	shPrisonHall:         {-1, -1, 0, 9, 5, 11},
	shLeftTurn:           {-1, -1, 0, 5, 5, 5},
	shRightTurn:          {-1, -1, 0, 5, 5, 5},
	shRoomCrossing:       {-4, -1, 0, 11, 7, 11},
	shStraightStairsDown: {-1, -7, 0, 5, 11, 8},
	shStairsDown:         {-1, -7, 0, 5, 11, 5},
	shFiveCrossing:       {-4, -3, 0, 10, 9, 11},
	shChestCorridor:      {-1, -1, 0, 5, 5, 7},
	shLibrary:            {-4, -1, 0, 14, 11, 15},
	shPortalRoom:         {-4, -1, 0, 11, 8, 16},
}

// shDoor is vanilla: StrongholdPiece.SmallDoorType.
type shDoor uint8

const (
	shOpening shDoor = iota
	shWoodDoor
	shGrates
	shIronDoor
)

var shDoorNames = [...]string{"OPENING", "WOOD_DOOR", "GRATES", "IRON_DOOR"}

// strongholdPiece is one piece of a stronghold; the flags and counts it keeps
// depend on its kind. vanilla: StrongholdPieces.StrongholdPiece
type strongholdPiece struct {
	kind     shKind
	bb       BoundingBox
	dir      fdir
	genDepth int
	door     shDoor
	// left, right (straight), leftLow, leftHigh, rightLow, rightHigh (five
	// crossing), tall (library).
	flags [4]bool
	// roomType is the room crossing's layout; steps the filler's length.
	roomType, steps int
}

// StrongholdTag is one saved field of a piece beyond its id, box, orientation,
// depth and entry door; Byte tells a byte from an int.
type StrongholdTag struct {
	Name  string
	Value int
	Byte  bool
}

// StrongholdPieceSave is a piece as a chunk saves it.
// vanilla: StructurePiece.createTag
type StrongholdPieceSave struct {
	ID          string
	BB          BoundingBox
	Orientation int
	GenDepth    int
	EntryDoor   string
	Tags        []StrongholdTag
}

func (p *strongholdPiece) save() StrongholdPieceSave {
	s := StrongholdPieceSave{ID: shIDs[p.kind], BB: p.bb, Orientation: fortressOrientation[p.dir],
		GenDepth: p.genDepth, EntryDoor: shDoorNames[p.door]}
	flag := func(name string, v bool) {
		b := 0
		if v {
			b = 1
		}
		s.Tags = append(s.Tags, StrongholdTag{name, b, true})
	}
	switch p.kind {
	case shStraight:
		flag("Left", p.flags[0])
		flag("Right", p.flags[1])
	case shRoomCrossing:
		s.Tags = append(s.Tags, StrongholdTag{"Type", p.roomType, false})
	case shStairsDown, shStart:
		flag("Source", p.kind == shStart)
	case shFiveCrossing:
		flag("leftLow", p.flags[0])
		flag("leftHigh", p.flags[1])
		flag("rightLow", p.flags[2])
		flag("rightHigh", p.flags[3])
	case shChestCorridor:
		flag("Chest", false)
	case shLibrary:
		flag("Tall", p.flags[0])
	case shPortalRoom:
		flag("Mob", false)
	case shFiller:
		s.Tags = append(s.Tags, StrongholdTag{"Steps", p.steps, false})
	}
	return s
}

// shWeight is one entry of the piece table. vanilla: StrongholdPieces.PieceWeight
type shWeight struct {
	kind              shKind
	weight, maxPlaced int
	placed            int
	// minDepth is the depth a piece must be deeper than.
	minDepth int
}

func (w *shWeight) valid() bool { return w.maxPlaced == 0 || w.placed < w.maxPlaced }

func (w *shWeight) doPlace(depth int) bool { return w.valid() && depth > w.minDepth }

// strongholdTree is the state of one growth of the tree; vanilla keeps it in
// static fields. vanilla: StrongholdPieces.resetPieces
type strongholdTree struct {
	rng     *LegacyRandomSource
	weights []*shWeight
	// imposed is the kind the next piece is forced to, or strongholdNoKind.
	imposed shKind
	// previous is the weight picked last, which the next pick may not repeat.
	previous *shWeight
	pieces   []*strongholdPiece
	pending  []*strongholdPiece
	start    *strongholdPiece
	portal   *strongholdPiece
}

func newStrongholdTree(rng *LegacyRandomSource) *strongholdTree {
	return &strongholdTree{rng: rng, imposed: strongholdNoKind, weights: []*shWeight{
		{kind: shStraight, weight: 40}, {kind: shPrisonHall, weight: 5, maxPlaced: 5},
		{kind: shLeftTurn, weight: 20}, {kind: shRightTurn, weight: 20},
		{kind: shRoomCrossing, weight: 10, maxPlaced: 6}, {kind: shStraightStairsDown, weight: 5, maxPlaced: 5},
		{kind: shStairsDown, weight: 5, maxPlaced: 5}, {kind: shFiveCrossing, weight: 5, maxPlaced: 4},
		{kind: shChestCorridor, weight: 5, maxPlaced: 4}, {kind: shLibrary, weight: 10, maxPlaced: 2, minDepth: 4},
		{kind: shPortalRoom, weight: 20, maxPlaced: 1, minDepth: 5},
	}}
}

func (t *strongholdTree) collision(box BoundingBox) *strongholdPiece {
	p, _ := findCollision(t.pieces, box, func(p *strongholdPiece) BoundingBox { return p.bb })
	return p
}

// randomDoor is vanilla: StrongholdPiece.randomSmallDoor.
func (t *strongholdTree) randomDoor() shDoor {
	switch t.rng.NextIntN(5) {
	case 2:
		return shWoodDoor
	case 3:
		return shGrates
	case 4:
		return shIronDoor
	}
	return shOpening
}

// create is each piece's createPiece: the piece at a foot, or nil when its box
// is too low or collides.
func (t *strongholdTree) create(kind shKind, footX, footY, footZ int, d fdir, depth int) *strongholdPiece {
	s := shBoxes[kind]
	box := orientBox(footX, footY, footZ, s.offX, s.offY, s.offZ, s.width, s.height, s.depth, d)
	ok := func(b BoundingBox) bool { return b.MinY > strongholdLowestY && t.collision(b) == nil }
	if kind == shLibrary && !ok(box) {
		box = orientBox(footX, footY, footZ, s.offX, s.offY, s.offZ, s.width, 6, s.depth, d)
	}
	if !ok(box) {
		return nil
	}
	p := &strongholdPiece{kind: kind, bb: box, dir: d, genDepth: depth}
	if kind == shPortalRoom {
		return p
	}
	p.door = t.randomDoor()
	switch kind {
	case shStraight:
		p.flags[0] = t.rng.NextIntN(2) == 0
		p.flags[1] = t.rng.NextIntN(2) == 0
	case shRoomCrossing:
		p.roomType = int(t.rng.NextIntN(5))
	case shFiveCrossing:
		p.flags[0] = t.rng.NextBoolean()
		p.flags[1] = t.rng.NextBoolean()
		p.flags[2] = t.rng.NextBoolean()
		p.flags[3] = t.rng.NextIntN(3) > 0
	case shLibrary:
		p.flags[0] = box.MaxY-box.MinY+1 > 6
	}
	return p
}

// fromSmallDoor is vanilla: StrongholdPieces.generatePieceFromSmallDoor.
func (t *strongholdTree) fromSmallDoor(footX, footY, footZ int, d fdir, depth int) *strongholdPiece {
	total := 0
	hasAny := false
	for _, w := range t.weights {
		if w.maxPlaced > 0 && w.placed < w.maxPlaced {
			hasAny = true
		}
		total += w.weight
	}
	if !hasAny {
		return nil
	}
	if t.imposed != strongholdNoKind {
		p := t.create(t.imposed, footX, footY, footZ, d, depth)
		t.imposed = strongholdNoKind
		if p != nil {
			return p
		}
	}
attempts:
	for range 5 {
		sel := int(t.rng.NextIntN(int32(total)))
		for i, w := range t.weights {
			if sel -= w.weight; sel >= 0 {
				continue
			}
			if !w.doPlace(depth) || w == t.previous {
				continue attempts
			}
			p := t.create(w.kind, footX, footY, footZ, d, depth)
			if p == nil {
				continue
			}
			w.placed++
			t.previous = w
			if !w.valid() {
				t.weights = append(t.weights[:i], t.weights[i+1:]...)
			}
			return p
		}
	}
	if box, ok := t.fillerBox(footX, footY, footZ, d); ok && box.MinY > 1 {
		steps := box.MaxX - box.MinX + 1
		if d == dN || d == dS {
			steps = box.MaxZ - box.MinZ + 1
		}
		return &strongholdPiece{kind: shFiller, bb: box, dir: d, genDepth: depth, steps: steps}
	}
	return nil
}

// fillerBox is vanilla: FillerCorridor.findPieceBox.
func (t *strongholdTree) fillerBox(footX, footY, footZ int, d fdir) (BoundingBox, bool) {
	box := orientBox(footX, footY, footZ, -1, -1, 0, 5, 5, 4, d)
	hit := t.collision(box)
	if hit == nil || hit.bb.MinY != box.MinY {
		return BoundingBox{}, false
	}
	for depth := 2; depth >= 1; depth-- {
		if !hit.bb.intersects(orientBox(footX, footY, footZ, -1, -1, 0, 5, 5, depth, d)) {
			return orientBox(footX, footY, footZ, -1, -1, 0, 5, 5, depth+1, d), true
		}
	}
	return BoundingBox{}, false
}

// add is vanilla: StrongholdPieces.generateAndAddPiece.
func (t *strongholdTree) add(footX, footY, footZ int, d fdir, depth int) {
	if depth > strongholdMaxDepth {
		return
	}
	if abs(footX-t.start.bb.MinX) > strongholdReach || abs(footZ-t.start.bb.MinZ) > strongholdReach {
		return
	}
	if p := t.fromSmallDoor(footX, footY, footZ, d, depth+1); p != nil {
		t.pieces = append(t.pieces, p)
		t.pending = append(t.pending, p)
	}
}

func (t *strongholdTree) child(p *strongholdPiece, side childSide, yOff, off int) {
	x, y, z, d := childFoot(p.bb, p.dir, side, yOff, off)
	t.add(x, y, z, d, p.genDepth)
}

// addChildren is each piece's addChildren.
func (t *strongholdTree) addChildren(p *strongholdPiece) {
	northEast := p.dir == dN || p.dir == dE
	switch p.kind {
	case shStraight:
		t.child(p, sideForward, 1, 1)
		if p.flags[0] {
			t.child(p, sideLeft, 1, 2)
		}
		if p.flags[1] {
			t.child(p, sideRight, 1, 2)
		}
	case shPrisonHall, shStraightStairsDown, shChestCorridor, shStairsDown:
		t.child(p, sideForward, 1, 1)
	case shStart:
		t.imposed = shFiveCrossing
		t.child(p, sideForward, 1, 1)
	case shLeftTurn, shRightTurn:
		if (p.kind == shLeftTurn) == northEast {
			t.child(p, sideLeft, 1, 1)
		} else {
			t.child(p, sideRight, 1, 1)
		}
	case shRoomCrossing:
		t.child(p, sideForward, 1, 4)
		t.child(p, sideLeft, 1, 4)
		t.child(p, sideRight, 1, 4)
	case shFiveCrossing:
		// vanilla swaps the two offsets for west and north.
		zA, zB := 3, 5
		if p.dir == dW || p.dir == dN {
			zA, zB = 8-zA, 8-zB
		}
		t.child(p, sideForward, 1, 5)
		if p.flags[0] {
			t.child(p, sideLeft, zA, 1)
		}
		if p.flags[1] {
			t.child(p, sideLeft, zB, 7)
		}
		if p.flags[2] {
			t.child(p, sideRight, zA, 1)
		}
		if p.flags[3] {
			t.child(p, sideRight, zB, 7)
		}
	case shPortalRoom:
		t.portal = p
	}
}

// NewStrongholdPieces grows the stronghold that starts in a chunk, retrying
// with the seed plus the try until a portal room fits, and moves it below the
// generator's sea level and above its minimum Y.
// vanilla: StrongholdStructure.generatePieces
func NewStrongholdPieces(seed int64, chunkX, chunkZ int32, seaLevel, minY int) []StrongholdPieceSave {
	rng := NewLegacyRandom(0)
	for tries := int64(0); ; tries++ {
		rng.SetLargeFeatureSeed(seed+tries, chunkX, chunkZ)
		t := newStrongholdTree(rng)
		start := &strongholdPiece{kind: shStart, dir: fdir(rng.NextIntN(4)), door: shOpening}
		x, z := int(chunkX)*16+2, int(chunkZ)*16+2
		// 5 by 11 by 5, so the same whichever way it faces.
		// vanilla: StructurePiece.makeBoundingBox
		start.bb = BoundingBox{x, strongholdStartY, z, x + 4, strongholdStartY + 10, z + 4}
		t.start = start
		t.pieces = append(t.pieces, start)
		t.addChildren(start)
		for len(t.pending) > 0 {
			i := int(rng.NextIntN(int32(len(t.pending))))
			p := t.pending[i]
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			t.addChildren(p)
		}
		t.moveBelowSeaLevel(seaLevel, minY)
		if t.portal != nil {
			out := make([]StrongholdPieceSave, len(t.pieces))
			for i, p := range t.pieces {
				out[i] = p.save()
			}
			return out
		}
	}
}

// moveBelowSeaLevel is vanilla: StructurePiecesBuilder.moveBelowSeaLevel.
func (t *strongholdTree) moveBelowSeaLevel(seaLevel, minY int) {
	box := unionOf(t.pieces, func(p *strongholdPiece) BoundingBox { return p.bb })
	maxY := seaLevel - strongholdBelowSea
	y1 := box.MaxY - box.MinY + 1 + minY + 1
	if y1 < maxY {
		y1 += int(t.rng.NextIntN(int32(maxY - y1)))
	}
	dy := y1 - box.MaxY
	for _, p := range t.pieces {
		p.bb.MinY += dy
		p.bb.MaxY += dy
	}
}
