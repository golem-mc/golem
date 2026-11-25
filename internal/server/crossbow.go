package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The crossbow uses the bow's session (bow.go): Use Item starts the charge,
// the tick loads charged_projectiles when it completes, and the next Use
// Item fires. Releasing the charge fires nothing.
// vanilla: CrossbowItem

// isFireworkRocket reports whether the stack is a firework rocket.
func isFireworkRocket(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemFireworkRocket
}

// findCrossbowAmmo finds a crossbow's load: an arrow or rocket in the off
// hand, then main hand, else the first arrow in the inventory.
// vanilla: Player.getProjectile, CrossbowItem.getSupportedHeldProjectiles
func (c *client) findCrossbowAmmo() bowFind {
	var found bowFind
	c.player.Inventory(func(inv *player.Inventory) {
		for _, slot := range []int{player.OffHandSlot, player.HotbarSlot + int(inv.Selected)} {
			if stack := inv.Slots[slot]; isFireworkRocket(stack) {
				found = bowFind{slot: slot, stack: stack, ok: true}
				return
			}
		}
	})
	if found.ok {
		return found
	}
	return c.findArrow()
}

// isCrossbow reports whether the stack is a crossbow.
func isCrossbow(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemCrossbow
}

// handleUseCrossbow fires a loaded crossbow or starts charging an unloaded
// one, reporting whether a use began or a shot went.
// vanilla: CrossbowItem.use
func (c *client) handleUseCrossbow(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if !isCrossbow(held) {
		return false
	}
	if len(held.ChargedProjectiles()) > 0 {
		c.fireCrossbow(hand)
		return true
	}
	c.bowMu.Lock()
	defer c.bowMu.Unlock()
	if c.bow.ticks > 0 {
		return true
	}
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if raised {
		return false
	}
	if mode != protocol.GameModeCreative && !c.findCrossbowAmmo().ok {
		return false
	}
	c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1}
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	return true
}

// chargeCrossbow plays the charging sounds and loads after ticks of charge.
// vanilla: CrossbowItem.onUseTick
func (c *client) chargeCrossbow(hand, item, ticks int32) {
	// The session counts from one at the use and ticks before this runs, so the
	// use's own tick is vanilla's zeroth.
	elapsed := ticks - 2
	held := c.heldItem(hand)
	if held.ItemID != item || !isCrossbow(held) || len(held.ChargedProjectiles()) > 0 {
		return
	}
	duration := rules.CrossbowChargeTicks(held, &rules.EnchContext{Rand: c.ownDice()})
	pos := c.player.Position()
	sounds := rules.ChargingSounds(held)
	play := func(sound protocol.SoundHolder, volume, pitch float32) {
		if sound.ID >= 0 || sound.Name != "" {
			c.srv.broadcastSoundHolder(c.theWorld(), sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, volume, pitch)
		}
	}
	start, mid, load := rules.CrossbowChargeStep(elapsed, duration)
	if start {
		play(sounds.Start, 0.5, 1.0)
	}
	if mid {
		play(sounds.Mid, 0.5, 1.0)
	}
	if load && c.loadCrossbow(hand, held) {
		play(sounds.End, 1.0, float32(1.0/(c.srv.levelRng.Float64()*0.5+1.0)+0.2))
	}
}

// loadCrossbow draws the volley's arrows into the crossbow, reporting
// whether it loaded.
// vanilla: CrossbowItem.tryLoadProjectiles, ProjectileWeaponItem.draw
func (c *client) loadCrossbow(hand int32, crossbow protocol.ItemStack) bool {
	creative := c.player.GameMode() == protocol.GameModeCreative
	ammo := c.findCrossbowAmmo()
	if !ammo.ok {
		if !creative {
			return false
		}
		ammo = bowFind{stack: protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 1}, slot: -1, ok: true}
	}
	var drawn []protocol.ItemStack
	for i := range rules.CrossbowProjectileCount(crossbow, c.itemEnchContext()) {
		use := int32(0)
		if i == 0 && !creative {
			use = rules.ProcessAmmoUse(crossbow, ammo.stack)
		}
		if use > ammo.stack.Count {
			continue
		}
		one := ammo.stack
		one.Count = 1
		if use == 0 {
			one.Intangible = true
		}
		drawn = append(drawn, one)
		if use > 0 {
			c.consumeAmmo(ammo, 1)
			ammo.stack.Count -= use
		}
	}
	if len(drawn) == 0 {
		return false
	}
	crossbow.SetChargedProjectiles(drawn)
	c.setHeld(hand, crossbow)
	return true
}

// setHeld replaces the hand's stack and tells the owner and watchers.
func (c *client) setHeld(hand int32, stack protocol.ItemStack) {
	var payload []byte
	var visible bool
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		inv.Set(slot, stack)
		payload = inv.WindowSlot(playerWindowID, int32(slot), stack)
		visible = player.VisibleSlot(inv, slot)
	})
	c.send(protocol.IDPlayContainerSlot, payload)
	if visible {
		c.broadcastEquipment()
	}
}

// fireCrossbow fires each loaded projectile, one wear each.
// vanilla: CrossbowItem.performShooting, ProjectileWeaponItem.shoot
func (c *client) fireCrossbow(hand int32) {
	crossbow := c.heldItem(hand)
	projectiles := crossbow.ChargedProjectiles()
	if len(projectiles) == 0 {
		return
	}
	crossbow.SetChargedProjectiles(nil)
	c.setHeld(hand, crossbow)

	s := c.srv
	rng := s.levelRng
	pos := c.player.Position()
	angles := rules.CrossbowAngles(len(projectiles), rules.CrossbowMaxSpread(crossbow, c.itemEnchContext()))
	anyFirework := slices.ContainsFunc(projectiles, isFireworkRocket)
	power := rules.CrossbowShotPower(anyFirework)
	for i, projectile := range projectiles {
		aimX, aimY, aimZ := entity.CrossbowShotVector(pos.Yaw, pos.Pitch, angles[i])
		vx, vy, vz := entity.ShootVelocity(aimX, aimY, aimZ, power, 1.0, rng)
		yaw, pitch := entity.ArrowAngles(vx, vy, vz)
		firework := isFireworkRocket(projectile)
		if firework {
			// vanilla: CrossbowItem.createProjectile
			rocket := s.newFirework(c.theWorld(), projectile, pos.X,
				pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight-0.15, pos.Z,
				c.player.EntityID, c.player.UUID, 0)
			rocket.rocket.ShotAtAngle = true
			rocket.rocket.VX, rocket.rocket.VY, rocket.rocket.VZ = vx, vy, vz
			rocket.rocket.Yaw, rocket.rocket.Pitch = yaw, pitch
			rocket.spawning = len(projectile.Enchantments) > 0
			s.registerFirework(rocket)
			if rocket.spawning {
				s.spawnEffects(spawnedProjectile{w: rocket.w, typ: protocol.EntityTypeFireworkRocket, body: rocket}, projectile, protocol.ItemStack{})
			}
			c.shotSoundAndWear(hand, i, firework)
			if c.heldItem(hand).Empty() {
				break
			}
			continue
		}
		pickup := entity.PickupAllowed
		if projectile.Intangible {
			pickup = entity.PickupCreativeOnly
		}
		// A shooter that is a player always shoots critical arrows.
		// vanilla: CrossbowItem.performShooting
		it := newArrowEntity(s, c.theWorld(),
			pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight-entity.ArrowSpawnBelow, pos.Z,
			vx, vy, vz, yaw, pitch, true, entity.ArrowBaseDamage, pickup,
			c.player.EntityID, c.player.UUID, crossbow, 0)
		armArrow(it, projectile)
		it.arrow.PierceLevel = rules.PiercingLevel(crossbow, projectile, &rules.EnchContext{Rand: s.levelRng})
		it.arrow.HitSound = crossbowHitSound
		s.launchArrowFrom(it, projectile, crossbow)
		c.shotSoundAndWear(hand, i, firework)
		if c.heldItem(hand).Empty() {
			break
		}
	}
	// vanilla: CrossbowItem.performShooting
	c.shotCrossbowAdvancements(crossbow)
	c.awardItemStat(registry.StatTypeUsed, crossbow.ItemID, 1)
}

// shotSoundAndWear plays the index-th projectile's shot sound and wears the
// crossbow. vanilla: CrossbowItem.getShotPitch, ProjectileWeaponItem.shoot
func (c *client) shotSoundAndWear(hand int32, index int, firework bool) {
	s := c.srv
	pos := c.player.Position()
	soundPitch := float32(1.0)
	if index > 0 {
		base := float32(0.43)
		if index&1 == 1 {
			base = 0.63
		}
		soundPitch = float32(1.0/(s.levelRng.Float64()*0.5+1.8)) + base
	}
	s.broadcastSoundID(c.theWorld(),
		registry.SoundItemCrossbowShoot, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 1.0, soundPitch)

	c.wearHeld(hand, rules.CrossbowWearPerShot(firework))
}
