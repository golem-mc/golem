package entity

import "github.com/golem-mc/golem/internal/server/rules"

// honeySlideCell is one cell's share of a non-item body's block effects when
// the cell is honey: a body pressed against its side has its fall throttled
// and its sideways motion cut, and its fall distance cleared. It reports
// whether the body slid, for the slide's sound and particles.
// vanilla: HoneyBlock.entityInside, doSlideMovement
func honeySlideCell(state uint16, x, y, z int32, px, py, pz, width float64, onGround bool, vx, vy, vz, fall *float64) bool {
	if !rules.IsHoneyBlock(state) || !rules.HoneySliding(x, y, z, px, py, pz, *vy, width, onGround) {
		return false
	}
	*vx, *vy, *vz = rules.HoneySlide(*vx, *vy, *vz)
	*fall = 0
	return true
}
