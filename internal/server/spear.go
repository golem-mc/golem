package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// stab runs a spear's Stab action with a piercing_weapon main hand: every
// player and mob along the look within attack range takes the attack
// damage, each wearing the item once. The charge is spent hit or not.
// vanilla: ServerGamePacketListenerImpl.handlePlayerAction (STAB),
// PiercingWeapon.attack, Player.stabAttack
func (c *client) stab() {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	held := c.heldItem(protocol.HandMain)
	ticks := int(c.attackStrengthTicks.Load())
	attackSpeed := c.attackSpeed()
	if rules.CannotAttackWithItem(held, ticks, rules.StabChargeTolerance, attackSpeed) {
		return
	}
	pw, ok := registry.StackPiercingWeapon(held)
	if !ok {
		return
	}
	w := c.theWorld()
	pos := c.player.Position()
	minReach, maxReach, margin := rules.AttackReach(held, c.player.GameMode() == protocol.GameModeCreative, c.entityRange())
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)
	movement := c.player.KnownMovement()
	ray := entity.PiercingRay{
		EyeX: pos.X, EyeY: pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight, EyeZ: pos.Z,
		LookX: lx, LookY: ly, LookZ: lz,
		MinReach: minReach, MaxReach: maxReach, Margin: margin,
		// vanilla: Entity.getKnownMovement dotted with the look
		Forward: movement.X*lx + movement.Y*ly + movement.Z*lz,
	}

	players, mobs, hits := c.piercingTargets(w, ray)

	damageType := registry.StackDamageType(held)
	if damageType == 0 {
		damageType = rules.DamageTypePlayerAttack
	}
	strength := rules.AttackStrengthScale(ticks, attackSpeed)
	attackDamage := c.attackDamage()
	hitSomething := false
	for _, i := range hits {
		var hit bool
		if i < len(players) {
			hit = c.stabPlayer(players[i], held, pw, damageType, attackDamage, strength, true)
		} else {
			hit = c.stabMob(mobs[i-len(players)], held, pw, damageType, attackDamage, strength, true)
		}
		hitSomething = hitSomething || hit
	}
	for _, b := range c.piercingBodies(w, ray) {
		hitSomething = c.stabBody(b, held, pw, damageType, attackDamage, strength) || hitSomething
	}
	// vanilla: Player.onAttack
	c.attackStrengthTicks.Store(0)
	c.postPiercingAttack(w, held)
	if hitSomething && pw.HitSound != registry.SoundNone {
		c.srv.broadcastSoundID(w,
			pw.HitSound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)

	}
	if pw.Sound != registry.SoundNone {
		// The jabber is the source, so their client, which played it, is skipped.
		c.srv.broadcastSoundIDExcept(w, c,
			pw.Sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)

	}
	if c.swing.startIfAble(c.swingDuration(registry.StackAttackAnimation(held))) {
		c.srv.broadcastExcept(c, protocol.IDPlaySwingAnimation, swingAttackArm(c.player, held))
	}
}

// inPiercingRange is false for a box the ray cannot reach: its nearest point
// to the eye lies beyond the reach, the speed bonus and the margin.
func inPiercingRange(r entity.PiercingRay, b entity.AABB) bool {
	minX, minY, minZ, maxX, maxY, maxZ := b.Corners()
	dx := r.EyeX - min(max(r.EyeX, minX), maxX)
	dy := r.EyeY - min(max(r.EyeY, minY), maxY)
	dz := r.EyeZ - min(max(r.EyeZ, minZ), maxZ)
	reach := r.MaxReach + max(0, r.Forward) + r.Margin
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

// piercingTargets lists the players and mobs a ray can hit and the indices
// it does hit, players first then mobs.
func (c *client) piercingTargets(w *world.World, ray entity.PiercingRay) (players []*client, mobs []*mobEntity, hits []int) {
	// Candidate boxes are read under their locks and the ray walked after, so
	// no world read happens under the mob lock.
	pvp := c.theWorld().BoolRule(world.RulePvp)
	mount, boat, cart := c.mount.Load(), c.vehicle.Load(), c.minecart.Load()
	var boxes []entity.AABB
	c.srv.mu.RLock()
	for _, v := range c.srv.clients {
		// A spectator is not pickable, and players are not targets with pvp off
		// or to a teammate under friendly fire off.
		// vanilla: PiercingWeapon.canHitEntity, Player.canHarmPlayer
		if v == c || !pvp || v.theWorld() != w || v.player.Vitals().Dead ||
			v.player.GameMode() == protocol.GameModeSpectator ||
			!c.srv.scoreboard.CanHarm(v.player.Name, c.player.Name) {
			continue
		}
		// vanilla: PiercingWeapon.canHitEntity, Entity.isPassengerOfSameVehicle
		if mount != nil && v.mount.Load() == mount || boat != nil && v.vehicle.Load() == boat ||
			cart != nil && v.minecart.Load() == cart {
			continue
		}
		box := entity.NewAABB(v.Box())
		if !inPiercingRange(ray, box) {
			continue
		}
		players = append(players, v)
		boxes = append(boxes, box)
	}
	c.srv.mu.RUnlock()
	c.srv.mobsMu.Lock()
	for m, ent := range c.srv.herdOf(w).book {
		// vanilla: PiercingWeapon.canHitEntity, Entity.isInvulnerableToPiercingWeapon
		if m.Dead || m.Invulnerable {
			continue
		}
		// vanilla: PiercingWeapon.canHitEntity, Entity.isPassengerOfSameVehicle
		if mount != nil && (ent == mount || m.Vehicle == mount.mob || mount.mob.Vehicle == m) ||
			boat != nil && ent.vehicle == boat || cart != nil && ent.cart == cart {
			continue
		}
		box := entity.NewAABB(m.BlastBox())
		if !inPiercingRange(ray, box) {
			continue
		}
		mobs = append(mobs, ent)
		boxes = append(boxes, box)
	}
	c.srv.mobsMu.Unlock()

	return players, mobs, entity.PiercingHits(w, ray, boxes)
}

// stabBody is a non-living body a jab can meet: its box, its entity type for
// the enchantment context, and the hurt a blow of the damage lands on it.
type stabBody struct {
	id, typeID int32
	box        entity.AABB
	hurt       func(damage float64) bool
	// push adds to the body's velocity; nil for a body a push does not move.
	push func(x, y, z float64)
}

// piercingBodies lists the boats, minecarts and end crystals a ray hits.
// vanilla: PiercingWeapon.canHitEntity
func (c *client) piercingBodies(w *world.World, ray entity.PiercingRay) []stabBody {
	s := c.srv
	creative := c.player.GameMode() == protocol.GameModeCreative
	ownBoat, ownCart := c.vehicle.Load(), c.minecart.Load()
	var bodies []stabBody
	var boxes []entity.AABB
	add := func(b stabBody) {
		if inPiercingRange(ray, b.box) {
			bodies = append(bodies, b)
			boxes = append(boxes, b.box)
		}
	}
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w != w || it.boat.Dead || it == ownBoat {
			continue
		}
		add(stabBody{id: it.boat.ID, typeID: it.entityTypeID(), box: entity.NewAABB(it.boat.Box()),
			hurt: func(damage float64) bool {
				landed, metadata := s.boatHurt(it, damage, registry.DamageTypeSpear, creative, c.player.EntityID)
				if metadata != nil {
					s.broadcastWorld(w, protocol.IDPlayEntityMetadata, metadata)
				}
				return landed
			},
			push: func(x, y, z float64) { s.pushBoat(it, x, y, z) }})
	}
	s.boatsMu.Unlock()
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.w != w || it.cart.Dead || it == ownCart {
			continue
		}
		add(stabBody{id: it.cart.ID, typeID: it.entityTypeID(), box: entity.NewAABB(it.cart.Box()),
			hurt: func(damage float64) bool {
				return s.minecartHurtBy(it, w, damage, creative, entity.TNTCartHit{OwnerID: c.player.EntityID})
			},
			push: func(x, y, z float64) { s.pushMinecart(it, x, y, z) }})
	}
	s.minecartsMu.Unlock()
	s.endCrystalsMu.Lock()
	for _, ent := range s.endCrystals {
		// An invulnerable crystal is skipped even for a creative jabber.
		if ent.w != w || ent.crystal.Dead || ent.crystal.Invulnerable {
			continue
		}
		id := ent.crystal.ID
		add(stabBody{id: id, typeID: protocol.EntityTypeEndCrystal, box: entity.NewAABB(ent.crystal.BlastBox()),
			hurt: func(float64) bool {
				s.endCrystalsMu.Lock()
				ent := s.endCrystalByIDLocked(w, id)
				if ent != nil && s.entityDamageListened() {
					uid := ent.uuid
					s.endCrystalsMu.Unlock()
					if _, ok := s.askNonlivingDamage(w, uid, registry.DamageTypeSpear, &damageCause{entityID: c.player.EntityID}, 1); !ok {
						return false
					}
					s.endCrystalsMu.Lock()
					ent = s.endCrystalByIDLocked(w, id)
				}
				if ent == nil {
					s.endCrystalsMu.Unlock()
					return false
				}
				s.removeEndCrystalLocked(ent)
				s.endCrystalsMu.Unlock()
				s.destroyEndCrystal(ent, c.player.EntityID)
				s.explodeEndCrystal(ent, c.player.EntityID, c.player.Name, c.player.UUID)
				return true
			}})
	}
	s.endCrystalsMu.Unlock()
	hits := entity.PiercingHits(w, ray, boxes)
	out := make([]stabBody, len(hits))
	for i, h := range hits {
		out[i] = bodies[h]
	}
	return out
}

// stabBody lands a jab on a non-living body. It wears nothing and counts no
// use. vanilla: Player.stabAttack, Player.itemAttackInteraction
func (c *client) stabBody(b stabBody, held protocol.ItemStack, pw registry.PiercingWeapon,
	damageType registry.DamageType, attackDamage, strength float64) bool {
	ctx := attackEnchContext(damageType, b.typeID)
	damage, magic := rules.StabDamage(held, &ctx, attackDamage, strength)
	landed := b.hurt(damage)
	c.stabEndsSprint(pw)
	if pw.DealsKnockback && b.push != nil {
		// vanilla: Player.causeExtraKnockback pushes a non-living target
		// whether or not the blow landed.
		yaw := float64(c.player.Position().Yaw)
		for _, k := range [...]float64{rules.StabKnockback,
			rules.AttackKnockback(rules.EnchantedKnockback(held, &ctx, c.attackKnockbackBase()), false)} {
			if k > 0 {
				b.push(rules.ExtraKnockbackPush(yaw, k))
			}
		}
	}
	if !rules.StabCountsAsHit(landed, pw.DealsKnockback) {
		return false
	}
	if magic > 0 {
		c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityAnimation,
			entityAnimationPayload(b.id, protocol.AnimationMagicCriticalHit))
	}
	c.exhaust(rules.AttackExhaustion)
	return true
}

// postPiercingAttack runs the main-hand weapon's post_piercing_attack effects
// on the jabber, hit or miss: the wear, exhaustion, dash and sound of Lunge.
// vanilla: LivingEntity.postPiercingAttack, EnchantmentHelper.doPostPiercingAttackEffects
func (c *client) postPiercingAttack(w *world.World, held protocol.ItemStack) {
	pos := c.player.Position()
	vitals := c.player.Vitals()
	user := rules.EnchUser{
		Riding:     c.vehicleKind() != "",
		FallFlying: c.player.Appearance().FallFlying,
		InWater:    c.waterAt(pos) != rules.Dry,
		IsPlayer:   true,
		GameMode:   [...]string{"survival", "creative", "adventure", "spectator"}[c.player.GameMode()],
		Food:       vitals.Food,
	}
	for _, a := range rules.PostPiercingAttack(held, user, pos.Yaw, pos.Pitch) {
		c.wearHeld(protocol.HandMain, a.Wear)
		c.exhaust(a.Exhaustion)
		c.srv.applyPackEffects(w, a.Effect, a.Level, enchantTarget{uuid: c.player.UUID, name: c.player.Name, player: true, x: pos.X, y: pos.Y, z: pos.Z,
			width: playerWidth(c), height: playerHeight(c), entityID: c.player.EntityID})
		if a.HasImpulse {
			c.addVelocity(w, a.Impulse[0], a.Impulse[1], a.Impulse[2])
			// vanilla: ApplyEntityImpulse.apply
			c.player.UpdateVitals(func(v *player.Vitals) { rules.ApplyImpulseGrace(v, 10) })
		}
		if a.Sound.Name != "" || a.Sound.ID >= 0 {
			c.srv.broadcastSoundHolder(w, a.Sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)
		}
	}
}

// stabBlow is how a jab differs from a swing when it lands on a mob.
type stabBlow struct {
	damageType registry.DamageType
	// knockback is the weapon's deals_knockback.
	knockback bool
}

// stabMob lands a jab on a mob (hurts false: a kinetic contact whose damage
// condition failed), reporting whether it counts as a hit: one
// that knocks back counts even if the hurt did not land.
func (c *client) stabMob(ent *mobEntity, held protocol.ItemStack, pw registry.PiercingWeapon,
	damageType registry.DamageType, attackDamage, strength float64, hurts bool) bool {
	ctx := attackEnchContext(damageType, mobEntityType(ent.mob.Type))
	damage, magic := rules.StabDamage(held, &ctx, attackDamage, strength)
	if !hurts {
		damage = 0
	}
	pos := c.player.Position()
	attacker := entity.Player{
		X: pos.X, Y: pos.Y, Z: pos.Z,
		Yaw:      float64(pos.Yaw),
		EntityID: c.player.EntityID,
		Huntable: true,
		Creative: c.player.GameMode() == protocol.GameModeCreative,
	}
	attacker = c.sized(attacker)
	c.srv.mobsMu.Lock()
	if ent.mob.Dead {
		c.srv.mobsMu.Unlock()
		return false
	}
	healthBefore := ent.mob.Health
	// vanilla: Guardian.hurtServer, only where Player.stabAttack deals damage
	var pricks *rules.Killer
	if hurts && ent.mob.ThornsOut() {
		pricks = c.srv.mobKiller(ent)
	}
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &ctx, c.attackKnockbackBase()), false)
	oldVX, oldVY, oldVZ, oldImpulse := ent.mob.VX, ent.mob.VY, ent.mob.VZ, ent.mob.Impulse
	hurt, ink, laid, softened := landMobHurt(ent, damage, attacker, extra, held, ctx,
		&stabBlow{damageType: damageType, knockback: pw.DealsKnockback})
	kb := c.srv.holdMobKnockback(ent, c.player.UUID, oldVX, oldVY, oldVZ, oldImpulse)
	var thorns mobThorns
	if laid && hurts {
		// vanilla: Player.stabAttack's wasHurt needs dealsDamage
		thorns = mobRetaliations(ent, &ctx, held)
	}
	mx, my, mz := ent.mob.X, ent.mob.Y, ent.mob.Z
	kind := ent.mob.Type
	_, lowY, _, _, highY, _ := ent.mob.BlastBox()
	dealt := healthBefore - ent.mob.Health
	resisted, absorbed := ent.mob.BlowResisted, ent.mob.BlowAbsorbed
	c.srv.mobsMu.Unlock()
	if kb != nil {
		vx, vy, vz := c.srv.applyMobKnockback(kb)
		if hurt != nil {
			hurt.vx, hurt.vy, hurt.vz = vx, vy, vz
		}
	}
	if pricks != nil {
		c.srv.guardianThorns(c, ent.mob.ID, mx, my, mz, pricks)
	}
	if laid {
		c.awardDamageStat(registry.CustomStatDamageDealtResisted, resisted)
		c.awardDamageStat(registry.CustomStatDamageDealtAbsorbed, absorbed)
	}
	c.stabEndsSprint(pw)

	w := c.theWorld()
	if laid {
		if softened {
			c.srv.sendInk(w, kind, ink)
		} else {
			hurt.damageType = damageType
			c.srv.broadcastMobHurt(w, hurt)
		}
		c.thornsFromMob(w, thorns, syncVelocityNow)
	}
	if !rules.StabCountsAsHit(laid, pw.DealsKnockback) {
		return false
	}
	c.stabFeedback(w, ent.mob.ID, held, magic, mx, (lowY+highY)/2, mz, dealt)
	return true
}

// stabEndsSprint ends the sprint when the weapon knocks back, landed or not.
// vanilla: Player.stabAttack, Player.causeExtraKnockback
func (c *client) stabEndsSprint(pw registry.PiercingWeapon) {
	if pw.DealsKnockback {
		c.endSprintAfterKnockback(rules.StabKnockback)
	}
}

// stabPlayer lands a jab on a player, reporting a hit as stabMob does.
func (c *client) stabPlayer(victim *client, held protocol.ItemStack, pw registry.PiercingWeapon,
	damageType registry.DamageType, attackDamage, strength float64, hurts bool) bool {
	ctx := attackEnchContext(damageType, protocol.EntityTypePlayer)
	damage, magic := rules.StabDamage(held, &ctx, attackDamage, strength)
	if !hurts {
		damage = 0
	}
	c.stabEndsSprint(pw)
	from := c.player.Position()
	healthBefore := victim.player.Vitals().Health
	var landed, softened bool
	if hurts {
		landed, softened = victim.hurt(damage, damageType, &damageCause{
			entityID: c.player.EntityID,
			x:        from.X, y: from.Y, z: from.Z,
			weapon:         held,
			killer:         &rules.Killer{Name: c.DisplayName(), Weapon: held},
			disableSeconds: disableSeconds(held),
			shove:          true,
		})
	}
	w := c.theWorld()
	// The victim's client gets a shove only while hurtMarked is set: by a
	// whole hurt the shield did not fully block, not a miss or a window-only
	// hit. The first shove clears it, so the enchantments' following shove
	// never reaches the victim.
	// vanilla: Player.stabAttack, Player.causeExtraKnockback, LivingEntity.hurtServer
	if pw.DealsKnockback && landed && !softened {
		victim.knockbackFromPointExtraOnlyBy(w, c.player.UUID, float64(from.Yaw), rules.StabKnockback, syncVelocityNow)
	}
	if landed {
		// The victim's pieces answer before the weapon's own effects.
		// vanilla: EnchantmentHelper.doPostAttackEffectsWithItemSource
		c.srv.postAttack(w, attackBody{player: c}, attackBody{player: victim}, held, &ctx, syncVelocityNow)
	}
	if !rules.StabCountsAsHit(landed, pw.DealsKnockback) {
		return false
	}
	to := victim.player.Position()
	c.stabFeedback(w, victim.player.EntityID, held, magic,
		to.X, to.Y+playerHeight(victim)/2, to.Z, healthBefore-victim.player.Vitals().Health)
	return true
}

// stabFeedback follows each body a jab hit: magic critical, item wear,
// damage indicator, exhaustion. A jab plays no strong, weak or critical
// sound. vanilla: Player.stabAttack, attackVisualEffects
func (c *client) stabFeedback(w *world.World, targetID int32, held protocol.ItemStack, magic, x, y, z, dealt float64) {
	if magic > 0 {
		c.srv.broadcastWorld(w, protocol.IDPlayEntityAnimation,
			entityAnimationPayload(targetID, protocol.AnimationMagicCriticalHit))
	}
	c.wearHeld(protocol.HandMain, registry.StackDamagePerAttack(held))
	c.awardWeaponHit(held)
	c.damageDealt(w, x, y, z, dealt)
	c.exhaust(rules.AttackExhaustion)
}
