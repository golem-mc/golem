package rules

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// Enchantment effects are evaluated from each minecraft:enchantment entry's
// "effects" map: components (hit damage, armour protection, spared wear) of
// level-based values, each gated by a loot-predicate condition.
// vanilla: EnchantmentHelper
//
// EnchContext is what the conditions ask about, shaped like vanilla's damage
// context: damage type, directness, the hit's three bodies by entity type ID,
// and the stack match_tool tests, empty wherever vanilla's context has no
// tool, so match_tool refuses outside item contexts. An unset body is -1 and
// matches nothing.
type EnchContext struct {
	Thundering, CanSeeSky bool
	Block                 registry.Block
	DamageType            registry.DamageType
	// Direct is vanilla's DamageSource.isDirect: whether the blow's cause
	// and its carrier are one - a swung weapon, and the environment's
	// blows with nobody behind them, but not an arrow its shooter shot.
	Direct bool
	// Victim, Attacker and DirectAttacker are minecraft:entity_type IDs of
	// the hurt body, the one the blow is attributed to and the one that
	// carried it; -1 for none (a causeless environment blow).
	Victim, Attacker, DirectAttacker int32
	// DirectAttackerFlying includes gliding and ability flight (EntityFlagsPredicate).
	DirectAttackerFlying       bool
	DirectAttackerFallDistance float64
	// Tool is what match_tool sees: the item the effect runs on (the tool
	// worn, the ammunition spent); empty in damage contexts, where vanilla
	// has no tool.
	Tool protocol.ItemStack
	// User is the state of the body running a user-side effect.
	User EnchUser
	// Level and Rand are the enchantment level and the dice a
	// random_chance condition reads; set per enchantment by the walk that
	// owns them, nil dice refusing every chance.
	Level int32
	Rand  *rand.Rand
	// Active is the location_changed walk's ENCHANTMENT_ACTIVE: whether the
	// entry was active before this run; nil outside that walk, where
	// enchantment_active_check fails.
	// vanilla: Enchantment.locationContext
	Active *bool
	// Body is a frozen snapshot of the wearer and World the blocks its
	// position predicates read; a nil Body fails every entity_properties on
	// "this" that asks after movement, ground contact or age.
	Body  *LootEntity
	World BlockAt
	// BodyOf builds Body on the first predicate that reads it, so a walk
	// whose entries never test the body does not snapshot it.
	BodyOf func() *LootEntity
	// AttackerOf and DirectAttackerOf build the blow's ATTACKING_ENTITY and
	// DIRECT_ATTACKING_ENTITY for a loot condition; nil leaves them absent.
	AttackerOf, DirectAttackerOf func() *LootEntity
	// LootWorld fills the level and origin of a loot condition's context;
	// nil leaves them unset.
	LootWorld func(*LootContext)
}

// EnchUser is the state an entity_properties "this" test reads on the body
// whose item runs the effect.
type EnchUser struct {
	Riding, FallFlying, InWater, IsPlayer bool
	// GameMode is the name (survival, creative, ...) and Food the food level.
	GameMode string
	Food     int32
}

// EnchPiece is one equipment piece: the stack and its EquipmentSlot name
// (mainhand, offhand, head, chest, legs, feet), which an enchantment's
// slot list must match for its per-slot effects to count.
type EnchPiece struct {
	Stack protocol.ItemStack
	Slot  string
}

// enchInstance is one enchantment a stack carries, with its level.
type enchInstance struct {
	def   *registry.EnchantmentDef
	level int32
}

// enchantsOn reads a stack's enchantments as (enchantment, level) pairs in
// registry order.
func enchantsOn(stack protocol.ItemStack) []enchInstance {
	return enchantsOnInto(nil, stack)
}

// enchantsOnInto is enchantsOn appending to out, so a hot caller can pass a
// stack-backed buffer.
func enchantsOnInto(out []enchInstance, stack protocol.ItemStack) []enchInstance {
	if len(stack.Enchantments) == 0 {
		return out
	}
	var idBuf [8]int32
	ids := idBuf[:0]
	for id := range stack.Enchantments {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		level := stack.Enchantments[id]
		def, ok := registry.EnchantmentEffectsByID(id)
		if !ok || level <= 0 {
			continue
		}
		out = append(out, enchInstance{def: def, level: level})
	}
	return out
}

// enchLevelValue is LevelBasedValue.calculate in float32 throughout, then
// widened: the amount an attribute modifier carries on the wire.
func enchLevelValue(v *registry.EnchLevelValue, level int32) float64 {
	if v == nil {
		return 0
	}
	switch v.Kind {
	case registry.EnchValueConstant:
		return float64(v.Constant)
	case registry.EnchValueLinear:
		// The explicit float32 on the product stops arm64 fusing the
		// multiply-add, which would round once where Java rounds twice.
		return float64(v.Base + float32(v.PerLevel*float32(level-1)))
	case registry.EnchValueFraction:
		den := enchLevelValue(v.Den, level)
		if den == 0 {
			return 0
		}
		return float64(float32(enchLevelValue(v.Num, level)) / float32(den))
	case registry.EnchValueLevelsSquared:
		return float64(float32(level*level) + v.Added)
	case registry.EnchValueClamped:
		return float64(min(max(float32(enchLevelValue(v.Value, level)), v.Min), v.Max))
	case registry.EnchValueLookup:
		if int(level) <= len(v.Values) && level >= 1 {
			return float64(v.Values[level-1])
		}
		return enchLevelValue(v.Fallback, level)
	case registry.EnchValueExponent:
		return float64(float32(math.Pow(enchLevelValue(v.ExpBase, level), enchLevelValue(v.ExpPower, level))))
	}
	return 0
}

// enchProcessValue applies one value effect; remove_binomial keeps each
// point only where a roll against its chance says. A nil rng is a caller
// whose component never rolls in vanilla (damage and protection adds); a
// binomial it reaches leaves the points rather than inventing a roll.
// vanilla: EnchantmentValueEffect.process
func enchProcessValue(e registry.EnchEffect, level int32, rng *rand.Rand, value float64) float64 {
	switch e.Kind {
	case registry.EnchAdd:
		return value + enchLevelValue(e.Value, level)
	case registry.EnchMultiply:
		return value * enchLevelValue(e.Value, level)
	case registry.EnchExponential:
		// vanilla: ScaleExponentially.process computes in double, returns float
		return float64(float32(value * math.Pow(enchLevelValue(e.Value, level), enchLevelValue(e.Exponent, level))))
	case registry.EnchSet:
		return enchLevelValue(e.Value, level)
	case registry.EnchAllOf:
		for _, sub := range e.Effects {
			value = enchProcessValue(sub, level, rng, value)
		}
		return value
	case registry.EnchRemoveBinomial:
		if rng == nil {
			return value
		}
		chance := enchLevelValue(e.Value, level)
		dropped := 0
		for point := 0; point < int(value); point++ {
			if rng.Float64() < chance {
				dropped++
			}
		}
		return value - float64(dropped)
	}
	return value
}

// enchCondition matches a condition against ctx: match_tool's item,
// entity_properties' entity types, damage_source_properties' type and
// directness, and the composites.
func enchCondition(c *registry.EnchCondition, ctx *EnchContext) bool {
	if c == nil {
		return true
	}
	switch c.Kind {
	case registry.EnchCondWeatherCheck:
		return c.Thundering == nil || *c.Thundering == ctx.Thundering
	case registry.EnchCondLocationCheck:
		return enchLocationMatches(c, ctx)
	case registry.EnchCondMatchTool:
		// Vanilla's MatchTool: no tool in the context refuses, as does a
		// tool outside the predicate's item set.
		if ctx.Tool.Empty() {
			return false
		}
		if len(c.ToolItems) == 0 && c.ToolTag == "" {
			return true
		}
		if slices.Contains(c.ToolItems, registry.Item(ctx.Tool.ItemID)) {
			return true
		}
		return c.ToolTag != "" && registry.Item(ctx.Tool.ItemID).InTag(registry.ItemTagOf(c.ToolTag))
	case registry.EnchCondActive:
		return ctx.Active != nil && *ctx.Active == c.Active
	case registry.EnchCondLoot:
		return enchLootConditions(c.Loot, ctx)
	case registry.EnchCondBody:
		if ctx.Body == nil && ctx.BodyOf != nil {
			ctx.Body = ctx.BodyOf()
		}
		if ctx.Body == nil {
			return false
		}
		loot := &LootContext{HasOrigin: true}
		if ctx.World != nil {
			loot.BlockAt = ctx.World.BlockAt
		}
		return entityPredicate(*c.Body, *ctx.Body, loot)
	case registry.EnchCondEntityState:
		return c.Entity == "this" && enchEntityStateMatches(c.State, &ctx.User)
	case registry.EnchCondEntityProperties:
		entity := int32(-1)
		switch c.Entity {
		case "this":
			entity = ctx.Victim
		case "attacker":
			entity = ctx.Attacker
		case "direct_attacker":
			entity = ctx.DirectAttacker
		}
		if entity < 0 {
			return false
		}
		if c.IsFlying != nil || c.MinFallDistance != nil {
			return c.Entity == "direct_attacker" &&
				(c.IsFlying == nil || *c.IsFlying == ctx.DirectAttackerFlying) &&
				(c.MinFallDistance == nil || ctx.DirectAttackerFallDistance >= *c.MinFallDistance)
		}
		if c.CanSeeSky != nil || c.BlockTag != "" {
			return c.Entity == "this" && enchLocationMatches(c, ctx)
		}
		if c.EntityTypeTag != "" {
			return registry.EntityTypeInTag(entity, c.EntityTypeTag)
		}
		if c.EntityTypeID != "" {
			id, ok := registry.EntityTypeID(c.EntityTypeID)
			return ok && id == entity
		}
		return false
	case registry.EnchCondDamageSource:
		for _, expect := range c.DamageTags {
			if expect.Types.Contains(ctx.DamageType) != expect.Expected {
				return false
			}
		}
		if c.Direct >= 0 && ctx.Direct != (c.Direct == 1) {
			return false
		}
		return true
	case registry.EnchCondRandomChance:
		// vanilla: LootItemRandomChanceCondition.test
		return ctx.Rand != nil &&
			ctx.Rand.Float32() < float32(enchLevelValue(c.Chance, ctx.Level))
	case registry.EnchCondAllOf:
		for _, term := range c.Terms {
			if !enchCondition(term, ctx) {
				return false
			}
		}
		return true
	case registry.EnchCondAnyOf:
		for _, term := range c.Terms {
			if enchCondition(term, ctx) {
				return true
			}
		}
		return false
	case registry.EnchCondInverted:
		return !enchCondition(c.Term, ctx)
	}
	return false
}

func enchEntityStateMatches(s *registry.EnchEntityState, u *EnchUser) bool {
	if s.Vehicle && !u.Riding || s.Player && !u.IsPlayer {
		return false
	}
	if s.FallFlying != nil && *s.FallFlying != u.FallFlying || s.InWater != nil && *s.InWater != u.InWater {
		return false
	}
	if len(s.Gamemodes) > 0 && !slices.Contains(s.Gamemodes, u.GameMode) {
		return false
	}
	return s.FoodMin == nil || u.Food >= *s.FoodMin
}

func enchLocationMatches(c *registry.EnchCondition, ctx *EnchContext) bool {
	return (c.CanSeeSky == nil || *c.CanSeeSky == ctx.CanSeeSky) &&
		(c.BlockTag == "" || ctx.Block.InTag(registry.BlockTagOf(c.BlockTag)))
}

// TridentHitEffects reads the thrown weapon's summon and sound effects for a landed hit.
// vanilla: EnchantmentHelper.doPostAttackEffects, onHitBlock
func TridentHitEffects(weapon protocol.ItemStack, ctx *EnchContext, block bool) []registry.EnchEntityEffect {
	var effects []registry.EnchEntityEffect
	if !block {
		for _, a := range PostAttackEffects(weapon, nil, ctx, ctx.Rand) {
			if e := a.Effect; a.Target == "victim" && (e.SummonEntity != "" || e.PlaySound != nil) {
				effects = append(effects, e)
			}
		}
		return effects
	}
	for _, ench := range enchantsOn(weapon) {
		for _, hit := range ench.def.Effects.HitBlock {
			if e := hit.Effect; (e.SummonEntity != "" || e.PlaySound != nil) && enchCondition(hit.Requires, ctx) {
				effects = append(effects, e)
			}
		}
	}
	return effects
}

// enchValueEffects runs the value effects of one component on a value: each
// effect whose condition passes takes the value the one before left, the
// component's list being the chain vanilla's applyEffects walks.
func enchValueEffects(list []registry.EnchConditional, level int32, rng *rand.Rand, ctx *EnchContext, value float64) float64 {
	ctx.Level = level
	for _, conditional := range list {
		if !enchCondition(conditional.Requires, ctx) {
			continue
		}
		value = enchProcessValue(conditional.Effect, level, rng, value)
	}
	return value
}

// EnchantmentLevel is the level at which one named enchantment runs on a
// stack, zero where the stack does not carry it.
func EnchantmentLevel(stack protocol.ItemStack, name string) int32 {
	for _, ench := range enchantsOn(stack) {
		if ench.def.Name == name {
			return ench.level
		}
	}
	return 0
}

// EnchantedDamage runs the weapon's damage effects over damage, which is the
// caller's total with the critical already in, as vanilla passes it: the
// enchantment share is never multiplied by the critical.
// vanilla: EnchantmentHelper.modifyDamage
func EnchantedDamage(weapon protocol.ItemStack, ctx *EnchContext, damage float64) float64 {
	for _, ench := range enchantsOn(weapon) {
		damage = enchValueEffects(ench.def.Effects.Damage, ench.level, nil, ctx, damage)
	}
	return damage
}

// SmashDamagePerFallenBlock is the extra damage per block fallen the
// weapon's enchantments (density) add to a mace smash.
// vanilla: EnchantmentHelper.modifyFallBasedDamage from zero
func SmashDamagePerFallenBlock(weapon protocol.ItemStack, ctx *EnchContext) float64 {
	var perBlock float64
	for _, ench := range enchantsOn(weapon) {
		perBlock = enchValueEffects(ench.def.Effects.SmashDamagePerFallenBlock, ench.level, nil, ctx, perBlock)
	}
	return perBlock
}

// EnchantedKnockback runs the weapon's knockback effects. The caller halves
// the result, as getKnockback does.
// vanilla: EnchantmentHelper.modifyKnockback
func EnchantedKnockback(weapon protocol.ItemStack, ctx *EnchContext, knockback float64) float64 {
	for _, ench := range enchantsOn(weapon) {
		knockback = enchValueEffects(ench.def.Effects.Knockback, ench.level, nil, ctx, knockback)
	}
	return knockback
}

// ArmourEffectiveness is the weapon's armor_effectiveness delta (Breach),
// added to the armour fraction by DamageAfterArmourEffective.
// vanilla: EnchantmentHelper.modifyArmorEffectiveness
func ArmourEffectiveness(weapon protocol.ItemStack, ctx *EnchContext) float64 {
	var delta float64
	for _, ench := range enchantsOn(weapon) {
		delta = enchValueEffects(ench.def.Effects.ArmorEffectiveness, ench.level, nil, ctx, delta)
	}
	return delta
}

// DamageProtection sums the enchantment protection points the equipment
// raises against one blow, each piece counting only in its working slots
// and where the damage type passes. Capping at 20 (CombatRules) is the
// caller's; rng is the victim's, which these components never roll.
// vanilla: EnchantmentHelper.getDamageProtection
func DamageProtection(pieces []EnchPiece, ctx *EnchContext, rng *rand.Rand) float64 {
	var total float64
	for _, piece := range pieces {
		for _, ench := range enchantsOn(piece.Stack) {
			if !ench.def.MatchesSlot(piece.Slot) {
				continue
			}
			total = enchValueEffects(ench.def.Effects.DamageProtection, ench.level, rng, ctx, total)
		}
	}
	return float64(float32(total)) // vanilla sums into a MutableFloat
}

// ScaledIgniteTicks is the burn an ignition of ticks leaves a body whose
// burning_time is scale.
// vanilla: LivingEntity.igniteForTicks
func ScaledIgniteTicks(ticks int, scale float64) int {
	return int(math.Ceil(float64(ticks) * scale))
}

// ProcessDurabilityChange is the wear a stack takes after its enchantments
// (unbreaking's remove_binomial, rolled on the holder's rng), truncated to an
// int. A non-damageable stack takes 0.
// vanilla: EnchantmentHelper.processDurabilityChange, ItemStack.isDamageableItem
func ProcessDurabilityChange(stack protocol.ItemStack, n int32, rng *rand.Rand) int32 {
	if !registry.StackDamageable(stack) {
		return 0
	}
	if n <= 0 || len(stack.Enchantments) == 0 {
		return n
	}
	ctx := &EnchContext{Tool: stack}
	value := float64(n)
	for _, ench := range enchantsOn(stack) {
		value = enchValueEffects(ench.def.Effects.ItemDamage, ench.level, rng, ctx, value)
	}
	return int32(value)
}

// ProcessDurabilityChangeWithoutBreaking caps enchanted wear to leave one durability.
// vanilla: ItemStack.hurtWithoutBreaking
func ProcessDurabilityChangeWithoutBreaking(stack protocol.ItemStack, n int32, rng *rand.Rand) int32 {
	return min(ProcessDurabilityChange(stack, n, rng), registry.StackMaxDamage(stack)-1-stack.Damage)
}

// DurabilityFromXp is the durability xp points of repair are worth to a
// stack's repair_with_xp enchantments; never below nought.
// vanilla: EnchantmentHelper.modifyDurabilityToRepairFromXp
func DurabilityFromXp(stack protocol.ItemStack, xp int) int32 {
	value := float64(xp)
	ctx := &EnchContext{Tool: stack}
	for _, ench := range enchantsOn(stack) {
		value = enchValueEffects(ench.def.Effects.RepairWithXp, ench.level, nil, ctx, value)
	}
	if value < 0 {
		return 0
	}
	return int32(value)
}

// BlockExperience is the experience a broken block drops after the tool's
// block_experience enchantments (silk touch zeroes it); never below nought.
// A zero LootTool (no player tool) changes nothing.
// vanilla: EnchantmentHelper.processBlockExperience
func BlockExperience(tool LootTool, amount int32) int32 {
	if len(tool.Enchantments) == 0 {
		return amount
	}
	value := float64(amount)
	ctx := &EnchContext{}
	for name, level := range tool.Enchantments {
		id, ok := registry.EnchantmentID(name)
		if !ok || level <= 0 {
			continue
		}
		if def, ok := registry.EnchantmentEffectsByID(id); ok {
			value = enchValueEffects(def.Effects.BlockExperience, level, nil, ctx, value)
		}
	}
	return int32(max(value, 0))
}

// equipmentValue runs one component's targeted value effects over the pieces
// whose enchanted side is owner, each enchantment counting in its own slots.
func equipmentValue(pieces []EnchPiece, owner string, ctx *EnchContext, rng *rand.Rand, value float64) float64 {
	for _, piece := range pieces {
		for _, ench := range enchantsOn(piece.Stack) {
			if !ench.def.MatchesSlot(piece.Slot) {
				continue
			}
			here := *ctx
			here.Level, here.Rand = ench.level, rng
			for _, e := range ench.def.Effects.EquipmentDrops {
				if e.Enchanted == owner && enchCondition(e.Requires, &here) {
					value = enchProcessValue(e.Effect, ench.level, rng, value)
				}
			}
		}
	}
	return value
}

// EquipmentDropChance is a slot's drop chance after the victim's own
// equipment's equipment_drops effects, then the attacker's.
// vanilla: EnchantmentHelper.processEquipmentDropChance
func EquipmentDropChance(victimPieces, attackerPieces []EnchPiece, ctx *EnchContext, rng *rand.Rand, chance float32) float32 {
	value := equipmentValue(victimPieces, "victim", ctx, rng, float64(chance))
	return float32(equipmentValue(attackerPieces, "attacker", ctx, rng, value))
}

// MobExperience is a mob kill's experience after the killer's equipment's
// mob_experience effects.
// vanilla: EnchantmentHelper.processMobExperience
func MobExperience(killerPieces []EnchPiece, ctx *EnchContext, rng *rand.Rand, xp int) int {
	value := float64(xp)
	for _, piece := range killerPieces {
		for _, ench := range enchantsOn(piece.Stack) {
			if ench.def.MatchesSlot(piece.Slot) {
				value = enchValueEffects(ench.def.Effects.MobExperience, ench.level, rng, ctx, value)
			}
		}
	}
	return int(value)
}

// PreventsEquipmentDrop reports whether the stack is kept from dropping when
// its holder dies (Curse of Vanishing).
// vanilla: EnchantmentHelper.has(stack, EnchantmentEffectComponents.PREVENT_EQUIPMENT_DROP)
func PreventsEquipmentDrop(stack protocol.ItemStack) bool {
	for _, ench := range enchantsOn(stack) {
		if ench.def.Effects.PreventEquipmentDrop {
			return true
		}
	}
	return false
}

// ImmuneToDamage reports whether an enchantment on the equipment, working in
// its piece's slot, makes the wearer immune to the damage in ctx.
// vanilla: EnchantmentHelper.isImmuneToDamage
func ImmuneToDamage(pieces []EnchPiece, ctx *EnchContext) bool {
	for _, piece := range pieces {
		for _, ench := range enchantsOn(piece.Stack) {
			if !ench.def.MatchesSlot(piece.Slot) {
				continue
			}
			here := *ctx
			here.Level = ench.level
			for _, e := range ench.def.Effects.DamageImmunity {
				if enchCondition(e.Requires, &here) {
					return true
				}
			}
		}
	}
	return false
}

// highestLevel is the effects list of the enchantment with the highest level
// on the stack among those that carry the component, and that level; the
// first in ID order wins a tie.
// vanilla: EnchantmentHelper.getHighestLevel
func highestLevel[T any](stack protocol.ItemStack, component func(*registry.EnchantmentEffects) []T) (list []T, level int32) {
	for _, ench := range enchantsOn(stack) {
		if l := component(&ench.def.Effects); l != nil && ench.level > level {
			list, level = l, ench.level
		}
	}
	return list, level
}

// ChargingSounds is the crossbow's start, mid and end sounds (ID -1 absent):
// the highest-level enchantment's entry for its level, else the plain
// crossbow's.
// vanilla: CrossbowItem.getChargingSounds
func ChargingSounds(stack protocol.ItemStack) registry.EnchChargingSounds {
	if list, level := highestLevel(stack, func(e *registry.EnchantmentEffects) []registry.EnchChargingSounds { return e.CrossbowChargingSounds }); len(list) > 0 {
		return list[min(level, int32(len(list)))-1]
	}
	return registry.EnchChargingSounds{
		Start: registry.SoundItemCrossbowLoadingStart.Holder(),
		Mid:   registry.SoundItemCrossbowLoadingMiddle.Holder(),
		End:   registry.SoundItemCrossbowLoadingEnd.Holder(),
	}
}

// TridentSound is the sound of a throw or a riptide launch: the
// highest-level enchantment's entry for its level, else the plain throw's.
// vanilla: TridentItem.releaseUsing
func TridentSound(stack protocol.ItemStack) protocol.SoundHolder {
	if list, level := highestLevel(stack, func(e *registry.EnchantmentEffects) []protocol.SoundHolder { return e.TridentSound }); len(list) > 0 {
		return list[min(level, int32(len(list)))-1]
	}
	return registry.SoundItemTridentThrow.Holder()
}

// ProcessAmmoUse is the ammunition one shot spends after the weapon's
// enchantments (infinity zeroes a plain arrow's cost). match_tool sees the
// ammunition, not the bow.
// vanilla: EnchantmentHelper.processAmmoUse
func ProcessAmmoUse(weapon, ammo protocol.ItemStack) int32 {
	if len(weapon.Enchantments) == 0 {
		return 1
	}
	ctx := &EnchContext{Tool: ammo}
	value := float64(1)
	for _, ench := range enchantsOn(weapon) {
		value = enchValueEffects(ench.def.Effects.AmmoUse, ench.level, nil, ctx, value)
	}
	return int32(value)
}

// IgniteTicks is an ignite effect's seconds as fire ticks, floored at 20 a
// second. The burning-time attribute multiplier is not modelled (default 1).
// vanilla: Entity.igniteForSeconds
func IgniteTicks(seconds float64) int32 {
	return int32(math.Floor(seconds * 20))
}

// AttackExplosion is a post-attack explosion at the attacker, without a damage source.
type AttackExplosion struct {
	Radius, Knockback float64
	Interaction       ExplosionInteraction
}

// PostAttackAction is one conditional effect and the body and item it acts on.
// Piece indexes victim equipment; -1 identifies the attacker's weapon.
type PostAttackAction struct {
	Target string
	Piece  int
	Level  int32
	Effect registry.EnchEntityEffect
}

// PostAttackEffects walks victim equipment before the attacker's weapon.
// vanilla: EnchantmentHelper.doPostAttackEffectsWithItemSourceOnBreak
func PostAttackEffects(weapon protocol.ItemStack, victimPieces []EnchPiece, ctx *EnchContext, rng *rand.Rand) []PostAttackAction {
	var out []PostAttackAction
	walk := func(stack protocol.ItemStack, slot, owner string, piece int) {
		for _, ench := range enchantsOn(stack) {
			if !ench.def.MatchesSlot(slot) {
				continue
			}
			here := *ctx
			here.Level, here.Rand = ench.level, rng
			for _, targeted := range ench.def.Effects.PostAttack {
				if targeted.Enchanted == owner && enchCondition(targeted.Requires, &here) {
					out = append(out, PostAttackAction{Target: targeted.Affected, Piece: piece, Level: ench.level, Effect: targeted.Effect})
				}
			}
		}
	}
	for i, piece := range victimPieces {
		walk(piece.Stack, piece.Slot, "victim", i)
	}
	walk(weapon, "mainhand", "attacker", -1)
	return out
}

// Explosion reads an action's supported wind explosion.
func (a PostAttackAction) Explosion() (AttackExplosion, bool) {
	e := a.Effect.Explode
	if e == nil || !e.WindBurst() {
		return AttackExplosion{}, false
	}
	return AttackExplosion{Radius: max(0, enchLevelValue(e.Radius, a.Level)), Knockback: enchLevelValue(e.KnockbackMultiplier, a.Level), Interaction: ExplosionTrigger}, true
}

// PostAttackExplosions reads the weapon's attacker-side wind explosions.
func PostAttackExplosions(weapon protocol.ItemStack, ctx *EnchContext) []AttackExplosion {
	var out []AttackExplosion
	for _, a := range PostAttackEffects(weapon, nil, ctx, ctx.Rand) {
		if e, ok := a.Explosion(); ok && a.Target == "attacker" {
			out = append(out, e)
		}
	}
	return out
}

// MobEffects rolls the action's status effect on the target's dice.
// vanilla: ApplyMobEffect.apply
func (a PostAttackAction) MobEffects(rng *rand.Rand) []effect.Instance {
	m := a.Effect.MobEffect
	if m == nil {
		return nil
	}
	id, ok := registry.MobEffectID(m.Name)
	if !ok {
		return nil
	}
	seconds := enchRollBetween(rng, enchLevelValue(m.MinDuration, a.Level), enchLevelValue(m.MaxDuration, a.Level))
	amplifier := enchRollBetween(rng, enchLevelValue(m.MinAmplifier, a.Level), enchLevelValue(m.MaxAmplifier, a.Level))
	return []effect.Instance{effect.New(id, int32(math.Round(seconds*20)), max(0, int32(math.Round(amplifier))), false, true, true)}
}

// Retaliation is damage and wear from an enchanted equipment piece.
type Retaliation struct {
	Piece      int
	Damage     float64
	DamageType registry.DamageType
	ItemWear   int32
}

// Retaliation rolls an action's damage on the target's dice.
// vanilla: DamageEntity.apply
func (a PostAttackAction) Retaliation(rng *rand.Rand) Retaliation {
	r := Retaliation{Piece: a.Piece, ItemWear: int32(enchLevelValue(a.Effect.ChangeItemDamage, a.Level))}
	if hit := a.Effect.DamageEntity; hit != nil {
		r.Damage = enchRollBetween(rng, enchLevelValue(hit.Min, a.Level), enchLevelValue(hit.Max, a.Level))
		r.DamageType = hit.DamageType
	}
	return r
}

// PostAttackRetaliations reads victim equipment damage targeting the attacker.
func PostAttackRetaliations(pieces []EnchPiece, ctx *EnchContext, rng *rand.Rand) []Retaliation {
	var out []Retaliation
	for _, a := range PostAttackEffects(protocol.ItemStack{}, pieces, ctx, rng) {
		if a.Target == "attacker" && a.Effect.DamageEntity != nil {
			out = append(out, a.Retaliation(rng))
		}
	}
	return out
}

// enchRollBetween rolls a point between the two ends in the float the Java
// arithmetic runs in; a nil pair of dice stands still at the bottom end.
// vanilla: Mth.randomBetween
func enchRollBetween(rng *rand.Rand, min, max float64) float64 {
	if rng == nil {
		return min
	}
	return float64(rng.Float32()*float32(max-min) + float32(min))
}

// ProjectileIgniteTicks is the burn the weapon's projectile_spawned ignite
// effects give the projectile (flame: an arrow arrives burning for 100 s).
// vanilla: EnchantmentHelper.onProjectileSpawned
func ProjectileIgniteTicks(weapon protocol.ItemStack) int32 {
	var best int32
	for _, ench := range enchantsOn(weapon) {
		for _, spawned := range ench.def.Effects.ProjectileSpawned {
			if !enchCondition(spawned.Requires, &EnchContext{}) {
				continue
			}
			if ticks := IgniteTicks(enchLevelValue(spawned.Effect.Duration, ench.level)); ticks > best {
				best = ticks
			}
		}
	}
	return best
}

// LeveledEffect is an entity effect with the level of the enchantment it came from.
type LeveledEffect struct {
	Level  int32
	Effect registry.EnchEntityEffect
}

// ProjectileSpawnedEffects lists the weapon's projectile_spawned effects.
// vanilla: EnchantmentHelper.onProjectileSpawned
func ProjectileSpawnedEffects(weapon protocol.ItemStack) []LeveledEffect {
	var out []LeveledEffect
	for _, ench := range enchantsOn(weapon) {
		for _, spawned := range ench.def.Effects.ProjectileSpawned {
			if enchCondition(spawned.Requires, &EnchContext{}) {
				out = append(out, LeveledEffect{ench.level, spawned.Effect})
			}
		}
	}
	return out
}

// HitBlockEffects lists the weapon's hit_block effects whose requirements the
// hit passes.
// vanilla: EnchantmentHelper.onHitBlock
func HitBlockEffects(weapon protocol.ItemStack, ctx *EnchContext) []LeveledEffect {
	var out []LeveledEffect
	for _, ench := range enchantsOn(weapon) {
		here := *ctx
		here.Level = ench.level
		for _, hit := range ench.def.Effects.HitBlock {
			if enchCondition(hit.Requires, &here) {
				out = append(out, LeveledEffect{ench.level, hit.Effect})
			}
		}
	}
	return out
}

// Disenchant removes every non-curse enchantment from the stack's
// component and returns how many remain; an emptied component is dropped,
// which the wire treats the same as empty. The experience worth and the
// stripped item's fate are the caller's.
// vanilla: GrindstoneMenu.removeNonCursesFrom, EnchantmentTags.CURSE
func Disenchant(stack *protocol.ItemStack) int {
	levels := cloneEnchantments(anvilEnchantmentsOf(*stack))
	for id := range levels {
		if !registry.EnchantmentInTag(id, "minecraft:curse") {
			delete(levels, id)
		}
	}
	kept := len(levels)
	if kept == 0 {
		levels = nil
	}
	stack.SetEnchantments(isEnchantedBook(*stack), levels)
	return kept
}

// EnchLevelValueAt is LevelBasedValue.calculate for the exported callers.
func EnchLevelValueAt(v *registry.EnchLevelValue, level int32) float32 {
	return float32(enchLevelValue(v, level))
}

// itemEnchValue folds an item's conditional value component in its loot context.
func itemEnchValue(stack protocol.ItemStack, contexts []*EnchContext, component func(*registry.EnchantmentEffects) []registry.EnchConditional, value float64) float64 {
	ctx := EnchContext{Victim: -1, Attacker: -1, DirectAttacker: -1}
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = *contexts[0]
	}
	for _, ench := range enchantsOn(stack) {
		ctx.Level = ench.level
		value = enchValueEffects(component(&ench.def.Effects), ench.level, ctx.Rand, &ctx, value)
	}
	return value
}

// unfilteredEnchValue folds single value components, which have no requirements.
func unfilteredEnchValue(stack protocol.ItemStack, contexts []*EnchContext, component func(*registry.EnchantmentEffects) *registry.EnchEffect, value float64) float64 {
	var rng *rand.Rand
	if len(contexts) > 0 && contexts[0] != nil {
		rng = contexts[0].Rand
	}
	for _, ench := range enchantsOn(stack) {
		if effect := component(&ench.def.Effects); effect != nil {
			value = enchProcessValue(*effect, ench.level, rng, value)
		}
	}
	return value
}

// TridentReturnAcceleration is the thrown stack's return speed before the synced byte clamp.
// vanilla: EnchantmentHelper.getTridentReturnToOwnerAcceleration
func TridentReturnAcceleration(stack protocol.ItemStack, ctx ...*EnchContext) int32 {
	value := itemEnchValue(stack, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.TridentReturnAcceleration }, 0)
	return max(0, int32(value))
}

// LocatedAction is one location_changed or tick entry that holds for a worn
// stack: the slot it sits in, the enchantment's level and the entry.
type LocatedAction struct {
	Slot        string
	Enchantment string
	Level       int32
	Entry       registry.EnchLocated
	// Became is set by LocatedState.Step on an entry that was not active
	// before the run, the only time an attribute entry applies.
	Became bool
}

// HasLocated reports whether an enchantment on the stack has a
// location_changed or tick entry. It is not cached: a reload can change the
// definitions.
func HasLocated(stack protocol.ItemStack) bool {
	for id := range stack.Enchantments {
		if def, ok := registry.EnchantmentEffectsByID(id); ok && (len(def.Effects.LocationChanged) > 0 || len(def.Effects.Tick) > 0) {
			return true
		}
	}
	return false
}

// LocatedEffects lists the entries of a worn stack's enchantments whose
// condition holds (tick picks the tick component, else location_changed). A
// broken stack applies none.
// vanilla: EnchantmentHelper.runLocationChangedEffects, tickEffects
func LocatedEffects(stack protocol.ItemStack, slot string, tick bool, ctx *EnchContext, rng *rand.Rand) []LocatedAction {
	if stack.Empty() || broken(stack) {
		return nil
	}
	var out []LocatedAction
	var enchBuf [4]enchInstance
	for _, ench := range enchantsOnInto(enchBuf[:0], stack) {
		if !ench.def.MatchesSlot(slot) {
			continue
		}
		list := ench.def.Effects.LocationChanged
		if tick {
			list = ench.def.Effects.Tick
		}
		here := *ctx
		here.Level, here.Rand, here.Tool = ench.level, rng, stack
		for _, entry := range list {
			if enchCondition(entry.Requires, &here) {
				out = append(out, LocatedAction{Slot: slot, Level: ench.level, Entry: entry})
			}
		}
	}
	return out
}

// LocatedAttributeModifier is the modifier an attribute entry holds at the
// action's level, id suffixed with the slot.
// vanilla: AttributeEffect.makeId, getDirectModifier
func (a LocatedAction) LocatedAttributeModifier() (registry.AttributeModifier, bool) {
	attr := a.Entry.Attribute
	if attr == nil {
		return registry.AttributeModifier{}, false
	}
	op, ok := registry.AttributeOperationByName(attr.Operation)
	target, known := registry.AttributeByName(attr.Attribute)
	if !ok || !known {
		return registry.AttributeModifier{}, false
	}
	return registry.AttributeModifier{
		Attribute: registry.AttributeID(target.ID),
		ID:        attr.ID + "/" + a.Slot,
		Amount:    enchLevelValue(attr.Amount, a.Level),
		Operation: op,
		Slot:      a.Slot,
	}, true
}

// MobEffects rolls the entry's apply_mob_effect on the dice.
func (a LocatedAction) MobEffects(rng *rand.Rand) []effect.Instance {
	return PostAttackAction{Level: a.Level, Effect: a.Entry.Effect}.MobEffects(rng)
}

// enchLootConditions tests loot conditions against the effect's context.
// vanilla: Enchantment.damageContext, itemContext, locationContext
func enchLootConditions(conds []registry.LootCondition, ctx *EnchContext) bool {
	loot := LootContext{Tool: ToolOf(ctx.Tool), DamageType: ctx.DamageType, EnchantmentActive: ctx.Active, EnchantmentLevel: ctx.Level}
	if ctx.Body == nil && ctx.BodyOf != nil {
		ctx.Body = ctx.BodyOf()
	}
	// A body the caller gave no snapshot is known by its type alone.
	loot.This = bodyOrType(ctx.Body, ctx.Victim)
	loot.Attacker = bodyOrType(callBody(ctx.AttackerOf), ctx.Attacker)
	loot.DirectAttacker = bodyOrType(callBody(ctx.DirectAttackerOf), ctx.DirectAttacker)
	if ctx.LootWorld != nil {
		ctx.LootWorld(&loot)
	}
	rng := ctx.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(0))
	}
	for _, c := range conds {
		if !TestLootCondition(c, &loot, rng) {
			return false
		}
	}
	return true
}

func callBody(f func() *LootEntity) *LootEntity {
	if f == nil {
		return nil
	}
	return f()
}

// bodyOrType is e, or a body of the entity type typeID (-1 for none) when e is nil.
func bodyOrType(e *LootEntity, typeID int32) LootEntity {
	switch {
	case e != nil:
		return *e
	case typeID >= 0:
		return LootEntity{Type: registry.EntityType(typeID).Name()}
	}
	return LootEntity{}
}
