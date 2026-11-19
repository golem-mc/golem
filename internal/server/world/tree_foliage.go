package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// foliagePlacer is a FoliagePlacer.
type foliagePlacer interface {
	foliageHeight(rng *levelgen.WorldgenRandom, treeHeight int32) int32
	foliageRadius(rng *levelgen.WorldgenRandom, trunkHeight int32) int32
	createFoliage(t *treeBuild, treeHeight int32, a foliageAttach, foliageHeight, leafRadius int32)
}

// foliageKind is one foliage placer: the parts vanilla overrides per class.
type foliageKind struct {
	radius, offset intProvider
	// height is foliageHeight.
	height func(rng *levelgen.WorldgenRandom, treeHeight int32) int32
	// extraRadius is added to the sampled radius (pine).
	extraRadius func(rng *levelgen.WorldgenRandom, trunkHeight int32) int32
	// create is createFoliage with the sampled offset.
	create func(f *foliageKind, t *treeBuild, a foliageAttach, foliageHeight, leafRadius, offset int32)
	// skip is shouldSkipLocation.
	skip func(rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool
	// skipSigned, when set, replaces shouldSkipLocationSigned (dark oak).
	skipSigned func(f *foliageKind, rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool
	// hanging are the cherry placer's hanging-leaves chances.
	hanging, hangingExt float32
}

func (f *foliageKind) foliageHeight(rng *levelgen.WorldgenRandom, treeHeight int32) int32 {
	return f.height(rng, treeHeight)
}

func (f *foliageKind) foliageRadius(rng *levelgen.WorldgenRandom, trunkHeight int32) int32 {
	r := f.radius(rng)
	if f.extraRadius != nil {
		r += f.extraRadius(rng, trunkHeight)
	}
	return r
}

func (f *foliageKind) createFoliage(t *treeBuild, treeHeight int32, a foliageAttach, foliageHeight, leafRadius int32) {
	f.create(f, t, a, foliageHeight, leafRadius, f.offset(t.rng))
}

func (f *foliageKind) skipSignedAt(rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool {
	if f.skipSigned != nil {
		return f.skipSigned(f, rng, dx, y, dz, cr, double)
	}
	return f.baseSkipSigned(rng, dx, y, dz, cr, double)
}

func (f *foliageKind) baseSkipSigned(rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool {
	if double {
		dx, dz = min(abs32(dx), abs32(dx-1)), min(abs32(dz), abs32(dz-1))
	} else {
		dx, dz = abs32(dx), abs32(dz)
	}
	return f.skip(rng, dx, y, dz, cr, double)
}

// placeLeavesRow places a square of leaves around origin at height y.
// vanilla: FoliagePlacer.placeLeavesRow
func (f *foliageKind) placeLeavesRow(t *treeBuild, origin treePos, cr, y int32, double bool) {
	off := int32(0)
	if double {
		off = 1
	}
	for dx := -cr; dx <= cr+off; dx++ {
		for dz := -cr; dz <= cr+off; dz++ {
			if f.skipSignedAt(t.rng, dx, y, dz, cr, double) {
				continue
			}
			t.tryPlaceLeaf(at(origin, treePos{dx, y, dz}))
		}
	}
}

// placeLeavesRowWithHangingLeavesBelow is a row with leaves hung below its
// edge leaves.
// vanilla: FoliagePlacer.placeLeavesRowWithHangingLeavesBelow
func (f *foliageKind) placeLeavesRowWithHangingLeavesBelow(t *treeBuild, origin treePos, cr, y int32, double bool) {
	f.placeLeavesRow(t, origin, cr, y, double)
	off := int32(0)
	if double {
		off = 1
	}
	logPos := at(origin, treePos{0, -1, 0})
	for alongEdge := range 4 {
		toEdge := (alongEdge + 1) % 4 // getClockWise
		toEdgeOffset := cr
		if horizontalDirs[toEdge][0]+horizontalDirs[toEdge][1] > 0 {
			toEdgeOffset = cr + off
		}
		pos := horizStep(horizStep(at(origin, treePos{0, y - 1, 0}), toEdge, toEdgeOffset), alongEdge, -cr)
		for i := -cr; i < cr+off; i++ {
			leavesAbove := t.foliage.has(at(pos, treePos{0, 1, 0}))
			if leavesAbove && f.tryPlaceExtension(t, f.hanging, logPos, pos) {
				f.tryPlaceExtension(t, f.hangingExt, logPos, at(pos, treePos{0, -1, 0}))
			}
			pos = horizStep(pos, alongEdge, 1)
		}
	}
}

func (f *foliageKind) tryPlaceExtension(t *treeBuild, chance float32, logPos, pos treePos) bool {
	if abs32(pos[0]-logPos[0])+abs32(pos[1]-logPos[1])+abs32(pos[2]-logPos[2]) >= 7 {
		return false
	}
	if t.rng.NextFloat() > chance {
		return false
	}
	return t.tryPlaceLeaf(pos)
}

// tryPlaceLeaf places a foliage block where a tree may stand and no
// persistent leaves are.
// vanilla: FoliagePlacer.tryPlaceLeaf
func (t *treeBuild) tryPlaceLeaf(p treePos) bool {
	if registry.StateBool(t.state(p), registry.PropPersistent) || !t.validTreePos(p) {
		return false
	}
	st := t.cfg.foliageProvider(t.r, p)
	if _, has := registry.WithBool(st, registry.PropWaterlogged, false); has {
		fl := fluidHolding(t.state(p))
		st, _ = registry.WithBool(st, registry.PropWaterlogged, fl.kind == fluidWater && fl.isSource())
	}
	t.put(&t.foliage, p, st)
	return true
}

func blobSkip(rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, _ bool) bool {
	return dx == cr && dz == cr && (rng.NextIntN(2) == 0 || y == 0)
}

func cornerSkip(_ *levelgen.WorldgenRandom, dx, _, dz, cr int32, _ bool) bool {
	return dx == cr && dz == cr && cr > 0
}

func bigCircleSkip(_ *levelgen.WorldgenRandom, dx, _, dz, cr int32, _ bool) bool {
	return dx+dz >= 7 || dx*dx+dz*dz > cr*cr
}

func constHeight(h int32) func(*levelgen.WorldgenRandom, int32) int32 {
	return func(*levelgen.WorldgenRandom, int32) int32 { return h }
}

func parseFoliage(o jobj) foliagePlacer {
	f := &foliageKind{radius: compileInt(o["radius"]), offset: compileInt(o["offset"])}
	switch o.str("type") {
	case "minecraft:poplar_foliage_placer":
		return poplarFoliage{f.radius, f.offset, compileInt(o["height"]), float32(o.num("side_hole_chance"))}
	case "minecraft:blob_foliage_placer":
		f.height, f.skip = constHeight(o.integer("height")), blobSkip
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			for yo := off; yo >= off-fh; yo-- {
				f.placeLeavesRow(t, a.pos, max(lr+a.radiusOffset-1-yo/2, 0), yo, a.doubleTrunk)
			}
		}
	case "minecraft:bush_foliage_placer":
		f.height = constHeight(o.integer("height"))
		f.skip = func(rng *levelgen.WorldgenRandom, dx, _, dz, cr int32, _ bool) bool {
			return dx == cr && dz == cr && rng.NextIntN(2) == 0
		}
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			for yo := off; yo >= off-fh; yo-- {
				f.placeLeavesRow(t, a.pos, lr+a.radiusOffset-1-yo, yo, a.doubleTrunk)
			}
		}
	case "minecraft:fancy_foliage_placer":
		f.height = constHeight(o.integer("height"))
		f.skip = func(_ *levelgen.WorldgenRandom, dx, _, dz, cr int32, _ bool) bool {
			sq := func(v float32) float32 { return v * v }
			return sq(float32(dx)+0.5)+sq(float32(dz)+0.5) > float32(cr*cr)
		}
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			for yo := off; yo >= off-fh; yo-- {
				cr := lr
				if yo != off && yo != off-fh {
					cr++
				}
				f.placeLeavesRow(t, a.pos, cr, yo, a.doubleTrunk)
			}
		}
	case "minecraft:acacia_foliage_placer":
		f.height = constHeight(0)
		f.skip = func(_ *levelgen.WorldgenRandom, dx, y, dz, cr int32, _ bool) bool {
			if y == 0 {
				return (dx > 1 || dz > 1) && dx != 0 && dz != 0
			}
			return dx == cr && dz == cr && cr > 0
		}
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			p := at(a.pos, treePos{0, off, 0})
			f.placeLeavesRow(t, p, lr+a.radiusOffset, -1-fh, a.doubleTrunk)
			f.placeLeavesRow(t, p, lr-1, -fh, a.doubleTrunk)
			f.placeLeavesRow(t, p, lr+a.radiusOffset-1, 0, a.doubleTrunk)
		}
	case "minecraft:cherry_foliage_placer":
		height := compileInt(o["height"])
		f.height = func(rng *levelgen.WorldgenRandom, _ int32) int32 { return height(rng) }
		wide, corner := float32(o.num("wide_bottom_layer_hole_chance")), float32(o.num("corner_hole_chance"))
		f.hanging, f.hangingExt = float32(o.num("hanging_leaves_chance")), float32(o.num("hanging_leaves_extension_chance"))
		f.skip = func(rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, _ bool) bool {
			if y == -1 && (dx == cr || dz == cr) && rng.NextFloat() < wide {
				return true
			}
			isCorner := dx == cr && dz == cr
			if cr > 2 {
				return isCorner || dx+dz > cr*2-2 && rng.NextFloat() < corner
			}
			return isCorner && rng.NextFloat() < corner
		}
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			p := at(a.pos, treePos{0, off, 0})
			cr := lr + a.radiusOffset - 1
			f.placeLeavesRow(t, p, cr-2, fh-3, a.doubleTrunk)
			f.placeLeavesRow(t, p, cr-1, fh-4, a.doubleTrunk)
			for y := fh - 5; y >= 0; y-- {
				f.placeLeavesRow(t, p, cr, y, a.doubleTrunk)
			}
			f.placeLeavesRowWithHangingLeavesBelow(t, p, cr, -1, a.doubleTrunk)
			f.placeLeavesRowWithHangingLeavesBelow(t, p, cr-1, -2, a.doubleTrunk)
		}
	case "minecraft:dark_oak_foliage_placer":
		f.height = constHeight(4)
		f.skip = func(_ *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool {
			if y == -1 && !double {
				return dx == cr && dz == cr
			}
			if y == 1 {
				return dx+dz > cr*2-2
			}
			return false
		}
		f.skipSigned = func(f *foliageKind, rng *levelgen.WorldgenRandom, dx, y, dz, cr int32, double bool) bool {
			if y == 0 && double && (dx == -cr || dx >= cr) && (dz == -cr || dz >= cr) {
				return true
			}
			return f.baseSkipSigned(rng, dx, y, dz, cr, double)
		}
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			p := at(a.pos, treePos{0, off, 0})
			if a.doubleTrunk {
				f.placeLeavesRow(t, p, lr+2, -1, true)
				f.placeLeavesRow(t, p, lr+3, 0, true)
				f.placeLeavesRow(t, p, lr+2, 1, true)
				if t.rng.NextBoolean() {
					f.placeLeavesRow(t, p, lr, 2, true)
				}
				return
			}
			f.placeLeavesRow(t, p, lr+2, -1, false)
			f.placeLeavesRow(t, p, lr+1, 0, false)
		}
	case "minecraft:jungle_foliage_placer":
		f.height, f.skip = constHeight(o.integer("height")), bigCircleSkip
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			leafHeight := fh
			if !a.doubleTrunk {
				leafHeight = 1 + t.rng.NextIntN(2)
			}
			for yo := off; yo >= off-leafHeight; yo-- {
				f.placeLeavesRow(t, a.pos, lr+a.radiusOffset+1-yo, yo, a.doubleTrunk)
			}
		}
	case "minecraft:mega_pine_foliage_placer":
		crown := compileInt(o["crown_height"])
		f.height = func(rng *levelgen.WorldgenRandom, _ int32) int32 { return crown(rng) }
		f.skip = bigCircleSkip
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			prev := int32(0)
			for yy := a.pos[1] - fh + off; yy <= a.pos[1]+off; yy++ {
				yo := a.pos[1] - yy
				smooth := lr + a.radiusOffset + int32(math.Floor(float64(float32(yo)/float32(fh)*3.5)))
				jagged := smooth
				if yo > 0 && smooth == prev && yy&1 == 0 {
					jagged = smooth + 1
				}
				f.placeLeavesRow(t, treePos{a.pos[0], yy, a.pos[2]}, jagged, 0, a.doubleTrunk)
				prev = smooth
			}
		}
	case "minecraft:pine_foliage_placer":
		height := compileInt(o["height"])
		f.height = func(rng *levelgen.WorldgenRandom, _ int32) int32 { return height(rng) }
		f.extraRadius = func(rng *levelgen.WorldgenRandom, trunkHeight int32) int32 {
			return rng.NextIntN(max(trunkHeight+1, 1))
		}
		f.skip = cornerSkip
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			cr := int32(0)
			for yo := off; yo >= off-fh; yo-- {
				f.placeLeavesRow(t, a.pos, cr, yo, a.doubleTrunk)
				if cr >= 1 && yo == off-fh+1 {
					cr--
				} else if cr < lr+a.radiusOffset {
					cr++
				}
			}
		}
	case "minecraft:spruce_foliage_placer":
		trunkH := compileInt(o["trunk_height"])
		f.height = func(rng *levelgen.WorldgenRandom, treeHeight int32) int32 { return max(4, treeHeight-trunkH(rng)) }
		f.skip = cornerSkip
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			cr := t.rng.NextIntN(2)
			maxR, minR := int32(1), int32(0)
			for yo := off; yo >= -fh; yo-- {
				f.placeLeavesRow(t, a.pos, cr, yo, a.doubleTrunk)
				if cr >= maxR {
					cr = minR
					minR = 1
					maxR = min(maxR+1, lr+a.radiusOffset)
				} else {
					cr++
				}
			}
		}
	case "minecraft:random_spread_foliage_placer":
		height := compileInt(o["foliage_height"])
		f.height = func(rng *levelgen.WorldgenRandom, _ int32) int32 { return height(rng) }
		attempts := o.integer("leaf_placement_attempts")
		f.skip = func(*levelgen.WorldgenRandom, int32, int32, int32, int32, bool) bool { return false }
		f.create = func(f *foliageKind, t *treeBuild, a foliageAttach, fh, lr, off int32) {
			rng := t.rng
			// vanilla: RandomSpreadFoliagePlacer.createFoliage; the draws are
			// ordered, and the height bound is the fh argument.
			for range attempts {
				x1, x2 := rng.NextIntN(lr), rng.NextIntN(lr)
				y1, y2 := rng.NextIntN(fh), rng.NextIntN(fh)
				z1, z2 := rng.NextIntN(lr), rng.NextIntN(lr)
				dx, dy, dz := x1-x2, y1-y2, z1-z2
				t.tryPlaceLeaf(at(a.pos, treePos{dx, dy, dz}))
			}
		}
	default:
		panic("world: tree data: foliage placer " + o.str("type"))
	}
	return f
}
