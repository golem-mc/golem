package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// attackBody identifies a living participant without holding its body lock.
// An empty body represents a projectile with no living owner.
type attackBody struct {
	player    *client
	mob       *entity.Mob
	mannequin *mannequinEntity
	// w is the world a mob body lives in when it is not the hit's; nil means the hit's.
	w *world.World
}

// at is the world holding b's body, given the world of the hit.
func (b attackBody) at(w *world.World) *world.World {
	if b.w != nil {
		return b.w
	}
	return w
}

func (s *Server) attackBodyEntity(b attackBody) plugin.Entity {
	switch {
	case b.player != nil:
		return pluginPlayer{c: b.player}
	case b.mob != nil:
		if ent := s.mobEntityOf(b.mob); ent != nil {
			return pendingMob{id: ent.uuid}
		}
	case b.mannequin != nil:
		return pluginEntity{id: b.mannequin.uuid}
	}
	return nil
}

// attackBodyEntityLocked is attackBodyEntity with mobsMu held.
func (s *Server) attackBodyEntityLocked(w *world.World, b attackBody) plugin.Entity {
	switch {
	case b.player != nil:
		return pluginPlayer{c: b.player}
	case b.mob != nil:
		if ent := s.herdOf(b.at(w)).book[b.mob]; ent != nil {
			return pendingMob{id: ent.uuid}
		}
	case b.mannequin != nil:
		return pluginEntity{id: b.mannequin.uuid}
	}
	return nil
}

func (b attackBody) cause(s *Server, w *world.World) *damageCause {
	if b.player != nil {
		return b.player.retaliationCause()
	}
	if b.mannequin != nil {
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		m := b.mannequin.mannequin
		return &damageCause{entityID: m.ID, x: m.X, y: m.Y, z: m.Z, weapon: m.MainHand, shove: true}
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := b.mob
	return &damageCause{entityID: m.ID, x: m.X, y: m.Y, z: m.Z, weapon: m.MainHand, killer: s.mobKiller(s.herdOf(b.at(w)).book[m]), shove: true}
}

// postAttack dispatches weapon and victim-equipment actions for a landed hit.
// Body mutations take their own locks; summons and packets run after them.
// vanilla: Enchantment.doPostAttack
func (s *Server) postAttack(w *world.World, attacker, victim attackBody, weapon protocol.ItemStack, ctx *rules.EnchContext, when velocitySync) {
	here := *ctx
	ctx = &here
	if c := attacker.player; c != nil {
		ctx.DirectAttackerFallDistance = c.player.Vitals().FallDistance
		ctx.DirectAttackerFlying = c.fallFlying.Load() || c.flying.Load()
	}
	var x, y, z float64
	if victim.player != nil {
		at := victim.player.player.Position()
		x, y, z = at.X, at.Y, at.Z
	} else if victim.mannequin != nil {
		s.mannequinsMu.Lock()
		m := victim.mannequin.mannequin
		x, y, z = m.X, m.Y, m.Z
		s.mannequinsMu.Unlock()
	} else {
		s.mobsMu.Lock()
		x, y, z = victim.mob.X, victim.mob.Y, victim.mob.Z
		s.mobsMu.Unlock()
	}
	bx, by, bz := rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)
	ctx.Thundering = w.IsThundering()
	ctx.CanSeeSky = w.SkyLightAt(bx, by, bz) == 15
	ctx.Block = registry.BlockOf(w.BlockAt(bx, by, bz))
	ctx.Body = nil
	s.bindBlowLoot(w, ctx, attacker, victim, victim.mob != nil)
	var actions []rules.PostAttackAction
	if c := victim.player; c != nil {
		var pieces [len(player.EquipmentSlots)]rules.EnchPiece
		c.player.Inventory(func(inv *player.Inventory) {
			stacks := inv.Equipment()
			for i, stack := range stacks {
				pieces[i] = rules.EnchPiece{Stack: stack, Slot: player.EquipmentSlots[i]}
			}
		})
		actions = rules.PostAttackEffects(weapon, pieces[:], ctx, c.ownDice())
	} else if victim.mannequin != nil {
		// The conditions read the mannequin's body, which takes its lock.
		s.mannequinsMu.Lock()
		pieces := victim.mannequin.mannequin.EnchEquipment()
		s.mannequinsMu.Unlock()
		actions = rules.PostAttackEffects(weapon, pieces, ctx, s.levelRng)
	} else {
		s.mobsMu.Lock()
		actions = rules.PostAttackEffects(weapon, victim.mob.EnchEquipment(), ctx, victim.mob.Rand)
		s.mobsMu.Unlock()
	}
	for _, a := range actions {
		target := victim
		switch a.Target {
		case "attacker":
			target = attacker
		case "damaging_entity":
			// A melee blow's direct entity is the attacker; a projectile is separate.
			if !ctx.Direct {
				continue
			}
			target = attacker
		case "victim":
		default:
			continue
		}
		if target.player == nil && target.mob == nil && target.mannequin == nil {
			continue
		}
		owner := victim
		if a.Piece < 0 {
			owner = attacker
		}
		var cause *damageCause
		if a.Effect.DamageEntity != nil {
			cause = owner.cause(s, w)
		}
		s.applyPostAttack(w, target, owner, a, cause, when)
	}
}

func (s *Server) applyPostAttack(w *world.World, target, owner attackBody, a rules.PostAttackAction, cause *damageCause, when velocitySync) {
	e := a.Effect
	// The target's body and feedback live in its own world; effects that take
	// the level run in the hit's. vanilla: Enchantment.applyEffects
	tw := target.at(w)
	var x, y, z float64
	var silent bool
	var body enchantTarget
	category := int32(protocol.SoundCategoryPlayer)
	if c := target.player; c != nil {
		at := c.player.Position()
		x, y, z = at.X, at.Y, at.Z
		body = enchantTarget{uuid: c.player.UUID, name: c.player.Name, player: true, width: playerWidth(c), height: playerHeight(c)}
		for _, in := range a.MobEffects(c.ownDice()) {
			c.effectCause = effect.CauseAttack
			c.AddEffect(in)
		}
		if e.Duration != nil {
			c.igniteForTicksBy(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level))), s.attackBodyEntity(owner))
		}
		c.exhaust(float64(rules.EnchLevelValueAt(e.ApplyExhaustion, a.Level)))
		if i := e.ApplyImpulse; i != nil {
			impulse := rules.LookImpulse(at.Yaw, at.Pitch, i.CoordinateScale, float64(rules.EnchLevelValueAt(i.Magnitude, a.Level)))
			c.addVelocity(w, impulse[0], impulse[1], impulse[2])
			c.player.UpdateVitals(func(v *player.Vitals) { rules.ApplyImpulseGrace(v, 10) })
		}
		if e.DamageEntity != nil {
			hit := a.Retaliation(c.ownDice())
			if owner.mob != nil {
				hit.Damage = rules.ScaledWithDifficulty(hit.Damage, w.Difficulty())
			}
			if landed, softened := c.hurt(hit.Damage, hit.DamageType, cause); landed && !softened {
				c.knockbackFromPoint(w, cause.x, cause.y, cause.z, when)
				if hit.DamageType == registry.DamageTypeThorns {
					s.broadcastSoundID(w, thornsHitSound, category, x, y, z, 1, 1)
				}
			}
		}
	} else if ent := target.mannequin; ent != nil {
		var igniteBy entity.Combuster
		if e.Duration != nil {
			igniteBy = s.attackBodyEntity(owner)
		}
		s.mannequinsMu.Lock()
		m := ent.mannequin
		if m.Dead {
			s.mannequinsMu.Unlock()
			return
		}
		x, y, z = m.X, m.Y, m.Z
		body = enchantTarget{uuid: ent.uuid, width: 0.6, height: 1.8}
		category = protocol.SoundCategoryNeutral
		for _, in := range a.MobEffects(s.levelRng) {
			m.AddEffect(in)
		}
		if e.Duration != nil {
			m.IgniteForTicksBy(int(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level)))), entity.CombustSource{By: igniteBy})
		}
		if i := e.ApplyImpulse; i != nil {
			impulse := rules.LookImpulse(m.Yaw, m.Pitch, i.CoordinateScale, float64(rules.EnchLevelValueAt(i.Magnitude, a.Level)))
			m.Push(impulse[0], impulse[1], impulse[2])
		}
		var hurt mannequinHurt
		var landed bool
		hit := a.Retaliation(s.levelRng)
		if e.DamageEntity != nil {
			shove := &mannequinKnockback{x: cause.x, z: cause.z}
			hurt, landed = s.hurtMannequinLocked(ent, hit.Damage, hit.DamageType, cause.entityID+1, cause.entityID+1, nil, creativeClient(owner.player), shove)
		}
		syncMannequinEffects(ent)
		s.mannequinsMu.Unlock()
		if landed {
			s.tellMannequinHurt(hurt)
		}
	} else {
		s.mobsMu.Lock()
		m := target.mob
		ent := s.herdOf(tw).book[m]
		if ent == nil {
			s.mobsMu.Unlock()
			return
		}
		x, y, z, silent = m.X, m.Y, m.Z, m.Silent
		body = enchantTarget{uuid: ent.uuid, width: m.BbWidth(), height: m.BbHeight(), vx: m.VX, vy: m.VY, vz: m.VZ}
		category = mobSoundCategory(m.Type)
		var changes []effect.Change
		for _, in := range a.MobEffects(m.Rand) {
			m.EffectCause = effect.CauseAttack
			if ch, ok := m.AddEffect(in); ok {
				changes = append(changes, ch)
			}
		}
		if e.Duration != nil {
			m.IgniteForTicksBy(int(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level)))), entity.CombustSource{By: s.attackBodyEntityLocked(w, owner)})
		}
		if i := e.ApplyImpulse; i != nil {
			impulse := rules.LookImpulse(float32(m.Yaw), float32(m.Pitch), i.CoordinateScale, float64(rules.EnchLevelValueAt(i.Magnitude, a.Level)))
			m.Push(impulse[0], impulse[1], impulse[2])
		}
		hit := a.Retaliation(m.Rand)
		var landed, softened bool
		if e.DamageEntity != nil {
			if c := owner.player; c != nil {
				landed, softened = m.HurtByPlayerThorns(hit.Damage, hit.DamageType, c.player.EntityID, creativeClient(c), cause.x, cause.z)
			} else if owner.mob != nil {
				landed = m.HurtByMobAs(hit.Damage, hit.DamageType, owner.mob)
			} else {
				landed, softened = m.HurtByCommand(hit.Damage, hit.DamageType, cause.entityID, uuid.Nil, false, false, cause.x, cause.z)
			}
		}
		health := ent.syncHealth()
		tell := mobEffectTellOf(ent, changes)
		id, kind := m.ID, m.Type
		sound, volume, pitch := mobSound(m, true), m.SoundVolume(), m.VoicePitch()
		vx, vy, vz := m.VX, m.VY, m.VZ
		s.mobsMu.Unlock()
		s.tellSplashMob(tw, tell)
		if e.ApplyImpulse != nil {
			s.broadcastWorld(tw, protocol.IDPlaySetEntityMotion, entityVelocityPayload(id, vx, vy, vz))
		}
		if landed {
			s.broadcastWorld(tw, protocol.IDPlayEntityMetadata, health)
			if !softened {
				// The hit's level does not track a mob in another dimension, so no damage event.
				// vanilla: ServerLevel.broadcastDamageEvent
				if tw == w {
					s.broadcastWorld(w, protocol.IDPlayDamageEvent, damageEventPayload(id, hit.DamageType, causeEntityID(cause), causeEntityID(cause)))
				}
				s.broadcastVoice(tw, kind, silent, x, y, z, sound, volume, pitch)
				if hit.DamageType == registry.DamageTypeThorns {
					s.broadcastSoundID(tw, thornsHitSound, category, x, y, z, 1, 1)
				}
			}
		}
	}
	body.x, body.y, body.z = x, y, z
	s.applyPackEffects(w, e, a.Level, body)
	if sound := e.PlaySound; sound != nil && !silent {
		event := sound.Sound
		if len(sound.Sounds) > 0 {
			event = sound.Sounds[min(max(int(a.Level)-1, 0), len(sound.Sounds)-1)]
		}
		s.broadcastSoundHolder(w, event, category, x, y, z, sound.Volume, sound.PitchWith(s.levelRng))
	}
	if e.SummonEntity == "minecraft:lightning_bolt" {
		bolt := s.spawnBolt(w, x, y, z, false, plugin.LightningTrident)
		if bolt != nil && owner.player != nil {
			s.boltMu.Lock()
			bolt.cause = owner.player.player.UUID
			s.boltMu.Unlock()
		}
	}
	if e.ChangeItemDamage != nil {
		s.wearPostAttack(w, owner, a.Piece, int32(rules.EnchLevelValueAt(e.ChangeItemDamage, a.Level)))
	}
}

func (s *Server) wearPostAttack(w *world.World, owner attackBody, piece int, amount int32) {
	if c := owner.player; c != nil {
		if c.player.GameMode() == protocol.GameModeCreative {
			return
		}
		if piece < 0 {
			c.wearHeld(protocol.HandMain, amount)
			return
		}
		var slot int
		c.player.Inventory(func(inv *player.Inventory) { slot = inv.EquipmentWindowSlots()[piece] })
		if asked := c.askWear(amount, nil, slot); asked != nil {
			amount = asked[0]
		}
		var worn slotWear
		c.player.Inventory(func(inv *player.Inventory) {
			worn = c.wearUnder(inv, slot, rules.ProcessDurabilityChange(inv.Slots[slot], amount, c.ownDice()))
		})
		c.tellWear(worn)
	} else if ent := owner.mannequin; ent != nil {
		s.mannequinsMu.Lock()
		m := ent.mannequin
		if piece < 0 {
			piece = len(m.Worn)
		}
		status := m.WearEnchPiece(piece, amount, s.levelRng)
		s.mannequinsMu.Unlock()
		if status != 0 {
			s.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, status))
		}
	} else {
		s.mobsMu.Lock()
		if piece < 0 {
			piece = len(owner.mob.Worn)
		}
		status := owner.mob.WearEnchPiece(piece, amount)
		id := owner.mob.ID
		s.mobsMu.Unlock()
		if status != 0 {
			s.broadcastWorld(owner.at(w), protocol.IDPlayEntityEvent, entityEventPayload(id, status))
		}
	}
}

// bindBlowLoot gives ctx the bodies of a blow for a data-pack effect's loot
// conditions, with the victim's position as the origin. mobsHeld says the
// caller holds mobsMu while the conditions run; a mob body is then read
// without taking it. A mannequin body takes mannequinsMu, so the caller holds
// none of its own while the conditions run.
// vanilla: Enchantment.damageContext
func (s *Server) bindBlowLoot(w *world.World, ctx *rules.EnchContext, attacker, victim attackBody, mobsHeld bool) {
	body := func(b attackBody) func() *rules.LootEntity {
		switch {
		case b.player != nil:
			return victimLoot(b.player)
		case b.mannequin != nil:
			return func() *rules.LootEntity {
				s.mannequinsMu.Lock()
				snap := s.mannequinSnapshot(b.mannequin, nil)
				s.mannequinsMu.Unlock()
				if snap == nil {
					return nil
				}
				return snapshotLoot(snap)()
			}
		case b.mob != nil:
			return func() *rules.LootEntity {
				if !mobsHeld {
					s.mobsMu.Lock()
					defer s.mobsMu.Unlock()
				}
				if ent := s.herdOf(b.at(w)).book[b.mob]; ent != nil {
					return s.mobLootLocked(ent)
				}
				return nil
			}
		}
		return nil
	}
	ctx.Body, ctx.BodyOf = nil, body(victim)
	ctx.AttackerOf = body(attacker)
	ctx.DirectAttackerOf = nil
	if ctx.Direct {
		ctx.DirectAttackerOf = ctx.AttackerOf
	}
	ctx.LootWorld = func(loot *rules.LootContext) { s.fillBlowWorld(w, loot, [3]float64{}) }
}

// fillBlowWorld sets the level and origin of a blow's loot context: the
// victim's position, or at when the victim's snapshot has none.
func (s *Server) fillBlowWorld(w *world.World, loot *rules.LootContext, at [3]float64) {
	if p := loot.This.Place; p != nil {
		at = [3]float64{p.X, p.Y, p.Z}
	}
	s.fillLootOrigin(w, loot, at)
}

// fillLootOrigin points loot's origin and its level, block and biome reads at at.
func (s *Server) fillLootOrigin(w *world.World, loot *rules.LootContext, at [3]float64) {
	x, y, z := rules.BlockOf(at[0]), rules.BlockOf(at[1]), rules.BlockOf(at[2])
	loot.BlockAt = func(dx, dy, dz int32) uint16 { return w.BlockAt(x+dx, y+dy, z+dz) }
	loot.BiomeAt = func(dx, dy, dz int32) string { return w.BiomeAt(x+dx, y+dy, z+dz) }
	loot.Dimension = w.DimensionName()
	loot.Origin, loot.HasOrigin = at, true
	loot.Level = s.lootLevelAt(w, at[0], at[1], at[2])
}
