package entity

import (
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Spawn equipment: the armour roll of Mob.populateDefaultEquipmentSlots,
// the zombie family's iron tool on top, and the drowned's override (trident
// or fishing rod only). Also loot pickup for CanPickUpLoot: the reach, the
// slot a stack goes in, and which of two pieces is kept.

// The armour slots as Worn indexes them: EquipmentSlot ordinals after the
// hands.
const (
	wornFeet  = 0
	wornLegs  = 1
	wornChest = 2
	wornHead  = 3
)

// ArmorSlotNames are the armour slot names shared by Set Equipment, the
// saved equipment compound and attribute modifiers, in Worn order.
var ArmorSlotNames = [...]string{"feet", "legs", "chest", "head"}

// populationOrder is EQUIPMENT_POPULATION_ORDER, head first, feet last.
var populationOrder = [4]int{wornHead, wornChest, wornLegs, wornFeet}

// spawnArmour is getEquipmentForSlot's table by material (leather, copper,
// golden, chainmail, iron, diamond) and slot.
var spawnArmour = [6][4]registry.Item{
	{
		registry.ItemLeatherBoots, registry.ItemLeatherLeggings,
		registry.ItemLeatherChestplate, registry.ItemLeatherHelmet,
	},
	{
		registry.ItemCopperBoots, registry.ItemCopperLeggings,
		registry.ItemCopperChestplate, registry.ItemCopperHelmet,
	},
	{
		registry.ItemGoldenBoots, registry.ItemGoldenLeggings,
		registry.ItemGoldenChestplate, registry.ItemGoldenHelmet,
	},
	{
		registry.ItemChainmailBoots, registry.ItemChainmailLeggings,
		registry.ItemChainmailChestplate, registry.ItemChainmailHelmet,
	},
	{
		registry.ItemIronBoots, registry.ItemIronLeggings,
		registry.ItemIronChestplate, registry.ItemIronHelmet,
	},
	{
		registry.ItemDiamondBoots, registry.ItemDiamondLeggings,
		registry.ItemDiamondChestplate, registry.ItemDiamondHelmet,
	},
}

// Mob.populateDefaultEquipmentSlots' numbers: the share of the special
// multiplier the roll needs, the odds of each of the three one-step
// material lifts (base draw from the bottom three), and the odds the walk
// down the body stops before the next piece (a tenth on hard, a quarter
// otherwise).
const (
	armourRollOdds = 0.15
	armourLiftOdds = 0.1087
	armourStopHard = 0.1
	armourStopOdds = 0.25
)

// rollSpawnArmour is Mob.populateDefaultEquipmentSlots: on the special
// multiplier's share, a material from the bottom three lifted by up to three
// rolls, then pieces head down, the head always and each lower one only if
// the stop roll before it failed. Draws come from the dice the caller
// passes.
// vanilla: Mob.populateDefaultEquipmentSlots, getEquipmentForSlot
func (m *Mob) rollSpawnArmour(dice Dice, local rules.LocalDifficulty) {
	if dice.NextFloat() >= armourRollOdds*local.Special {
		return
	}
	tier := int(dice.NextIntN(3))
	for range 3 {
		if dice.NextFloat() < armourLiftOdds {
			tier++
		}
	}
	stop := float32(armourStopOdds)
	if local.Hard {
		stop = armourStopHard
	}
	for i, slot := range populationOrder {
		if i > 0 && dice.NextFloat() < stop {
			return
		}
		if !m.Worn[slot].Empty() {
			continue
		}
		m.SetEquipment(protocol.EquipFeet+slot, protocol.ItemStack{ItemID: int32(spawnArmour[tier][slot]), Count: 1})
	}
}

// Zombie.populateDefaultEquipmentSlots' iron tool: 1 in 100 (1 in 20 on
// hard), and of its six-faced die one sword, one spear, four shovel.
const (
	zombieToolOdds     = 0.01
	zombieToolOddsHard = 0.05
	zombieToolDie      = 6
)

// rollZombieTool is Zombie.populateDefaultEquipmentSlots' tool half, after
// the armour, on the same dice.
func (m *Mob) rollZombieTool(dice Dice, local rules.LocalDifficulty) {
	odds := float32(zombieToolOdds)
	if local.Hard {
		odds = zombieToolOddsHard
	}
	if dice.NextFloat() >= odds {
		return
	}
	tool := int32(registry.ItemIronShovel)
	switch dice.NextIntN(zombieToolDie) {
	case 0:
		tool = int32(registry.ItemIronSword)
	case 1:
		tool = int32(registry.ItemIronSpear)
	}
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: tool, Count: 1})
}

// Drowned.populateDefaultEquipmentSlots and finalizeSpawn odds: 1 in 10 holds
// something, 10 in 16 of those a trident and the rest a fishing rod, and 3
// in 100 carry a nautilus shell that always drops.
const (
	drownedArmedOdds    = 0.9
	drownedTridentShare = 10
	drownedShellOdds    = 0.03
)

// populateEquipment is the zombie family's populateDefaultEquipmentSlots
// from finalizeSpawn. The drowned's override fills its hands only; the rest
// of the family roll the base armour and the zombie's iron tool.
// vanilla: Zombie.finalizeSpawn, Drowned.populateDefaultEquipmentSlots
func (m *Mob) populateEquipment(dice Dice, local rules.LocalDifficulty) {
	switch m.Type {
	case TypeDrowned:
		m.rollDrownedHands(dice)
	case TypeZombifiedPiglin:
		m.rollPiglinHand(dice)
	default:
		m.rollSpawnArmour(dice, local)
		m.rollZombieTool(dice, local)
	}
	m.rollSpawnEnchantments(dice, local)
}

// Enchantment roll numbers: the special-multiplier share for the main hand
// and each armour piece.
// vanilla: Mob.enchantSpawnedWeapon, enchantSpawnedArmor
const (
	spawnWeaponEnchantOdds = 0.25
	spawnArmourEnchantOdds = 0.5
)

// rollSpawnEnchantments is Mob.populateDefaultEquipmentEnchantments: the
// main hand, then armour feet to head, each enchanted on its multiplier
// share by the mob_spawn_equipment provider. Draws and enchantment
// selection both run on the dice the caller passes.
// vanilla: Mob.populateDefaultEquipmentEnchantments, EnchantmentsByCostWithDifficulty.enchant
func (m *Mob) rollSpawnEnchantments(dice Dice, local rules.LocalDifficulty) {
	r := diceEnchantRandom{dice}
	enchant := func(stack *protocol.ItemStack, odds float32) {
		if stack.Empty() || dice.NextFloat() >= odds*local.Special {
			return
		}
		enchantForSpawn(r, stack, local)
	}
	stack := m.MainHand
	enchant(&stack, spawnWeaponEnchantOdds)
	m.SetEquipment(protocol.EquipMainHand, stack)
	for _, slot := range [4]int{wornFeet, wornLegs, wornChest, wornHead} {
		stack = m.Worn[slot]
		enchant(&stack, spawnArmourEnchantOdds)
		m.SetEquipment(protocol.EquipFeet+slot, stack)
	}
}

// diceEnchantRandom reads Dice as the enchantment selection's random.
type diceEnchantRandom struct{ Dice }

// NextIntBound is Dice.NextIntN.
func (d diceEnchantRandom) NextIntBound(bound int32) int32 { return d.NextIntN(bound) }

// enchantForSpawn enchants the stack by the mob_spawn_equipment provider.
// vanilla: VanillaEnchantmentProviders.MOB_SPAWN_EQUIPMENT
func enchantForSpawn(r rules.EnchantRandom, stack *protocol.ItemStack, local rules.LocalDifficulty) {
	enchantByKey("minecraft:mob_spawn_equipment", r, stack, local.Special)
}

// enchantByKey enchants the stack by the enchantment provider registered under
// key; an unknown key leaves it alone.
func enchantByKey(key string, r rules.EnchantRandom, stack *protocol.ItemStack, special float32) {
	if p, ok := registry.EnchantmentProviderByName(key); ok {
		rules.EnchantByProvider(p, r, stack, special)
	}
}

// Halloween head numbers: the share of bare heads that get one, and the
// share of those that are jack o'lanterns.
// vanilla: Zombie.finalizeSpawn, AbstractSkeleton.finalizeSpawn
const (
	halloweenHeadOdds    = 0.25
	halloweenLanternOdds = 0.1
	halloweenSalt        = 0x3B7A1C95E4D2086F
)

// rollHalloweenHead puts a carved pumpkin (a jack o'lantern one time in
// ten) on a bare head on Halloween; it never drops.
// vanilla: Zombie.finalizeSpawn, AbstractSkeleton.finalizeSpawn
func (m *Mob) rollHalloweenHead(dice Dice, local rules.LocalDifficulty) {
	if !local.Halloween || !m.Worn[wornHead].Empty() {
		return
	}
	if dice.NextFloat() >= halloweenHeadOdds {
		return
	}
	item := int32(registry.ItemCarvedPumpkin)
	if dice.NextFloat() < halloweenLanternOdds {
		item = int32(registry.ItemJackOLantern)
	}
	m.SetEquipment(protocol.EquipFeet+wornHead, protocol.ItemStack{ItemID: item, Count: 1})
	m.DropChances[2+wornHead] = 0
}

// equipmentSalt separates the equipment roll's seed from the loot and
// left-handed streams.
const equipmentSalt = 0x1D8E4C6A2F09B7C3

// equipmentDice is the mob's own equipment stream, apart from its dice and
// loot.
func (m *Mob) equipmentDice() Dice { return GoDice{m.equipmentRand()} }

// equipmentRand is the same stream as a math/rand source, for the kinds that
// draw their own fields from it too.
func (m *Mob) equipmentRand() *rand.Rand {
	return rand.New(rand.NewSource(m.lootSeed ^ equipmentSalt))
}

// halloweenDice is the skeleton's own Halloween head stream.
func (m *Mob) halloweenDice() Dice {
	return GoDice{rand.New(rand.NewSource(m.lootSeed ^ halloweenSalt))}
}

// rollDrownedHands is Drowned.populateDefaultEquipmentSlots: a trident or
// fishing rod, no armour roll.
// vanilla: Drowned.populateDefaultEquipmentSlots
func (m *Mob) rollDrownedHands(dice Dice) {
	if float64(dice.NextFloat()) > drownedArmedOdds {
		if dice.NextIntN(16) < drownedTridentShare {
			m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1})
		} else {
			m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1})
		}
	}
}

// LootItem is an item entity as the loot pass sees it, read under the
// caller's lock.
type LootItem struct {
	X, Y, Z float64
	Stack   protocol.ItemStack
}

// LootTake is an item a mob decided to take: its index among those passed
// in, the slot name its piece goes in, and the count.
type LootTake struct {
	Mob   *Mob
	Item  int
	Slot  string
	Count int32
	// Banner marks a raider taking the captain's banner, which does not make
	// it persistent.
	// vanilla: Raider.pickUpItem
	Banner bool
}

// lootReach is the pickup box's growth: one block horizontally, none
// vertically.
// vanilla: Mob.aiStep's ITEM_PICKUP_REACH
const lootReach = 1

func lootPickupBox(m *Mob) AABB {
	box := m.mobBox()
	// vanilla: Allay.getPickupReach is (1, 1, 1)
	if m.Type == TypeAllay {
		return box.Inflate(lootReach)
	}
	box.minX -= lootReach
	box.maxX += lootReach
	box.minZ -= lootReach
	box.maxZ += lootReach
	return box
}

func (loot *LootItem) box() AABB {
	return AABB{
		loot.X - halfSize, loot.Y - halfSize, loot.Z - halfSize,
		loot.X + halfSize, loot.Y + halfSize, loot.Z + halfSize,
	}
}

func lootWithinReach(box AABB, loot *LootItem) bool {
	return box.Crosses(loot.box())
}

func (m *Mob) lootPickupEligible() bool {
	return m.LootPickupEnabled() && m.Alive() &&
		(m.Type != TypeSulfurCube || !m.IsBaby() && !m.NoAI && m.sulfur.pickupTimer <= 0)
}

func (mobs Mobs) lootGrid(items []LootItem) *BoxGrid {
	const minPickers, minItems = 8, 32
	if len(mobs) < minPickers || len(items) < minItems {
		return nil
	}
	eligible := 0
	for _, m := range mobs {
		if m.lootPickupEligible() {
			eligible++
			if eligible == minPickers {
				break
			}
		}
	}
	if eligible < minPickers {
		return nil
	}
	grid := &BoxGrid{}
	for i := range items {
		if loot := &items[i]; !loot.Stack.Empty() {
			grid.Add(int32(i), loot.box())
		}
	}
	return grid
}

// TakeLoot decides one tick's pickups: each live body with CanPickUpLoot
// takes the items in reach it can hold or wear better. Nothing is equipped
// here; the caller claims each take first, so an item lost in between
// equips nobody.
// vanilla: Mob.aiStep's looting, pickUpItem, equipItemIfPossible
func (mobs Mobs) TakeLoot(items []LootItem) []LootTake {
	grid := mobs.lootGrid(items)
	var takes []LootTake
	for _, m := range mobs {
		if !m.lootPickupEligible() {
			continue
		}
		box := lootPickupBox(m)
		staged := stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
		swallowed := false
		take := func(i int) {
			loot := &items[i]
			if swallowed || loot.Stack.Empty() || !lootWithinReach(box, loot) {
				return
			}
			if m.Type == TypeSulfurCube {
				if registry.Item(loot.Stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable) {
					takes = append(takes, LootTake{Mob: m, Item: i, Slot: "body", Count: 1})
					swallowed = true
				}
				return
			}
			if slot, count, ok := staged.take(m, loot.Stack); ok {
				takes = append(takes, LootTake{Mob: m, Item: i, Slot: slot, Count: count})
			}
		}
		if grid != nil {
			grid.Query(box, func(i int32) { take(int(i)) })
			continue
		}
		for i := range items {
			take(i)
			if swallowed {
				break
			}
		}
	}
	return takes
}

// LootSlotNames are the six mob slots in equipment and DropChances order:
// hands, then armour feet to head.
var LootSlotNames = [6]string{"mainhand", "offhand", "feet", "legs", "chest", "head"}

// GuaranteedDropChance is DropChances.PRESERVE_ITEM_DROP_CHANCE: above 1, so
// a player-fitted or picked-up piece drops without a player kill.
const GuaranteedDropChance = 2

const guaranteedDropChance = GuaranteedDropChance

// lootSlotIndex is a slot name's equipment index, -1 for a non-mob slot.
func lootSlotIndex(slot string) int {
	for i, name := range LootSlotNames {
		if name == slot {
			return i
		}
	}
	return -1
}

// slotStack reads one equipment slot by its index.
func (m *Mob) slotStack(i int) protocol.ItemStack {
	switch i {
	case 0:
		return m.MainHand
	case 1:
		return m.OffHand
	}
	return m.Worn[i-2]
}

// LootSlotStack reads the equipment slot named in LootSlotNames; ok is false
// for any other name.
func (m *Mob) LootSlotStack(slot string) (stack protocol.ItemStack, ok bool) {
	i := lootSlotIndex(slot)
	if i < 0 {
		return protocol.ItemStack{}, false
	}
	return m.slotStack(i), true
}

// EquipmentRevision counts writes to the mob's equipment, including durability
// and component updates. The caller holds the same lock as the mob's state.
func (m *Mob) EquipmentRevision() uint64 { return m.equipmentRevision }

// EquipmentSlot reads a Set Equipment slot: hands, feet through head, then body.
func (m *Mob) EquipmentSlot(slot int) protocol.ItemStack {
	if slot == 6 {
		return m.Body
	}
	return m.slotStack(slot)
}

// SetEquipment replaces a Set Equipment slot and invalidates its tracking snapshot.
// In-place stack updates must pass the modified copy here too.
func (m *Mob) SetEquipment(slot int, stack protocol.ItemStack) {
	switch slot {
	case protocol.EquipMainHand:
		m.MainHand = stack
	case protocol.EquipOffHand:
		m.OffHand = stack
	case 6:
		m.Body = stack
	default:
		m.Worn[slot-protocol.EquipFeet] = stack
	}
	m.equipmentRevision++
}

// setSlotStack dresses one loot slot by its index.
func (m *Mob) setSlotStack(i int, stack protocol.ItemStack) { m.SetEquipment(i, stack) }

// dropChance is DropChances.byEquipment: 2 for a picked-up slot, else the
// default 0.085 unless a save or command set it.
func (m *Mob) dropChance(i int) float32 {
	return m.DropChances[i]
}

// TookLoot equips a claimed take: sets the slot, returns the displaced piece
// for the caller to drop, marks the slot a guaranteed drop, and makes the
// body persistent (as every equip does). The displaced piece drops on
// vanilla's roll, which a picked-up slot's guarantee makes certain.
// vanilla: Mob.equipItemIfPossible, setItemSlotAndDropWhenKilled
func (m *Mob) TookLoot(take LootTake, stack protocol.ItemStack) protocol.ItemStack {
	if !m.Alive() {
		return protocol.ItemStack{}
	}
	if m.Type == TypeFox {
		m.Fox.TicksSinceEaten = 0
	}
	if m.Type == TypeAllay {
		// The sensed item is gone until the next scan.
		m.barter.hasWanted = false
	}
	if m.Type == TypeSulfurCube && take.Slot == "body" {
		_, _ = m.AbsorbSulfurBlock(stack)
		return protocol.ItemStack{}
	}
	if take.Slot == "eat" {
		// vanilla: PiglinAi.eat
		m.barter.ate = piglinEatCooldown
		return protocol.ItemStack{}
	}
	if take.Slot == "inventory" {
		stack.Count = take.Count
		return m.addToInventory(stack)
	}
	i := lootSlotIndex(take.Slot)
	if i < 0 {
		return protocol.ItemStack{}
	}
	// A shield also lands in the off hand but is worn, not admired.
	// vanilla: PiglinAi.pickUpItem
	if m.isPiglin() && take.Slot == "offhand" && isLoved(stack) {
		m.barter.admiring = piglinAdmireTicks
	}
	// A piglin holding anything but the bartering currency becomes persistent.
	// vanilla: Piglin.holdInOffHand
	keepsPersistence := m.isPiglin() && stack.ItemID == int32(registry.ItemGoldIngot)
	if keepsPersistence || m.Type == TypeDolphin || m.Type == TypePanda || take.Banner {
		defer func(persistent bool) { m.Persistence = persistent }(m.Persistence)
	}
	var dropped protocol.ItemStack
	if current := m.slotStack(i); !current.Empty() {
		// A fox spits what its mouth held, unrolled.
		// vanilla: Fox.pickUpItem, Fox.spitOutItem
		if m.Type == TypeFox {
			dropped = current
		} else if roll := m.Rand.Float32() - 0.1; max(roll, 0) < m.dropChance(i) {
			// vanilla: Math.max(random.nextFloat()-0.1, 0) < dropChances.byEquipment(slot)
			dropped = current
		}
	}
	equipped := stack
	equipped.Count = take.Count
	m.setSlotStack(i, equipped)
	m.DropChances[i] = guaranteedDropChance
	m.Persistence = true
	return dropped
}

// stagedSlots is a mob's equipment as its loot decisions leave it, so later
// takes in a tick build on earlier ones.
type stagedSlots struct {
	main, off protocol.ItemStack
	worn      [4]protocol.ItemStack
}

// currentOf reads one slot's stack, named as the saves name them.
func (s *stagedSlots) currentOf(slot string) protocol.ItemStack {
	switch slot {
	case "mainhand":
		return s.main
	case "offhand":
		return s.off
	}
	for i, name := range ArmorSlotNames {
		if name == slot {
			return s.worn[i]
		}
	}
	return protocol.ItemStack{}
}

// set dresses one slot with the stack given.
func (s *stagedSlots) set(slot string, stack protocol.ItemStack) {
	switch slot {
	case "mainhand":
		s.main = stack
	case "offhand":
		s.off = stack
	default:
		for i, name := range ArmorSlotNames {
			if name == slot {
				s.worn[i] = stack
			}
		}
	}
}

// take is equipItemIfPossible for one stack: the slot, the count (hands take
// the stack, armour one) and whether it goes at all; armour that beats
// nothing falls back to an empty main hand. The kind's pickup wishes veto
// first, and a piece for a slot the mob cannot use (an animal's body or
// saddle) is refused.
// vanilla: Mob.equipItemIfPossible, wantsToPickUp, isEquippableInSlot
func (s *stagedSlots) take(m *Mob, stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if m.Type == TypePillager && !registry.IsBannerItem(stack.ItemID) {
		// vanilla: Pillager.pickUpItem only delegates banners to Raider
		return "", 0, false
	}
	if m.Type == TypeDolphin {
		if !s.main.Empty() {
			return "", 0, false
		}
		s.main = stack
		return "mainhand", stack.Count, true
	}
	if m.Type == TypeFox {
		return s.takeFox(m, stack)
	}
	if m.Type == TypePanda {
		return s.takePanda(stack)
	}
	if m.isPiglin() {
		return s.takePiglin(m, stack)
	}
	if m.Type == TypeVillager {
		return s.takeVillager(m, stack)
	}
	if m.Type == TypeAllay {
		return s.takeAllay(m, stack)
	}
	if !m.wantsToPickUp(stack) {
		return "", 0, false
	}
	slot = lootSlotOf(stack)
	if !equippableInMobSlot(stack, slot) {
		return "", 0, false
	}
	current := s.currentOf(slot)
	replace := replaceLoot(m, stack, current, slot)
	if isArmourSlot(slot) && !replace {
		slot = "mainhand"
		current = s.currentOf(slot)
		replace = current.Empty()
	}
	if !replace {
		return "", 0, false
	}
	count = stack.Count
	if isArmourSlot(slot) {
		count = 1
	}
	equipped := stack
	equipped.Count = count
	s.set(slot, equipped)
	return slot, count, true
}

// lootSlotOf is LivingEntity.getEquipmentSlotForItem for a mob: the
// equippable component's slot, else the main hand. equippableInMobSlot
// decides whether the mob can use it.
// vanilla: LivingEntity.getEquipmentSlotForItem
func lootSlotOf(stack protocol.ItemStack) string {
	item, ok := registry.StackEquippable(stack)
	if !ok {
		return "mainhand"
	}
	return item.Slot
}

// equippableInMobSlot is isEquippableInSlot for a mob: without an equippable
// component only the main hand; with one, its slot if the mob has it, else
// (an animal's body or saddle) nowhere.
// vanilla: LivingEntity.isEquippableInSlot
func equippableInMobSlot(stack protocol.ItemStack, slot string) bool {
	item, ok := registry.StackEquippable(stack)
	if !ok {
		return slot == "mainhand"
	}
	return slot == item.Slot && lootSlotIndex(slot) >= 0
}

// isArmourSlot reports whether the slot is one of the four armour slots.
func isArmourSlot(slot string) bool {
	for _, name := range ArmorSlotNames {
		if name == slot {
			return true
		}
	}
	return false
}

// replaceLoot is Mob.canReplaceCurrentItem: an empty slot is always filled;
// armour ranks by protection; a main hand by the kind's preferred weapon
// type, then damage; the off hand only when empty. Binding curse pieces are
// never swapped, and the drowned keeps its nautilus shell. The body's own
// attribute share would matter only for the multiplied operations, which no
// weapon or armour uses, so zero stands in for it.
// vanilla: Mob.canReplaceCurrentItem, compareArmor, compareWeapons,
// Drowned.canReplaceCurrentItem
func replaceLoot(m *Mob, found, current protocol.ItemStack, slot string) bool {
	if current.Empty() {
		return true
	}
	if m.isPiglin() {
		// vanilla: Piglin.canReplaceCurrentItem
		if player.HasBindingCurse(current) {
			return false
		}
		if wanted := m.piglinWants(found); wanted != m.piglinWants(current) {
			return wanted
		}
	}
	if m.Type == TypeDrowned && current.ItemID == int32(registry.ItemNautilusShell) {
		return false
	}
	if isArmourSlot(slot) {
		// vanilla: EnchantmentHelper.has(current, PREVENT_ARMOR_CHANGE)
		if player.HasBindingCurse(current) {
			return false
		}
		if better := attributeBattle(found, current, registry.AttributeArmor, slot); better != 0 {
			return better > 0
		}
		if better := attributeBattle(found, current, registry.AttributeArmorToughness, slot); better != 0 {
			return better > 0
		}
		return replaceEqualLoot(found, current)
	}
	if slot != "mainhand" {
		return false
	}
	if tag := m.preferredWeaponTag(); tag != "" {
		oldPreferred := registry.Item(current.ItemID).InTag(registry.ItemTagOf(tag))
		if oldPreferred != registry.Item(found.ItemID).InTag(registry.ItemTagOf(tag)) {
			return !oldPreferred
		}
	}
	if better := attributeBattle(found, current, registry.AttributeAttackDamage, slot); better != 0 {
		return better > 0
	}
	return replaceEqualLoot(found, current)
}

// attributeBattle compares two stacks by one attribute's approximate value
// in a slot: positive when found is higher, negative when lower, zero for a
// tie.
func attributeBattle(found, current protocol.ItemStack, attribute registry.AttributeID, slot string) float64 {
	return rules.ApproximateAttributeWith(found, attribute, slot, 0) -
		rules.ApproximateAttributeWith(current, attribute, slot, 0)
}

// replaceEqualLoot is Mob.canReplaceEqualItem's tie-break: more
// enchantments, then less wear, then only found being named.
// vanilla: Mob.canReplaceEqualItem
func replaceEqualLoot(found, current protocol.ItemStack) bool {
	if len(found.Enchantments) != len(current.Enchantments) {
		return len(found.Enchantments) > len(current.Enchantments)
	}
	if found.Damage != current.Damage {
		return found.Damage < current.Damage
	}
	return found.CustomName != nil && current.CustomName == nil
}

// preferredWeaponTag is Mob.getPreferredWeaponType: the item tag ranked
// before damage (the skeletons' bows, the drowned's trident); empty
// otherwise.
// vanilla: AbstractSkeleton.getPreferredWeaponType, Drowned.getPreferredWeaponType,
// Piglin.getPreferredWeaponType
func (m *Mob) preferredWeaponTag() string {
	if m.is(TypeSkeleton) && m.Type != TypeWitherSkeleton {
		return "minecraft:skeleton_preferred_weapons"
	}
	if m.Type == TypeDrowned {
		return "minecraft:drowned_preferred_weapons"
	}
	if m.Type == TypePiglin && !m.IsBaby() {
		return "minecraft:piglin_preferred_weapons"
	}
	return ""
}

// piglinWants is Piglin.canReplaceCurrentItem's "wanted": a loved item or
// the preferred weapon.
func (m *Mob) piglinWants(stack protocol.ItemStack) bool {
	tag := m.preferredWeaponTag()
	return isLoved(stack) || tag != "" && registry.Item(stack.ItemID).InTag(registry.ItemTagOf(tag))
}

// wantsToPickUp is Mob.wantsToPickUp with subclass refusals: skeletons and
// the drowned skip spears; the zombie family refuses glow ink sacs, and a
// baby riding a mount refuses eggs too (they are its mount's). The wither
// skeleton refuses bows and crossbows.
// vanilla: Mob.wantsToPickUp, AbstractSkeleton.wantsToPickUp,
// Drowned.wantsToPickUp, Zombie.wantsToPickUp, Zombie.canHoldItem,
// WitherSkeleton.canHoldItem
func (m *Mob) wantsToPickUp(stack protocol.ItemStack) bool {
	if m.Type == TypeWitherSkeleton && registry.Item(stack.ItemID).InTag(registry.ItemTagIDWitherSkeletonDislikedWeapons) {
		return false
	}
	if m.is(TypeZombie) {
		if stack.ItemID == int32(registry.ItemGlowInkSac) {
			return false
		}
		if m.IsBaby() && m.RidesVehicle && registry.Item(stack.ItemID).InTag(registry.ItemTagIDEggs) {
			return false
		}
	}
	if (m.is(TypeSkeleton) || m.Type == TypeDrowned) && registry.Item(stack.ItemID).InTag(registry.ItemTagIDSpears) {
		return false
	}
	return true
}

// EquipRolled equips an equipment table's stacks: each in its item's slot
// (main hand without an equippable component), the first to claim a slot
// keeping it. A slot in chances takes that drop chance.
// vanilla: EquipmentUser.equip, EquipmentUser.resolveSlot
func (m *Mob) EquipRolled(stacks []protocol.ItemStack, chances map[string]float32) {
	var taken []string
	for _, stack := range stacks {
		if stack.Empty() {
			continue
		}
		slot := "mainhand"
		if eq, ok := registry.StackEquippable(stack); ok {
			slot = eq.Slot
		}
		if slices.Contains(taken, slot) {
			continue
		}
		taken = append(taken, slot)
		i := lootSlotIndex(slot)
		if i < 0 {
			// body and saddle: slots this mob model does not hold
			continue
		}
		m.setSlotStack(i, stack)
		if chance, ok := chances[slot]; ok {
			m.DropChances[i] = chance
		}
	}
}

// CanUseSlot reports whether the mob has a usable "body" or "saddle" slot;
// any other slot is usable.
// vanilla: LivingEntity.canUseSlot and the overrides in AbstractHorse, Horse,
// SkeletonHorse, ZombieHorse, Llama, Pig, Strider, AbstractNautilus, HappyGhast,
// SulfurCube
func (m *Mob) CanUseSlot(slot string) bool {
	grown := m.Alive() && !m.IsBaby()
	switch {
	case m.Type == TypeHorse || IsUndeadHorse(m.Type) || IsLlama(m.Type):
		return true
	case IsNautilus(m.Type):
		return slot != "saddle" && slot != "body" || m.NautilusCanEquip()
	case slot == "saddle" && IsEquine(m.Type):
		// vanilla: Camel.isTamed is always true
		return grown && (m.Horse.Tame || IsCamel(m.Type))
	case slot == "saddle" && (m.Type == TypePig || m.IsStrider()):
		return grown
	case slot == "body" && (m.Type == TypeHappyGhast || m.Type == TypeSulfurCube):
		return grown
	}
	return true
}

// TakesInSlot reports whether /item may put the stack in the mob's "body" or
// "saddle" slot: an empty stack, or an item the mob assigns to that slot.
// Unlike a click, the piece's allowed entities are not asked.
// vanilla: LivingEntity.createEquipmentSlotAccess, SulfurCube.getEquipmentSlotForItem
func (m *Mob) TakesInSlot(slot string, stack protocol.ItemStack) bool {
	if stack.Empty() {
		return true
	}
	if m.Type == TypeSulfurCube && registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable) {
		// The slot override precedes the equippable component and its age gate.
		return slot == "body"
	}
	item, ok := registry.StackEquippable(stack)
	return ok && item.Slot == slot && m.CanUseSlot(slot)
}
