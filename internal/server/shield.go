package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The raised shield. Use Item raises it, release or a held-item swap lowers
// it, and its effect on a hurt is package rules' (rules/shield.go); this
// file keeps the state, applies the results (wear, an axe's disable) and
// sends the packets.

// raiseSession is the raised shield: hand and item (so a mid-raise swap
// lowers it), the item's blocks_attacks as it was raised, and ticks since
// the raise, against the block delay. raised is false with no shield up.
type raiseSession struct {
	hand   int32
	item   int32
	group  string // the raised stack's cooldown group, which a disable starts
	blocks registry.BlocksAttacks
	ticks  int32 // elapsed ticks since the raise, 0 on the raising tick
	raised bool
}

// c.cooldowns is the player's ItemCooldowns: the ticks left, by group (see
// registry.CooldownGroup). vanilla: ItemCooldowns

// handleUseShield raises a blocks_attacks item in the hand, reporting
// whether it did. A meal, bow draw or existing use refuses it, as does an
// axe's cooldown. vanilla: Item.use, ServerGamePacketListenerImpl.handleUseItem
func (c *client) handleUseShield(hand int32) bool {
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	blocks, ok := registry.StackBlocksAttacks(held)
	if held.Empty() || !ok {
		return false
	}
	c.eatMu.Lock()
	eating := c.eat.ticks > 0
	c.eatMu.Unlock()
	if eating {
		return false
	}
	c.bowMu.Lock()
	drawing := c.bow.ticks > 0
	c.bowMu.Unlock()
	if drawing {
		return false
	}
	c.shieldMu.Lock()
	if c.raise.raised || c.itemCooldownTicks(held) > 0 {
		c.shieldMu.Unlock()
		return false
	}
	c.raise = raiseSession{hand: hand, item: held.ItemID, group: registry.CooldownGroup(held), blocks: blocks, raised: true}
	c.shieldMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	return true
}

// releaseShield lowers the shield, ending the use.
// vanilla: LivingEntity.stopUsingItem
func (c *client) releaseShield() {
	c.shieldMu.Lock()
	if !c.raise.raised {
		c.shieldMu.Unlock()
		return
	}
	c.raise = raiseSession{}
	c.shieldMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
}

// tickShield advances the raise and cooldowns: the raise ends on death,
// spectator, or the item leaving the hand; an expired cooldown is cleared on
// the wire. vanilla: LivingEntity.tick, Player.tick, ItemCooldowns.tick
func (c *client) tickShield() {
	v := c.player.Vitals()
	c.shieldMu.Lock()
	before := c.raise
	if c.raise.raised {
		if v.Dead || v.GameMode == protocol.GameModeSpectator {
			c.raise = raiseSession{}
		} else {
			var held int32
			c.player.Inventory(func(inv *player.Inventory) {
				held = inv.Slots[rules.SlotOfHand(inv, c.raise.hand)].ItemID
			})
			if held != c.raise.item {
				c.raise = raiseSession{}
			} else {
				c.raise.ticks++
			}
		}
	}
	after := c.raise
	var ended []string
	for group, ticks := range c.cooldowns {
		if ticks <= 1 {
			delete(c.cooldowns, group)
			if ticks == 1 {
				ended = append(ended, group)
			}
			continue
		}
		c.cooldowns[group] = ticks - 1
	}
	c.shieldMu.Unlock()
	if before.raised && !after.raised {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	}
	for _, group := range ended {
		c.send(protocol.IDPlayCooldown, newCooldownPayload(group, 0))
	}
}

// itemCooldownTicks is the ticks left on the stack's cooldown group, 0 for
// none. shieldMu is held.
func (c *client) itemCooldownTicks(stack protocol.ItemStack) int32 {
	return c.cooldowns[registry.CooldownGroup(stack)]
}

// startItemCooldown starts (or restarts) a cooldown group and sends Set
// Cooldown; the clear follows when it expires.
// vanilla: ItemCooldowns.addCooldown
func (c *client) startItemCooldown(group string, ticks int32) {
	if group == "" {
		return
	}
	c.shieldMu.Lock()
	if c.cooldowns == nil {
		c.cooldowns = make(map[string]int32)
	}
	c.cooldowns[group] = ticks
	c.shieldMu.Unlock()
	c.send(protocol.IDPlayCooldown, newCooldownPayload(group, ticks))
}

// startUseCooldown starts the use_cooldown a finished use of the stack
// carries, if any. vanilla: ItemStack.applyAfterUseComponentSideEffects
func (c *client) startUseCooldown(stack protocol.ItemStack) {
	if ticks, group, ok := registry.StackUseCooldown(stack); ok {
		c.startItemCooldown(group, ticks)
	}
}

// shieldBlocks returns how much of a hurt the raised item blocks, and
// applies the cost: wear, and the disable from a shield-disabling weapon. A
// zero hurt blocks nothing, and a blow that broke the item disables nothing.
// The sound is the item's block_sound, to play once the hurt lands.
// vanilla: LivingEntity.applyItemBlocking, BlocksAttacks.hurtBlockingItem,
// Player.blockUsingItem
func (c *client) shieldBlocks(damageType registry.DamageType, cause *damageCause, damage float64) (float64, *protocol.SoundHolder) {
	if damage <= 0 || (cause != nil && cause.piercing) {
		return 0, nil
	}
	c.shieldMu.Lock()
	raise := c.raise
	c.shieldMu.Unlock()
	if !raise.raised {
		return 0, nil
	}
	// The block reads the use's starting item; the tick lowers a swapped one,
	// but a hurt may come first.
	if !c.holdsRaisedItem(raise) {
		return 0, nil
	}
	blocks := &raise.blocks
	if !rules.BlockEngages(blocks, raise.ticks, damageType) {
		return 0, nil
	}
	pos := c.player.Position()
	angle := rules.BlockNoSourceAngle
	if cause != nil {
		angle = rules.BlockAngle(float64(pos.Yaw), pos.X, pos.Z, cause.x, cause.z)
	}
	blocked := rules.ResolveBlockedDamage(blocks, damageType, angle, damage)
	// Every hit it engages counts a use, even one from behind that blocks
	// nothing. vanilla: BlocksAttacks.hurtBlockingItem
	c.awardItemStat(registry.StatTypeUsed, raise.item, 1)
	if wear := rules.BlockItemDamage(blocks, blocked); wear > 0 {
		c.wearHeld(raise.hand, wear)
	}
	if blocked <= 0 {
		return blocked, nil
	}
	if cause != nil && cause.shove && blocked < damage {
		c.blockShove(cause.x, cause.z)
	}
	// A break empties the stack, leaving nothing to disable; only a
	// disabling weapon's blow on a surviving item disables it.
	// vanilla: Player.blockUsingItem
	if cause != nil && cause.disableSeconds > 0 && c.holdsRaisedItem(raise) {
		if ticks := rules.BlockDisableTicks(blocks, cause.disableSeconds); ticks > 0 {
			c.disableShield(ticks, raise.group, blocks.DisableSound)
		}
	}
	return blocked, blocks.BlockSound
}

// blockShoveStrength is the knockback strength of a partial block's shove.
// vanilla: LivingEntity.blockedByItem
const blockShoveStrength = 0.5

// blockShove folds a partial block's shove toward the attacker at (x, z)
// into the holder's movement; the hit's own knockback, which follows, sends
// both. vanilla's knockback is given the defender minus the attacker and
// negates it, so the holder is pulled toward the blow, not away.
// vanilla: LivingEntity.blockedByItem, LivingEntity.knockback
func (c *client) blockShove(x, z float64) {
	var scale float64
	c.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
	if scale == 0 {
		return
	}
	at := c.player.Position()
	ux, uz := rules.KnockbackDirection(x-at.X, z-at.Z, c.srv.levelRng)
	strength := blockShoveStrength * scale
	var vy float64
	if at.OnGround {
		vy = strength
	}
	c.player.Knock(ux*strength, vy, uz*strength, at.OnGround)
}

// shovesOnBlock reports whether a mob's partly blocked blow takes the
// default shove; a hoglin, zoglin or ravager answers with its own.
// vanilla: Hoglin.blockedByItem, Zoglin.blockedByItem, Ravager.blockedByItem
func shovesOnBlock(t registry.EntityType) bool {
	return t != registry.EntityTypeHoglin && t != registry.EntityTypeZoglin && t != registry.EntityTypeRavager
}

// holdsRaisedItem reports whether the raised item is still in its hand,
// which a breaking blow empties. vanilla: LivingEntity.getItemBlockingWith
func (c *client) holdsRaisedItem(raise raiseSession) bool {
	var held int32
	c.player.Inventory(func(inv *player.Inventory) {
		held = inv.Slots[rules.SlotOfHand(inv, raise.hand)].ItemID
	})
	return held == raise.item
}

// disableShield puts a raised item away for a cooldown: the use ends, the
// disable sound plays, the cooldown starts and Set Cooldown tells the client,
// sent again with 0 when it expires. vanilla: BlocksAttacks.disable
func (c *client) disableShield(ticks int32, group string, sound *protocol.SoundHolder) {
	c.shieldMu.Lock()
	c.raise = raiseSession{}
	c.shieldMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	c.startItemCooldown(group, ticks)
	if sound == nil {
		return
	}
	pos := c.player.Position()
	c.srv.broadcastSoundHolder(c.theWorld(),
		*sound, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 0.8, 0.8+c.srv.soundRng.Float32()*0.4)
}

// playShieldBlock plays a blocked blow's sound at the holder, to all; an
// item with no block_sound plays none. vanilla: BlocksAttacks.onBlocked
func (c *client) playShieldBlock(sound *protocol.SoundHolder) {
	if sound == nil {
		return
	}
	pos := c.player.Position()
	c.srv.broadcastSoundHolder(c.theWorld(),
		*sound, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 1, 0.8+c.srv.soundRng.Float32()*0.4)
}

// newCooldownPayload builds Set Cooldown for a group: the ticks, 0 to clear.
func newCooldownPayload(group string, ticks int32) []byte {
	var b protocol.Buffer
	b.PutString(group)
	b.PutVarInt(ticks)
	return b.Bytes()
}

// disableSeconds is the seconds a melee blow with the held stack disables a
// blocking item for: its weapon's disable_blocking_for_seconds.
// vanilla: LivingEntity.getSecondsToDisableBlocking
func disableSeconds(held protocol.ItemStack) float32 {
	w, _ := registry.StackWeapon(held)
	return w.DisableBlockingSeconds
}

// livingDisableSeconds is the seconds a living attacker's melee blow disables
// a blocking item for: 5 for a warden, else its main hand's.
// vanilla: LivingEntity.getSecondsToDisableBlocking, Warden.getSecondsToDisableBlocking
func livingDisableSeconds(kind registry.EntityType, mainHand protocol.ItemStack) float32 {
	if kind == registry.EntityTypeWarden {
		return 5
	}
	return disableSeconds(mainHand)
}
