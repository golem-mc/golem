package entity

import (
	"math"
	"math/rand"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// RollExperience rolls a death's experience (LivingEntity.dropExperience
// into ExperienceOrb.award): the kind's base (Animal.getBaseExperienceReward's
// 1-3, or the Monster constructor's 5) plus, for a kind on
// Mob.getBaseExperienceReward (a fixed positive reward; not Piglin or Hoglin,
// which return xpReward, nor Animal's 1-3), 1-3 per held or worn stack with
// drop chance at most 1, i.e. all but guaranteed drops. Rolls use the mob's own dice. An animal baby gives nothing
// (shouldDropExperience reads !isBaby()), but Monster and Hoglin override it to true;
// Zombie.getBaseExperienceReward scales a baby's reward by 2.5.
func (m *Mob) RollExperience(killer rules.LootKiller) int {
	xp := m.baseExperience()
	if len(killer.CreditedPieces) == 0 {
		return xp
	}
	// vanilla: LivingEntity.getExperienceReward
	ctx := rules.EnchContext{DamageType: m.lastDamageSource(), Victim: m.typeID(), Attacker: int32(registry.EntityTypePlayer), DirectAttacker: -1}
	return rules.MobExperience(killer.CreditedPieces, &ctx, m.Rand, xp)
}

// baseExperience is the reward before the killer's enchantments.
func (m *Mob) baseExperience() int {
	if m.Type == TypeTadpole || m.IsBaby() && (!MonsterKind(m.Type) || m.Type == TypeZombieHorse || m.Type == TypeZombieNautilus) {
		return 0
	}
	if m.Type == TypeChicken && m.ChickenJockey {
		// vanilla: Chicken.getBaseExperienceReward, the jockey hen's ten
		return 10
	}
	min, max, ok := rules.MobExperienceReward(m.Type.Name())
	if m.Type == TypeSulfurCube {
		if m.IsBaby() {
			return 0
		}
		return 1 + m.Rand.Intn(2)
	}
	if m.isCube() {
		// vanilla: Slime and MagmaCube.setSize's xpReward of the size
		min, max, ok = m.Size, m.Size, true
	}
	if !ok {
		return 0
	}
	mobBase := min == max && min > 0 && m.Type != TypePiglin && m.Type != TypeHoglin
	total := min
	if spare := max - min; spare > 0 {
		total += m.Rand.Intn(spare + 1)
	}
	if m.IsBaby() && m.is(TypeZombie) {
		// vanilla: Zombie.getBaseExperienceReward, 2.5 rounded down: a zombie's
		// 5 becomes 12 before equipment.
		total = total * 5 / 2
	}
	if m.Type == TypeHoglin && m.IsBaby() {
		// vanilla: Hoglin.ageBoundaryReached
		total = 3
	}
	if !mobBase {
		return total
	}
	for _, held := range m.equipment() {
		if !held.stack.Empty() && !held.sure {
			total += 1 + m.Rand.Intn(3)
		}
	}
	return total
}

// LootEntity is the facts of the mob a loot table's conditions ask about.
func (m *Mob) LootEntity() rules.LootEntity {
	e := rules.LootEntity{
		Type:        m.Type.Name(),
		OnFire:      m.OnFire(),
		Baby:        m.IsBaby(),
		Sheep:       m.Type == TypeSheep,
		SheepColour: m.Colour,
		Sheared:     m.Sheared,
		Raider:      IsRaiderType(m.Type),
		Captain:     m.Captain(),
		Mooshroom:   m.Mushroom,
		CubeSize:    m.Size,
		FrogVariant: m.Variant,
	}
	if m.Type == TypeChicken {
		e.ChickenVariant = m.Variant
	}
	e.Mainhand, e.Offhand = rules.ToolOf(m.MainHand), rules.ToolOf(m.OffHand)
	e.Feet, e.Legs, e.Chest, e.Head = rules.ToolOf(m.Worn[0]), rules.ToolOf(m.Worn[1]), rules.ToolOf(m.Worn[2]), rules.ToolOf(m.Worn[3])
	e.Body = rules.ToolOf(m.Body)
	e.Facts = func() rules.LootFacts {
		return rules.LootFacts{
			Flags: rules.LootEntityFlags{
				OnGround: m.OnGround, InWater: m.InWater(),
				Sneaking: m.Crouching, Sprinting: m.Sprinting, Swimming: m.Swimming,
			},
			Effects:   LootEffects(m.AllEffects()),
			TickCount: int64(m.Age),
		}
	}
	if m.LootExtra != nil {
		m.LootExtra(&e)
	}
	return e
}

// LootEffects is the active effects as an effects predicate reads them.
// vanilla: MobEffectsPredicate.matches
func LootEffects(active []effect.Instance) []rules.LootEffect {
	out := make([]rules.LootEffect, len(active))
	for i, inst := range active {
		out[i] = rules.LootEffect{
			Name: registry.MobEffectName(inst.ID), Amplifier: inst.Amplifier, Duration: inst.Duration,
			Ambient: inst.Ambient, Visible: inst.Visible,
		}
	}
	return out
}

// RollDrops rolls the kind's loot table with the rules loot engine against
// the death's context: killed_by_player (the 100-tick player memory), the
// killer's looting and fire aspect, the body's burning (furnace_smelt) and a
// sheep's colour and fleece. The dice are a stream of the mob's seed apart
// from its AI, so a seed drops the same loot however long the mob lived. A
// baby drops nothing (shouldDropLoot reads !isBaby()) unless the kind is a
// Monster, which overrides it, so a baby monster rolls with its babyhood in
// the context.
// Level is the level the body died in; nil fails the conditions reading it.
func (m *Mob) RollDrops(killer rules.LootKiller, level rules.LootLevel) []protocol.ItemStack {
	if m.IsBaby() && !babyDropsLoot(m.Type) {
		return nil
	}
	ctx := rules.LootContext{
		KilledByPlayer: m.PlayerKillCredit(),
		DamageType:     m.lastDamageSource(),
		This:           m.LootEntity(),
		Level:          level,
	}
	ctx.This.HasRaid = killer.ThisHasRaid
	ctx.This.NBT = killer.ThisNBT
	if killer.ThisFill != nil {
		killer.ThisFill(&ctx.This)
	}
	if m.PlayerKillCredit() {
		ctx.AttackingPlayer, ctx.Luck = killer.CreditedPlayer, killer.Luck
	}
	// The direct attacker is the attacker for a melee swing, the arrow
	// otherwise, so a bowman's fire aspect smelts nothing.
	// vanilla: LivingEntity.dropFromLootTable
	player := killer.Player || killer.Weapon.Item != ""
	var attacker rules.LootEntity
	attackerID := int32(-1)
	switch {
	case player:
		attackerID = int32(registry.EntityTypePlayer)
		attacker = rules.LootEntity{Type: "minecraft:player", Mainhand: killer.Weapon, Profile: killer.Profile, NBT: killer.PlayerNBT}
		if killer.AttackerFill != nil {
			killer.AttackerFill(&attacker)
		}
	case killer.AttackerType != NoMobType:
		attackerID = int32(killer.AttackerType)
		attacker = rules.LootEntity{Type: killer.AttackerType.Name(), FrogVariant: killer.AttackerVariant}
		if killer.AttackerFill != nil {
			killer.AttackerFill(&attacker)
		}
	}
	if attacker.Type != "" {
		ctx.Attacker = attacker
		ctx.DirectAttacker = attacker
		if direct := projectileOf(m.lastDamageSource(), killer.AttackerType); direct != "" {
			ctx.DirectAttacker, ctx.Indirect = rules.LootEntity{Type: direct}, true
		} else if player && !killer.Melee {
			ctx.DirectAttacker, ctx.Indirect = rules.LootEntity{Type: "minecraft:arrow"}, true
		}
	}
	// Built here, not at spawn or load, so only a death pays for the second
	// source.
	if m.lootRand == nil {
		m.lootRand = rand.New(rand.NewSource(m.lootSeed))
	}
	table := rules.EntityLootTable(m.Type.Name())
	if m.DeathLootTable != "" {
		table = m.DeathLootTable
	}
	ctx.Random = rules.SeededLootRandom(m.DeathLootTableSeed)
	drops := rules.RollLoot(table, &ctx, m.lootRand)
	m.DeathLootTable = ""
	drops = append(drops, m.carriedDrops(level)...)
	if m.Type != TypeAllay { // the allay drops its inventory in TakeDeathEquipment
		drops = append(drops, m.inventoryDrops()...)
	}
	return append(drops, m.rollEquipment(killer, attackerID)...)
}

// TakeChest takes a donkey's or mule's chest off and returns what it held,
// then the chest, except stacks with curse of vanishing. It is not part of the loot: a baby and a world with mob
// drops off still drop it.
// vanilla: AbstractHorse.dropEquipment, AbstractChestedHorse.dropEquipment
func (m *Mob) TakeChest() []protocol.ItemStack {
	if !m.Horse.Chest {
		return nil
	}
	var drops []protocol.ItemStack
	for _, stack := range m.Horse.Items {
		if !stack.Empty() && !rules.PreventsEquipmentDrop(stack) {
			drops = append(drops, stack)
		}
	}
	m.SetMetadataHorseChest(false)
	m.Horse.Items = nil
	return append(drops, protocol.ItemStack{ItemID: int32(registry.ItemChest), Count: 1})
}

// TakeDeathEquipment empties what a dying mob drops whatever the mob_drops
// rule says: an allay's inventory and main hand (not one with curse of
// vanishing), a copper golem's preserved slots.
// vanilla: Allay.dropEquipment, CopperGolem.dropEquipment
func (m *Mob) TakeDeathEquipment() []protocol.ItemStack {
	switch m.Type {
	case TypeAllay:
		drops := m.inventoryDrops()
		if hand := m.MainHand; !hand.Empty() {
			if !rules.PreventsEquipmentDrop(hand) {
				drops = append(drops, hand)
			}
			m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		}
		return drops
	case TypeCopperGolem:
		return m.TakePreservedEquipment()
	}
	return nil
}

// RollChargedHead rolls the charged creeper table for the victim's skull,
// nothing for an unlisted type.
// vanilla: Creeper.killedEntity, BuiltInLootTables.CHARGED_CREEPER
func (m *Mob) RollChargedHead(level rules.LootLevel) []protocol.ItemStack {
	if m.lootRand == nil {
		m.lootRand = rand.New(rand.NewSource(m.lootSeed))
	}
	ctx := rules.LootContext{This: m.LootEntity(), Level: level}
	return rules.RollLoot("minecraft:charged_creeper/root", &ctx, m.lootRand)
}

// giftContext is the context of a gift or barter roll: the mob and its level.
// vanilla: LivingEntity.dropFromGiftLootTable, PiglinAi.getBarterResponseItems
func (m *Mob) giftContext(w World) *rules.LootContext {
	return &rules.LootContext{This: m.LootEntity(), Level: m.lootLevel(w)}
}

// heldStack is a slot's stack and whether its drop is guaranteed.
type heldStack struct {
	stack protocol.ItemStack
	// chance is the slot's drop chance; sure is a chance above one.
	chance float32
	sure   bool
}

// equipment lists the mob's slots in EquipmentSlot order: hands, feet, legs,
// chest, head, saddle and body armor.
func (m *Mob) equipment() [8]heldStack {
	var slots [8]heldStack
	slots[0].stack = m.MainHand
	slots[1].stack = m.OffHand
	for i := range m.Worn {
		slots[2+i].stack = m.Worn[i]
	}
	for i := range m.DropChances {
		slots[i].chance = m.DropChances[i]
		slots[i].sure = m.DropChances[i] > 1
	}
	slots[6].stack, slots[6].chance, slots[6].sure = m.SaddleSlot(), m.SaddleDropChance, m.SaddleDropChance > 1
	slots[7].stack, slots[7].chance, slots[7].sure = m.Body, m.BodyDropChance, m.BodyDropChance > 1
	return slots
}

// equipmentDropChance is DropChances.DEFAULT_EQUIPMENT_DROP_CHANCE.
const equipmentDropChance = 0.085

// DefaultDropChances is DropChances.DEFAULT, every slot at the plain chance.
func DefaultDropChances() [6]float32 {
	var chances [6]float32
	for i := range chances {
		chances[i] = equipmentDropChance
	}
	return chances
}

// ResetDropChances sets every slot, body and saddle included, to DropChances.DEFAULT.
func (m *Mob) ResetDropChances() {
	m.DropChances = DefaultDropChances()
	m.BodyDropChance, m.SaddleDropChance = equipmentDropChance, equipmentDropChance
}

// TakePreservedEquipment empties the slots whose drop is guaranteed and
// returns what they held.
// vanilla: Mob.dropPreservedEquipment, DropChances.isPreserved
func (m *Mob) TakePreservedEquipment() []protocol.ItemStack {
	return m.takePreservedEquipment(func(int, protocol.ItemStack) bool { return true })
}

// takePreservedEquipment lets conversion keep selected slots instead of dropping them.
func (m *Mob) takePreservedEquipment(shouldDrop func(int, protocol.ItemStack) bool) []protocol.ItemStack {
	var taken []protocol.ItemStack
	for i, held := range m.equipment() {
		if held.stack.Empty() || !shouldDrop(i, held.stack) || !held.sure {
			continue
		}
		taken = append(taken, held.stack)
		switch {
		case i == 0:
			m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		case i == 1:
			m.SetEquipment(protocol.EquipOffHand, protocol.ItemStack{})
		case i < 6:
			m.SetEquipment(protocol.EquipFeet+i-2, protocol.ItemStack{})
		case i == 6 && m.Type == TypeCopperGolem:
			m.SetMetadataAntenna(protocol.ItemStack{})
		case i == 6:
			m.SetMetadataHorseSaddle(protocol.ItemStack{})
		default:
			m.SetEquipment(6, protocol.ItemStack{})
		}
	}
	return taken
}

// rollEquipment is Mob.dropCustomDeathLoot: each slot drops by its chance
// (without a player kill only guaranteed drops do); a plain drop of a
// damageable item comes out badly worn.
func (m *Mob) rollEquipment(killer rules.LootKiller, attacker int32) []protocol.ItemStack {
	var drops []protocol.ItemStack
	ctx := rules.EnchContext{DamageType: m.lastDamageSource(), Victim: m.typeID(), Attacker: attacker, DirectAttacker: -1}
	victimPieces := m.enchPieces()
	for _, held := range m.equipment() {
		if held.chance == 0 {
			continue
		}
		// vanilla: EnchantmentHelper.processEquipmentDropChance
		if held.stack.Empty() || rules.PreventsEquipmentDrop(held.stack) || (!m.PlayerKillCredit() && !held.sure) {
			continue
		}
		chance := rules.EquipmentDropChance(victimPieces, killer.AttackerPieces, &ctx, m.lootRand, held.chance)
		if m.lootRand.Float32() >= chance {
			continue
		}
		stack := held.stack
		if !held.sure && registry.StackDamageable(stack) {
			wear := registry.StackMaxDamage(stack)
			stack.Damage = wear - int32(m.lootRand.Intn(1+m.lootRand.Intn(int(math.Max(float64(wear-3), 1)))))
		}
		drops = append(drops, stack)
	}
	return drops
}

// Shearable reports whether shears would shear the mob now: an unsheared
// grown sheep or bogged, a live snow golem with its pumpkin, or a live grown unsheared mooshroom
// (Shearable.readyForShearing returns !isBaby; Mob.interact refuses the
// dead).
func (m *Mob) Shearable() bool {
	switch m.Type {
	case TypeSulfurCube:
		return m.Alive() && m.SulfurCubeFilled()
	case TypeBogged:
		return !m.Sheared
	case TypeSheep:
		return !m.Sheared && !m.IsBaby()
	case TypeMooshroom:
		return !m.Sheared && !m.IsBaby() && !m.Dead
	case TypeCopperGolem:
		// vanilla: CopperGolem.readyForShearing
		return m.Alive() && m.CopperShearable()
	case TypeSnowGolem:
		// Sheared is the pumpkin gone. vanilla: SnowGolem.readyForShearing
		return !m.Sheared && m.Alive()
	}
	return false
}

// RollShear rolls the kind's shearing table with the shears as tool: a sheep
// by colour, a mooshroom by variant. Rolls use the mob's dice, so the same
// seed replays its shearing. The caller marks the mob sheared after the
// roll, as Sheep.shear does, because the sheep table requires the fleece.
func (m *Mob) RollShear(shears protocol.ItemStack, level rules.LootLevel) []protocol.ItemStack {
	if m.Type == TypeSulfurCube {
		stack := m.Body
		m.SetEquipment(6, protocol.ItemStack{})
		m.sulfur.pickupTimer = 100 // SulfurCube.shear
		return []protocol.ItemStack{stack}
	}
	ctx := rules.LootContext{
		Tool:  rules.ToolOf(shears),
		Level: level,
		This: rules.LootEntity{
			Type:        m.Type.Name(),
			Sheep:       m.Type == TypeSheep,
			SheepColour: m.Colour,
			Sheared:     m.Sheared,
			Mooshroom:   m.Mushroom,
		},
	}
	return rules.RollLoot("minecraft:shearing/"+strings.TrimPrefix(m.Type.Name(), "minecraft:"), &ctx, m.Rand)
}

// lootSeed derives the loot stream's seed from the mob's seed with
// splitmix64's finaliser, so the streams are unrelated.
func lootSeed(seed int64) int64 {
	z := uint64(seed) + 0x9E3779B97F4A7C15
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return int64(z ^ z>>31)
}

// lootLevel is w as the level of a roll at the mob's position.
func (m *Mob) lootLevel(w World) rules.LootLevel {
	return rules.LevelAt(w, w.LootReads(), m.X, m.Y, m.Z)
}

// projectileOf is the entity type of the projectile that carries a damage
// type; "" where the type names none. A fireball is a small one from a blaze
// and a large one from anyone else (small fireballs never have a player
// owner).
// vanilla: LargeFireball.onHitEntity, SmallFireball.onHitEntity
func projectileOf(damage registry.DamageType, attacker registry.EntityType) string {
	switch damage {
	case rules.DamageTypeArrow:
		return "minecraft:arrow"
	case rules.DamageTypeTrident:
		return "minecraft:trident"
	case rules.DamageTypeWitherSkull:
		return "minecraft:wither_skull"
	case rules.DamageTypeFireball:
		if attacker == TypeBlaze {
			return "minecraft:small_fireball"
		}
		return "minecraft:fireball"
	}
	return ""
}
