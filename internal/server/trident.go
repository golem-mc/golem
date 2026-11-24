package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The trident's charge and throw follow the bow (bow.go): Use Item starts
// a draw on the same item-use state, and release-use throws. The charge is
// a tick count, not a power, and the throw sends the trident itself into
// the world with one wear that never breaks it.
// vanilla: TridentItem

// Throw numbers from TridentItem's bytecode: a 10-tick minimum charge
// (TridentItem.THROW_THRESHOLD_TIME), power 2.5
// (TridentItem.PROJECTILE_SHOOT_POWER) and inaccuracy 1.0.
const (
	tridentThrowThreshold = 10
	tridentThrowPower     = 2.5
	tridentThrowSpread    = 1.0
)

// handleUseTrident starts a trident draw, reporting whether one runs. A
// trident one wear from breaking does not draw: the throw would spend the
// last point, so vanilla refuses the use. vanilla: TridentItem.use
func (c *client) handleUseTrident(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemTrident {
		return false
	}
	if rules.NextWearBreaks(held) {
		return false
	}
	// A riptide charge starts only in water or rain.
	// vanilla: TridentItem.use
	if rules.SpinAttackStrength(held, &rules.EnchContext{Rand: c.ownDice()}) > 0 && !c.inWaterOrRain(c.player.Position()) {
		return false
	}
	c.bowMu.Lock()
	defer c.bowMu.Unlock()
	if c.bow.ticks > 0 {
		return true // a draw already running is not started again
	}
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if raised {
		return false
	}
	c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1}
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	return true
}

// prepareTridentRelease evaluates the strength once for the release's guard,
// vibration and action; initial use makes its own roll.
// vanilla: TridentItem.releaseUsing
func (c *client) prepareTridentRelease(held protocol.ItemStack, ticks int32) (strength float64, allowed bool) {
	if ticks < tridentThrowThreshold {
		return 0, false
	}
	strength = rules.SpinAttackStrength(held, &rules.EnchContext{Rand: c.ownDice()})
	if strength > 0 && (c.isPassenger() || !c.inWaterOrRain(c.player.Position())) {
		return strength, false
	}
	return strength, !rules.NextWearBreaks(held)
}

// releaseTrident throws a trident charged past its threshold, the thrower's
// movement added as for the bow. The throw spends one wear that never
// breaks it and the stack leaves the hand; a creative thrower keeps it,
// and only creative players may pick the thrown one up.
// vanilla: TridentItem.releaseUsing
func (c *client) releaseTrident(hand int32, held protocol.ItemStack, strength float64) {
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.wearHeldWith(hand, 1, rules.ProcessDurabilityChangeWithoutBreaking)
	// Negative strength wears the item and counts its use, but performs no action.
	// vanilla: TridentItem.releaseUsing
	if strength < 0 {
		return
	}
	defer c.startUseCooldown(held)
	if strength > 0 {
		c.startSpinAttack(hand, strength)
		return
	}
	creative := c.player.GameMode() == protocol.GameModeCreative

	// The thrown stack is the held one with the throw's wear (unbreaking may
	// spare it), count one. A creative thrower keeps theirs, unworn.
	// vanilla: hurtWithoutBreaking, consumeAndReturn
	stack := c.heldItem(hand)
	stack.Count = 1
	if !creative {
		// Read the slot under the inventory lock and spend it outside, as the bow
		// does.
		var heldSlot int
		var hasHeld bool
		c.player.Inventory(func(inv *player.Inventory) {
			heldSlot = rules.SlotOfHand(inv, hand)
			hasHeld = !inv.Slots[heldSlot].Empty()
		})
		if hasHeld {
			c.consumeAmmo(heldSlot, 1)
		}
	}

	pickup := entity.PickupAllowed
	if creative {
		pickup = entity.PickupCreativeOnly
	}
	pos := c.player.Position()
	rng := c.srv.levelRng
	aimX, aimY, aimZ := entity.LookVector(pos.Yaw, pos.Pitch)
	vx, vy, vz := entity.ShootVelocity(aimX, aimY, aimZ, tridentThrowPower, tridentThrowSpread, rng)
	// The spawn's heading is the throw velocity's: vanilla Projectile.shoot
	// sets yRot/xRot before shootFromRotation adds getKnownMovement.
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	// Add the thrower's movement (vanilla: Projectile.shootFromRotation's
	// getKnownMovement), vertical only when airborne.
	movement := c.player.KnownMovement()
	vx += movement.X
	vz += movement.Z
	if !pos.OnGround {
		vy += movement.Y
	}
	x := pos.X
	y := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight - entity.ArrowSpawnBelow
	z := pos.Z
	c.srv.spawnTrident(c.theWorld(), x, y, z, vx, vy, vz, yaw, pitch,
		pickup, c.player.EntityID, c.player.UUID, stack)
	// The throw sound, at volume and pitch 1.
	c.srv.broadcastSoundHolder(c.theWorld(), rules.TridentSound(stack), protocol.SoundCategoryPlayer,
		x, y, z, 1.0, 1.0)
}
