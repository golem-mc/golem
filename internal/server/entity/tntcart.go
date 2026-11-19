package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

const (
	// TNTCartFuse is the fuse a priming sets, in ticks.
	// vanilla: MinecartTNT.primeFuse
	TNTCartFuse = 80
	// TNTCartDefaultPower and TNTCartDefaultSpeedFactor are the explosion
	// settings a cart without saved ones has.
	// vanilla: MinecartTNT.DEFAULT_EXPLOSION_POWER_BASE
	TNTCartDefaultPower       = float32(4)
	TNTCartDefaultSpeedFactor = float32(1)
	// TNTCartMaxSetting caps both explosion settings on load.
	// vanilla: MinecartTNT.readAdditionalSaveData
	TNTCartMaxSetting = float32(128)
	// TNTCartImpactSpeedSqr is the squared horizontal speed a wall hit or a
	// breaking blow must reach to set the cart off.
	// vanilla: MinecartTNT.tick, MinecartTNT.destroy (0.01f)
	TNTCartImpactSpeedSqr = float64(float32(0.01))
	// TNTCartFallThreshold is the fall that sets the cart off.
	// vanilla: MinecartTNT.causeFallDamage
	TNTCartFallThreshold = 3.0
	// tntCartSpeedCap caps the speed the blast power scales with.
	tntCartSpeedCap = 5.0
)

// TNTCart is the state a TNT minecart adds to the cart: its fuse (-1 while
// unlit) and the explosion settings.
type TNTCart struct {
	Fuse        int32
	Power       float32
	SpeedFactor float32
	// OwnerID is the entity the blast is told by, 0 for none; the entity
	// that lit the fuse. Not saved.
	OwnerID int32
}

// NewTNTCart is an unlit cart with the default explosion.
func NewTNTCart() TNTCart {
	return TNTCart{Fuse: -1, Power: TNTCartDefaultPower, SpeedFactor: TNTCartDefaultSpeedFactor}
}

// Primed is whether the fuse is lit. vanilla: MinecartTNT.isPrimed
func (t *TNTCart) Primed() bool { return t.Fuse > -1 }

// Step spends one tick of the fuse and reports whether it has run out: a
// fuse of 80 explodes on the 81st tick. vanilla: MinecartTNT.tick
func (t *TNTCart) Step() (explode bool) {
	if t.Fuse > 0 {
		t.Fuse--
		return false
	}
	return t.Fuse == 0
}

// Radius is the blast power for a squared speed, roll being the cart's
// nextDouble. vanilla: MinecartTNT.explode
func (t *TNTCart) Radius(roll, speedSqr float64) float64 {
	speed := math.Min(math.Sqrt(speedSqr), tntCartSpeedCap)
	return float64(float32(float64(t.Power) + float64(t.SpeedFactor)*roll*1.5*speed))
}

// FallSpeedSqr is the squared speed a fall of the given height stands for.
// vanilla: MinecartTNT.causeFallDamage
func FallSpeedSqr(fall float64) float64 {
	power := fall / 10.0
	return power * power
}

// BreakFuse is the fuse a breaking blow leaves, two rolls of 0-19 summed.
// vanilla: MinecartTNT.destroy
func BreakFuse(rng *rand.Rand) int32 {
	return int32(rng.Intn(20) + rng.Intn(20))
}

// ClampTNTCartSetting bounds a saved explosion setting.
func ClampTNTCartSetting(v float32) float32 {
	return min(max(v, 0), TNTCartMaxSetting)
}

// TNTCartHit is what struck a cart, as far as a TNT cart tells.
type TNTCartHit struct {
	// DamageType is the source type for the invulnerability gate.
	DamageType registry.DamageType
	// OwnerID is the entity behind the blow, 0 for none.
	OwnerID int32
	// Ignites is a fire or explosion source, or a burning projectile.
	// vanilla: MinecartTNT.damageSourceIgnitesTnt
	Ignites bool
	// FireArrow is a burning arrow that hit, FireArrowSpeedSq its squared
	// speed. vanilla: MinecartTNT.hurtServer
	FireArrow        bool
	FireArrowSpeedSq float64
}

// TNTCartBlast is an explosion a cart decided on.
type TNTCartBlast struct {
	X, Y, Z float64
	Radius  float64
	OwnerID int32
	// Primed is whether the fuse was lit when the cart went off, which makes
	// the blast spare rails. vanilla: MinecartTNT.getBlockExplosionResistance
	Primed bool
}

// TNTCartEffects is what a cart's decisions owe the world, run by the
// server once the cart lock is released: lit fuses (Primes, only X, Y, Z)
// and blasts.
type TNTCartEffects struct {
	Primes []TNTCartBlast
	Blasts []TNTCartBlast
}

// TNTCartEnv is what a cart's decisions read from outside it: the dice and
// the tnt_explodes rule.
type TNTCartEnv struct {
	Rng      *rand.Rand
	Explodes bool
}

// Prime lights the fuse, unless tnt_explodes is off. vanilla: MinecartTNT.primeFuse
func (t *TNTCart) Prime(m *Minecart, env TNTCartEnv, ownerID int32, fx *TNTCartEffects) {
	if !env.Explodes {
		return
	}
	t.Fuse = TNTCartFuse
	if ownerID != 0 && t.OwnerID == 0 {
		t.OwnerID = ownerID
	}
	fx.Primes = append(fx.Primes, TNTCartBlast{X: m.X, Y: m.Y, Z: m.Z})
}

// Explode sets the cart off at a squared speed and ends it; with tnt_explodes
// off only a lit cart is discarded. vanilla: MinecartTNT.explode
func (t *TNTCart) Explode(m *Minecart, env TNTCartEnv, ownerID int32, speedSq float64, fx *TNTCartEffects) {
	if m.Dead {
		return
	}
	if env.Explodes {
		fx.Blasts = append(fx.Blasts, TNTCartBlast{
			X: m.X, Y: m.Y, Z: m.Z,
			Radius:  t.Radius(env.Rng.Float64(), speedSq),
			OwnerID: ownerID,
			Primed:  t.Primed(),
		})
	} else if !t.Primed() {
		return
	}
	m.Dead = true
	m.NoLoot = true
}

// Hurt is VehicleEntity.hurtServer for a TNT cart: rock and add damage, a
// burning arrow's blast, then the break: a fire or explosion source, or
// damage past 40, lights the fuse if the cart moves or the source burns, and
// otherwise breaks it to its item. It reports whether the blow landed.
// vanilla: MinecartTNT.hurtServer, MinecartTNT.destroy
func (t *TNTCart) Hurt(m *Minecart, env TNTCartEnv, damage float64, dropless bool, hit TNTCartHit, fx *TNTCartEffects) bool {
	if m.Dead {
		return false
	}
	if hit.FireArrow {
		t.Explode(m, env, hit.OwnerID, hit.FireArrowSpeedSq, fx)
		if m.Dead {
			return true
		}
	}
	landed := m.Hurt(damage, hit.DamageType, dropless)
	if !landed {
		return false
	}
	if !m.Dead {
		if hit.Ignites {
			t.destroy(m, env, hit, fx)
		}
		return landed
	}
	// The blow broke it: past forty (survival) or a creative discard.
	if hit.Ignites || !dropless {
		m.Dead, m.NoLoot = false, false
		t.destroy(m, env, hit, fx)
	}
	return landed
}

// destroy lights a fuse of two rolls where the source ignites or the cart
// moves, and breaks the cart otherwise. vanilla: MinecartTNT.destroy
func (t *TNTCart) destroy(m *Minecart, env TNTCartEnv, hit TNTCartHit, fx *TNTCartEffects) {
	speedSq := m.VX*m.VX + m.VZ*m.VZ
	if hit.Ignites || speedSq >= TNTCartImpactSpeedSqr {
		if t.Fuse < 0 {
			t.Prime(m, env, hit.OwnerID, fx)
			t.Fuse = BreakFuse(env.Rng)
		}
		return
	}
	m.Dead = true
}

// Tick is a TNT cart's part of a tick after the ride: lava and fire hurt it,
// a landing, the lit activator, the fuse and a wall hit set it off.
// vanilla: MinecartTNT.tick
func (t *TNTCart) Tick(m *Minecart, env TNTCartEnv, fx *TNTCartEffects) {
	if fall := m.LandedFall; fall > 0 {
		m.LandedFall = 0
		if fall >= TNTCartFallThreshold {
			t.Explode(m, env, t.OwnerID, FallSpeedSqr(fall), fx)
		}
	}
	if m.OnActivator && m.ActivatorPowered && t.Fuse < 0 && !m.Dead {
		t.Prime(m, env, 0, fx)
	}
	if m.Dead {
		return
	}
	speedSq := m.VX*m.VX + m.VZ*m.VZ
	if t.Step() {
		t.Explode(m, env, t.OwnerID, speedSq, fx)
	}
	if m.HorizontalCollision && speedSq >= TNTCartImpactSpeedSqr {
		t.Explode(m, env, t.OwnerID, speedSq, fx)
	}
}
