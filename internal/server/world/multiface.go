package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// multifaceSpreader is MultifaceSpreader for one block, reading the world
// through get so decoration and bone meal share it.
type multifaceSpreader struct {
	block registry.Block
	get   func(x, y, z int32) uint16
}

// stateFor is the block's state with the face toward d added to old, false
// when that face is taken or has nothing to attach to.
// vanilla: MultifaceBlock.getStateForPlacement
func (m multifaceSpreader) stateFor(old uint16, p [3]int32, d int) (uint16, bool) {
	same := registry.BlockOf(old) == m.block
	if same && registry.StateBool(old, facePropIDs[d]) {
		return 0, false
	}
	n := dirDelta[d]
	if !registry.FaceAttachable(m.get(p[0]+n[0], p[1]+n[1], p[2]+n[2]), blocklogic.Opposite(d)) {
		return 0, false
	}
	s := old
	if !same {
		s = m.block.DefaultState()
		if isWaterSource(old) {
			s, _ = registry.WithBool(s, registry.PropWaterlogged, true)
		}
	}
	s, _ = registry.WithBool(s, facePropIDs[d], true)
	return s, true
}

// canSpreadInto is the state at p taking a face toward d, spread from src.
// vanilla: MultifaceSpreader.DefaultSpreaderConfig.canSpreadInto, SculkVeinSpreaderConfig
func (m multifaceSpreader) canSpreadInto(src, p [3]int32, d int) (uint16, bool) {
	old := m.get(p[0], p[1], p[2])
	if m.block == registry.BlockSculkVein {
		if !veinCanSpreadInto(m.get, src[0], src[1], src[2], p[0], p[1], p[2], int32(d)) {
			return 0, false
		}
	} else if !IsAirState(old) && registry.BlockOf(old) != m.block && (!isWater(old) || !isWaterSource(old)) {
		return 0, false
	}
	return m.stateFor(old, p, d)
}

// target is the position and new state of the first of SAME_POSITION,
// SAME_PLANE and WRAP_AROUND that takes a face spread from s's face from
// toward dir; false when none does.
// vanilla: MultifaceSpreader.getSpreadFromFaceTowardDirection
func (m multifaceSpreader) target(s uint16, p [3]int32, from, dir int) ([3]int32, uint16, bool) {
	if from/2 == dir/2 || !registry.StateBool(s, facePropIDs[from]) || registry.StateBool(s, facePropIDs[dir]) {
		return p, 0, false
	}
	n, f := dirDelta[dir], dirDelta[from]
	targets := [3]struct {
		pos  [3]int32
		face int
	}{
		{p, dir},
		{[3]int32{p[0] + n[0], p[1] + n[1], p[2] + n[2]}, from},
		{[3]int32{p[0] + n[0] + f[0], p[1] + n[1] + f[1], p[2] + n[2] + f[2]}, blocklogic.Opposite(dir)},
	}
	for _, t := range targets {
		if ns, ok := m.canSpreadInto(p, t.pos, t.face); ok {
			return t.pos, ns, true
		}
	}
	return p, 0, false
}
