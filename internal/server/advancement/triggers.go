package advancement

import (
	"slices"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The triggers this server fires. vanilla: CriteriaTriggers
const (
	TriggerInventoryChanged       = "minecraft:inventory_changed"
	TriggerChangedDimension       = "minecraft:changed_dimension"
	TriggerPlayerKilledEntity     = "minecraft:player_killed_entity"
	TriggerEntityKilledPlayer     = "minecraft:entity_killed_player"
	TriggerLocation               = "minecraft:location"
	TriggerConsumeItem            = "minecraft:consume_item"
	TriggerEnchantedItem          = "minecraft:enchanted_item"
	TriggerBredAnimals            = "minecraft:bred_animals"
	TriggerPlacedBlock            = "minecraft:placed_block"
	TriggerItemUsedOnBlock        = "minecraft:item_used_on_block"
	TriggerTameAnimal             = "minecraft:tame_animal"
	TriggerFilledBucket           = "minecraft:filled_bucket"
	TriggerEffectsChanged         = "minecraft:effects_changed"
	TriggerSummonedEntity         = "minecraft:summoned_entity"
	TriggerSleptInBed             = "minecraft:slept_in_bed"
	TriggerTick                   = "minecraft:tick"
	TriggerShotCrossbow           = "minecraft:shot_crossbow"
	TriggerVillagerTrade          = "minecraft:villager_trade"
	TriggerCuredZombieVillager    = "minecraft:cured_zombie_villager"
	TriggerConstructBeacon        = "minecraft:construct_beacon"
	TriggerBrewedPotion           = "minecraft:brewed_potion"
	TriggerFishingRodHooked       = "minecraft:fishing_rod_hooked"
	TriggerPlayerHurtEntity       = "minecraft:player_hurt_entity"
	TriggerKilledByArrow          = "minecraft:killed_by_arrow"
	TriggerKillMobNearCatalyst    = "minecraft:kill_mob_near_sculk_catalyst"
	TriggerInteractedEntity       = "minecraft:player_interacted_with_entity"
	TriggerShearedEquipment       = "minecraft:player_sheared_equipment"
	TriggerContainerLoot          = "minecraft:player_generates_container_loot"
	TriggerNetherTravel           = "minecraft:nether_travel"
	TriggerFallFromHeight         = "minecraft:fall_from_height"
	TriggerLevitation             = "minecraft:levitation"
	TriggerEnterBlock             = "minecraft:enter_block"
	TriggerUsedTotem              = "minecraft:used_totem"
	TriggerEntityHurtPlayer       = "minecraft:entity_hurt_player"
	TriggerTargetHit              = "minecraft:target_hit"
	TriggerStartedRiding          = "minecraft:started_riding"
	TriggerItemDurability         = "minecraft:item_durability_changed"
	TriggerRideInLava             = "minecraft:ride_entity_in_lava"
	TriggerRecipeCrafted          = "minecraft:recipe_crafted"
	TriggerCrafterRecipe          = "minecraft:crafter_recipe_crafted"
	TriggerSlideDownBlock         = "minecraft:slide_down_block"
	TriggerUsingItem              = "minecraft:using_item"
	TriggerHeroOfTheVillage       = "minecraft:hero_of_the_village"
	TriggerBeeNestDestroyed       = "minecraft:bee_nest_destroyed"
	TriggerAvoidVibration         = "minecraft:avoid_vibration"
	TriggerFallAfterExplosion     = "minecraft:fall_after_explosion"
	TriggerLightningStrike        = "minecraft:lightning_strike"
	TriggerThrownPickedUpByPlayer = "minecraft:thrown_item_picked_up_by_player"
	TriggerThrownPickedUpByEntity = "minecraft:thrown_item_picked_up_by_entity"
	TriggerAllayDropItemOnBlock   = "minecraft:allay_drop_item_on_block"
	TriggerChanneledLightning     = "minecraft:channeled_lightning"
	TriggerSpearMobs              = "minecraft:spear_mobs"
	TriggerRecipeUnlocked         = "minecraft:recipe_unlocked"
	TriggerUsedEnderEye           = "minecraft:used_ender_eye"
	TriggerRaidOmen               = "minecraft:voluntary_exile"
	TriggerDefaultBlockUse        = "minecraft:default_block_use"
	TriggerAnyBlockUse            = "minecraft:any_block_use"
)

// trigger awards each unobtained modelled criterion of trigger, on an
// advancement not done, that match passes; it reports whether any was.
// vanilla: SimpleCriterionTrigger.trigger, PlayerAdvancements.registerListeners
func (p *Progress) trigger(trigger string, at time.Time, match func(c *registry.AdvancementCriterion) bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fire(p.tree.byTrigger[trigger], at, match)
}

// fire is trigger over the given criteria, with p.mu held.
func (p *Progress) fire(refs []criterionRef, at time.Time, match func(c *registry.AdvancementCriterion) bool) bool {
	awarded := false
	for _, ref := range refs {
		n, c := ref.n, &ref.n.Criteria[ref.i]
		if p.isDone(n) {
			continue
		}
		if _, ok := p.obtained[n.index][c.Name]; ok {
			continue
		}
		if match(c) && p.award(n, c.Name, at) {
			awarded = true
		}
	}
	return awarded
}

// Inventory is what inventory_changed reads of the player's inventory: the
// item IDs held and changed, and the counts of occupied, full and empty slots.
// vanilla: InventoryChangeTrigger.trigger
type Inventory struct {
	Held                  []int32
	Changed               []int32
	Occupied, Full, Empty int32
}

// inventorySize is vanilla's Inventory.getContainerSize: 36 main and hotbar
// slots and 7 equipment ones, the body and saddle of which a player never
// fills, so they count as empty.
const inventorySize = 43

// InventoryOf reads inventory_changed's view of slots, the player's armour,
// main, hotbar and off-hand slots, building Held in buf's storage. A
// window's crafting squares are not part of it.
// vanilla: InventoryChangeTrigger.trigger
func InventoryOf(slots []protocol.ItemStack, buf []int32) Inventory {
	inv := Inventory{Held: buf[:0]}
	for _, stack := range slots {
		if stack.Empty() {
			continue
		}
		inv.Occupied++
		if stack.Count >= registry.StackMaxStackSize(stack) {
			inv.Full++
		}
		if !slices.Contains(inv.Held, stack.ItemID) {
			inv.Held = append(inv.Held, stack.ItemID)
		}
	}
	inv.Empty = inventorySize - inv.Occupied
	return inv
}

// Same reports whether two reads found the same inventory, so firing the
// second can be skipped.
func (i Inventory) Same(o Inventory) bool {
	return slices.Equal(i.Held, o.Held) && slices.Equal(i.Changed, o.Changed) && i.Occupied == o.Occupied && i.Full == o.Full
}

// InventoryChanged fires inventory_changed: a criterion passes when the slot
// counts are within its bounds and each item predicate accepts a held item,
// or a changed stack when there is just one predicate.
// vanilla: InventoryChangeTrigger.TriggerInstance.matches
func (p *Progress) InventoryChanged(inv Inventory, at time.Time) bool {
	match := func(c *registry.AdvancementCriterion) bool {
		if !c.Occupied.Matches(float64(inv.Occupied)) || !c.Full.Matches(float64(inv.Full)) ||
			!c.Empty.Matches(float64(inv.Empty)) {
			return false
		}
		items := inv.Held
		if len(c.Items) == 1 {
			items = inv.Changed
		}
		for _, predicate := range c.Items {
			if !slices.ContainsFunc(items, func(id int32) bool { return slices.Contains(predicate, id) }) {
				return false
			}
		}
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	awarded := p.fire(p.tree.heldAny, at, match)
	for _, id := range inv.Changed {
		if p.fire(p.tree.heldBy[id], at, match) {
			awarded = true
		}
	}
	return awarded
}

// ChangedDimension fires changed_dimension for a crossing between the two
// dimensions, by name. vanilla: ChangeDimensionTrigger.TriggerInstance.matches
func (p *Progress) ChangedDimension(from, to string, at time.Time) bool {
	return p.trigger(TriggerChangedDimension, at, func(c *registry.AdvancementCriterion) bool {
		return (c.From == "" || c.From == from) && (c.To == "" || c.To == to)
	})
}

// Entity is what the entity predicates of a trigger read of an entity: its
// type name and the dimension it stands in. Direct is the projectile entity
// type that dealt a killed entity's last blow, empty for none.
type Entity struct {
	Type, Dimension, Direct string
	// Captain is a raider wearing the ominous banner.
	Captain bool
	// Baby is a mob in its baby age.
	Baby bool
	// Distance is the horizontal distance in blocks from the killing player.
	Distance float64
}

// typeMatches reports whether an entity type name passes a criterion's
// entity-type condition: empty for any, a name, or "#" and a tag.
// vanilla: EntityTypePredicate.matches
func typeMatches(want, got string) bool {
	if want == "" {
		return true
	}
	tag, isTag := strings.CutPrefix(want, "#")
	if !isTag {
		return want == got
	}
	id, ok := registry.EntityTypeID(got)
	return ok && registry.EntityTypeInTag(id, tag)
}

// PlayerKilledEntity fires player_killed_entity for the player's kill of
// victim. vanilla: KilledTrigger.TriggerInstance.matches
func (p *Progress) PlayerKilledEntity(victim Entity, at time.Time) bool {
	return p.trigger(TriggerPlayerKilledEntity, at, func(c *registry.AdvancementCriterion) bool {
		return typeMatches(c.Entity, victim.Type) && (c.Dimension == "" || c.Dimension == victim.Dimension) &&
			(c.Direct == "" || victim.Direct != "" && typeMatches(c.Direct, victim.Direct)) &&
			(!c.Captain || victim.Captain) && (!c.Projectile || victim.Direct != "") &&
			victim.Distance >= c.MinDistance
	})
}

// HeroOfTheVillage fires hero_of_the_village, whose advancement sets no
// condition. vanilla: CriteriaTriggers.RAID_WIN
func (p *Progress) HeroOfTheVillage(at time.Time) bool {
	return p.trigger(TriggerHeroOfTheVillage, at, func(*registry.AdvancementCriterion) bool { return true })
}

// BeeNestDestroyed fires bee_nest_destroyed for a hive block broken with a
// tool carrying enchantments, with bees stored inside.
// vanilla: BeehiveBlock.playerDestroy, BeeNestDestroyedTrigger.TriggerInstance.matches
func (p *Progress) BeeNestDestroyed(block string, enchantments map[int32]int32, bees int, at time.Time) bool {
	return p.trigger(TriggerBeeNestDestroyed, at, func(c *registry.AdvancementCriterion) bool {
		return (c.Block == "" || c.Block == block) &&
			(c.Enchantment == "" || hasEnchantment(enchantments, c.Enchantment)) &&
			(c.Bees == 0 || int(c.Bees) == bees)
	})
}

func hasEnchantment(enchantments map[int32]int32, name string) bool {
	id, ok := registry.EnchantmentID(name)
	return ok && enchantments[id] >= 1
}

// AvoidVibration fires avoid_vibration, whose advancement sets no condition.
// vanilla: CriteriaTriggers.AVOID_VIBRATION
func (p *Progress) AvoidVibration(at time.Time) bool {
	return p.trigger(TriggerAvoidVibration, at, func(*registry.AdvancementCriterion) bool { return true })
}

// SlideDownBlock fires slide_down_block, whose advancement sets no condition.
// vanilla: CriteriaTriggers.HONEY_BLOCK_SLIDE
func (p *Progress) SlideDownBlock(at time.Time) bool {
	return p.trigger(TriggerSlideDownBlock, at, func(*registry.AdvancementCriterion) bool { return true })
}

// KillMobNearSculkCatalyst fires kill_mob_near_sculk_catalyst, whose
// advancement sets no condition. vanilla: CriteriaTriggers.KILL_MOB_NEAR_SCULK_CATALYST
func (p *Progress) KillMobNearSculkCatalyst(at time.Time) bool {
	return p.trigger(TriggerKillMobNearCatalyst, at, func(*registry.AdvancementCriterion) bool { return true })
}

// EntityKilledPlayer fires entity_killed_player for a death at the hands of
// an entity. vanilla: KilledTrigger.TriggerInstance.matches
func (p *Progress) EntityKilledPlayer(at time.Time) bool {
	return p.trigger(TriggerEntityKilledPlayer, at, func(*registry.AdvancementCriterion) bool { return true })
}

// Standing is what a location criterion tests of the player besides where it
// is: Feet the feet slot's item ID (0 when empty), and On the state of the
// block it stands on, which counts only while OnGround.
type Standing struct {
	Feet     int32
	OnGround bool
	On       uint16
}

// Location fires location for the biome and structure pieces at the player.
// vanilla: PlayerTrigger.trigger, LocationPredicate.matches,
// EquipmentPredicate.matches, SteppingOnPredicate.matches
func (p *Progress) Location(biome string, inStructure func([]string) bool, s Standing, at time.Time) bool {
	return p.trigger(TriggerLocation, at, func(c *registry.AdvancementCriterion) bool {
		return (c.Biome == "" || c.Biome == biome) &&
			(c.Structures == nil || inStructure != nil && inStructure(c.Structures)) &&
			(c.Feet == nil || slices.Contains(c.Feet, s.Feet)) &&
			(c.SteppingOn == nil || s.OnGround && blockNamed(c.SteppingOn, s.On))
	})
}

// ConsumeItem fires consume_item for a finished eat or drink of the item.
// vanilla: ConsumeItemTrigger.TriggerInstance.matches
func (p *Progress) ConsumeItem(item int32, at time.Time) bool {
	return p.trigger(TriggerConsumeItem, at, func(c *registry.AdvancementCriterion) bool {
		return len(c.Items) == 0 || slices.Contains(c.Items[0], item)
	})
}

// EnchantedItem fires enchanted_item for an item enchanted at an enchanting
// table. vanilla: EnchantedItemTrigger.TriggerInstance.matches
func (p *Progress) EnchantedItem(at time.Time) bool {
	return p.trigger(TriggerEnchantedItem, at, func(*registry.AdvancementCriterion) bool { return true })
}

// BredAnimals fires bred_animals, the parents matching in either order. child
// is empty for a breeding without one (the turtle's egg), which no child
// condition accepts.
// vanilla: BredAnimalsTrigger.TriggerInstance.matches
func (p *Progress) BredAnimals(parent, partner, child string, at time.Time) bool {
	return p.trigger(TriggerBredAnimals, at, func(c *registry.AdvancementCriterion) bool {
		if c.Child != "" && !typeMatches(c.Child, child) {
			return false
		}
		return typeMatches(c.Parent, parent) && typeMatches(c.Partner, partner) ||
			typeMatches(c.Parent, partner) && typeMatches(c.Partner, parent)
	})
}

// Blocks reads the block state at an offset from the position a trigger
// fired at.
type Blocks func(dx, dy, dz int32) uint16

// blockNamed reports whether state's block is one of blocks, names or one
// "#" and a block tag.
func blockNamed(blocks []string, state uint16) bool {
	name, _, ok := registry.BlockStateName(state)
	return ok && slices.ContainsFunc(blocks, func(want string) bool {
		tag, isTag := strings.CutPrefix(want, "#")
		return want == name || isTag && registry.BlockOf(state).InTag(registry.BlockTagOf(tag))
	})
}

// holds evaluates a location condition against the blocks around a position.
// vanilla: LocationCheck.test, LootItemBlockStatePropertyCondition.test
func holds(cond *registry.AdvancementCondition, at Blocks) bool {
	switch {
	case len(cond.All) > 0:
		return !slices.ContainsFunc(cond.All, func(t registry.AdvancementCondition) bool { return !holds(&t, at) })
	case len(cond.Any) > 0:
		return slices.ContainsFunc(cond.Any, func(t registry.AdvancementCondition) bool { return holds(&t, at) })
	}
	state := at(cond.Offset[0], cond.Offset[1], cond.Offset[2])
	if !blockNamed(cond.Blocks, state) {
		return false
	}
	if cond.Smokey && !rules.HiveSedated(at.reader(cond.Offset), 0, 0, 0) {
		return false
	}
	for key, want := range cond.State {
		if p, known := registry.PropNamed(key); !known || !registry.StateHas(state, p) || registry.StateValue(state, p) != want {
			return false
		}
	}
	return true
}

// reader views the cells around the position as a rules.BlockReader at an
// origin offset from it.
func (at Blocks) reader(origin [3]int32) rules.BlockReader {
	return blockReader(func(x, y, z int32) uint16 { return at(origin[0]+x, origin[1]+y, origin[2]+z) })
}

type blockReader func(x, y, z int32) uint16

func (r blockReader) BlockAt(x, y, z int32) uint16 { return r(x, y, z) }

func allHold(conds []registry.AdvancementCondition, at Blocks) bool {
	for i := range conds {
		if !holds(&conds[i], at) {
			return false
		}
	}
	return true
}

// PlacedBlock fires placed_block with the tool before placement consumes or damages it.
// vanilla: ItemUsedOnLocationTrigger.trigger
func (p *Progress) PlacedBlock(at Blocks, tool int32, when time.Time) bool {
	return p.trigger(TriggerPlacedBlock, when, func(c *registry.AdvancementCriterion) bool {
		return (len(c.Items) == 0 || slices.Contains(c.Items[0], tool)) && allHold(c.Where, at)
	})
}

// ItemUsedOnBlock fires item_used_on_block for tool used on the block at, as
// the use left it.
// vanilla: ItemUsedOnLocationTrigger.trigger
func (p *Progress) ItemUsedOnBlock(at Blocks, tool int32, biome string, when time.Time) bool {
	return p.trigger(TriggerItemUsedOnBlock, when, func(c *registry.AdvancementCriterion) bool {
		return (len(c.Items) == 0 || slices.Contains(c.Items[0], tool)) && (c.WhereBiome == "" || c.WhereBiome == biome) &&
			allHold(c.Where, at)
	})
}

// DefaultBlockUse fires default_block_use for a block whose own use, with
// no item, answered the click; at is the world around it.
// vanilla: DefaultBlockInteractionTrigger.TriggerInstance.matches
func (p *Progress) DefaultBlockUse(at Blocks, when time.Time) bool {
	return p.trigger(TriggerDefaultBlockUse, when, func(c *registry.AdvancementCriterion) bool {
		// Its loot context has no tool, so a match_tool condition never passes.
		return len(c.Items) == 0 && allHold(c.Where, at)
	})
}

// AnyBlockUse fires any_block_use for a click on the block at that the
// block's or the item's use consumed, tool being the hand's item after it.
// vanilla: AnyBlockInteractionTrigger.TriggerInstance.matches
func (p *Progress) AnyBlockUse(at Blocks, tool int32, when time.Time) bool {
	return p.trigger(TriggerAnyBlockUse, when, func(c *registry.AdvancementCriterion) bool {
		return (len(c.Items) == 0 || slices.Contains(c.Items[0], tool)) && allHold(c.Where, at)
	})
}

// RecipeUnlocked fires recipe_unlocked for a recipe newly added to the
// player's book. vanilla: RecipeUnlockedTrigger.TriggerInstance.matches
func (p *Progress) RecipeUnlocked(recipe string, at time.Time) bool {
	return p.trigger(TriggerRecipeUnlocked, at, func(c *registry.AdvancementCriterion) bool {
		return c.RecipeID == recipe
	})
}

// UsedEnderEye fires used_ender_eye for an eye thrown toward a structure
// sqrDist squared blocks away horizontally.
// vanilla: UsedEnderEyeTrigger.trigger, MinMaxBounds.Doubles.matchesSqr
func (p *Progress) UsedEnderEye(sqrDist float64, at time.Time) bool {
	return p.trigger(TriggerUsedEnderEye, at, func(c *registry.AdvancementCriterion) bool {
		r := c.Distance.Horizontal
		return (!r.HasMin || sqrDist >= r.Min*r.Min) && (!r.HasMax || sqrDist <= r.Max*r.Max)
	})
}

// RaidOmen fires voluntary_exile for a player whose raid omen a raid
// absorbed before its first wave, whose advancement sets no condition.
// vanilla: Raid.absorbRaidOmen, CriteriaTriggers.RAID_OMEN
func (p *Progress) RaidOmen(at time.Time) bool {
	return p.trigger(TriggerRaidOmen, at, func(*registry.AdvancementCriterion) bool { return true })
}

// AllayDropItemOnBlock fires allay_drop_item_on_block for the player an
// allay likes, with the item it threw at the block at.
// vanilla: AllayAi.throwItem, ItemUsedOnLocationTrigger.trigger
func (p *Progress) AllayDropItemOnBlock(at Blocks, item int32, when time.Time) bool {
	return p.trigger(TriggerAllayDropItemOnBlock, when, func(c *registry.AdvancementCriterion) bool {
		return (len(c.Items) == 0 || slices.Contains(c.Items[0], item)) && allHold(c.Where, at)
	})
}

// ThrownItemPickedUp fires thrown_item_picked_up_by_player for a player who
// picked up item thrown by other, or, with byEntity, thrown_item_picked_up_by_entity
// for the thrower whose item other picked up. worn is the player's armor item IDs.
// vanilla: ServerPlayer.onItemPickup, LivingEntity.onItemPickup,
// PickedUpItemTrigger.TriggerInstance.matches
func (p *Progress) ThrownItemPickedUp(byEntity bool, item int32, other Entity, worn []int32, at time.Time) bool {
	trigger := TriggerThrownPickedUpByPlayer
	if byEntity {
		trigger = TriggerThrownPickedUpByEntity
	}
	return p.trigger(trigger, at, func(c *registry.AdvancementCriterion) bool {
		return (len(c.Items) == 0 || slices.Contains(c.Items[0], item)) && typeMatches(c.Entity, other.Type) &&
			(!c.Adult || !other.Baby) && !slices.ContainsFunc(worn, func(id int32) bool { return slices.Contains(c.NotWearing, id) })
	})
}

// TameAnimal fires tame_animal for a tamed entity type; variant is its
// cat_variant or wolf_variant entry, empty for an animal without one.
// vanilla: TameAnimalTrigger.TriggerInstance.matches
func (p *Progress) TameAnimal(animal, variant string, at time.Time) bool {
	return p.trigger(TriggerTameAnimal, at, func(c *registry.AdvancementCriterion) bool {
		return typeMatches(c.Entity, animal) && (c.Variant == "" || c.Variant == variant)
	})
}

// EntityInteraction is the entity of an interaction as the interaction left
// it: its type, whether a baby, its variant entry (empty for a kind without
// one) and its body slot, Body being the item ID in it (0 when empty) and
// BodyDamage that piece's damage.
type EntityInteraction struct {
	Type       string
	Baby       bool
	Variant    string
	Body       int32
	BodyDamage int32
}

// matches is PlayerInteractTrigger.TriggerInstance.matches for the held or
// sheared item, the entity and the item IDs the player wears.
func (e EntityInteraction) matches(c *registry.AdvancementCriterion, item int32, worn []int32) bool {
	return (len(c.Items) == 0 || slices.Contains(c.Items[0], item)) && typeMatches(c.Entity, e.Type) &&
		(!c.Baby || e.Baby) && (!c.Adult || !e.Baby) && (c.Variant == "" || c.Variant == e.Variant) &&
		(len(c.Body) == 0 || slices.Contains(c.Body, e.Body)) && (!c.BodyUndamaged || e.BodyDamage == 0) &&
		!slices.ContainsFunc(worn, func(id int32) bool { return slices.Contains(c.NotWearing, id) })
}

// InteractedWithEntity fires player_interacted_with_entity for a successful
// interaction; held is the item in hand when it began, 0 for one that did not
// use it, and worn the item IDs of the player's armour.
// vanilla: ServerGamePacketListenerImpl.handleInteract
func (p *Progress) InteractedWithEntity(e EntityInteraction, held int32, worn []int32, at time.Time) bool {
	return p.trigger(TriggerInteractedEntity, at, func(c *registry.AdvancementCriterion) bool {
		return e.matches(c, held, worn)
	})
}

// ShearedEquipment fires player_sheared_equipment for the item taken off;
// worn is the item IDs of the player's armour. vanilla: Mob.shearItem
func (p *Progress) ShearedEquipment(e EntityInteraction, item int32, worn []int32, at time.Time) bool {
	return p.trigger(TriggerShearedEquipment, at, func(c *registry.AdvancementCriterion) bool {
		return e.matches(c, item, worn)
	})
}

// FilledBucket fires filled_bucket for the bucket item a scoop produced.
// vanilla: FilledBucketTrigger.TriggerInstance.matches
func (p *Progress) FilledBucket(item int32, at time.Time) bool {
	return p.trigger(TriggerFilledBucket, at, func(c *registry.AdvancementCriterion) bool {
		return len(c.Items) == 0 || slices.Contains(c.Items[0], item)
	})
}

// EffectsChanged fires effects_changed for the effect names the player now
// has. vanilla: EffectsChangedTrigger.TriggerInstance.matches
func (p *Progress) EffectsChanged(active []string, at time.Time) bool {
	return p.EffectsChangedFrom(active, "", at)
}

// EffectsChangedFrom tests effects_changed with the entity that supplied the effect.
func (p *Progress) EffectsChangedFrom(active []string, source string, at time.Time) bool {
	return p.trigger(TriggerEffectsChanged, at, func(c *registry.AdvancementCriterion) bool {
		if c.Entity != "" && (source == "" || !typeMatches(c.Entity, source)) {
			return false
		}
		for _, want := range c.Effects {
			if !slices.Contains(active, want) {
				return false
			}
		}
		return true
	})
}

// SummonedEntity fires summoned_entity for a golem or boss the player built.
// vanilla: SummonedEntityTrigger.TriggerInstance.matches
func (p *Progress) SummonedEntity(kind string, at time.Time) bool {
	return p.trigger(TriggerSummonedEntity, at, func(c *registry.AdvancementCriterion) bool {
		return typeMatches(c.Entity, kind)
	})
}

// SleptInBed fires slept_in_bed for a player who lay down.
// vanilla: PlayerTrigger.trigger
func (p *Progress) SleptInBed(at time.Time) bool {
	return p.trigger(TriggerSleptInBed, at, func(*registry.AdvancementCriterion) bool { return true })
}

// Tick fires tick, which has no condition this server models.
// vanilla: ServerPlayer.tick, PlayerTrigger.trigger
func (p *Progress) Tick(at time.Time) bool {
	return p.trigger(TriggerTick, at, func(*registry.AdvancementCriterion) bool { return true })
}

// ShotCrossbow fires shot_crossbow for the crossbow item that was fired.
// vanilla: ShotCrossbowTrigger.TriggerInstance.matches
func (p *Progress) ShotCrossbow(item int32, at time.Time) bool {
	return p.trigger(TriggerShotCrossbow, at, func(c *registry.AdvancementCriterion) bool {
		return len(c.Items) == 0 || slices.Contains(c.Items[0], item)
	})
}

// VillagerTrade fires villager_trade for a trade a player took the result
// of, at the height the player stands. vanilla: TradeTrigger.TriggerInstance.matches
func (p *Progress) VillagerTrade(playerY float64, at time.Time) bool {
	return p.trigger(TriggerVillagerTrade, at, func(c *registry.AdvancementCriterion) bool {
		return !c.HasMinY || playerY >= c.MinY
	})
}

// CuredZombieVillager fires cured_zombie_villager after a successful cure.
// vanilla: CuredZombieVillagerTrigger.TriggerInstance.matches
func (p *Progress) CuredZombieVillager(at time.Time) bool {
	return p.trigger(TriggerCuredZombieVillager, at, func(*registry.AdvancementCriterion) bool { return true })
}

// ConstructBeacon fires construct_beacon for a beacon that came alive with
// the given pyramid level. vanilla: ConstructBeaconTrigger.TriggerInstance.matches
func (p *Progress) ConstructBeacon(level int32, at time.Time) bool {
	return p.trigger(TriggerConstructBeacon, at, func(c *registry.AdvancementCriterion) bool {
		return level >= c.LevelMin && (c.LevelMax == 0 || level <= c.LevelMax)
	})
}

// BrewedPotion fires brewed_potion for a potion taken out of a brewing
// stand. vanilla: BrewedPotionTrigger.TriggerInstance.matches
func (p *Progress) BrewedPotion(at time.Time) bool {
	return p.trigger(TriggerBrewedPotion, at, func(*registry.AdvancementCriterion) bool { return true })
}

// FishingRodHooked fires fishing_rod_hooked for the items a reel brought up,
// or the item entity it pulled in. vanilla: FishingRodHookedTrigger.TriggerInstance.matches
func (p *Progress) FishingRodHooked(caught []int32, at time.Time) bool {
	return p.trigger(TriggerFishingRodHooked, at, func(c *registry.AdvancementCriterion) bool {
		if len(c.Items) == 0 {
			return true
		}
		return slices.ContainsFunc(caught, func(id int32) bool { return slices.Contains(c.Items[0], id) })
	})
}

// PlayerHurtProjectile fires player_hurt_entity for damage a player's
// projectile of the given entity type dealt. vanilla: PlayerHurtEntityTrigger.TriggerInstance.matches
func (p *Progress) PlayerHurtProjectile(direct string, at time.Time) bool {
	return p.trigger(TriggerPlayerHurtEntity, at, func(c *registry.AdvancementCriterion) bool {
		return c.Direct != "" && typeMatches(c.Direct, direct)
	})
}

// PlayerHurtSmash fires player_hurt_entity for a mace smash that dealt damage
// before armor, with held in the main hand. vanilla: PlayerHurtEntityTrigger.TriggerInstance.matches
func (p *Progress) PlayerHurtSmash(held int32, damage float64, at time.Time) bool {
	return p.trigger(TriggerPlayerHurtEntity, at, func(c *registry.AdvancementCriterion) bool {
		return c.SmashMin > 0 && damage >= c.SmashMin &&
			(len(c.Items) == 0 || slices.Contains(c.Items[0], held))
	})
}

// KilledByArrow fires killed_by_arrow for the entity types an arrow killed,
// fired from the weapon item. vanilla: KilledByArrowTrigger.TriggerInstance.matches
func (p *Progress) KilledByArrow(victims []string, weapon int32, at time.Time) bool {
	unique := map[string]bool{}
	for _, v := range victims {
		unique[v] = true
	}
	return p.trigger(TriggerKilledByArrow, at, func(c *registry.AdvancementCriterion) bool {
		if len(c.Items) > 0 && !slices.Contains(c.Items[0], weapon) {
			return false
		}
		rest := slices.Clone(victims)
		for _, want := range c.Victims {
			i := slices.IndexFunc(rest, func(got string) bool { return typeMatches(want, got) })
			if i < 0 {
				return false
			}
			rest = slices.Delete(rest, i, i+1)
		}
		n := int32(len(unique))
		return n >= c.UniqueMin && (c.UniqueMax == 0 || n <= c.UniqueMax)
	})
}

// ChanneledLightning fires channeled_lightning for the entity types one tick
// of a player's channeling bolt struck; each victim predicate needs one struck
// entity, and two may share one. vanilla: ChanneledLightningTrigger.TriggerInstance.matches
func (p *Progress) ChanneledLightning(struck []string, at time.Time) bool {
	return p.trigger(TriggerChanneledLightning, at, func(c *registry.AdvancementCriterion) bool {
		for _, want := range c.Victims {
			if !slices.ContainsFunc(struck, func(got string) bool { return typeMatches(want, got) }) {
				return false
			}
		}
		return true
	})
}

// SpearMobs fires spear_mobs with the number of living bodies a spear charge
// has stabbed so far. vanilla: SpearMobsTrigger.TriggerInstance.matches
func (p *Progress) SpearMobs(stabbed int, at time.Time) bool {
	return p.trigger(TriggerSpearMobs, at, func(c *registry.AdvancementCriterion) bool {
		return stabbed >= int(c.Count)
	})
}

// GeneratedContainerLoot fires player_generates_container_loot for the loot
// table a player's open or brush rolled. vanilla: GenerateContainerLootTrigger.TriggerInstance.matches
func (p *Progress) GeneratedContainerLoot(table string, at time.Time) bool {
	return p.trigger(TriggerContainerLoot, at, func(c *registry.AdvancementCriterion) bool {
		return c.LootTable == table
	})
}

// RecipeCrafted fires recipe_crafted for a recipe a player took the result
// of; a criterion's ingredient predicates each take one distinct used item.
// vanilla: RecipeCraftedTrigger.TriggerInstance.matches
func (p *Progress) RecipeCrafted(recipe string, used []int32, at time.Time) bool {
	return p.recipeCrafted(TriggerRecipeCrafted, recipe, used, at)
}

// CrafterRecipeCrafted fires crafter_recipe_crafted for a crafter's craft
// near the player, matching like RecipeCrafted.
// vanilla: CriteriaTriggers.CRAFTER_RECIPE_CRAFTED
func (p *Progress) CrafterRecipeCrafted(recipe string, used []int32, at time.Time) bool {
	return p.recipeCrafted(TriggerCrafterRecipe, recipe, used, at)
}

func (p *Progress) recipeCrafted(trigger, recipe string, used []int32, at time.Time) bool {
	return p.trigger(trigger, at, func(c *registry.AdvancementCriterion) bool {
		if c.RecipeID != recipe {
			return false
		}
		remaining := slices.Clone(used)
		for _, predicate := range c.Items {
			i := slices.IndexFunc(remaining, func(id int32) bool { return slices.Contains(predicate, id) })
			if i < 0 {
				return false
			}
			remaining = slices.Delete(remaining, i, i+1)
		}
		return true
	})
}

// UsingItem fires using_item for an item being used; lookingAt names the
// entity type the player's look meets and is asked only for a criterion that
// wants one, at most once. vanilla: UsingItemTrigger.TriggerInstance.matches
func (p *Progress) UsingItem(item int32, lookingAt func() string, at time.Time) bool {
	var looked *string
	return p.trigger(TriggerUsingItem, at, func(c *registry.AdvancementCriterion) bool {
		if len(c.Items) > 0 && !slices.Contains(c.Items[0], item) {
			return false
		}
		if c.LookingAt == "" {
			return true
		}
		if looked == nil {
			name := lookingAt()
			looked = &name
		}
		return *looked == c.LookingAt
	})
}

// apart reports d holding between a and b. vanilla: DistancePredicate.matches
func apart(d registry.AdvancementDistance, a, b [3]float64) bool {
	return d.Matches(float32(a[0]-b[0]), float32(a[1]-b[1]), float32(a[2]-b[2]))
}

// Moved fires a distance trigger, nether_travel, fall_from_height or
// levitation, for a player now at now that started at start.
// vanilla: DistanceTrigger.TriggerInstance.matches, LevitationTrigger.TriggerInstance.matches
func (p *Progress) Moved(trigger string, start, now [3]float64, at time.Time) bool {
	return p.trigger(trigger, at, func(c *registry.AdvancementCriterion) bool {
		return c.StartY.Matches(start[1]) && c.PlayerY.Matches(now[1]) && apart(c.Distance, start, now)
	})
}

// FallAfterExplosion fires fall_after_explosion for a player whose fall
// began at now after an explosion of the entity type cause hit them at
// impact; cause is empty for none. vanilla: FallAfterExplosionTrigger.TriggerInstance.matches
func (p *Progress) FallAfterExplosion(impact, now [3]float64, cause string, at time.Time) bool {
	return p.trigger(TriggerFallAfterExplosion, at, func(c *registry.AdvancementCriterion) bool {
		return c.StartY.Matches(impact[1]) && apart(c.Distance, impact, now) && typeMatches(c.Entity, cause)
	})
}

// LightningStrike fires lightning_strike for a player at pos when a bolt at
// bolt, which lit fires blocks, ends with bystanders (entity type names)
// near it. vanilla: LightningStrikeTrigger.TriggerInstance.matches
func (p *Progress) LightningStrike(pos, bolt [3]float64, fires int, bystanders []string, at time.Time) bool {
	return p.trigger(TriggerLightningStrike, at, func(c *registry.AdvancementCriterion) bool {
		return c.Fires.Matches(float64(fires)) && apart(c.Distance, pos, bolt) &&
			(c.Entity == "" || slices.ContainsFunc(bystanders, func(t string) bool { return typeMatches(c.Entity, t) }))
	})
}

// EnterBlock fires enter_block for the block at the origin of at, one the
// player's body is inside. vanilla: EnterBlockTrigger.TriggerInstance.matches
func (p *Progress) EnterBlock(at Blocks, when time.Time) bool {
	return p.trigger(TriggerEnterBlock, when, func(c *registry.AdvancementCriterion) bool {
		return allHold(c.Where, at)
	})
}

// itemPasses reports whether item passes a criterion's one item predicate,
// any item when it has none.
func itemPasses(c *registry.AdvancementCriterion, item int32) bool {
	return len(c.Items) == 0 || slices.Contains(c.Items[0], item)
}

// UsedTotem fires used_totem for the item that saved the player from death.
// vanilla: UsedTotemTrigger.TriggerInstance.matches
func (p *Progress) UsedTotem(item int32, at time.Time) bool {
	return p.trigger(TriggerUsedTotem, at, func(c *registry.AdvancementCriterion) bool {
		return itemPasses(c, item)
	})
}

// EntityHurtPlayer fires entity_hurt_player for a hurt the player took or a
// shield blocked; projectile is a damage type of the is_projectile tag.
// vanilla: EntityHurtPlayerTrigger.TriggerInstance.matches, DamagePredicate.matches
func (p *Progress) EntityHurtPlayer(projectile, blocked bool, at time.Time) bool {
	return p.trigger(TriggerEntityHurtPlayer, at, func(c *registry.AdvancementCriterion) bool {
		return (!c.Projectile || projectile) && (!c.Blocked || blocked)
	})
}

// TargetHit fires target_hit for a target block the player's projectile hit
// for signal, the projectile then at hit and the player at from.
// vanilla: TargetBlockTrigger.TriggerInstance.matches
func (p *Progress) TargetHit(signal int32, from, hit [3]float64, at time.Time) bool {
	return p.trigger(TriggerTargetHit, at, func(c *registry.AdvancementCriterion) bool {
		return c.Signal.Matches(float64(signal)) && apart(c.Distance, from, hit)
	})
}

// Rider is what a player condition reads of the player: the dimension it
// stands in, its vehicle's entity type, empty on foot, and the types of the
// vehicle's passengers, the player's own included.
type Rider struct {
	Dimension, Vehicle string
	Passengers         []string
}

// holds reports whether r passes c's player condition.
// vanilla: EntityPredicate.matches (location, vehicle, passenger)
func (r Rider) holds(c *registry.AdvancementCriterion) bool {
	if c.Dimension != "" && c.Dimension != r.Dimension {
		return false
	}
	if c.Vehicle != "" && (r.Vehicle == "" || !typeMatches(c.Vehicle, r.Vehicle)) {
		return false
	}
	return c.Passenger == "" || slices.ContainsFunc(r.Passengers, func(t string) bool { return typeMatches(c.Passenger, t) })
}

// StartedRiding fires started_riding for a player whose vehicle, or one it
// rides on, just took a passenger. vanilla: Entity.startRiding
func (p *Progress) StartedRiding(who Rider, at time.Time) bool {
	return p.trigger(TriggerStartedRiding, at, who.holds)
}

// ItemDurabilityChanged fires item_durability_changed for wear put on the
// player's item. vanilla: ItemDurabilityTrigger.TriggerInstance.matches
func (p *Progress) ItemDurabilityChanged(item int32, who Rider, at time.Time) bool {
	return p.trigger(TriggerItemDurability, at, func(c *registry.AdvancementCriterion) bool {
		return itemPasses(c, item) && who.holds(c)
	})
}

// RodeInLava fires ride_entity_in_lava for a player whose vehicle has been
// in lava since start, the player now at now.
// vanilla: DistanceTrigger.TriggerInstance.matches
func (p *Progress) RodeInLava(who Rider, start, now [3]float64, at time.Time) bool {
	return p.trigger(TriggerRideInLava, at, func(c *registry.AdvancementCriterion) bool {
		return who.holds(c) && apart(c.Distance, start, now)
	})
}
