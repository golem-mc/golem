package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The bow follows the meal's pattern (food.go): Use Item starts a draw and
// sets hand-active, the tick counts it, and Player Action release-use fires
// with the power the draw earned.
// vanilla: BowItem, ProjectileWeaponItem, AbstractArrow

// bowSession is the draw in progress. item is the bow's item ID, so a
// mid-draw swap fires nothing. ticks is the vanilla elapsed use time plus
// one; 0 means no draw. limit is the use duration: the use ends on the tick
// ticks passes it, and 0 means it runs until release.
type bowSession struct {
	hand  int32
	item  int32
	ticks int32
	limit int32
}

// bowMinPower is the least power a release fires at; vanilla:
// BowItem.releaseUsing returns false below it.
const bowMinPower = 0.1

// handleUseBow starts a draw, which needs an arrow (findArrow) unless
// creative, and reports whether a draw is running.
func (c *client) handleUseBow(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemBow {
		return false
	}
	creative := mode == protocol.GameModeCreative
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
	if !creative && !c.findArrow().ok {
		return false
	}
	c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1}
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	return true
}

// bowFind is a found arrow: its slot in window numbering and its stack.
type bowFind struct {
	slot  int
	stack protocol.ItemStack
	ok    bool
}

// findArrow finds an arrows-tag item: off hand, main hand, then the first
// in the inventory (hotbar, then main rows). vanilla: Player.getProjectile
func (c *client) findArrow() bowFind {
	var found bowFind
	c.player.Inventory(func(inv *player.Inventory) {
		for _, slot := range []int{player.OffHandSlot, player.HotbarSlot + int(inv.Selected)} {
			if stack := inv.Slots[slot]; !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDArrows) {
				found = bowFind{slot: slot, stack: stack, ok: true}
				return
			}
		}
		// Inventory order is hotbar, then main rows; not the crafting grid or
		// armour.
		for _, run := range [][2]int{{player.HotbarSlot, player.HotbarSlots}, {player.MainSlot, player.MainInvSlots - player.HotbarSlots}} {
			for slot := run[0]; slot < run[0]+run[1]; slot++ {
				if stack := inv.Slots[slot]; !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDArrows) {
					found = bowFind{slot: slot, stack: stack, ok: true}
					return
				}
			}
		}
	})
	return found
}

// tickBowDraw advances the draw by one tick and cancels it on death or when the bow leaves the hand.
func (c *client) tickBowDraw() {
	c.bowMu.Lock()
	if c.bow.ticks <= 0 {
		c.bowMu.Unlock()
		return
	}
	if c.player.Vitals().Dead {
		c.bow.ticks = 0
		c.bowMu.Unlock()
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		return
	}
	// vanilla: LivingEntity.updatingUsingItem
	var inHand int32
	c.player.Inventory(func(inv *player.Inventory) {
		inHand = inv.Slots[rules.SlotOfHand(inv, c.bow.hand)].ItemID
	})
	if inHand != c.bow.item {
		c.bow = bowSession{}
		c.bowMu.Unlock()
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		return
	}
	c.bow.ticks++
	hand, item, ticks := c.bow.hand, c.bow.item, c.bow.ticks
	defer func() {
		if isSpyglassItem(item) {
			c.usingItemAdvancements(item)
		}
	}()
	// vanilla: LivingEntity.completeUsingItem
	finished := c.bow.limit > 0 && ticks > c.bow.limit
	bundle := rules.IsBundle(item)
	if finished {
		c.bow = bowSession{}
	}
	c.bowMu.Unlock()
	if bundle {
		c.tickBundleUse(hand, ticks) // the last tick still drops
		if finished {
			c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		}
		return
	}
	if finished {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		if isSpyglassItem(item) {
			// vanilla: SpyglassItem.finishUsingItem
			c.playSpyglassSound(registry.SoundItemSpyglassStopUsing)
		}
		return
	}
	// The crossbow's load touches the inventory, so it runs outside the session
	// lock.
	c.chargeCrossbow(hand, item, ticks)
}

// cancelBow drops a draw without firing, for a use that ends without
// release. vanilla: LivingEntity.stopUsingItem
func (c *client) cancelBow() {
	c.bowMu.Lock()
	if c.bow.ticks <= 0 {
		c.bowMu.Unlock()
		return
	}
	c.bow = bowSession{}
	c.bowMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
}

// releaseBow handles release-use: it fires the arrow (or throws a drawn
// trident, which shares the session), or drops the draw when the power is
// below bowMinPower, the bow left the hand, or the arrows ran out.
func (c *client) releaseBow() {
	c.bowMu.Lock()
	if c.bow.ticks <= 0 {
		c.bowMu.Unlock()
		return
	}
	hand, item, ticks := c.bow.hand, c.bow.item, c.bow.ticks
	elapsed := ticks - 1 // vanilla's timeHeld; ticks starts at 1
	c.bow = bowSession{}
	c.bowMu.Unlock()
	// The player may have swapped the drawn item away mid-draw.
	held := c.heldItem(hand)
	var spinStrength float64
	var tridentRelease bool
	if held.ItemID == item && registry.Item(held.ItemID) == registry.ItemTrident {
		spinStrength, tridentRelease = c.prepareTridentRelease(held, elapsed)
		if tridentRelease && spinStrength == 0 && c.player.GameMode() != protocol.GameModeCreative {
			c.spendUse()
		}
	}
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	if held.ItemID != item {
		return
	}
	if isSpyglassItem(held.ItemID) {
		c.playSpyglassSound(registry.SoundItemSpyglassStopUsing)
		c.startUseCooldown(held)
		return
	}
	if rules.IsBundle(held.ItemID) {
		return // the drops already happened
	}
	if isCrossbow(held) {
		// vanilla: CrossbowItem.releaseUsing
		if elapsed >= rules.CrossbowChargeTicks(held, &rules.EnchContext{Rand: c.ownDice()}) && len(held.ChargedProjectiles()) > 0 {
			c.startUseCooldown(held)
		}
		return // the charge fires nothing; the next use does
	}
	if registry.Item(held.ItemID) == registry.ItemTrident {
		if tridentRelease {
			c.releaseTrident(hand, held, spinStrength)
		}
		return
	}
	if registry.Item(held.ItemID) != registry.ItemBow {
		return // a goat horn's release does nothing
	}
	power := rules.BowPower(int(elapsed))
	if power < bowMinPower {
		return
	}
	creative := c.player.GameMode() == protocol.GameModeCreative
	ammo := c.findArrow()
	if !creative && !ammo.ok {
		return
	}

	// Speed is power*3 with inaccuracy 1 (vanilla: Projectile.shoot); a full
	// draw is critical. Infinity can make the arrow free (vanilla:
	// ProjectileWeaponItem.useAmmo); a free or creative arrow is a pickup for
	// creative players only. The bow is the fired-from weapon whose
	// enchantments the hit reads; a flame bow sets the arrow alight.
	pos := c.player.Position()
	ammoUse := int32(0) // infinite materials spend nothing
	if !creative {
		ammoUse = rules.ProcessAmmoUse(held, ammo.stack)
	}
	free := ammoUse == 0
	power, consume, ok := c.shootBowAllowed(held, power, !free)
	if !ok {
		return
	}
	free = !consume
	if !free && ammo.ok {
		c.consumeAmmo(ammo)
	}
	pickup := map[bool]entity.ArrowPickup{true: entity.PickupCreativeOnly, false: entity.PickupAllowed}[creative || free]
	fireTicks := rules.ProjectileIgniteTicks(held)
	rng := c.srv.levelRng
	aimX, aimY, aimZ := entity.LookVector(pos.Yaw, pos.Pitch)
	vx, vy, vz := entity.ShootVelocity(aimX, aimY, aimZ, power*3.0, 1.0, rng)
	// The spawn's heading is the bow velocity's: vanilla Projectile.shoot sets
	// yRot/xRot before shootFromRotation adds getKnownMovement.
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	// Add the shooter's movement (vanilla: Projectile.shootFromRotation's
	// getKnownMovement), vertical only when airborne.
	movement := c.player.KnownMovement()
	vx += movement.X
	vz += movement.Z
	if !pos.OnGround {
		vy += movement.Y
	}
	s := c.srv
	s.spawnArrowFrom(c.theWorld(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight-entity.ArrowSpawnBelow, pos.Z,
		vx, vy, vz, yaw, pitch,
		power == 1.0, entity.ArrowBaseDamage,
		pickup,
		c.player.EntityID, c.player.UUID, held, fireTicks, ammo.stack)
	// Played from the shooter's feet (vanilla: BowItem.releaseUsing), pitch
	// 1/(rand*0.4+1.2) + power*0.5.
	s.broadcastSoundID(c.theWorld(), registry.SoundEntityArrowShoot, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 1.0,
		float32(1.0/(rng.Float64()*0.4+1.2)+power*0.5))
	// vanilla: BowItem.releaseUsing
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	// vanilla: getDurabilityUse, one per shot.
	c.wearHeld(hand, 1)
	c.startUseCooldown(held)
}

// consumeAmmo takes one arrow from the slot and sends it, since the client
// waits for the server's decrement; watchers get an equipment update if the
// slot is visible.
func (c *client) consumeAmmo(ammo bowFind) {
	var payload []byte
	var visible bool
	c.player.Inventory(func(inv *player.Inventory) {
		stack := inv.Slots[ammo.slot]
		stack.Count--
		if stack.Count <= 0 {
			stack = protocol.ItemStack{}
		}
		inv.Set(ammo.slot, stack)
		payload = inv.WindowSlot(playerWindowID, int32(ammo.slot), stack)
		visible = player.VisibleSlot(inv, ammo.slot)
	})
	c.send(protocol.IDPlayContainerSlot, payload)
	if visible {
		c.broadcastEquipment()
	}
}
