package registry

// The vanilla block and entity loot tables as Go data, generated into
// loot_gen.go by tools/gen loot; the engine that rolls them is in
// internal/server/rules.
//
// The data is inert because registry has no game logic. Item entries carry
// typed IDs and tags are expanded when the tables load.

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// LootTable is one loot table: its pools in order, and the functions every
// stack they produce passes through after the pools' own.
type LootTable struct {
	RandomSequence string
	Pools          []LootPool
	Functions      []LootFunction
}

// LootPool is one roll of a table: its entries, its conditions, and the
// functions every stack passes through after the entry's own.
type LootPool struct {
	Rolls      LootNumber
	BonusRolls LootNumber // the zero value is none, which rolls nothing extra
	Entries    []LootEntry
	Conditions []LootCondition
	Functions  []LootFunction
}

// LootNumber is a context int or float provider. Whether a node reads as an
// int or a float is decided by the field that holds it; the generated tables
// use only the constant, uniform, binomial and sum.
type LootNumber interface {
	isLootNumber()
}

// LootConstant is a fixed number, the float the data pack wrote, floored
// where an integer is wanted.
// vanilla: ConstantValue.getInt
type LootConstant struct {
	Value float64
}

// LootUniform rolls between two bounds; for an integer both are floored and
// the roll is inclusive at both ends, so 0 to 2 is 0, 1 or 2.
// vanilla: UniformGenerator.getInt
type LootUniform struct {
	Min, Max LootNumber
}

// LootBinomial is a number provider that counts the successes of n rolls
// each succeeding with probability p.
type LootBinomial struct {
	N, P LootNumber
}

// LootSum is add: an exact int sum, or a float32 sum.
// vanilla: ints.AddProvider, floats.AddProvider
type LootSum struct {
	Summands []LootNumber
}

// LootUnary is one of abs, negate, ceil, floor, round, truncate, sqrt, sin
// and cos, named by Op.
type LootUnary struct {
	Op    string
	Input LootNumber
}

// LootBinary is one of sub, div, floor_div, floor_mod, mod and pow, named by
// Op; pow's Left is the base and Right the exponent.
type LootBinary struct {
	Op          string
	Left, Right LootNumber
}

// LootAggregate is one of mul, max, min, avg and length over its inputs,
// named by Op.
// vanilla: AggregateProvider
type LootAggregate struct {
	Op     string
	Inputs []LootNumber
}

// LootConvert is from_float in an int field and from_int in a float field.
type LootConvert struct {
	Input LootNumber
}

// LootWeightedList picks one of its values by weight.
type LootWeightedList struct {
	Values  []LootNumber
	Weights []int64
}

// LootConditional is OnTrue where the condition holds, else OnFalse (nil is
// zero).
type LootConditional struct {
	Condition       LootCondition
	OnTrue, OnFalse LootNumber
}

// LootDispatcher is the value of the first case whose condition holds, else
// Default (nil is zero).
type LootDispatcher struct {
	Cases   []LootDispatchCase
	Default LootNumber
}

// LootDispatchCase is one case of a LootDispatcher.
type LootDispatchCase struct {
	Condition LootCondition
	Value     LootNumber
}

// LootScore is a scoreboard value: Target names a context entity, or is the
// holder's own name when Fixed. A missing score reads Fallback (nil is zero).
type LootScore struct {
	Target    string
	Fixed     bool
	Objective string
	Fallback  LootNumber
}

// LootStorage is the single number Path (raw NBT path text) selects in a
// command storage, else Fallback (nil is zero).
type LootStorage struct {
	Storage, Path string
	Fallback      LootNumber
}

// LootEnvAttribute is an environment attribute's value at the origin.
type LootEnvAttribute struct {
	Attribute string
}

// LootEnchantmentLevel is a level-based value read at the context's
// enchantment level.
type LootEnchantmentLevel struct {
	Amount *EnchLevelValue
}

// LootProviderRef is the context_int_provider (or, when Float,
// context_float_provider) registry entry Name, decoded when evaluated.
type LootProviderRef struct {
	Name  string
	Float bool
}

func (LootSum) isLootNumber()              {}
func (LootConstant) isLootNumber()         {}
func (LootUniform) isLootNumber()          {}
func (LootBinomial) isLootNumber()         {}
func (LootUnary) isLootNumber()            {}
func (LootBinary) isLootNumber()           {}
func (LootAggregate) isLootNumber()        {}
func (LootConvert) isLootNumber()          {}
func (LootWeightedList) isLootNumber()     {}
func (LootConditional) isLootNumber()      {}
func (LootDispatcher) isLootNumber()       {}
func (LootScore) isLootNumber()            {}
func (LootStorage) isLootNumber()          {}
func (LootEnvAttribute) isLootNumber()     {}
func (LootEnchantmentLevel) isLootNumber() {}
func (LootProviderRef) isLootNumber()      {}

// LootEntry is one choice a pool can make: an item, one of the composite
// shapes, an empty space, a reference to another table, or a tag of items.
type LootEntry interface {
	isLootEntry()
}

// LootItemEntry is an entry that produces one stack of its item, count one
// before its functions ran.
type LootItemEntry struct {
	Item       Item
	Weight     int32
	Quality    int32 // the weight added per point of the roll's luck
	Conditions []LootCondition
	Functions  []LootFunction
}

// LootAlternatives collects candidates in order until a child succeeds.
type LootAlternatives struct {
	Children   []LootEntry
	Conditions []LootCondition
}

// LootGroup is every child, each its own candidate of the roll, in order.
type LootGroup struct {
	Children   []LootEntry
	Conditions []LootCondition
}

// LootSequence collects candidates in order until a child fails.
type LootSequence struct {
	Children   []LootEntry
	Conditions []LootCondition
}

// LootEmpty is an entry that produces nothing but can still take the roll.
type LootEmpty struct {
	Weight     int32
	Quality    int32
	Conditions []LootCondition
}

// LootReference is an entry that rolls another table whole, into this
// roll's output.
type LootReference struct {
	Table string
	// Inline is a nested table written out in the entry; Table is empty.
	Inline     *LootTable
	Weight     int32
	Quality    int32
	Conditions []LootCondition
	Functions  []LootFunction
}

// LootTag is an item tag expanded by the generator: expanded, each item is
// its own candidate; whole, every item drops at once.
type LootTag struct {
	Items      []Item
	Expand     bool
	Weight     int32
	Quality    int32
	Conditions []LootCondition
	Functions  []LootFunction
}

// LootDynamic is an entry whose stacks the looted thing supplies at roll
// time (a decorated pot's sherds); it yields nothing unless the context
// carries the drop.
type LootDynamic struct {
	Name       string
	Weight     int32
	Quality    int32
	Conditions []LootCondition
}

// LootSlots is an entry that yields a copy of every stack its slot source
// finds in the roll's context.
// vanilla: SlotLoot
type LootSlots struct {
	Source     SlotSource
	Weight     int32
	Quality    int32
	Conditions []LootCondition
	Functions  []LootFunction
}

func (LootSlots) isLootEntry()        {}
func (LootItemEntry) isLootEntry()    {}
func (LootAlternatives) isLootEntry() {}
func (LootGroup) isLootEntry()        {}
func (LootSequence) isLootEntry()     {}
func (LootEmpty) isLootEntry()        {}
func (LootReference) isLootEntry()    {}
func (LootTag) isLootEntry()          {}
func (LootDynamic) isLootEntry()      {}

// LootCondition gates a pool, entry or function on the roll's context. Only
// the kinds this version's tables use exist; another stops the generator.
type LootCondition interface {
	isLootCondition()
}

// LootMatchTool requires the tool in play (the breaking item or the killing
// weapon) to match an item predicate.
type LootMatchTool struct {
	Predicate LootItemPredicate
}

// LootSurvivesExplosion passes outside an explosion, or with one chance in
// the blast radius.
type LootSurvivesExplosion struct{}

// LootRandomChance is the condition that a chance, a context float
// provider, came up.
type LootRandomChance struct {
	Chance LootNumber
}

// LootEnchantedChance passes on a chance that climbs linearly with an
// enchantment's level on the killer's weapon (looting's rare drops).
type LootEnchantedChance struct {
	Enchantment string
	Unenchanted float64
	Base        float64
	PerLevel    float64
	// Chance, when set, is the enchanted chance as any level-based value
	// and replaces Base and PerLevel, which carry the linear form.
	Chance *EnchLevelValue
}

// LootTableBonus passes on a chance indexed by a tool enchantment's level
// (fortune on leaves' saplings and gravel's flint).
type LootTableBonus struct {
	Enchantment string
	Chances     []float64
}

// LootKilledByPlayer is the condition that a player's kill is what this
// death was.
type LootKilledByPlayer struct{}

// LootBlockState is the condition that the block in play is a given block
// in one of the states named - the ripeness of a crop, the crack of a pot.
type LootBlockState struct {
	Block      string
	Properties []LootStateProperty
}

// LootMatchBlock requires the block state in play to be one of the blocks
// named, with every property equal; a roll with no block in play fails.
// Blocks nil means any block; an empty non-nil Blocks matches none.
// vanilla: MatchBlock
type LootMatchBlock struct {
	Blocks     []string
	Properties []LootStateProperty
}

// LootLocationCheck requires the block at an offset to be a given block in
// one of the named states, and its biome to be one of the named (the tall
// grass halves check each other, the mineshaft disc its cave); a part the
// table left empty asks after nothing. Block is a block name, or "#" and a
// block tag's name.
//
// The rest of vanilla's LocationPredicate is each part present asked after:
// Position (x, y, z on the double position), Dimension, Structures (a piece
// of one), Smokey, Light (the raw brightness at the block), Fluid, CanSeeSky
// and BlockNBT (the block entity's saved data, with or without Block).
type LootLocationCheck struct {
	Block                     string
	Properties                []LootStateProperty
	Biomes                    []string
	OffsetX, OffsetY, OffsetZ int32

	Position *[3]AdvancementRange
	// Dimension is the dimension's name the roll must happen in; empty asks after none.
	Dimension  string
	Structures []string
	Smokey     *bool
	Light      *AdvancementRange
	Fluid      *LootFluidCheck
	CanSeeSky  *bool
	BlockNBT   nbt.CompoundTag
}

// LootFluidCheck is a FluidPredicate: Fluids are the fluid names accepted
// (nil any) and Properties the exact fluid state properties required.
// vanilla: FluidPredicate
type LootFluidCheck struct {
	Fluids     []string
	Properties []LootStateProperty
}

// LootEntityProperties requires a context entity (the dying body, the
// attacker) to match an entity predicate.
type LootEntityProperties struct {
	Entity    string
	Predicate LootEntityPredicate
}

// LootDamageSource requires the killer to be of the named type (and, with
// FrogVariant, a frog of that variant), or the damage type to be one type or
// in a tag.
type LootDamageSource struct {
	SourceType  string
	FrogVariant string
	// DamageTypes, when set, replaces the entity test with holder membership.
	DamageTypes *DamageTypeSet
	// DirectType also demands the entity that carried the blow have this type.
	DirectType string
	// Tags are the TagPredicate terms; each must hold.
	Tags []LootDamageTag
	// Direct and Source test the entity that carried the blow and the one
	// that caused it; a missing entity fails.
	Direct, Source *LootEntityPredicate
	// IsDirect asks whether the two are the same entity.
	IsDirect *bool
}

// LootDamageTag is one damage-type tag term: the damage type is in Set
// exactly when Expected.
type LootDamageTag struct {
	Set      *DamageTypeSet
	Expected bool
}

// LootInverted is the condition that its term does not hold.
type LootInverted struct {
	Term LootCondition
}

// LootAnyOf is the condition that any of its terms holds.
type LootAnyOf struct {
	Terms []LootCondition
}

// LootAllOf is the condition that every one of its terms holds.
type LootAllOf struct {
	Terms []LootCondition
}

// LootTimeCheck requires a clock's total ticks, modulo Period when it is
// not zero, to be in Value.
// vanilla: TimeCheck
type LootTimeCheck struct {
	Clock  string
	Period int64
	Value  LootRange
}

// LootWeatherCheck requires the level's rain and thunder to be as named; a
// nil flag asks after nothing.
// vanilla: WeatherCheck
type LootWeatherCheck struct {
	Raining, Thundering *bool
}

// LootEntityScores requires a context entity to hold a score in every named
// objective, each in its range.
// vanilla: EntityHasScoreCondition
type LootEntityScores struct {
	Entity string
	Scores []LootScoreRange
}

// LootScoreRange is one objective of an entity_scores condition.
type LootScoreRange struct {
	Objective string
	Range     LootRange
}

// LootEnchantmentActive requires the roll to be an enchantment effect's
// whose active flag equals Active.
// vanilla: EnchantmentActiveCheck
type LootEnchantmentActive struct {
	Active bool
}

// LootEnvironmentCheck requires a float-convertible environment attribute,
// read at the roll's origin, to equal Value.
// vanilla: EnvironmentAttributeCheck
type LootEnvironmentCheck struct {
	Attribute string
	Value     float32
}

// LootValueCheck requires a number to be in a range: Float reads both as
// floats (float_value_check), otherwise as integers (int_value_check).
// vanilla: IntValueCheck, FloatValueCheck
type LootValueCheck struct {
	Value LootNumber
	Range LootRange
	Float bool
}

// LootRange is an IntRangePredicate or FloatRangePredicate: Exact, when
// set, is a point the input must equal; otherwise the input lies between
// Min and Max, a nil end being open.
type LootRange struct {
	Exact, Min, Max LootNumber
}

func (LootTimeCheck) isLootCondition()         {}
func (LootWeatherCheck) isLootCondition()      {}
func (LootEntityScores) isLootCondition()      {}
func (LootEnchantmentActive) isLootCondition() {}
func (LootEnvironmentCheck) isLootCondition()  {}
func (LootValueCheck) isLootCondition()        {}

func (LootMatchBlock) isLootCondition() {}

func (LootMatchTool) isLootCondition()         {}
func (LootSurvivesExplosion) isLootCondition() {}
func (LootRandomChance) isLootCondition()      {}
func (LootEnchantedChance) isLootCondition()   {}
func (LootTableBonus) isLootCondition()        {}
func (LootKilledByPlayer) isLootCondition()    {}
func (LootBlockState) isLootCondition()        {}
func (LootLocationCheck) isLootCondition()     {}
func (LootEntityProperties) isLootCondition()  {}
func (LootDamageSource) isLootCondition()      {}
func (LootInverted) isLootCondition()          {}
func (LootAnyOf) isLootCondition()             {}
func (LootAllOf) isLootCondition()             {}

// LootStateProperty is one exact property match: the block state's value
// for the property has to be this.
type LootStateProperty struct {
	Key, Value string
}

// LootItemPredicate matches items, enchantments and dyed colour presence, and
// with Full any other part of an item predicate.
type LootItemPredicate struct {
	Items              []string // empty: any item
	Enchantments       []LootEnchantments
	StoredEnchantments []LootEnchantments
	HasDyedColor       bool
	HasMapID           bool
	// Full is the whole item predicate, set when it asks for more than the
	// fields above (count, components, other component predicates); it is
	// tested on the stack itself in addition to them.
	Full *ItemPredicate
}

// LootEnchantments requires one enchantment (named, from a tag, or any)
// within a level range; a zero bound is no bound, as levels start at 1.
type LootEnchantments struct {
	Enchantments []string
	Min, Max     int32
}

// LootEntityPredicate holds only facts a simulated mob death can answer;
// any other field stops the generator.
type LootEntityPredicate struct {
	Type        string   // the entity type itself, exact
	Types       []string // the entity type, one of a tag's members
	OnFire      *bool
	Baby        *bool
	Vehicle     string // the type of the body it rides, when asked
	SheepColour string // the colour component of a sheep, when asked
	Sheared     *bool  // whether a sheep has lost its fleece
	Captain     *bool  // whether a raider is its wave's captain
	HasRaid     *bool  // whether a raider belongs to or stands near a raid
	Mainhand    *LootItemPredicate
	// Mooshroom is the mooshroom/variant component of a mooshroom, when
	// asked.
	Mooshroom string
	// ChickenVariant is the chicken/variant component, when asked.
	ChickenVariant string
	// CubeSizeMin and CubeSizeMax bound a slime's or magma cube's size, nil
	// for an end the table left open.
	CubeSizeMin, CubeSizeMax *float64

	// The other flags of EntityFlagsPredicate, nil where not asked.
	OnGround, Sneaking, Sprinting, Swimming, Flying, InWater, FallFlying *bool
	// Head, Chest, Legs, Feet, Body and Offhand are the other equipment
	// stacks asked about.
	Head, Chest, Legs, Feet, Body, Offhand *LootItemPredicate
	// Effects maps an effect name to what the active instance must be;
	// every one listed must be active.
	Effects map[string]LootEffectPredicate
	// NBT is the compound the entity's saved data must contain.
	NBT nbt.CompoundTag
	// Team is the team the entity must belong to, when HasTeam.
	Team    string
	HasTeam bool
	// TagsAny, TagsAll and TagsNone are the scoreboard tags tests; nil
	// where not asked.
	TagsAny, TagsAll, TagsNone []string
	HasTags                    bool
	// Period asks tickCount % Period == 0; zero for no test.
	Period int32
	// VehiclePredicate, Passenger and Targeted are nested predicates tested
	// against the ridden body, any passenger and the mob's target.
	VehiclePredicate, Passenger, Targeted *LootEntityPredicate
	// Player, Lightning and FishingHook are the type-specific predicates;
	// each is nil where the table has none, and a present one fails for any
	// other kind of body.
	Player      *LootPlayerPredicate
	Lightning   *LootLightningPredicate
	FishingHook *LootFishingHookPredicate
	// Components maps an entity component's name to the value it must hold
	// exactly; every one listed must be held.
	// vanilla: EntityExactDataComponentsPredicate
	Components map[string]string
	// TagComponents maps a LootEntityTagComponents name to the value its
	// saved field must equal.
	TagComponents map[string]nbt.Tag
	// ComponentTests are the entity's partial component predicates.
	// vanilla: EntityPartialComponentsPredicate
	ComponentTests []LootComponentTest
	// Location, SteppingOn and MovementAffectedBy test the block at the
	// entity's position, below it and under its movement; Distance the offset
	// from the roll's origin; Movement its known movement; Slots the stacks
	// in named slot ranges. Nil (or empty) where not asked.
	Location, SteppingOn, MovementAffectedBy *LootLocationCheck
	Distance                                 *AdvancementDistance
	Movement                                 *LootMovement
	Slots                                    []LootSlotPredicate
}

// LootEntityComponents are the components an entity holds and an exact
// components predicate can name, all with a string value.
// vanilla: Entity.get, the applyImplicitComponent overrides
var LootEntityComponents = [...]string{
	"minecraft:wolf/variant", "minecraft:wolf/sound_variant", "minecraft:wolf/collar",
	"minecraft:cat/variant", "minecraft:cat/sound_variant", "minecraft:cat/collar",
	"minecraft:pig/variant", "minecraft:pig/sound_variant", "minecraft:cow/variant", "minecraft:cow/sound_variant",
	"minecraft:chicken/variant", "minecraft:chicken/sound_variant", "minecraft:frog/variant",
	"minecraft:zombie_nautilus/variant", "minecraft:villager/variant", "minecraft:sheep/color",
	"minecraft:mooshroom/variant", "minecraft:fox/variant", "minecraft:parrot/variant", "minecraft:axolotl/variant",
	"minecraft:salmon/size", "minecraft:rabbit/variant", "minecraft:horse/variant", "minecraft:llama/variant",
	"minecraft:shulker/color", "minecraft:tropical_fish/pattern", "minecraft:tropical_fish/base_color",
	"minecraft:tropical_fish/pattern_color",
}

// LootTagComponent is an entity component read from the entity's saved
// compound: the field Path under one of Types (nil for every kind of entity).
type LootTagComponent struct {
	Types []string
	Path  []string
}

// projectileTypes are the kinds extending Projectile, the only ones that
// hold can_break.
// vanilla: Projectile.applyImplicitComponent
var projectileTypes = []string{
	"minecraft:arrow", "minecraft:spectral_arrow", "minecraft:trident", "minecraft:fishing_bobber",
	"minecraft:fireball", "minecraft:small_fireball", "minecraft:dragon_fireball", "minecraft:wither_skull",
	"minecraft:wind_charge", "minecraft:breeze_wind_charge", "minecraft:experience_bottle", "minecraft:egg",
	"minecraft:snowball", "minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:ender_pearl",
	"minecraft:shulker_bullet", "minecraft:llama_spit", "minecraft:firework_rocket",
}

// LootEntityTagComponents are the components held by every entity or one
// non-mob kind, by name; their values are tags, not strings.
// vanilla: Entity.get, Painting.get, Cushion.get, AreaEffectCloud.get, Mannequin.get, SulfurCube.get, Projectile.applyImplicitComponent
var LootEntityTagComponents = map[string]LootTagComponent{
	"minecraft:custom_name":           {Path: []string{"CustomName"}},
	"minecraft:custom_data":           {Path: []string{"data"}},
	"minecraft:painting/variant":      {Types: []string{"minecraft:painting"}, Path: []string{"variant"}},
	"minecraft:cushion/color":         {Types: []string{"minecraft:cushion"}, Path: []string{"color"}},
	"minecraft:potion_contents":       {Types: []string{"minecraft:area_effect_cloud"}, Path: []string{"potion_contents"}},
	"minecraft:potion_duration_scale": {Types: []string{"minecraft:area_effect_cloud"}, Path: []string{"potion_duration_scale"}},
	"minecraft:profile":               {Types: []string{"minecraft:mannequin"}, Path: []string{"profile"}},
	"minecraft:sulfur_cube_content":   {Types: []string{"minecraft:sulfur_cube"}, Path: []string{"equipment", "body"}},
	"minecraft:can_break":             {Types: projectileTypes, Path: []string{"can_break"}},
}

// LootComponentTest is one partial component predicate of an entity: the
// villager/variant types one of which the entity must have, the potions one
// of which its potion_contents must hold, or the compound its custom_data
// must contain. Any other predicate type reads a component no entity holds,
// so Never is set and the test fails.
type LootComponentTest struct {
	Never         bool
	VillagerTypes []string
	// Potion marks a potion_contents test; Potions then lists the allowed
	// potions, nil for any.
	Potion     bool
	Potions    []string
	CustomData nbt.CompoundTag
}

// LootPlayerPredicate holds the fields of vanilla's PlayerPredicate; a
// range's nil end is open.
type LootPlayerPredicate struct {
	LevelMin, LevelMax           *float64
	FoodMin, FoodMax             *float64
	SaturationMin, SaturationMax *float64
	// GameModes lists the modes allowed, nil for any.
	GameModes []string
	Stats     []LootStatPredicate
	// Recipes maps a recipe name to whether the player must know it.
	Recipes map[string]bool
	// Advancements maps an advancement name to its test.
	Advancements map[string]LootAdvancementPredicate
	LookingAt    *LootEntityPredicate
	// Input holds the keys asked about, nil where not asked.
	Input *LootInputPredicate
}

// LootStatPredicate tests one statistic's count.
type LootStatPredicate struct {
	Type, Stat string
	Min, Max   *float64
}

// LootAdvancementPredicate is a done test (Criteria nil) or a per-criterion
// test.
type LootAdvancementPredicate struct {
	Done     bool
	Criteria map[string]bool
}

// LootInputPredicate holds the keys of vanilla's InputPredicate.
type LootInputPredicate struct {
	Forward, Backward, Left, Right, Jump, Sneak, Sprint *bool
}

// LootLightningPredicate is vanilla's LightningBoltPredicate.
type LootLightningPredicate struct {
	FiredMin, FiredMax *float64
	Struck             *LootEntityPredicate
}

// LootFishingHookPredicate is vanilla's FishingHookPredicate; a nil
// InOpenWater matches any hook.
type LootFishingHookPredicate struct {
	InOpenWater *bool
}

// LootMovement is a MovementPredicate.
type LootMovement struct{ X, Y, Z, Speed, HorizontalSpeed, VerticalSpeed, FallDistance AdvancementRange }

// LootSlotPredicate is one entry of an EntitySlotsPredicate: a slot range's
// name (as /item names it) and the item some slot in it must hold.
type LootSlotPredicate struct {
	Slots string
	Item  LootItemPredicate
}

// HasFlagTests reports whether the predicate asks after any flag of
// EntityFlagsPredicate besides OnFire and Baby.
func (p LootEntityPredicate) HasFlagTests() bool {
	return p.OnGround != nil || p.Sneaking != nil || p.Sprinting != nil || p.Swimming != nil ||
		p.Flying != nil || p.InWater != nil || p.FallFlying != nil
}

// LootEffectPredicate is one entry of an effects predicate: ranges with a
// nil end open, and booleans nil where not asked.
// vanilla: MobEffectsPredicate.MobEffectInstancePredicate
type LootEffectPredicate struct {
	AmplifierMin, AmplifierMax *float64
	DurationMin, DurationMax   *float64
	Ambient, Visible           *bool
}

// LootFunction is one function of an entry or a pool: a change to a stack
// the roll produced, made after the entry's own and before the pool's.
type LootFunction interface {
	isLootFunction()
}

// LootSetCount sets a stack's count from a number provider, or adds to it
// when Add is set.
type LootSetCount struct {
	Count      LootNumber
	Add        bool
	Conditions []LootCondition
}

// LootApplyBonus multiplies a stack's count out by a fortune-style bonus
// formula rolled against the tool's level of one enchantment.
type LootApplyBonus struct {
	Enchantment string
	Formula     LootBonusFormula
	Conditions  []LootCondition
}

// LootBonusFormula is one of the count bonuses: ore_drops,
// uniform_bonus_count or binomial_with_bonus_count.
type LootBonusFormula interface {
	isLootBonusFormula()
}

// LootOreDrops is the ore formula: the count times one more than a bonus
// that is zero with a chance of one in (level+1), else one to level.
type LootOreDrops struct{}

// LootUniformBonus is the uniform formula: the count plus a roll between
// nothing and the level times the multiplier, both ends inclusive.
type LootUniformBonus struct {
	Multiplier int32
}

// LootBinomialBonus is the binomial formula: level plus the extra rounds
// of a coin that lands with the probability given, each adding one.
type LootBinomialBonus struct {
	Extra       int32
	Probability float64
}

func (LootOreDrops) isLootBonusFormula()      {}
func (LootUniformBonus) isLootBonusFormula()  {}
func (LootBinomialBonus) isLootBonusFormula() {}

// LootExplosionDecay is the function that rolls each item of a stack
// against the blast's survival, one chance in the radius each.
type LootExplosionDecay struct {
	Conditions []LootCondition
}

// LootLimitCount clamps a stack's count into a range whose bounds are
// context int providers; a nil bound is open.
type LootLimitCount struct {
	Min, Max   LootNumber
	Conditions []LootCondition
}

// LootEnchantedCount grows a stack by the level of one enchantment on the
// killer's weapon times a number provider - the looting of a mob's drops.
type LootEnchantedCount struct {
	Enchantment string
	Count       LootNumber
	Limit       int32 // zero: no limit
	Conditions  []LootCondition
}

// LootFurnaceSmelt turns a stack into what smelting it would give - the
// cooked meat of a mob that died burning.
type LootFurnaceSmelt struct {
	UseInputCount bool // multiply the result count by the input count
	Conditions    []LootCondition
}

// LootCopyComponents copies components off the roll's block entity (a
// chest's name, a shulker box's contents and lock, a pot's decorations), the
// tool, or a context entity.
type LootCopyComponents struct {
	// Source is the context parameter's name: block_entity (also the empty
	// default), tool, this, attacker, direct_attacker, attacking_player,
	// target_entity or interacting_entity.
	Source     string
	Include    []string
	Exclude    []string
	Conditions []LootCondition
}

// LootCopyState copies block state properties into minecraft:block_state (a
// silk-touched hive's honey level, a statue's pose).
type LootCopyState struct {
	Block      string
	Properties []string
	Conditions []LootCondition
}

// LootSetPotion sets a stack's potion contents to the potion named - the
// stray's tipped arrow, the bogged's poisoned one.
type LootSetPotion struct {
	Potion     string
	Conditions []LootCondition
}

// LootSetRandomPotion chooses an option, or any registered potion if absent.
type LootSetRandomPotion struct {
	Options    []string
	Conditions []LootCondition
}

// LootSetRandomDyes blends the rolled number of random dye colours.
type LootSetRandomDyes struct {
	Count      LootNumber
	Conditions []LootCondition
}

// LootSetOminousBottleAmplifier sets an ominous bottle's amplifier, clamped
// to 0..4.
type LootSetOminousBottleAmplifier struct {
	Amplifier  LootNumber
	Conditions []LootCondition
}

// LootSetStewEffect adds one effect of the listed to a suspicious stew, its
// seconds twentyfold in ticks unless the effect is instantaneous.
// vanilla: SetStewEffectFunction
type LootSetStewEffect struct {
	Effects    []LootStewEffect
	Conditions []LootCondition
}

// LootStewEffect is one candidate effect of a stew, its duration given in
// seconds.
type LootStewEffect struct {
	Effect   string
	Duration LootNumber
}

// LootSetInstrument sets a goat horn's instrument to one of the options, the
// names a tag resolved to at generation time.
// vanilla: SetInstrumentFunction
type LootSetInstrument struct {
	Options    []string
	Conditions []LootCondition
}

// LootSetDamage sets a damageable stack's wear from a fraction of its
// durability left, or adds to that fraction when Add is set.
type LootSetDamage struct {
	Damage     LootNumber
	Add        bool
	Conditions []LootCondition
}

// LootSetName sets a stack's custom name, or its minecraft:item_name when
// ItemName is set.
type LootSetName struct {
	Name     protocol.Text
	ItemName bool
	// Entity names the context entity the name's selectors resolve against
	// (this, attacker, ...); empty leaves them for the client.
	Entity     string
	Conditions []LootCondition
}

// LootListOperation is a ListOperation: Mode is replace_all, replace_section,
// insert or append; Offset and Size (-1 for absent) are the section's.
// vanilla: ListOperation
type LootListOperation struct {
	Mode   string
	Offset int
	Size   int
}

// LootSetLore edits a stack's minecraft:lore by Mode.
// vanilla: SetLoreFunction
type LootSetLore struct {
	Lines     []protocol.Text
	Operation LootListOperation
	// Entity is LootSetName's Entity for the lines.
	Entity     string
	Conditions []LootCondition
}

// LootSetCustomData merges Tag into a stack's minecraft:custom_data.
// vanilla: SetCustomDataFunction
type LootSetCustomData struct {
	Tag        nbt.CompoundTag
	Conditions []LootCondition
}

// LootToggleTooltips shows or hides data components in a stack's tooltip,
// by component name.
// vanilla: ToggleTooltips
type LootToggleTooltips struct {
	Toggles    map[string]bool
	Conditions []LootCondition
}

// LootCopyName copies the custom name of Source (this, attacker,
// direct_attacker, attacking_player, target_entity, interacting_entity or
// block_entity) onto the stack; only block_entity has a name here.
// vanilla: CopyNameFunction
type LootCopyName struct {
	Source     string
	Conditions []LootCondition
}

// LootAttributeModifier is one modifier a set_attributes adds.
type LootAttributeModifier struct {
	ID        string
	Attribute string
	Operation AttributeOperation
	Amount    LootNumber
	Slots     []int32 // EquipmentSlotGroup ids, one drawn at random
}

// LootSetAttributes adds modifiers to a stack's minecraft:attribute_modifiers,
// or replaces them all.
// vanilla: SetAttributesFunction
type LootSetAttributes struct {
	Modifiers  []LootAttributeModifier
	Replace    bool
	Conditions []LootCondition
}

// LootExplorationMap turns an empty map into one leading to the nearest of
// the Destination structures (a tag's members, resolved at generation time)
// within SearchRadius chunks, marked with Decoration.
// vanilla: ExplorationMapFunction
type LootExplorationMap struct {
	Destination        []string
	Decoration         string
	Zoom               int32
	SearchRadius       int32
	SkipExistingChunks bool
	Conditions         []LootCondition
}

// LootSetEnchantments sets the level of each enchantment named, or adds to
// it when Add is set; a plain book turns into an enchanted one.
type LootSetEnchantments struct {
	Enchantments map[string]LootNumber
	Add          bool
	Conditions   []LootCondition
}

// LootEnchantRandomly adds one enchantment from Options (nil for all) at a
// random level; OnlyCompatible keeps to what the item supports. A non-empty
// OptionsTag replaces Options with the tag's members as loaded.
type LootEnchantRandomly struct {
	Options               []string
	OptionsTag            string
	OnlyCompatible        bool
	IncludeAdditionalCost bool
	Conditions            []LootCondition
}

// LootEnchantWithLevels enchants as an enchanting table offer of the rolled
// worth would, from Options (nil for all registered) or from OptionsTag's
// members as loaded.
type LootEnchantWithLevels struct {
	IncludeAdditionalCost bool
	Levels                LootNumber
	Options               []string
	OptionsTag            string
	Conditions            []LootCondition
}

// LootFiltered applies one branch according to the result stack's predicate.
// vanilla: FilteredFunction
type LootFiltered struct {
	Predicate      LootItemPredicate
	OnPass, OnFail LootFunction
	Conditions     []LootCondition
}

// LootSetItem turns a stack into another item, keeping its count and
// components.
type LootSetItem struct {
	Item       string
	Conditions []LootCondition
}

// LootSetComponents patches a stack's components: Set holds each set
// component's SNBT value by its qualified name, Remove the names removed.
type LootSetComponents struct {
	Set        map[string]string
	Remove     []string
	Conditions []LootCondition
}

// LootNBTSource is the NBT a copy_custom_data reads: the context's entity
// or block entity named by Target, or the command storage named by Storage.
// vanilla: ContextNbtProvider, StorageNbtProvider
type LootNBTSource struct {
	Target  string
	Storage string
}

// LootCopyOp is one operation of copy_custom_data: the value at the Source
// path of the source lands at the Target path of the stack's custom data
// by Op (replace, append or merge).
type LootCopyOp struct {
	Source, Target, Op string
}

// LootCopyCustomData copies parts of an NBT source into the stack's
// minecraft:custom_data.
type LootCopyCustomData struct {
	Source     LootNBTSource
	Ops        []LootCopyOp
	Conditions []LootCondition
}

// LootSetContents replaces the items a stack holds in Component
// (minecraft:container, bundle_contents or charged_projectiles) with what
// Entries make.
type LootSetContents struct {
	Component  string
	Entries    []LootEntry
	Conditions []LootCondition
}

// LootModifyContents runs Modifier over each non-empty stack a stack holds
// in Component (minecraft:container, bundle_contents or
// charged_projectiles).
// vanilla: ModifyContainerContents
type LootModifyContents struct {
	Component  string
	Modifier   LootFunction
	Conditions []LootCondition
}

// LootSetBannerPattern sets a stack's minecraft:banner_patterns to Patterns,
// or appends them when Append is set.
// vanilla: SetBannerPatternFunction
type LootSetBannerPattern struct {
	Patterns   []protocol.BannerPatternLayer
	Append     bool
	Conditions []LootCondition
}

// LootSetBookCover sets the title, author and generation of a stack's
// minecraft:written_book_content; a nil field keeps the stack's own.
// vanilla: SetBookCoverFunction
type LootSetBookCover struct {
	Title      *protocol.FilterablePage
	Author     *string
	Generation *int32
	Conditions []LootCondition
}

// LootStandAlone is a ListOperation with the values it lays.
// vanilla: ListOperation.StandAlone
type LootStandAlone[T any] struct {
	Values    []T
	Operation LootListOperation
}

// LootSetCustomModelData edits the lists of a stack's
// minecraft:custom_model_data; a nil list is left as it was. The float and
// colour providers are rolled per use.
// vanilla: SetCustomModelDataFunction
type LootSetCustomModelData struct {
	Floats     *LootStandAlone[LootNumber]
	Flags      *LootStandAlone[bool]
	Strings    *LootStandAlone[string]
	Colors     *LootStandAlone[LootNumber]
	Conditions []LootCondition
}

// LootSetFireworkExplosion edits a stack's minecraft:firework_explosion; a
// nil field keeps the stack's own (or the small ball's default).
// vanilla: SetFireworkExplosionFunction
type LootSetFireworkExplosion struct {
	Shape              *int32
	Colors, FadeColors *[]int32
	Trail, Twinkle     *bool
	Conditions         []LootCondition
}

// LootSetFireworks edits a stack's minecraft:fireworks; a nil field keeps
// the stack's own.
// vanilla: SetFireworksFunction
type LootSetFireworks struct {
	Explosions *LootStandAlone[protocol.FireworkExplosion]
	Flight     *int32
	Conditions []LootCondition
}

// LootSetContainerLoot sets a stack's minecraft:container_loot.
// vanilla: SetContainerLootTable
type LootSetContainerLoot struct {
	Table      string
	Seed       int64
	Conditions []LootCondition
}

// LootSetWritableBookPages edits the pages of a stack's
// minecraft:writable_book_content.
// vanilla: SetWritableBookPagesFunction
type LootSetWritableBookPages struct {
	Pages      []protocol.FilterablePage
	Operation  LootListOperation
	Conditions []LootCondition
}

// LootSetWrittenBookPages edits the pages of a stack's
// minecraft:written_book_content, which leaves it unresolved.
// vanilla: SetWrittenBookPagesFunction
type LootSetWrittenBookPages struct {
	Pages      []protocol.WrittenPage
	Operation  LootListOperation
	Conditions []LootCondition
}

// LootFillPlayerHead gives a player head the profile of Entity, the context
// target, when that is a player.
// vanilla: FillPlayerHead
type LootFillPlayerHead struct {
	Entity     string
	Conditions []LootCondition
}

// LootDiscard empties the stack.
type LootDiscard struct {
	Conditions []LootCondition
}

// LootFunctionSequence runs its functions one after another.
type LootFunctionSequence struct {
	Functions []LootFunction
}

// LootFunctionReference runs the data pack's item modifier Name, looked up
// when it runs; an unknown name, or one already running, leaves the stack.
// vanilla: FunctionReference, a LootItemFunctions.CODEC holder by id
type LootFunctionReference struct {
	Name       string
	Conditions []LootCondition
}

// lootFloat returns a pointer to f; nil means the table carried no bound.
//
//go:fix inline
func lootFloat(f float64) *float64 { return new(f) }

// lootBool returns a pointer to b; nil means the table did not ask.
//
//go:fix inline
func lootBool(b bool) *bool { return new(b) }

// lootItem returns a pointer to p; nil means the table did not ask after
// that slot.
//
//go:fix inline
func lootItem(p LootItemPredicate) *LootItemPredicate { return new(p) }

func (LootSetCount) isLootFunction()                  {}
func (LootApplyBonus) isLootFunction()                {}
func (LootExplosionDecay) isLootFunction()            {}
func (LootLimitCount) isLootFunction()                {}
func (LootEnchantedCount) isLootFunction()            {}
func (LootFurnaceSmelt) isLootFunction()              {}
func (LootCopyComponents) isLootFunction()            {}
func (LootCopyState) isLootFunction()                 {}
func (LootSetRandomPotion) isLootFunction()           {}
func (LootSetRandomDyes) isLootFunction()             {}
func (LootSetPotion) isLootFunction()                 {}
func (LootSetOminousBottleAmplifier) isLootFunction() {}
func (LootSetStewEffect) isLootFunction()             {}
func (LootSetInstrument) isLootFunction()             {}

func (LootSetDamage) isLootFunction()            {}
func (LootSetName) isLootFunction()              {}
func (LootSetLore) isLootFunction()              {}
func (LootSetCustomData) isLootFunction()        {}
func (LootToggleTooltips) isLootFunction()       {}
func (LootCopyName) isLootFunction()             {}
func (LootSetAttributes) isLootFunction()        {}
func (LootExplorationMap) isLootFunction()       {}
func (LootSetEnchantments) isLootFunction()      {}
func (LootEnchantRandomly) isLootFunction()      {}
func (LootEnchantWithLevels) isLootFunction()    {}
func (LootSetItem) isLootFunction()              {}
func (LootSetComponents) isLootFunction()        {}
func (LootCopyCustomData) isLootFunction()       {}
func (LootSetContents) isLootFunction()          {}
func (LootDiscard) isLootFunction()              {}
func (LootModifyContents) isLootFunction()       {}
func (LootSetBannerPattern) isLootFunction()     {}
func (LootSetBookCover) isLootFunction()         {}
func (LootSetCustomModelData) isLootFunction()   {}
func (LootSetFireworkExplosion) isLootFunction() {}
func (LootSetFireworks) isLootFunction()         {}
func (LootSetContainerLoot) isLootFunction()     {}
func (LootSetWritableBookPages) isLootFunction() {}
func (LootSetWrittenBookPages) isLootFunction()  {}
func (LootFillPlayerHead) isLootFunction()       {}
func (LootFiltered) isLootFunction()             {}
func (LootFunctionSequence) isLootFunction()     {}
func (LootFunctionReference) isLootFunction()    {}

// LootTables is every built-in table, keyed by name (minecraft:blocks/
// diamond_ore, minecraft:entities/sheep/white), filled by loot_gen.go.
var LootTables map[string]LootTable

// FindLootTable returns the table by name, a data pack's table replacing
// the built-in one.
func FindLootTable(name string) (LootTable, bool) {
	if t, ok := PackLootTable(name); ok {
		return t, true
	}
	if st := packLoot.Load(); st != nil && st.hidden[name] {
		return LootTable{}, false
	}
	t, ok := LootTables[name]
	return t, ok
}
