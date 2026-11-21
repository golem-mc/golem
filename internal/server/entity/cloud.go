package entity

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
)

// The area effect cloud a lingering potion leaves, a plain value like the
// thrown projectile: bodies are passed in, and the ones its pass reached
// come back for the server to apply effects and send packets.

// Lingering cloud numbers (ThrownLingeringPotion and AreaEffectCloud):
// radius 3, a 600-tick life, a 10-tick wait before the first pass, a
// twentieth of the radius lost each tick after the wait, 0.5 lost per
// application, a 20-tick reapplication cooldown per victim, and a pass every
// 5 ticks. 0.5 is also the smallest radius a live cloud keeps.
// vanilla: ThrownLingeringPotion.onHitAsPotion, AreaEffectCloud
const (
	CloudStartRadius     = float32(3.0)
	CloudDuration        = 600
	CloudWaitTime        = 10
	CloudReapplication   = 20
	CloudRadiusOnUse     = float32(-0.5)
	CloudApplicationFlip = 5
	cloudMinRadius       = float32(0.5)
)

// CloudTracking is AREA_EFFECT_CLOUD's 10-chunk tracking range. Its update
// interval is unbounded: a tracked cloud sends only metadata changes.
// vanilla: EntityTypes.AREA_EFFECT_CLOUD's builder
const CloudTracking = 10

// Cloud is one area effect cloud of a world.
type Cloud struct {
	ID int32
	Place

	// TickCount is the age of the cloud in ticks, one counted before
	// each step's own body runs.
	TickCount int

	// Dead marks one discarded: shrunk to the floor, or lasted out.
	Dead bool

	// Duration is the lasting time in ticks past the wait; -1 never lasts out.
	// DurationOnUse is added to it per body reached, RadiusOnUse to the radius.
	Duration      int
	WaitTime      int
	Reapplication int
	DurationOnUse int
	RadiusOnUse   float32

	radius        float32
	radiusPerTick float32

	// Effects are the potion's effect instances the cloud applies; instant ones
	// go through the per-tick countdown, as when drunk.
	Effects []effect.Instance

	// victims maps a body to the tick its last touch holds it off until,
	// by entity ID.
	victims map[int32]int

	waiting bool
}

// NewCloud places a cloud with the shatter's effects: radius 3, shrinking
// over its 600 ticks.
func NewCloud(id int32, x, y, z float64, effects []effect.Instance) *Cloud {
	c := &Cloud{
		ID: id, X: x, Y: y, Z: z,
		radius:   CloudStartRadius,
		Effects:  effects,
		Duration: CloudDuration, WaitTime: CloudWaitTime, Reapplication: CloudReapplication,
		RadiusOnUse: CloudRadiusOnUse,
		victims:     make(map[int32]int),
	}
	c.radiusPerTick = -c.radius / float32(CloudDuration)
	return c
}

// Restore sets the age and the radius a saved cloud had run to.
// The timing fields are set before the call, which derives the wait from them.
func (c *Cloud) Restore(age int, radius, radiusPerTick float32) {
	c.Reload(age, radius, radiusPerTick)
	c.waiting = age < c.WaitTime
}

// Reload loads serialized state while leaving the tick's waiting flag and victim cooldowns intact.
// vanilla: AreaEffectCloud.readAdditionalSaveData
func (c *Cloud) Reload(age int, radius, radiusPerTick float32) {
	c.TickCount = age
	c.radius = radius
	c.radiusPerTick = radiusPerTick
}

// Waiting is the pass's hold: the cloud's first wait-time ticks rain
// nothing yet.
func (c *Cloud) Waiting() bool { return c.waiting }

// Radius is the reach the cloud's rain covers now.
func (c *Cloud) Radius() float32 { return c.radius }

// RadiusPerTick is the shrink the cloud's radius takes each tick, the
// negative share of the radius over its six hundred.
func (c *Cloud) RadiusPerTick() float32 { return c.radiusPerTick }

// CloudTarget is the cloud's view of one body its rain may land on: the
// box the reach intersects, and the feet the radius measures from.
type CloudTarget struct {
	ArrowTarget
	FeetX, FeetZ float64
}

// Step advances the cloud one tick (AreaEffectCloud.serverTick) and returns
// the bodies this tick's pass reached, for the caller to apply the effects.
// Expiry or shrinking may end it; the pass runs every 5th tick after the
// wait; a reached body is skipped for 20 ticks. A body immune to all its
// effects is never reached.
// vanilla: AreaEffectCloud.serverTick
func (c *Cloud) Step(targets []CloudTarget) []CloudTarget {
	c.TickCount++
	if c.Duration != -1 && c.TickCount-c.WaitTime >= c.Duration {
		c.Dead = true
		return nil
	}
	if waiting := c.TickCount < c.WaitTime; waiting != c.waiting {
		c.waiting = waiting
	}
	if c.waiting {
		return nil
	}
	radius := c.radius
	if c.radiusPerTick != 0 {
		radius += c.radiusPerTick
		if radius < cloudMinRadius {
			c.Dead = true
			return nil
		}
		c.radius = radius
	}
	if c.TickCount%CloudApplicationFlip != 0 {
		return nil
	}

	for id, until := range c.victims {
		if c.TickCount >= until {
			delete(c.victims, id)
		}
	}
	if len(c.Effects) == 0 {
		clear(c.victims)
		return nil
	}
	var reached []CloudTarget
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: getEntitiesOfClass's NO_SPECTATORS
		}
		if _, held := c.victims[target.EntityID]; held {
			continue
		}
		// The candidate's box must meet the cloud's own: the scalable
		// radius-wide, half-block-tall box it stands in.
		if target.MinX >= c.X+float64(radius) || target.MaxX <= c.X-float64(radius) ||
			target.MinY >= c.Y+0.5 || target.MaxY <= c.Y ||
			target.MinZ >= c.Z+float64(radius) || target.MaxZ <= c.Z-float64(radius) {
			continue
		}
		if !cloudTakesEffect(c, target) {
			continue
		}
		dx, dz := target.FeetX-c.X, target.FeetZ-c.Z
		if dx*dx+dz*dz > float64(radius)*float64(radius) {
			continue
		}
		c.victims[target.EntityID] = c.TickCount + c.Reapplication
		reached = append(reached, *target)
		if c.RadiusOnUse != 0 {
			radius += c.RadiusOnUse
			if radius < cloudMinRadius {
				c.Dead = true
				break
			}
			c.radius = radius
		}
		if c.DurationOnUse != 0 && c.Duration != -1 {
			c.Duration += c.DurationOnUse
			if c.Duration <= 0 {
				c.Dead = true
				break
			}
		}
	}
	return reached
}

// cloudTakesEffect is the pass's immunity gate: a body none of the effects
// can land on is skipped entirely and never becomes a victim.
// vanilla: AreaEffectCloud.serverTick's canBeAffected noneMatch
func cloudTakesEffect(c *Cloud, target *CloudTarget) bool {
	for _, in := range c.Effects {
		if rules.CanBeAffected(target.Type, in.ID) {
			return true
		}
	}
	return false
}
