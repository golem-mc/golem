package rules

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vineFaces counts the faces a vine state hangs on, VineBlock.countFaces.
func vineFaces(state uint16) int {
	n := 0
	for _, p := range [...]registry.Prop{registry.PropUp, registry.PropNorth, registry.PropSouth, registry.PropWest, registry.PropEast} {
		if registry.StateBool(state, p) {
			n++
		}
	}
	return n
}

// VinePlacementState adds the first nearest-looking direction (down skipped)
// whose face is free and can hold a vine, to the cell's vine or a fresh one.
// A fresh vine with none is refused; a clicked vine with none comes back
// unchanged, placing nothing.
// vanilla: VineBlock.getStateForPlacement
func VinePlacementState(w BlockReader, ctx PlacementContext, x, y, z int32) (uint16, bool) {
	cur := w.BlockAt(x, y, z)
	clickedVine := registry.BlockOf(cur) == registry.BlockVine
	base := cur
	if !clickedVine {
		base = registry.BlockVine.DefaultState()
	}
	for _, d := range nearestLookingDirections(ctx) {
		if d == dirDown || (clickedVine && registry.StateBool(cur, blocklogic.FaceProp(d))) || !vineCanSupportAtFace(w, x, y, z, d) {
			continue
		}
		s, _ := registry.WithBool(base, blocklogic.FaceProp(d), true)
		return s, true
	}
	if clickedVine {
		return cur, true
	}
	return 0, false
}

// vineCanSupportAtFace is VineBlock.canSupportAtFace: the neighbour's face
// toward the vine is full (registry.FaceAttachable), or for a side, the
// vine above hangs on the same side.
func vineCanSupportAtFace(w BlockReader, x, y, z int32, d int) bool {
	if d == dirDown {
		return false
	}
	if registry.FaceAttachable(w.BlockAt(x+dirDeltas[d][0], y+dirDeltas[d][1], z+dirDeltas[d][2]), blocklogic.Opposite(d)) {
		return true
	}
	if d == dirUp {
		return false
	}
	above := w.BlockAt(x, y+1, z)
	return registry.BlockOf(above) == registry.BlockVine && registry.StateBool(above, blocklogic.FaceProp(d))
}

// nearestLookingDirections is BlockPlaceContext.getNearestLookingDirections:
// Direction.orderedByNearest, with the clicked face's opposite moved to the
// front when the placement went to the face's neighbour.
func nearestLookingDirections(ctx PlacementContext) [6]int {
	dirs := orderedByNearest(ctx.Yaw, ctx.Pitch)
	if ctx.ReplacingClickedOn {
		return dirs
	}
	want := blocklogic.Opposite(int(ctx.Face))
	i := 0
	for i < len(dirs) && dirs[i] != want {
		i++
	}
	if i > 0 && i < len(dirs) {
		copy(dirs[1:i+1], dirs[:i])
		dirs[0] = want
	}
	return dirs
}

// orderedByNearest orders the six directions by angle to the look, read
// through Mth's sine table so ties at the exact quadrants match.
// vanilla: Direction.orderedByNearest
func orderedByNearest(yaw, pitch float32) [6]int {
	var pi float32 = math.Pi
	rad := pi / 180
	p := pitch * rad
	yw := -yaw * rad
	pitchSin, pitchCos := world.MthSin(float64(p)), world.MthCos(float64(p))
	yawSin, yawCos := world.MthSin(float64(yw)), world.MthCos(float64(yw))
	xPos, yPos, zPos := yawSin > 0, pitchSin < 0, yawCos > 0
	xYaw, yMag, zYaw := yawSin, pitchSin, yawCos
	if !xPos {
		xYaw = -yawSin
	}
	if yPos {
		yMag = -pitchSin
	}
	if !zPos {
		zYaw = -yawCos
	}
	xMag, zMag := xYaw*pitchCos, zYaw*pitchCos
	axisX, axisY, axisZ := dirWest, dirDown, dirNorth
	if xPos {
		axisX = dirEast
	}
	if yPos {
		axisY = dirUp
	}
	if zPos {
		axisZ = dirSouth
	}
	order := func(a, b, c int) [6]int {
		return [6]int{a, b, c, blocklogic.Opposite(c), blocklogic.Opposite(b), blocklogic.Opposite(a)}
	}
	if xYaw > zYaw {
		switch {
		case yMag > xMag:
			return order(axisY, axisX, axisZ)
		case zMag > yMag:
			return order(axisX, axisZ, axisY)
		}
		return order(axisX, axisY, axisZ)
	}
	switch {
	case yMag > zMag:
		return order(axisY, axisZ, axisX)
	case xMag > yMag:
		return order(axisZ, axisX, axisY)
	}
	return order(axisZ, axisY, axisX)
}
