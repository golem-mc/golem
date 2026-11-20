package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/rules"
)

// LightningTrackingRange is the bolt's clientTrackingRange, in chunks.
// vanilla: EntityTypes.LIGHTNING_BOLT
const LightningTrackingRange = 16

// boltStartLife is the bolt's first life count; the strike lands on the tick
// that finds it still there.
// vanilla: LightningBolt.START_LIFE
const boltStartLife = 2

// Bolt is a lightning bolt. The client plays its flash and thunder itself;
// the body only owns the lifetime and when the strike's effects are due.
type Bolt struct {
	ID      int32
	X, Y, Z float64
	// VisualOnly is a bolt that hurts and ignites nothing, the one a
	// skeleton trap comes with.
	VisualOnly bool

	life    int
	flashes int
	rng     *rand.Rand
}

// NewBolt builds a bolt with the flash count the constructor rolls.
// vanilla: LightningBolt.<init>
func NewBolt(id int32, x, y, z float64, visualOnly bool, rng *rand.Rand) *Bolt {
	return &Bolt{ID: id, X: x, Y: y, Z: z, VisualOnly: visualOnly,
		life: boltStartLife, flashes: rng.Intn(3) + 1, rng: rng}
}

// BoltStep is what one tick of a bolt asks of the world, in the order the
// fields are listed.
type BoltStep struct {
	// Strike is the first tick, when the fire around the strike is lit.
	Strike bool
	// Flash is a later flash, which lights the strike cell's fire.
	Flash bool
	// Harm says the bolt's box hurts what stands in it this tick.
	Harm bool
	// Done says the bolt is spent and leaves the world.
	Done bool
}

// Step runs one tick.
// vanilla: LightningBolt.tick
func (b *Bolt) Step() BoltStep {
	var out BoltStep
	if b.life == boltStartLife {
		out.Strike = true
	}
	b.life--
	if b.life < 0 {
		if b.flashes == 0 {
			out.Done = true
			return out
		}
		if b.life < -b.rng.Intn(10) {
			b.flashes--
			b.life = 1
			out.Flash = true
		}
	}
	out.Harm = b.life >= 0 && !b.VisualOnly
	return out
}

// ThunderHit is Entity.thunderHit for an item: the bolt's fire tick, then
// lightning damage, which a stack resisting it refuses. It reports whether
// the item was destroyed.
// vanilla: Entity.thunderHit, ItemEntity.hurtServer
func (it *Item) ThunderHit() bool {
	var ignite bool
	it.fireTicks, ignite = rules.LightningThunderHitFire(it.fireTicks)
	if ignite {
		it.fireTicks = max(it.fireTicks, rules.LightningIgniteTicks)
	}
	var events []ItemEvent
	it.hurt(rules.LightningDamage, rules.DamageTypeLightningBolt, &events)
	return it.Dead
}

// ThunderHit is a bolt's effect on a mob in its box: a tick on the fire
// counter, eight seconds of burn when it reaches zero, and the bolt's hurt;
// a creeper is charged unless the caller asks to charge it itself (a
// listener may refuse). It reports how the hurt went.
// vanilla: Entity.thunderHit, Creeper.thunderHit
func (m *Mob) ThunderHit(holdCharge bool, bolt ...any) (landed, softened bool) {
	if m.Type == TypeTurtle {
		// vanilla: Turtle.thunderHit, a blow that nothing survives
		return m.HurtByWorld(math.MaxFloat32, rules.DamageTypeLightningBolt)
	}
	fire, ignite := rules.LightningThunderHitFire(m.FireTicks)
	m.SetMetadataFireTicks(fire)
	if ignite {
		var src CombustSource
		if len(bolt) > 0 {
			src.By = bolt[0]
		}
		m.IgniteForTicksBy(rules.LightningIgniteTicks, src)
	}
	landed, softened = m.HurtByWorld(rules.LightningDamage, rules.DamageTypeLightningBolt)
	if Family(m.Type) == TypeCreeper && !holdCharge {
		m.SetMetadataPowered(true)
	}
	return landed, softened
}
