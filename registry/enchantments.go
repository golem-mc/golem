package registry

import (
	"math/rand"
	"slices"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
)

// The effects of the minecraft:enchantment registry, read from the data
// pack's data/minecraft/enchantment/*.json by tools/gen enchantments into
// the generated table: effect components, each a list of value effects,
// entity effects or attribute modifiers gated by loot conditions. They are
// interpreted in internal/server/rules/enchantments.go, game logic staying out of
// this package.
//
// Only evaluated components are carried; the generated file's header names
// what was dropped.

// EnchLevelValue is one level-based value; Kind names the variant, the
// other variants' fields are zero. vanilla: LevelBasedValue
type EnchLevelValue struct {
	Kind EnchLevelValueKind
	// Constant carries the minecraft:constant variant's value, which the
	// data files write as a bare number.
	Constant float32
	// Base and PerLevel carry the minecraft:linear variant: the value at
	// level one, and what each level above the first adds.
	Base, PerLevel float32
	// Added carries the minecraft:levels_squared variant: the square of
	// the level, plus this.
	Added float32
	// Min and Max carry the minecraft:clamped variant's bounds, which the
	// calculation carried by Value is cut to.
	Min, Max float32
	Value    *EnchLevelValue
	// Values and Fallback carry the minecraft:lookup variant: the value
	// per level while the list reaches, the fallback past it.
	Values   []float32
	Fallback *EnchLevelValue
	// Num and Den carry the minecraft:fraction variant: the numerator over
	// the denominator, zero where the denominator computes to zero.
	Num, Den *EnchLevelValue
	// ExpBase and ExpPower carry the minecraft:exponent variant: the base
	// raised to the power, both computed at the level.
	ExpBase, ExpPower *EnchLevelValue
}

// EnchLevelValueKind names which variant of a level-based value a node is:
// the types of the vanilla enchantment_level_based_value_type registry.
type EnchLevelValueKind uint8

const (
	EnchValueConstant      EnchLevelValueKind = iota // minecraft:constant
	EnchValueLinear                                  // minecraft:linear
	EnchValueFraction                                // minecraft:fraction
	EnchValueLevelsSquared                           // minecraft:levels_squared
	EnchValueClamped                                 // minecraft:clamped
	EnchValueLookup                                  // minecraft:lookup
	EnchValueExponent                                // minecraft:exponent
)

// EnchEffect is one value effect. A component's effects chain, each taking
// the value the one before left. vanilla: EnchantmentValueEffect
type EnchEffect struct {
	Kind EnchEffectKind
	// Value is the add's addend, the multiply's factor, the set value, or
	// remove_binomial's per-point chance.
	Value *EnchLevelValue
	// Exponent is the exponential's exponent; Value is its base.
	Exponent *EnchLevelValue
	// Effects carries the minecraft:all_of variant's own list, each run on
	// the value in order.
	Effects []EnchEffect
}

// EnchEffectKind names which value effect a node is.
type EnchEffectKind uint8

const (
	EnchAdd      EnchEffectKind = iota // minecraft:add
	EnchMultiply                       // minecraft:multiply
	EnchSet                            // minecraft:set
	EnchAllOf                          // minecraft:all_of
	// EnchRemoveBinomial keeps each point of damage only where a roll
	// against the chance keeps it (unbreaking).
	EnchRemoveBinomial
	// EnchExponential scales the value by base^exponent.
	EnchExponential
)

// EnchCondition is an effect's "requirements" condition.
// vanilla: LootItemCondition
type EnchCondition struct {
	Kind EnchConditionKind
	// Thundering and CanSeeSky are optional weather and location tests.
	Thundering *bool
	CanSeeSky  *bool
	// BlockTag is location_check's block tag, without the # prefix.
	BlockTag string
	// ToolItems and ToolTag (without '#') are match_tool's item set; both
	// empty matches anything.
	ToolItems []Item
	ToolTag   string
	// Entity is entity_properties' entity ("this", "direct_attacker",
	// "attacker"); EntityTypeTag (without '#') or EntityTypeID is its
	// entity_type test. Unmodelled predicates make the generator refuse the file.
	Entity        string
	EntityTypeTag string
	EntityTypeID  string
	// IsFlying and MinFallDistance test the direct attacker's flags and movement.
	IsFlying        *bool
	MinFallDistance *float64
	// DamageTags and Direct are damage_source_properties: holder sets with
	// expected membership, and is_direct as 1 true, 0 false, -1 not asked.
	DamageTags []EnchTagExpect
	Direct     int8
	// Chance carries the minecraft:random_chance variant's probability, a
	// level-based value read at the enchantment's level.
	Chance *EnchLevelValue
	// State is entity_properties' user-state test, on Entity.
	State *EnchEntityState
	// Terms carries the all_of and any_of variants' own conditions.
	Terms []*EnchCondition
	// Term carries the minecraft:inverted variant's single condition.
	Term *EnchCondition
	// Body is EnchCondBody's predicate, tested on the wearer's snapshot.
	Body *LootEntityPredicate
	// Active is EnchCondActive's expected value.
	Active bool
	// Loot is EnchCondLoot's condition, which every entry must pass.
	Loot []LootCondition
}

// EnchConditionKind names which condition a node is.
type EnchConditionKind uint8

const (
	EnchCondMatchTool        EnchConditionKind = iota // minecraft:match_tool
	EnchCondEntityProperties                          // minecraft:entity_properties
	EnchCondDamageSource                              // minecraft:damage_source_properties
	EnchCondAllOf                                     // minecraft:all_of
	EnchCondAnyOf                                     // minecraft:any_of
	EnchCondInverted                                  // minecraft:inverted
	EnchCondRandomChance                              // minecraft:random_chance
	EnchCondWeatherCheck
	EnchCondLocationCheck
	EnchCondEntityState // minecraft:entity_properties with vehicle, flags or player data
	EnchCondBody        // minecraft:entity_properties on "this" asking after movement, ground contact or age
	EnchCondActive      // minecraft:enchantment_active_check
	EnchCondLoot        // any other loot condition, tested with rules.TestLootCondition
)

// EnchEntityState is the user-state half of an entity_properties predicate:
// Vehicle needs a vehicle, FallFlying and InWater match the flag, Player needs
// a player of one of Gamemodes (any when empty) with food level FoodMin or more.
// vanilla: EntityPredicate
type EnchEntityState struct {
	Vehicle, Player     bool
	FallFlying, InWater *bool
	Gamemodes           []string
	FoodMin             *int32
}

// EnchTagExpect is a damage-type holder set and its expected membership.
type EnchTagExpect struct {
	Types    *DamageTypeSet
	Expected bool
}

// EnchAttribute is a modifier applied while the item sits in a matching
// slot. ID is the data file's; the equipment walk appends the slot name, one
// modifier per slot. Operation is add_value, add_multiplied_base or
// add_multiplied_total.
// vanilla: EnchantmentAttributeEffect
type EnchAttribute struct {
	Attribute string
	ID        string
	Operation string
	Amount    *EnchLevelValue
}

// EnchEntityEffect is one hit or projectile-spawned effect; all_of sets several fields.
// vanilla: EnchantmentEntityEffect
type EnchEntityEffect struct {
	Explode          *EnchExplode
	SummonEntity     string
	PlaySound        *EnchPlaySound
	Duration         *EnchLevelValue
	MobEffect        *EnchMobEffect
	DamageEntity     *EnchDamageEntity
	ChangeItemDamage *EnchLevelValue
	ApplyExhaustion  *EnchLevelValue
	ApplyImpulse     *EnchApplyImpulse
	RunFunction      string
	Replace          *EnchReplace
	SetProperties    *EnchSetProperties
	Particles        *EnchParticles
	Summon           *EnchSummon
}

// EnchBlockPredicate is the block predicate of a replace effect: a kind
// (matching_blocks, matching_block_tag, matching_fluids, matching_biomes, solid,
// replaceable, has_sturdy_face, would_survive, inside_world_bounds, height_range,
// volume_match, unobstructed, not, all_of, any_of, true) over the block at the
// predicate's own offset from the tested position. Terms holds the operands of
// not, all_of, any_of and volume_match's match.
// vanilla: BlockPredicate
type EnchBlockPredicate struct {
	Kind   string
	Offset [3]int32
	Blocks []Block
	Tag    BlockTagID
	Terms  []*EnchBlockPredicate
	// Names are the fluids of matching_fluids or the biomes of matching_biomes.
	Names []string
	// Face is has_sturdy_face's direction, as a block state property value.
	Face string
	// State is would_survive's state.
	State uint16
	// Min and Max are volume_match's inclusive offsets.
	Min, Max [3]int32
	// Bottom and Top are height_range's anchors: Kind is absolute, above_bottom
	// or below_top, Value its number.
	Bottom, Top EnchAnchor
}

// EnchAnchor is a vertical anchor.
// vanilla: VerticalAnchor
type EnchAnchor struct {
	Kind  string
	Value int32
}

// EnchReplace is a replace_block effect, or a replace_disk one when Radius is set.
// State is the simple state provider's state.
// vanilla: ReplaceBlock, ReplaceDisk
type EnchReplace struct {
	Offset [3]int32
	State  uint16
	// Provider is the raw JSON of a state provider other than a plain state;
	// when set it picks the state for each cell, and State is unused.
	Provider  any
	Predicate *EnchBlockPredicate
	GameEvent GameEvent
	Radius    *EnchLevelValue
	Height    *EnchLevelValue
}

// EnchSetProperties is a set_block_properties effect.
// vanilla: SetBlockProperties
type EnchSetProperties struct {
	Offset    [3]int32
	Props     []PropValue // only properties some block has; a block without one skips it
	GameEvent GameEvent   // GameEventNone fires nothing
}

// EnchFloatRange is a float provider: a constant when Max <= Min, else uniform in [Min, Max).
type EnchFloatRange struct{ Min, Max float32 }

// EnchParticleAxis is one axis of spawn_particles: where it spawns (InBox
// picks a random spot of the body's box scaled by Scale; else the position)
// plus Offset, and the velocity Movement * the body's movement + Base.
type EnchParticleAxis struct {
	InBox         bool
	Offset, Scale float32
	Movement      float32
	Base          EnchFloatRange
}

// EnchParticles is a spawn_particles effect with a data-less particle.
// vanilla: SpawnParticlesEffect
type EnchParticles struct {
	Particle             int32
	Horizontal, Vertical EnchParticleAxis
	Speed                EnchFloatRange
}

// EnchSummon is a summon_entity effect: one of Entities is picked at random.
// vanilla: SummonEntityEffect
type EnchSummon struct {
	Entities []string
	JoinTeam bool
}

// EnchApplyImpulse is an apply_impulse effect along the user's look (the
// direction (0, 0, 1) in its own frame), scaled per axis by CoordinateScale,
// then by Magnitude at the level.
// vanilla: ApplyEntityImpulse
type EnchApplyImpulse struct {
	CoordinateScale [3]float32
	Magnitude       *EnchLevelValue
}

// EnchExplode is an explode effect. ImmuneBlocks is a block tag name and
// ImmuneList block names; with neither, every block keeps its own resistance.
// DamageType empty means the blast hurts no entity. The wind burst is the one
// shape with the gust particles and sound, which the server plays itself.
type EnchExplode struct {
	Radius, KnockbackMultiplier    *EnchLevelValue
	BlockInteraction, ImmuneBlocks string
	ImmuneList                     []string
	AttributeToUser, CreateFire    bool
	DamageType                     string
	Offset                         [3]float64
	// Look is the particles and sound the blast sends; nil for the wind burst
	// and the generated table's explosions.
	Look *EnchExplosionLook
}

// EnchExplosionLook is an explode effect's small_particle, large_particle,
// block_particles and sound. A particle is an ID of a particle with no data.
// vanilla: ExplodeEffect
type EnchExplosionLook struct {
	Small, Large int32
	Sound        protocol.SoundHolder
	Blocks       []EnchBlockParticle
}

// EnchBlockParticle is one weighted entry of block_particles.
// vanilla: ExplosionParticleInfo
type EnchBlockParticle struct {
	Particle       int32
	Scaling, Speed float32
	Weight         int32
}

// WindBurst reports whether the effect is the wind charge's harmless burst.
func (e *EnchExplode) WindBurst() bool {
	return e.BlockInteraction == "trigger" && e.ImmuneBlocks == "minecraft:blocks_wind_charge_explosions" && e.DamageType == ""
}

// EnchPlaySound is a sound effect with constant volume; Sounds overrides
// Sound with a list indexed by level - 1, clamped. The pitch is Pitch, or
// uniform in [Pitch, PitchMax) when PitchMax is above it.
type EnchPlaySound struct {
	Sound         protocol.SoundHolder
	Sounds        []protocol.SoundHolder
	Volume, Pitch float32
	PitchMax      float32
}

// PitchWith samples the pitch.
// vanilla: UniformFloat.sample
func (s *EnchPlaySound) PitchWith(rng *rand.Rand) float32 {
	if s.PitchMax <= s.Pitch || rng == nil {
		return s.Pitch
	}
	return rng.Float32()*(s.PitchMax-s.Pitch) + s.Pitch
}

// EnchDamageEntity is the hurt a minecraft:damage_entity effect deals: a
// damage of the named type, rolled between the two ends at the level.
type EnchDamageEntity struct {
	DamageType DamageType
	Min, Max   *EnchLevelValue
}

// EnchMobEffect is apply_mob_effect's effect, with the amplifier and
// duration (seconds) ranges rolled at the enchantment's level.
type EnchMobEffect struct {
	Name         string
	MinAmplifier *EnchLevelValue
	MaxAmplifier *EnchLevelValue
	MinDuration  *EnchLevelValue
	MaxDuration  *EnchLevelValue
}

// EnchTargeted is a post-attack effect with its targets: the enchanted side
// of the hit and the body affected ("attacker", "victim", "damaging_entity").
// vanilla: TargetedConditionalEffect
type EnchTargeted struct {
	Enchanted string
	Affected  string
	Effect    EnchEntityEffect
	Requires  *EnchCondition
}

// EnchTargetedValue is a value effect with its targets, like EnchTargeted.
// vanilla: TargetedConditionalEffect
type EnchTargetedValue struct {
	Enchanted string
	Affected  string
	Effect    EnchEffect
	Requires  *EnchCondition
}

// EnchConditional is a value effect with its condition; nil passes always.
// vanilla: ConditionalEffect
type EnchConditional struct {
	Effect   EnchEffect
	Requires *EnchCondition
}

// EnchSpawned is a projectile_spawned or hit_block effect with its condition. vanilla: ConditionalEffect
type EnchSpawned struct {
	Effect   EnchEntityEffect
	Requires *EnchCondition
}

// EnchLocated is one location_changed or tick entry: an entity effect or, for
// location_changed only, an attribute modifier held while Requires does.
// Group numbers the data file's entry: an all_of of attribute effects is one
// entry that becomes several EnchLocated sharing a Group, active together.
// vanilla: ConditionalEffect<EnchantmentLocationBasedEffect>
type EnchLocated struct {
	Effect    EnchEntityEffect
	Attribute *EnchAttribute
	Requires  *EnchCondition
	Group     int
}

// EnchImmunity is one damage_immunity entry: the damage it makes the wearer
// immune to where Requires holds (nil always). vanilla: DamageImmunity
type EnchImmunity struct {
	Requires *EnchCondition
}

// EnchChargingSounds is one crossbow_charging_sounds entry; a sound with ID -1
// is absent (vanilla's empty Optional). vanilla: CrossbowItem.ChargingSounds
type EnchChargingSounds struct {
	Start, Mid, End protocol.SoundHolder
}

// EnchantmentEffects is one enchantment's evaluated effect components.
type EnchantmentEffects struct {
	Damage                    []EnchConditional
	Knockback                 []EnchConditional
	ArmorEffectiveness        []EnchConditional
	DamageProtection          []EnchConditional
	ItemDamage                []EnchConditional
	AmmoUse                   []EnchConditional
	FishingTimeReduction      []EnchConditional
	FishingLuckBonus          []EnchConditional
	ProjectileCount           []EnchConditional
	ProjectileSpread          []EnchConditional
	ProjectilePiercing        []EnchConditional
	TridentReturnAcceleration []EnchConditional
	CrossbowChargeTime        *EnchEffect
	TridentSpinAttackStrength *EnchEffect
	RepairWithXp              []EnchConditional
	BlockExperience           []EnchConditional
	// MobExperience scales a mob kill's experience by the killer's equipment.
	MobExperience []EnchConditional
	// EquipmentDrops adds to a slot's drop chance, from the victim's or the
	// attacker's equipment.
	EquipmentDrops []EnchTargetedValue
	// PreventEquipmentDrop keeps the stack from dropping when its holder dies.
	PreventEquipmentDrop bool
	// PreventArmorChange keeps the stack in its armour slot against the
	// player taking it off, a shear or a zombie villager's cure.
	PreventArmorChange bool
	// DamageImmunity makes the wearer immune to the damage its entries name.
	DamageImmunity []EnchImmunity
	// CrossbowChargingSounds and TridentSound are lists indexed by level - 1,
	// clamped, the highest-level enchantment on the stack choosing.
	CrossbowChargingSounds []EnchChargingSounds
	TridentSound           []protocol.SoundHolder
	PostAttack             []EnchTargeted
	// PostPiercingAttack runs after every spear jab, hit or miss.
	PostPiercingAttack []EnchSpawned
	ProjectileSpawned  []EnchSpawned
	HitBlock           []EnchSpawned
	Attributes         []EnchAttribute
	// LocationChanged runs when the wearer's block changes or the stack is
	// equipped, Tick every tick; both come from a data pack only.
	LocationChanged, Tick []EnchLocated
	// SmashDamagePerFallenBlock is the mace smash's bonus per block fallen.
	SmashDamagePerFallenBlock []EnchConditional
}

// EnchantmentDef is one minecraft:enchantment entry as this server reads it:
// the EquipmentSlotGroup names it works in, its max level, the enchanting
// table's cost numbers and its effects.
type EnchantmentDef struct {
	Name     string
	Slots    []string
	MaxLevel int32
	Effects  EnchantmentEffects

	// Weight is the enchantment's share of the table's weighted draws: the
	// EnchantmentInstance.weight vanilla's WeightedRandom walks.
	Weight int32

	// AnvilCost is the levels an anvil charges per level added, doubled for
	// a same-item combine and halved from a book; 1 when the file omits it.
	AnvilCost int32

	// MinCostBase/PerLevel and MaxCostBase/PerLevel are min_cost and
	// max_cost, always linear: base at level 1, plus per level above. They
	// gate which levels an offer of a given worth may carry.
	MinCostBase     float32
	MinCostPerLevel float32
	MaxCostBase     float32
	MaxCostPerLevel float32

	// SupportedItems, PrimaryItems and ExclusiveSet are tag names, empty
	// when omitted. Primary is the shorter list a table picks from,
	// supported what canEnchant and the anvil accept, and the exclusive set
	// the family that cannot share an item.
	SupportedItems string
	PrimaryItems   string
	ExclusiveSet   string

	// SupportedList, PrimaryList and ExclusiveList are the same sets where a
	// data pack wrote one id or a list of ids instead of a tag; nil when
	// omitted or a tag.
	SupportedList, PrimaryList []Item
	ExclusiveList              []string
}

// Supports reports whether the enchantment accepts the item; no set accepts
// everything. vanilla: Enchantment.isSupportedItem
func (d *EnchantmentDef) Supports(item Item) bool {
	return itemInSet(item, d.SupportedItems, d.SupportedList)
}

// PrimaryFor reports whether the item is in the primary set, which an
// omitted set makes every item. vanilla: Enchantment.isPrimaryItem
func (d *EnchantmentDef) PrimaryFor(item Item) bool {
	return itemInSet(item, d.PrimaryItems, d.PrimaryList)
}

func itemInSet(item Item, tag string, list []Item) bool {
	switch {
	case tag != "":
		return item.InTag(ItemTagOf(tag))
	case list != nil:
		return slices.Contains(list, item)
	}
	return true
}

// ExcludesWith reports whether the enchantment's exclusive set holds the
// enchantment with the given ID. vanilla: Enchantment.areCompatible
func (d *EnchantmentDef) ExcludesWith(id int32) bool {
	switch {
	case d.ExclusiveSet != "":
		return EnchantmentInTag(id, d.ExclusiveSet)
	case d.ExclusiveList != nil:
		name, ok := EnchantmentName(id)
		return ok && slices.Contains(d.ExclusiveList, name)
	}
	return false
}

// MinCost is the smallest offer worth that may carry level: the linear cost
// truncated toward zero, as Java narrows a float. vanilla: Enchantment.getMinCost
func (d *EnchantmentDef) MinCost(level int32) int32 {
	return int32(d.MinCostBase + d.MinCostPerLevel*float32(level-1))
}

// MaxCost is the largest offer worth a level of this enchantment may carry.
func (d *EnchantmentDef) MaxCost(level int32) int32 {
	return int32(d.MaxCostBase + d.MaxCostPerLevel*float32(level-1))
}

// MatchesSlot reports whether the enchantment works in the named slot
// (mainhand, offhand, head, chest, legs, feet), via SlotGroupHolds.
func (d *EnchantmentDef) MatchesSlot(slot string) bool {
	for _, s := range d.Slots {
		if SlotGroupHolds(s, slot) {
			return true
		}
	}
	return false
}

// enchantmentIndex is the enchantment definitions by registry name and by
// minecraft:enchantment ID, with the data packs' definitions layered over the
// generated table, since the evaluator asks per stack.
type enchantmentIndex struct {
	gen    uint64
	byName map[string]*EnchantmentDef
	byID   []*EnchantmentDef
}

var (
	enchantmentDefs  atomic.Pointer[enchantmentIndex]
	enchantmentGen   atomic.Uint64
	enchantmentPacks atomic.Pointer[map[string]EnchantmentDef]
)

func init() { enchantmentDefs.Store(indexEnchantments(nil)) }

// indexEnchantments reads the registry's current entries, so it runs again
// when ApplyPackRegistries changes them.
func indexEnchantments(packs map[string]EnchantmentDef) *enchantmentIndex {
	m := make(map[string]*EnchantmentDef, len(EnchantmentEffectsTable)+len(packs))
	for i := range EnchantmentEffectsTable {
		m[EnchantmentEffectsTable[i].Name] = &EnchantmentEffectsTable[i]
	}
	for name, def := range packs {
		def.Name = name
		m[name] = &def
	}
	idx := &enchantmentIndex{byName: m, gen: enchantmentGen.Add(1)}
	if r, ok := Get("minecraft:enchantment"); ok {
		idx.byID = make([]*EnchantmentDef, len(r.Entries))
		for i, name := range r.Entries {
			idx.byID[i] = m[name]
		}
	}
	return idx
}

func reindexEnchantments() {
	var packs map[string]EnchantmentDef
	if p := enchantmentPacks.Load(); p != nil {
		packs = *p
	}
	enchantmentDefs.Store(indexEnchantments(packs))
}

// ApplyPackEnchantments makes the data packs' enchantment definitions, by
// name, what EnchantmentEffectsByID answers over the built-in ones; nil takes
// them away. A name needs an ID in the minecraft:enchantment registry, so it
// runs after ApplyPackRegistries. vanilla: RegistryDataLoader.WORLD_REGISTRIES
func ApplyPackEnchantments(packs map[string]EnchantmentDef) {
	enchantmentPacks.Store(&packs)
	reindexEnchantments()
}

// EnchantmentEffectsByID returns one enchantment by its minecraft:enchantment
// ID, or false for an ID the registry lacks.
func EnchantmentEffectsByID(id int32) (*EnchantmentDef, bool) {
	byID := enchantmentDefs.Load().byID
	if id < 0 || int(id) >= len(byID) || byID[id] == nil {
		return nil, false
	}
	return byID[id], true
}

// EnchantmentGeneration changes whenever the enchantment definitions do, so a
// cache of what they say can tell it is stale.
func EnchantmentGeneration() uint64 { return enchantmentDefs.Load().gen }
