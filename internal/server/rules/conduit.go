package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// ConduitRefreshTicks is the game-time stride a conduit rescans its frame
// and rehands its power on.
// vanilla: ConduitBlockEntity.serverTick, the gameTime % 40 gate
const ConduitRefreshTicks int64 = 40

// ConduitMinFrame is the frame size that has a conduit active.
// vanilla: ConduitBlockEntity.MIN_ACTIVE_SIZE
const ConduitMinFrame = 16

// ConduitHuntFrame is the frame size that has a conduit hunting, the
// whole 42-cell cage.
// vanilla: ConduitBlockEntity.MIN_KILL_SIZE
const ConduitHuntFrame = 42

// ConduitEffectDuration is the length of the power a conduit hands out,
// thirteen seconds - longer than the forty ticks between its refreshes.
// vanilla: ConduitBlockEntity.EFFECT_DURATION, 13 seconds
const ConduitEffectDuration = 260

// ConduitHuntRange is how far from the conduit its beam's target may
// stand. vanilla: ConduitBlockEntity.KILL_RANGE
const ConduitHuntRange = 8

// ConduitAttackDamage is the hurt the beam of a hunting conduit deals.
// vanilla: ConduitBlockEntity.updateAndAttackTarget
const ConduitAttackDamage = 4.0

// IsConduitFrameBlock reports whether a block state is one the cage's
// frame is built of, the four of the prismarine family with the sea
// lantern. vanilla: ConduitBlockEntity.VALID_BLOCKS
func IsConduitFrameBlock(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockPrismarine, registry.BlockPrismarineBricks, registry.BlockDarkPrismarine, registry.BlockSeaLantern:
		return true
	}
	return false
}

// ConduitFrame counts the cage's frame blocks around the conduit, 0 unless
// the 3x3x3 around it is all water. Frame cells are the 5x5x5 shell's arms
// around each axis.
// vanilla: ConduitBlockEntity.updateShape
func ConduitFrame(w BlockAt, x, y, z int32) int {
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if !IsWater(w.BlockAt(x+dx, y+dy, z+dz)) {
					return 0
				}
			}
		}
	}
	frame := 0
	for ox := int32(-2); ox <= 2; ox++ {
		for oy := int32(-2); oy <= 2; oy++ {
			for oz := int32(-2); oz <= 2; oz++ {
				ax, ay, az := absCell(ox), absCell(oy), absCell(oz)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				open := (ox != 0 || (ay != 2 && az != 2)) &&
					(oy != 0 || (ax != 2 && az != 2)) &&
					(oz != 0 || (ax != 2 && ay != 2))
				if !open && IsConduitFrameBlock(w.BlockAt(x+ox, y+oy, z+oz)) {
					frame++
				}
			}
		}
	}
	return frame
}

// ConduitHunts reports whether a box meets the conduit block inflated by 8,
// where a new target is picked; a kept target is measured by ConduitReaches.
// vanilla: ConduitBlockEntity.selectNewTarget
func ConduitHunts(x, y, z int32, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return maxX > float64(x)-ConduitHuntRange && minX < float64(x)+1+ConduitHuntRange &&
		maxY > float64(y)-ConduitHuntRange && minY < float64(y)+1+ConduitHuntRange &&
		maxZ > float64(z)-ConduitHuntRange && minZ < float64(z)+1+ConduitHuntRange
}

// ConduitEffectRange is how far an active frame reaches, a sixteen-block
// step for every seven frame blocks.
// vanilla: ConduitBlockEntity.applyEffects, activeSize / 7 * 16
func ConduitEffectRange(frame int) int {
	return frame / 7 * 16
}

// ConduitPower is the instance an active conduit keeps on the swimmers it
// reaches.
// vanilla: ConduitBlockEntity.applyEffects
func ConduitPower() effect.Instance {
	return effect.New(effectConduitPower, ConduitEffectDuration, 0, true, true, true)
}

// ConduitReaches reports whether the conduit block's corner is strictly
// within reach of the corner of the cell at the body's feet.
// vanilla: Vec3i.closerThan, as applyEffects and the hunt read it
func ConduitReaches(x, y, z int32, px, py, pz float64, reach int) bool {
	dx := float64(x) - math.Floor(px)
	dy := float64(y) - math.Floor(py)
	dz := float64(z) - math.Floor(pz)
	return dx*dx+dy*dy+dz*dz < float64(reach)*float64(reach)
}

// absCell is a scan offset's size, in blocks from the conduit.
func absCell(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ConduitToggleSound is the sound a conduit plays as its frame turns it on
// or off.
// vanilla: ConduitBlockEntity.serverTick
func ConduitToggleSound(active bool) registry.Sound {
	if active {
		return registry.SoundBlockConduitActivate
	}
	return registry.SoundBlockConduitDeactivate
}

// ConduitAmbientPulse reports whether an active conduit plays its ambient
// pulse at age, on the eighty-tick line.
// vanilla: ConduitBlockEntity.serverTick
func ConduitAmbientPulse(age int64) bool {
	return age%80 == 0
}

// ConduitShortAmbient reports whether an active conduit whose next short
// ambient is due after next plays it at age, and the next one it rolls,
// sixty to ninety-nine ticks on. rng is drawn only when it plays.
// vanilla: ConduitBlockEntity.serverTick
func ConduitShortAmbient(age, next int64, rng *rand.Rand) (int64, bool) {
	if age <= next {
		return next, false
	}
	return age + 60 + int64(rng.Intn(40)), true
}
