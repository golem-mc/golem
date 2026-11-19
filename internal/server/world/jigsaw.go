package world

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Jigsaw assembly: a start piece and the pieces its jigsaw blocks attach, as
// vanilla's JigsawPlacement grows them. The result depends only on the seed and
// the start chunk, and on the surface height where a start is projected to the
// heightmap or a piece matches the terrain.
// vanilla: JigsawPlacement

// jbox is a BoundingBox: inclusive block coordinates.
type jbox struct{ minX, minY, minZ, maxX, maxY, maxZ int32 }

func (b jbox) moved(dx, dy, dz int32) jbox {
	return jbox{b.minX + dx, b.minY + dy, b.minZ + dz, b.maxX + dx, b.maxY + dy, b.maxZ + dz}
}

func (b jbox) inside(x, y, z int32) bool {
	return x >= b.minX && x <= b.maxX && y >= b.minY && y <= b.maxY && z >= b.minZ && z <= b.maxZ
}

func (b jbox) intersects(o jbox) bool {
	return b.maxX >= o.minX && b.minX <= o.maxX && b.maxZ >= o.minZ && b.minZ <= o.maxZ &&
		b.maxY >= o.minY && b.minY <= o.maxY
}

func (b jbox) union(o jbox) jbox {
	return jbox{min(b.minX, o.minX), min(b.minY, o.minY), min(b.minZ, o.minZ),
		max(b.maxX, o.maxX), max(b.maxY, o.maxY), max(b.maxZ, o.maxZ)}
}

// contains reports whether o lies within b.
func (b jbox) contains(o jbox) bool {
	return o.minX >= b.minX && o.maxX <= b.maxX && o.minY >= b.minY && o.maxY <= b.maxY && o.minZ >= b.minZ && o.maxZ <= b.maxZ
}

// jigsawFree is the space a piece may grow into: a bound minus the boxes taken
// from it. A candidate fits when it lies inside the bound and meets no taken
// box. vanilla: JigsawPlacement.Placer.tryPlacingChildren (its VoxelShape test
// deflates the candidate by 0.25, which for block-aligned boxes is this).
type jigsawFree struct {
	bound jbox
	taken []jbox
}

func (f *jigsawFree) fits(b jbox) bool {
	if !f.bound.contains(b) {
		return false
	}
	for _, t := range f.taken {
		if t.intersects(b) {
			return false
		}
	}
	return true
}

// jigsawPiece is a placed template. vanilla: PoolElementStructurePiece
type jigsawPiece struct {
	element  *jigsawElement
	pos      [3]int32
	rotation Rotation
	box      jbox
	// groundLevelDelta and junctions are saved with the piece and read by the
	// beardifier.
	groundLevelDelta int32
	junctions        []jigsawJunction
	// ignoreWaterlogging is saved as the piece's liquid_settings.
	ignoreWaterlogging bool
	// keepJigsaws writes the template's jigsaw blocks instead of their
	// final_state.
	keepJigsaws bool
}

// beard is the piece as the beardifier reads it; every piece is rigid.
// vanilla: Beardifier.forStructuresInChunk
func (p *jigsawPiece) beard() levelgen.BeardPiece {
	bp := levelgen.BeardPiece{Rigid: !p.element.matching, GroundLevelDelta: int(p.groundLevelDelta)}
	for _, j := range p.junctions {
		bp.Junctions = append(bp.Junctions, levelgen.BeardJunction{X: int(j.sourceX), GroundY: int(j.sourceGroundY), Z: int(j.sourceZ)})
	}
	return bp
}

// jigsawJunction is a connection to another piece: the joining jigsaw's column,
// the ground height there and the height change across the joint.
// vanilla: JigsawJunction
type jigsawJunction struct {
	sourceX, sourceGroundY, sourceZ, deltaY int32
	// matching is the joined piece's projection being terrain_matching.
	matching bool
}

// jigsawStart is a grown structure: its pieces in the order they were added
// and the box around them.
type jigsawStart struct {
	pieces []*jigsawPiece
	box    jbox
	// centre is the block whose biome the start needs: the centre of the first
	// piece's box in X and Z, at the start jigsaw's height.
	centre [3]int32
	// chunkX, chunkZ is the chunk the start belongs to.
	chunkX, chunkZ int32
}

// boundingBox is the template's box at pos turned by rotation.
// vanilla: StructureTemplate.getBoundingBox
func (t *jigsawTemplate) boundingBox(pos [3]int32, r Rotation) jbox {
	x0, _, z0 := TransformTemplatePos(0, 0, 0, MirrorNone, r)
	x1, _, z1 := TransformTemplatePos(t.Size[0]-1, t.Size[1]-1, t.Size[2]-1, MirrorNone, r)
	return jbox{
		min(x0, x1) + pos[0], pos[1], min(z0, z1) + pos[2],
		max(x0, x1) + pos[0], pos[1] + t.Size[1] - 1, max(z0, z1) + pos[2],
	}
}

// featureJigsaw is the jigsaw a feature element offers: nothing attaches to it.
// vanilla: FeaturePoolElement.getShuffledJigsawBlocks
var featureJigsaw = jigsawBlock{
	front: jigsawDown, top: jigsawSouth, rollable: true,
	name: "minecraft:bottom", pool: "minecraft:empty", target: "minecraft:empty", finalState: "minecraft:air",
}

// jigsawsAt appends the element's jigsaw blocks at pos turned by r to dst[:0].
// vanilla: StructurePoolElement.getShuffledJigsawBlocks
func (e *jigsawElement) jigsawsAt(dst []jigsawAt, pos [3]int32, r Rotation, rng *levelgen.LegacyRandomSource) []jigsawAt {
	switch {
	case e.list != nil:
		return e.list[0].jigsawsAt(dst, pos, r, rng)
	case e.feature != "":
		return append(dst[:0], jigsawAt{&featureJigsaw, pos, jigsawDown, rotateJigsawDir(jigsawSouth, r)})
	}
	return e.template.jigsawsAt(dst, pos, r, rng)
}

// boundingBox is the element's box at pos turned by r: a list's surrounds its
// elements, a feature's is the block itself.
// vanilla: StructurePoolElement.getBoundingBox
func (e *jigsawElement) boundingBox(pos [3]int32, r Rotation) jbox {
	switch {
	case e.list != nil:
		box := e.list[0].boundingBox(pos, r)
		for _, sub := range e.list[1:] {
			b := sub.boundingBox(pos, r)
			box = jbox{min(box.minX, b.minX), min(box.minY, b.minY), min(box.minZ, b.minZ), max(box.maxX, b.maxX), max(box.maxY, b.maxY), max(box.maxZ, b.maxZ)}
		}
		return box
	case e.feature != "":
		return jbox{pos[0], pos[1], pos[2], pos[0], pos[1], pos[2]}
	}
	return e.template.boundingBox(pos, r)
}

// jigsawAt is a jigsaw block placed in the world.
type jigsawAt struct {
	*jigsawBlock
	pos        [3]int32
	front, top jigsawDir
}

// jigsawsAt appends the template's jigsaw blocks at pos turned by r to dst[:0],
// in a random order. vanilla: SinglePoolElement.getShuffledJigsawBlocks
func (t *jigsawTemplate) jigsawsAt(dst []jigsawAt, pos [3]int32, r Rotation, rng *levelgen.LegacyRandomSource) []jigsawAt {
	out := dst[:0]
	for i := range t.jigsaws {
		j := &t.jigsaws[i]
		x, y, z := TransformTemplatePos(j.pos[0], j.pos[1], j.pos[2], MirrorNone, r)
		out = append(out, jigsawAt{j, [3]int32{x + pos[0], y + pos[1], z + pos[2]}, rotateJigsawDir(j.front, r), rotateJigsawDir(j.top, r)})
	}
	shuffle(out, rng)
	// A stable sort of equal keys leaves the shuffle as it is.
	if !t.uniformSelection {
		slices.SortStableFunc(out, func(a, b jigsawAt) int { return cmp.Compare(b.selection, a.selection) })
	}
	return out
}

// jigsawDir is a jigsaw's front or top, in vanilla's Direction order, so that
// d^1 is the opposite direction.
type jigsawDir uint8

const (
	jigsawDown jigsawDir = iota
	jigsawUp
	jigsawNorth
	jigsawSouth
	jigsawWest
	jigsawEast
)

var jigsawDirNames = [...]string{"down", "up", "north", "south", "west", "east"}

// jigsawDirClockwise turns each direction a quarter clockwise; up and down stay.
var jigsawDirClockwise = [...]jigsawDir{jigsawDown, jigsawUp, jigsawEast, jigsawWest, jigsawNorth, jigsawSouth}

var jigsawDirSteps = [...][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// parseJigsawDir reads a direction name, false for an unknown one.
func parseJigsawDir(name string) (jigsawDir, bool) {
	i := slices.Index(jigsawDirNames[:], name)
	return jigsawDir(i), i >= 0
}

// rotateJigsawDir turns d by r. vanilla: Rotation.rotate(Direction)
func rotateJigsawDir(d jigsawDir, r Rotation) jigsawDir {
	for range r {
		d = jigsawDirClockwise[d]
	}
	return d
}

func oppositeJigsawDir(d jigsawDir) jigsawDir { return d ^ 1 }

// jigsawDirStep is d's unit vector.
func jigsawDirStep(d jigsawDir) (x, y, z int32) {
	s := jigsawDirSteps[d]
	return s[0], s[1], s[2]
}

// shuffle is vanilla's Fisher-Yates variant. vanilla: Util.shuffle
func shuffle[T any](list []T, rng interface{ NextIntN(int32) int32 }) {
	for i := len(list); i > 1; i-- {
		j := rng.NextIntN(int32(i))
		list[i-1], list[j] = list[j], list[i-1]
	}
}

// directionStep is a direction's unit vector.
func directionStep(d string) (x, y, z int32) {
	switch d {
	case "north":
		return 0, 0, -1
	case "south":
		return 0, 0, 1
	case "east":
		return 1, 0, 0
	case "west":
		return -1, 0, 0
	case "up":
		return 0, 1, 0
	}
	return 0, -1, 0
}

// JigsawFrontStep is the offset of the block a jigsaw block state faces.
// vanilla: JigsawBlock.getFrontFacing
func JigsawFrontStep(state uint16) (x, y, z int32) {
	front, _, _ := strings.Cut(registry.StateValue(state, registry.PropOrientation), "_")
	return directionStep(front)
}

// canAttach reports whether target joins source: facing each other, with the
// same top unless the source rolls, and the source's target naming the
// target's name.
// vanilla: JigsawBlock.canAttach
func canAttach(source, target jigsawAt) bool {
	return source.front == oppositeJigsawDir(target.front) && (source.rollable || source.top == target.top) &&
		source.target == target.name
}

// groundLevelDelta is how far a single element's ground level is above its
// bottom, so the start piece sits one below the start height.
// vanilla: StructurePoolElement.getGroundLevelDelta
const groundLevelDelta = 1

// jigsawPieceState is a piece whose jigsaws have yet to be tried.
type jigsawPieceState struct {
	piece    *jigsawPiece
	free     *jigsawFree
	depth    int
	priority int32
}

// buildJigsawStart grows the structure that starts in chunk (cx, cz) of a
// dimension whose blocks span minY..maxY, nil when its start pool is empty,
// has no start jigsaw or does not fit the dimension padding.
// vanilla: JigsawStructure.findGenerationPoint, JigsawPlacement.addPieces
//
// ground is the first free Y of a column in the world surface heightmap; it
// may be nil for a structure that neither projects its start nor has a
// terrain-matching element.
func buildJigsawStart(def *jigsawStructure, seed int64, cx, cz, minY, maxY int32, ground func(x, z int32) int32) *jigsawStart {
	start, expand := planJigsawStart(def, seed, cx, cz, minY, maxY, ground)
	if start != nil {
		expand()
	}
	return start
}

// planJigsawStart places the centre piece and returns the expansion, which
// vanilla runs only once the start's biome is valid. Nothing between the plan
// and the expansion may draw from the start's random source, and start.box is
// valid only after expand.
// vanilla: Structure.GenerationStub
func planJigsawStart(def *jigsawStructure, seed int64, cx, cz, minY, maxY int32, ground func(x, z int32) int32) (*jigsawStart, func()) {
	rng := levelgen.NewLegacyRandom(0)
	rng.SetLargeFeatureSeed(seed, cx, cz)
	height := def.startHeight
	if def.startHeightMax != def.startHeight {
		height += rng.NextIntN(def.startHeightMax - def.startHeight + 1)
	}
	pos := [3]int32{cx * 16, height, cz * 16}
	// vanilla: PoolAliasLookup.create
	lookup := map[string]string{}
	if len(def.aliases) > 0 {
		arng := levelgen.NewLegacyRandom(levelgen.NewLegacyRandom(seed).NextLong() ^ levelgen.PositionSeed(pos[0], pos[1], pos[2]))
		for i := range def.aliases {
			def.aliases[i].resolve(arng, lookup)
		}
	}
	return planJigsawAt(def, rng, pos, lookup, minY, maxY, ground, false)
}

// planJigsawAt is planJigsawStart from the start position on: it rolls the
// rotation and the centre element from rng. keep leaves the pieces' jigsaw
// blocks in place when they are written.
// vanilla: JigsawPlacement.addPieces
func planJigsawAt(def *jigsawStructure, rng *levelgen.LegacyRandomSource, pos [3]int32, lookup map[string]string, minY, maxY int32,
	ground func(x, z int32) int32, keep bool) (*jigsawStart, func()) {
	cx, cz := pos[0]>>4, pos[2]>>4
	rotation := Rotation(rng.NextIntN(4))
	startPool := def.startPool
	if len(def.startPool.templates) == 0 {
		return nil, nil
	}
	centerElement := startPool.templates[rng.NextIntN(int32(len(startPool.templates)))]
	if centerElement == emptyElement {
		return nil, nil
	}
	var anchor [3]int32
	if def.startJigsaw != "" {
		found := false
		for _, j := range centerElement.jigsawsAt(nil, pos, rotation, rng) {
			if j.name == def.startJigsaw {
				anchor, found = j.pos, true
				break
			}
		}
		if !found {
			return nil, nil
		}
	} else {
		anchor = pos
	}
	local := [3]int32{anchor[0] - pos[0], anchor[1] - pos[1], anchor[2] - pos[2]}
	adjusted := [3]int32{pos[0] - local[0], pos[1] - local[1], pos[2] - local[2]}
	box := centerElement.boundingBox(adjusted, rotation)
	centre := &jigsawPiece{element: centerElement, pos: adjusted, rotation: rotation, box: box, groundLevelDelta: groundLevelDelta, ignoreWaterlogging: def.ignoreWaterlogging, keepJigsaws: keep}
	centerX, centerZ := (box.maxX+box.minX)/2, (box.maxZ+box.minZ)/2
	bottomY := adjusted[1]
	if def.projectStart {
		bottomY = pos[1] + ground(centerX, centerZ)
	}
	dy := bottomY - (box.minY + groundLevelDelta)
	centre.pos[1] += dy
	centre.box = box.moved(0, dy, 0)
	if (def.paddingBottom != 0 || def.paddingTop != 0) && (centre.box.minY < minY+def.paddingBottom || centre.box.maxY > maxY-def.paddingTop) {
		return nil, nil
	}
	centerY := bottomY + local[1]
	start := &jigsawStart{pieces: []*jigsawPiece{centre}, chunkX: cx, chunkZ: cz, centre: [3]int32{centerX, centerY, centerZ}}
	return start, func() {
		if def.maxDepth > 0 {
			bound := jbox{centerX - def.maxDistance, max(centerY-def.maxDistance, minY+def.paddingBottom), centerZ - def.maxDistance,
				centerX + def.maxDistance, min(centerY+def.maxDistance, maxY-def.paddingTop), centerZ + def.maxDistance}
			p := &jigsawPlacer{ignoreWaterlogging: def.ignoreWaterlogging, pools: def.pools, lookup: lookup, rng: rng, maxDepth: def.maxDepth, start: start,
				hack: def.expansionHack, ground: ground}
			p.run(centre, &jigsawFree{bound: bound, taken: []jbox{centre.box}})
			for _, pc := range start.pieces {
				pc.keepJigsaws = keep
			}
		}
		start.box = centre.box
		for _, pc := range start.pieces[1:] {
			start.box = start.box.union(pc.box)
		}
	}
}

type jigsawPlacer struct {
	pools              map[string]*jigsawPool
	ignoreWaterlogging bool
	// lookup maps a pool name to the pool an alias stands for.
	lookup   map[string]string
	rng      *levelgen.LegacyRandomSource
	maxDepth int
	start    *jigsawStart
	// hack is use_expansion_hack; ground is the surface height of a column.
	hack   bool
	ground func(x, z int32) int32
	// queue holds the pieces to expand in the order they were added.
	queue []jigsawPieceState
	// sourceJigsaws and targetJigsaws are scratch lists for expand and
	// tryTarget; two, because tryTarget runs inside expand's loop.
	sourceJigsaws, targetJigsaws []jigsawAt
}

func (p *jigsawPlacer) run(centre *jigsawPiece, free *jigsawFree) {
	p.expand(centre, free, 0)
	for len(p.queue) > 0 {
		// The highest priority first, the earliest added within one.
		// vanilla: SequencedPriorityIterator
		best := 0
		for i, q := range p.queue {
			if q.priority > p.queue[best].priority {
				best = i
			}
		}
		st := p.queue[best]
		p.queue = slices.Delete(p.queue, best, best+1)
		p.expand(st.piece, st.free, st.depth)
	}
}

// expand attaches a child to each jigsaw of the piece that finds one.
// vanilla: JigsawPlacement.Placer.tryPlacingChildren
func (p *jigsawPlacer) expand(source *jigsawPiece, contextFree *jigsawFree, depth int) {
	var sourceFree *jigsawFree
	sourceBoxY := source.box.minY
	sourceRigid := !source.element.matching
	p.sourceJigsaws = source.element.jigsawsAt(p.sourceJigsaws, source.pos, source.rotation, p.rng)
	for _, sj := range p.sourceJigsaws {
		sx, sy, sz := jigsawDirStep(sj.front)
		targetPos := [3]int32{sj.pos[0] + sx, sj.pos[1] + sy, sj.pos[2] + sz}
		// A pool that is missing, or empty without being minecraft:empty (the
		// pool with no fallback), or has such a fallback, places nothing.
		poolName := sj.pool
		if to, ok := p.lookup[poolName]; ok {
			poolName = to
		}
		pool := p.pools[poolName]
		if pool == nil || len(pool.templates) == 0 || len(pool.fallback.templates) == 0 && pool.fallback.fallback != nil {
			continue
		}
		var childrenFree *jigsawFree
		if source.box.inside(targetPos[0], targetPos[1], targetPos[2]) {
			if sourceFree == nil {
				sourceFree = &jigsawFree{bound: source.box}
			}
			childrenFree = sourceFree
		} else {
			childrenFree = contextFree
		}
		var candidates []*jigsawElement
		if depth != p.maxDepth {
			candidates = shuffledCopy(pool.templates, p.rng)
		}
		candidates = append(candidates, shuffledCopy(pool.fallback.templates, p.rng)...)
		sourceLocalY := sj.pos[1] - sourceBoxY
		// The surface at the source jigsaw, read when a joint needs it.
		baseHeight := int32(math.MinInt32)
		for _, target := range candidates {
			if target == emptyElement {
				break
			}
			if p.tryTarget(source, sj, target, targetPos, sourceLocalY, sy, childrenFree, depth, sourceRigid, &baseHeight) {
				break
			}
		}
	}
}

// tryTarget tries every rotation and every jigsaw of the target element
// against the source jigsaw, adding the first that attaches and fits.
func (p *jigsawPlacer) tryTarget(source *jigsawPiece, sj jigsawAt, target *jigsawElement, targetPos [3]int32,
	sourceLocalY, sourceStepY int32, free *jigsawFree, depth int, sourceRigid bool, baseHeight *int32) bool {
	targetRigid := !target.matching
	base := func() int32 {
		if *baseHeight == math.MinInt32 {
			*baseHeight = p.ground(sj.pos[0], sj.pos[2])
		}
		return *baseHeight
	}
	for _, rot := range shuffledRotations(p.rng) {
		p.targetJigsaws = target.jigsawsAt(p.targetJigsaws, [3]int32{}, rot, p.rng)
		tjs := p.targetJigsaws
		expandTo := p.expansion(target, tjs, rot)
		for _, tj := range tjs {
			if !canAttach(sj, tj) {
				continue
			}
			rawPos := [3]int32{targetPos[0] - tj.pos[0], targetPos[1] - tj.pos[1], targetPos[2] - tj.pos[2]}
			rawBox := target.boundingBox(rawPos, rot)
			deltaY := sourceLocalY - tj.pos[1] + sourceStepY
			var targetBoxY int32
			if sourceRigid && targetRigid {
				targetBoxY = source.box.minY + deltaY
			} else {
				targetBoxY = base() - tj.pos[1]
			}
			yOffset := targetBoxY - rawBox.minY
			box := rawBox.moved(0, yOffset, 0)
			if expandTo > 0 {
				box.maxY = max(box.maxY, box.minY+max(expandTo+1, box.maxY-box.minY))
			}
			if !free.fits(box) {
				continue
			}
			free.taken = append(free.taken, box)
			groundDelta := int32(groundLevelDelta)
			if targetRigid {
				groundDelta = source.groundLevelDelta - deltaY
			}
			piece := &jigsawPiece{element: target, pos: [3]int32{rawPos[0], rawPos[1] + yOffset, rawPos[2]}, rotation: rot, box: box,
				groundLevelDelta: groundDelta, ignoreWaterlogging: p.ignoreWaterlogging}
			var junctionY int32
			switch {
			case sourceRigid:
				junctionY = source.box.minY + sourceLocalY
			case targetRigid:
				junctionY = targetBoxY + tj.pos[1]
			default:
				junctionY = base() + deltaY/2
			}
			source.junctions = append(source.junctions, jigsawJunction{targetPos[0], junctionY - sourceLocalY + source.groundLevelDelta, targetPos[2], deltaY, target.matching})
			piece.junctions = append(piece.junctions, jigsawJunction{sj.pos[0], junctionY - tj.pos[1] + piece.groundLevelDelta, sj.pos[2], -deltaY, source.element.matching})
			p.start.pieces = append(p.start.pieces, piece)
			if depth+1 <= p.maxDepth {
				p.queue = append(p.queue, jigsawPieceState{piece, free, depth + 1, sj.placement})
			}
			return true
		}
	}
	return false
}

// expansion is how far the expansion hack grows a target's box upward: the
// tallest pool a jigsaw that points inside the element's own box leads to,
// zero for an element more than 16 tall.
// vanilla: JigsawPlacement.Placer.tryPlacingChildren (expandTo)
func (p *jigsawPlacer) expansion(target *jigsawElement, jigsaws []jigsawAt, rot Rotation) int32 {
	hackBox := target.boundingBox([3]int32{}, rot)
	if !p.hack || hackBox.maxY-hackBox.minY+1 > 16 {
		return 0
	}
	var expandTo int32
	for _, tj := range jigsaws {
		sx, sy, sz := jigsawDirStep(tj.front)
		if !hackBox.inside(tj.pos[0]+sx, tj.pos[1]+sy, tj.pos[2]+sz) {
			continue
		}
		name := tj.pool
		if to, ok := p.lookup[name]; ok {
			name = to
		}
		if pool := p.pools[name]; pool != nil {
			expandTo = max(expandTo, pool.maxSize(), pool.fallback.maxSize())
		}
	}
	return expandTo
}

// shuffledCopy is a shuffled copy of the list. vanilla: Util.shuffledCopy
func shuffledCopy[T any](list []T, rng *levelgen.LegacyRandomSource) []T {
	out := append([]T(nil), list...)
	shuffle(out, rng)
	return out
}

// shuffledRotations lists the four rotations in a random order.
// vanilla: Rotation.getShuffled
func shuffledRotations(rng *levelgen.LegacyRandomSource) [4]Rotation {
	rots := [4]Rotation{RotateNone, RotateClockwise90, Rotate180, RotateCounterclockwise90}
	shuffle(rots[:], rng)
	return rots
}

func (p *jigsawPiece) bounds() jbox { return p.box }

// place writes the piece's blocks inside the chunk: the template's blocks in
// placement order, each through the jigsaw replacement and the element's
// processors, rotated with the piece; ref[1] is the bottom of the start's
// first piece.
// vanilla: SinglePoolElement.place, StructureTemplate.placeInWorld
func (p *jigsawPiece) place(r *decorRegion, chunk jbox, ref [3]int32) {
	p.placeElement(p.element, r, chunk, ref, "")
}

// placeElement places a single element's template, a list's elements in
// order, or a feature element's feature at the piece's position.
// vanilla: ListPoolElement.place, FeaturePoolElement.place
func (p *jigsawPiece) placeElement(el *jigsawElement, r *decorRegion, chunk jbox, ref [3]int32, path string) {
	for i, sub := range el.list {
		p.placeElement(sub, r, chunk, ref, path+"/"+strconv.Itoa(i))
	}
	if el.feature != "" {
		jigsawFeatures[el.feature](r, p.pos[0], p.pos[1], p.pos[2])
		return
	}
	if el.template == nil {
		return
	}
	wholePiece := false
	for _, proc := range el.processors {
		if _, ok := proc.(cappedProcessor); ok {
			wholePiece = true
		}
	}
	write := func(x, y, z int32, state uint16, tag nbt.Compound) {
		if !chunk.inside(x, y, z) {
			return
		}
		state = RotateState(state, p.rotation)
		// flags 18 carry UPDATE_KNOWN_SHAPE: no post-processing mark above soul sand or magma.
		// vanilla: SinglePoolElement.place, WorldGenRegion.setBlock
		r.setKnownShape(x, y, z, state)
		if tag != nil {
			r.placeBlockEntity(x, y, z, state, tag)
		}
	}
	placed := &PlacedTemplate{}
	// A terrain-matching element's blocks stand on the surface column by
	// column. vanilla: GravityProcessor (WORLD_SURFACE_WG, offset -1)
	for _, blk := range el.template.Palettes[0] {
		tx, ty, tz := TransformTemplatePos(blk.Pos[0], blk.Pos[1], blk.Pos[2], MirrorNone, p.rotation)
		x, y, z := tx+p.pos[0], ty+p.pos[1], tz+p.pos[2]
		if !wholePiece && !chunk.inside(x, y, z) {
			continue
		}
		state, tag, ok := blk.State, blk.NBT, true
		if !p.keepJigsaws || registry.BlockOf(blk.State) != registry.BlockJigsaw {
			state, tag, ok = processTemplateBlock(blk, x, y, z)
		}
		if !ok {
			continue
		}
		world := r.get(x, y, z)
		for _, proc := range el.processors {
			if state, tag, ok = applyProcessor(proc, state, world, x, y, z, ref[1], tag); !ok {
				break
			}
		}
		if !ok {
			continue
		}
		if el.legacy && registry.BlockOf(state).InTag(registry.TagIDAir) {
			continue
		}
		if el.matching {
			y = r.heightOf(hmWorldSurfaceWG, x, z) - 1 + blk.Pos[1]
		}
		if el.protected && registry.BlockOf(r.get(x, y, z)).InTag(registry.TagIDFeaturesCannotReplace) {
			continue
		}
		if !wholePiece {
			write(x, y, z, state, tag)
			continue
		}
		placed.Changes = append(placed.Changes, BlockChange{X: x, Y: y, Z: z, State: state})
		placed.Tags = append(placed.Tags, tag)
	}
	for _, proc := range el.processors {
		if cap, ok := proc.(cappedProcessor); ok {
			cap.finalize(placed, r.worldSeed, p.pos, ref[1], r.get)
		}
	}
	for i, b := range placed.Changes {
		write(b.X, b.Y, b.Z, b.State, placed.Tags[i])
	}

	// SinglePoolElement.getSettings uses knownShape=true, so jigsaw pieces
	// skip the template shape pass.
	// Nether templates are handed off by NetherGenerator.GeneratedEntities.
	if r.g == nil {
		p.placeEntities(el, r, chunk, path)
	}
}
