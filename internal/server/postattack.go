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
)

// attackBody identifies a living participant without holding its body lock.
// An empty body represents a projectile with no living owner.
type attackBody struct {
	player    *client
	mob       *entity.Mob
	mannequin *mannequinEntity
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
	return &damageCause{entityID: m.ID, x: m.X, y: m.Y, z: m.Z, weapon: m.MainHand, killer: s.mobKiller(s.herdOf(w).book[m]), shove: true}
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
		s.mannequinsMu.Lock()
		actions = rules.PostAttackEffects(weapon, victim.mannequin.mannequin.EnchEquipment(), ctx, s.levelRng)
		s.mannequinsMu.Unlock()
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
			c.igniteForTicks(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level))))
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
			m.IgniteForTicks(int(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level)))))
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
		packets, watchers := mannequinEffectPackets(ent), trackedWatchers(ent.tracked)
		s.mannequinsMu.Unlock()
		if landed {
			s.tellMannequinHurt(hurt)
		}
		s.tellWatchers(w, watchers, packets)
	} else {
		s.mobsMu.Lock()
		m := target.mob
		ent := s.herdOf(w).book[m]
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
			m.IgniteForTicks(int(rules.IgniteTicks(float64(rules.EnchLevelValueAt(e.Duration, a.Level)))))
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
				landed, softened = m.HurtByCommand(hit.Damage, hit.DamageType, cause.entityID, false, false, cause.x, cause.z)
			}
		}
		health := ent.syncHealth()
		tell := mobEffectTellOf(ent, changes)
		id, kind := m.ID, m.Type
		sound, volume, pitch := mobSound(m, true), m.SoundVolume(), m.VoicePitch()
		vx, vy, vz := m.VX, m.VY, m.VZ
		s.mobsMu.Unlock()
		s.tellSplashMob(w, tell)
		if e.ApplyImpulse != nil {
			s.broadcastWorld(w, protocol.IDPlaySetEntityMotion, entityVelocityPayload(id, vx, vy, vz))
		}
		if landed {
			s.broadcastWorld(w, protocol.IDPlayEntityMetadata, health)
			if !softened {
				s.broadcastWorld(w, protocol.IDPlayDamageEvent, damageEventPayload(id, hit.DamageType, causeEntityID(cause), causeEntityID(cause)))
				s.broadcastVoice(w, kind, silent, x, y, z, sound, volume, pitch)
				if hit.DamageType == registry.DamageTypeThorns {
					s.broadcastSoundID(w, thornsHitSound, category, x, y, z, 1, 1)
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
			s.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(id, status))
		}
	}
}
