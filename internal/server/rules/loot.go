package rules

// The loot engine: rolls a table from registry/loot_gen.go (pools, entries,
// conditions, functions as plain structs) and returns what came out. The
// formulas are vanilla's: ApplyBonusCount's three bonuses,
// EnchantedCountIncreaseFunction's looting, ExplosionCondition's
// one-in-the-radius survival and its decay twin, LootItemRandomChance, and
// the uniform and binomial providers with Mth.nextInt's inclusive roll.
//
// One caller-supplied *rand.Rand (the world's for a break, the blast's for an
// explosion, the mob's for a death) serves a whole roll. Distributions match
// vanilla, streams do not: Go's math/rand stands in for RandomSource.
// vanilla: LootContext

import (
	"maps"
	"math"
	"math/rand"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// LootTool is the item in play (the breaking tool, the killing weapon) as a
// table reads it. The zero value is no item: a bare hand, an explosion, or a
// death with nobody behind it.
type LootTool struct {
	Item  string // the item's name, empty for no tool
	Count int32  // how many of it there are, for the predicates that count
	// Enchantments is the stack's enchantments by name and level, the
	// fortune and silk touch and looting the tables' conditions and
	// functions read.
	Enchantments       map[string]int32
	StoredEnchantments map[string]int32
	HasDyedColor       bool
	HasMapID           bool
	// Stack is the stack itself, for the item predicates that read its
	// components.
	Stack protocol.ItemStack
}

// ToolOf reads a stack's item and enchantments by table name; an empty or
// unknown stack is no tool.
func ToolOf(stack protocol.ItemStack) LootTool {
	if stack.Empty() {
		return LootTool{}
	}
	if stack.ItemID < 0 || int(stack.ItemID) >= len(registry.Items) {
		return LootTool{}
	}
	name := registry.Item(stack.ItemID).Name()
	_, hasMapID := stack.MapIDOf()
	tool := LootTool{Stack: stack, HasMapID: hasMapID, Item: name, Count: stack.Count, HasDyedColor: stack.HasDyedColor(), Enchantments: map[string]int32{}}
	for id, level := range stack.Enchantments {
		if ench, ok := registry.EnchantmentName(id); ok {
			tool.Enchantments[ench] = level
		}
	}
	if len(stack.StoredEnchantments) > 0 {
		tool.StoredEnchantments = make(map[string]int32, len(stack.StoredEnchantments))
		for id, level := range stack.StoredEnchantments {
			if name, ok := registry.EnchantmentName(id); ok {
				tool.StoredEnchantments[name] = level
			}
		}
	}
	return tool
}

// LootEntity is a body as a table's entity conditions read it: the one
// dying, the attacker, the thing the blow rode. The zero value is nobody, as
// for an explosion's "this" or an old-age death's attacker.
type LootEntity struct {
	Type           string // the entity type's name, empty for nobody
	OnFire         bool
	Baby           bool
	Vehicle        string // the type of the body it rides, empty for none
	Sheep          bool   // the body is a sheep, which the colour and fleece ask about
	SheepColour    string // the sheep's colour, empty for none
	Sheared        bool   // the sheep has lost its fleece
	Raider         bool   // the body is a Raider subclass
	Captain        bool   // the raider leads its wave under the ominous banner
	HasRaid        bool   // the raider belongs to or stands near an active raid
	Mooshroom      string // the mooshroom's variant, empty for none
	ChickenVariant string // the chicken's variant, empty for none
	Mainhand       LootTool
	FrogVariant    string
	CubeSize       int            // a slime's or magma cube's size, zero for any other body
	Name           *protocol.Text // the custom name, nil for none; copy_name reads it
	// ScoreHolder is the body's scoreboard name (a player's name, anything
	// else's UUID), empty where the caller does not know it.
	// vanilla: Entity.getScoreboardName
	ScoreHolder string
	// Profile is a player's game profile, nil for any other body;
	// fill_player_head reads it.
	Profile *protocol.Profile
	// NBT reads the body's saved compound, read by copy_custom_data; nil
	// where the caller cannot write it. The result may be shared.
	// vanilla: NbtPredicate.getEntityTagToCompare
	NBT func() nbt.Compound

	// Facts reads the live flags, effects and age; nil for none. It is called
	// only when a predicate asks for one of them.
	Facts func() LootFacts
	// Head, Chest, Legs, Feet, Body and Offhand are the equipment stacks
	// besides Mainhand.
	Head, Chest, Legs, Feet, Body, Offhand LootTool
	// Tags are the scoreboard tags; Team the team's name, empty for none.
	Tags []string
	Team string
	// VehicleOf, Passengers and TargetOf build the related bodies' snapshots
	// when a predicate asks; nil where the caller cannot say.
	VehicleOf  func() (LootEntity, bool)
	Passengers func() []LootEntity
	TargetOf   func() (LootEntity, bool)

	// Player is the facts only a player has; nil for any other body.
	Player *LootPlayer
	// Lightning and FishingHook are set for a bolt and a bobber.
	Lightning   *LootLightning
	FishingHook *LootFishingHook
	// Component reads an entity component's value; nil where the body holds
	// none. vanilla: Entity.get
	Component func(name string) (string, bool)
	// Place is where the body stands, nil where the caller cannot say; a
	// predicate asking after a position fails then.
	Place *LootPlace
	// Slot reads the stacks a slot range's name (as /item names it) spans,
	// false for a name that is no range; nil where the caller cannot say.
	// vanilla: SlotsPredicate.matches over Entity.getSlot
	Slot func(name string) ([]LootTool, bool)
	// SlotStacks reads a copy of the stack in each slot a range's name
	// spans, empty slots included; nil where the caller cannot say.
	// vanilla: SlotProvider.getSlotsFromRange
	SlotStacks func(name string) []protocol.ItemStack
}

// LootPlace is a body's position and the blocks under it.
type LootPlace struct {
	X, Y, Z float64
	// OnPos is the block Entity.getOnPos names and AffectingPos the one
	// getBlockPosBelowThatAffectsMyMovement does; OnGround is
	// Entity.onGround, which SteppingOnPredicate asks first.
	OnPos, AffectingPos [3]int32
	OnGround            bool
}

// LootEntityFlags are the booleans of vanilla's EntityFlagsPredicate.
type LootEntityFlags struct {
	OnGround, Sneaking, Sprinting, Swimming, Flying, InWater, FallFlying bool
}

// Freeze reads the live facts and related bodies now, so the snapshot can
// be tested later without the locks that read them. Related bodies are
// frozen to their own relations too; a body already on the path from the
// root (a rider and its vehicle name each other) keeps no relations, and a
// body with no ScoreHolder to tell it by stops at lootFreezeDepth.
func (e *LootEntity) Freeze() { e.freeze(map[string]bool{}, 0) }

const lootFreezeDepth = 4

func (e *LootEntity) freeze(path map[string]bool, depth int) {
	if e.Facts != nil {
		facts := e.Facts()
		e.Facts = func() LootFacts { return facts }
	}
	if e.ScoreHolder != "" {
		if path[e.ScoreHolder] {
			e.VehicleOf, e.Passengers, e.TargetOf = nil, nil, nil
			return
		}
		path[e.ScoreHolder] = true
		defer delete(path, e.ScoreHolder)
	} else if depth >= lootFreezeDepth {
		e.VehicleOf, e.Passengers, e.TargetOf = nil, nil, nil
		return
	}
	if e.VehicleOf != nil {
		vehicle, ok := e.VehicleOf()
		vehicle.freeze(path, depth+1)
		e.VehicleOf = func() (LootEntity, bool) { return vehicle, ok }
	}
	if e.Passengers != nil {
		riders := e.Passengers()
		for i := range riders {
			riders[i].freeze(path, depth+1)
		}
		e.Passengers = func() []LootEntity { return riders }
	}
	if e.TargetOf != nil {
		target, ok := e.TargetOf()
		target.freeze(path, depth+1)
		e.TargetOf = func() (LootEntity, bool) { return target, ok }
	}
}

// LootFacts are the live facts of a body a command-side predicate tests:
// the flags, the active effects and the ticks lived.
type LootFacts struct {
	Flags     LootEntityFlags
	Effects   []LootEffect
	TickCount int64
	// Movement is the known movement in blocks per tick and FallDistance
	// the blocks fallen.
	// vanilla: Entity.getKnownMovement, Entity.fallDistance
	Movement     [3]float64
	FallDistance float64
}

// LootEffect is one active effect as an effects predicate reads it.
type LootEffect struct {
	Name                string
	Amplifier, Duration int32
	Ambient, Visible    bool
}

// LootLevel is the level a roll happens in, read by the conditions that ask
// after the weather, a clock, the scoreboard or an environment attribute.
type LootLevel interface {
	// IsRaining and IsThundering are Level.isRaining and isThundering, the
	// fading levels past their thresholds.
	IsRaining() bool
	IsThundering() bool
	// ClockTicks is a world clock's total ticks; false for an unknown clock.
	ClockTicks(clock string) (int64, bool)
	// Score is the holder's score in the objective; false when either the
	// objective or the score is missing.
	Score(holder, objective string) (int32, bool)
	// EnvironmentFloat is a float-convertible attribute's value at the
	// roll's origin.
	EnvironmentFloat(attribute string) float32
	// StorageNumber is the single numeric tag the NBT path (its text)
	// selects in a command storage; false when it selects anything else or
	// does not parse.
	StorageNumber(storage, path string) (nbt.Tag, bool)
	// Storage is a command storage's compound, nil for an unknown storage;
	// the result may be shared.
	Storage(id string) nbt.Compound
}

// LevelWorld is the part of a dimension LevelAt reads.
type LevelWorld interface {
	IsRaining() bool
	IsThundering() bool
	ClockTime(clock string) (worldAge, total int64, ok bool)
	EnvironmentFloat(attribute string, x, y, z float64) float32
}

// LevelReads are the server-wide stores a level's rolls read: the scoreboard
// and the command storages.
type LevelReads interface {
	// Score is LootLevel.Score.
	Score(holder, objective string) (int32, bool)
	// StorageNumber is LootLevel.StorageNumber.
	StorageNumber(storage, path string) (nbt.Tag, bool)
	// Storage is LootLevel.Storage.
	Storage(id string) nbt.Compound
}

// LevelAt is w as the level of a roll at the origin (x, y, z), with reads
// answering its scores and storages; nil reads find neither.
// vanilla: LootParams.Builder(ServerLevel), LootContextParams.ORIGIN
func LevelAt(w LevelWorld, reads LevelReads, x, y, z float64) LootLevel {
	return lootLevelAt{w: w, reads: reads, x: x, y: y, z: z, origin: true}
}

// LevelWithoutOrigin is w as the level of a roll with no origin: a positional
// attribute reads its default.
// vanilla: LootContextParamSets.EMPTY, EnvironmentAttributeReader.getValue(LootContext, EnvironmentAttribute)
func LevelWithoutOrigin(w LevelWorld, reads LevelReads) LootLevel {
	return lootLevelAt{w: w, reads: reads}
}

type lootLevelAt struct {
	w       LevelWorld
	reads   LevelReads
	x, y, z float64
	origin  bool
}

func (l lootLevelAt) IsRaining() bool    { return l.w.IsRaining() }
func (l lootLevelAt) IsThundering() bool { return l.w.IsThundering() }

func (l lootLevelAt) ClockTicks(clock string) (int64, bool) {
	_, total, ok := l.w.ClockTime(clock)
	return total, ok
}

func (l lootLevelAt) Score(holder, objective string) (int32, bool) {
	if l.reads == nil {
		return 0, false
	}
	return l.reads.Score(holder, objective)
}

func (l lootLevelAt) EnvironmentFloat(attribute string) float32 {
	if a, ok := registry.EnvAttributeByName(attribute); ok && a.Positional && !l.origin {
		return a.Default
	}
	return l.w.EnvironmentFloat(attribute, l.x, l.y, l.z)
}

func (l lootLevelAt) StorageNumber(storage, path string) (nbt.Tag, bool) {
	if l.reads == nil {
		return nil, false
	}
	return l.reads.StorageNumber(storage, path)
}

func (l lootLevelAt) Storage(id string) nbt.Compound {
	if l.reads == nil {
		return nil
	}
	return l.reads.Storage(id)
}

// LootContext is what a table rolls against: the block and state of a break,
// the blast, a death's bodies, and the tool. Fields the table does not read
// stay zero.
type LootContext struct {
	// ExplorationMap searches from the roll's origin and, when it finds the
	// destination, gives the stack a map leading there and reports true.
	// Absence or a failed search leaves the input unchanged.
	ExplorationMap func(registry.LootExplorationMap, *protocol.ItemStack) bool
	// BlockState is the state a break rolls for, whose properties (the
	// ripeness and crack) the block state conditions read. HasBlock false
	// means no block is in play; state 0 is air.
	BlockState uint16
	HasBlock   bool
	// Tool is the tool the break was made with. An explosion's roll
	// carries none: no silk touch survives a blast.
	Tool LootTool
	// Radius is the explosion's unscaled radius (EXPLOSION_RADIUS), zero
	// outside one; survives_explosion and the decay divide one by it.
	Radius float64
	// This, Attacker and DirectAttacker are a death's bodies: the dying,
	// the one behind the blow, and what the blow rode (the swing, or the
	// arrow). Looting and fire aspect read Attacker's main hand.
	This, Attacker, DirectAttacker LootEntity
	// AttackingPlayer is the player a death credits (LAST_DAMAGE_PLAYER),
	// whatever struck the blow, such as the player whose hit a fall or burn
	// finished. An empty Type falls back to Attacker when a player is it.
	// vanilla: LivingEntity.dropFromLootTable, lastHurtByPlayer
	AttackingPlayer LootEntity
	// Target is the entity interacted with; Interacting is the body using
	// the tool in an entity or block interaction.
	// vanilla: LootContextParams.TARGET_ENTITY, INTERACTING_ENTITY
	Target, Interacting LootEntity
	// DamageType is the damage type of the killing blow, read by the
	// damage_source_properties tag test.
	DamageType registry.DamageType
	// KilledByPlayer says a player's kill is what this death was: the
	// hundred-tick memory of the last player's blow, which is what the
	// vanilla tables' killed_by_player asks after.
	KilledByPlayer bool
	// Container is the block entity's slots, copied by copy_components'
	// minecraft:container (a broken shulker box's contents); nil at no
	// container.
	Container []protocol.ItemStack
	// ContainerSlots reads the slots a range's name spans off the container
	// a command's slot source reads; nil outside one.
	// vanilla: LootContextParams.CONTAINER
	ContainerSlots func(name string) []protocol.ItemStack
	// CustomName is the block entity's name, copied by copy_components'
	// minecraft:custom_name; nil when unnamed.
	CustomName *protocol.Text
	// Lock and ContainerLoot are the block entity's lock and unresolved loot.
	Lock          nbt.CompoundTag
	ContainerLoot protocol.ContainerLoot
	// Components are the block entity's raw, unmodelled components, copied
	// by a copy_components include (a player head's minecraft:profile); nil
	// when there are none.
	Components []protocol.RawComponent
	// Extra are the block entity's own leftover components by name, which a
	// copy_components include of any other name copies; nil when none.
	Extra nbt.Compound
	// Dynamic is the stacks the thing being looted supplies for a dynamic
	// drop by that drop's name - the decorated pot's sherds, one per face.
	// Nil for a roll whose table asks after no dynamic drop.
	Dynamic map[string][]protocol.ItemStack
	// BlockAt reads the block at an offset from the roll's place, for the
	// location checks that look past the block itself. Nil fails them.
	BlockAt func(dx, dy, dz int32) uint16
	// Origin is the roll's position, which the distance predicate measures
	// from; HasOrigin is false for a roll without one.
	Origin    [3]float64
	HasOrigin bool
	// BiomeAt reads the biome at an offset from the roll's place, its wire
	// name, for the location checks that ask after biomes. Nil fails them.
	BiomeAt func(dx, dy, dz int32) string
	// BlockEntity reads the block entity's saved compound with its id and
	// position, copy_custom_data's block_entity; nil at none. The result may
	// be shared.
	// vanilla: BlockEntity.saveWithFullMetadata
	BlockEntity func() nbt.Compound
	// ResolveText resolves a component's selectors against the named
	// context entity (set_name's and set_lore's entity). Nil, or an entity
	// the caller does not hold, leaves the text as it was.
	// vanilla: ComponentUtils.updateForEntity
	ResolveText func(entity string, t protocol.Text) protocol.Text
	// Level is the level the roll happens in. Nil fails the conditions that
	// read it.
	Level LootLevel
	// Luck is the roll's luck (a player's luck attribute, a fishing rod's
	// Luck of the Sea added), which bonus rolls and entry quality scale by.
	// vanilla: LootContext.getLuck
	Luck float32
}

// lootStack is a stack in flight; functions may still change the item
// (furnace smelt: beef to cooked beef).
type lootStack struct {
	// ready is a finished stack a fishing sub-table rolled; it skips the
	// item, count and component fields below, and the entry's functions do
	// not touch it.
	ready *protocol.ItemStack
	item  registry.Item
	count int32
	// container is the minecraft:container component a copy_components
	// laid on the stack - the contents of the block entity the roll
	// happened at. It rides to the resolved stack.
	container []protocol.ItemStack
	// customName is the minecraft:custom_name component a copy_components
	// laid on the stack, the name of the block entity the roll happened
	// at. It rides to the resolved stack.
	customName *protocol.Text
	// itemName is the minecraft:item_name a set_name laid on the stack; it
	// rides every stack the count splits into.
	itemName *protocol.Text
	// components are the raw components a copy_components laid on the
	// stack, the block entity's own that this server does not model. They
	// ride to the resolved stack.
	components []protocol.RawComponent
	// modified carries item-function components on every split stack.
	modified       []protocol.RawComponent
	mapDecorations map[string]protocol.MapDecoration
	// ominous is the amplifier a set_ominous_bottle_amplifier chose, and
	// hasOminous says one was set.
	ominous    int32
	hasOminous bool
	// instrument is a set_instrument's goat horn; hasInstrument says one
	// was set. Both ride every stack the count splits into.
	instrument    int32
	hasInstrument bool
	// damage, enchantments and stored are the wear and enchantments the
	// item modifiers among a table's functions put on the stack; they ride
	// every stack the count splits into.
	damage       int32
	enchantments map[int32]int32
	stored       map[int32]int32
	// trim is the minecraft:trim a set_components laid on the stack, as the
	// trim material and pattern registry IDs; hasTrim says one was laid.
	trimMaterial, trimPattern int32
	hasTrim                   bool
	// containerLoot is the minecraft:container_loot a set_loot_table laid
	// on the stack; it rides every stack the count splits into.
	containerLoot protocol.ContainerLoot
	// rest holds the components an item function laid that no field above
	// carries (unbreakable, repair cost, glint override, lock, ...); its
	// fields the others carry are stale. It rides every split stack.
	rest protocol.ItemStack
}

// lift is the stack as a protocol stack for an item function to run over.
func (s lootStack) lift() protocol.ItemStack {
	st := s.rest
	st.ItemID, st.Count, st.Damage, st.Raw, st.MapDecorations = int32(s.item), s.count, s.damage, s.modified, s.mapDecorations
	st.Enchantments, st.StoredEnchantments, st.Container, st.ContainerLoot = s.enchantments, s.stored, s.container, s.containerLoot
	st.CustomName = s.customName
	return st
}

// fold takes back an item function's changes to a lifted stack.
func (s *lootStack) fold(st protocol.ItemStack) {
	s.item = registry.Item(st.ItemID)
	s.damage, s.enchantments, s.stored = st.Damage, st.Enchantments, st.StoredEnchantments
	s.count, s.modified, s.mapDecorations = st.Count, st.Raw, st.MapDecorations
	s.container, s.containerLoot, s.customName = st.Container, st.ContainerLoot, st.CustomName
	s.rest = st
}

// LootKiller is what the server knows of a death's killer: the held
// weapon and whether the blow was its swing rather than an arrow, and the
// dying body's saved compound.
type LootKiller struct {
	// Weapon is the stack in the killer's main hand, which is where the
	// looting and the fire aspect are read from. The zero value is no
	// weapon: no killer, or a killer gone from the world.
	Weapon LootTool
	// AttackerPieces and CreditedPieces are every equipment slot of the
	// killing attacker and of the credited player, for the equipment effects
	// that walk them.
	AttackerPieces, CreditedPieces []EnchPiece
	// Player says a player struck the killing blow, whatever they held:
	// an empty hand leaves Weapon at its zero value.
	Player bool
	// Melee is a swing of the weapon itself (the tables' direct attacker),
	// which fire aspect's smelting checks; an arrow carries the shooter's
	// weapon for looting but not this.
	Melee bool
	// AttackerType is the entity type of a mob that struck or shot the
	// killing blow; zero where a player or no one did (type zero is no mob).
	AttackerType    registry.EntityType
	AttackerVariant string
	// CreditedPlayer is the player the death credits within the memory
	// window, empty Type for none; the context's AttackingPlayer.
	CreditedPlayer LootEntity
	// Luck is CreditedPlayer's luck, the roll's.
	Luck float32
	// Profile is the killing player's profile, which fill_player_head
	// reads; nil where no player did.
	Profile *protocol.Profile
	// ThisFill adds what only the caller knows of the dying body to its
	// snapshot; nil adds nothing.
	ThisFill func(*LootEntity)
	// AttackerFill adds the live facts, equipment, tags and team of the
	// attacker (player or mob) to its snapshot; nil adds nothing.
	AttackerFill func(*LootEntity)
	// PlayerNBT reads the killing player's compound, the roll's attacker for
	// copy_custom_data; nil reads none.
	PlayerNBT func() nbt.Compound
	// ThisNBT reads the dying body's saved compound, the roll's this for
	// copy_custom_data, which the server holds parts of; nil reads none.
	ThisNBT func() nbt.Compound
	// ThisHasRaid is the dying raider's membership or proximity to a raid.
	ThisHasRaid bool
}

// lootRoller is one roll of one table: the context it rolls against, the
// dice it rolls with, and the tables it has already entered, which a
// reference that loops back on itself must not enter twice.
type lootRoller struct {
	ctx     *LootContext
	rng     LootRand
	visited map[string]bool
	// predicates are the loaded predicates being evaluated, which a
	// reference may not loop back to.
	predicates map[string]bool
	// modifiers are the pack item modifiers a reference is running, which
	// it may not loop back to.
	modifiers map[string]bool
	// additionalCost is present only in a villager trade context.
	additionalCost *int32
	// refDepth counts the context provider registry entries being
	// evaluated, which a pack's entries can make refer to themselves.
	refDepth int
}

// TestLootCondition answers a condition against the context with the one
// random source, the way a table's own gate would.
func TestLootCondition(c registry.LootCondition, ctx *LootContext, rng LootRand) bool {
	r := lootRoller{ctx: ctx, rng: rng}
	return r.condition(c)
}

// RollLoot rolls the named table (minecraft:blocks/<block>,
// minecraft:entities/<mob>) against ctx with one random source. It returns
// the stacks split at each item's maximum, without emptied stacks or items
// this version lacks.
func RollLoot(name string, ctx *LootContext, rng LootRand) []protocol.ItemStack {
	table, ok := registry.FindLootTable(name)
	if !ok {
		return nil
	}
	return RollLootTable(name, table, ctx, rng)
}

// RollLootTable rolls a table the caller holds, such as an inline one a
// command carries; name is the table's own, which a reference entry may not
// loop back to.
func RollLootTable(name string, table registry.LootTable, ctx *LootContext, rng LootRand) []protocol.ItemStack {
	r := lootRoller{ctx: ctx, rng: rng, visited: map[string]bool{name: true}}
	var stacks []lootStack
	// The table's own functions run last of all, after the pool's and the
	// entry's.
	// vanilla: LootTable.getRandomItemsRaw
	emit := func(s lootStack) {
		for _, f := range table.Functions {
			s = r.function(f, s)
		}
		if s.count > 0 {
			stacks = append(stacks, s)
		}
	}
	for i := range table.Pools {
		r.pool(&table.Pools[i], emit)
	}
	return splitStacks(stacks)
}

// splitStacks turns the stacks in flight into item stacks, one per stack
// size.
func splitStacks(stacks []lootStack) []protocol.ItemStack {
	var out []protocol.ItemStack
	for _, s := range stacks {
		if s.ready != nil {
			out = append(out, *s.ready)
			continue
		}
		id := int32(s.item)
		max := int32(registry.MaxStackSize(id))
		if max <= 0 {
			max = 64
		}
		for count := s.count; count > 0; {
			n := min(count, max)
			// Every split carries a copy of all the stack's components.
			// vanilla: LootTable.createStackSplitter (copyWithCount)
			stack := s.rest
			stack.ItemID, stack.Count, stack.Container, stack.CustomName = id, n, slices.Clone(s.container), s.customName
			stack.Raw, stack.Damage, stack.MapDecorations = append(slices.Clone(s.components), s.modified...), s.damage, s.mapDecorations
			stack.Enchantments, stack.StoredEnchantments, stack.ContainerLoot = s.enchantments, s.stored, s.containerLoot
			if s.hasTrim {
				stack.SetTrim(s.trimMaterial, s.trimPattern)
			}
			if s.hasOminous {
				stack.SetOminousBottleAmplifier(s.ominous)
			}
			if s.hasInstrument {
				stack.SetInstrument(s.instrument)
			}
			if s.itemName != nil {
				stack.SetItemName(*s.itemName)
			}
			out = append(out, stack)
			count -= n
		}
	}
	return out
}

// pool rolls one pool of a table: the conditions it happens under, the
// rolls it makes, and its own functions laid over every stack the entries
// give before the table's see it.
func (r *lootRoller) pool(p *registry.LootPool, out func(lootStack)) {
	if !r.conditions(p.Conditions) {
		return
	}
	decorated := func(s lootStack) {
		for _, f := range p.Functions {
			s = r.function(f, s)
		}
		out(s)
	}
	// vanilla: LootPool.addRandomItems
	rolls := r.numInt(p.Rolls) + int32(math.Floor(float64(float32(r.numFloat(p.BonusRolls))*r.ctx.Luck)))
	for range rolls {
		r.pickOne(p.Entries, decorated)
	}
}

// lootCandidate is one choice a pool's roll can land on: an entry that
// expanded into the running, how much of the weight it takes, and the
// making of its stacks.
type lootCandidate struct {
	weight int32
	create func(out func(lootStack))
}

// pickOne makes one choice among a pool's entries: every entry is expanded
// into the candidates it contributes, and the one the weighted dice land
// on makes its stacks. A single candidate rolls no dice.
func (r *lootRoller) pickOne(entries []registry.LootEntry, out func(lootStack)) {
	var valid []lootCandidate
	total := int32(0)
	for i := range entries {
		ok, cands := r.expand(&entries[i])
		if !ok {
			continue
		}
		for _, c := range cands {
			if c.weight > 0 {
				valid = append(valid, c)
				total += c.weight
			}
		}
	}
	if len(valid) == 0 || total == 0 {
		return
	}
	if len(valid) == 1 {
		valid[0].create(out)
		return
	}
	index := r.rng.Int31n(total)
	for _, c := range valid {
		index -= c.weight
		if index < 0 {
			c.create(out)
			return
		}
	}
}

// weight is an entry's share of the pool's choice: its quality scaled by
// the roll's luck added, in float32, floored and never below zero.
// vanilla: UniformContainerBase.EntryBase.getWeight
func (r *lootRoller) weight(weight, quality int32) int32 {
	return max(int32(math.Floor(float64(float32(weight)+float32(quality)*r.ctx.Luck))), 0)
}

// expand reports whether an entry can run and its candidates for the pool's
// choice. Composites recurse: alternatives take the first child that can,
// sequence all or none, group every one that can.
func (r *lootRoller) expand(e *registry.LootEntry) (bool, []lootCandidate) {
	switch k := (*e).(type) {
	case registry.LootItemEntry:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
			s := lootStack{item: k.Item, count: 1}
			for _, f := range k.Functions {
				s = r.function(f, s)
			}
			out(s)
		}}}
	case registry.LootEmpty:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		// An empty entry takes the roll and gives nothing back.
		return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {}}}
	case registry.LootTag:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		if !k.Expand {
			return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
				for _, item := range k.Items {
					s := lootStack{item: item, count: 1}
					for _, f := range k.Functions {
						s = r.function(f, s)
					}
					out(s)
				}
			}}}
		}
		// An expanded tag puts every item of it forward as its own
		// candidate, each at the tag's weight and each without the tag's
		// functions - the raw items.
		cands := make([]lootCandidate, 0, len(k.Items))
		for _, item := range k.Items {
			cands = append(cands, lootCandidate{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
				out(lootStack{item: item, count: 1})
			}})
		}
		return true, cands
	case registry.LootReference:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
			if k.Inline != nil {
				r.rollTable(k.Inline, k.Functions, out)
				return
			}
			r.subTable(k.Table, k.Functions, out)
		}}}
	case registry.LootSlots:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		// vanilla: SlotLoot.createItemStack
		return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
			for _, stack := range SlotSourceStacks(k.Source, r.ctx) {
				if stack.Empty() {
					continue
				}
				s := lootStack{ready: &stack, count: stack.Count}
				for _, f := range k.Functions {
					s = r.function(f, s)
				}
				out(s)
			}
		}}}
	case registry.LootDynamic:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		// A dynamic entry's stacks come from the looted thing (a decorated
		// pot's sherds). It still wins its alternatives when empty, so a
		// cracked pot with no sherds does not drop whole instead.
		supplies := r.ctx.Dynamic[k.Name]
		return true, []lootCandidate{{weight: r.weight(k.Weight, k.Quality), create: func(out func(lootStack)) {
			for _, supply := range supplies {
				out(lootStack{item: registry.Item(supply.ItemID), count: supply.Count})
			}
		}}}
	case registry.LootAlternatives:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		for i := range k.Children {
			if ok, cands := r.expand(&k.Children[i]); ok {
				return true, cands
			}
		}
		return false, nil
	case registry.LootSequence:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		var cands []lootCandidate
		for i := range k.Children {
			ok, more := r.expand(&k.Children[i])
			if !ok {
				return false, nil
			}
			cands = append(cands, more...)
		}
		return true, cands
	case registry.LootGroup:
		if !r.conditions(k.Conditions) {
			return false, nil
		}
		var cands []lootCandidate
		for i := range k.Children {
			if ok, more := r.expand(&k.Children[i]); ok {
				cands = append(cands, more...)
			}
		}
		return true, cands
	}
	return false, nil
}

// subTable rolls another table into this one (LootReference: the sheep's
// colour table). The entry's functions run on each stack after the
// sub-table's own; the outer pool's and table's run later still, as they
// decorate the consumer it rolls into.
func (r *lootRoller) subTable(name string, funcs []registry.LootFunction, out func(lootStack)) {
	if r.visited[name] {
		// A table that loops back on itself stops here.
		return
	}
	// Fishing tables are rolled by rules.FishingTableLoot, without a biome,
	// so a jungle's bamboo junk stays out. A pack's table of that name takes
	// the generic path, where the visited set stops reference loops.
	// A seeded roll (a JavaRandom) has no hand-written fishing path.
	goRng, isGo := r.rng.(*rand.Rand)
	if _, packed := registry.PackLootTable(name); !packed && isGo {
		if stacks, ok := FishingTableLoot(name, goRng, "", r.ctx.Level); ok {
			for i := range stacks {
				out(lootStack{ready: &stacks[i], count: stacks[i].Count})
			}
			return
		}
	}
	sub, ok := registry.FindLootTable(name)
	if !ok {
		return
	}
	r.visited[name] = true
	defer delete(r.visited, name)
	r.rollTable(&sub, funcs, out)
}

// rollTable rolls a table's pools into out, running the table's functions
// and then the entry's on each stack.
func (r *lootRoller) rollTable(sub *registry.LootTable, funcs []registry.LootFunction, out func(lootStack)) {
	inner := func(s lootStack) {
		for _, f := range sub.Functions {
			s = r.function(f, s)
		}
		for _, f := range funcs {
			s = r.function(f, s)
		}
		out(s)
	}
	for i := range sub.Pools {
		r.pool(&sub.Pools[i], inner)
	}
}

// conditions says whether every condition of a list holds. An empty list
// holds, the way an ungated drop is an ungated drop.
func (r *lootRoller) conditions(cs []registry.LootCondition) bool {
	for _, c := range cs {
		if !r.condition(c) {
			return false
		}
	}
	return true
}

// condition answers one condition of the tables: a question about the
// tool, the blast, the block, the bodies, or the dice.
func (r *lootRoller) condition(c registry.LootCondition) bool {
	switch k := c.(type) {
	case registry.LootMatchTool:
		return r.ctx.Tool.Item != "" && itemPredicate(k.Predicate, r.ctx.Tool)
	case registry.LootSurvivesExplosion:
		if r.ctx.Radius <= 0 {
			return true
		}
		return float64(r.rng.Float32()) <= float64(float32(1/r.ctx.Radius))
	case registry.LootRandomChance:
		chance := r.numFloat(k.Chance)
		return float64(r.rng.Float32()) < float64(float32(chance))
	case registry.LootEnchantedChance:
		// The chance rides the enchantment's level on the attacker's
		// weapon; nobody behind the blow rolls the unenchanted chance.
		level := r.entityLevel(r.ctx.Attacker, k.Enchantment)
		chance := k.Unenchanted
		if level > 0 {
			chance = k.Base + k.PerLevel*float64(level-1)
			if k.Chance != nil {
				chance = enchLevelValue(k.Chance, level)
			}
		}
		return float64(r.rng.Float32()) < float64(float32(chance))
	case registry.LootTableBonus:
		// The chance is the enchantment level's place in the table, the
		// top level holding the last entry down.
		level := r.ctx.Tool.Enchantments[k.Enchantment]
		chance := k.Chances[min(int(level), len(k.Chances)-1)]
		return float64(r.rng.Float32()) < float64(float32(chance))
	case registry.LootKilledByPlayer:
		return r.ctx.KilledByPlayer
	case registry.LootBlockState:
		return r.blockMatches(k.Block, k.Properties)
	case registry.LootMatchBlock:
		if !r.ctx.HasBlock {
			return false
		}
		return (k.Blocks == nil || slices.Contains(k.Blocks, registry.BlockOf(r.ctx.BlockState).Name())) &&
			propertiesMatch(r.ctx.BlockState, k.Properties)
	case registry.LootLocationCheck:
		// The parts the table asked after must every one hold; a part
		// left empty asks after nothing.
		if k.Block != "" && !r.locationBlockMatches(k) {
			return false
		}
		if len(k.Biomes) > 0 && !r.locationBiomeMatches(k) {
			return false
		}
		return true
	case registry.LootEntityProperties:
		e := r.entity(k.Entity)
		return e.Type != "" && entityPredicate(k.Predicate, e, r.ctx)
	case registry.LootDamageSource:
		if k.DamageTypes != nil {
			return k.DamageTypes.Contains(r.ctx.DamageType) && (k.DirectType == "" || r.ctx.DirectAttacker.Type == k.DirectType)
		}
		return r.ctx.Attacker.Type == k.SourceType && (k.FrogVariant == "" || r.ctx.Attacker.FrogVariant == k.FrogVariant)
	case registry.LootReferenceCondition:
		// A predicate that refers back to itself fails rather than recursing.
		// vanilla: ConditionReference.test
		cond, ok := registry.FindPredicate(k.Name)
		if !ok || r.predicates[k.Name] {
			return false
		}
		if r.predicates == nil {
			r.predicates = map[string]bool{}
		}
		r.predicates[k.Name] = true
		defer delete(r.predicates, k.Name)
		return r.condition(cond)
	case registry.LootTimeCheck:
		if r.ctx.Level == nil {
			return false
		}
		ticks, ok := r.ctx.Level.ClockTicks(k.Clock)
		if !ok {
			return false
		}
		if k.Period != 0 {
			ticks %= k.Period
		}
		return r.intInRange(k.Value, int32(ticks))
	case registry.LootWeatherCheck:
		if r.ctx.Level == nil {
			return false
		}
		return (k.Raining == nil || *k.Raining == r.ctx.Level.IsRaining()) &&
			(k.Thundering == nil || *k.Thundering == r.ctx.Level.IsThundering())
	case registry.LootEntityScores:
		e := r.entity(k.Entity)
		if e.ScoreHolder == "" || r.ctx.Level == nil {
			return false
		}
		for _, s := range k.Scores {
			score, ok := r.ctx.Level.Score(e.ScoreHolder, s.Objective)
			if !ok || !r.intInRange(s.Range, score) {
				return false
			}
		}
		return true
	case registry.LootEnchantmentActive:
		// No roll this server makes is an enchantment effect's, so
		// ENCHANTMENT_ACTIVE is always absent, which fails either way.
		return false
	case registry.LootEnvironmentCheck:
		return r.ctx.Level != nil && r.ctx.Level.EnvironmentFloat(k.Attribute) == k.Value
	case registry.LootValueCheck:
		if k.Float {
			return r.floatInRange(k.Range, float32(r.numFloat(k.Value)))
		}
		return r.intInRange(k.Range, r.numInt(k.Value))
	case registry.LootInverted:
		return !r.condition(k.Term)
	case registry.LootAnyOf:
		return slices.ContainsFunc(k.Terms, r.condition)
	case registry.LootAllOf:
		for _, t := range k.Terms {
			if !r.condition(t) {
				return false
			}
		}
		return true
	}
	return false
}

// intInRange tests an IntRangePredicate, rolling its bounds as integers.
// vanilla: IntRangePredicate.test
func (r *lootRoller) intInRange(rg registry.LootRange, v int32) bool {
	if rg.Exact != nil {
		return r.numInt(rg.Exact) == v
	}
	return (rg.Min == nil || v >= r.numInt(rg.Min)) && (rg.Max == nil || v <= r.numInt(rg.Max))
}

// floatInRange tests a FloatRangePredicate, rolling its bounds as floats.
// vanilla: FloatRangePredicate.test
func (r *lootRoller) floatInRange(rg registry.LootRange, v float32) bool {
	if rg.Exact != nil {
		return float32(r.numFloat(rg.Exact)) == v
	}
	return (rg.Min == nil || v >= float32(r.numFloat(rg.Min))) && (rg.Max == nil || v <= float32(r.numFloat(rg.Max)))
}

// locationBlockMatches answers the block part of a location check; a zero
// offset reads the block in play itself, an offset the world at that place. A roll with no block in play reads the
// world at the origin too.
func (r *lootRoller) locationBlockMatches(k registry.LootLocationCheck) bool {
	if k.OffsetX == 0 && k.OffsetY == 0 && k.OffsetZ == 0 && (r.ctx.HasBlock || r.ctx.BlockAt == nil) {
		return r.blockMatches(k.Block, k.Properties)
	}
	return blockAtMatches(r.ctx, k, k.OffsetX, k.OffsetY, k.OffsetZ)
}

// blockAtMatches tests the block part of k against the world at an offset
// from the roll's origin; a roll without a world fails it.
func blockAtMatches(ctx *LootContext, k registry.LootLocationCheck, dx, dy, dz int32) bool {
	return ctx.BlockAt != nil && stateMatches(ctx.BlockAt(dx, dy, dz), k.Block, k.Properties)
}

// biomeAtMatches tests the biome part of k against the world at an offset
// from the roll's origin; a roll without a world fails it.
func biomeAtMatches(ctx *LootContext, k registry.LootLocationCheck, dx, dy, dz int32) bool {
	return ctx.BiomeAt != nil && slices.Contains(k.Biomes, ctx.BiomeAt(dx, dy, dz))
}

// locationBiomeMatches answers the biome part of a location check, which a
// roll without a world behind it fails, such as a death this server did not
// watch.
func (r *lootRoller) locationBiomeMatches(k registry.LootLocationCheck) bool {
	return biomeAtMatches(r.ctx, k, k.OffsetX, k.OffsetY, k.OffsetZ)
}

// entity is the body an entity condition names, empty when the roll
// carries none.
func (r *lootRoller) entity(target string) LootEntity {
	e, _ := r.ctx.entity(target)
	return e
}

// blockMatches says whether the block in play is the named block in a
// state that answers every exact property match.
func (r *lootRoller) blockMatches(block string, props []registry.LootStateProperty) bool {
	return r.ctx.HasBlock && stateMatches(r.ctx.BlockState, block, props)
}

// stateMatches says whether a state is the named block with every property
// equal; a property the game has no name for fails.
func stateMatches(state uint16, block string, props []registry.LootStateProperty) bool {
	if tag, ok := strings.CutPrefix(block, "#"); ok {
		return registry.BlockOf(state).InTag(registry.BlockTagOf(tag)) && propertiesMatch(state, props)
	}
	return registry.BlockOf(state).Name() == block && propertiesMatch(state, props)
}

// propertiesMatch says whether a state has every property equal.
func propertiesMatch(state uint16, props []registry.LootStateProperty) bool {
	for _, p := range props {
		prop, ok := registry.PropNamed(p.Key)
		if !ok || registry.StateValue(state, prop) != p.Value {
			return false
		}
	}
	return true
}

// itemPredicate says whether a stack answers an item predicate: the item
// it names, and every enchantment predicate the predicate carries.
func itemPredicate(p registry.LootItemPredicate, stack LootTool) bool {
	if p.Full != nil && !p.Full.Matches(stack.Stack) {
		return false
	}
	if p.HasMapID && !stack.HasMapID {
		return false
	}
	if p.HasDyedColor && !stack.HasDyedColor {
		return false
	}
	if len(p.Items) > 0 {
		found := slices.Contains(p.Items, stack.Item)
		if !found {
			return false
		}
	}
	for _, e := range p.Enchantments {
		if !enchantmentsContain(e, stack.Enchantments) {
			return false
		}
	}
	for _, e := range p.StoredEnchantments {
		if !enchantmentsContain(e, stack.StoredEnchantments) {
			return false
		}
	}
	return true
}

// enchantmentsContain reports whether the enchantments match a predicate:
// a named one at a level in range, or any in range when none are named. A
// zero bound is no bound.
func enchantmentsContain(e registry.LootEnchantments, ench map[string]int32) bool {
	if len(e.Enchantments) > 0 {
		for _, name := range e.Enchantments {
			if level := ench[name]; level > 0 && levelInRange(e, level) {
				return true
			}
		}
		return false
	}
	for _, level := range ench {
		if level > 0 && levelInRange(e, level) {
			return true
		}
	}
	return false
}

// levelInRange says whether a level sits in a predicate's range, the zero
// bounds meaning no bound on that side.
func levelInRange(e registry.LootEnchantments, level int32) bool {
	if level < e.Min {
		return false
	}
	return e.Max == 0 || level <= e.Max
}

// entityPredicate says whether a body answers an entity predicate: the
// facts it carries, each asked only where the predicate asks it.
func entityPredicate(p registry.LootEntityPredicate, e LootEntity, ctx *LootContext) bool {
	if p.Type != "" && p.Type != e.Type {
		return false
	}
	if len(p.Types) > 0 {
		found := slices.Contains(p.Types, e.Type)
		if !found {
			return false
		}
	}
	if p.OnFire != nil && *p.OnFire != e.OnFire {
		return false
	}
	if p.Baby != nil && *p.Baby != e.Baby {
		return false
	}
	if p.Vehicle != "" && p.Vehicle != e.Vehicle {
		return false
	}
	var facts LootFacts
	if e.Facts != nil && (len(p.Effects) > 0 || p.Period > 0 || p.HasFlagTests() || p.Movement != nil) {
		facts = e.Facts()
	}
	if !entityPlaceMatch(p, e, facts, ctx) {
		return false
	}
	if !entityFlagsMatch(p, facts.Flags) || !entityEquipmentMatch(p, e) || !entityRelatedMatch(p, e, ctx) {
		return false
	}
	if len(p.Effects) > 0 && !effectsMatch(p.Effects, facts.Effects) {
		return false
	}
	if p.NBT != nil {
		var data nbt.Compound
		if e.NBT != nil {
			data = e.NBT()
		}
		if data == nil || !nbt.Matches(p.NBT, data) {
			return false
		}
	}
	if p.HasTeam && (e.Team == "" || e.Team != p.Team) {
		return false
	}
	if p.HasTags && !tagsMatch(p, e.Tags) {
		return false
	}
	if p.Period > 0 && facts.TickCount%int64(p.Period) != 0 {
		return false
	}
	if p.SheepColour != "" && (!e.Sheep || e.SheepColour != p.SheepColour) {
		return false
	}
	if p.Sheared != nil && (!e.Sheep || *p.Sheared != e.Sheared) {
		return false
	}
	// vanilla: RaiderPredicate.matches; omitted flags both default to false.
	if p.Captain != nil || p.HasRaid != nil {
		captain := p.Captain != nil && *p.Captain
		hasRaid := p.HasRaid != nil && *p.HasRaid
		if !e.Raider || captain != e.Captain || hasRaid != e.HasRaid {
			return false
		}
	}
	if p.Mooshroom != "" && e.Mooshroom != p.Mooshroom {
		return false
	}
	if p.ChickenVariant != "" && e.ChickenVariant != p.ChickenVariant {
		return false
	}
	if p.Mainhand != nil && !itemPredicate(*p.Mainhand, e.Mainhand) {
		return false
	}
	if !entityTypeSpecificMatch(p, e, ctx) || !entityComponentsMatch(p, e) {
		return false
	}
	if p.CubeSizeMin != nil && (e.CubeSize == 0 || float64(e.CubeSize) < *p.CubeSizeMin) {
		return false
	}
	if p.CubeSizeMax != nil && (e.CubeSize == 0 || float64(e.CubeSize) > *p.CubeSizeMax) {
		return false
	}
	return true
}

// entityFlagsMatch tests the flags other than burning and babyhood.
// vanilla: EntityFlagsPredicate.matches
func entityFlagsMatch(p registry.LootEntityPredicate, f LootEntityFlags) bool {
	for _, c := range [...]struct {
		want *bool
		got  bool
	}{
		{p.OnGround, f.OnGround}, {p.Sneaking, f.Sneaking}, {p.Sprinting, f.Sprinting},
		{p.Swimming, f.Swimming}, {p.Flying, f.Flying}, {p.InWater, f.InWater},
		{p.FallFlying, f.FallFlying},
	} {
		if c.want != nil && *c.want != c.got {
			return false
		}
	}
	return true
}

// entityEquipmentMatch tests the equipment stacks besides the main hand.
// vanilla: EntityEquipmentPredicate.matches
func entityEquipmentMatch(p registry.LootEntityPredicate, e LootEntity) bool {
	for _, c := range [...]struct {
		want *registry.LootItemPredicate
		got  LootTool
	}{{p.Head, e.Head}, {p.Chest, e.Chest}, {p.Legs, e.Legs}, {p.Feet, e.Feet}, {p.Body, e.Body}, {p.Offhand, e.Offhand}} {
		if c.want != nil && !itemPredicate(*c.want, c.got) {
			return false
		}
	}
	return true
}

// effectsMatch says whether every effect the predicate lists is active and
// inside its ranges.
// vanilla: MobEffectsPredicate.matches
func effectsMatch(want map[string]registry.LootEffectPredicate, have []LootEffect) bool {
	inRange := func(v int32, lo, hi *float64) bool {
		return (lo == nil || float64(v) >= *lo) && (hi == nil || float64(v) <= *hi)
	}
	for name, w := range want {
		i := slices.IndexFunc(have, func(f LootEffect) bool { return f.Name == name })
		if i < 0 {
			return false
		}
		f := have[i]
		if !inRange(f.Amplifier, w.AmplifierMin, w.AmplifierMax) || !inRange(f.Duration, w.DurationMin, w.DurationMax) ||
			w.Ambient != nil && *w.Ambient != f.Ambient || w.Visible != nil && *w.Visible != f.Visible {
			return false
		}
	}
	return true
}

// tagsMatch tests a body's scoreboard tags.
// vanilla: EntityTagPredicate.matches
func tagsMatch(p registry.LootEntityPredicate, tags []string) bool {
	if p.TagsAny != nil && !slices.ContainsFunc(p.TagsAny, func(t string) bool { return slices.Contains(tags, t) }) {
		return false
	}
	if p.TagsNone != nil && slices.ContainsFunc(p.TagsNone, func(t string) bool { return slices.Contains(tags, t) }) {
		return false
	}
	for _, t := range p.TagsAll {
		if !slices.Contains(tags, t) {
			return false
		}
	}
	return true
}

// entityRelatedMatch tests the nested vehicle, passenger and target
// predicates against the related bodies' snapshots.
// vanilla: VehiclePredicate, PassengerPredicate, TargetedEntityPredicate
func entityRelatedMatch(p registry.LootEntityPredicate, e LootEntity, ctx *LootContext) bool {
	if p.VehiclePredicate != nil {
		var vehicle LootEntity
		var ok bool
		switch {
		case e.VehicleOf != nil:
			vehicle, ok = e.VehicleOf()
		case e.Vehicle != "":
			vehicle, ok = LootEntity{Type: e.Vehicle}, true
		}
		if !ok || !entityPredicate(*p.VehiclePredicate, vehicle, ctx) {
			return false
		}
	}
	if p.Passenger != nil {
		if e.Passengers == nil || !slices.ContainsFunc(e.Passengers(), func(r LootEntity) bool { return entityPredicate(*p.Passenger, r, ctx) }) {
			return false
		}
	}
	if p.Targeted != nil {
		if e.TargetOf == nil {
			return false
		}
		target, ok := e.TargetOf()
		if !ok || !entityPredicate(*p.Targeted, target, ctx) {
			return false
		}
	}
	return true
}

// entityLevel is the level of one enchantment on a body's main hand, the
// reading vanilla's EnchantmentHelper.getEnchantmentLevel gives the killer
// of a death: the weapon's own enchantments.
func (r *lootRoller) entityLevel(e LootEntity, enchantment string) int32 {
	if e.Type == "" {
		return 0
	}
	return e.Mainhand.Enchantments[enchantment]
}

// roundInt rounds half up.
// vanilla: Math.round, NumberProvider.getInt
func roundInt(v float64) int32 {
	return int32(math.Floor(v + 0.5))
}

// limitCount clamps a count to the bounds a limit_count function carries.
// vanilla: LimitCount.run, IntLimit.clamp
func (r *lootRoller) limitCount(count int32, lo, hi registry.LootNumber) int32 {
	if lo != nil {
		count = max(count, r.numInt(lo))
	}
	if hi != nil {
		count = min(count, r.numInt(hi))
	}
	return count
}

// function runs one function of a table over a stack, its own conditions
// first: a gate that refuses leaves the stack as it was.
func (r *lootRoller) function(f registry.LootFunction, s lootStack) lootStack {
	if !r.conditions(functionConditions(f)) {
		return s
	}
	switch k := f.(type) {
	case registry.LootSetCount:
		base := int32(0)
		if k.Add {
			base = s.count
		}
		s.count = base + r.numInt(k.Count)
	case registry.LootApplyBonus:
		// A block roll always carries a tool, the empty stack at least, so its
		// bonus runs at level 0 (vanilla: Block.getDrops, ApplyBonusCount.run).
		if !r.ctx.HasBlock && r.ctx.Tool.Item == "" {
			return s
		}
		level := r.ctx.Tool.Enchantments[k.Enchantment]
		switch formula := k.Formula.(type) {
		case registry.LootOreDrops:
			if level > 0 {
				bonus := max(r.rng.Int31n(level+2)-1, 0)
				s.count *= bonus + 1
			}
		case registry.LootUniformBonus:
			s.count += r.rng.Int31n(formula.Multiplier*level + 1)
		case registry.LootBinomialBonus:
			for i := int32(0); i < level+formula.Extra; i++ {
				if float64(r.rng.Float32()) < float64(float32(formula.Probability)) {
					s.count++
				}
			}
		}
	case registry.LootExplosionDecay:
		if r.ctx.Radius > 0 {
			probability := 1 / r.ctx.Radius
			kept := int32(0)
			for i := int32(0); i < s.count; i++ {
				if float64(r.rng.Float32()) <= float64(float32(probability)) {
					kept++
				}
			}
			s.count = kept
		}
	case registry.LootLimitCount:
		s.count = r.limitCount(s.count, k.Min, k.Max)
	case registry.LootEnchantedCount:
		level := r.entityLevel(r.ctx.Attacker, k.Enchantment)
		if level == 0 {
			return s
		}
		addition := math.Round(float64(level) * r.numFloat(k.Count))
		s.count += int32(addition)
		if k.Limit > 0 && s.count > k.Limit {
			s.count = k.Limit
		}
	case registry.LootFurnaceSmelt:
		if smelted := furnaceSmelt(k, int32(s.item), s.count); !smelted.Empty() {
			s.item = registry.Item(smelted.ItemID)
			s.count = smelted.Count
		}
	case registry.LootCopyComponents:
		// vanilla: CopyComponentsFunction.run
		for _, name := range k.Include {
			switch name {
			case "minecraft:container":
				if len(r.ctx.Container) > 0 {
					s.container = r.ctx.Container
				}
			case "minecraft:custom_name":
				if r.ctx.CustomName != nil {
					s.customName = r.ctx.CustomName
				}
			case "minecraft:lock":
				if r.ctx.Lock != nil {
					s.rest.Lock = r.ctx.Lock
				}
			case "minecraft:container_loot":
				if r.ctx.ContainerLoot.Table != "" {
					s.containerLoot = r.ctx.ContainerLoot
				}
			case "minecraft:profile", "minecraft:note_block_sound",
				"minecraft:bees", "minecraft:banner_patterns", "minecraft:pot_decorations":
				if raw, ok := r.ctx.rawComponent(name); ok {
					s.components = append(s.components, raw)
				}
			default:
				if value, ok := r.ctx.Extra[name]; ok {
					var scratch protocol.ItemStack
					if registry.ReadComponent(&scratch, name, value) {
						s.components = append(s.components, scratch.Raw...)
					}
				}
			}
		}
	case registry.LootCopyState:
		// A property the state has is written even at its default
		// (honey_level=0). vanilla: CopyBlockState.run
		if !r.ctx.HasBlock {
			return s
		}
		props := map[string]string{}
		for _, name := range k.Properties {
			if p, ok := registry.PropNamed(name); ok && registry.StateHas(r.ctx.BlockState, p) {
				props[name] = registry.StateValue(r.ctx.BlockState, p)
			}
		}
		// No listed property on the state leaves the item's default, no patch.
		// vanilla: CopyBlockState.run, BlockItemStateProperties.EMPTY
		if len(props) > 0 {
			var scratch protocol.ItemStack
			scratch.SetBlockStateProperties(props)
			s.components = append(s.components, scratch.Raw...)
		}
	case registry.LootFiltered:
		if s.count <= 0 {
			return s
		}
		id := int32(s.item)
		branch := k.OnFail
		if itemPredicate(k.Predicate, ToolOf(protocol.ItemStack{ItemID: id, Count: s.count,
			Enchantments: s.enchantments, StoredEnchantments: s.stored, Raw: s.modified})) {
			branch = k.OnPass
		}
		if branch != nil {
			return r.function(branch, s)
		}
	case registry.LootDiscard:
		s.count = 0
	case registry.LootFunctionSequence:
		for _, f := range k.Functions {
			s = r.function(f, s)
		}
	case registry.LootFunctionReference:
		r.referencedModifier(k.Name, func(fn registry.LootFunction) { s = r.function(fn, s) })
	case registry.LootSetDamage, registry.LootSetEnchantments,
		registry.LootEnchantRandomly, registry.LootEnchantWithLevels,
		registry.LootSetPotion, registry.LootSetRandomPotion, registry.LootSetRandomDyes, registry.LootSetStewEffect, registry.LootExplorationMap,
		registry.LootSetLore, registry.LootSetCustomData, registry.LootToggleTooltips, registry.LootSetAttributes,
		registry.LootSetContents, registry.LootModifyContents, registry.LootSetBannerPattern, registry.LootSetBookCover, registry.LootSetCustomModelData,
		registry.LootSetFireworkExplosion, registry.LootSetFireworks, registry.LootSetContainerLoot,
		registry.LootSetWritableBookPages, registry.LootSetWrittenBookPages, registry.LootFillPlayerHead,
		registry.LootSetItem, registry.LootCopyCustomData:
		// One implementation with /item modify: the stack is lifted to a
		// protocol stack, run through it and its changes folded back.
		if s.count <= 0 {
			return s
		}
		s.fold(r.apply(f, s.lift(), nil))
	case registry.LootSetComponents:
		// The trim rides its own fields; the other components go through
		// the /item modify patch.
		if text, ok := k.Set["minecraft:trim"]; ok {
			s.trimMaterial, s.trimPattern, s.hasTrim = parseTrim(text)
		}
		if slices.Contains(k.Remove, "minecraft:trim") {
			s.hasTrim = false
		}
		rest := registry.LootSetComponents{Set: maps.Clone(k.Set), Remove: slices.DeleteFunc(slices.Clone(k.Remove), func(name string) bool {
			return name == "minecraft:trim"
		})}
		delete(rest.Set, "minecraft:trim")
		if s.count > 0 && (len(rest.Set) > 0 || len(rest.Remove) > 0) {
			s.fold(r.apply(rest, s.lift(), nil))
		}
	case registry.LootSetOminousBottleAmplifier:
		// vanilla: SetOminousBottleAmplifierFunction.run
		s.ominous, s.hasOminous = min(max(r.numInt(k.Amplifier), 0), 4), true
	case registry.LootSetName:
		name := k.Name
		if k.ItemName {
			s.itemName = &name
		} else {
			s.customName = &name
		}
	case registry.LootCopyName:
		// vanilla: CopyNameFunction.run
		if name, ok := r.ctx.copiedName(k.Source); ok {
			s.customName = name
		}
	case registry.LootSetInstrument:
		// One of the options becomes the goat horn's instrument; other
		// items and names this version does not know are left alone.
		// vanilla: SetInstrumentFunction.run
		if s.item != registry.ItemGoatHorn || len(k.Options) == 0 {
			return s
		}
		name := k.Options[r.rng.Int31n(int32(len(k.Options)))]
		if id, ok := registry.InstrumentID(name); ok {
			s.instrument, s.hasInstrument = id, true
		}
	}
	return s
}

// rawComponent returns the context's raw component of the given registry
// name, the block entity's own that a copy_components includes and this
// server does not model. A name with no modelled raw component answers false.
func (c *LootContext) rawComponent(name string) (protocol.RawComponent, bool) {
	var id int32
	switch name {
	case "minecraft:profile":
		id = protocol.DataComponentProfile
	case "minecraft:note_block_sound":
		id = protocol.DataComponentNoteBlockSound
	case "minecraft:bees":
		id = protocol.DataComponentBees
	case "minecraft:banner_patterns":
		id = protocol.DataComponentBannerPatterns
	case "minecraft:pot_decorations":
		id = protocol.DataComponentPotDecorations
	default:
		return protocol.RawComponent{}, false
	}
	for _, raw := range c.Components {
		if raw.Type == id {
			return raw, true
		}
	}
	return protocol.RawComponent{}, false
}

// functionConditions is the list of conditions a function carries, the
// gate vanilla's LootItemConditionalFunction runs before it changes
// anything.
func functionConditions(f registry.LootFunction) []registry.LootCondition {
	switch k := f.(type) {
	case registry.LootSetCount:
		return k.Conditions
	case registry.LootApplyBonus:
		return k.Conditions
	case registry.LootExplosionDecay:
		return k.Conditions
	case registry.LootLimitCount:
		return k.Conditions
	case registry.LootEnchantedCount:
		return k.Conditions
	case registry.LootFurnaceSmelt:
		return k.Conditions
	case registry.LootCopyComponents:
		return k.Conditions
	case registry.LootCopyState:
		return k.Conditions
	case registry.LootSetRandomPotion:
		return k.Conditions
	case registry.LootSetRandomDyes:
		return k.Conditions
	case registry.LootSetPotion:
		return k.Conditions
	case registry.LootSetOminousBottleAmplifier:
		return k.Conditions
	case registry.LootSetStewEffect:
		return k.Conditions
	case registry.LootSetInstrument:
		return k.Conditions
	case registry.LootSetDamage:
		return k.Conditions
	case registry.LootSetName:
		return k.Conditions
	case registry.LootSetLore:
		return k.Conditions
	case registry.LootSetCustomData:
		return k.Conditions
	case registry.LootToggleTooltips:
		return k.Conditions
	case registry.LootCopyName:
		return k.Conditions
	case registry.LootSetAttributes:
		return k.Conditions
	case registry.LootExplorationMap:
		return k.Conditions
	case registry.LootSetEnchantments:
		return k.Conditions
	case registry.LootEnchantRandomly:
		return k.Conditions
	case registry.LootEnchantWithLevels:
		return k.Conditions
	case registry.LootSetItem:
		return k.Conditions
	case registry.LootSetComponents:
		return k.Conditions
	case registry.LootCopyCustomData:
		return k.Conditions
	case registry.LootSetContents:
		return k.Conditions
	case registry.LootFiltered:
		return k.Conditions
	case registry.LootDiscard:
		return k.Conditions
	case registry.LootModifyContents:
		return k.Conditions
	case registry.LootSetBannerPattern:
		return k.Conditions
	case registry.LootSetBookCover:
		return k.Conditions
	case registry.LootSetCustomModelData:
		return k.Conditions
	case registry.LootSetFireworkExplosion:
		return k.Conditions
	case registry.LootSetFireworks:
		return k.Conditions
	case registry.LootSetContainerLoot:
		return k.Conditions
	case registry.LootSetWritableBookPages:
		return k.Conditions
	case registry.LootSetWrittenBookPages:
		return k.Conditions
	case registry.LootFillPlayerHead:
		return k.Conditions
	case registry.LootFunctionReference:
		return k.Conditions
	}
	return nil
}

// referencedModifier runs the pack item modifier name; an unknown one, or
// one that refers back to itself, runs nothing.
// vanilla: FunctionReference.run
func (r *lootRoller) referencedModifier(name string, run func(registry.LootFunction)) {
	fn, ok := registry.FindItemModifier(name)
	if !ok || r.modifiers[name] {
		return
	}
	if r.modifiers == nil {
		r.modifiers = map[string]bool{}
	}
	r.modifiers[name] = true
	defer delete(r.modifiers, name)
	run(fn)
}

var blockLootTables = func() (t [registry.BlockCount]string) {
	for b := range registry.Block(registry.BlockCount) {
		name := b.Name()
		if standing, ok := registry.StandingVariant(name); ok {
			name = standing
		}
		t[b] = "minecraft:blocks/" + strings.TrimPrefix(name, "minecraft:")
	}
	return
}()

// BlockLootTable is a block's minecraft:blocks/<block> table; a wall
// variant rolls its standing block's (WallTorchBlock and the others take
// its loot).
func BlockLootTable(b registry.Block) string {
	if b >= registry.BlockCount {
		return ""
	}
	return blockLootTables[b]
}

// EntityLootTable is the name the entity loot table of a mob rolls under:
// minecraft:entities/<mob>.
func EntityLootTable(entityType string) string {
	return "minecraft:entities/" + strings.TrimPrefix(entityType, "minecraft:")
}

// parseTrim reads minecraft:trim's SNBT value: the material and pattern
// names as their registry IDs, false for a value that names a trim material
// or pattern this version does not have.
// vanilla: ArmorTrim.CODEC
func parseTrim(text string) (material, pattern int32, ok bool) {
	tag, _, err := nbt.ParseValueTag(text)
	if err != nil {
		return 0, 0, false
	}
	value, isMap := tag.(nbt.Compound)
	if !isMap {
		return 0, 0, false
	}
	m, _ := nbt.Get[nbt.String](value, "material")
	p, _ := nbt.Get[nbt.String](value, "pattern")
	material, okM := registryEntryID("minecraft:trim_material", registry.Qualify(string(m)))
	pattern, okP := registryEntryID("minecraft:trim_pattern", registry.Qualify(string(p)))
	return material, pattern, okM && okP
}

// registryEntryID is the ID of a name in a synchronized registry, the order
// the registry lists its entries in.
func registryEntryID(reg, name string) (int32, bool) {
	r, _ := registry.Get(reg)
	if i := r.ID(name); i >= 0 {
		return i, true
	}
	return 0, false
}

// copiedName is the custom name copy_name copies from the source, and false
// when the context holds no such source. A source without a name answers a
// nil name, which clears the stack's.
// vanilla: CopyNameFunction.run
func (c *LootContext) copiedName(source string) (*protocol.Text, bool) {
	if source == "block_entity" {
		return c.CustomName, c.HasBlock
	}
	e, ok := c.entity(source)
	return e.Name, ok
}

// nbtSource is the compound an NBT provider reads, nil when the roll holds
// no such source.
// vanilla: ContextNbtProvider.get, StorageNbtProvider.get
func (c *LootContext) nbtSource(source registry.LootNBTSource) nbt.Compound {
	read := c.BlockEntity
	switch {
	case source.Storage != "":
		if c.Level == nil {
			return nil
		}
		return c.Level.Storage(source.Storage)
	case source.Target != "block_entity":
		e, _ := c.entity(source.Target)
		read = e.NBT
	}
	if read == nil {
		return nil
	}
	return read()
}

// entity is the body a LootContext.EntityTarget names, and false when the
// context holds none. attacking_player (LAST_DAMAGE_PLAYER) is the credited
// player, else the attacker when a player is it.
// vanilla: LootContext.EntityTarget
func (c *LootContext) entity(target string) (LootEntity, bool) {
	var e LootEntity
	switch target {
	case "this":
		e = c.This
	case "attacker":
		e = c.Attacker
	case "attacking_player":
		e = c.AttackingPlayer
		if e.Type == "" && c.Attacker.Type == "minecraft:player" {
			e = c.Attacker
		}
	case "direct_attacker":
		e = c.DirectAttacker
	case "target_entity":
		e = c.Target
	case "interacting_entity":
		e = c.Interacting
	}
	return e, e.Type != ""
}

// entityPlaceMatch tests the location, stepping_on, movement_affected_by,
// distance, movement and slots predicates.
// vanilla: EntityLocationPredicate, SteppingOnPredicate, MovementAffectedByPredicate, DistanceToPlayerPredicate, MovementPredicate, EntitySlotsPredicate
func entityPlaceMatch(p registry.LootEntityPredicate, e LootEntity, facts LootFacts, ctx *LootContext) bool {
	if p.Location != nil || p.SteppingOn != nil || p.MovementAffectedBy != nil || p.Distance != nil {
		pl := e.Place
		if pl == nil {
			return false
		}
		if p.Location != nil && !locationAt(*p.Location, ctx, int32(math.Floor(pl.X)), int32(math.Floor(pl.Y)), int32(math.Floor(pl.Z))) {
			return false
		}
		if p.SteppingOn != nil && (!pl.OnGround || !locationAt(*p.SteppingOn, ctx, pl.OnPos[0], pl.OnPos[1], pl.OnPos[2])) {
			return false
		}
		if p.MovementAffectedBy != nil && !locationAt(*p.MovementAffectedBy, ctx, pl.AffectingPos[0], pl.AffectingPos[1], pl.AffectingPos[2]) {
			return false
		}
		if p.Distance != nil {
			if ctx == nil || !ctx.HasOrigin {
				return false
			}
			if !p.Distance.Matches(float32(ctx.Origin[0]-pl.X), float32(ctx.Origin[1]-pl.Y), float32(ctx.Origin[2]-pl.Z)) {
				return false
			}
		}
	}
	if m := p.Movement; m != nil {
		x, y, z := facts.Movement[0]*20, facts.Movement[1]*20, facts.Movement[2]*20
		if !m.X.Matches(x) || !m.Y.Matches(y) || !m.Z.Matches(z) || !m.Speed.MatchesSqr(x*x+y*y+z*z) ||
			!m.HorizontalSpeed.MatchesSqr(x*x+z*z) || !m.VerticalSpeed.Matches(math.Abs(y)) || !m.FallDistance.Matches(facts.FallDistance) {
			return false
		}
	}
	for _, s := range p.Slots {
		if e.Slot == nil {
			return false
		}
		stacks, _ := e.Slot(s.Slots) // the decoder has refused an unknown name
		if !slices.ContainsFunc(stacks, func(t LootTool) bool { return itemPredicate(s.Item, t) }) {
			return false
		}
	}
	return true
}

// locationAt tests a location predicate against the block at an absolute
// position; a roll without a world fails it.
// vanilla: LocationPredicate.matches
func locationAt(k registry.LootLocationCheck, ctx *LootContext, x, y, z int32) bool {
	if ctx == nil || !ctx.HasOrigin || ctx.BlockAt == nil {
		return false
	}
	dx, dy, dz := x-BlockOf(ctx.Origin[0]), y-BlockOf(ctx.Origin[1]), z-BlockOf(ctx.Origin[2])
	return (k.Block == "" || blockAtMatches(ctx, k, dx, dy, dz)) &&
		(len(k.Biomes) == 0 || biomeAtMatches(ctx, k, dx, dy, dz))
}
