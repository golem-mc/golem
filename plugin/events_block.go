package plugin

// BlockBreakEvent fires when a player breaks a block, before it is
// removed; Drops and Experience may be turned off or changed. Bukkit:
// BlockBreakEvent
type BlockBreakEvent struct {
	Cancellable
	Player     Player
	Block      Block
	State      BlockState
	DropItems  bool
	Experience int
}

// BlockDropItemEvent fires with the items a broken block drops, before
// they spawn; Items may be changed. Bukkit: BlockDropItemEvent
type BlockDropItemEvent struct {
	Cancellable
	Player Player
	Block  Block
	State  BlockState
	Items  []ItemStack
}

// BlockDamageEvent fires when a player starts breaking a block; InstaBreak
// may be set. Bukkit: BlockDamageEvent
type BlockDamageEvent struct {
	Cancellable
	Player     Player
	Block      Block
	Item       ItemStack
	InstaBreak bool
}

// BlockDamageAbortEvent fires when a player stops breaking a block.
type BlockDamageAbortEvent struct {
	Base
	Player Player
	Block  Block
}

// BlockPlaceEvent fires when a player places a block, with the state it will
// have and before the world is written; cancelling places and spends nothing
// and resends the cells to the player.
// Blocks lists every position a multi-block placement (a bed, a door)
// sets. Bukkit: BlockPlaceEvent, BlockMultiPlaceEvent
type BlockPlaceEvent struct {
	Cancellable
	Player   Player
	Block    Block
	Placed   BlockState
	Replaced BlockState
	Against  Block
	Hand     Hand
	Item     ItemStack
	Blocks   []BlockPos
}

// SignChangeEvent fires when a player edits a sign; Lines may be changed.
// Bukkit: SignChangeEvent
type SignChangeEvent struct {
	Cancellable
	Player Player
	Block  Block
	Front  bool
	Lines  [4]Text
}

// BlockBurnEvent fires when fire destroys a block. Bukkit: BlockBurnEvent
type BlockBurnEvent struct {
	Cancellable
	Block Block
	// Fire is the fire block that spread to it, nil for lava.
	Fire *Block
}

// BlockIgniteEvent fires when a block is set on fire. Bukkit:
// BlockIgniteEvent
type BlockIgniteEvent struct {
	Cancellable
	Block  Block
	Cause  IgniteCause
	Player Player // nil when no player lit it
	Entity Entity // the igniting entity, if any
}

// IgniteCause is what set a block on fire.
type IgniteCause uint8

const (
	IgniteFlintAndSteel IgniteCause = iota
	IgniteFireball
	IgniteLava
	IgniteLightning
	IgniteSpread
	IgniteExplosion
	IgniteArrow
	IgniteEnderCrystal
)

// BlockSpreadEvent fires when a block spreads to another: fire, grass,
// mycelium, vines, mushrooms, sculk, bamboo. Bukkit: BlockSpreadEvent
type BlockSpreadEvent struct {
	Cancellable
	Block    Block
	Source   Block
	NewState BlockState
}

// BlockFormEvent fires when a block forms by itself: snow, ice, concrete,
// cobblestone, obsidian, frosted ice. Bukkit: BlockFormEvent
type BlockFormEvent struct {
	Cancellable
	Block    Block
	NewState BlockState
	// Entity is the entity forming it (a snow golem, Frost Walker), if any.
	Entity Entity
}

// BlockFadeEvent fires when a block fades: ice or snow melting, coral
// dying, fire burning out, farmland drying. Bukkit: BlockFadeEvent
type BlockFadeEvent struct {
	Cancellable
	Block    Block
	NewState BlockState
}

// BlockGrowEvent fires when a crop, sapling stage, cactus, sugar cane or
// another plant grows; NewState may be changed. Bukkit: BlockGrowEvent
type BlockGrowEvent struct {
	Cancellable
	Block    Block
	NewState BlockState
}

// BlockFertilizeEvent fires when bone meal is used on a block; Blocks are
// the changes it makes. Bukkit: BlockFertilizeEvent
type BlockFertilizeEvent struct {
	Cancellable
	Player Player // nil for a dispenser
	Block  Block
	Blocks []BlockChange
}

// BlockChange is one block a growth, explosion or structure sets.
type BlockChange struct {
	Pos   BlockPos
	State BlockState
}

// StructureGrowEvent fires when a sapling or mushroom grows into a tree or
// huge mushroom; Blocks may be changed. Bukkit: StructureGrowEvent
type StructureGrowEvent struct {
	Cancellable
	World  World
	Pos    BlockPos
	Player Player // the player who bone-mealed it, if any
	Blocks []BlockChange
}

// BlockFromToEvent fires when water or lava flows into a block, and when a
// dragon egg teleports. Bukkit: BlockFromToEvent
type BlockFromToEvent struct {
	Cancellable
	From Block
	To   Block
	Face BlockFace
}

// BlockPhysicsEvent fires for every neighbour update a block receives:
// thousands a tick in a busy world, so a listener must be cheap.
// Cancelling stops the block reacting. Bukkit: BlockPhysicsEvent
type BlockPhysicsEvent struct {
	Cancellable
	Block  Block
	Source Block
	State  BlockState
}

// BlockRedstoneEvent fires when a block's redstone power changes; New may
// be changed. Bukkit: BlockRedstoneEvent
type BlockRedstoneEvent struct {
	Base
	Block    Block
	Old, New int
}

// BlockPistonEvent fires before a piston extends or retracts; Blocks are those
// it moves. Bukkit: BlockPistonExtendEvent, BlockPistonRetractEvent
type BlockPistonEvent struct {
	Cancellable
	Piston    Block
	Direction BlockFace
	Extending bool
	Blocks    []Block
}

// BlockDispenseEvent fires when a dispenser or dropper fires; Item and
// Velocity may be changed. Cancelling returns the item to its slot. Velocity
// aims a launched projectile; a plain item drop keeps its own motion. Bukkit: BlockDispenseEvent
type BlockDispenseEvent struct {
	Cancellable
	Block    Block
	Item     ItemStack
	Velocity Vec3
}

// BlockExplodeEvent fires when a block explodes (a bed or respawn anchor
// in the wrong dimension); Blocks may be trimmed, Yield is the share
// dropped. Bukkit: BlockExplodeEvent
type BlockExplodeEvent struct {
	Cancellable
	Block  Block
	State  BlockState
	Blocks []Block
	Yield  float32
}

// LeavesDecayEvent fires when leaves decay. Bukkit: LeavesDecayEvent
type LeavesDecayEvent struct {
	Cancellable
	Block Block
}

// NotePlayEvent fires when a note block plays; the note and instrument
// may be changed. Bukkit: NotePlayEvent
type NotePlayEvent struct {
	Cancellable
	Block      Block
	Instrument string
	Note       int
}

// BlockCookEvent fires when a furnace, smoker, blast furnace or campfire
// finishes an item; Result may be changed. Bukkit: BlockCookEvent
type BlockCookEvent struct {
	Cancellable
	Block  Block
	Source ItemStack
	Result ItemStack
}

// FurnaceBurnEvent fires when a furnace consumes fuel; BurnTime may be
// changed. Bukkit: FurnaceBurnEvent
type FurnaceBurnEvent struct {
	Cancellable
	Block    Block
	Fuel     ItemStack
	BurnTime int
}

// BrewEvent fires when a brewing stand finishes; Results may be changed.
// Bukkit: BrewEvent
type BrewEvent struct {
	Cancellable
	Block   Block
	Results [3]ItemStack
}

// CauldronLevelChangeEvent fires when a cauldron's contents change. Bukkit:
// CauldronLevelChangeEvent
type CauldronLevelChangeEvent struct {
	Cancellable
	Block    Block
	Entity   Entity // what changed it, if anything
	NewState BlockState
}

// TNTPrimeEvent fires when TNT is lit. Bukkit: TNTPrimeEvent
type TNTPrimeEvent struct {
	Cancellable
	Block  Block
	Entity Entity // what lit it, if anything
}

// SpongeAbsorbEvent fires when a sponge soaks up water; Blocks may be
// trimmed. Bukkit: SpongeAbsorbEvent
type SpongeAbsorbEvent struct {
	Cancellable
	Block  Block
	Blocks []BlockPos
}

// BellRingEvent fires when a bell rings. Bukkit: BellRingEvent
type BellRingEvent struct {
	Cancellable
	Block  Block
	Face   BlockFace
	Entity Entity
}

// GenericGameEvent fires for a vanilla game event (a step, a block change, an
// eat) that sculk sensors and wardens hear; Radius may be changed. Bukkit:
// GenericGameEvent
type GenericGameEvent struct {
	Cancellable
	Event  Key
	World  World
	Pos    Vec3
	Entity Entity
	Radius int
}
