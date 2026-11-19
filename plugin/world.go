package plugin

import (
	"iter"
	"time"

	"github.com/golem-mc/golem/registry"
)

// World is a loaded dimension. Bukkit: World
type World interface {
	// Key answers the dimension key, e.g. minecraft:the_nether.
	Key() Key
	// Dimension answers the dimension type's key.
	Dimension() Key
	Seed() int64
	// MinY and Height bound the buildable blocks: MinY to MinY+Height-1.
	MinY() int
	Height() int
	// SeaLevel answers the dimension's sea level Y: 63 in the overworld.
	// Bukkit: World.getSeaLevel
	SeaLevel() int

	// BlockState answers the state at pos; an unloaded chunk reads as air.
	BlockState(pos BlockPos) BlockState
	// SetBlockState sets the state at pos with vanilla's update flags,
	// as /setblock does; false when the chunk is not loaded.
	SetBlockState(pos BlockPos, s BlockState, flags UpdateFlags) bool
	// Block answers the block at pos, a handle for the methods on Block.
	Block(pos BlockPos) Block
	// BlockEntity answers the block entity at pos, if one is there; assert
	// it to Sign, Container and the other kinds.
	BlockEntity(pos BlockPos) (BlockEntity, bool)
	// GenerateTree grows a tree with its trunk's base at pos, as a sapling
	// does without one; false when it does not fit. Bukkit: World.generateTree
	GenerateTree(t TreeType, pos BlockPos) bool
	// HighestBlockY answers the highest matching block's Y at x, z,
	// or MinY()-1 for an empty column or unloaded chunk.
	HighestBlockY(x, z int, h Heightmap) int
	Biome(pos BlockPos) registry.Biome
	// Light answers the sky and block light at pos, 0 to 15.
	Light(pos BlockPos) (sky, block int)

	// Chunk answers the loaded chunk at pos.
	Chunk(pos ChunkPos) (Chunk, bool)
	// LoadedChunks answers every chunk in memory. Bukkit: World.getLoadedChunks
	LoadedChunks() []Chunk
	// LoadChunk loads, and with generate set generates, the chunk at pos,
	// calling done on the tick goroutine when it is ready (nil on failure).
	// The chunk unloads again as usual unless a ticket holds it.
	LoadChunk(pos ChunkPos, generate bool, done func(Chunk))
	// AddTicket keeps the chunks within radius of pos loaded, and those
	// within radius-1 ticking, until RemoveTicket: /forceload for a plugin.
	// The plugin's remaining tickets are released when it disables.
	AddTicket(pos ChunkPos, radius int)
	RemoveTicket(pos ChunkPos, radius int)

	// Entities answers the loaded entities, players included.
	Entities() iter.Seq[Entity]
	// EntitiesIn answers the loaded entities whose boxes meet b.
	EntitiesIn(b Box) []Entity
	Entity(id UUID) (Entity, bool)
	// Players answers the players in this world.
	Players() []Player
	// Spawn spawns an entity of type t at loc as /summon does, with reason
	// SpawnPlugin; configure runs on the entity after it is added to the world.
	Spawn(t registry.EntityType, loc Location, configure func(Entity)) (Entity, error)
	// DropItem drops s at pos; naturally offsets it as a broken block's
	// drop is.
	DropItem(pos Vec3, s ItemStack, naturally bool) ItemEntity

	// Time is the shared game time in ticks. DayTime is the dimension type's
	// default clock's total ticks (0 with no clock); SetDayTime changes that
	// clock, doing nothing for a dimension type without one.
	Time() int64
	DayTime() int64
	SetDayTime(t int64)
	// Weather answers the shared weather flags; SetWeather sets them as
	// /weather does. rain is how long the rain lasts (how long the sky stays
	// clear for WeatherClear), thunder how long the thunder lasts; 0 is
	// vanilla's random length for rain, and the rain's length for thunder.
	Weather() Weather
	SetWeather(w Weather, rain, thunder time.Duration)
	// SetThundering flips only the thunder flag, leaving the rain flag and its
	// timer alone; d is how long the new state lasts, 0 rolls vanilla's length.
	// Bukkit: setThundering, setThunderDuration
	// Both end a forced clear (/weather clear).
	SetThundering(on bool, d time.Duration)
	// SetStorm flips only the rain flag, leaving thunder alone; d is as for
	// SetThundering.
	// Bukkit: setStorm, setWeatherDuration
	SetStorm(on bool, d time.Duration)
	// RainDuration and ThunderDuration answer the time left on each timer.
	RainDuration() time.Duration
	ThunderDuration() time.Duration
	Difficulty() Difficulty
	// GameRule answers a shared rule's value: an int, or 0 and 1 for a boolean.
	GameRule(r GameRule) int
	SetGameRule(r GameRule, v int)
	// SpawnPoint answers the shared world spawn; SetSpawnPoint moves it into
	// this dimension, using the location's position and facing.
	SpawnPoint() Location
	SetSpawnPoint(Location)
	Border() WorldBorder

	// Explode makes an explosion at pos as a source entity would (nil for
	// none), with the block interaction a TNT, mob or block explosion has.
	Explode(pos Vec3, power float32, fire bool, blocks ExplosionBlocks, source Entity)
	// StrikeLightning spawns a bolt at pos; effectOnly makes one that
	// neither burns nor hurts.
	StrikeLightning(pos Vec3, effectOnly bool) Entity
	// PlaySound plays a sound to the players in range of pos.
	PlaySound(pos Vec3, s Sound, cat SoundCategory, volume, pitch float32)
	// SpawnParticles shows particles to the players in range of pos.
	SpawnParticles(p Particle, pos Vec3, count int, offset Vec3, speed float64)
	// LevelEvent plays a level event (vanilla's LevelEvent IDs: door
	// sounds, block break particles), with its data.
	LevelEvent(event int32, pos BlockPos, data int32)
	// Save saves the world as /save-all does.
	Save(flush bool)
}

// TreeType is a kind of tree or huge mushroom for World.GenerateTree.
// Bukkit: TreeType
type TreeType uint8

const (
	TreeOak TreeType = iota
	TreeFancyOak
	TreeSpruce
	TreePine
	TreeMegaSpruce
	TreeMegaPine
	TreeBirch
	TreeJungle
	TreeMegaJungle
	TreeJungleBush
	TreeAcacia
	TreeDarkOak
	TreeSwamp
	TreeMangrove
	TreeTallMangrove
	TreeCherry
	TreeAzalea
	TreeRedMushroom
	TreeBrownMushroom
	TreeCocoa
	TreeTallBirch
	TreeChorusPlant
	TreeCrimsonFungus
	TreeWarpedFungus
	TreePaleOak
	TreePaleOakCreaking
)

// UpdateFlags are vanilla's Block.UPDATE_* flags for a block change.
type UpdateFlags uint16

const (
	// UpdateNeighbors updates the neighbours (Block.UPDATE_NEIGHBORS).
	UpdateNeighbors UpdateFlags = 1 << iota
	// UpdateClients sends the change to clients (Block.UPDATE_CLIENTS).
	UpdateClients
	// UpdateInvisible suppresses client redraws; it has no effect on the server.
	UpdateInvisible
	UpdateImmediate
	UpdateKnownShape
	UpdateSuppressDrops
	UpdateMoveByPiston
	UpdateSkipShapeUpdateOnWire
	UpdateSkipBlockEntitySideEffects
	UpdateSkipOnPlace

	// UpdateAll is Block.UPDATE_ALL, what /setblock uses.
	UpdateAll = UpdateNeighbors | UpdateClients
)

// BlockState is a block state's ID, what registry's state functions take.
type BlockState uint16

// Block answers the state's block.
func (s BlockState) Block() registry.Block { return registry.BlockOf(uint16(s)) }

// Prop answers a property's value, false when the block has no such
// property.
func (s BlockState) Prop(p registry.Prop) (string, bool) {
	v := registry.StateValue(uint16(s), p)
	return v, v != ""
}

// With answers the state with property p set to v, false when the block has
// no such property or value.
func (s BlockState) With(p registry.Prop, v string) (BlockState, bool) {
	n, ok := registry.WithProp(uint16(s), p, v)
	return BlockState(n), ok
}

// DefaultState answers b's default state.
func DefaultState(b registry.Block) BlockState { return BlockState(b.DefaultState()) }

// Block is a position in a world, with the methods a block has. Bukkit: Block
type Block struct {
	World World
	Pos   BlockPos
}

func (b Block) State() BlockState { return b.World.BlockState(b.Pos) }

// Set sets the state with UpdateAll.
func (b Block) Set(s BlockState) bool { return b.World.SetBlockState(b.Pos, s, UpdateAll) }

// Relative answers the block n blocks away on face f.
func (b Block) Relative(f BlockFace, n int) Block { return Block{b.World, b.Pos.Offset(f, n)} }

// Heightmap is one of vanilla's heightmaps.
type Heightmap uint8

const (
	WorldSurface Heightmap = iota
	MotionBlocking
	MotionBlockingNoLeaves
	OceanFloor
)

// Chunk is a loaded chunk. Bukkit: Chunk
type Chunk interface {
	World() World
	Pos() ChunkPos
	// Entities answers the entities in the chunk.
	Entities() []Entity
	BlockEntities() []BlockEntity
	// InhabitedTime is the ticks players have spent in it.
	InhabitedTime() int64
	// Data is the plugins' data saved with the chunk.
	Data() Data
}

// Weather is a world's weather.
type Weather uint8

const (
	WeatherClear Weather = iota
	WeatherRain
	WeatherThunder
)

// Difficulty is the game difficulty.
type Difficulty uint8

const (
	DifficultyPeaceful Difficulty = iota
	DifficultyEasy
	DifficultyNormal
	DifficultyHard
)

// ExplosionBlocks is what an explosion does to blocks (vanilla's
// Explosion.BlockInteraction).
type ExplosionBlocks uint8

const (
	ExplosionKeep ExplosionBlocks = iota
	ExplosionDestroy
	ExplosionDestroyWithDecay
	ExplosionTriggerBlocks
)

// SoundCategory is the volume slider a sound plays under.
type SoundCategory uint8

const (
	SoundMaster SoundCategory = iota
	SoundMusic
	SoundRecords
	SoundWeather
	SoundBlocks
	SoundHostile
	SoundNeutral
	SoundPlayers
	SoundAmbient
	SoundVoice
	SoundUI
)

// Particle is a particle type with the options its type takes; the fields
// a type does not use are ignored.
type Particle struct {
	Type ParticleType
	// Color is dust's, entity effect's and others' ARGB colour; To is dust
	// transition's second colour; Size is the dust's scale.
	Color, To uint32
	Size      float32
	// State is block and falling dust particles' block state.
	State BlockState
	// Item is the item particle's stack.
	Item *ItemStack
	// Roll is sculk charge's angle; Delay is shriek's delay in ticks.
	Roll  float32
	Delay int
	// Power is effect and dragon breath's strength; nil uses 1.
	Power *float32
	// Target is trail's endpoint or vibration's block destination.
	Target Vec3
	// Duration is trail's lifetime; ArrivalTicks is vibration's travel time.
	Duration, ArrivalTicks int
	// WaterBlocks is geysers' water column height; BurstImpulse is base and poof's impulse.
	WaterBlocks  int
	BurstImpulse float32
	// Long shows the particle from up to 512 blocks rather than 32
	// (vanilla's overrideLimiter).
	Long bool
}

// WorldBorder is a world's border. Bukkit: WorldBorder
type WorldBorder interface {
	Center() (x, z float64)
	SetCenter(x, z float64)
	Size() float64
	// SetSize moves the border to size over d (0 at once).
	SetSize(size float64, d time.Duration)
	DamagePerBlock() float64
	SetDamagePerBlock(float64)
	WarningDistance() int
	SetWarningDistance(int)
	Contains(pos Vec3) bool
}

// Generator generates a world's chunks in place of vanilla's: a plugin's
// own terrain. It is called from the generation workers, not the tick, so
// it must be safe for concurrent use and touch nothing but the chunk it is
// given. Bukkit: ChunkGenerator
type Generator interface {
	// Generate fills a new chunk's blocks and biomes. rng is seeded from
	// the world seed and the chunk position.
	Generate(c ChunkWriter, rng Random)
	// Populate decorates a new chunk right after Generate: trees, ores,
	// structures. Its writes are limited to the chunk itself.
	Populate(r RegionWriter, pos ChunkPos, rng Random)
	// SpawnPoint answers where players first spawn.
	SpawnPoint(w World) Location
}

// ChunkWriter is a chunk being generated.
type ChunkWriter interface {
	Pos() ChunkPos
	MinY() int
	Height() int
	// Set and Get use coordinates within the chunk: x and z 0 to 15.
	Set(x, y, z int, s BlockState)
	Get(x, y, z int) BlockState
	SetBiome(x, y, z int, b registry.Biome)
}

// RegionWriter is the chunk being populated, in world coordinates. Unlike
// Bukkit's 3x3 region, a write outside the chunk is dropped and a read outside
// it answers air: chunks here have no generation status that would order a
// neighbour's decoration.
type RegionWriter interface {
	Set(pos BlockPos, s BlockState)
	Get(pos BlockPos) BlockState
	Biome(pos BlockPos) registry.Biome
}

// Random is the random number generator a generator is handed. vanilla:
// RandomSource
type Random interface {
	Int(n int) int
	Int64() int64
	Float64() float64
	Bool() bool
}

// BlockEntity is a block entity: the data a chest, sign or furnace keeps.
// Assert it to the interfaces below. Bukkit: TileState
type BlockEntity interface {
	World() World
	Pos() BlockPos
	// Type is the block entity type's key, e.g. minecraft:chest.
	Type() Key
	IsValid() bool
	// Data is the plugins' data saved with the block entity.
	Data() Data
}

// Nameable is a block entity or an entity with a custom name.
type Nameable interface {
	// CustomName answers the name, nil for none.
	CustomName() *Text
	SetCustomName(*Text)
}

// LockPredicate is a complete saved vanilla ItemPredicate compound in SNBT:
// the value of the lowercase `lock` key without that key, for example
// {items:"#minecraft:planks",count:{min:2}}. It is saved SNBT, not /give or
// /execute if items syntax. It replaces an ItemStack lock because one stack
// cannot represent item alternatives or tags, count ranges and independent
// exact and partial component predicates.
//
// {} is an explicit predicate that permits every hand, including empty.
// Assignment validates compound syntax only, so it does not guarantee codec
// validity or support by the current matcher, which handles items, count and
// exact custom names; the full component and predicate payload is preserved
// for saving.
type LockPredicate string

// Container is a block entity with an inventory: a chest, barrel, shulker
// box, hopper, dispenser, dropper, crafter, furnace or brewing stand. It is a
// live tick-thread handle for one block entity, one half of a double chest
// included. A removed or unloaded handle cannot edit a replacement: its
// settings read as zero values (nil for names and locks). Names apply to
// menus opened afterwards; changing a lock leaves open menus alone.
type Container interface {
	BlockEntity
	Nameable
	// Inventory is the container's own slots.
	Inventory() Inventory
	// Lock answers a detached snapshot normalised by the saved lock codec,
	// nil for none or a stale handle: unsupported top-level keys disappear,
	// component and predicate keys are qualified, empty matcher maps
	// disappear and custom-data predicates become SNBT strings. Formatting,
	// field order and redundant spelling are not kept; reading then setting
	// a lock keeps its normalised value through save and reload.
	Lock() *LockPredicate
	// SetLock accepts compound SNBT only; nil clears. An empty string,
	// malformed or non-compound SNBT and trailing input do nothing.
	SetLock(*LockPredicate)
	// LootTable answers the loot table filling it on first open; zero for
	// none.
	LootTable() (table Key, seed int64)
	// SetLootTable only applies to randomizable containers and does not
	// unpack the table; a zero table clears the table and seed. Furnaces
	// and brewing stands ignore it.
	SetLootTable(table Key, seed int64)
}

// Furnace is a furnace, blast furnace or smoker.
type Furnace interface {
	Container
	// BurnTime is the ticks the fuel has left; CookTime the ticks cooked of
	// CookTimeTotal.
	BurnTime() int
	SetBurnTime(int)
	CookTime() int
	// The setters accept any int32 and leave ordinary ticking to the
	// furnace's ticker.
	SetCookTime(int)
	CookTimeTotal() int
}

// Sign is a sign or hanging sign.
type Sign interface {
	BlockEntity
	// Lines answers a side's four lines.
	Lines(front bool) [4]Text
	SetLines(front bool, lines [4]Text)
	Color(front bool) DyeColor
	SetColor(front bool, c DyeColor)
	Glowing(front bool) bool
	SetGlowing(front bool, glow bool)
	Waxed() bool
	SetWaxed(bool)
}

// Spawner is a monster spawner's spawn data.
type Spawner interface {
	BlockEntity
	EntityType() registry.EntityType
	SetEntityType(registry.EntityType)
	// Delay is the ticks to the next spawn; MinDelay and MaxDelay bound the
	// random delay after it.
	Delay() int
	SetDelay(int)
	MinDelay() int
	MaxDelay() int
	SetDelays(lo, hi int)
	SpawnCount() int
	SetSpawnCount(int)
	RequiredPlayerRange() int
	SetRequiredPlayerRange(int)
}

// TrialSpawner is a trial spawner's live settings and selected spawn data.
// It is separate from the monster Spawner because trial settings have one
// fixed interval and four independent fractional counts, which Spawner's
// random delay bounds and integer count cannot represent. Its methods run on
// the tick goroutine. A removed or unloaded handle rejects writes and reads
// zero values (-1 for EntityType); Config on it answers the zero struct, not
// the defaults.
type TrialSpawner interface {
	BlockEntity
	// EntityType reads only the saved selection: -1 until spawn data is
	// selected, or when unrecognised or stale.
	EntityType() registry.EntityType
	// SetEntityType replaces both modes' potentials and resets the trial to
	// inactive, preserving ominous, private Data and loot being ejected;
	// entities it spawned stay alive.
	SetEntityType(registry.EntityType)
	// NextSpawnAt is absolute dimension game time, not a countdown.
	NextSpawnAt() int64
	// SetNextSpawnAt accepts any signed game time. A past value lets an
	// eligible active trial spawn, without activating it.
	SetNextSpawnAt(int64)
	RequiredPlayerRange() int
	// SetRequiredPlayerRange accepts 1..128 without resetting registrations;
	// the range is shared by both modes.
	SetRequiredPlayerRange(int)
	// Config returns a snapshot of the explicitly selected mode, true for the
	// ominous configuration, whatever the block's current mode is; zero if
	// stale. Fields missing from the saved configuration read as range 4,
	// total 6, simultaneous 2, added total 2, added simultaneous 1 and
	// interval 40.
	Config(ominous bool) TrialSpawnerConfig
	// SetConfig atomically replaces the numeric settings of the selected mode
	// only and never switches the block's ominous property. Potentials
	// (equipment and custom spawn rules included), loot settings, registered
	// players and runtime data are kept, and a named configuration is copied
	// inline on that spawner only. The interval affects the next scheduling
	// decision; a pending NextSpawnAt is unchanged. All fields are rejected
	// unless the range is 1..128, the interval 0..2147483647 and all four
	// counts finite and nonnegative.
	SetConfig(ominous bool, config TrialSpawnerConfig)
}

// TrialSpawnerConfig holds fixed cadence and fractional mob limits for one mode.
// Range is 1..128, interval is 0..2147483647, and counts are finite/nonnegative.
// Targets floor the sum of the base count and its per-additional-player count.
type TrialSpawnerConfig struct {
	SpawnRange                     int
	TotalMobs                      float32
	SimultaneousMobs               float32
	TotalMobsAddedPerPlayer        float32
	SimultaneousMobsAddedPerPlayer float32
	TicksBetweenSpawn              int
}

// Skull is a player head or mob skull.
type Skull interface {
	BlockEntity
	// Profile answers a player head's owner, nil for none.
	Profile() *Profile
	SetProfile(*Profile)
	NoteBlockSound() Key
	SetNoteBlockSound(Key)
}

// CommandBlock is a command block.
type CommandBlock interface {
	BlockEntity
	Nameable
	Command() string
	SetCommand(string)
	SuccessCount() int
}

// Jukebox is a jukebox.
type Jukebox interface {
	BlockEntity
	Record() ItemStack
	SetRecord(ItemStack)
	Playing() bool
}

// Lectern is a lectern.
type Lectern interface {
	BlockEntity
	Book() ItemStack
	SetBook(ItemStack)
	Page() int
	SetPage(int)
}

// Beacon is a beacon.
type Beacon interface {
	BlockEntity
	Nameable
	// Levels is the pyramid's height, 0 to 4.
	Levels() int
	// Effects are the chosen primary and secondary effects, -1 for none.
	Effects() (primary, secondary Effect)
	SetEffects(primary, secondary Effect)
}

// DyeColor is one of the 16 dye colours, in vanilla's order.
type DyeColor uint8

const (
	White DyeColor = iota
	Orange
	Magenta
	LightBlue
	Yellow
	Lime
	Pink
	Gray
	LightGray
	Cyan
	Purple
	Blue
	Brown
	Green
	Red
	Black
)
