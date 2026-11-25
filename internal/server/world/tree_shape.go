package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

var treeVineFaces = [4]struct {
	dir  int
	prop registry.Prop
}{{dirNorth, registry.PropNorth}, {dirSouth, registry.PropSouth}, {dirWest, registry.PropWest}, {dirEast, registry.PropEast}}

// treeShape holds the filled cells of a tree's inclusive bounding box.
// vanilla: BitSetDiscreteVoxelShape
type treeShape struct {
	lo, size treePos
	bits     []uint64
}

func newTreeShape(lo, hi treePos) treeShape {
	size := treePos{hi[0] - lo[0] + 1, hi[1] - lo[1] + 1, hi[2] - lo[2] + 1}
	return treeShape{lo: lo, size: size, bits: make([]uint64, (size[0]*size[1]*size[2]+63)/64)}
}

// index requires p to be inside the box.
func (s treeShape) index(p treePos) int32 {
	return ((p[0]-s.lo[0])*s.size[1]+p[1]-s.lo[1])*s.size[2] + p[2] - s.lo[2]
}

func (s treeShape) fill(p treePos) {
	i := s.index(p)
	s.bits[i>>6] |= uint64(1) << (i & 63)
}

func (s treeShape) has(p treePos) bool {
	i := s.index(p)
	return s.bits[i>>6]&(uint64(1)<<(i&63)) != 0
}

// updateShapeAtEdge runs the shape update vanilla gives every block on the
// outside faces of the tree's shape and its neighbours, in the order of
// DiscreteVoxelShape.forAllFaces (lines along Z, then Y, then X). Only the
// updates that can change a block are modelled: soil-supported plants, mushrooms, vines,
// hanging propagules and pale moss carpets lose or gain support, two-tall plants
// lose orphaned halves, fences and walls adjust their arms, and leaves and water
// only schedule ticks.
// vanilla: StructureTemplate.updateShapeAtEdge, DiscreteVoxelShape.forAllFaces
func (t *treeBuild) updateShapeAtEdge(shape treeShape) {
	lo, size := shape.lo, shape.size
	for _, ax := range [3][3]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
		a, b, c := ax[0], ax[1], ax[2]
		negative, positive := dirOfAxis(c, -1), dirOfAxis(c, 1)
		for ai := range size[a] {
			for bi := range size[b] {
				last := false
				for ci := int32(0); ci <= size[c]; ci++ {
					var p treePos
					p[a], p[b], p[c] = lo[a]+ai, lo[b]+bi, lo[c]+ci
					full := ci != size[c] && shape.has(p)
					if !last && full {
						t.faceUpdate(p, negative)
					}
					if last && !full {
						p[c]--
						t.faceUpdate(p, positive)
					}
					last = full
				}
			}
		}
	}
}

func dirOfAxis(axis, sign int) int {
	return [3][2]int{{dirWest, dirEast}, {dirDown, dirUp}, {dirNorth, dirSouth}}[axis][(sign+1)/2]
}

// faceUpdate updates the shape of the block at p from direction d and of the
// block beyond it from the opposite side.
func (t *treeBuild) faceUpdate(p treePos, d int) {
	n := at(p, dirDelta[d])
	st, ns := t.state(p), t.state(n)
	if next := t.shapeUpdate(st, p, d); next != st {
		t.r.set(p[0], p[1], p[2], next)
	}
	if next := t.shapeUpdate(ns, n, blocklogic.Opposite(d)); next != ns {
		t.r.set(n[0], n[1], n[2], next)
	}
}

// shapeUpdate is BlockState.updateShape for the blocks a tree can leave
// unsupported, and walls beside it.
// vanilla: VineBlock.updateShape, MangrovePropaguleBlock.updateShape, MossyCarpetBlock.updateShape, WallBlock.updateShape
func (t *treeBuild) shapeUpdate(state uint16, p treePos, d int) uint16 {
	// A disk can replace soil, and crops and mushrooms can lack suitable light;
	// removing plants here changes what later patches draw from their RNG.
	// vanilla: VegetationBlock.updateShape, SeagrassBlock.updateShape
	if s := survivalOf[state]; (s.kind == surviveSoil || s.kind == surviveCrop || s.kind == surviveMushroom || s.kind == surviveSturdyUnless) && s.tick == 0 && s.watch&(1<<d) != 0 &&
		!canSurvive(t.r, p[0], p[1], p[2], state) {
		return airState
	}
	// A canopy's bottom face reaches the litter below it, which a path now
	// under it no longer supports.
	// vanilla: VegetationBlock.updateShape, LeafLitterBlock.canSurvive
	if registry.BlockOf(state) == registry.BlockLeafLitter && !canSurvive(t.r, p[0], p[1], p[2], state) {
		return airState
	}
	// Decorators can replace soil beneath existing snow; the tree's edge pass
	// settles its snowy property after all decorators have placed their blocks.
	// vanilla: SnowyBlock.updateShape, SnowyBlock.isSnowySetting
	if d == dirUp && registry.StateHas(state, registry.PropSnowy) {
		next, _ := registry.WithBool(state, registry.PropSnowy,
			registry.BlockOf(t.state(at(p, dirDelta[dirUp]))).InTag(registry.TagIDSnow))
		return next
	}
	// vanilla: DoublePlantBlock.updateShape
	if b := registry.BlockOf(state); b.IsDoublePlant() && isDoublePlantState(b, stateIntOr(state, registry.PropAge, 0)) {
		half := registry.StateValue(state, registry.PropHalf)
		if half == "lower" && d == dirUp || half == "upper" && d == dirDown {
			partner := t.state(at(p, dirDelta[d]))
			if registry.BlockOf(partner) != b || registry.StateValue(partner, registry.PropHalf) == half {
				return airState
			}
		} else if half == "lower" && d == dirDown && !doublePlantSurvives(t.r, state, p[0], p[1], p[2]) {
			return airState
		}
	}

	// vanilla: FenceBlock.updateShape
	if b := registry.BlockOf(state); b.Type() == registry.BlockTypeFence && d >= dirNorth {
		neighbour := t.state(at(p, dirDelta[d]))
		next, _ := registry.WithBool(state, facePropIDs[d], registry.ConnectsToFence(b, neighbour, faceProps[d]))
		return next
	}
	if b := registry.BlockOf(state); b.IsWall() && d != dirDown {
		return registry.WallStateForNeighbor(t.r, b, state, p[0], p[1], p[2], [6]string{"", "", "north", "south", "west", "east"}[d])
	}
	switch registry.BlockOf(state) {
	case registry.BlockVine:
		if d == dirDown {
			return state
		}
		return t.vineUpdated(state, p)
	case registry.BlockPaleMossCarpet:
		if !canSurvive(t.r, p[0], p[1], p[2], state) {
			return airState
		}
		next := MossyCarpetState(t.r, state, p[0], p[1], p[2], false)
		if !mossCarpetHasFaces(next) {
			return airState
		}
		return next
	case registry.BlockMangrovePropagule:
		above := t.state(at(p, dirDelta[dirUp]))
		if d == dirUp && registry.StateBool(state, registry.PropHanging) && !registry.BlockOf(above).InTag(registry.TagIDSupportsHangingMangrovePropagule) {
			return airState
		}
	}
	return state
}

// vineUpdated drops the faces of a vine that nothing supports, and the vine
// when none is left.
// The up face tests the top face of the block above.
// vanilla: VineBlock.getUpdatedState
func (t *treeBuild) vineUpdated(state uint16, p treePos) uint16 {
	above := t.state(at(p, dirDelta[dirUp]))
	if registry.StateBool(state, registry.PropUp) {
		state, _ = registry.WithBool(state, registry.PropUp, registry.FaceAttachable(above, dirUp))
	}
	kept := registry.StateBool(state, registry.PropUp)
	for _, f := range treeVineFaces {
		if !registry.StateBool(state, f.prop) {
			continue
		}
		ok := registry.FaceAttachable(t.state(at(p, dirDelta[f.dir])), blocklogic.Opposite(f.dir)) ||
			registry.BlockOf(above) == registry.BlockVine && registry.StateBool(above, f.prop)
		state, _ = registry.WithBool(state, f.prop, ok)
		kept = kept || ok
	}
	if !kept {
		return airState
	}
	return state
}
