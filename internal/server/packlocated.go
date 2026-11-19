package server

import (
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// locatedState is a player's record of the location_changed and tick
// components: the block and equipment the last walk saw, and the entries
// active per slot. The tick loop and a step's survival side both walk, so mu
// guards it.
type locatedState struct {
	mu     sync.Mutex
	seen   bool
	block  [3]int32
	worn   player.Equipment
	active rules.LocatedState
}

// tickPackLocated walks the worn enchantments each tick, running their tick
// entries too.
// vanilla: EnchantmentHelper.tickEffects
func (c *client) tickPackLocated() { c.walkLocated(false, true) }

// walkLocated runs the location_changed entries of the worn stacks when the
// player's block changed, on a landing and for a slot whose stack changed, and
// with tick the tick entries as well. A step's survival side walks as the move
// lands, ahead of the tick.
// vanilla: LivingEntity.baseTick, checkFallDamage, collectEquipmentChanges,
// EnchantmentHelper.runLocationChangedEffects, stopLocationBasedEffects, tickEffects
func (c *client) walkLocated(landed, tick bool) {
	st := &c.located
	pos := c.player.Position()
	block := [3]int32{rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)}
	st.mu.Lock()
	// The equipment is copied only when it matters, as a player in no located
	// enchantment is the common case.
	enchanted := !st.active.Empty()
	var now player.Equipment
	c.player.Inventory(func(inv *player.Inventory) {
		if enchanted || inv.HasEquipment(rules.HasLocated) {
			enchanted, now = true, inv.Equipment()
		}
	})
	if !enchanted {
		st.seen, st.block, st.worn = false, block, player.Equipment{}
		st.mu.Unlock()
		return
	}
	// A spectator's block changes run nothing, its equipment changes do.
	// vanilla: ServerPlayer.onChangedBlock
	moved := (!st.seen || st.block != block || landed) && c.player.GameMode() != protocol.GameModeSpectator
	worn := st.worn
	st.seen, st.block, st.worn = true, block, now
	var changed [len(player.EquipmentSlots)]bool
	anyDue := moved || tick
	for i := range now {
		changed[i] = !now[i].Equal(worn[i])
		anyDue = anyDue || changed[i]
	}
	if !anyDue {
		st.mu.Unlock()
		return
	}

	// The snapshot reads the player's position, so it is built outside the
	// attributes lock.
	ctx := c.itemEnchContext()
	ctx.World = c.theWorld()
	ctx.BodyOf = func() *rules.LootEntity { return c.lootBody(pos) }
	var apply, remove, ticked []rules.LocatedAction
	for i, slot := range player.EquipmentSlots {
		if moved || changed[i] {
			a, r := st.active.Step(slot, now[i], ctx, changed[i])
			apply, remove = append(apply, a...), append(remove, r...)
		}
		if tick {
			ticked = append(ticked, rules.LocatedEffects(now[i], slot, true, ctx, ctx.Rand)...)
		}
	}
	st.mu.Unlock()
	if len(apply)+len(remove) > 0 {
		c.player.Attributes(func(a *player.Attributes) { entity.ApplyLocatedMods(a, apply, remove) })
	}
	w := c.theWorld()
	movement := c.player.KnownMovement()
	target := enchantTarget{uuid: c.player.UUID, player: true, x: pos.X, y: pos.Y, z: pos.Z, width: playerWidth(c), height: playerHeight(c),
		vx: movement.X, vy: movement.Y, vz: movement.Z}
	for _, act := range append(apply, ticked...) {
		c.applyLocatedAction(w, act, ctx, target)
	}
}

// applyLocatedAction runs the entity effects of one entry that holds: mob
// effects, the data-pack-only effects, the sound and the wear on the slot's
// stack.
// vanilla: EnchantmentEntityEffect.apply
func (c *client) applyLocatedAction(w *world.World, act rules.LocatedAction, ctx *rules.EnchContext, target enchantTarget) {
	e := act.Entry.Effect
	for _, in := range act.MobEffects(ctx.Rand) {
		c.AddEffect(in)
	}
	c.srv.applyPackEffects(w, e, act.Level, target)
	if ps := e.PlaySound; ps != nil {
		sd := resolveLocatedSound(ps, act.Level, ctx.Rand)
		c.srv.broadcastSoundHolder(w, sd.sound, protocol.SoundCategoryPlayer, target.x, target.y, target.z, sd.volume, sd.pitch)
	}
	if e.ChangeItemDamage != nil {
		piece := slices.Index(player.EquipmentSlots[:], act.Slot)
		c.srv.wearPostAttack(w, attackBody{player: c}, piece, int32(rules.EnchLevelValueAt(e.ChangeItemDamage, act.Level)))
	}
}

// lootBody is the snapshot of the player the entity_properties conditions
// of its worn enchantments read.
func (c *client) lootBody(pos player.Position) *rules.LootEntity {
	movement := c.player.KnownMovement()
	return bodyLootEntity(c.theWorld(), "minecraft:player", pos.X, pos.Y, pos.Z, c.dims(c.player.Appearance().Pose).Width/2,
		rules.LootEntityFlags{OnGround: pos.OnGround, Flying: c.flying.Load() || c.fallFlying.Load(), FallFlying: c.fallFlying.Load()},
		int64(c.player.Vitals().AgeTicks), [3]float64{movement.X, movement.Y, movement.Z})
}

// bodyLootEntity is the frozen snapshot of a body at (x, y, z) that a located
// enchantment's entity_properties conditions read; age is its tick count and
// movement its known movement per tick.
func bodyLootEntity(w rules.BlockAt, kind string, x, y, z, halfWidth float64, flags rules.LootEntityFlags, age int64, movement [3]float64) *rules.LootEntity {
	return new(lootBody).fill(w, kind, x, y, z, halfWidth, flags, age, movement)
}

// lootBody holds a snapshot and what it points at in one allocation, so a hot
// walk can take one from lootBodies and return it after the run.
type lootBody struct {
	ent   rules.LootEntity
	place rules.LootPlace
	facts rules.LootFacts
}

var lootBodies = sync.Pool{New: func() any { return new(lootBody) }}

// fill makes b the snapshot bodyLootEntity describes.
func (b *lootBody) fill(w rules.BlockAt, kind string, x, y, z, halfWidth float64, flags rules.LootEntityFlags, age int64, movement [3]float64) *rules.LootEntity {
	bx, by, bz := rules.StandingBlock(w, x, y, z, halfWidth)
	b.facts = rules.LootFacts{Flags: flags, TickCount: age, Movement: movement}
	b.place = rules.LootPlace{X: x, Y: y, Z: z,
		OnPos:        [3]int32{rules.BlockOf(x), int32(math.Floor(y - 1e-5)), rules.BlockOf(z)},
		AffectingPos: [3]int32{bx, by, bz}, OnGround: flags.OnGround}
	b.ent = rules.LootEntity{Type: kind, Facts: func() rules.LootFacts { return b.facts }, Place: &b.place}
	return &b.ent
}

// boots is the stack in the player's feet slot.
func (c *client) boots() protocol.ItemStack {
	var boots protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		boots = inv.Slots[player.ArmorSlot+3]
	})
	return boots
}
