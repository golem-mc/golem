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

// Box is the cloud's bounding box at its current radius.
// vanilla: AreaEffectCloud.getDimensions
func (c *Cloud) Box() AABB {
	r := float64(c.radius)
	return NewAABB(c.X-r, c.Y, c.Z-r, c.X+r, c.Y+0.5, c.Z+r)
}

// RadiusPerTick is the shrink the cloud's radius takes each tick, the
// negative share of the radius over its six hundred.
func (c *Cloud) RadiusPerTick() float32 { return c.radiusPerTick }

// CloudTarget is the cloud's view of one body its rain may land on: the
// box the reach intersects, and the feet the radius measures from.
type CloudTarget struct {
	ArrowTarget
	FeetX, FeetZ float64
}

// Step advances the cloud one tick and applies its pass to targets.
func (c *Cloud) Step(targets []CloudTarget) []CloudTarget {
	if !c.Advance() {
		return nil
	}
	return c.Apply(targets)
}

// Advance runs the tick's expiry, wait and shrink (AreaEffectCloud.serverTick)
// and reports whether this tick is one of the cloud's application passes
// (every 5th tick after the wait, with effects): only then does Apply need
// targets. Expiry or shrinking may end the cloud.
// vanilla: AreaEffectCloud.serverTick
func (c *Cloud) Advance() bool {
	c.TickCount++
	if c.Duration != -1 && c.TickCount-c.WaitTime >= c.Duration {
		c.Dead = true
		return false
	}
	if waiting := c.TickCount < c.WaitTime; waiting != c.waiting {
		c.waiting = waiting
	}
	if c.waiting {
		return false
	}
	if c.radiusPerTick != 0 {
		radius := c.radius + c.radiusPerTick
		if radius < cloudMinRadius {
			c.Dead = true
			return false
		}
		c.radius = radius
	}
	if c.TickCount%CloudApplicationFlip != 0 {
		return false
	}
	if len(c.Effects) == 0 {
		clear(c.victims)
		return false
	}
	return true
}

// Apply is the application pass Advance announced: it returns the bodies the
// cloud reached, for the caller to apply the effects. A reached body is
// skipped for 20 ticks. A body immune to all the effects is never reached.
// vanilla: AreaEffectCloud.serverTick
func (c *Cloud) Apply(targets []CloudTarget) []CloudTarget {
	radius := c.radius
	for id, until := range c.victims {
		if c.TickCount >= until {
			delete(c.victims, id)
		}
	}
	// The candidates are those meeting the box at the starting radius; the
	// radius a hit grows only moves the distance test below.
	box := c.Box()
	var reached []CloudTarget
	for i := range targets {
		target := &targets[i]
		if target.Unhittable {
			continue // vanilla: getEntitiesOfClass's NO_SPECTATORS
		}
		if _, held := c.victims[target.EntityID]; held {
			continue
		}
		if !box.Crosses(NewAABB(target.MinX, target.MinY, target.MinZ, target.MaxX, target.MaxY, target.MaxZ)) {
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
