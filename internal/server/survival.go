package server

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/advancement"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Survival: hurt, death, respawn and their packets. Numbers and decisions
// are package rules'; this runs them and sends the results.

// directNameOf is the display name of an entity of type typeID that carried a
// blow, for a death message; nil when killer is set, as the killer is named.
// vanilla: DamageSource.getLocalizedDeathMessage
func (s *Server) directNameOf(killer *rules.Killer, typeID int32, id uuid.UUID, custom customName) *protocol.Text {
	if killer != nil {
		return nil
	}
	name := entityDisplayName(s.scoreboard, registry.EntityType(typeID).Name(), id, customNameText(custom.tag()))
	return &name
}

// damageCause is a hurt's source, nil for none. x, y, z set the flinch
// direction; killer goes in the death message. directID is the wire ID
// (entity ID + 1) of what carried the blow (e.g. an arrow), 0 when the
// cause itself did. disableSeconds is the weapon's
// disable_blocking_for_seconds of a direct melee blow, which puts a raised
// blocks_attacks item away for that long times its disable_cooldown_scale.
type damageCause struct {
	entityID int32
	// exploder is the body a blast came from, already removed when its hurt
	// lands; nil for any other cause.
	exploder *removedBody
	directID int32
	// directUUID is the direct entity's identity, which plugins are told; nil
	// to resolve directID instead.
	directUUID uuid.UUID
	// directType is the entity type name of a projectile direct entity, which
	// is neither a player nor a mob; empty otherwise.
	directType string
	// directBurning is the projectile's fire flag when it hit.
	directBurning bool
	// directIsSource makes the projectile itself the source when it has no
	// shooter: advancement predicates and the Damage Event's cause ID use it.
	// vanilla: AbstractArrow.onHitEntity, ThrownTrident.onHitEntity
	directIsSource bool
	x, y, z        float64
	// weapon belongs to the direct entity, independently of death attribution.
	// vanilla: DamageSource.getWeaponItem
	weapon protocol.ItemStack
	killer *rules.Killer
	// directName names the direct entity (an arrow, a TNT) when no living
	// entity caused the blow; the death message names it.
	// vanilla: DamageSource.getLocalizedDeathMessage
	directName     *protocol.Text
	disableSeconds float32
	// piercing marks a piercing arrow's blow, which no shield blocks.
	// vanilla: LivingEntity.applyItemBlocking
	piercing bool
	// flying marks a projectile's blow, whose knockback follows its flight
	// (flightVX, flightVZ) rather than its position. The flight is in blocks
	// per tick at the hit; flightVY only feeds a directType's movement.
	// vanilla: Projectile.calculateHorizontalHurtKnockbackDirection
	flying                       bool
	flightVX, flightVY, flightVZ float64
	// posOnly marks a position cause with no entity (/damage at); eventPos one
	// whose x, y, z the Damage Event also carries.
	// vanilla: DamageSource.sourcePositionRaw
	posOnly, eventPos bool
	// noEvent skips EntityDamageEvent: a plugin's setHealth(0) kills outright.
	// Bukkit: CraftLivingEntity.setHealth
	noEvent bool
	// player marks a player cause, creative one in creative mode, for a mob's
	// retaliation.
	player, creative bool
	// blocked is set by hurt when a raised shield turned the blow aside.
	blocked bool
	// freshBlock is set by hurt when a shield fully blocked a hit outside the
	// hurt window, which still fires the attacker's player_hurt_entity.
	freshBlock bool
	// shieldAsked is set once hurt consulted the shield, so a caller whose hurt
	// returned earlier can ask itself.
	shieldAsked bool
	// shove marks a blow whose direct entity is a living attacker at (x, z)
	// that a partial block shoves the holder toward.
	// vanilla: LivingEntity.blockedByItem
	shove bool
}

// clientLoadTicks is how long after a join or respawn the server waits for
// Player Loaded before treating the client as loaded anyway. vanilla:
// ServerGamePacketListenerImpl.clientLoadedTimeoutTimer
const clientLoadTicks = 60

// clientLoaded reports whether the client has loaded the world (vanilla:
// ServerGamePacketListenerImpl.hasClientLoaded); until then world-acting
// packets are dropped and the player cannot be hurt.
func (c *client) clientLoaded() bool {
	return !c.awaitingRespawn.Load() && c.loadTicks.Load() <= 0
}

// hurt applies damage after armour and enchantment protection and tells
// everyone. It reports whether damage landed, and whether only the excess
// over the last hurt landed through the invulnerability window (softened:
// no damage event, flinch or base knockback). A shield-blocked hurt
// (shield.go) lands nothing but still opens the window, with the block
// sound and no knockback: the shield blocks the whole amount. A
// landed hit gives rules.HurtInvulnerableTicks (10) of invulnerability,
// during which a hit lands only what it exceeds the opener by and a weaker
// one is dropped. A loading client ignores hits, as do creative and
// spectator players unless the type bypasses invulnerability (out_of_world,
// /kill's generic_kill), and a hurt of zero is refused
// (vanilla: Player.hurtServer).
func (c *client) hurt(amount float64, damageType registry.DamageType, cause *damageCause) (landed, softened bool) {
	return c.hurtFalling(amount, damageType, cause, c.player.Vitals().FallDistance)
}

// hurtFalling is hurt for a body that fell fall blocks, recorded with the
// hit in the combat tracker.
func (c *client) hurtFalling(amount float64, damageType registry.DamageType, cause *damageCause, fall float64) (landed, softened bool) {
	original := amount
	// Refuse before taking anything, so an unlandable hit wears no armour; the
	// lock below decides and takes the hit.
	if !c.clientLoaded() {
		return false, false
	}
	throughMode := rules.InvulnerabilityBypassed(damageType)
	if v := c.player.Vitals(); v.Dead || ((rules.ModeInvulnerable(v.GameMode) || c.carriedInvulnerable()) && !throughMode) {
		return false, false
	}
	// Damage game rules (fall, drowning, fire) apply to players only.
	// vanilla: ServerPlayer.isInvulnerableTo
	if gate := rules.DamageRuleGate(damageType); gate != world.RuleUnknown && !c.theWorld().BoolRule(gate) {
		return false, false
	}
	effects := c.player.Effects()
	if rules.FireImmune(effects, damageType) {
		return false, false
	}
	// An enchantment's damage_immunity (Frost Walker's hot floor) is an
	// immunity of its own beside fire resistance.
	// vanilla: EnchantmentHelper.isImmuneToDamage, LivingEntity.isInvulnerableTo
	if rules.ImmuneToDamage(c.enchPieces(), c.hurtEnchContext(damageType, damageIsDirect(cause))) {
		return false, false
	}
	// With pvp off, or a teammate's blow on a team without friendly fire, a
	// hurt a player caused (a blow, their arrow, their TNT) is refused before
	// the shield or window. vanilla: ServerPlayer.hurtServer
	if attacker := c.playerBlow(cause); attacker != nil &&
		(!c.theWorld().BoolRule(world.RulePvp) || !c.srv.scoreboard.CanHarm(c.player.Name, attacker.player.Name)) {
		return false, false
	}
	// vanilla: Player.hurtServer
	c.removeEntitiesOnShoulder()
	// A hurt of nothing is refused before the shield, window or knockback.
	if amount == 0 {
		return false, false
	}
	// A raised shield blocks a frontal hurt first, its wear and any disable
	// being the shield's; a fully blocked blow has no damage event or flinch.
	// vanilla: LivingEntity.hurtServer calls applyItemBlocking first
	blocked, blockSound := c.shieldBlocks(damageType, cause, amount)
	amount -= blocked
	if cause != nil {
		cause.shieldAsked = true
		cause.blocked = blocked > 0
	}
	c.awardDamageStat(registry.CustomStatDamageBlockedByShield, blocked)
	// The sleeper wakes and the helmet wears before the window check, which
	// compares the softened amount. vanilla: LivingEntity.hurtServer
	c.stopSleeping(true, true)
	if damageType.InTag(registry.DamageTypeTagIDDamagesHelmet) && c.hurtHelmet(amount) {
		amount *= 0.75
	}
	// Land the hit and move the window under the lock, so the excess is taken
	// from the last hurt actually to beat. A hit inside the window leaves it
	// running; only a fresh hit opens one. A blocked fresh hit lands nothing
	// but still quietly opens the window: blows within it are turned aside
	// silently, as a window-excess hit is held back.
	// vanilla: LivingEntity.hurtServer
	full := amount
	if r := c.srv.plugins; r != nil && r.Has(pluginruntime.EntityDamage) && (cause == nil || !cause.noEvent) {
		var ok bool
		if full, ok = c.pluginDamage(full, damageType, cause, effects); !ok {
			return false, false
		}
	}
	var fresh bool
	responsible, setResponsible := c.srv.playerResponsible(c.theWorld(), causeEntityID(cause))
	c.player.UpdateVitals(func(v *player.Vitals) {
		if v.Dead || (rules.ModeInvulnerable(v.GameMode) && !throughMode) {
			return
		}
		if v.InvulnerableTicks > 0 {
			if full <= v.LastHurt {
				return
			}
			amount = full - v.LastHurt
			softened = true
		} else {
			amount = full
			v.InvulnerableTicks = rules.HurtInvulnerableTicks
			v.HurtTime = rules.HurtInvulnerableTicks
			fresh = true
		}
		v.LastHurt = full
		if setResponsible {
			v.LastHurtByPlayer, v.LastHurtByPlayerAge = responsible, v.AgeTicks
		}
		// A partial block still lands what it left. vanilla: LivingEntity.hurtServer
		landed = blocked == 0 || full > 0
		if landed {
			v.LastHurtNoWolfRetaliation = damageType.InTag(registry.DamageTypeTagIDNoWolfRetaliation)
		}
		// vanilla: LivingEntity.resolveMobResponsibleForDamage
		if id := causeEntityID(cause); id != 0 && !damageType.InTag(registry.DamageTypeTagIDNoAnger) {
			v.LastHurtBy, v.LastHurtByAge = id-1, v.AgeTicks
		}
	})
	// A fully blocked hit takes neither the default knockback nor a shove; only
	// a hoglin's, zoglin's or ravager's own blockedByItem answers (mobs.go).
	// vanilla: LivingEntity.hurtServer (fullyBlocked), LivingEntity.blockedByItem
	if !landed {
		if fresh && blocked > 0 {
			if cause != nil {
				cause.freshBlock = true
			}
			c.hurtAdvancements(damageType, cause, original, 0, true)
			c.playShieldBlock(blockSound)
			c.playHurtSound(damageType.Info().Effects.PlayerHurtSound())
		}
		return false, false
	}
	amount = c.armourAbsorb(amount, damageType, damageIsDirect(cause), effects, causeWeapon(cause), false)

	var died bool
	var tick int
	var absorbed float64
	c.player.UpdateVitals(func(v *player.Vitals) {
		if v.Dead || (rules.ModeInvulnerable(v.GameMode) && !throughMode) {
			return
		}
		tick = v.AgeTicks
		absorbed = amount
		amount = rules.AbsorbDamage(v, amount)
		absorbed -= amount
		if amount != 0 {
			rules.Exhaust(v, rules.DamageExhaustion(damageType))
		}
		v.Health = math.Max(0, v.Health-amount)
		if v.Health == 0 {
			v.Dead = true
			died = true
		}
	})
	c.awardDamageStat(registry.CustomStatDamageAbsorbed, absorbed)
	if amount != 0 {
		c.awardDamageStat(registry.CustomStatDamageTaken, amount)
	}
	if amount > 0 {
		entry := rules.CombatEntry{DamageType: damageType, Damage: amount, FallDistance: fall}
		if cause != nil {
			entry.Attacker = cause.killer
			if entry.Attacker == nil && cause.directName != nil {
				entry.Attacker, entry.DirectOnly = &rules.Killer{Name: *cause.directName}, true
			}
			entry.AttackerID = causeEntityID(cause)
		}
		c.recordCombat(tick, entry)
	}
	// Before dying, an item in hand may save the body. vanilla:
	// LivingEntity.hurtServer runs checkTotemDeathProtection before it lets the
	// body die
	dying := died
	if died && c.checkTotemDeathProtection(damageType) {
		died = false
	}
	c.sendHealth(c.player.Vitals())
	c.infestedHurt()
	// A softened hit is told by health alone; a whole hit's damage event and
	// flinch stay back.
	if !softened {
		direct := causeEntityID(cause)
		wireCause := direct
		if cause != nil && cause.directID != 0 {
			direct = cause.directID
			if cause.directIsSource {
				wireCause = direct
			}
		}
		// A blocked blow plays the item's sound in place of the damage event.
		// vanilla: LivingEntity.hurtServer
		if blocked > 0 {
			c.playShieldBlock(blockSound)
		} else {
			c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayDamageEvent,
				damageEventPayloadAt(c.player.EntityID, damageType, wireCause, direct, damageEventPos(cause)))
		}
		// Only the victim is told, and only of an unblocked hit that knocks back.
		// vanilla: ServerPlayer.indicateDamage via LivingEntity.dealDefaultKnockback
		if blocked == 0 && !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
			c.send(protocol.IDPlayHurtAnimation,
				hurtAnimationPayload(c.player.EntityID, hurtYaw(c, cause)))
		}
		// A body a totem saves makes neither sound; a blocked blow still
		// makes the hurt one. vanilla: LivingEntity.hurtServer (tookFullDamage)
		switch {
		case died:
			c.playHurtSound(registry.SoundEntityPlayerDeath)
		case !dying:
			c.playHurtSound(damageType.Info().Effects.PlayerHurtSound())
		}
	}
	// Fired right after the health is set, so a killing blow vibrates before
	// its death. A hit that absorption or Resistance took to nothing stays
	// silent. vanilla: Player.actuallyHurt
	pos := c.player.Position()
	if amount != 0 {
		c.srv.gameEvent(c.theWorld(), rules.GameEventEntityDamage, pos.X, pos.Y, pos.Z, c.gameEventContext(0))
	}
	if died {
		// A body that truly dies banks no fall for a landing that never comes; one
		// a totem saves keeps it and still takes landing damage.
		// vanilla: LivingEntity.checkTotemDeathProtection
		// leaves fallDistance alone.
		c.player.UpdateVitals(func(v *player.Vitals) { v.FallDistance = 0 })
		// The attackers are resolved with no combat lock held: the lookups
		// take the server lock.
		c.combatMu.Lock()
		ids := c.combat.Attackers(tick)
		c.combatMu.Unlock()
		live := make(map[int32]bool, len(ids))
		for _, id := range ids {
			live[id] = c.srv.attackerAlive(c, id-1, c.theWorld())
		}
		alive := func(id int32) bool { return live[id] }
		credit, creditKiller, remembered := c.srv.rememberedPlayer(c)
		c.combatMu.Lock()
		message := c.combat.DeathMessageAlive(c.DisplayName(), tick, creditKiller, remembered, alive)
		var creditID int32
		if e := c.combat.KillCredit(tick, creditKiller, remembered, alive); e != nil {
			creditID = e.AttackerID
		}
		c.combat.Clear()
		c.combatMu.Unlock()
		c.die(message, damageType, cause)
		// A credited player gets the kill; a credited mob gets the killed_by
		// stat. Either way the victim's entity_killed_player fires.
		// vanilla: ServerPlayer.die, LivingEntity.getKillCredit,
		// Entity.awardKillScore
		credited := false
		blow := c.srv.blowOf(c.theWorld(), damageType, cause, 0, 0, false)
		var killer advancement.Entity
		if k := credit; k != nil {
			k.scoreKill(c.scoreHolder(), true)
			victim := c.srv.advancementEntity("minecraft:player", c.theWorld())
			victim.Loot, victim.Blow = c.LootEntity, &blow
			k.killedEntityAdvancements(victim)
			killer = advancement.Entity{Type: "minecraft:player", Loot: k.LootEntity}
			c.awardStat(player.Stat{Type: registry.StatTypeKilledBy, Key: int32(registry.EntityTypePlayer)}, 1)
			credited = true
		} else if mob := c.srv.mobByEntityID(c.theWorld(), creditID-1); creditID != 0 && mob != nil {
			// vanilla: ServerPlayer.die, LivingEntity.createWitherRose
			if mob.mob.IsWither() {
				c.srv.witherRoseLeft(witherRose{w: c.theWorld(), x: pos.X, y: pos.Y, z: pos.Z})
			}
			c.awardStat(player.Stat{Type: registry.StatTypeKilledBy, Key: int32(mob.mob.Type)}, 1)
			killer = advancement.Entity{Type: mob.mob.Type.Name(), Loot: c.srv.mobLoot(mob)}
			credited = true
		}
		if credited {
			killer.Blow = &blow
			c.killedByEntityAdvancements(killer)
		}
	}
	c.hurtAdvancements(damageType, cause, original, amount, blocked > 0)
	return true, softened
}

// playerResponsible is the player a blow by the entity (ID plus one) makes
// responsible for the hurt: that player, or a tame wolf's owner (uuid.Nil
// when it has none). set is false for any other source, which leaves the
// memory alone.
// vanilla: LivingEntity.resolvePlayerResponsibleForDamage
func (s *Server) playerResponsible(w *world.World, causeID int32) (id uuid.UUID, set bool) {
	if causeID == 0 {
		return uuid.Nil, false
	}
	if k := s.entityClient(causeID - 1); k != nil {
		return k.player.UUID, true
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(w, causeID-1); ent != nil && ent.mob.Type == entity.TypeWolf && ent.mob.Tamable.Tame {
		return ent.mob.Tamable.Owner, true
	}
	return uuid.Nil, false
}

// rememberedPlayer is the online player, in any dimension, that victim's
// remembered kill credit names, nil for none, with the name the death
// message gives. remembered reports that the memory is set and unexpired,
// even when its player is offline or is victim; a remembered player then
// leaves no credit to a mob. Unlike vanilla, victim never credits their own
// death.
// vanilla: LivingEntity.getKillCredit
func (s *Server) rememberedPlayer(victim *client) (k *client, killer *rules.Killer, remembered bool) {
	v := victim.player.Vitals()
	if v.LastHurtByPlayer == uuid.Nil || v.AgeTicks-v.LastHurtByPlayerAge > 100 {
		return nil, nil, false
	}
	k = s.clientByUUID(v.LastHurtByPlayer)
	if k == nil || k == victim {
		return nil, nil, true
	}
	return k, &rules.Killer{Name: k.DisplayName(), Weapon: k.heldItem(protocol.HandMain)}, true
}

// attackerAlive reports whether the entity is a living player other than
// victim, or a mob still in the world.
// vanilla: LivingEntity.getKillCredit
func (s *Server) attackerAlive(victim *client, entityID int32, w *world.World) bool {
	if k := s.entityClient(entityID); k != nil {
		return k != victim && !k.player.Vitals().Dead
	}
	return s.mobByEntityID(w, entityID) != nil
}

// awardDamageStat adds damage in tenths of a health point to a custom stat;
// nothing for a non-positive amount or vanilla's "infinite" (the void's
// kill). vanilla: LivingEntity.actuallyHurt, Player.actuallyHurt
func (c *client) awardDamageStat(key registry.CustomStat, damage float64) {
	if damage > 0 && damage < math.MaxFloat32/1e3 {
		c.awardStat(player.Custom(key), rules.DamageStat(damage))
	}
}

// recordCombat adds a hit to the combat tracker with the body's fall
// location: the block last climbed, else the water it is in.
// vanilla: CombatTracker.recordDamage, FallLocation.getCurrentFallLocation
func (c *client) recordCombat(tick int, entry rules.CombatEntry) {
	c.combatMu.Lock()
	climbed, cell := c.hasLastClimbable, c.lastClimbable
	c.combatMu.Unlock()
	switch {
	case climbed:
		entry.Location = rules.BlockFallLocation(c.theWorld().BlockAt(cell[0], cell[1], cell[2]))
	case c.waterAt(c.player.Position()) != rules.Dry:
		entry.Location = rules.FallLocationWater
	}
	c.combatMu.Lock()
	c.combat.Record(tick, entry)
	c.combatMu.Unlock()
}

// playerBlow is the player a hurt's cause entity is (their blow, arrow or
// TNT), nil for a mob's blow or arrow, so pvp leaves those alone.
// vanilla: ServerPlayer.hurtServer
func (c *client) playerBlow(cause *damageCause) *client {
	if cause == nil || cause.posOnly {
		return nil
	}
	return c.srv.entityClient(cause.entityID)
}

// checkTotemDeathProtection runs a death-saving item in hand: one is spent,
// health stands at half a heart with the item's death effects applied, and
// the totem animation (entity event 35) reaches the player and watchers. It
// reports whether the body was saved; bypasses_invulnerability (/kill, the
// void) is beyond any totem.
// vanilla: LivingEntity.checkTotemDeathProtection
func (c *client) checkTotemDeathProtection(damageType registry.DamageType) bool {
	// Picking the hand and spending its item are one critical section: a held
	// slot change from the reader goroutine between them would spend a
	// different stack than the one checked.
	if !c.totemAllowed(damageType) {
		return false
	}
	var payload []byte
	var death []registry.ConsumeEffect
	var saved bool
	var totem protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		hand, effects := rules.TotemHand(damageType,
			inv.Slots[rules.SlotOfHand(inv, protocol.HandMain)], inv.Slots[player.OffHandSlot])
		if hand < 0 {
			return
		}
		saved = true
		death = effects
		slot := rules.SlotOfHand(inv, hand)
		stack := inv.Slots[slot]
		totem = stack
		stack.Count--
		if stack.Count <= 0 {
			stack = protocol.ItemStack{}
		}
		inv.Set(slot, stack)
		payload = inv.WindowSlot(playerWindowID, int32(slot), stack)
	})
	if !saved {
		return false
	}
	c.awardItemStat(registry.StatTypeUsed, totem.ItemID, 1)
	c.usedTotemAdvancements(totem)
	// The spent totem's copy posts it. vanilla: LivingEntity.checkTotemDeathProtection
	c.useVibration(totem, rules.GameEventItemInteractFinish)
	c.send(protocol.IDPlayContainerSlot, payload)
	c.broadcastEquipment()
	var changes []effect.Change
	c.player.UpdateBody(func(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
		changes = rules.TotemSave(v, effects, a, death, c.ownDice())
		rules.UpdateEffectAttributes(a, effects.All())
	})
	c.tellEffects(changes)
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, protocol.EntityStatusTotem))
	return true
}

// causeEntityID is the Damage Event's cause ID: entity ID + 1, 0 for none.
func causeEntityID(cause *damageCause) int32 {
	if cause == nil || cause.posOnly {
		return 0
	}
	return cause.entityID + 1
}

// damageIsDirect reports whether the cause also carried the blow, for
// enchantment conditions (vanilla: DamageSource.isDirect); a nil cause is
// direct, as vanilla's null == null.
func damageIsDirect(cause *damageCause) bool {
	if cause == nil || cause.posOnly {
		return true
	}
	return cause.directID == 0 || cause.directID == cause.entityID+1
}

// meleeDamageType is the damage type a player's blow with held deals.
// vanilla: ItemStack.getDamageSource
func meleeDamageType(held protocol.ItemStack) registry.DamageType {
	if d := registry.StackDamageType(held); d != 0 {
		return d
	}
	return rules.DamageTypePlayerAttack
}

// hurtEnchContext is the context of a blow on this player, whose worn
// enchantments run.
func (c *client) hurtEnchContext(damageType registry.DamageType, direct bool) *rules.EnchContext {
	return &rules.EnchContext{DamageType: damageType, Direct: direct, Victim: protocol.EntityTypePlayer, BodyOf: victimLoot(c)}
}

// attackEnchContext is the enchantment context of a player's melee blow on
// a victim of victimType.
// victim is the hurt body's loot snapshot for a data-pack effect's loot
// conditions; nil gives one that knows only the type.
func (c *client) attackEnchContext(damageType registry.DamageType, victimType int32, victim func() *rules.LootEntity) rules.EnchContext {
	ctx := rules.EnchContext{
		DamageType:     damageType,
		Direct:         true,
		Victim:         victimType,
		Attacker:       protocol.EntityTypePlayer,
		DirectAttacker: protocol.EntityTypePlayer,
	}
	c.bindLootBodies(&ctx, victim)
	return ctx
}

// causeWeapon is the item the blow's source weapon is, empty for none.
// vanilla: DamageSource.getWeaponItem
func causeWeapon(cause *damageCause) protocol.ItemStack {
	if cause == nil {
		return protocol.ItemStack{}
	}
	return cause.weapon
}

// armorSlotNames are the armour slots' equipment names in inventory order,
// as enchantment slot lists name them.
var armorSlotNames = [player.ArmorSlots]string{"head", "chest", "legs", "feet"}

// armourAbsorb returns the damage left after armour, then enchantment
// protection (vanilla: LivingEntity.getDamageAfterArmorAbsorb, then
// getDamageAfterMagicAbsorb). Worn pieces take wear unless the type bypasses
// armour; enchantment protection (e.g. feather falling) applies either way.
// dry takes no wear and awards no stat, to preview the amount.
func (c *client) armourAbsorb(damage float64, damageType registry.DamageType, direct bool, effects []effect.Instance, weapon protocol.ItemStack, dry bool) float64 {
	c.refreshAttributes(effects)
	var points, toughness float64
	c.player.Attributes(func(a *player.Attributes) {
		points, toughness = rules.ArmourValue(a)
	})
	var enchPieces []rules.EnchPiece
	var worn []slotWear
	bypassed := rules.ArmourBypassed(damageType)
	perPiece := rules.ArmourWearPerPiece(damage)
	var asked []int32
	if !dry && !bypassed {
		asked = c.askWear(perPiece, func(s protocol.ItemStack) bool { return registry.StackSparedHurtWear(s, damageType) },
			player.ArmorSlot, player.ArmorSlot+1, player.ArmorSlot+2, player.ArmorSlot+3)
	}
	c.player.Inventory(func(inv *player.Inventory) {
		for i := range player.ArmorSlots {
			slot := player.ArmorSlot + i
			wornStack := inv.Slots[slot]
			if wornStack.Empty() {
				continue
			}
			enchPieces = append(enchPieces, rules.EnchPiece{Stack: wornStack, Slot: armorSlotNames[i]})
			if dry || bypassed || registry.StackSparedHurtWear(wornStack, damageType) {
				// vanilla never calls hurtArmor for a bypassing type, and doHurtEquipment
				// skips a spared piece.
				continue
			}
			// Every worn piece wears (even without armour points, e.g. a carved
			// pumpkin), after its unbreaking roll.
			pieceWear := perPiece
			if asked != nil {
				pieceWear = asked[i]
			}
			wear := rules.ProcessDurabilityChange(wornStack, pieceWear, c.ownDice())
			worn = append(worn, c.wearUnder(inv, slot, wear))
		}
		// Hands count too. vanilla: EnchantmentHelper.runIterationOnEquipment
		enchPieces = append(enchPieces,
			rules.EnchPiece{Stack: inv.Slots[player.HotbarSlot+int(inv.Selected)], Slot: "mainhand"},
			rules.EnchPiece{Stack: inv.Slots[player.OffHandSlot], Slot: "offhand"})
	})
	for _, w := range worn {
		c.tellWear(w)
	}
	if points > 0 && !bypassed {
		damage = rules.DamageAfterArmourEffective(damage, points, toughness,
			rules.ArmourEffectiveness(weapon, c.hurtEnchContext(damageType, direct)))
	}
	resisted := damage
	damage = rules.Resist(effects, damageType, damage)
	if !dry {
		c.awardDamageStat(registry.CustomStatDamageResisted, resisted-damage)
	}
	if damage <= 0 {
		return 0
	}
	if rules.ProtectionBypassed(damageType) {
		return damage
	}
	// Protection is capped at 20 points.
	// vanilla: CombatRules.getDamageAfterMagicAbsorb
	ctx := c.hurtEnchContext(damageType, direct)
	if protection := rules.DamageProtection(enchPieces, ctx, c.ownDice()); protection > 0 {
		damage = rules.DamageAfterEnchantmentProtection(damage, protection)
	}
	return damage
}

// tickVitals runs a tick of survival state (rules.TickVitals), the meal and
// the bow draw, and sends changes. Hurts go through hurt in vanilla's order,
// so each window applies to the next. It runs on the main loop, so all
// sends use the mailbox.
func (c *client) tickVitals() {
	// vanilla: ServerGamePacketListenerImpl.tickClientLoadTimeout
	if c.loadTicks.Load() > 0 {
		c.loadTicks.Add(-1)
	}
	// A dead join's empty bar waited for its chunk; once the first fill went
	// out, the client stands on it, so the bar can open the death screen
	// without skipping the chunk wait. A body that respawned meanwhile sends
	// nothing here.
	if c.deadHealthPending.Load() && c.chunkFillSent.Load() {
		c.deadHealthPending.Store(false)
		if v := c.player.Vitals(); v.Dead {
			c.sendHealth(v)
		}
	}
	// vanilla: Player.tick counts takeXpDelay down every tick.
	if d := c.takeXpDelay.Load(); d > 0 {
		c.takeXpDelay.Store(d - 1)
	}
	pos := c.player.Position()
	before := c.player.Vitals()
	if seam := c.srv.vitalsSeam; seam != nil {
		seam(c.player.Name)
	}
	// vanilla: ServerPlayer.doTick
	if sh := c.player.Shoulders(); hasShoulder(sh.Left) || hasShoulder(sh.Right) {
		c.shoulderAmbient(sh)
		inWater, inSnow := c.perchState(pos)
		if before.FallDistance > 0.5 || inWater || inSnow || c.flying.Load() || c.player.Sleep().Lying {
			c.removeEntitiesOnShoulder()
		}
	}
	difficulty := c.theWorld().Difficulty()
	voidY := c.theWorld().VoidY()
	around := c.habitat(pos)
	c.bubbleSurfaceSplash(pos)
	if around.OnGround && !around.Sneaking {
		c.trampleTurtleEgg(world.TurtleEggStepOdds)
	}
	// The helmet (for a turtle helmet's water breathing), read before the
	// vitals lock.
	var headArmor protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		headArmor = inv.Slots[player.ArmorSlot]
	})

	var event rules.VitalsEvent
	var hurts []rules.Hurt
	var douse rules.Douse
	var changes []effect.Change
	passenger := c.isPassenger()
	var fellFrom *[3]float64
	var preTickFire, postTickFire int
	c.player.UpdateBody(func(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
		// A rider's fall is written off every tick, so none is left to land
		// on after the dismount; a fall that began fires fall_from_height at
		// the seat. vanilla: LivingEntity.rideTick, ServerPlayer.resetFallDistance
		if passenger {
			v.FallDistance = 0
			fellFrom = fallEnded(v, pos)
		}
		preTickFire = v.FireTicks
		event, hurts, douse, changes = rules.TickVitals(v, effects, a, difficulty, pos.Y < float64(voidY), around,
			c.srv.envRng, a.Value(registry.AttributeOxygenBonus), c.theWorld().BoolRule(world.RuleNaturalRegen))
		postTickFire = v.FireTicks
		if in, ok := rules.TurtleHelmetBreathing(headArmor, around.EyesInWater); ok {
			if change, ok := rules.AddEffect(v, effects, in); ok {
				changes = append(changes, change)
			}
		}
		if len(changes) > 0 {
			rules.UpdateEffectAttributes(a, effects.All())
		}
	})
	if fellFrom != nil {
		c.fellAdvancements(*fellFrom, pos)
	}
	c.pluginVitalsTick(before, pos, preTickFire, postTickFire)
	c.tellEffects(changes)
	if !before.Dead && around.EyesInWater && !around.EyesInBubbleColumn && c.mountDismountsUnderwater() {
		c.dismountMob(true)
	}
	c.stepOnRedstoneOre(pos, around)
	c.stepOnShrieker(pos, around)
	c.stepOnSculkSensor(pos, around)
	c.powderSnowFlakes(pos, around.PowderSnows)
	c.tickWardenSpawn()
	// The border hurts only a body not crushed in a wall this tick, after burn
	// and wall and before drowning. vanilla: LivingEntity.baseTick
	if !before.Dead && !slices.ContainsFunc(hurts, func(h rules.Hurt) bool { return h.DamageType == rules.DamageTypeInWall }) {
		if h, ok := c.borderHurt(pos.X, pos.Z); ok {
			at := slices.IndexFunc(hurts, func(h rules.Hurt) bool { return h.DamageType != rules.DamageTypeOnFire })
			if at < 0 {
				at = len(hurts)
			}
			hurts = slices.Insert(hurts, at, h)
		}
	}
	// vanilla: LivingEntity.aiStep runs pushEntities after the block effects.
	if !before.Dead {
		if h, ok := c.crammingHurt(); ok {
			hurts = append(hurts, h)
		}
	}
	// The cauldron that doused the burn drops a level, after the tick's fire is
	// counted.
	for _, cell := range douse.Cauldrons {
		w := c.theWorld()
		c.srv.cauldronDoused(w, cell.X, cell.Y, cell.Z)
	}

	// The powder snow a burning body stood in melts as it puts the fire out.
	for _, cell := range douse.PowderSnows {
		c.srv.meltPowderSnow(c.theWorld(), cell.X, cell.Y, cell.Z)
	}

	// The bite sound goes to all but the eater, whose client plays it; a
	// finished meal's bars are sent by the end-of-tick comparison.
	if use, hand, ok, bite := c.eatDue(); ok {
		hurts = append(hurts, c.finishEating(hand, use)...)
	} else if bite {
		pos := c.player.Position()
		c.srv.broadcastSoundPacketExcept(c.theWorld(), c,
			c.srv.eatBiteSoundAt(use, pos.X, pos.Y, pos.Z))

	}

	c.tickBowDraw()
	c.tickBrush()
	c.tickShield()
	c.tickSpearCharge()

	// Drowning's bubble event precedes the blow; a landed lava hurt plays the
	// fizz (vanilla: Entity.lavaHurt).
	w := c.theWorld()
	var landed bool
	for _, h := range hurts {
		if h.DamageType == rules.DamageTypeDrown {
			c.srv.broadcastWorld(w, protocol.IDPlayEntityEvent,
				entityEventPayload(c.player.EntityID, protocol.EntityEventDrown))
		}
		var cause *damageCause
		if h.DamageType == rules.DamageTypeIndirectMagic {
			// A drunk harm is indirectMagic(drinker, drinker).
			// vanilla: HealOrHarmMobEffect.applyInstantenousEffect
			cause = &damageCause{entityID: c.player.EntityID,
				killer: &rules.Killer{Name: c.DisplayName()}}
		}
		landedHit, softened := c.hurt(h.Amount, h.DamageType, cause)
		// Only a drunk harm is outside no_knockback; with no source position
		// its shove goes in a random direction. vanilla: LivingEntity.dealDefaultKnockback
		if landedHit && !softened && !h.DamageType.InTag(registry.DamageTypeTagIDNoKnockback) {
			c.knockbackFromPoint(w, pos.X, pos.Y, pos.Z, syncVelocityNow)
		}
		if landedHit {
			landed = true
			if h.DamageType == rules.DamageTypeLava {
				c.srv.broadcastSoundPacketExcept(w, nil,
					c.srv.burnSound(protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z))

			}
		}
	}
	// vanilla: Entity.playEntityOnFireExtinguishedSound
	if douse.Sound {
		c.srv.broadcastSoundPacketExcept(w, nil,
			c.srv.extinguishSound(protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z))

	}

	if event == rules.VitalsStarved {
		if landedHit, _ := c.hurt(1, rules.DamageTypeStarve, nil); landedHit {
			landed = true
		}
	}

	c.tellEnvironment(c.player.Vitals())
	c.tellBody()

	// A landed hurt already sent health; otherwise send the tick's heal or
	// exhaustion. A dead body sends none: its bars went with the death, and a
	// stale one queued here would cross the Respawn as a gone body's empty bar.
	if !landed {
		after := c.player.Vitals()
		if !after.Dead && c.lastHealth.differs(after) {
			c.sendHealth(after)
		}
	}
}

// broadcastDeathMessage shows the death in chat as the victim's team allows.
// vanilla: ServerPlayer.die, PlayerList.broadcastSystemToTeam,
// PlayerList.broadcastSystemToAllExceptTeam
func (c *client) broadcastDeathMessage(message protocol.Text) {
	board := c.srv.scoreboard
	visibility := scoreboard.VisibleAlways
	myTeam, onTeam := board.PlayerTeam(c.player.Name)
	if onTeam {
		if t, ok := board.Team(myTeam); ok {
			visibility = t.DeathMessage
		}
	}
	if visibility == scoreboard.VisibleNever {
		return
	}
	if visibility == scoreboard.VisibleAlways {
		c.srv.broadcastSystemMessage(message)
		return
	}
	payload := systemChatPayload(message, false)
	c.srv.mu.RLock()
	recipients := make([]*client, 0, len(c.srv.clients))
	for _, o := range c.srv.clients {
		team, in := board.PlayerTeam(o.player.Name)
		sameTeam := in && team == myTeam
		if visibility == scoreboard.HideForOtherTeams && (!sameTeam || o == c) ||
			visibility == scoreboard.HideForOwnTeam && sameTeam {
			continue
		}
		recipients = append(recipients, o)
	}
	c.srv.mu.RUnlock()
	for _, o := range recipients {
		o.send(protocol.IDPlaySystemChat, payload)
	}
}

// die drops inventory and experience (unless keep_inventory), shows the
// death screen and message, and plays the death animation for others. The
// body lies until tickDeadBodies removes it.
func (c *client) die(message protocol.Text, damageType registry.DamageType, cause *damageCause) {
	// vanilla: LivingEntity.die
	c.removeEntitiesOnShoulder()
	at := c.player.Position()
	c.srv.gameEvent(c.theWorld(), rules.GameEventEntityDie, at.X, at.Y, at.Z, c.gameEventContext(0))
	// The view returns to the dead body; a dead player follows no camera.
	c.clearCamera()
	// vanilla: ServerPlayer.die
	c.resetStat(player.Custom(registry.CustomStatTimeSinceRest))
	// A dead body leaves any seat: the boat stays, the body where the seat
	// held it.
	c.dismountBoat(true)
	c.dismountMinecart(true)
	c.dismountCushion(true)
	// A dead player counts as unloaded until respawn. vanilla:
	// ServerGamePacketListenerImpl.markClientUnloadedAfterDeath
	c.awaitingRespawn.Store(true)
	c.deathTicks.Store(0)
	// vanilla: ServerPlayer.die
	c.awardStat(player.Custom(registry.CustomStatDeaths), 1)
	c.resetStat(player.Custom(registry.CustomStatTimeSinceDeath))
	c.scoreDeath()
	// A sculk catalyst in reach takes the reward before the orbs drop.
	// vanilla: CatalystListener.handleGameEvent
	plan := c.deathPlan(message, damageType, cause)
	at = c.player.Position()
	xpTaken, _ := c.srv.physicsFor(c.theWorld()).SculkCatalystDeath(at.X, at.Y, at.Z, func() int {
		if plan.keepLevel {
			return 0
		}
		return plan.experience
	})
	// With keep_inventory nothing drops and respawn keeps items and experience
	// (vanilla: transferInventoryXpAndScore).
	var emptied [][]byte
	if !plan.keepInventory {
		emptied = c.dropInventory(plan.drops)
	}
	if !plan.keepLevel && !xpTaken && plan.experience > 0 {
		c.srv.spawnOrbs(c.theWorld(), at.X, at.Y, at.Z, plan.experience)
	}
	c.deathKeeps.Store(&deathKeeps{inventory: plan.keepInventory, level: plan.keepLevel})
	// With show_death_messages off, Combat Death has an empty message and
	// nothing goes to chat.
	if plan.showMessage {
		c.send(protocol.IDPlayCombatDeath, combatDeathPayload(c.player.EntityID, plan.message))
	} else {
		c.send(protocol.IDPlayCombatDeath, combatDeathPayload(c.player.EntityID, protocol.Text{}))
	}
	// Emptied slots follow the death screen packet, vanilla's container sync
	// order.
	for _, payload := range emptied {
		c.send(protocol.IDPlayContainerSlot, payload)
	}
	if plan.showMessage {
		c.broadcastDeathMessage(plan.message)
	}
	// vanilla: ServerPlayer.die clearFire, setTicksFrozen(0), setSharedFlagOnFire(false)
	pos := c.player.Position()
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.FireTicks = 0
		v.TicksFrozen = 0
		// vanilla: Player.die
		v.LastDeath = player.DeathLocation{
			Set: true, Dimension: c.dimension().name,
			X: int32(math.Floor(pos.X)), Y: int32(math.Floor(pos.Y)), Z: int32(math.Floor(pos.Z)),
		}
	})
	// The event plays the victim's death sound too. The watchers' health
	// metadata follows it to tip the body over red. vanilla: ServerPlayer.die
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, protocol.EntityStatusDeath))
	c.srv.broadcastWorldExcept(c.theWorld(), c, protocol.IDPlayEntityMetadata,
		playerMetadata(c.player, true))
}

// deathExperience is the reward the dying body carries: min(level*7, 100),
// none for a spectator. vanilla: Player.getBaseExperienceReward
func (c *client) deathExperience() int {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return 0
	}
	return rules.XpDeathDrops(c.player.Vitals().ExperienceLevel)
}

// respawnPlacement is where a death puts the body: feet position and
// facing.
type respawnPlacement struct {
	x, y, z    float64
	yaw, pitch float32
}

// position is the placement as the player position a teleport takes.
func (p respawnPlacement) position() player.Position {
	return player.Position{X: p.x, Y: p.y, Z: p.z, Yaw: p.yaw, Pitch: p.pitch}
}

// respawnPlacement is where and into which dimension a death returns the
// body: beside an anchor in the nether, beside a bed facing it, or at a
// forced point's cell centre 0.1 up facing its angles. A gone or blocked
// bed, an empty or enclosed anchor, or a forced cell no body fits drops the
// point (missing true); a point outside the world or in an unknown
// dimension uses the world spawn but keeps the point. A forced point in
// any known dimension resolves there. dest is the return dimension.
// vanilla: ServerPlayer.findRespawnPositionAndUseSpawnBlock, TeleportTransition
func (c *client) respawnPlacement(own *respawnPos, consumeAnchor bool) (respawnPlacement, *dimension, bool) {
	if own == nil {
		return c.worldSpawnFallback(false)
	}

	// A point outside the world or in an unknown dimension uses the world
	// spawn, keeping the point.
	pointDim := c.srv.findDimension(own.dim)
	if pointDim == nil {
		return c.worldSpawnFallback(false)
	}
	if !inWorld(float64(own.pos.X), float64(own.pos.Y), float64(own.pos.Z)) {
		return c.worldSpawnFallback(false)
	}

	// An anchor point in a dimension where respawn anchors work.
	// vanilla: RespawnAnchorBlock.canSetSpawn, ServerPlayer.findRespawnAndUseSpawnBlock
	if pointDim.w.RespawnAnchorWorks() {
		place, answer := c.anchorRespawn(pointDim.w, own, consumeAnchor)
		switch answer {
		case anchorHosted:
			return place, pointDim, false
		case anchorRefused:
			return c.worldSpawnFallback(true)
		case anchorAbsent:
			// Not an anchor; fall through to bed or forced-point checks.
		}
	}
	// The end has no bed or anchor, so an unforced point there has nothing to
	// stand on. vanilla: ServerPlayer.findRespawnAndUseSpawnBlock
	if own.dim == world.EndName && !own.forced {
		return c.worldSpawnFallback(true)
	}
	w := pointDim.w

	// Hold the point's chunk and the search's while it runs: BlockAt reads air
	// in an unloaded chunk, so a bed or forced cell unloaded by walking away
	// would look gone. A bed's search reaches two cells out, so three covers it.
	// vanilla: Level.getBlockState
	release := holdChunksAround(w, own.pos.X, own.pos.Z, 3)
	defer release()

	// A bed point: the first free cell around it, facing the bed; an
	// obstructed bed is refused like a gone one.
	// vanilla: BedBlock.findStandUpPosition
	// vanilla: ServerPlayer.findRespawnAndUseSpawnBlock, the bed rule's canSetSpawn
	if state := w.BlockAt(own.pos.X, own.pos.Y, own.pos.Z); registry.BlockOf(state).InGroup(registry.GroupBed) &&
		rules.BedRuleOf(w.DimensionTypeName(), registry.BlockOf(state)).CanSetSpawn.Holds(!w.BrightOutside()) {
		if forward, known := facingDelta[registry.StateValue(state, registry.PropFacing)]; known {
			if px, py, pz, ok := rules.BedStandUpSpot(w, own.pos.X, own.pos.Y, own.pos.Z, forward, own.yaw); ok {
				return respawnPlacement{
					x: px, y: py, z: pz,
					yaw: rules.LookAtYaw(px, pz, own.pos.X, own.pos.Z),
				}, pointDim, false
			}
		}
		return c.worldSpawnFallback(true)
	}

	// A /spawnpoint forced point: the cell and the one above must both fit a
	// body, which stands 0.1 into the floor, just above the point.
	if own.forced {
		bottom := w.BlockAt(own.pos.X, own.pos.Y, own.pos.Z)
		top := w.BlockAt(own.pos.X, own.pos.Y+1, own.pos.Z)
		if rules.PossibleToRespawnInThis(bottom) && rules.PossibleToRespawnInThis(top) {
			return respawnPlacement{
				x:   float64(own.pos.X) + 0.5,
				y:   float64(own.pos.Y) + 0.1,
				z:   float64(own.pos.Z) + 0.5,
				yaw: own.yaw, pitch: own.pitch,
			}, pointDim, false
		}
		return c.worldSpawnFallback(true)
	}

	// A point with neither bed nor forced flag has nothing to stand on.
	// vanilla: the empty return before the forced fallback
	return c.worldSpawnFallback(true)
}

// holdChunksAround acquires every chunk the square (x±r, z±r) touches and
// returns their release, standing in for vanilla's chunk-loading reads.
func holdChunksAround(w *world.World, x, z, r int32) func() {
	minX, minZ := world.ChunkOf(float64(x-r)), world.ChunkOf(float64(z-r))
	maxX, maxZ := world.ChunkOf(float64(x+r)), world.ChunkOf(float64(z+r))
	var held []world.ChunkPos
	for cx := minX; cx <= maxX; cx++ {
		for cz := minZ; cz <= maxZ; cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			held = append(held, pos)
		}
	}
	return func() {
		for _, pos := range held {
			w.Release(pos)
		}
	}
}

// worldSpawnFallback is respawnPlacement's world spawn answer: the shared
// spawn after the respawn_radius search, facing the world spawn's rotation,
// its dimension, and whether the player's point is dropped.
// vanilla: TeleportTransition.createDefault, TeleportTransition.missingRespawnBlock
func (c *client) worldSpawnFallback(missing bool) (respawnPlacement, *dimension, bool) {
	dim, spawn := c.srv.worldSpawn()
	spot := c.srv.adjustSpawnLocation(dim.w, spawn)
	return respawnPlacement{
		x: float64(spot.X) + 0.5, y: float64(spot.Y), z: float64(spot.Z) + 0.5,
		yaw: spawn.Yaw, pitch: spawn.Pitch,
	}, dim, missing
}

// worldSpawn is the world spawn and its dimension (the overworld if unknown),
// read as one pair so a concurrent /setworldspawn cannot split them, moved
// into the dimension's border when it lies outside.
func (s *Server) worldSpawn() (*dimension, world.SpawnPos) {
	spot, name := s.world.SpawnIn()
	dim := s.dimensionByName(name)
	return dim, borderAdjustedSpawn(dim, spot)
}

// borderAdjustedSpawn moves a spawn outside the border to the top
// motion-blocking position at the border's centre, keeping the rotation. The
// stored spawn is untouched.
// vanilla: Level.getWorldBorderAdjustedRespawnData
func borderAdjustedSpawn(dim *dimension, spot world.SpawnPos) world.SpawnPos {
	if dim.border.ContainsBlock(spot.X, spot.Z) {
		return spot
	}
	st := dim.border.State()
	spot.X, spot.Z = int32(math.Floor(st.CenterX)), int32(math.Floor(st.CenterZ))
	pos := world.ChunkPos{X: world.ChunkOf(float64(spot.X)), Z: world.ChunkOf(float64(spot.Z))}
	dim.w.Acquire(pos)
	// The height map value is the first free y, one above the block.
	spot.Y = dim.w.MotionBlockingHeight(spot.X, spot.Z) + 1
	dim.w.Release(pos)
	return spot
}

// sendNoRespawnBlock tells the client its spawn point was unusable (the
// block.minecraft.spawn.not_valid game event).
func (c *client) sendNoRespawnBlock() {
	c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventNoRespawnBlockAvailable, 0))
}

// respawn brings a dead player back in their respawn point's dimension
// (Client Command perform-respawn). After Respawn the client has cleared its
// world state and expects the full join sequence again.
func (c *client) respawn() error {
	if !c.player.Vitals().Dead {
		c.log.Debug("ignoring a respawn from a player who is alive")
		return nil
	}

	// Claim the corpse under the lock that protects its removal. The traveling
	// flag keeps the tracker off records the Respawn invalidates.
	c.trackMu.Lock()
	started := c.traveling.CompareAndSwap(false, true)
	c.trackMu.Unlock()
	if !started {
		return nil
	}
	defer c.traveling.Store(false)
	// The new body's first move only reads the water it stands in.
	c.waterSeen = false

	// The body is removed, and what its windows held (grids and cursor, not
	// dropped by the death) falls where it lay. vanilla: Player.remove(KILLED)
	if err := c.closeWindows(false, true, true); err != nil {
		return err
	}

	// The new body starts with no post effects, and the client is told after
	// the respawn.
	// vanilla: ServerPlayer.restoreFrom (post_effects are not copied)
	c.clearPostEffects()
	defer func() { c.logPostEffects(c.sendPostEffects()) }()

	// Resolve the return before any reset. A point whose bed or anchor is gone,
	// whose anchor is empty, or where no body fits, is unusable: the client is
	// told and the point dropped.
	// vanilla:
	// ServerPlayer.findRespawnPositionAndUseSpawnBlock, PlayerList.respawn
	point := c.spawnPoint.Load()
	place, dest, missing := c.respawnPlacement(point, true)
	if missing {
		c.sendNoRespawnBlock()
		c.spawnPoint.Store(nil)
	}

	// A respawn before the corpse's twenty ticks are up removes the body
	// itself. vanilla: PlayerList.respawn's removePlayerImmediately(KILLED)
	c.srv.fireMobEffectTrigger(c.takeDeathEffects())

	// Only a same-dimension respawn holds the writer for its whole sequence:
	// what the death queued (its bars included) goes before the Respawn, and a
	// bars packet a tick queues for the new body waits behind the sequence
	// rather than reaching the client before or inside it. A cross-dimension
	// respawn takes the hold at the dimension change below.
	pos := place.position()
	if c.pluginListening(pluginruntime.PlayerRespawn) {
		pos, dest = c.pluginRespawn(pos, dest, point, missing)
	}
	sameDimension := c.dimension() == dest
	if sameDimension {
		release, err := c.holdWriter()
		defer release()
		if err != nil {
			return err
		}
	}

	// The respawn removes and readds the player to the player list, putting
	// them at its end. vanilla: PlayerList.respawn
	c.srv.mu.Lock()
	c.srv.appendToPlayerList(c)
	c.srv.mu.Unlock()

	// A death in another dimension respawns through the full dimension change,
	// which readds and mints the body there.
	if !sameDimension {
		if err := c.enterDimensionHeld(dest, command.Move{Pos: pos}, respawnDeath, false); err != nil {
			return err
		}
		c.spectateAfterHardcoreDeath()
		return nil
	}

	if kept := c.resetRespawnBody(pos, respawnDeath); kept {
		// The client may have dropped the slots with the world, so resend the kept
		// inventory in full, after the Respawn below.
		c.player.Inventory(func(inv *player.Inventory) {
			c.send(protocol.IDPlayContainerContent, inv.ContainerContent())
		})
	}

	// A same-dimension Respawn keeps the client's world, so held bodies must be
	// removed first; a dimension change's new world drops them anyway. The
	// records then clear and the next tick pairs whatever is in range at the new
	// spot; a repeated Add of the same ID is harmless. vanilla: PlayerList.respawn
	// removes the old player, whose ChunkMap.removeEntity sends every body
	// it held back out first.
	if ids := c.srv.observerRemovals(c); len(ids) > 0 {
		if err := c.writePacket(protocol.IDPlayRemoveEntities, "remove entities", removeEntitiesPayload(ids)); err != nil {
			return err
		}
	}
	c.srv.forgetObserver(c)
	// Waypoint pairings go with the bodies; the pass remakes them within a
	// tick, a repeated track of the same UUID repairing the bar.
	c.srv.forgetWaypoints(c, c.theWorld())

	// A same-dimension respawn still readds the body at the end of its
	// dimension's list. vanilla: ServerLevel.addRespawnedPlayer
	c.srv.appendToLevel(c)

	// Respawn's layout is Login (play)'s from dimension type to sea level, plus
	// the data-kept flags.
	if err := c.writePacket(protocol.IDPlayRespawn, "respawn",
		respawnPayload(c.dimension(), c.srv.world, c.player.Vitals(), 0, 0)); err != nil {
		return err
	}
	if err := c.writeHealth(); err != nil {
		return err
	}
	// Empty, or the carried-over bar under keep_inventory or for a spectator.
	if err := c.sendExperience(); err != nil {
		return err
	}
	if err := c.sendAbilities(); err != nil {
		return err
	}
	if err := c.syncPosition(); err != nil {
		return err
	}
	// vanilla: PlayerList.respawn sends time, world spawn, weather and the
	// start-waiting-for-chunks game event; without it "Loading terrain" never
	// closes.
	if err := c.sendInitializeBorder(); err != nil {
		return err
	}
	if err := c.sendUpdateTime(); err != nil {
		return err
	}
	if err := c.sendSpawnPosition(); err != nil {
		return err
	}
	// A same-dimension respawn is an arrival too, into a clear sky.
	if err := c.sendWeather(c.dimension().w); err != nil {
		return err
	}
	if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventStartWaitingPayload()); err != nil {
		return err
	}
	// The client's new body starts from its defaults; so does the server's.
	c.player.ResetAttributes()
	if err := c.introduceAttributes(); err != nil {
		return err
	}
	// The anchor the body returned to plays its charge sound to this body only.
	// vanilla: PlayerList.respawn
	if payload := c.anchorDepletePayload(); payload != nil {
		c.send(protocol.IDPlaySoundEffect, payload)
	}

	// Replace the corpse others see with a live body at the spawn.
	w := c.theWorld()
	c.srv.broadcastWorldExcept(w, c, protocol.IDPlayRemoveEntities, removeEntity(c.player))
	c.srv.broadcastBodyWorldExcept(w, c, playerPairing(c.player, spawnPlayer(c.player)), true)
	c.srv.recordBody(w, c)
	c.sendChunksAsync()

	c.spectateAfterHardcoreDeath()
	c.log.Info("player respawned", "x", place.x, "y", place.y, "z", place.z)
	return nil
}

// spectateAfterHardcoreDeath puts a hardcore world's respawned body in
// spectator. vanilla: ServerGamePacketListenerImpl.handleClientCommand PERFORM_RESPAWN
func (c *client) spectateAfterHardcoreDeath() {
	if c.srv.world.Hardcore() {
		c.SetGameMode(protocol.GameModeSpectator)
	}
}

// respawnFromCredits ends the end credits (the Client Command their screen
// ends with): a respawn keeping everything the live body holds (health,
// effects, experience, inventory), moving it to the respawn point through a
// death's dimension change, out of the end.
// vanilla: ServerGamePacketListenerImpl.handleClientCommand's wonGame branch,
// PlayerList.respawn(player, keepAllPlayerData=true)
func (c *client) respawnFromCredits() error {
	// The respawn holds the traveling flag like a death's: a change in flight
	// refuses it with nothing half-done. The credits come off only once the
	// flag is held, or the portal's own travel could carry the player out ahead
	// of this respawn.
	if !c.traveling.CompareAndSwap(false, true) {
		return nil
	}
	defer c.traveling.Store(false)
	c.wonGame.Store(false)

	// Resolve the destination before moving. An unusable point is told and
	// dropped (vanilla keeps the point only where it stood). The credits spend
	// no anchor charge: PlayerList.respawn passes keepAll, so the search runs
	// with useSpawnBlock false.
	// vanilla: PlayerList.respawn's copyRespawnPosition
	place, dest, missing := c.respawnPlacement(c.spawnPoint.Load(), false)
	if missing {
		c.sendNoRespawnBlock()
		c.spawnPoint.Store(nil)
	}

	// The respawned player goes to the end of the player list, as after a
	// death. vanilla: PlayerList.respawn
	c.srv.mu.Lock()
	c.srv.appendToPlayerList(c)
	c.srv.mu.Unlock()

	return c.enterDimensionHeld(dest,
		command.Move{Pos: place.position()}, respawnCredits, false)
}

// takeDeathEffects clears the dead body's effects and captures the removal
// trigger so it can run at the corpse's position after the tracking lock is released.
// vanilla: LivingEntity.remove's triggerOnDeathMobEffects
func (c *client) takeDeathEffects() mobEffectTrigger {
	var dying []effect.Instance
	c.player.UpdateBody(func(_ *player.Vitals, effects *effect.Set, _ *player.Attributes) {
		for _, id := range [...]int32{effectOozing, effectWeaving, effectWindCharged} {
			if in, ok := effects.Get(id); ok {
				dying = append(dying, in)
			}
		}
		effects.Clear()
	})
	if len(dying) == 0 {
		return mobEffectTrigger{}
	}
	pos := c.player.Position()
	minX, minY, minZ, maxX, maxY, maxZ := c.Box()
	return mobEffectTrigger{
		w:      c.theWorld(),
		player: true,
		box:    entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ).Inflate(2),
		x:      pos.X, y: pos.Y, z: pos.Z,
		dice:    c.ownDice(),
		effects: dying,
		killed:  true,
		height:  maxY - minY,
	}
}

// resetRespawnBody sets up the body a respawn places at pos as newly
// arrived: metadata baseline, dig reset, client-load timer. A death's body is
// fresh (vitals reset, effects gone); the credits' keeps everything. It runs
// only once pos is in the respawn dimension, so no tick sees a fresh live
// body in the old world, and reports whether a death's vitals kept the
// inventory.
func (c *client) resetRespawnBody(pos player.Position, respawn respawnKind) bool {
	var kept bool
	if respawn == respawnDeath {
		// Experience carries over exactly when dropExperience dropped none; vitals
		// reset and bar kept in one update so no tick interleaves. The credits'
		// respawn is restoreFrom with everything, so nothing resets.
		// vanilla: ServerPlayer.restoreFrom
		keep := c.takeDeathKeeps()
		c.player.UpdateBody(func(v *player.Vitals, _ *effect.Set, attrs *player.Attributes) {
			*v, kept = rules.RespawnVitalsKeeping(*v, keep.inventory, keep.level)
			// The new body keeps bases, without the dead body's modifiers.
			v.Health = attrs.Base(registry.AttributeMaxHealth)
		})
		// Effects go with the dead body (vanilla:
		// LivingEntity.triggerOnDeathMobEffects); the credits' body keeps its own.
		c.player.UpdateBody(func(_ *player.Vitals, effects *effect.Set, _ *player.Attributes) { effects.Clear() })
	}
	// The new body has no use or cooldown from before: vanilla builds the
	// respawned player fresh ItemCooldowns. vanilla: ServerPlayer.<init>
	c.shieldMu.Lock()
	c.raise = raiseSession{}
	c.cooldowns = nil
	c.shieldMu.Unlock()
	c.spearMu.Lock()
	c.spear = spearCharge{}
	c.spearMu.Unlock()
	c.trackMu.Lock()
	if respawn == respawnDeath {
		c.deathTicks.Store(0)
		c.bodyGone.Store(false)
		c.holdsDropped.Store(false)
	}
	c.sentFire = false
	c.sentAir = rules.MaxAirSupply
	c.sentFrozen = 0
	c.sentArrows = 0
	c.sentStingers = 0
	// Baseline the new body's look, so a spectator's invisible bit is not a
	// change on the first tick.
	c.shown = bodyNow(c.player, bodyShown{})
	c.trackMu.Unlock()
	// Drop the interrupted dig and its delayed destroy: vanilla's respawn
	// builds a fresh game mode.
	c.resetDig()
	// vanilla: ServerGamePacketListenerImpl.restartClientLoadTimerAfterRespawn
	c.awaitingRespawn.Store(false)
	c.scoreResample.Store(true)
	c.loadTicks.Store(clientLoadTicks)
	c.player.SetPosition(pos)
	c.portalPrevStale.Store(true)
	// Otherwise the first move would be sent as a delta from the death spot.
	c.trackMu.Lock()
	c.tracked = track(pos)
	c.trackMu.Unlock()
	return kept
}

// trackMovement applies a step's survival effects (rules.TrackMovement):
// fall damage, farmland trampling, jump and sprint exhaustion.
func (c *client) trackMovement(was, now player.Position) {
	look := c.player.Appearance()

	water := c.waterAt(now)
	climbing := c.climbableAt(was)
	netherPortalInstant := c.theWorld().IntRule(world.RulePlayersNetherPortalDefaultDelay) == 0
	swept := rules.FallResetSweep(c.theWorld(), c.player.Vitals().FallDistance, was, now, true, netherPortalInstant)
	landing := c.landingAt(now, was)
	var safeFall, fallMultiplier float64
	c.player.Attributes(func(a *player.Attributes) {
		safeFall = a.Value(registry.AttributeSafeFallDistance)
		fallMultiplier = a.Value(registry.AttributeFallDamageMultiplier)
	})
	effects := c.player.Effects()
	// vanilla: ServerGamePacketListenerImpl.handleMovePlayer (isFallFlying,
	// isAutoSpinAttack, hasLandedInLava); TrackMovement covers the rest.
	impulseReset := look.FallFlying || look.SpinAttack
	if !impulseReset && c.player.Vitals().ImpulseImpact != nil && now.Y-was.Y < 1e-5 {
		dims := c.dims(look.Pose)
		impulseReset = entity.BoxInLava(c.theWorld(), now.X, now.Y, now.Z, dims.Width, dims.Height)
	}
	var fallDamage float64
	var landedFall float64
	var fellFrom *[3]float64
	var blastAt [3]float64
	var blastCause string
	var blasted bool
	c.player.UpdateVitals(func(v *player.Vitals) {
		fallDamage, landedFall = rules.TrackMovement(v, effects, was, now, c.flying.Load(), look.Sprinting, water, look.Swimming, climbing, swept, safeFall, fallMultiplier, landing)
		if impulseReset {
			rules.TryResetImpulse(v)
		}
		blastAt, blastCause, blasted = fallBegan(v, now)
		fellFrom = fallEnded(v, now)
	})
	if blasted {
		c.fellAfterExplosion(blastAt, blastCause, now)
	}
	c.countMovement(was, now, look, water)
	c.countFall(landedFall, landing.Ground)
	if climbing {
		c.combatMu.Lock()
		c.lastClimbable = [3]int32{int32(math.Floor(was.X)), int32(math.Floor(was.Y)), int32(math.Floor(was.Z))}
		c.hasLastClimbable = true
		c.combatMu.Unlock()
	}
	if now.OnGround && landedFall > 0 {
		c.smashDust(landing.Ground, now, landedFall)
		if count := rules.LandingParticleCount(landedFall, safeFall); count > 0 && registry.BlockOf(landing.Ground) != registry.BlockAir {
			// vanilla: LivingEntity.checkFallDamage, before fallOn scales anything
			x, z := rules.LandingParticleXZ(now.X, now.Z, landing.GroundX, landing.GroundZ)
			c.srv.sendBlockParticles(c.theWorld(), protocol.ParticleBlock, landing.Ground, x, now.Y, z, 0, 0, 0, 0.15, count)
		}
	}
	if fallDamage > 0 {
		// Both sounds go to all but the faller, whose client plays them, and
		// before the hurt whatever it then does.
		// vanilla: LivingEntity.causeFallDamage, playBlockFallSound
		w := c.theWorld()
		c.srv.broadcastSoundIDExcept(w, c, rules.FallSound(fallDamage, rules.FallPlayer),
			protocol.SoundCategoryPlayer, now.X, now.Y, now.Z, 1, 1)
		if fall, volume, pitch, ok := blockFallSound(w, now.X, now.Y, now.Z); ok {
			c.srv.broadcastSoundIDExcept(w, c, fall, protocol.SoundCategoryPlayer, now.X, now.Y, now.Z, volume, pitch)
		}
		c.hurtFalling(fallDamage, rules.FallOn(landing.Ground).DamageType, nil, landedFall)
	}
	if now.OnGround {
		c.combatMu.Lock()
		c.hasLastClimbable = false
		c.combatMu.Unlock()
	}
	if now.OnGround && landedFall > 0 && rules.IsHoneyBlock(landing.Ground) {
		c.honeyLanded(fallDamage > 0)
	}
	// Restitution needs the held downward movement to beat gravity.
	// vanilla: Entity.restituteMovementAfterCollisions
	bounced := false
	if now.OnGround && now.Y < was.Y && !look.Sneaking && rules.BlockBounceRestitution(landing.Ground, true) > 0 {
		var gravity float64
		c.player.Attributes(func(a *player.Attributes) { gravity = a.Value(registry.AttributeGravity) })
		bounced = -math.Float64frombits(c.priorDY.Load()) > gravity
	}
	if bounced {
		c.bouncedAt.Store(c.srv.ticksRun.Load() + 1)
	} else {
		c.bouncedAt.Store(0)
	}
	if now.OnGround && landedFall > 0 && !look.Sneaking {
		c.bounceLanded(now)
	}
	c.honeySlideAdvancements(was, now)
	c.splashEvent(was, now, water != rules.Dry)
	c.stepEvent(was, now, look, water != rules.Dry)
	if landedFall > 0 {
		c.trampleUnderfoot(landedFall, landing)
		c.trampleTurtleEgg(world.TurtleEggLandOdds)
		// vanilla: Entity.checkFallDamage
		c.srv.gameEvent(c.theWorld(), rules.GameEventHitGround, now.X, now.Y, now.Z, c.gameEventContext(landing.Ground))
	}
	// The location_changed entries run when the step changed the block, and on
	// a settled fall cell change or not.
	// vanilla: LivingEntity.checkFallDamage calls onChangedBlock
	c.walkLocated(landedFall > 0, false)
	if fellFrom != nil {
		c.fellAdvancements(*fellFrom, now)
	}
}

// honeyLanded plays a honey block landing: the slide sound and, if the
// fall damaged, the block's fall sound at playBlockFallSound's half volume
// and three-quarter pitch. Both go to all but the lander, whose client plays
// them; the slide event reaches everyone.
// vanilla: HoneyBlock.fallOn, LivingEntity.playBlockFallSound
func (c *client) honeyLanded(damaged bool) {
	w := c.theWorld()
	pos := c.player.Position()
	c.srv.broadcastSoundIDExcept(w, c,
		registry.SoundBlockHoneyBlockSlide, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)

	c.srv.broadcastWorld(w, protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, protocol.EntityEventHoneySlide))
	if !damaged {
		return
	}
	if fall, volume, pitch, ok := rules.BlockFallSound(registry.BlockHoneyBlock); ok {
		c.srv.broadcastSoundIDExcept(w, c,
			fall, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, volume, pitch)

	}
}

// bounceLanded plays the bounce sound of the block a landing player bounces
// on, to everyone, the player included: vanilla plays it with no player to
// skip. The client bounces itself.
// vanilla: Entity.restituteMovementAfterCollisions, ShelfMushroomBlock.bounceOn
func (c *client) bounceLanded(now player.Position) {
	w := c.theWorld()
	dims := c.dims(c.player.Appearance().Pose)
	x, y, z, state := rules.OnPosCell(w, now.X, now.Y, now.Z, dims.Width/2)
	if sound := rules.BounceSound(state); sound != registry.SoundNone {
		c.srv.broadcastSoundID(w, sound, protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	}
}

// trampleUnderfoot may turn farmland 0.2 below the feet to dirt after a
// fall; creative tramples too. It is the player half of FarmlandBlock.fallOn:
// spawn protection and a body too small in its pose (crawling, swimming)
// keep the farmland. Unlike vanilla, a gated landing rolls no dice.
// vanilla: FarmlandBlock.fallOn
func (c *client) trampleUnderfoot(fallDistance float64, landing rules.Landing) {
	w := c.theWorld()
	dims := c.dims(c.player.Appearance().Pose)
	// The landing's supporting block, not the one under the centre: an edge
	// landing tramples the farmland the feet rest on.
	x, ground, z := landing.GroundX, landing.GroundY, landing.GroundZ
	if c.underSpawnProtection(x, z) {
		return
	}
	// vanilla compares the float bounding box against 0.512f.
	if bw, bh := float32(dims.Width), float32(dims.Height); bw*bw*bh <= 0.512 {
		return
	}
	changes := c.srv.physicsFor(w).FarmlandFallenOn(x, ground, z, fallDistance)
	// The event is posted before the client is told, as vanilla's comes
	// within the tick and the block reaches the client at its end.
	if len(changes) > 0 {
		c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(ground)+0.5, float64(z)+0.5, w.BlockAt(x, ground, z))
	}
	c.srv.tellBlockChanges(w, changes)
}

// powderSnowFlakes sends, for each powder snow cell the player is inside
// while its feet are in powder snow and it moves sideways, a snowflake above
// the cell on half the ticks.
// vanilla: PowderSnowBlock.entityInside
func (c *client) powderSnowFlakes(pos player.Position, snows []rules.CauldronCell) {
	if len(snows) == 0 || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	if m := c.player.KnownMovement(); m.X == 0 && m.Z == 0 {
		return
	}
	w := c.theWorld()
	if !rules.IsPowderSnow(w.BlockAt(int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))) {
		return
	}
	s := c.srv
	for _, cell := range snows {
		if s.levelRng.Intn(2) == 0 {
			continue
		}
		s.sendSnowflake(w, pos.X, float64(cell.Y+1), pos.Z)
	}
}

// sendSnowflake sends the snowflake a body kicks up from powder snow at x,
// y, z, drifting sideways at random and rising.
// vanilla: PowderSnowBlock.entityInside
func (s *Server) sendSnowflake(w *world.World, x, y, z float64) {
	sx := (s.levelRng.Float32()*2 - 1) * 0.083333336
	sz := (s.levelRng.Float32()*2 - 1) * 0.083333336
	s.sendParticlePayload(w, motionParticlesPayload(protocol.ParticleSnowflake, x, y, z,
		0, 0, 0, sx, 0.05, sz, 1, particleRandomAlternative), x, y, z)
}

// snowflakeSpot is one point a body kicked up a snowflake's chance at.
type snowflakeSpot struct {
	w       *world.World
	x, y, z float64
}

// rollSnowflakes sends a snowflake at each spot on the level random's even
// odds.
// vanilla: PowderSnowBlock.entityInside
func (s *Server) rollSnowflakes(spots []snowflakeSpot) {
	for _, f := range spots {
		if s.levelRng.Intn(2) == 1 {
			s.sendSnowflake(f.w, f.x, f.y, f.z)
		}
	}
}

// meltPowderSnow destroys the powder snow a burning body doused itself in,
// with the break event and no drops.
// vanilla: PowderSnowBlock.entityInside, Level.destroyBlock
func (s *Server) meltPowderSnow(w *world.World, x, y, z int32) {
	state := w.BlockAt(x, y, z)
	if !rules.IsPowderSnow(state) {
		return
	}
	s.broadcastLevelEvent(w,
		protocol.WorldEventBlockBreak, x, y, z, int32(state))

	s.setBlockAndTell(w, x, y, z, airBlockState)
}

// mobMeltedSnow melts the powder snow a burning mob doused itself in, when
// mob_griefing allows.
func (s *Server) mobMeltedSnow(g mobGraze) {
	if g.w.BoolRule(world.RuleMobGriefing) {
		s.meltPowderSnow(g.w, g.x, g.y, g.z)
	}
}

// habitat gathers what the environment rules read for one tick. It scans
// only the cells the pose's box overlaps now; unlike vanilla's
// checkInsideBlocks it does not sweep the path between reports, so a fast
// fall is judged where it landed.
func (c *client) habitat(pos player.Position) rules.Habitat {
	w := c.theWorld()
	dims := c.dims(c.player.Appearance().Pose)
	raining := rules.RainsOnBody(w, pos.X, pos.Y, pos.Z, pos.Y+dims.Height)
	effects := c.player.Effects()
	// A spectator never freezes; others are protected only by
	// freeze_immune_wearables in the four armour slots vanilla's canFreeze
	// checks, never by held items.
	freezeImmune := c.player.GameMode() == protocol.GameModeSpectator
	if !freezeImmune {
		c.player.Inventory(func(inv *player.Inventory) {
			freezeImmune = rules.FreezeImmuneWearables(
				inv.Slots[player.ArmorSlot], inv.Slots[player.ArmorSlot+1],
				inv.Slots[player.ArmorSlot+2], inv.Slots[player.ArmorSlot+3])
		})
	}
	overlapped, cauldrons, snows := c.overlapCells(pos, dims.Width/2, dims.Height)
	_, eyesIn, bubbleAtEyes := entity.BoxInWater(w, pos.X, pos.Y, pos.Z,
		dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	known := c.player.KnownMovement()
	return rules.Habitat{
		Overlapped:         overlapped,
		InLava:             entity.BoxInLava(w, pos.X, pos.Y, pos.Z, dims.Width, dims.Height),
		Ground:             rules.OnPos(w, pos.X, pos.Y, pos.Z, dims.Width/2),
		EyesInWater:        eyesIn,
		EyesInBubbleColumn: bubbleAtEyes,
		WaterBreathing:     rules.HasWaterBreathing(effects),
		HoldAir:            !rules.ShouldRefillAirSupply(effects),
		EyesInWall:         c.eyesInWall(pos),
		Raining:            raining,
		Sneaking:           c.player.Appearance().Sneaking,
		Moving:             rules.BerryBushMoving(known.X, known.Z),
		OnGround:           pos.OnGround,
		FreezeImmune:       freezeImmune,
		Cauldrons:          cauldrons,
		PowderSnows:        snows,
		BurningTimeDelta:   c.burningTime() - 1,
	}
}

// bubbleSurfaceSplash sends the splash of each bubble column surface cell
// the player's box overlaps; a spectator and a flier are skipped.
// vanilla: Player.onAboveBubbleColumn, Entity.sendBubbleColumnParticles
func (c *client) bubbleSurfaceSplash(pos player.Position) {
	if c.player.GameMode() == protocol.GameModeSpectator || c.flying.Load() {
		return
	}
	dims := c.dims(c.player.Appearance().Pose)
	w := c.theWorld()
	entity.BubbleSurfaceCells(w, pos.X, pos.Y, pos.Z, dims.Width, dims.Height, func(x, y, z int32) {
		c.srv.sendBubbleSurface(bubbleSurface{w, x, y, z})
	})
}

// burningTime is the player's burning_time attribute, scaling ignition
// ticks.
func (c *client) burningTime() float64 {
	var v float64
	c.player.Attributes(func(a *player.Attributes) {
		v = a.Value(rules.BurningTimeAttribute)
	})
	return v
}

// overlapCells returns the block states of cells a box of halfWidth and
// height at pos overlaps, plus the layered cauldrons and powder snow among
// them, which a burning body lowers and melts.
// vanilla: LayeredCauldronBlock.entityInside, PowderSnowBlock.entityInside
func (c *client) overlapCells(pos player.Position, halfWidth, height float64) ([]uint16, []rules.CauldronCell, []rules.CauldronCell) {
	w := c.theWorld()
	minX, maxX := rules.CellRange(pos.X-halfWidth, pos.X+halfWidth)
	minY, maxY := rules.CellRange(pos.Y, pos.Y+height)
	minZ, maxZ := rules.CellRange(pos.Z-halfWidth, pos.Z+halfWidth)
	var cells []uint16
	var cauldrons, snows []rules.CauldronCell
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				state := w.BlockAt(x, y, z)
				cells = append(cells, state)
				if rules.IsLayeredCauldron(state) {
					cauldrons = append(cauldrons, rules.CauldronCell{X: x, Y: y, Z: z})
				}
				if rules.IsPowderSnow(state) {
					snows = append(snows, rules.CauldronCell{X: x, Y: y, Z: z})
				}
			}
		}
	}
	return cells, cauldrons, snows
}

// eyesInWall reports whether a suffocating block is at the eyes, by the
// rules' mob slab (vanilla: Entity.isInWall).
func (c *client) eyesInWall(pos player.Position) bool {
	dims := c.dims(c.player.Appearance().Pose)
	return rules.EyesInWall(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width, dims.EyeHeight)
}

// tellEnvironment sends on-fire, air and frozen ticks, when changed, to the
// whole world including the player.
func (c *client) tellEnvironment(v player.Vitals) {
	c.trackMu.Lock()
	fire := rules.OnFire(&v)
	burn := fire != c.sentFire
	air := v.AirSupply != c.sentAir
	frozen := v.TicksFrozen != c.sentFrozen
	arrows := v.ArrowCount != c.sentArrows
	stingers := v.StingerCount != c.sentStingers
	if !burn && !air && !frozen && !arrows && !stingers {
		c.trackMu.Unlock()
		return
	}
	c.sentFire = fire
	c.sentAir = v.AirSupply
	c.sentFrozen = v.TicksFrozen
	c.sentArrows = v.ArrowCount
	c.sentStingers = v.StingerCount
	// Broadcast after the unlock: it uses s.mu's read lock, which a mover holds
	// while taking trackMu.
	c.trackMu.Unlock()
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata,
		burningMetadata(c.player, burn, fire, air, v.AirSupply, frozen, v.TicksFrozen, arrows, v.ArrowCount, stingers, v.StingerCount))
}

// exhaust adds food exhaustion under the vitals lock.
func (c *client) exhaust(amount float64) {
	c.player.UpdateVitals(func(v *player.Vitals) { rules.Exhaust(v, amount) })
}

// tickAttackStrength advances the attack cooldown, resetting it when the
// main-hand item changes. vanilla: Player.tick
func (c *client) tickAttackStrength() {
	c.attackStrengthTicks.Add(1)
	c.swing.tick()
	held := c.heldItem(protocol.HandMain)
	if !sameAttackItem(held, c.lastAttackItem) {
		c.lastAttackItem = held
		c.attackStrengthTicks.Store(0)
	}
}

// sameAttackItem compares item IDs only, ignoring count and components.
// vanilla: ItemStack.isSameItem
func sameAttackItem(a, b protocol.ItemStack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() == b.Empty()
	}
	return a.ItemID == b.ItemID
}

// waterAt reports Submerged with the eyes under water and Wading with the
// box in it, via the fluid box walk vanilla's isEyeInFluid and isInWater
// use: a box overlapping a water cell is in the water even if its centre is
// beside it. vanilla: EntityFluidInteraction.update, Entity.isInWater
func (c *client) waterAt(pos player.Position) rules.Water {
	dims := c.dims(c.player.Appearance().Pose)
	inWater, eyesIn, _ := entity.BoxInWater(c.theWorld(), pos.X, pos.Y, pos.Z,
		dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	switch {
	case eyesIn:
		return rules.Submerged
	case inWater:
		return rules.Wading
	}
	return rules.Dry
}

// climbableAt reports whether the feet block at pos is climbable (the block
// below for the trapdoor rule). Callers pass the step's start, as vanilla's
// LivingEntity.onClimbable reads the body before the move.
func (c *client) climbableAt(pos player.Position) bool {
	w := c.theWorld()
	x, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Z))
	y := int32(math.Floor(pos.Y))
	return rules.IsClimbable(w.BlockAt(x, y, z), w.BlockAt(x, y-1, z))
}

// attackDamage is the current attack_damage attribute: the fist's base with
// held item, strength and weakness modifiers.
// vanilla: Player.attack, getAttributeValue(ATTACK_DAMAGE)
func (c *client) attackDamage() float64 {
	// A spin hits with its starting damage, not the attribute.
	// vanilla: Player.attack (isAutoSpinAttack ? autoSpinAttackDmg)
	if c.player.Appearance().SpinAttack {
		return rules.SpinAttackDamage
	}
	var damage float64
	c.player.Attributes(func(a *player.Attributes) {
		damage = a.Value(registry.AttributeAttackDamage)
	})
	return damage
}

// attackKnockbackBase is the attack_knockback attribute, the knockback the
// weapon's enchantments start from.
// vanilla: LivingEntity.getKnockback
func (c *client) attackKnockbackBase() float64 {
	var base float64
	c.player.Attributes(func(a *player.Attributes) {
		base = a.Value(registry.AttributeAttackKnockback)
	})
	return base
}

// attackSpeed is the current attack_speed attribute: the fist's base with
// held item, haste and mining fatigue modifiers.
// vanilla: Player.getCurrentItemAttackStrengthDelay
func (c *client) attackSpeed() float64 {
	var speed float64
	c.player.Attributes(func(a *player.Attributes) {
		speed = a.Value(registry.AttributeAttackSpeed)
	})
	return speed
}

// landingAt gathers what a landing reads: the supporting block, the feet
// block (for powder snow), and the box's cells, where a web or berry bush
// cancels the fall. A step that neither lands nor falls skips the scan.
// vanilla: Entity.getOnPosLegacy, Entity.checkInsideBlocks
func (c *client) landingAt(now, was player.Position) rules.Landing {
	if !now.OnGround && now.Y >= was.Y {
		return rules.Landing{}
	}
	w := c.theWorld()
	dims := c.dims(c.player.Appearance().Pose)
	overlapped, _, _ := c.overlapCells(now, dims.Width/2, dims.Height)
	gx, gy, gz, ground := rules.OnPosCellMoved(w, now.X, now.Y, now.Z, dims.Width/2, now.X-was.X, now.Z-was.Z)
	return rules.Landing{
		Ground:     ground,
		GroundX:    gx,
		GroundY:    gy,
		GroundZ:    gz,
		Feet:       w.BlockAt(int32(math.Floor(now.X)), int32(math.Floor(now.Y)), int32(math.Floor(now.Z))),
		Overlapped: overlapped,
	}
}

// handleAttack handles serverbound Attack on a player or a mob (see
// attackMob). Damage scales with the attack cooldown (vanilla:
// Player.getAttackStrengthScale); a landed hit knocks back and wears the
// weapon (registry.StackDamagePerAttack).
func (c *client) handleAttack(r *protocol.Reader) error {
	target := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("attack", err)
	}
	// vanilla: ServerGamePacketListenerImpl.handleAttack ignores loading clients
	// and spectators; the packet is decoded first.
	if !c.clientLoaded() {
		return nil
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	if c.player.Vitals().Dead {
		return nil
	}
	if c.targetOutsideBorder(target) {
		return nil
	}
	// A piercing weapon's blow is its jab; vanilla refuses it after the reach
	// check, both silently, so the order does not show.
	if _, ok := registry.StackPiercingWeapon(c.heldItem(protocol.HandMain)); ok {
		return nil
	}
	if !c.pluginAttack(target) {
		return nil
	}
	victim := c.srv.entityClient(target)
	if victim == c {
		return nil
	}
	if victim == nil {
		// Not a player: try a painting, then a frame in this dimension, an armor
		// stand, a boat, then a mob.
		if c.attackPainting(target) {
			return nil
		}
		if c.attackItemFrame(target) {
			return nil
		}
		// Leash knots come before mobs, like the other hanging bodies.
		if c.attackKnot(target) {
			return nil
		}
		if c.attackArmorStand(target) {
			return nil
		}
		if c.attackMannequin(target) {
			return nil
		}
		if c.attackEndCrystal(target) {
			return nil
		}
		if c.attackInteraction(target) {
			return nil
		}
		if c.attackCushion(target) {
			return nil
		}
		// Boats come before mobs, with their own reach and handling.
		if c.srv.boatByEntityID(c.theWorld(), target) != nil {
			c.attackBoat(target)
			return nil
		}
		// Then carts, with their own reach and accumulated damage.
		if c.srv.minecartByEntityID(c.theWorld(), target) != nil {
			c.attackMinecart(target)
			return nil
		}
		if c.attackFireball(target) {
			return nil
		}
		c.attackMob(target)
		return nil
	}
	c.attackClient(victim)
	return nil
}

// noteHurtMob records id as the entity this player last hit, which a tame
// wolf's owner-hurt-target goal reads.
// vanilla: LivingEntity.setLastHurtMob
func (c *client) noteHurtMob(id int32) {
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.LastHurtMob, v.LastHurtMobAge = id, v.AgeTicks
	})
}

// ownerHurtMark is a projectile's hit on id, to record as its player owner's
// last-hurt entity once the locks are released.
type ownerHurtMark struct {
	owner *client
	id    int32
}

// markOwnerHurt appends the mark when the owner is a connected player.
func markOwnerHurt(marks []ownerHurtMark, clients map[int32]*client, ownerID, id int32) []ownerHurtMark {
	if c := clients[ownerID]; c != nil {
		marks = append(marks, ownerHurtMark{c, id})
	}
	return marks
}

func (*Server) noteOwnerHurt(m ownerHurtMark) { m.owner.noteHurtMob(m.id) }

// attackClient is the player's half of a hit on another player.
// vanilla: Player.attack
func (c *client) attackClient(victim *client) {
	// Out of reach spends nothing, not even the cooldown.
	minX, minY, minZ, maxX, maxY, maxZ := victim.Box()
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return
	}
	held := c.attackWeapon()
	blow, ok := c.prepareAttack(held, protocol.EntityTypePlayer, true, victimLoot(victim))
	if !ok {
		return
	}
	from := c.player.Position()
	healthBefore := victim.player.Vitals().Health
	damageType := rules.DamageTypePlayerAttack
	if blow.smash {
		damageType = rules.DamageTypeMaceSmash
	}
	if d := registry.StackDamageType(held); d != 0 {
		damageType = d
	}
	landedHit, softened := victim.hurt(blow.damage, damageType, &damageCause{
		entityID: c.player.EntityID,
		x:        from.X, y: from.Y, z: from.Z,
		weapon:         held,
		killer:         &rules.Killer{Name: c.DisplayName(), Weapon: held},
		disableSeconds: disableSeconds(held),
		shove:          true,
	})
	if !landedHit {
		c.attackSound(soundAttackNoDamage)
		return
	}
	// The blow was decided in the victim's world; a victim who changed
	// dimension meanwhile is not pushed.
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback)
	c.endSprintAfterKnockback(extra)
	if softened {
		// A window-only hit skipped default knockback; the attacker's extra shove
		// still applies.
		if extra > 0 {
			victim.knockbackFromPointExtraOnlyBy(c.theWorld(), c.player.UUID, float64(from.Yaw), extra, syncVelocityNow)
		}
	} else {
		victim.knockbackFromPointExtraBy(c.theWorld(), c.player.UUID, from.X, from.Y, from.Z, float64(from.Yaw), extra, syncVelocityNow)
	}
	if blow.feedback.Sweep {
		c.sweepHurt(c.theWorld(), held, blow.baseDamage, victim.player.EntityID,
			minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.noteHurtMob(victim.player.EntityID)
	c.landedBlowFeedback(blow.feedback, victim.player.EntityID)
	if blow.smash {
		to := victim.player.Position()
		c.landSmash(c.theWorld(), blow.fall, smashVictim{
			id: victim.player.EntityID, x: to.X, y: to.Y, z: to.Z, onGround: to.OnGround,
			halfWidth: victim.dims(victim.player.Appearance().Pose).Width / 2, damage: blow.damage,
			player: victim.player.UUID})
	}
	c.srv.postAttack(c.theWorld(), attackBody{player: c}, attackBody{player: victim}, held, &blow.context, syncVelocityNow)
	c.resetSmashFall(blow.smash)
	c.awardWeaponHit(held)
	c.wearAttackWeapon(registry.StackDamagePerAttack(held))
	to := victim.player.Position()
	c.damageDealt(c.theWorld(), to.X, to.Y+playerHeight(victim)/2, to.Z,
		healthBefore-victim.player.Vitals().Health)
	c.exhaust(rules.AttackExhaustion)
}

// The sounds of a player's blow.
const (
	soundAttackCrit      = registry.SoundEntityPlayerAttackCrit
	soundAttackKnockback = registry.SoundEntityPlayerAttackKnockback
	soundAttackNoDamage  = registry.SoundEntityPlayerAttackNodamage
	soundAttackStrong    = registry.SoundEntityPlayerAttackStrong
	soundAttackSweep     = registry.SoundEntityPlayerAttackSweep
	soundAttackWeak      = registry.SoundEntityPlayerAttackWeak
)

// canCriticalAttack reports whether a full-strength blow now would crit,
// against a living target or not. It needs fallDistance > 0, not just the
// ground bit unset, so a fresh join's first blow never crits.
// vanilla: Player.canCriticalAttack
func (c *client) canCriticalAttack(living bool) bool {
	if !living || c.isPassenger() ||
		rules.HasBlindness(c.player.Effects()) {
		return false
	}
	pos := c.player.Position()
	return c.player.Vitals().FallDistance > 0 && !pos.OnGround &&
		!c.climbableAt(pos) && c.waterAt(pos) == rules.Dry &&
		!c.player.Appearance().Sprinting
}

// endSprintAfterKnockback ends the attacker's sprint after a blow shoved
// its victim; the attacker's own motion is the client's.
// vanilla: Player.causeExtraKnockback
func (c *client) endSprintAfterKnockback(extra float64) {
	if extra <= 0 {
		return
	}
	c.updateAppearance(func(a *player.Appearance) {
		a.Sprinting = false
		a.Swimming = c.swimmingNow(*a)
	})
}

// playerHeight is the height of the player's body in its pose.
func playerHeight(c *client) float64 {
	return c.dims(c.player.Appearance().Pose).Height
}

// playerWidth is the player's box width by pose and scale.
func playerWidth(c *client) float64 {
	return c.dims(c.player.Appearance().Pose).Width
}

// attackSound plays a blow's sound from the attacker to everyone, attacker
// included. vanilla: Player.playServerSideSound
func (c *client) attackSound(sound registry.Sound) {
	pos := c.player.Position()
	c.srv.broadcastSoundID(c.theWorld(),
		sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)

}

// blowFeedback decides a blow's feedback and plays its pre-hurt part, the
// sprint knockback sound. vanilla: Player.attack
func (c *client) blowFeedback(held protocol.ItemStack, strength float64, critical bool, magicBoost float64) rules.AttackFeedback {
	movement := c.player.KnownMovement()
	var speed float64
	c.player.Attributes(func(a *player.Attributes) { speed = a.Value(registry.AttributeMovementSpeed) })
	fx := rules.Blow{
		Strength:     strength,
		Critical:     critical,
		Sprinting:    c.player.Appearance().Sprinting,
		OnGround:     c.player.Position().OnGround,
		KnownSpeedSq: movement.X*movement.X + movement.Z*movement.Z,
		Speed:        speed,
		Sword:        !held.Empty() && registry.Item(held.ItemID).InTag(registry.ItemTagIDSwords),
		MagicBoost:   magicBoost,
	}.Feedback()
	if fx.Knockback {
		c.attackSound(soundAttackKnockback)
	}
	return fx
}

// landedBlowFeedback plays what follows a landed blow: sweep, crit, strong
// or weak sound, and crit animations to the attacker and their trackers.
// vanilla: Player.doSweepAttack, attackVisualEffects, ServerPlayer.crit, magicCrit
func (c *client) landedBlowFeedback(fx rules.AttackFeedback, targetID int32) {
	w := c.theWorld()
	if fx.Sweep {
		c.attackSound(soundAttackSweep)
		pos := c.player.Position()
		yaw := float64(pos.Yaw) * math.Pi / 180
		dx, dz := -math.Sin(yaw), math.Cos(yaw)
		c.srv.sendParticles(w, protocol.ParticleSweepAttack,
			pos.X+dx, pos.Y+playerHeight(c)/2, pos.Z+dz, float32(dx), 0, float32(dz), 0, 0)
	}
	switch {
	case fx.Critical:
		c.attackSound(soundAttackCrit)
		c.srv.broadcastWorld(w, protocol.IDPlayEntityAnimation,
			entityAnimationPayload(targetID, protocol.AnimationCriticalHit))
	case fx.Sweep:
	case fx.Strong:
		c.attackSound(soundAttackStrong)
	default:
		c.attackSound(soundAttackWeak)
	}
	if fx.Magic {
		c.srv.broadcastWorld(w, protocol.IDPlayEntityAnimation,
			entityAnimationPayload(targetID, protocol.AnimationMagicCriticalHit))
	}
}

// sweepHurt applies a landed blow's sweep to every other living body in
// the victim's grown box. vanilla: Player.doSweepAttack
func (c *client) sweepHurt(w *world.World, held protocol.ItemStack, attackDamage float64, targetID int32,
	vMinX, vMinY, vMinZ, vMaxX, vMaxY, vMaxZ, strength float64) {
	from := c.player.Position()
	var ratio float64
	c.player.Attributes(func(a *player.Attributes) { ratio = a.Value(registry.AttributeSweepingDamageRatio) })
	sweep := rules.SweepDamage(ratio, rules.AttackDamage(attackDamage, false, strength))
	volMin, volMax := rules.SweptVolume(vMinX, vMinY, vMinZ, vMaxX, vMaxY, vMaxZ)

	// Other players: gathered under the read lock, hurt after it, as in the
	// blast's player pass.
	c.srv.mu.RLock()
	var players, inWorld []*client
	for _, v := range c.srv.clients {
		if v == c || v.theWorld() != w {
			continue
		}
		inWorld = append(inWorld, v)
		if v.player.EntityID != targetID {
			players = append(players, v)
		}
	}
	c.srv.mu.RUnlock()
	myTeam, allies := c.alliesAmong(inWorld)
	ctx := c.attackEnchContext(meleeDamageType(held), protocol.EntityTypePlayer, nil)
	cause := &damageCause{entityID: c.player.EntityID, x: from.X, y: from.Y, z: from.Z,
		weapon: held, killer: &rules.Killer{Name: c.DisplayName(), Weapon: held}, shove: true}
	for _, v := range players {
		// vanilla: Player.doSweepAttack skips allies whatever the flag
		if allies[v.player.UUID] {
			continue
		}
		at := v.player.Position()
		bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ := v.Box()
		if !rules.SweepReaches(from.X, from.Y, from.Z, at.X, at.Y, at.Z,
			volMin, volMax,
			[3]float64{bMinX, bMinY, bMinZ}, [3]float64{bMaxX, bMaxY, bMaxZ}) {
			continue
		}
		// Enchantments apply against each swept body's kind, as against the
		// victim's.
		ctx.Body, ctx.BodyOf = nil, victimLoot(v)
		landed, softened := v.hurt(rules.EnchantedDamage(held, &ctx, sweep)*strength,
			ctx.DamageType, cause)
		if !landed {
			continue
		}
		if softened {
			v.knockbackFromPointExtraOnlyBy(w, c.player.UUID, float64(from.Yaw), rules.SweepShove, syncVelocityNow)
		} else {
			v.knockbackFromPointExtraBy(w, c.player.UUID, from.X, from.Y, from.Z, float64(from.Yaw), rules.SweepShove, syncVelocityNow)
		}
		c.srv.postAttack(w, attackBody{player: c}, attackBody{player: v}, held, &ctx, syncVelocityNow)
	}

	c.sweepStands(float64(from.Yaw), func(ent *armorStandEntity) bool {
		bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ := ent.stand.BlastBox()
		return ent.stand.ID != targetID && rules.SweepReaches(from.X, from.Y, from.Z, ent.stand.X, ent.stand.Y, ent.stand.Z,
			volMin, volMax,
			[3]float64{bMinX, bMinY, bMinZ}, [3]float64{bMaxX, bMaxY, bMaxZ})
	})

	// Mobs: the herd walked under the mob lock, hurts taken there and told
	// after, as for the blow's own mob half.
	attacker := entity.Player{
		X: from.X, Y: from.Y, Z: from.Z,
		Yaw:      float64(from.Yaw),
		Pitch:    float64(from.Pitch),
		EntityID: c.player.EntityID,
		Huntable: true,
		Creative: c.player.GameMode() == protocol.GameModeCreative,
	}
	attacker = c.sized(attacker)
	var bodies []*mobHurt
	var kbs []*mobKnockback
	var inks []mobInk
	var thorns []mobThorns
	var pricks []guardianPrick
	var struck []advancement.Entity
	self := c.LootEntity
	c.srv.mobsMu.Lock()
	for m, ent := range c.srv.herdOf(w).book {
		if m.ID == targetID || m.Dead || alliedTo(c.player.UUID, myTeam, allies, m) {
			continue
		}
		bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ := m.BlastBox()
		if !rules.SweepReaches(from.X, from.Y, from.Z, m.X, m.Y, m.Z,
			volMin, volMax,
			[3]float64{bMinX, bMinY, bMinZ}, [3]float64{bMaxX, bMaxY, bMaxZ}) {
			continue
		}
		mobCtx := c.attackEnchContext(meleeDamageType(held), mobEntityType(m.Type), func() *rules.LootEntity { return c.srv.mobLootLocked(ent) })
		// The spikes are read before the blow lands. vanilla: Guardian.hurtServer
		if m.ThornsOut() {
			pricks = append(pricks, guardianPrick{id: m.ID, x: m.X, y: m.Y, z: m.Z, killer: c.srv.mobKiller(ent)})
		}
		oldVX, oldVY, oldVZ, oldImpulse := m.VX, m.VY, m.VZ, m.Impulse
		healthBefore := m.Health
		swept := rules.EnchantedDamage(held, &mobCtx, sweep) * strength
		hurt, ink, laid, _ := landMobHurt(ent, swept,
			attacker, rules.SweepShove, held, mobCtx, nil)
		kb := c.srv.holdMobKnockback(ent, c.player.UUID, oldVX, oldVY, oldVZ, oldImpulse)
		if laid {
			struck = append(struck, advancement.Entity{Type: m.Type.Name(), Loot: c.srv.mobLoot(ent), Blow: &advancement.Blow{
				Dealt: swept, Taken: healthBefore - m.Health, Type: mobCtx.DamageType, Attacker: self, Direct: self}})
			c.awardDamageStat(registry.CustomStatDamageDealtResisted, m.BlowResisted)
			c.awardDamageStat(registry.CustomStatDamageDealtAbsorbed, m.BlowAbsorbed)
			thorns = append(thorns, mobRetaliations(ent, &mobCtx, held))
		}
		if hurt != nil {
			bodies = append(bodies, hurt)
			kbs = append(kbs, kb)
		} else if kb != nil {
			bodies = append(bodies, nil)
			kbs = append(kbs, kb)
		}
		if ink != nil {
			inks = append(inks, mobInk{w: w, kind: m.Type, cloud: ink})
		}
	}
	c.srv.mobsMu.Unlock()
	for _, v := range struck {
		c.hurtEntityAdvancements(v)
	}
	for _, p := range pricks {
		c.srv.guardianThorns(c, p.id, p.x, p.y, p.z, p.killer)
	}
	for i, body := range bodies {
		if kb := kbs[i]; kb != nil {
			vx, vy, vz := c.srv.applyMobKnockback(kb)
			if body != nil {
				body.vx, body.vy, body.vz = vx, vy, vz
			}
		}
		if body != nil {
			c.srv.broadcastMobHurt(w, body)
		}
	}
	for _, ink := range inks {
		c.srv.sendInk(ink.w, ink.kind, ink.cloud)
	}
	for _, t := range thorns {
		c.thornsFromMob(w, t, syncVelocityNow)
	}
}

// alliesAmong returns the attacker's team ("" for none) and the UUIDs of
// the attacker and their teammates among others, whose tamed pets are the
// attacker's allies too.
func (c *client) alliesAmong(others []*client) (string, map[uuid.UUID]bool) {
	myTeam, _ := c.srv.scoreboard.PlayerTeam(c.player.Name)
	allies := map[uuid.UUID]bool{c.player.UUID: true}
	for _, v := range others {
		if t, ok := c.srv.scoreboard.PlayerTeam(v.player.Name); ok && t == myTeam {
			allies[v.player.UUID] = true
		}
	}
	return myTeam, allies
}

// alliedTo reports whether the mob is an ally of the attacking player: on
// their team, tamed by them or by a teammate in allies, or the allay that
// likes them.
// vanilla: Entity.isAlliedTo, TamableAnimal.considersEntityAsAlly, Allay.considersEntityAsAlly
func alliedTo(attacker uuid.UUID, team string, allies map[uuid.UUID]bool, m *entity.Mob) bool {
	return team != "" && m.Team == team ||
		m.Tamable.Tame && allies[m.Tamable.Owner] ||
		m.AllayLikesPlayer(attacker)
}

// mobHurt is a mob's landed, unsoftened hurt as told: body, sound origin,
// shove, health left, and post-attack effect metadata with its watchers.
type mobHurt struct {
	id                  int32
	attackerID          int32
	x, y, z, vx, vy, vz float64
	sound               protocol.SoundHolder
	volume, pitch, tilt float32
	kind                entity.MobType
	silent              bool
	health              []byte
	// ink is a struck squid's ink, nil otherwise.
	ink *inkCloud
	// damageType is the damage event's type, zero for player_attack.
	damageType registry.DamageType
}

// landMobHurt lands a blow on a mob under the mob lock and returns its feedback.
// Squid ink is returned separately even when the hurt window softens the blow.
// vanilla: LivingEntity.hurtServer
func landMobHurt(ent *mobEntity, damage float64, attacker entity.Player, extra float64,
	held protocol.ItemStack, ctx rules.EnchContext, stab *stabBlow, part ...int) (*mobHurt, *inkCloud, bool, bool) {
	m := ent.mob
	var laid, softened bool
	attacker.ArmourEffectiveness = rules.ArmourEffectiveness(held, &ctx)
	if len(part) > 0 {
		m.AimDragonPart(part[0])
		defer m.AimDragonPart(entity.DragonBody)
	}
	if stab != nil {
		laid, softened = m.TakeStabDamage(damage, stab.damageType, attacker)
		if stab.knockback {
			// Applied whether or not the hurt landed. vanilla: Player.stabAttack
			m.KnockbackAlong(attacker.Yaw, rules.StabKnockback)
			if extra > 0 {
				m.KnockbackAlong(attacker.Yaw, extra)
			}
		}
	} else {
		damageType := ctx.DamageType
		if damageType == 0 {
			damageType = rules.DamageTypePlayerAttack
		}
		laid, softened = m.TakeDamageTypedExtra(damage, damageType, attacker, extra)
	}
	if !laid {
		return nil, nil, false, false
	}
	// A softened blow still landed a hurt in vanilla, so its ink still comes
	// out, returned beside the nil hurt for the caller to tell.
	ink := takeInk(m)
	if softened {
		return nil, ink, true, true
	}
	// Record the new health for every watcher, so the metadata pass does not
	// resend what the broadcast below says.
	ent.updateShownMetadata(func(state *mobMetadataState) { state.health = float32(m.Health) })
	sound := mobSound(m, true)
	return &mobHurt{
		id:         m.ID,
		attackerID: attacker.EntityID,
		x:          m.X, y: m.Y, z: m.Z,
		vx: m.VX, vy: m.VY, vz: m.VZ,
		sound:      sound,
		volume:     m.SoundVolume(),
		pitch:      m.VoicePitch(),
		tilt:       mobHurtYaw(attacker, m),
		kind:       m.Type,
		silent:     m.Silent,
		health:     mobHealthPayload(ent),
		damageType: ctx.DamageType,
		ink:        ink,
	}, nil, true, false
}

// broadcastMobHurt tells w of a mob's landed hurt (damage event, hurt
// animation, shove, health, hurt sound). vanilla: Player.attack
func (s *Server) broadcastMobHurt(w *world.World, h *mobHurt) {
	damageType := h.damageType
	if damageType == 0 {
		damageType = rules.DamageTypePlayerAttack
	}
	s.broadcastWorld(w, protocol.IDPlayDamageEvent,
		damageEventPayload(h.id, damageType, h.attackerID+1, h.attackerID+1))
	s.broadcastWorld(w, protocol.IDPlayHurtAnimation,
		hurtAnimationPayload(h.id, h.tilt))
	s.broadcastWorld(w, protocol.IDPlaySetEntityMotion,
		entityVelocityPayload(h.id, h.vx, h.vy, h.vz))
	s.broadcastWorld(w, protocol.IDPlayEntityMetadata, h.health)
	s.broadcastVoice(w, h.kind, h.silent, h.x, h.y, h.z, h.sound, h.volume, h.pitch)
	s.sendInk(w, h.kind, h.ink)
}

// damageDealt counts the health a blow took and raises the damage
// indicator. vanilla: Player.damageStatsAndHearts
func (c *client) damageDealt(w *world.World, x, y, z, dealt float64) {
	c.awardStat(player.Custom(registry.CustomStatDamageDealt), rules.DamageStat(dealt))
	c.srv.damageIndicator(w, x, y, z, dealt)
}

// damageIndicator raises the dark hearts over a body a player's blow took
// dealt health from. vanilla: Player.damageStatsAndHearts
func (s *Server) damageIndicator(w *world.World, x, y, z, dealt float64) {
	if n := rules.DamageIndicatorCount(dealt); n > 0 {
		s.sendParticles(w, protocol.ParticleDamageIndicator, x, y, z, 0.1, 0, 0.1, 0.2, n)
	}
}

type velocitySync uint8

const (
	syncVelocityNow velocitySync = iota
	syncVelocityAfterTravel
	// syncVelocityBeforeTravel holds the shove at full strength until the
	// next travel step, so the sends queued after the hit go out first.
	syncVelocityBeforeTravel
)

// stepVelocity ages the player's motion and sends an entity-tick shove.
func (c *client) stepVelocity(w *world.World, onGround bool) {
	if c.velocityFlushFirst {
		c.velocityFlushFirst = false
		c.sendVelocity(w, c.player.Velocity())
		c.velocityDirty = false
	}
	c.player.StepVelocity(onGround)
	if c.velocityDirty {
		c.velocityDirty = false
		c.sendVelocity(w, c.player.Velocity())
	}
}

// syncVelocity defers entity-tick hits until travel has aged their motion.
// Packet-time hits reach tracking before travel in vanilla, so send those now.
// vanilla: ServerLevel.tick, MinecraftServer.tickChildren, ServerEntity.sendChanges
func (c *client) syncVelocity(w *world.World, v player.Velocity, when velocitySync) {
	switch when {
	case syncVelocityAfterTravel:
		c.velocityDirty = true
		return
	case syncVelocityBeforeTravel:
		c.velocityDirty, c.velocityFlushFirst = true, true
		return
	}
	c.sendVelocity(w, v)
}

// sendVelocity tells this player's client its delta movement.
func (c *client) sendVelocity(w *world.World, v player.Velocity) {
	c.sendIn(w, protocol.IDPlaySetEntityMotion,
		entityVelocityPayload(c.player.EntityID, v.X, v.Y, v.Z))
}

// setVelocity replaces this player's delta movement and tells their client.
func (c *client) setVelocity(w *world.World, vx, vy, vz float64) {
	v := player.Velocity{X: vx, Y: vy, Z: vz}
	c.player.SetVelocity(v)
	c.sendVelocity(w, v)
}

// addVelocity adds (vx, vy, vz) to this player's movement and tells their
// client. vanilla: Entity.push
func (c *client) addVelocity(w *world.World, vx, vy, vz float64) {
	c.player.AddVelocity(vx, vy, vz)
	c.sendVelocity(w, c.player.Velocity())
}

// knockVelocity folds a knockback into this player's movement and tells
// their client. vanilla: LivingEntity.knockback
func (c *client) knockVelocity(w *world.World, vx, vy, vz float64, when velocitySync) {
	c.syncVelocity(w, c.player.Knock(vx, vy, vz, c.player.Position().OnGround), when)
}

// knockbackFromPoint pushes this player away from (x, y, z) in w; only the
// victim's client is told, others see the movement.
func (c *client) knockbackFromPoint(w *world.World, x, y, z float64, when velocitySync) {
	c.knockbackFromPointExtraBy(w, uuid.Nil, x, y, z, 0, 0, when)
}

// knockbackFromPointExtra adds the attack's extra strength along the
// attacker's yaw. The hurt's shove and the extra are two knockbacks, each
// halving the kept velocity, so the victim's old motion is quartered.
// vanilla: LivingEntity.knockback, Player.causeExtraKnockback
func (c *client) knockbackFromPointExtra(w *world.World, x, y, z, yaw, extra float64, when velocitySync) {
	c.knockbackFromPointExtraBy(w, uuid.Nil, x, y, z, yaw, extra, when)
}

func (c *client) knockbackFromPointExtraBy(w *world.World, pusher uuid.UUID, x, y, z, yaw, extra float64, when velocitySync) {
	var scale float64
	c.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
	if scale == 0 {
		return
	}
	to := c.player.Position()
	vx, vy, vz := rules.KnockbackExtra(to.X-x, to.Z-z, 0, 0, scale, to.OnGround, c.srv.levelRng)
	if c.srv.listeningKnockback() {
		old := c.player.Velocity()
		v := player.KnockStep(old, vx, vy, vz, to.OnGround)
		if extra > 0 {
			vx, vy, vz = rules.KnockbackExtraOnly(yaw, extra, scale, to.OnGround)
			v = player.KnockStep(v, vx, vy, vz, to.OnGround)
		}
		c.applyPlayerKnockback(w, pusher, old, v, when)
		return
	}
	v := c.player.Knock(vx, vy, vz, to.OnGround)
	if extra > 0 {
		vx, vy, vz = rules.KnockbackExtraOnly(yaw, extra, scale, to.OnGround)
		v = c.player.Knock(vx, vy, vz, to.OnGround)
	}
	c.syncVelocity(w, v, when)
}

// knockbackFromPointExtraOnly pushes with only the attacker's extra shove,
// what a window-only hit still deals after skipping default knockback.
func (c *client) knockbackFromPointExtraOnly(w *world.World, yaw, extra float64, when velocitySync) {
	c.knockbackFromPointExtraOnlyBy(w, uuid.Nil, yaw, extra, when)
}

func (c *client) knockbackFromPointExtraOnlyBy(w *world.World, pusher uuid.UUID, yaw, extra float64, when velocitySync) {
	var scale float64
	c.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
	if scale == 0 {
		return
	}
	onGround := c.player.Position().OnGround
	vx, vy, vz := rules.KnockbackExtraOnly(yaw, extra, scale, onGround)
	if c.srv.listeningKnockback() {
		old := c.player.Velocity()
		v := player.KnockStep(old, vx, vy, vz, onGround)
		c.applyPlayerKnockback(w, pusher, old, v, when)
		return
	}
	c.knockVelocity(w, vx, vy, vz, when)
}

// arrowKnockback pushes this player along the shaft's flight: the default
// shove for a whole hit and Punch's extra on every landed hit, each scaled
// by knockback resistance. A window-only hit skips the default but keeps
// Punch, per vanilla's tookFullDamage gate.
// vanilla: Projectile.calculateHorizontalHurtKnockbackDirection, LivingEntity.hurtServer
func (c *client) arrowKnockback(w *world.World, flightVX, flightVZ, punch float64, full bool, when velocitySync) {
	c.arrowKnockbackBy(w, uuid.Nil, flightVX, flightVZ, punch, full, when)
}

func (c *client) arrowKnockbackBy(w *world.World, pusher uuid.UUID, flightVX, flightVZ, punch float64, full bool, when velocitySync) {
	var scale float64
	c.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
	if scale == 0 {
		return
	}
	px, py, pz := rules.ArrowPunch(flightVX, flightVZ, punch, scale)
	var kx, ky, kz float64
	onGround := c.player.Position().OnGround
	if full {
		kx, ky, kz = rules.ArrowDefaultKnockback(flightVX, flightVZ, scale, onGround, c.srv.levelRng)
	}
	if kx == 0 && ky == 0 && kz == 0 && px == 0 && py == 0 && pz == 0 {
		return
	}
	if c.srv.listeningKnockback() {
		old := c.player.Velocity()
		v := old
		if full {
			v = player.KnockStep(v, kx, ky, kz, onGround)
		}
		v.X += px
		v.Y += py
		v.Z += pz
		c.applyPlayerKnockback(w, pusher, old, v, when)
		return
	}
	// The default shove is a knockback, Punch a push on top.
	// vanilla: AbstractArrow.doKnockback
	if full {
		c.player.Knock(kx, ky, kz, onGround)
	}
	c.player.AddVelocity(px, py, pz)
	c.syncVelocity(w, c.player.Velocity(), when)
}

func (c *client) applyPlayerKnockback(w *world.World, pusher uuid.UUID, old, target player.Velocity, when velocitySync) {
	dx, dy, dz := target.X-old.X, target.Y-old.Y, target.Z-old.Z
	if dx == 0 && dy == 0 && dz == 0 {
		return
	}
	kx, ky, kz, ok := c.srv.knockbackEvent(c.pluginSelf(), pusher, dx, dy, dz)
	if !ok {
		return
	}
	nv := player.Velocity{X: old.X + kx, Y: old.Y + ky, Z: old.Z + kz}
	c.player.SetVelocity(nv)
	c.syncVelocity(w, nv, when)
}

// igniteForTicks raises fire ticks to at least ticks (scaled by
// burning_time) and thaws the body; an invulnerable mode holds the count at
// one. vanilla: LivingEntity.igniteForTicks
func (c *client) igniteForTicks(ticks int32) {
	c.igniteForTicksBy(ticks, nil)
}

// handleClientCommand handles Client Command: a respawn (after death or the
// end credits) or a gamemaster's game rule values request.
func (c *client) handleClientCommand(r *protocol.Reader) error {
	action := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("client_command", err)
	}
	// An action past the vanilla enum fails its decode.
	// vanilla: FriendlyByteBuf.readEnum
	if action < 0 || action > protocol.ClientCommandRequestGameRuleValues {
		return c.decodeFailure("client_command",
			fmt.Errorf("client command carries action %d, which is not an action", action))
	}
	switch action {
	case protocol.ClientCommandPerformRespawn:
		// The credits' PERFORM_RESPAWN takes the living player home; one from a
		// body that died racing the credits takes the death path. The credits end
		// either way. vanilla: ServerGamePacketListenerImpl.handleClientCommand
		if c.wonGame.Load() && !c.player.Vitals().Dead {
			return c.respawnFromCredits()
		}
		c.wonGame.Store(false)
		return c.respawn()
	case protocol.ClientCommandRequestStats:
		c.sendStats()
	case protocol.ClientCommandRequestGameRuleValues:
		c.sendGameRuleValues()
	}
	return nil
}

// setHealthPayload builds the Set Health packet.
func setHealthPayload(v player.Vitals) []byte {
	var b protocol.Buffer
	b.PutF32(float32(v.Health))
	b.PutVarInt(v.Food)
	b.PutF32(float32(v.Saturation))
	return b.Bytes()
}

// sentHealth is the last health, food and saturation-zero state sent to the
// client; the zero value means nothing was sent yet.
// vanilla: ServerPlayer.lastSentHealth, lastSentFood, lastFoodSaturationZero
type sentHealth struct {
	mu      sync.Mutex
	set     bool
	health  float32
	food    int32
	satZero bool
}

func (s *sentHealth) record(v player.Vitals) {
	s.mu.Lock()
	s.set, s.health, s.food, s.satZero = true, float32(v.Health), v.Food, v.Saturation == 0
	s.mu.Unlock()
}

// differs reports whether v needs a Set Health: health or food changed, or
// saturation crossed zero. vanilla: ServerPlayer.doTick
func (s *sentHealth) differs(v player.Vitals) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.set || float32(v.Health) != s.health || v.Food != s.food || (v.Saturation == 0) != s.satZero
}

// sendHealth queues Set Health for v and records it as sent.
func (c *client) sendHealth(v player.Vitals) {
	c.lastHealth.record(v)
	c.send(protocol.IDPlaySetHealth, setHealthPayload(v))
}

// writeHealth writes this client's Set Health directly.
func (c *client) writeHealth() error {
	v := c.player.Vitals()
	c.lastHealth.record(v)
	return c.writePacket(protocol.IDPlaySetHealth, "set health", setHealthPayload(v))
}

// setExperiencePayload builds the Set Experience packet.
func setExperiencePayload(v player.Vitals) []byte {
	var b protocol.Buffer
	b.PutF32(v.ExperienceProgress)
	b.PutVarInt(int32(v.ExperienceLevel))
	b.PutVarInt(int32(v.ExperienceTotal))
	return b.Bytes()
}

// sendExperience sends this client its experience, on join and respawn.
func (c *client) sendExperience() error {
	return c.writePacket(protocol.IDPlaySetExperience, "set experience", setExperiencePayload(c.player.Vitals()))
}

// damageEventPayload builds Damage Event. causeID is who is responsible
// (an arrow's shooter), directID what carried the blow (the arrow); both
// entity ID + 1, 0 for none.
func damageEventPayload(victimID int32, damageType registry.DamageType, causeID, directID int32) []byte {
	return damageEventPayloadAt(victimID, damageType, causeID, directID, nil)
}

// damageEventPos is a cause's explicit source position, nil for none.
func damageEventPos(cause *damageCause) *[3]float64 {
	if cause == nil || !cause.eventPos {
		return nil
	}
	return &[3]float64{cause.x, cause.y, cause.z}
}

// damageEventPayloadAt is damageEventPayload with the optional source
// position, set only by /damage's at clause.
func damageEventPayloadAt(victimID int32, damageType registry.DamageType, causeID, directID int32, at *[3]float64) []byte {
	var b protocol.Buffer
	b.PutVarInt(victimID)
	b.PutVarInt(int32(damageType) - 1)
	b.PutVarInt(causeID)  // the entity responsible
	b.PutVarInt(directID) // the one that directly dealt it
	b.PutBool(at != nil)
	if at != nil {
		b.PutF64(at[0])
		b.PutF64(at[1])
		b.PutF64(at[2])
	}
	return b.Bytes()
}

// hurtAnimationPayload builds the Hurt Animation packet.
func hurtAnimationPayload(victimID int32, yaw float32) []byte {
	var b protocol.Buffer
	b.PutVarInt(victimID)
	b.PutF32(yaw)
	return b.Bytes()
}

// hurtYaw is the direction of the blow relative to the victim's facing, in
// degrees, for Hurt Animation. vanilla: ServerPlayer.indicateDamage
func hurtYaw(victim *client, cause *damageCause) float32 {
	var xd, zd float64
	switch {
	case cause == nil:
	case cause.flying:
		xd, zd = -cause.flightVX, -cause.flightVZ
	default:
		to := victim.player.Position()
		xd, zd = cause.x-to.X, cause.z-to.Z
	}
	return float32(math.Atan2(zd, xd)*57.2957763671875 - float64(victim.player.Position().Yaw))
}

// entityEventPayload builds Entity Event; the entity ID is an Int here, not
// a VarInt.
func entityEventPayload(entityID int32, status uint8) []byte {
	var b protocol.Buffer
	b.PutI32(entityID)
	b.PutU8(status)
	return b.Bytes()
}

// combatDeathPayload builds the Combat Death packet (the death screen).
func combatDeathPayload(entityID int32, message protocol.Text) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutText(message)
	return b.Bytes()
}

// systemChatPayload builds the System Chat packet.
func systemChatPayload(message protocol.Text, overlay bool) []byte {
	var b protocol.Buffer
	b.PutText(message)
	b.PutBool(overlay)
	return b.Bytes()
}

// entityVelocityPayload builds Set Entity Velocity, in blocks per tick.
func entityVelocityPayload(entityID int32, vx, vy, vz float64) []byte {
	var b protocol.Buffer
	// The ID, then an LpVec3 of six bytes and a scale of at most five.
	b.Grow(5 + 6 + 5)
	b.PutVarInt(entityID)
	b.PutLpVec3(vx, vy, vz)
	return b.Bytes()
}

// gameEventPayload builds Game Event; value's meaning depends on the event.
func gameEventPayload(event uint8, value float32) []byte {
	var b protocol.Buffer
	b.PutU8(event)
	b.PutF32(value)
	return b.Bytes()
}

// entityClient returns the client with this player entity ID, or nil.
func (s *Server) entityClient(entityID int32) *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.player.EntityID == entityID {
			return c
		}
	}
	return nil
}

// abilityFlags returns the Player Abilities flags the game mode grants, as a
// plugin's flight grant or revocation adjusts them.
func (c *client) abilityFlags() int8 {
	mayFly := c.abilityState().mayFly
	switch c.player.GameMode() {
	case protocol.GameModeCreative:
		flags := int8(protocol.AbilityInvulnerable | protocol.AbilityAllowFlying | protocol.AbilityCreative)
		if mayFly < 0 {
			flags &^= protocol.AbilityAllowFlying
		} else if c.flying.Load() {
			flags |= protocol.AbilityFlying
		}
		return flags
	case protocol.GameModeSpectator:
		return int8(protocol.AbilityInvulnerable | protocol.AbilityAllowFlying | protocol.AbilityFlying)
	default: // survival, adventure
		if mayFly > 0 {
			flags := int8(protocol.AbilityAllowFlying)
			if c.flying.Load() {
				flags |= protocol.AbilityFlying
			}
			return flags
		}
		return 0
	}
}

// abilitiesPayload builds the Player Abilities packet.
func (c *client) abilitiesPayload() []byte {
	var b protocol.Buffer
	b.PutI8(c.abilityFlags())
	st := c.abilityState()
	b.PutF32(st.flySpeed)
	b.PutF32(st.walkSpeed) // field of view modifier
	return b.Bytes()
}

// gameModeNames is indexed by protocol game mode; package command keeps its
// own copy for /gamemode.
var gameModeNames = []string{
	"survival",
	"creative",
	"adventure",
	"spectator",
}

// configGameMode maps a server.properties gamemode to its protocol number,
// defaulting to survival (Load already validated it).
func configGameMode(name string) int32 {
	for mode, candidate := range gameModeNames {
		if strings.EqualFold(candidate, name) {
			return int32(mode)
		}
	}
	return protocol.GameModeSurvival
}

// stepOnRedstoneOre lights redstone ore a standing, unsneaking body rests
// on. vanilla: RedStoneOreBlock.stepOn, Entity.applyEffectsFromBlocks
func (c *client) stepOnRedstoneOre(pos player.Position, around rules.Habitat) {
	// A spectator is never on the ground.
	// vanilla: Player.tick, Entity.applyEffectsFromBlocks
	if c.player.GameMode() == protocol.GameModeSpectator || !around.OnGround || around.Sneaking || !rules.IsRedstoneOre(around.Ground) {
		return
	}
	dims := c.dims(c.player.Appearance().Pose)
	x, y, z, _ := rules.OnPosCell(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width/2)
	c.lightRedstoneOre(x, y, z)
}

// stepEvent plays a step or swim sound and posts its game event when the
// walk distance crosses the next step, for the block under the player's feet.
// The sounds go to everyone near but the walker, whose client plays its own.
// The move's delta stands in for the velocity the swim sound's volume reads.
// vanilla: Entity.applyMovementEmissionAndPlaySound, Player.playSound
func (c *client) stepEvent(was, now player.Position, look player.Appearance, inWater bool) {
	// A flying body and one sneaking on the ground emit nothing and add no
	// walk distance. vanilla: Player.getMovementEmission
	if c.flying.Load() || now.OnGround && look.Sneaking {
		return
	}
	w := c.theWorld()
	crouching := look.Pose == protocol.PoseSneaking
	gx, gy, gz, ground := rules.OnPosCell(w, now.X, now.Y, now.Z, c.dims(look.Pose).Width/2)
	dx, dy, dz := now.X-was.X, now.Y-was.Y, now.Z-was.Z
	stepped, swam := c.steps.Advance(rules.Stride{
		DX: dx, DY: dy, DZ: dz,
		Climbing: rules.IsClimbableSupport(ground), InWater: inWater, SupportAir: ground == 0,
		OnGround: now.OnGround, Crouching: crouching, Swimming: look.Swimming,
	})
	play := func(snd rules.StepSound) {
		if snd.Event != registry.SoundNone {
			c.srv.broadcastSoundIDExcept(w, c, snd.Event, protocol.SoundCategoryPlayer, now.X, now.Y, now.Z, snd.Volume, snd.Pitch)
		}
	}
	swim := func() {
		play(rules.StepSound{Event: registry.SoundEntityPlayerSwim, Volume: rules.SwimSoundVolume(dx, dy, dz),
			Pitch: rules.SwimSoundPitch(rand.Float32(), rand.Float32())})
	}
	switch {
	case stepped:
		// vanilla: Entity.walkingStepSound
		step, muffled := rules.PlayerStepSounds(ground, w.BlockAt(gx, gy+1, gz), inWater)
		if inWater {
			swim()
		}
		play(step)
		play(muffled)
		if chime, ok := c.crystal.Chime(ground, int64(c.srv.ticksRun.Load()), rand.Float32); ok {
			play(chime)
		}
		c.srv.gameEvent(w, rules.GameEventStep, now.X, now.Y, now.Z, c.gameEventContext(ground))
	case swam:
		swim()
		c.srv.gameEvent(w, rules.GameEventSwim, now.X, now.Y, now.Z, c.gameEventContext(ground))
	}
}

// splashEvent plays the splash of a body entering water and posts its game
// event, on a move that takes it from dry into water; the first move after
// a join or respawn only reads the water, and a spectator makes no splash.
// The move's delta stands in for the velocity the volume reads.
// vanilla: Entity.updateFluidInteraction, doWaterSplashEffect, Player.doWaterSplashEffect
func (c *client) splashEvent(was, now player.Position, inWater bool) {
	entered := inWater && !c.wasInWater && c.waterSeen
	c.wasInWater, c.waterSeen = inWater, true
	if !entered || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	w := c.theWorld()
	event, volume := rules.PlayerSplashSound(now.X-was.X, now.Y-was.Y, now.Z-was.Z)
	c.srv.broadcastSoundIDExcept(w, c, event, protocol.SoundCategoryPlayer, now.X, now.Y, now.Z,
		volume, rules.SwimSoundPitch(rand.Float32(), rand.Float32()))
	c.srv.gameEvent(w, rules.GameEventSplash, now.X, now.Y, now.Z, c.gameEventContext(0))
}

// playHurtSound plays a player's hurt or death sound at the body for everyone
// in earshot but the victim, whose client plays its own.
// vanilla: Player.playSound, Player.getHurtSound
func (c *client) playHurtSound(sound registry.Sound) {
	pos := c.player.Position()
	c.srv.broadcastSoundIDExcept(c.theWorld(), c, sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 1, 1)
}

// smashDust raises the block dust of a landing after a smash on a grounded
// victim, once. vanilla: ServerPlayer.checkFallDamage
func (c *client) smashDust(ground uint16, now player.Position, fall float64) {
	pending := false
	c.player.UpdateVitals(func(v *player.Vitals) {
		pending = v.SpawnExtraParticlesOnFall
		v.SpawnExtraParticlesOnFall = false
	})
	if !pending {
		return
	}
	w := c.theWorld()
	bx, by, bz := rules.StandingBlock(w, now.X, now.Y, now.Z, playerWidth(c)/2)
	c.srv.sendBlockParticles(w, protocol.ParticleBlock, ground,
		float64(bx)+0.5, float64(by)+1.0, float64(bz)+0.5, 0.3, 0.3, 0.3, 0.15,
		int32(math.Min(math.Max(50*fall, 0), 200)))
}
