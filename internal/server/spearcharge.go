package server

import (
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The player's spear charge: holding use with a kinetic_weapon item. Use Item
// starts it, release or a held-item swap ends it, and each tick of it stabs
// what the look ray meets (KineticContact, shared with the mob's charge).

// playerBaseAttackDamage is the player's attack_damage base value, which a
// kinetic contact starts from instead of the attribute. vanilla:
// KineticWeapon.damageEntities, getAttributeBaseValue
const playerBaseAttackDamage = 1.0

// spearCharge is the running charge: hand and item (a swap ends it), ticks of
// use so far, and the tick each body was last hit at.
// vanilla: LivingEntity.recentKineticEnemies
type spearCharge struct {
	hand    int32
	item    int32
	ticks   int32
	active  bool
	stabbed map[int32]int32
}

// handleUseSpear starts a charge with a kinetic_weapon item in the hand,
// reporting whether it did. vanilla: Item.use (startUsingItem)
func (c *client) handleUseSpear(hand int32) bool {
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if held.Empty() || registry.StackKineticWeapon(held) == nil {
		return false
	}
	c.eatMu.Lock()
	eating := c.eat.ticks > 0
	c.eatMu.Unlock()
	c.bowMu.Lock()
	drawing := c.bow.ticks > 0
	c.bowMu.Unlock()
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if eating || drawing || raised {
		return false
	}
	c.spearMu.Lock()
	if c.spear.active {
		c.spearMu.Unlock()
		return false
	}
	c.spear = spearCharge{hand: hand, item: held.ItemID, active: true, stabbed: map[int32]int32{}}
	c.spearMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	if k := registry.StackKineticWeapon(held); k.Sound != registry.SoundNone {
		// The user's client plays it itself. vanilla: KineticWeapon.makeSound
		pos := c.player.Position()
		c.srv.broadcastSoundIDExcept(c.theWorld(), c, k.Sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)
	}
	return true
}

// releaseSpear ends the charge. vanilla: LivingEntity.stopUsingItem
func (c *client) releaseSpear() {
	c.spearMu.Lock()
	was := c.spear.active
	c.spear = spearCharge{}
	c.spearMu.Unlock()
	if was {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	}
}

// tickSpearCharge ends the charge on death, spectator or the item leaving the
// hand, else stabs along the look for this tick of use.
// vanilla: ItemStack.onUseTick, KineticWeapon.damageEntities
func (c *client) tickSpearCharge() {
	c.spearMu.Lock()
	sp := c.spear
	c.spearMu.Unlock()
	if !sp.active {
		return
	}
	v := c.player.Vitals()
	held := c.heldItem(sp.hand)
	k := registry.StackKineticWeapon(held)
	if v.Dead || v.GameMode == protocol.GameModeSpectator || held.ItemID != sp.item || k == nil {
		c.releaseSpear()
		return
	}
	// A release between the copy and here clears the session: count the tick
	// only if it is still running. sp.stabbed is private to this goroutine.
	c.spearMu.Lock()
	still := c.spear.active
	if still {
		c.spear.ticks++
	}
	c.spearMu.Unlock()
	if !still {
		return
	}
	used := int(sp.ticks)
	if used < int(k.Delay) {
		return
	}
	used -= int(k.Delay)

	w := c.theWorld()
	pos := c.player.Position()
	minReach, maxReach, margin := rules.AttackReach(held, v.GameMode == protocol.GameModeCreative, c.entityRange())
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)
	movement := c.player.KnownMovement()
	// vanilla: KineticWeapon.getMotion, Entity.getKnownSpeed
	forward := movement.X*lx + movement.Y*ly + movement.Z*lz
	ray := entity.PiercingRay{
		EyeX: pos.X, EyeY: pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight, EyeZ: pos.Z,
		LookX: lx, LookY: ly, LookZ: lz,
		MinReach: minReach, MaxReach: maxReach, Margin: margin, Forward: forward,
	}
	players, mobs, hits := c.piercingTargets(w, ray)
	damageType := registry.StackDamageType(held)
	if damageType == 0 {
		damageType = rules.DamageTypePlayerAttack
	}
	// The hit mobs' known speeds, read under one lock for the whole tick.
	var mobVel map[int]([3]float64)
	for _, i := range hits {
		if i >= len(players) {
			if mobVel == nil {
				mobVel = map[int][3]float64{}
				c.srv.mobsMu.Lock()
			}
			m := mobs[i-len(players)].mob
			mobVel[i] = m.KnownSpeed()
		}
	}
	if mobVel != nil {
		c.srv.mobsMu.Unlock()
	}
	affected := false
	for _, i := range hits {
		var id int32
		var target [3]float64 // the body's movement per tick
		if i < len(players) {
			p := players[i]
			id = p.player.EntityID
			m := p.player.KnownMovement()
			target = [3]float64{m.X, m.Y, m.Z}
		} else {
			m := mobs[i-len(players)].mob
			id = m.ID
			target = mobVel[i]
		}
		if at, ok := sp.stabbed[id]; ok && sp.ticks-at < k.ContactCooldown {
			continue
		}
		sp.stabbed[id] = sp.ticks
		stab, damage, ok := entity.KineticContact(k, used, 1, forward*20,
			(lx*target[0]+ly*target[1]+lz*target[2])*20, playerBaseAttackDamage)
		if !ok {
			continue
		}
		pw := registry.PiercingWeapon{DealsKnockback: stab.Knockback}
		var hit bool
		if i < len(players) {
			hit = c.stabPlayer(players[i], held, pw, damageType, damage, 1, stab.DealsDamage)
			if stab.Dismount {
				players[i].StopRiding()
				hit = true
			}
		} else {
			ent := mobs[i-len(players)]
			hit = c.stabMob(ent, held, pw, damageType, damage, 1, stab.DealsDamage)
			if stab.Dismount && c.srv.dismountStabbed(ent) {
				hit = true
			}
		}
		affected = affected || hit
	}
	if affected {
		c.srv.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(c.player.EntityID, protocol.EntityStatusKineticHit))
		// Every body the charge has met so far, players and mobs alike, is
		// living. vanilla: LivingEntity.stabbedEntities
		if progress := c.advancements.Load(); progress != nil {
			progress.SpearMobs(len(sp.stabbed), time.Now())
		}
	}
}
