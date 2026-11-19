package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The lightning strike's numbers.
const (
	// LightningChunkOdds is the denominator of a ticking chunk's strike roll
	// in a thunderstorm.
	// vanilla: ServerLevel.tickThunder
	LightningChunkOdds = 100000

	// LightningDamage is what a bolt deals each body in its box, every tick
	// it is live.
	// vanilla: Entity.thunderHit
	LightningDamage = 5.0

	// LightningIgniteTicks is the burn a body left at -1 fire ticks takes,
	// eight seconds.
	// vanilla: Entity.thunderHit
	LightningIgniteTicks = 160

	// DamageTypeLightningBolt is the damage type of a bolt's hurt.
	DamageTypeLightningBolt = registry.DamageTypeLightningBolt
)

// LightningStrikes rolls one chunk's thunderstorm die at 1 in odds, with 0
// standing for LightningChunkOdds.
// vanilla: ServerLevel.tickThunder
func LightningStrikes(rng *rand.Rand, odds int) bool {
	if odds <= 0 {
		odds = LightningChunkOdds
	}
	return rng.Intn(odds) == 0
}

// LightningBox is an axis-aligned box a strike searches or hurts in.
type LightningBox struct {
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64
}

// Hits reports whether the box overlaps the other, edges excluded.
func (b LightningBox) Hits(o LightningBox) bool {
	return b.MinX < o.MaxX && b.MaxX > o.MinX &&
		b.MinY < o.MaxY && b.MaxY > o.MinY &&
		b.MinZ < o.MaxZ && b.MaxZ > o.MinZ
}

// LightningSearchBox is the box a strike over the column (x, z) with its
// top at y looks for a living body in: three blocks around, from three below
// the top up with no ceiling.
// vanilla: ServerLevel.findLightningTargetAround
func LightningSearchBox(x, y, z int32) LightningBox {
	return LightningBox{
		MinX: float64(x - 3), MinY: float64(y - 3), MinZ: float64(z - 3),
		MaxX: float64(x + 4), MaxY: math.MaxFloat64, MaxZ: float64(z + 4),
	}
}

// LightningHurtBox is what a live bolt at (x, y, z) hurts: three blocks
// around and nine up.
// vanilla: LightningBolt.tick
func LightningHurtBox(x, y, z float64) LightningBox {
	return LightningBox{
		MinX: x - 3, MinY: y - 3, MinZ: z - 3,
		MaxX: x + 3, MaxY: y + 6 + 3, MaxZ: z + 3,
	}
}

// LightningPickTarget picks the strike cell among the cells of the bodies the
// search found, and reports false when there are none.
// vanilla: ServerLevel.findLightningTargetAround
func LightningPickTarget(rng *rand.Rand, cells [][3]int32) (x, y, z int32, ok bool) {
	if len(cells) == 0 {
		return 0, 0, 0, false
	}
	c := cells[rng.Intn(len(cells))]
	return c[0], c[1], c[2], true
}

// LightningThunderHitFire advances a body's fire counter by the bolt's tick
// and reports whether the counter reached nought, which sets the body alight
// for LightningIgniteTicks.
// vanilla: Entity.thunderHit
func LightningThunderHitFire(fireTicks int) (next int, ignite bool) {
	fireTicks++
	return fireTicks, fireTicks == 0
}

// LightningLightsFire reports whether a bolt's fire may be set in the cell:
// it holds one of the three air blocks and a fire survives there.
// vanilla: LightningBolt.spawnFire
func LightningLightsFire(w FireWorld, x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	return world.IsKnownAirState(state) && FireSurvivesAt(w, x, y, z)
}

// LightningFireOffset is one extra fire source's offset from the strike
// cell, each axis in -1..1.
// vanilla: LightningBolt.spawnFire
func LightningFireOffset(rng *rand.Rand) (dx, dy, dz int32) {
	return int32(rng.Intn(3) - 1), int32(rng.Intn(3) - 1), int32(rng.Intn(3) - 1)
}

// LightningFiresAtStrike reports whether the strike tick also lights extra
// fires: the normal and hard difficulties.
// vanilla: LightningBolt.tick
func LightningFiresAtStrike(difficulty uint8) bool {
	return difficulty == 2 || difficulty == 3
}
