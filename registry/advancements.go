package registry

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
)

// Advancement is one advancement of the vanilla data pack as
// advancements_gen.go carries it. vanilla: Advancement
type Advancement struct {
	// ID and Parent are resource locations; Parent is empty for a root.
	ID, Parent string
	// Display is nil for an advancement the screen never shows.
	Display  *AdvancementDisplay
	Criteria []AdvancementCriterion
	// Requirements is the criteria groups: the advancement is done when
	// every group has one criterion obtained.
	Requirements [][]string
	// Telemetry is sends_telemetry_event, which the client reads off the
	// wire.
	Telemetry bool
	// Experience is rewards.experience.
	Experience int32
	// Loot is rewards.loot, the loot table ids rolled for the player.
	Loot []string
	// Function is rewards.function, the function run as the player; empty
	// for none.
	Function string
	// Recipes is rewards.recipes, the recipe IDs completing it unlocks.
	Recipes []string
}

// AdvancementDisplay is how the screen draws an advancement.
// vanilla: DisplayInfo
type AdvancementDisplay struct {
	// Title and Description are translation keys, or literal text when the
	// matching Literal flag is set. TitleText and DescriptionText, when
	// non-nil, carry a full text component from a data pack and take
	// precedence over the string fields.
	Title, Description               string
	TitleLiteral, DescriptionLiteral bool
	TitleText, DescriptionText       *protocol.Text
	// Icon is an item ID; IconStack, when non-nil, carries the full icon
	// template including its component patch.
	Icon, IconCount int32
	IconStack       *protocol.ItemStack
	// Frame is AdvancementType's ordinal: 0 task, 1 challenge, 2 goal.
	Frame int32
	// Background is the tab's texture, set on the roots alone.
	Background                      string
	ShowToast, AnnounceChat, Hidden bool
}

// AdvancementCriterion is one named criterion and the conditions of its
// trigger this server tests.
type AdvancementCriterion struct {
	Name, Trigger string
	// Player is the trigger's player condition, loot conditions that must all
	// hold of the player; empty for none.
	// vanilla: SimpleCriterionTrigger.SimpleInstance.player
	Player []LootCondition
	// Items is an inventory_changed criterion's item predicates, or the
	// one predicate of a consume_item, filled_bucket, item_used_on_block
	// or any_block_use (the tool), allay_drop_item_on_block (the item dropped) or
	// shot_crossbow criterion, each the item IDs it accepts.
	Items [][]int32
	// From and To are a changed_dimension criterion's dimensions; empty
	// matches any.
	From, To string
	// Entity is the entity type a player_killed_entity criterion wants of
	// the victim, or an effects_changed source; Child, Parent and Partner those of a bred_animals
	// criterion. Each is a type name or "#" and a tag, empty for any.
	Entity, Child, Parent, Partner string
	// Dimension is the dimension the victim must stand in, or for a
	// ride_entity_in_lava criterion the player, empty for any.
	Dimension string
	// Vehicle is the entity type a started_riding, item_durability_changed
	// or ride_entity_in_lava criterion wants the player to ride, and
	// Passenger one the vehicle must also carry; each a name or "#" and a
	// tag, empty for any.
	Vehicle, Passenger string
	// Variant is the cat, frog or wolf variant entry a tame_animal or
	// player_interacted_with_entity criterion wants of the entity, empty for
	// any.
	Variant string
	// Biome is the biome a location criterion wants the player in.
	Biome string
	// WhereBiome is the biome an item_used_on_block criterion wants at the
	// clicked block, empty for any.
	WhereBiome string
	// Feet is the item IDs a location criterion wants in the player's feet
	// slot, nil for any.
	Feet []int32
	// SteppingOn is the blocks, names or one "#" and a tag, a location
	// criterion wants the player standing on, nil for any.
	SteppingOn []string
	// Structures accepts any listed structure containing the player in a piece;
	// nil imposes no structure condition, an empty list never matches.
	Structures []string
	// Where is the location conditions of a placed_block,
	// item_used_on_block, allay_drop_item_on_block, default_block_use or
	// any_block_use criterion, all of which must hold; for enter_block,
	// the one check of the block entered.
	Where []AdvancementCondition
	// Effects names the effects an effects_changed criterion wants the
	// player to have all of.
	Effects []string
	// MinY is the height a villager_trade criterion wants the player at or
	// above, when HasMinY is set.
	MinY    float64
	HasMinY bool
	// LevelMin and LevelMax bound a construct_beacon criterion's pyramid
	// level; a LevelMax of 0 is unbounded.
	LevelMin, LevelMax int32
	// Direct is the entity type of the projectile a player_hurt_entity
	// criterion wants the damage dealt by, or a player_killed_entity
	// criterion wants the killing blow dealt by, a name or "#" and a tag.
	Direct string
	// SmashMin is the least damage a player_hurt_entity criterion wants a
	// mace smash with the held Items to deal, 0 for none.
	SmashMin float64
	// Captain requires the killed raider to wear the ominous banner.
	Captain bool
	// Projectile requires a player_killed_entity killing blow, or an
	// entity_hurt_player hurt, dealt by any projectile; MinDistance is the
	// least horizontal distance in blocks between the player and the
	// victim, 0 for none. Blocked requires a hurt a shield blocked.
	Projectile  bool
	MinDistance float64
	Blocked     bool
	// Victims are the entity types a killed_by_arrow criterion wants one
	// distinct killed victim each of, or a channeled_lightning criterion one
	// struck entity each of; UniqueMin and UniqueMax bound how many
	// different types the shot killed, a UniqueMax of 0 unbounded. The
	// weapon is Items' one predicate.
	Victims              []string
	UniqueMin, UniqueMax int32
	// Count is the least number of bodies a spear_mobs criterion wants a
	// charge to have stabbed, 0 for any.
	Count int32
	// Body is the item IDs a player_interacted_with_entity or
	// player_sheared_equipment criterion wants in the entity's body slot,
	// and BodyUndamaged that the piece has taken no damage; the entity type
	// is Entity and the held item Items' one predicate.
	Body          []int32
	BodyUndamaged bool
	// Adult requires a thrown_item_picked_up_by_entity,
	// thrown_item_picked_up_by_player or player_interacted_with_entity
	// criterion's Entity not to be a baby, and Baby requires it to be one;
	// the item is Items' one predicate.
	Adult, Baby bool
	// NotWearing is the item IDs such a criterion wants the player to wear
	// none of in its feet, legs, chest or head slot.
	NotWearing []int32
	// LootTable is the table a player_generates_container_loot criterion
	// wants rolled, such as "minecraft:chests/bastion_bridge".
	LootTable string
	// RecipeID is the recipe a recipe_crafted or crafter_recipe_crafted
	// criterion wants made, with Items the ingredients it must have used,
	// or the one a recipe_unlocked criterion wants unlocked.
	RecipeID string
	// LookingAt is the entity type a using_item criterion wants the player
	// to look at, empty for any.
	LookingAt string
	// Distance bounds how far a nether_travel, fall_from_height,
	// levitation or ride_entity_in_lava criterion's player got from where
	// it started, or a target_hit projectile from the player; its
	// Horizontal bounds a used_ender_eye player's distance from the
	// structure the eye flies to. StartY bounds
	// the start's Y and PlayerY the player's own when the trigger fires.
	Distance        AdvancementDistance
	StartY, PlayerY AdvancementRange
	// Signal bounds a target_hit criterion's redstone signal, 0 to 15.
	Signal AdvancementRange
	// Occupied, Full and Empty bound an inventory_changed criterion's
	// counts of occupied, full and empty inventory slots.
	// vanilla: InventoryChangeTrigger.TriggerInstance.Slots
	Occupied, Full, Empty AdvancementRange
	// Fires bounds how many blocks a lightning_strike criterion's bolt set
	// on fire; its Distance is from the player to the bolt, and its Entity
	// the type one bystander near the bolt must have.
	Fires AdvancementRange
	// Block, Enchantment and Bees are what a bee_nest_destroyed criterion
	// wants: the block broken, an enchantment of level 1 or more on the
	// tool, and the exact number of bees inside (0 for any).
	Block, Enchantment string
	Bees               int32
	// Unmodelled marks a criterion whose conditions go beyond the ones
	// above; it never fires.
	Unmodelled bool
}

// AdvancementRange bounds a number; an end without its Has flag is open.
// vanilla: MinMaxBounds.Doubles
type AdvancementRange struct {
	Min, Max       float64
	HasMin, HasMax bool
}

// Matches reports v inside the range.
func (r AdvancementRange) Matches(v float64) bool {
	return (!r.HasMin || v >= r.Min) && (!r.HasMax || v <= r.Max)
}

// MatchesSqr reports a squared value inside the range's bounds squared.
// vanilla: MinMaxBounds.Doubles.matchesSqr
func (r AdvancementRange) MatchesSqr(sq float64) bool {
	return (!r.HasMin || sq >= r.Min*r.Min) && (!r.HasMax || sq <= r.Max*r.Max)
}

// AdvancementDistance bounds the distance between two positions in blocks:
// each axis's, the horizontal and the straight-line one.
// vanilla: DistancePredicate
type AdvancementDistance struct {
	X, Y, Z, Horizontal, Absolute AdvancementRange
}

// Matches reports d holding for the offset (dx, dy, dz) between two
// positions. The differences are floats, as vanilla's are.
// vanilla: DistancePredicate.matches
func (d AdvancementDistance) Matches(dx, dy, dz float32) bool {
	abs := func(f float32) float64 { return math.Abs(float64(f)) }
	return d.X.Matches(abs(dx)) && d.Y.Matches(abs(dy)) && d.Z.Matches(abs(dz)) &&
		d.Horizontal.MatchesSqr(float64(dx*dx+dz*dz)) && d.Absolute.MatchesSqr(float64(dx*dx+dy*dy+dz*dz))
}

// AdvancementCondition is one loot condition over the blocks around a
// trigger's position: all of All, any of Any, or, when both are empty, a
// check that the block at Offset is one of Blocks with the State properties.
// vanilla: AllOfCondition, AnyOfCondition, LocationCheck,
// LootItemBlockStatePropertyCondition
type AdvancementCondition struct {
	All, Any []AdvancementCondition
	// Blocks are block names or one "#" and a block tag.
	Blocks []string
	State  map[string]string
	// Offset is relative to the trigger's position.
	Offset [3]int32
	// Smokey requires a lit campfire's smoke to reach the cell.
	Smokey bool
}
