package world

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The in-memory shape every dimension shares, the overworld's: one chunk type
// over this Y band, within which narrower dimensions' generators keep to the
// band their dimension type describes (dimension.go).
const (
	worldMinY    = -64
	worldHeight  = 384
	sectionCount = worldHeight / sectionSize
	SeaLevel     = 63
	GroundLevel  = 64 // the first air block above the stone

	// flatSeaLevel is the flat generator's sea level, one above the world's
	// floor.
	// vanilla: FlatLevelSource.getSeaLevel
	flatSeaLevel = -63

	// Global palette state IDs, built into the client: air is 0 and stone 1,
	// the first two blocks registered.
	airState   = 0
	stoneState = 1

	// lightSections covers the world's sections plus one below and one above.
	lightSections = sectionCount + 2
	lightArrayLen = 2048 // 4 bits per block, 4096 blocks per section
)

// ChunkPos identifies a chunk column by its chunk coordinates.
type ChunkPos struct {
	X, Z int32
}

// World holds the chunks players have loaded. A chunk is read from disk on
// first acquire, or generated if absent, and saved when the last player leaves
// it, so a broken block stays broken.
//
// A chunk only generated and never changed is not saved: the generator
// recreates it, and skipping the write keeps a walk across an empty world off
// the disk. Only a changed chunk is marked dirty and written on release.
//
// Spawn and metadata come from level.dat, or defaults with a level.dat written
// on Save so vanilla can open the world; the age round-trips the same way.
// Clocks and weather are saved to data/minecraft/world_clocks.dat and
// weather.dat (clocks.go, weather.go), where vanilla reads them too.
//
// A World is one of vanilla's three dimensions: its wire name and shape
// (dimension.go) decide how chunks reach a client, and which one it is decides
// whether it owns level.dat or only keeps region files under the world's
// dimensions folder.
type World struct {
	// journal and journaling are guarded by mu; see JournalStart.
	journal    []journalEntry
	journaling bool
	// limit is the dimension's world border, set once at construction; nil
	// means no limit.
	limit *Border
	dir   string
	gen   Generator
	// debug is the debug world flag: no block writes, ticks or spawning.
	// Every dimension of a debug world carries it.
	debug atomic.Bool
	// fixture, when set, hands out the chunks gen would generate.
	fixture *GenerationFixture
	level   *LevelData
	// lootFill rolls a container's pending loot table; set once at start-up.
	lootFill LootFill
	// shulkerLidsOpen holds the cells of shulker boxes whose lid is not CLOSED,
	// each with its progress in tenths (int). Physics writes it, SetBlock drops
	// a cell it writes, and ConductorAt and ShulkerBoxBoxes read it lock-free
	// from under the engine's mutex.
	shulkerLidsOpen sync.Map
	// lootReads answers the scores and storages a roll in this level reads;
	// set once at start-up.
	lootReads LootReads
	// signResolver reads command state when a live sign loads its text.
	signResolver SignResolver
	// sittingCats is the mob query behind ChestBlockedAt; set once at start-up.
	sittingCats SittingCats
	// storage is the dimension's folder, holding its region and entities
	// folders.
	storage string
	// border tracks what world_border.dat last held.
	border borderSaved

	// dim is the shape chunks are cut to on wire and disk, and dimName the name
	// Login (play) and Respawn carry. levelDat says whether this world owns
	// level.dat; only the overworld does.
	dim      dimensionShape
	dimName  string
	levelDat bool
	// skyLight is the type's gameplay/sky_light_level layers, which SkyDarken
	// reads without a position.
	skyLight        envLayers
	environment     map[string]envLayers
	tickEnvironment [envCount]envLayers
	// testTimelines replaces the layers after the defaults during a game test.
	testTimelines atomic.Pointer[[]registry.EnvTimeline]
	// spawnOwner is the overworld whose level.dat holds the spawn; nil on that
	// world itself.
	spawnOwner *World

	// mu guards the chunk map; its read lock covers reads of a chunk's blocks
	// and encoded form, except BlockAt's through the near cache, which take no
	// lock (Chunk.shared). Lock order: a block entity's lock (BlockEntity.Use)
	// before mu, since a tick holding an entity's lock marks its chunk dirty
	// through here. So never take an entity's lock while holding mu: entity
	// data is read and written with mu released (ChunkPacket, saveChunk,
	// BlockEntityTag, SetBlockEntityTag).
	mu      sync.RWMutex
	chunks  map[ChunkPos]*loadedChunk
	packets chunkPacketRetention
	// Block counts in w.chunks, updated under mu. Touch reads the comparator
	// count without it.
	touchBlocks      int
	comparatorBlocks atomic.Int64
	// tickingChunks holds the loaded chunks with a trial spawner or vault, so a
	// tick without any walks nothing. Guarded by mu.
	tickingChunks map[ChunkPos]struct{}
	// near is a direct-mapped chunk cache indexed by the coordinates' low bits,
	// so a hit skips the hash. A slot holds at most one of the chunks sharing
	// it; the rest are in the map. Slots are loaded atomically, so BlockAt
	// reads one with no lock. Unload and load change a slot under mu for
	// writing, in step with the map; a read that missed refills its slot under
	// mu for reading, which excludes both. So a chunk that has left the map has
	// left its slot.
	near [nearSlots * nearSlots]atomic.Pointer[loadedChunk]
	// chunkSet counts chunks entering or leaving w.chunks, so a reader that
	// last saw the same count knows the loaded set is unchanged.
	chunkSet atomic.Uint64
	// forced is the chunks /forceload keeps loaded, guarded by mu.
	forced  *forcedChunks
	tickets chunkTickets
	// blockEntityOrder is all loaded chunks' block entities in the order they
	// joined the world, the order Level.blockEntityTickers ticks them: one list
	// per level, appended on add and on chunk load, dropped on unload. Guarded
	// by mu.
	blockEntityOrder    []*BlockEntity
	nextEntityTickOrder uint64
	// tickingBlockEntities is the subset of blockEntityOrder whose kind
	// blockEntityTicks, in the same order. Guarded by mu (TickingBlockEntities).
	tickingBlockEntities []*BlockEntity
	// sculkBlockEntities is the subset of blockEntityOrder that keeps a
	// listener or is a catalyst, in the same order. Guarded by mu.
	sculkBlockEntities []*BlockEntity

	// changed is the cells told to the players since the tick's last flush.
	changed SectionChanges
	// lightChanged is the chunks whose recomputed light awaits the tick's flush.
	lightChanged LightChanges

	// containerChanges is the containers whose contents moved since the last
	// physics tick, in order, each with its tick. Touch and TouchEntity append
	// under mu; Physics.Tick drains them to wake comparators. The wake is
	// deferred because TouchEntity runs under the entity's lock in
	// decideDispense and commitDispense, which the engine takes in the other
	// order (BlockEntity.Use's lock order).
	containerChanges []containerChange
	// containerChangeSeen is reused by takeContainerChanges under mu.
	containerChangeSeen map[uint64]int
	// containerChangesSpare is the buffer of the drain before last, taken back
	// for the next batch; a drained slice is valid until the following drain.
	containerChangesSpare []containerChange

	// pendingGenerated is the blocks generated chunks scheduled a tick for,
	// in world coordinates and chunk-load order: recorded on the chunk
	// (Chunk.scheduleGenerated), appended on load, and drained by Physics.Tick so a
	// spring flows on the chunk's first tick. Guarded by mu.
	pendingGenerated []posKey

	// pendingLoaded is the ticks loaded chunks read and unloadedChunks the
	// chunks that left memory. Physics takes unloads before booking or ticking,
	// and loaded ticks before ticking; tickSource is its save view. Guarded by mu.
	pendingLoaded  []loadedTick
	unloadedChunks []ChunkPos
	tickSource     func(ChunkPos) chunkTicks

	// chunkWriteGate is a test hook; see SetChunkWriteGate.
	chunkWriteGate atomic.Pointer[func()]
	// chunkGenerationGate is a test hook; see SetChunkGenerationGate.
	chunkGenerationGate atomic.Pointer[func(ChunkPos)]
	chunkHooks          atomic.Pointer[ChunkHooks]
	// entityWriteFault is a test hook; see SetEntityWriteFault.
	entityWriteFault atomic.Pointer[error]
	entityWriteGate  atomic.Pointer[func()]
	regionMu         sync.Mutex
	regions          *regionCache
	entityRegions    *regionCache
	entityWrites     entityWrites
	// saveFormat is what chunk and entity records are written with, set by
	// SetSaveFormat before the first region file opens and guarded by regionMu
	// with the caches.
	saveFormat saveFormat

	// onLowDiskSpace and freeSpace are set once at construction
	// (SetSaveFailureHook, SetFreeSpace).
	onLowDiskSpace func()
	freeSpace      func(dir string) (uint64, error)

	// timeMu guards the world's age and clocks (clocks.go), advanced once per
	// tick and read for Update Time. It is separate from mu so the tick does
	// not contend with chunk loads and saves.
	timeMu sync.Mutex
	// worldAge mirrors level.WorldAge for readers that need only the tick.
	worldAge atomic.Int64
	// pluginGen is a plugin world's terrain spec, guarded by timeMu.
	pluginGen PluginGenSpec

	// timeSource is the overworld whose age, clocks and weather flags this
	// world reads, set for every other dimension by the server: vanilla
	// dimensions share the overworld's time and weather (DerivedLevelData), so
	// a container change anywhere carries the tick physics runs on. Nil for
	// the overworld.
	timeSource *World

	// ruleSource is the overworld whose game rules a plugin world with its own
	// level state (SetOwnLevel) obeys; nil for every other world.
	ruleSource *World

	// zoomSeed is levelgen.BiomeZoomSeed of the level seed, set when the seed
	// settles; a nether or end world reads its timeSource's.
	zoomSeed int64

	// diceMu guards dice, the weather's and spawn search's random source,
	// seeded through SeedDice.
	diceMu sync.Mutex
	dice   *rand.Rand

	// clocks is one clock per minecraft:world_clock entry in registry ID order
	// (clocks.go), guarded by timeMu, read from data/minecraft/world_clocks.dat
	// and written back on Save. Only the overworld carries real state; the
	// nether's and end's defaults are never asked.
	clocks []clock

	// rules is the game rule table from data/minecraft/game_rules.dat, written
	// back on Save so /gamerule persists. Only the overworld has one (the
	// server has one set, and the other dimensions own no level.dat); every
	// reader asks the overworld, whose accessors fall back to defaults. Guarded
	// by timeMu with the clock and weather the rules gate.
	rules *ruleValues
	// ownRules marks a plugin world whose rules live in its own data/ folder.
	ownRules bool
	// feedbackOff has a bit set per feedback rule that is false, so the zero
	// value is the all-true defaults of a world without a table; see
	// FeedbackRules.
	feedbackOff atomic.Uint32
	// spawnEnemies is latched at load, difficulty sets and spawn_monsters
	// writes. Changing spawn_mobs alone must not refresh it.
	spawnEnemies atomic.Bool
	// spawnWorlds shares rule/difficulty refreshes with attached dimensions,
	// guarded by timeMu. Each dimension keeps its own latched flag.
	spawnWorlds []*World

	// sky is the rain and thunder levels fading toward the weather flags,
	// guarded by timeWorld().timeMu, which also guards the flags: each world
	// with weather fades its own levels toward the overworld's flags.
	sky skyLevels
	// raining is w.sky.raining() && w.CanHaveWeather(), stored by whoever
	// writes w.sky under timeMu.
	raining atomic.Bool

	exec *executors
	// relights queues the relights after block changes (relightlane.go).
	relights relightLane
	// relightComputed, when set, runs in relightArea between a copy's compute
	// and install with no lock held, for tests to change the world in that gap.
	relightComputed func()
	// joinComputed, when set, runs in insertChunk between a join's compute and
	// install with no lock held, for tests to change the world in that gap.
	joinComputed func(ChunkPos)
	// dropUntouched leaves fresh chunks unmarked, so an unchanged one is
	// discarded on unload instead of saved. Guarded by mu; see
	// SetDropUntouchedChunks.
	dropUntouched bool
}

// containerChange is a container whose contents moved and the tick it moved in,
// from which Physics.Tick schedules the comparator so its delay does not grow
// by the ticks until the drain.
type containerChange struct {
	pos  posKey
	tick int64
}

// loadedChunk is a chunk in memory and the number of players that have it.
type loadedChunk struct {
	// pos and chunk are fixed at insert, so BlockAt reads them with no lock.
	pos   ChunkPos
	chunk *Chunk
	// users is atomic because an acquire finding the chunk loaded raises it
	// under the read lock. It is lowered under the read lock only while it
	// stays above zero (dropHold); the drop to zero, and the unload decision,
	// happen under the write lock, which no raise can overlap.
	users atomic.Int32
	// structureBounds caches saved piece or full bounds per structure, guarded by mu.
	structureBounds map[structureBoundsKey][]structureBox
	// structureRefs caches the structure ids the chunk references, guarded by
	// mu; structureRefsRead tells nil from not yet read.
	structureRefs     []string
	structureRefsRead bool
	// fresh is set when the chunk was generated because no region file held it.
	fresh bool
	// dirtyGen counts marks. Unload sets its high bit to claim the chunk
	// from Touch. A loaded chunk needs saving while dirtyGen is ahead of savedGen.
	dirtyGen atomic.Uint64
	// containerGen publishes block and container-identity changes to hopper readers.
	containerGen atomic.Uint64
	// surface caches the per-column surface heights (highest block's y), which
	// the natural spawn asks once per column it rolls for; the scan walks the
	// chunk's blocks without a lock per block. The first ask computes it and a
	// write keeps its own column true (updateSurface), so a constantly changing
	// chunk is not rescanned whole. A cell is read and written atomically:
	// SurfaceHeight reads it through the near cache without w.mu.
	surface atomic.Pointer[[16][16]int32]
	// motion caches the per-column MOTION_BLOCKING heights the rain reads,
	// primed off the tick thread at load; a write refreshes only its column
	// rather than dropping it.
	motion *[16][16]int32
	// saveMu serialises the chunk's unload saves in the I/O pool, and savedGen
	// is the newest dirty generation written. Otherwise two unload saves of a
	// chunk released, reloaded, changed and released again could run at once,
	// and the older landing last would leave stale bytes.
	saveMu   sync.Mutex
	savedGen atomic.Uint64
}

// nearSlots is the side of the near cache's square, a power of two.
const nearSlots = 64

func nearIndex(pos ChunkPos) int {
	return int(pos.X&(nearSlots-1))*nearSlots + int(pos.Z&(nearSlots-1))
}

// nearAt is pos's chunk if its near slot holds it, else nil. It takes no lock.
func (w *World) nearAt(pos ChunkPos) *loadedChunk {
	if l := w.near[nearIndex(pos)].Load(); l != nil && l.pos == pos {
		return l
	}
	return nil
}

// loadedAt is w.chunks[pos], through the near cache. The caller holds w.mu.
func (w *World) loadedAt(pos ChunkPos) (*loadedChunk, bool) {
	if l := w.nearAt(pos); l != nil {
		return l, true
	}
	l, ok := w.chunks[pos]
	return l, ok
}

// dropNear clears pos's near slot if it holds l. The caller holds w.mu for
// writing.
func (w *World) dropNear(pos ChunkPos, l *loadedChunk) {
	w.near[nearIndex(pos)].CompareAndSwap(l, nil)
}

const unloadedGen = uint64(1) << 63

// markDirty reports whether the mark landed before unload claimed the chunk.
func (l *loadedChunk) markDirty() bool {
	return l.dirtyGen.Add(1) < unloadedGen
}

// NewWorld builds the overworld, which owns the world's level.dat, saving
// chunks under dir and generating missing ones with gen. Spawn and metadata
// come from level.dat, or defaults. A seeded generator's seed is settled from
// level.dat, level-seed, or minted at creation, and a new world gets the spawn
// its generator finds written to a new level.dat. A level.dat and backup that
// both fail to read are an error.
func NewWorld(dir string, gen Generator) (*World, error) {
	return newWorld(dir, gen, DimensionName, dimensionShapes[DimensionName], true)
}

// newWorld builds one dimension's world: dir is the world folder, name the
// dimension's wire name, shape its chunk form on wire and disk, and levelDat
// whether it owns a level.dat (the nether and end do not; see
// NewDimensionWorld). An overworld whose level.dat and backup both fail to read
// is an error, so a fresh world never starts over unreadable data.
func newWorld(dir string, gen Generator, name string, shape dimensionShape, levelDat bool) (*World, error) {
	level := defaultLevel("")
	if levelDat {
		loaded, err := LoadLevel(dir)
		if err != nil {
			return nil, err
		}
		level = loaded
	}
	w := &World{
		dir:        dir,
		gen:        gen,
		level:      level,
		storage:    DimensionDir(dir, name),
		dim:        shape,
		dimName:    name,
		levelDat:   levelDat,
		chunks:     make(map[ChunkPos]*loadedChunk),
		exec:       newExecutors(),
		clocks:     defaultClocks(),
		saveFormat: defaultSaveFormat,
		dice:       rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	w.worldAge.Store(level.WorldAge)
	w.setEnvironment(registry.EnvDimensionDefaults(shape.typ))
	w.loadForcedChunks()
	w.regions = newRegionCache(func(rx, rz int32) (*regionFile, error) {
		return openRegion(regionFilePath(w.storage, rx, rz), w.saveFormat)
	})
	w.entityRegions = newRegionCache(func(rx, rz int32) (*regionFile, error) {
		return openRegion(entitiesRegionFilePath(w.storage, rx, rz), w.saveFormat)
	})
	if levelDat {
		w.clocks = loadClocks(dir, level)
		w.rules = loadGameRules(dir, level)
		w.refreshFeedbackRules()
		loadWeather(dir, level)
	}
	w.refreshSpawnEnemies()
	w.prepareWeather()
	if g, ok := gen.(seedSource); ok {
		w.settleSeed(g)
	}
	if g, ok := gen.(*OverworldGenerator); ok {
		g.structures.readStart = func(id string, cx, cz int32) nbt.Compound {
			c, err := w.loadChunk(ChunkPos{cx, cz})
			if err != nil {
				slog.Warn("could not read saved structure start", "x", cx, "z", cz, "error", err)
			}
			if c == nil {
				return nil
			}
			structures, _ := nbt.Get[nbt.Compound](c.carriedRoot, "structures")
			starts, _ := nbt.Get[nbt.Compound](structures, "starts")
			start, _ := nbt.Get[nbt.Compound](starts, id)
			return start
		}
	}
	if g, ok := gen.(ProtoGenerator); ok {
		g.OpenProtos(filepath.Join(w.storage, "proto"))
	}
	// A level-less dimension has no level.dat to finish; its creator picks the spawn.
	if overworld, ok := gen.(*OverworldGenerator); ok && levelDat {
		w.initOverworld(overworld)
	}
	if flat, ok := gen.(*FlatGenerator); ok && levelDat && !w.level.existed {
		// A fresh flat world spawns at the first air above its ground, settled
		// at creation like an overworld's.
		w.settleCreation(SpawnPos{X: 0, Y: flat.spawnY(), Z: 0})
	}
	if _, ok := gen.(*DebugGenerator); ok {
		w.SetDebug()
		if levelDat && !w.level.existed {
			w.settleCreation(DebugSpawn)
		}
	}
	return w, nil
}

// DimensionName is this world's dimension wire name, as a player file's
// Dimension field holds it.
func (w *World) DimensionName() string {
	return w.dimName
}

// Existed reports whether the world was on disk at load; a world being created
// picks its spawn.
func (w *World) Existed() bool {
	return w.level.existed
}

// IsFlat is the Is Flat flag of the login and Respawn packets. Ask the
// overworld, whose generator decides it; nether and end worlds answer false.
// vanilla: WorldData.isFlatWorld
func (w *World) IsFlat() bool {
	return isFlatGenerator(w.gen)
}

// IsDebug is the Is Debug flag of the login and Respawn packets. A debug
// world's dimensions all answer true and refuse every block write.
// vanilla: Level.isDebug
func (w *World) IsDebug() bool { return w.debug.Load() }

// SetDebug marks the world as a dimension of a debug world.
func (w *World) SetDebug() { w.debug.Store(true) }

// isFlatGenerator reports whether gen makes a flat world: a FlatGenerator or a
// flat-mode OverworldGenerator.
func isFlatGenerator(gen Generator) bool {
	switch g := gen.(type) {
	case *FlatGenerator:
		return true
	case *OverworldGenerator:
		return g.Flat() != nil
	}
	return false
}

// SeaLevel is the sea level the packets tell the client: the dimension's noise
// settings' level.
// vanilla: ChunkGenerator.getSeaLevel
func (w *World) SeaLevel() int32 {
	return w.gen.SeaLevel()
}

// ChunkLoadedAt is ChunkLoaded by block coordinates, for a spawn check walking
// a pack across a chunk border.
func (w *World) ChunkLoadedAt(x, z int32) bool {
	return w.ChunkLoaded(ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))})
}

// LoadedAt reports whether the block position is within build height and in a
// loaded chunk.
// vanilla: Level.isLoaded, Level.isInValidBounds
func (w *World) LoadedAt(x, y, z int32) bool {
	return w.inBuildHeight(y) && w.ChunkLoadedAt(x, z)
}

// SpawnPoint returns the world spawn and whether it belongs to this dimension,
// which decides whether the natural spawn's keep-clear rule applies here.
// vanilla: NaturalSpawner.isRightDistanceToPlayerAndSpawnPoint
func (w *World) SpawnPoint() (x, y, z int32, guarded bool) {
	owner := w
	if w.spawnOwner != nil {
		owner = w.spawnOwner
	}
	sp, dim := owner.SpawnIn()
	return sp.X, sp.Y, sp.Z, w.dimName == dim
}

// FollowSpawnOf makes SpawnPoint answer from owner, whose level.dat holds the
// spawn.
func (w *World) FollowSpawnOf(owner *World) { w.spawnOwner = owner }

// SpawnDimension is the wire name of the dimension the spawn is in.
func (w *World) SpawnDimension() string {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.spawnDimension()
}

// Spawn returns the block new players spawn at, from level.dat or the flat
// world's origin default.
func (w *World) Spawn() SpawnPos {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.Spawn
}

// ticksPerDay is a day in game ticks (20 per second for 20 minutes). Clock
// totals grow without bound; the client wraps at this for display.
const ticksPerDay = 24000

// timeWorld is the world holding the time: the time source if set (the nether
// and end have no level.dat and use the overworld's), else this world, so no
// accessor reads a stale age.
func (w *World) timeWorld() *World {
	if w.timeSource != nil {
		return w.timeSource
	}
	return w
}

// Time returns the world's age and the overworld clock's total, read by tests
// and the time-of-day readers (daylight, moon phase). The total grows without
// bound; the client wraps at a day.
func (w *World) Time() (worldAge, dayTime int64) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.level.WorldAge, w.clock(OverworldClock).total
}

// SetTimeSource makes this world use overworld's age, clocks and weather; the
// server points every other dimension at it so their changes (a hopper move, a
// furnace slot) carry the tick physics runs on. A world with weather starts
// from the overworld's.
func (w *World) SetTimeSource(overworld *World) {
	w.timeSource = overworld
	overworld.timeMu.Lock()
	overworld.spawnWorlds = append(overworld.spawnWorlds, w)
	if !w.ownRules {
		w.spawnEnemies.Store(overworld.spawnEnemies.Load())
	}
	overworld.timeMu.Unlock()
	w.prepareWeather()
}

// SetOwnLevel gives a plugin dimension its own age, clocks, weather and
// difficulty, which timeWorld then answers with the dimension itself. A world
// without UseOwnRules still reads game rules from overworld and mirrors its
// latched spawn-enemies gate. Lock order: a plugin world's timeMu before the
// overworld's.
func (w *World) SetOwnLevel(overworld *World) {
	w.ruleSource = overworld
	if !w.ownRules {
		overworld.timeMu.Lock()
		overworld.spawnWorlds = append(overworld.spawnWorlds, w)
		w.spawnEnemies.Store(overworld.spawnEnemies.Load())
		overworld.timeMu.Unlock()
	}
}

// ruleOf is ruleBool for a world that may take its rules from ruleSource, with
// w.timeMu held.
func (w *World) ruleOf(id GameRuleIndex) bool {
	if w.ruleSource != nil && !w.ownRules {
		return w.ruleSource.BoolRule(id)
	}
	return w.ruleBool(id)
}

// SetWorldAge sets the age counter without touching the clocks: the counter the
// creature pass's 400-tick gate reads, which spawn tests set to switch it.
func (w *World) SetWorldAge(age int64) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.setWorldAge(age)
}

// The caller holds timeMu.
func (w *World) setWorldAge(age int64) {
	w.level.WorldAge = age
	w.worldAge.Store(age)
}

// Acquire returns the chunk at pos, loading or generating it, and records one
// more holder; every Acquire needs a Release or the chunk never unloads. The
// second result is the chunks whose light the load changed across borders, for
// Update Light to clients that have them.
func (w *World) Acquire(pos ChunkPos) (*Chunk, []ChunkPos) {
	if c := w.holdLoaded(pos); c != nil {
		return c, nil
	}
	c, motion, fresh := w.readyChunk(pos)
	return w.insertChunk(pos, c, motion, fresh)
}

// HoldIfLoaded holds and returns the chunk at pos only if already in memory,
// never loading or generating; it needs a Release.
func (w *World) HoldIfLoaded(pos ChunkPos) *Chunk { return w.holdLoaded(pos) }

// holdLoaded is an acquire's fast path: hold the chunk if loaded, under the
// read lock alone, else nil.
func (w *World) holdLoaded(pos ChunkPos) *Chunk {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.holdLocked(pos)
}

// holdLocked holds the chunk at pos if loaded, else nil. The caller holds w.mu,
// read or write.
func (w *World) holdLocked(pos ChunkPos) *Chunk {
	loaded, ok := w.chunks[pos]
	if !ok {
		return nil
	}
	loaded.users.Add(1)
	return loaded.chunk
}

// readyChunk loads or generates the chunk, lights it alone and primes its
// MOTION_BLOCKING heightmap: the expensive half of an acquire, run without the
// world lock so chunks prepare in parallel without holding readers up.
func (w *World) readyChunk(pos ChunkPos) (*Chunk, *[16][16]int32, bool) {
	c, err := w.loadChunk(pos)
	if err != nil {
		slog.Warn("could not load chunk, generating instead; changes to it will not be saved",
			"x", pos.X, "z", pos.Z, "error", err)
	}
	fresh := c == nil
	if fresh {
		if gate := w.chunkGenerationGate.Load(); gate != nil {
			(*gate)(pos)
		}
		if c = w.fromFixture(pos); c == nil {
			c = newChunk(pos.X, pos.Z)
			w.gen.Generate(c)
		}
		w.encodeChunk(c)
	}
	if !c.lightCorrect {
		w.lightAlone(c)
	}
	return c, w.primeMotion(c), fresh
}

// insertChunk puts a readyChunk chunk in the map held once and lights it across
// its borders, cached light or not (vanilla lights a chunk only with its neighbours loaded); if another acquire inserted the position meanwhile, that chunk
// wins and c is dropped. The second result is Acquire's.
//
// The crossing is computed under the read lock into copies (newLightJoin); the
// write lock is taken only to verify the surroundings are as the join found
// them and install its light with the insert. Surroundings that keep changing
// are joined under the write lock after relightTries attempts.
func (w *World) insertChunk(pos ChunkPos, c *Chunk, motion *[16][16]int32, fresh bool) (*Chunk, []ChunkPos) {
	lock := heldLock{mu: &w.mu}
	defer lock.unlock()
	for range relightTries {
		lock.rlock()
		if held := w.holdLocked(pos); held != nil {
			return held, nil
		}
		j := w.newLightJoin(pos, c)
		lock.unlock()
		if w.joinComputed != nil {
			w.joinComputed(pos)
		}

		lock.lock()
		if held := w.holdLocked(pos); held != nil {
			return held, nil
		}
		if j.current(w) {
			j.install(w)
			w.putChunk(pos, c, motion, fresh)
			return c, j.changed
		}
		lock.unlock()
	}
	lock.lock()
	if held := w.holdLocked(pos); held != nil {
		return held, nil
	}
	// Joined into copies even here: a relight's shadow may be reading the
	// neighbours' arrays with no lock.
	j := w.newLightJoin(pos, c)
	j.install(w)
	w.putChunk(pos, c, motion, fresh)
	return c, j.changed
}

// heldLock tracks the lock a goroutine holds on mu so that a deferred unlock
// releases it on a panic, which the pool recovers: a world lock left held
// would block every later block read and write in the dimension.
type heldLock struct {
	mu    *sync.RWMutex
	read  bool
	write bool
}

func (l *heldLock) rlock() { l.mu.RLock(); l.read = true }

func (l *heldLock) lock() { l.mu.Lock(); l.write = true }

// unlock releases whichever lock is held, if any.
func (l *heldLock) unlock() {
	switch {
	case l.read:
		l.mu.RUnlock()
	case l.write:
		l.mu.Unlock()
	}
	l.read, l.write = false, false
}

// putChunk adds c at pos, held once. The caller holds w.mu for writing and has
// checked pos is free.
func (w *World) putChunk(pos ChunkPos, c *Chunk, motion *[16][16]int32, fresh bool) {
	loaded := &loadedChunk{pos: pos, chunk: c, motion: motion, fresh: fresh}
	// A generated chunk starts unsaved, so it reaches disk on its first unload
	// or save even unchanged.
	// vanilla: LevelChunk(ServerLevel, ProtoChunk, PostLoadProcessor)
	if fresh && !w.dropUntouched {
		loaded.markDirty()
	}
	loaded.users.Add(1)
	w.queueGeneratedTicks(c)
	w.queueLoadedTicks(c)
	w.chunks[pos] = loaded
	touches, comparators := c.redstoneBlockCounts()
	w.touchBlocks += touches
	if comparators != 0 {
		w.comparatorBlocks.Add(int64(comparators))
	}
	w.chunkSet.Add(1)
	w.syncTickingChunk(pos, c)
	// Shared before the slot is stored: from the store on, BlockAt may read
	// the chunk with no lock, so its writes must store atomically.
	c.shared = true
	w.near[nearIndex(pos)].Store(loaded)
	w.appendBlockEntityOrder(c.blockEntities()...)
	if h := w.chunkHooks.Load(); h != nil && h.Load != nil {
		h.Load(pos, c, fresh)
	}
}

// AcquireAsync generates and lights a chunk on a worker, returning a channel
// that receives it. The chunk is in the map before the channel fires, so a
// concurrent Acquire for the same position finds it rather than generating
// twice. Every completed AcquireAsync needs a Release.
func (w *World) AcquireAsync(pos ChunkPos) <-chan *Chunk {
	if c := w.holdLoaded(pos); c != nil {
		ch := make(chan *Chunk, 1)
		ch <- c
		return ch
	}
	done := make(chan *Chunk, 1)
	w.exec.submitChunk(func() {
		w.prepareChunk(pos, done)
	})
	return done
}

// AcquireMany acquires each position like AcquireAsync, delivering the chunks
// in the given order on a channel closed after the last, so a sender fills a
// view distance with one call.
//
// Every position is submitted before returning; only the relaying runs on a
// goroutine. Lazy submission could hand the pool tasks while shutdown waits for
// it to drain, which its WaitGroups forbid. The channel buffers every chunk, so
// a caller that stops reading (a dead connection) does not strand the
// forwarder; each acquired chunk still needs a Release.
func (w *World) AcquireMany(pos []ChunkPos) <-chan *Chunk {
	chans := make([]<-chan *Chunk, len(pos))
	for i, p := range pos {
		chans[i] = w.AcquireAsync(p)
	}
	out := make(chan *Chunk, len(pos))
	go func() {
		defer close(out)
		for _, ch := range chans {
			out <- <-ch
		}
	}()
	return out
}

// prepareChunk is a gen-pool worker's AcquireAsync task: generate or load,
// insert, light. Only the insert takes the write lock (readyChunk,
// insertChunk).
func (w *World) prepareChunk(pos ChunkPos, done chan *Chunk) {
	// Another task or a synchronous Acquire may have loaded the chunk while
	// this was queued; return it.
	if c := w.holdLoaded(pos); c != nil {
		done <- c
		return
	}

	c, motion, fresh := w.readyChunk(pos)
	c, _ = w.insertChunk(pos, c, motion, fresh)
	done <- c
}

// Release gives up a chunk a player no longer has. A chunk with marks its
// region file lacks (fresh ones included) is saved, then unloaded, so it
// survives a walk away and back; one the file already holds is dropped.
//
// The save runs on a goroutine so the caller (the read loop, via forgetChunks
// or unloadChunk) does not block on disk I/O, and the chunk leaves the map only
// after it finishes, so a player walking back mid-save finds it in memory.
func (w *World) Release(pos ChunkPos) { w.release(pos, nil) }

// ReleaseWait is Release plus registering its save, if any, with done, so a
// caller leaving the world waits for just its own writes, not the whole I/O
// backlog.
func (w *World) ReleaseWait(pos ChunkPos, done *sync.WaitGroup) { w.release(pos, done) }

func (w *World) release(pos ChunkPos, done *sync.WaitGroup) {
	if w.dropHold(pos) {
		return
	}
	w.mu.Lock()
	loaded, ok := w.chunks[pos]
	if !ok {
		w.mu.Unlock()
		return
	}
	if loaded.users.Add(-1) > 0 {
		w.mu.Unlock()
		return
	}
	// A near-cache Touch can mark the chunk even while mu is held.
	gen := loaded.dirtyGen.Load()
	if gen == loaded.savedGen.Load() && loaded.dirtyGen.CompareAndSwap(gen, unloadedGen) {
		w.removeChunk(pos, loaded)
		w.mu.Unlock()
		w.forgetChunkPacket(loaded.chunk)
		return
	}
	// gen is the chunk's state at this hold; later marks are unseen by the
	// coming save, which unloadSaved re-saves before the chunk may leave.
	chunk := loaded.chunk
	w.mu.Unlock()
	save := func() {
		w.unloadSaved(pos, chunk, gen)
	}
	w.exec.submitIOTracked(done, save)
}

// dropHold is a release's fast path: under the read lock, drop one hold of a
// loaded chunk if another remains, so only the last release (which may unload)
// takes the write lock. It reports whether the release is done.
func (w *World) dropHold(pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return true
	}
	for {
		n := loaded.users.Load()
		if n <= 1 {
			return false
		}
		if loaded.users.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

// unloadSaved finishes a release whose save read generation gen: the chunk
// leaves only once its region file holds every mark since. A change the save
// missed (a click, or a dispense's give-back, under a block entity's lock)
// bumps dirtyGen and triggers another save instead of dropping it unwritten.
// The re-saves are bounded like saveChunk's restarts, so a never-settling chunk
// stays in memory until a later clean release or the shutdown save.
//
// The write goes through saveLoaded, which serialises it with other saves of
// the chunk in the I/O pool, so an older release's bytes cannot land after a
// newer one's.
func (w *World) unloadSaved(pos ChunkPos, chunk *Chunk, gen uint64) {
	for range saveTries {
		w.mu.RLock()
		l, ok := w.chunks[pos]
		w.mu.RUnlock()
		if !ok || l.chunk != chunk {
			return
		}
		if _, err := w.saveLoaded(pos, l, gen); err != nil {
			slog.Error("could not save chunk, keeping it in memory",
				"x", pos.X, "z", pos.Z, "error", err)
			w.reportSaveFailure()
			return // retried on the next Save (shutdown)
		}
		w.mu.Lock()
		l, ok = w.chunks[pos]
		if !ok || l.chunk != chunk || l.users.Load() > 0 {
			// Not this release's to drop: a player re-acquired it before the
			// save finished (deleting would strand their edits), or it already
			// left another way.
			w.mu.Unlock()
			return
		}
		if l.dirtyGen.CompareAndSwap(gen, unloadedGen) {
			w.removeChunk(pos, l)
			w.mu.Unlock()
			w.forgetChunkPacket(chunk)
			return
		}
		// A newer mark arrived while nobody held the chunk; save it too.
		gen = l.dirtyGen.Load()
		w.mu.Unlock()
	}
}

// removeChunk drops a chunk from the loaded world. The caller holds mu.
func (w *World) removeChunk(pos ChunkPos, loaded *loadedChunk) {
	w.removeChunkBlockEntities(loaded.chunk)
	w.dropChunkTicks(pos)
	w.dropNear(pos, loaded)
	delete(w.chunks, pos)
	touches, comparators := loaded.chunk.redstoneBlockCounts()
	w.touchBlocks -= touches
	if comparators != 0 {
		w.comparatorBlocks.Add(-int64(comparators))
	}
	w.chunkSet.Add(1)
	delete(w.tickingChunks, pos)
	w.chunkUnloaded(pos, loaded.chunk)
}

func (c *Chunk) redstoneBlockCounts() (touches, comparators int) {
	for i := range c.sections {
		touches += int(c.sections[i].touches.Load())
		comparators += int(c.sections[i].comparators)
	}
	return touches, comparators
}

// LoadedChunks is how many chunks are currently in memory and held by players.
func (w *World) LoadedChunks() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.chunks)
}

// ChunkLoaded reports whether the chunk is in memory, which location-bound
// simulation keys on: a mob ticks while its chunk is loaded and is saved with
// it the moment it is not.
func (w *World) ChunkLoaded(pos ChunkPos) bool {
	// Unload clears the slot before removing the chunk from the map, so a
	// matching pointer observes a loaded chunk at the instant of the read.
	if w.nearAt(pos) != nil {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.chunks[pos]
	return ok
}

// FreshlyGenerated reports whether the chunk was generated because no region
// file held it. Starting animals grow only on that first generation, keyed on
// this rather than on an entity entry: a disk chunk without one holds no
// entities, not unpopulated ones.
func (w *World) FreshlyGenerated(pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	return ok && loaded.fresh
}

// MarkDirty flags a loaded chunk for saving though no block changed: the wake
// marks fresh terrain it populated with starting animals, so a reload reads it
// back rather than regenerating and regrowing the herd. An unloaded position is
// ignored.
func (w *World) MarkDirty(pos ChunkPos) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if loaded, ok := w.chunks[pos]; ok {
		loaded.markDirty()
	}
}

// ChunkDirty reports whether a loaded chunk has unsaved marks.
func (w *World) ChunkDirty(pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	return ok && loaded.dirtyGen.Load() > loaded.savedGen.Load()
}

// ChunkSetVersion changes whenever a chunk is loaded or unloaded.
func (w *World) ChunkSetVersion() uint64 { return w.chunkSet.Load() }

// LoadedChunkPositions lists the chunks in memory; a simulation compares passes
// to see arrivals and pairs it with ChunkLoaded for departures.
func (w *World) LoadedChunkPositions() []ChunkPos {
	return w.AppendLoadedChunkPositions([]ChunkPos{})
}

// AppendLoadedChunkPositions appends the current loaded chunk positions to dst.
func (w *World) AppendLoadedChunkPositions(dst []ChunkPos) []ChunkPos {
	w.mu.RLock()
	defer w.mu.RUnlock()
	dst = slices.Grow(dst, len(w.chunks))
	for pos := range w.chunks {
		dst = append(dst, pos)
	}
	return dst
}

// ChunkLightReady reports whether the chunk's light is computed. An
// AcquireAsync chunk is in the map while its light builds on the worker and
// reads zero until then, so callers acting on darkness (the natural spawn) must
// wait for this. A chunk whose light came from its save is ready on insert.
func (w *World) ChunkLightReady(pos ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.loadedAt(pos)
	if !ok {
		return false
	}
	return loaded.chunk.lightCorrect
}

// FlushSaves waits for pending async chunk saves, for a caller that has
// quiesced the world's writers (a test step, a benchmark window). It does not
// hold submissions out, so not beside a live writer: a WaitGroup forbids an Add
// from zero concurrent with Wait. Shutdown, with writers possibly up, uses
// DrainSaves.
func (w *World) FlushSaves() {
	w.exec.waitIO()
	w.logEntityWriteRetries()
}

// DrainSaves waits for pending async saves while holding submissions out, so no
// release Adds beside the Wait; the held-out tasks run after, the pool staying
// open (stop closes it). The main loop's stop calls it once the tick has
// exited, connections possibly still up.
func (w *World) DrainSaves() {
	w.exec.drainIO()
	w.logEntityWriteRetries()
}

// SetBlock changes the block and reports whether it differed. The chunk is
// marked dirty and its wire form only marked stale (rebuilt at its next
// ChunkPacket), so a run of writes costs one re-encode, not one per write.
// Light is not recomputed; call Relight afterwards to schedule it on the worker
// pool.
func (w *World) SetBlock(x, y, z int32, state uint16) bool {
	_, ok := w.SwapBlock(x, y, z, state)
	return ok
}

// inBuildHeight reports whether y is within this dimension's build height and
// the in-memory chunk span.
// vanilla: LevelHeightAccessor.isOutsideBuildHeight
func (w *World) inBuildHeight(y int32) bool {
	return contains(y) && y >= w.dim.minY && y < w.dim.minY+w.dim.height
}

// SwapBlock is SetBlock that also returns the cell's previous state, read
// under the same lock as the write.
// vanilla: Level.setBlock
func (w *World) SwapBlock(x, y, z int32, state uint16) (old uint16, changed bool) {
	if w.IsDebug() || !w.inBuildHeight(y) {
		return 0, false
	}
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return 0, false
	}
	old = loaded.chunk.BlockAt(x&15, y, z&15)
	if old == state {
		return old, false
	}
	loaded.chunk.SetBlock(x&15, y, z&15, state)
	w.updateRedstoneBlockCounts(old, state)
	if w.journaling {
		w.journal = append(w.journal, journalEntry{x, y, z, old, state})
	}
	if be := loaded.chunk.blockEntity(x, y, z); be != nil {
		be.Wake()
	}
	if old != airState && shulkerBoxBlocks[registry.BlockOf(old)] {
		w.shulkerLidsOpen.Delete([3]int32{x, y, z})
	}
	w.updateMotion(loaded, x&15, z&15)
	w.updateSurface(loaded, x&15, y, z&15, state)
	w.afterWrite(loaded)
	return old, true
}

// journalEntry is one write recorded while a journal runs.
type journalEntry struct {
	x, y, z  int32
	old, new uint16
}

// JournalStart records every SwapBlock write until JournalStop, so a rule that
// writes before its results are known can be undone. Not reentrant; a write
// from another goroutine in between is recorded too, which JournalStop's caller
// must tolerate by reverting only cells still holding the recorded state.
func (w *World) JournalStart() {
	w.mu.Lock()
	w.journaling, w.journal = true, w.journal[:0]
	w.mu.Unlock()
}

// JournalStop ends the journal and returns the writes in order: each cell with
// the state it had and the one it was given.
func (w *World) JournalStop() (cells [][3]int32, olds, news []uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.journaling = false
	for _, e := range w.journal {
		cells = append(cells, [3]int32{e.x, e.y, e.z})
		olds = append(olds, e.old)
		news = append(news, e.new)
	}
	w.journal = w.journal[:0]
	return cells, olds, news
}

// afterWrite is the per-chunk bookkeeping a block write owes, shared by
// SetBlock and SetBlocks; the caller holds w.mu.
func (w *World) afterWrite(loaded *loadedChunk) {
	loaded.chunk.stale = true
	loaded.chunk.writes++
	loaded.containerGen.Add(1)
	loaded.markDirty()
}

// updateMotion refreshes the written column's MOTION_BLOCKING height so the
// rain needs no rescan. The caller holds w.mu.
func (w *World) updateMotion(loaded *loadedChunk, lx, lz int32) {
	if loaded.motion != nil {
		loaded.motion[lx][lz] = w.motionHeight(loaded.chunk, lx, lz)
	}
}

// updateSurface keeps the written column's surface true: a block above raises
// it, air over the surface walks down to the next block. The caller holds w.mu.
func (w *World) updateSurface(loaded *loadedChunk, lx, y, lz int32, state uint16) {
	s := loaded.surface.Load()
	if s == nil || !contains(y) {
		return // nothing cached, or a write the chunk ignored
	}
	h := &s[lx][lz]
	cur := atomic.LoadInt32(h)
	switch {
	case state != airState && y > cur:
		atomic.StoreInt32(h, y)
	case state == airState && y == cur:
		// One store of the final height: a lock-free reader never sees the
		// column empty on the way down.
		next := w.dim.minY - 1
		for y--; y >= worldMinY; y-- {
			if loaded.chunk.BlockAt(lx, y, lz) != airState {
				next = y
				break
			}
		}
		atomic.StoreInt32(h, next)
	}
}

// SetBlocks is SetBlock batched for writes arriving by the hundred (an
// explosion's crater): one write-lock hold and one stale and dirty mark per
// chunk. It returns, in batch order, each write that took, with the cell's
// previous state.
func (w *World) SetBlocks(changes []BlockChange) []BlockChange {
	if len(changes) == 0 || w.IsDebug() {
		return nil
	}
	var applied []BlockChange
	var touchedBuf [16]*loadedChunk
	touched := touchedBuf[:0]
	var seen map[*loadedChunk]struct{}
	var lastPos ChunkPos
	var lastLoaded *loadedChunk
	var lastTouched bool
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, ch := range changes {
		if !w.inBuildHeight(ch.Y) {
			continue
		}
		pos := ChunkPos{X: ch.X >> 4, Z: ch.Z >> 4}
		var loaded *loadedChunk
		if lastLoaded != nil && pos == lastPos {
			loaded = lastLoaded
		} else {
			var ok bool
			loaded, ok = w.chunks[pos]
			if !ok {
				continue
			}
			lastPos = pos
			lastLoaded = loaded
			lastTouched = false
		}
		lx, lz := ch.X&15, ch.Z&15
		old := loaded.chunk.BlockAt(lx, ch.Y, lz)
		if old == ch.State {
			continue
		}
		if applied == nil {
			applied = make([]BlockChange, 0, len(changes)-i)
		}
		applied = append(applied, BlockChange{X: ch.X, Y: ch.Y, Z: ch.Z, State: old})
		if !lastTouched {
			if seen != nil {
				if _, ok := seen[loaded]; !ok {
					seen[loaded] = struct{}{}
					touched = append(touched, loaded)
				}
			} else if !slices.Contains(touched, loaded) {
				if len(touched) == len(touchedBuf) {
					seen = make(map[*loadedChunk]struct{}, len(touched)*2)
					for _, t := range touched {
						seen[t] = struct{}{}
					}
					seen[loaded] = struct{}{}
				}
				touched = append(touched, loaded)
			}
			lastTouched = true
		}
		loaded.chunk.SetBlock(lx, ch.Y, lz, ch.State)
		w.updateRedstoneBlockCounts(old, ch.State)
		if be := loaded.chunk.blockEntity(ch.X, ch.Y, ch.Z); be != nil {
			be.Wake()
		}
		if old != airState && shulkerBoxBlocks[registry.BlockOf(old)] {
			w.shulkerLidsOpen.Delete([3]int32{ch.X, ch.Y, ch.Z})
		}
		w.updateMotion(loaded, lx, lz)
		w.updateSurface(loaded, lx, ch.Y, lz, ch.State)
	}
	for _, loaded := range touched {
		w.afterWrite(loaded)
	}
	return applied
}

func (w *World) updateRedstoneBlockCounts(old, state uint16) {
	oldBlock, block := registry.BlockOf(old), registry.BlockOf(state)
	if oldBlock == block {
		return
	}
	before, after := isTouchBlock[oldBlock], isTouchBlock[block]
	switch {
	case after && !before:
		w.touchBlocks++
	case before && !after:
		w.touchBlocks--
	}
	switch {
	case block == registry.BlockComparator:
		w.comparatorBlocks.Add(1)
	case oldBlock == registry.BlockComparator:
		w.comparatorBlocks.Add(-1)
	}
}

// Touch marks the block's chunk dirty without changing a block, vanilla's
// setChanged for a block entity whose contents moved, so a chest's, furnace's
// or hopper's contents reach disk at the next save. The position also waits for
// the next physics tick to wake its comparators.
func (w *World) Touch(x, y, z int32) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	// A comparator added after the touch reads the container on placement.
	if w.comparatorBlocks.Load() == 0 {
		if loaded := w.nearAt(pos); loaded != nil && loaded.markDirty() {
			return
		}
		w.mu.RLock()
		defer w.mu.RUnlock()
		if loaded := w.chunks[pos]; loaded != nil {
			loaded.markDirty()
		}
		return
	}
	tick := w.timeWorld().worldAge.Load()
	w.mu.Lock()
	defer w.mu.Unlock()
	if loaded, ok := w.loadedAt(pos); ok {
		loaded.markDirty()
		if w.comparatorBlocks.Load() != 0 {
			w.containerChanges = append(w.containerChanges, containerChange{pos: posKey{x, y, z}, tick: tick})
		}
	}
}

// packPos packs a position into one word, a cheaper map key than the triple:
// x and z in 26 bits, y in 12. vanilla: BlockPos.asLong
func packPos(p posKey) uint64 {
	return uint64(p[0]&0x3FFFFFF)<<38 | uint64(p[2]&0x3FFFFFF)<<12 | uint64(p[1]&0xFFF)
}

// takeContainerChanges drains the changes for loaded comparators, keeping the
// earliest tick for each container as the start of its comparator delay.
func (w *World) takeContainerChanges() []containerChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	changes := w.containerChanges
	w.containerChanges, w.containerChangesSpare = w.containerChangesSpare[:0], changes
	// All comparators may have unloaded since the touch.
	if w.comparatorBlocks.Load() == 0 {
		return nil
	}
	if len(changes) < 2 {
		return changes
	}
	seen := w.containerChangeSeen
	if seen == nil {
		seen = make(map[uint64]int, len(changes))
		w.containerChangeSeen = seen
	}
	clear(seen)
	unique := changes[:0]
	for _, c := range changes {
		key := packPos(c.pos)
		if i, ok := seen[key]; ok {
			if c.tick < unique[i].tick {
				unique[i].tick = c.tick
			}
			continue
		}
		seen[key] = len(unique)
		unique = append(unique, c)
	}
	return unique
}

// takePendingGenerated returns and clears the generated chunks' scheduled
// fluid sources.
func (w *World) takePendingGenerated() []posKey {
	w.mu.Lock()
	defer w.mu.Unlock()
	ticks := w.pendingGenerated
	w.pendingGenerated = nil
	return ticks
}

// queueGeneratedTicks moves a fresh chunk's scheduled fluid ticks to the
// world's queue in world coordinates. The caller holds w.mu.
func (w *World) queueGeneratedTicks(c *Chunk) {
	for _, p := range c.pendingGenerated {
		w.pendingGenerated = append(w.pendingGenerated, posKey{c.x*sectionSize + p[0], p[1], c.z*sectionSize + p[2]})
	}
}

// Relight recomputes light within lightRadius of the coordinates and returns
// the light sections that changed; run it on the worker pool after
// SetBlock so the read loop is not blocked.
func (w *World) Relight(x, z int32) []LightChange {
	return w.relightArea(x, z, x, z)
}

// LightMarkedIn reports whether any column of the box widened by the light
// radius carries a light mark, the box relightArea tests first.
func (w *World) LightMarkedIn(minX, minZ, maxX, maxZ int32) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lightDirtyIn(addClamped(minX, -lightRadius), addClamped(minZ, -lightRadius),
		addClamped(maxX, lightRadius), addClamped(maxZ, lightRadius), false)
}

// relightArea recomputes light in the column box widened by the light radius
// (covering each column's own relight box, larger where they scatter) and
// returns the light sections that changed.
//
// A box with no light mark (Chunk.lightDirty) is skipped as already at the
// fixpoint; marks are sought over the whole wiped box, so an unrelit write
// still forces the next relight reaching it.
//
// When every mark's cells are recorded (Chunk.lightCells), it relights from
// those cells (relightCells) and clears the whole box's marks; otherwise it
// wipes and refills (relightBox) and clears marks only on the requested
// columns, whose whole radius the wipe covers.
//
// It computes on a shadow of the chunks reached (box widened by twice the
// radius) with no lock: a relight takes tens of milliseconds, and under the
// write lock every block read of the tick would wait. The shadow is taken
// under the read lock and installed under the write lock if none of those
// chunks changed; a region that keeps changing is relit on a shadow under the
// write lock after relightTries tries, so a live light array is never written
// in place while another relight's shadow may read it.
func (w *World) relightArea(minX, minZ, maxX, maxZ int32) []LightChange {
	wipeMinX, wipeMinZ := addClamped(minX, -lightRadius), addClamped(minZ, -lightRadius)
	wipeMaxX, wipeMaxZ := addClamped(maxX, lightRadius), addClamped(maxZ, lightRadius)
	lock := heldLock{mu: &w.mu}
	defer lock.unlock()
	for try := 0; ; try++ {
		locked := try == relightTries
		if locked {
			lock.lock()
		} else {
			lock.rlock()
		}
		if !w.lightDirtyIn(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ, false) {
			return nil
		}
		cells, incremental := w.dirtyCells(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ)
		var shadow *lightShadow
		if incremental {
			// A cell's light reaches lightRadius blocks, and the cell beyond is
			// read to confirm it is unchanged. The wiped box is stamped too:
			// its marks are cleared on install, so a write anywhere in it must
			// fail the currency check.
			cMinX, cMinZ, cMaxX, cMaxZ := cellsBox(cells)
			shadow = w.newLightShadow(
				min(addClamped(cMinX, -lightRadius-1), wipeMinX), min(addClamped(cMinZ, -lightRadius-1), wipeMinZ),
				max(addClamped(cMaxX, lightRadius+1), wipeMaxX), max(addClamped(cMaxZ, lightRadius+1), wipeMaxZ))
		} else {
			shadow = w.newLightShadow(addClamped(wipeMinX, -lightRadius), addClamped(wipeMinZ, -lightRadius),
				addClamped(wipeMaxX, lightRadius), addClamped(wipeMaxZ, lightRadius))
		}
		if !locked {
			lock.unlock()
		}

		if incremental {
			shadow.w.relightCells(cells)
		} else {
			shadow.w.relightBox(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ)
		}
		var changed []LightChange
		if !locked {
			if w.relightComputed != nil {
				w.relightComputed()
			}
			lock.lock()
		}
		installed := locked || shadow.current(w)
		if installed {
			if incremental {
				w.lightDirtyIn(wipeMinX, wipeMinZ, wipeMaxX, wipeMaxZ, true)
			} else {
				w.lightDirtyIn(minX, minZ, maxX, maxZ, true)
			}
			changed = shadow.install()
		}
		lock.unlock()
		shadow.release(installed)
		if installed {
			return changed
		}
	}
}

// lightDirtyIn reports any light-marked column in the box, clearing the box's
// marks if clear. The caller holds w.mu.
func (w *World) lightDirtyIn(minX, minZ, maxX, maxZ int32, clear bool) bool {
	dirty := false
	for cx := floorDiv(minX, sectionSize); cx <= floorDiv(maxX, sectionSize); cx++ {
		for cz := floorDiv(minZ, sectionSize); cz <= floorDiv(maxZ, sectionSize); cz++ {
			loaded, ok := w.chunks[ChunkPos{cx, cz}]
			if !ok {
				continue
			}
			c := loaded.chunk
			lx0, lx1 := max(minX-cx*16, 0), min(maxX-cx*16, 15)
			lz0, lz1 := max(minZ-cz*16, 0), min(maxZ-cz*16, 15)
			mask := uint16(0xFFFF<<lz0) & uint16(0xFFFF>>(15-lz1)) // bits lz0..lz1
			cleared := false
			for lx := lx0; lx <= lx1; lx++ {
				if c.lightDirty[lx]&mask == 0 {
					continue
				}
				if !clear {
					return true
				}
				dirty, cleared = true, true
				c.lightDirty[lx] &^= mask
			}
			if cleared {
				c.dropLightCells(lx0, lx1, lz0, lz1)
			}
		}
	}
	return dirty
}

// RelightAsync queues a light recompute (RelightAreaAsync) and returns a
// channel receiving the changed light sections, for Update Light when it fires.
func (w *World) RelightAsync(x, z int32) <-chan []LightChange {
	return w.RelightAreaAsync(x, z, x, z)
}

// BlockAt is the block at the coordinates, air if the chunk is not loaded.
//
// A near-cache hit takes no lock: the slot, the section's array and its word
// are loaded atomically and written under mu, so the answer is the cell as it
// stood at one instant of the call. A SetBlocks batch landing meanwhile may
// show some of its writes and not others, as it may between two reads, and a
// chunk unloaded after its slot was loaded reads as it left, its blocks no
// longer written.
func (w *World) BlockAt(x, y, z int32) uint16 {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	if loaded := w.nearAt(pos); loaded != nil {
		return loaded.chunk.sharedBlockAt(x&15, y, z&15)
	}
	w.mu.RLock()
	loaded, ok := w.chunks[pos]
	if !ok {
		w.mu.RUnlock()
		return airState
	}
	// Two chunks sharing a slot (more than nearSlots chunks apart on an axis)
	// take it from each other on a miss, so a run of reads in one chunk hits.
	if slot := &w.near[nearIndex(pos)]; slot.Load() != loaded {
		slot.Store(loaded)
	}
	state := loaded.chunk.BlockAt(x&15, y, z&15)
	w.mu.RUnlock()
	return state
}

// blockReader reads blocks under one hold of the read lock with a last-chunk
// cache. The hold keeps every chunk in w.chunks (unload and replace take the
// write lock), so the cache cannot go stale; nothing under a reader may write
// the world or take the read lock again (a queued writer would deadlock the
// nested RLock).
// vanilla: ServerChunkCache.getChunk lastChunkPos/lastChunk
type blockReader struct {
	w      *World
	pos    ChunkPos
	loaded *loadedChunk
}

// readBlocks takes the read lock; the caller must call done.
func (w *World) readBlocks() blockReader {
	w.mu.RLock()
	return blockReader{w: w}
}

func (r *blockReader) done() { r.w.mu.RUnlock() }

// at is World.BlockAt without its own lock.
func (r *blockReader) at(x, y, z int32) uint16 {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	if r.loaded == nil || r.pos != pos {
		loaded, ok := r.w.loadedAt(pos)
		if !ok {
			return airState
		}
		r.pos, r.loaded = pos, loaded
	}
	return r.loaded.chunk.BlockAt(x&15, y, z&15)
}

// conductor is World.ConductorAt without its own block-state lock.
func (r *blockReader) conductor(x, y, z int32) bool {
	return r.w.conductorState(x, y, z, r.at(x, y, z))
}

// BlocksIn appends to buf[:0] every cell's state from (x0, y0, z0) to (x1, y1,
// z1) inclusive, x outermost then y then z, air where unloaded: BlockAt over
// the box under one read-lock hold, resolving each chunk and section once.
func (w *World) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	buf = buf[:0]
	if x1 < x0 || y1 < y0 || z1 < z0 {
		return buf
	}
	ny, nz := int(y1-y0+1), int(z1-z0+1)
	buf = slices.Grow(buf, int(x1-x0+1)*ny*nz)[:int(x1-x0+1)*ny*nz]
	w.mu.RLock()
	defer w.mu.RUnlock()
	for cx := x0 >> 4; cx <= x1>>4; cx++ {
		loX, hiX := max(x0, cx<<4), min(x1, cx<<4|15)
		for cz := z0 >> 4; cz <= z1>>4; cz++ {
			loZ, hiZ := max(z0, cz<<4), min(z1, cz<<4|15)
			width := int(hiZ - loZ + 1)
			loaded, _ := w.loadedAt(ChunkPos{X: cx, Z: cz})
			// Count y by offset, so a box ending at math.MaxInt32 stops
			// instead of wrapping when it advances to the next section.
			for dy := 0; dy < ny; {
				y := y0 + int32(dy)
				height := min(16-int(y&15), ny-dy)
				fill := uint16(airState)
				var words *blockWords
				if ly := uint32(y - worldMinY); loaded != nil && ly < worldHeight {
					s := &loaded.chunk.sections[ly>>4]
					fill, words = s.fill, s.blocks.Load()
				}
				for dx := range int(hiX - loX + 1) {
					column := ((int(loX-x0)+dx)*ny+dy)*nz + int(loZ-z0)
					if words == nil {
						for range height {
							row := buf[column : column+width]
							for z := range row {
								row[z] = fill
							}
							column += nz
						}
					} else {
						cell := int(y&15<<8|(loZ&15)<<4|loX&15) + dx
						for range height {
							row := buf[column : column+width]
							for z := range row {
								row[z] = words.get(cell + z*16)
							}
							column += nz
							cell += 256
						}
					}
				}
				dy += height
			}
		}
	}
	return buf
}

// BiomeAt is the wire name of the biome Biome reads, or "" where nothing can be
// read.
func (w *World) BiomeAt(x, y, z int32) string {
	return w.Biome(x, y, z).Name()
}

// Biome reads the zoom-selected stored quart, then the generator, else NoBiome.
// vanilla: BiomeManager.getBiome, LevelReader.getNoiseBiome
func (w *World) Biome(x, y, z int32) registry.Biome { return w.biome(x, y, z, nil) }

func (w *World) biome(x, y, z int32, origin *loadedChunk) registry.Biome {
	// vanilla: ChunkAccess.getNoiseBiome clamps quarts to the dimension.
	minQY, maxQY := w.MinY()>>2, (w.MinY()+w.Height()-1)>>2
	if b, ok := w.loadedZoomBiome(x, y, z, minQY, maxQY, origin); ok {
		return b
	}
	qx, qy, qz := levelgen.ZoomedQuart(w.timeWorld().zoomSeed, x, y, z)
	qy = min(max(qy, minQY), maxQY)
	if b := w.NoiseBiome(qx*4, qy*4, qz*4); b != registry.NoBiome {
		return b
	}
	sampler, ok := w.gen.(BiomeSampler)
	if !ok {
		return registry.NoBiome
	}
	b, _ := registry.BiomeByName(sampler.NewBiomeProbe()(qx, qy, qz))
	return b
}

// loadedZoomBiome selects one quart, preserving uniform and NoBiome source rules.
// vanilla: BiomeManager.getBiome
func (w *World) loadedZoomBiome(x, y, z, minQY, maxQY int32, origin *loadedChunk) (registry.Biome, bool) {
	px, py, pz := levelgen.ZoomParent(x, y, z)
	low, high := min(max(py, minQY), maxQY), min(max(py+1, minQY), maxQY)
	var grid *biomeSnapshot
	if px>>2 == (px+1)>>2 && pz>>2 == (pz+1)>>2 && low>>2 == high>>2 {
		loaded := w.spawnBiomeChunk(ChunkPos{X: px >> 2, Z: pz >> 2}, origin)
		if loaded == nil {
			return registry.NoBiome, false
		}
		grid = w.rawBiomeGrid(&loaded.chunk.sections[(low*4-worldMinY)/16])
		if grid.cells == nil {
			return registry.Biome(grid.uniform), true
		}
		if b, ok := grid.cornersAgree(px&3, low, pz&3, high, nil); ok {
			return registry.Biome(b), true
		}
	}
	qx, qy, qz := levelgen.ZoomedQuart(w.timeWorld().zoomSeed, x, y, z)
	qy = min(max(qy, minQY), maxQY)
	if grid == nil {
		loaded := w.spawnBiomeChunk(ChunkPos{X: qx >> 2, Z: qz >> 2}, origin)
		if loaded == nil {
			return registry.NoBiome, false
		}
		grid = w.rawBiomeGrid(&loaded.chunk.sections[(qy*4-worldMinY)/16])
	}
	b := registry.Biome(grid.at(qx&3, qy, qz&3))
	if b == registry.NoBiome {
		for i := range 4 {
			if w.spawnBiomeChunk(ChunkPos{X: (px + int32(i>>1)) >> 2, Z: (pz + int32(i&1)) >> 2}, origin) == nil {
				return b, false
			}
		}
	}
	return b, true
}

func (w *World) spawnBiomeChunk(pos ChunkPos, origin *loadedChunk) *loadedChunk {
	if origin != nil && pos == origin.pos {
		return origin
	}
	return w.biomeChunk(pos)
}

// NoiseBiome is the biome of the block's own 4x4x4 cell, or NoBiome where the
// chunk is not loaded.
// vanilla: BiomeManager.getNoiseBiomeAtPosition
func (w *World) NoiseBiome(x, y, z int32) registry.Biome {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return registry.NoBiome
	}
	return registry.Biome(loaded.chunk.BiomeAt(x&15, y, z&15))
}

// FortressAt reports whether the block is in a nether fortress's bounds and in
// one of its pieces; a world without a nether generator has none.
func (w *World) FortressAt(x, y, z int32) (inBounds, inPiece bool) {
	if g, ok := w.gen.(interface {
		FortressAt(x, y, z int32) (bool, bool)
	}); ok {
		return g.FortressAt(x, y, z)
	}
	return false, false
}

// InhabitedTime is the ticks players spent in the chunk holding the block, the
// local share of a spawn's difficulty; zero if not loaded.
// vanilla: ServerLevel.getCurrentDifficultyAt
func (w *World) InhabitedTime(x, z int32) int64 {
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return 0
	}
	return loaded.chunk.inhabitedTime
}

// InhabitTime ages each named chunk (those near a player) by one tick; it marks
// nothing dirty and reaches disk with the next save of the chunk.
// vanilla: ServerChunkCache.tickSpawningChunk, ChunkAccess.incrementInhabitedTime
func (w *World) InhabitTime(pos []ChunkPos) {
	if len(pos) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range pos {
		if loaded, ok := w.chunks[p]; ok {
			loaded.chunk.inhabitedTime++
		}
	}
}

// SetDropUntouchedChunks sets whether chunks generated from now are discarded
// on unload when unchanged rather than saved once. Test harnesses enable it:
// their worlds are fresh, and writing every chunk cost much of the tests' CPU.
func (w *World) SetDropUntouchedChunks(drop bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropUntouched = drop
}

// SurfaceHeight is the WORLD_SURFACE height of the column (its highest non-air
// block), or one below the bottom for an empty column or an unloaded chunk,
// neither offering a spawn. The monster spawn rolls its height under it. It is
// computed per chunk on first ask with one hold rather than a lock per block,
// and a write keeps its column true (updateSurface), so a neighbourhood pass
// does not rescan every column each tick.
func (w *World) SurfaceHeight(x, z int32) int32 {
	// Empty or unloaded columns answer one below the dimension's floor, so the
	// spawn's height roll lands on the floor itself, no place for a monster,
	// matching the vanilla heightmap's empty answer.
	empty := w.dim.minY - 1
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	lx, lz := x&15, z&15
	// A near slot only ever holds a loaded chunk, as BlockAt relies on.
	if l := w.nearAt(pos); l != nil {
		if s := l.surface.Load(); s != nil {
			return atomic.LoadInt32(&s[lx][lz])
		}
	}

	// The scan reads the whole chunk, so it runs under the read lock and the
	// write lock only stores it, kept if no write landed in between; a chunk
	// that keeps changing is scanned under the write lock after surfaceTries
	// scans.
	for range surfaceTries {
		w.mu.RLock()
		loaded, ok := w.chunks[pos]
		if !ok {
			w.mu.RUnlock()
			return empty
		}
		if s := loaded.surface.Load(); s != nil {
			h := atomic.LoadInt32(&s[lx][lz])
			w.mu.RUnlock()
			return h
		}
		writes := loaded.chunk.writes
		surface := scanSurface(loaded.chunk, empty)
		w.mu.RUnlock()

		w.mu.Lock()
		if cur, ok := w.chunks[pos]; ok && cur == loaded && loaded.surface.Load() == nil && loaded.chunk.writes == writes {
			loaded.surface.Store(surface)
		}
		w.mu.Unlock()
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return empty
	}
	s := loaded.surface.Load()
	if s == nil {
		s = scanSurface(loaded.chunk, empty)
		loaded.surface.Store(s)
	}
	return atomic.LoadInt32(&s[lx][lz])
}

// surfaceTries bounds SurfaceHeight's scans under the read lock.
const surfaceTries = 3

// scanSurface computes each column's highest non-air y, or empty, skipping
// all-air sections.
func scanSurface(c *Chunk, empty int32) *[16][16]int32 {
	surface := new([16][16]int32)
	var found [16][16]bool
	for lx := range surface {
		for lz := range surface[lx] {
			surface[lx][lz] = empty
		}
	}
	left := 16 * 16
	for i := len(c.sections) - 1; i >= 0 && left > 0; i-- {
		if c.sections[i].isUniform(airState) {
			continue
		}
		bottom := worldMinY + int32(i)*sectionSize
		for lx := range int32(16) {
			for lz := range int32(16) {
				if found[lx][lz] {
					continue
				}
				for y := bottom + sectionSize - 1; y >= bottom; y-- {
					if c.BlockAt(lx, y, lz) != airState {
						surface[lx][lz], found[lx][lz] = y, true
						left--
						break
					}
				}
			}
		}
	}
	return surface
}

// Save writes every dirty loaded chunk, plus the overworld's level.dat (age),
// data/minecraft/world_clocks.dat and weather.dat, the files vanilla uses; the
// nether and end own no level.dat, so for them it is only chunks. Shutdown
// calls it so changes are not lost and vanilla can open the world, and
// auto-save so a crash loses at most an interval, clock state included.
//
// The age goes back into the raw compound read from level.dat, so the rest
// (seed, game type, border) is carried; a world still keeping game rules in
// level.dat has them written back. A world without level.dat gets this server's
// defaults.
func (w *World) Save() {
	w.logEntityWriteRetries()
	if w.levelDat {
		// Write the age and difficulty into the raw compound first, so
		// level.dat carries them current with everything else untouched.
		if compound := w.syncLevelData(); compound == nil {
			slog.Error("could not save level.dat", "error", "level data has no compound to write")
		} else if err := writeLevelCompound(w.dir, compound); err != nil {
			slog.Error("could not save level.dat", "error", err)
		} else {
			w.timeMu.Lock()
			w.level.existed = true
			w.timeMu.Unlock()
		}

		if err := w.saveGameRules(); err != nil {
			slog.Error("could not save game_rules.dat", "error", err)
		}
		if err := w.saveClocks(); err != nil {
			slog.Error("could not save world_clocks.dat", "error", err)
		}
		if err := w.saveWeather(); err != nil {
			slog.Error("could not save weather.dat", "error", err)
		}
		if err := w.ensureWorldGen(); err != nil {
			slog.Error("could not save world_gen_settings.dat", "error", err)
		}
	}

	if w.ownRules {
		if err := w.saveGameRules(); err != nil {
			slog.Error("could not save game_rules.dat", "dimension", w.dimName, "error", err)
		}
	}

	if w.ruleSource != nil {
		w.SavePluginMeta()
	}
	w.saveForcedChunks()

	w.mu.Lock()
	type saving struct {
		loaded *loadedChunk
		gen    uint64
	}
	dirty := make(map[ChunkPos]saving, len(w.chunks))
	for pos, loaded := range w.chunks {
		if gen := loaded.dirtyGen.Load(); gen > loaded.savedGen.Load() {
			dirty[pos] = saving{loaded, gen}
		}
	}
	w.mu.Unlock()

	saved := 0
	for pos, pending := range dirty {
		// saveLoaded shares the chunk's save lock and generation with unload
		// saves, so an autosave cannot write an older generation over one an
		// unload save is writing or has written.
		written, err := w.saveLoaded(pos, pending.loaded, pending.gen)
		if err != nil {
			slog.Error("could not save chunk", "x", pos.X, "z", pos.Z, "error", err)
			w.reportSaveFailure()
			continue
		}
		if written {
			saved++
		}
	}
	if saved > 0 {
		slog.Info("saved chunks", "chunks", saved)
	}
}

// Close drains queued saves, retries pending entity writes and releases the
// open region files, returning write, sync or close errors. Safe after a failed NewWorld.
func (w *World) Close() error {
	if w.exec != nil {
		w.exec.stop()
	}
	entityErr := w.retryEntityWrites()
	if g, ok := w.gen.(ProtoGenerator); ok {
		g.FlushProtos()
	}
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	err := errors.Join(entityErr, w.regions.close(), w.entityRegions.close())
	w.regions = nil
	w.entityRegions = nil
	return err
}

// RegionFileCounts is the open region handles, chunks' and entities' apart,
// which tests watch to check the caches stay bounded.
func (w *World) RegionFileCounts() (chunks, entities int) {
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	return w.regions.len(), w.entityRegions.len()
}

// loadChunk reads a chunk from its region file, (nil, nil) when absent so the
// caller generates. A missing region file is not created, so a walk across an
// empty world leaves no empty .mca files. A chunk stored under other
// coordinates is relocated to the slot read.
// vanilla: SerializableChunkData.read
func (w *World) loadChunk(pos ChunkPos) (*Chunk, error) {
	rx, rz := regionCoords(pos)
	path := regionFilePath(w.storage, rx, rz)
	if _, err := os.Stat(path); err != nil {
		return nil, nil // no region file, no chunk to load
	}
	rf, err := w.region(rx, rz)
	if err != nil {
		return nil, err
	}
	defer w.releaseRegion(rf)
	index := regionIndex(pos.X, pos.Z)
	data, err := rf.readChunk(pos)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil // chunk not in this region
	}
	root, err := decodeRoot(data)
	var chunk *Chunk
	if err == nil {
		relocateChunkRoot(root, pos)
		chunk, err = decodeChunkNBT(root)
	}
	if err != nil {
		rf.markUnreadable(index)
		return nil, fmt.Errorf("chunk (%d,%d): %w", pos.X, pos.Z, err)
	}
	// Encode for this dimension: the decode built the full-height form, which
	// nether and end clients would read past the end of their shorter chunks.
	if chunk != nil {
		w.encodeChunk(chunk)
	}
	return chunk, nil
}

// decodeRoot parses a chunk's region NBT, whose root must be a compound.
func decodeRoot(data []byte) (nbt.Compound, error) {
	_, tag, err := nbt.DecodeOrderedFields(data, "block_entities", "Entities")
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	root, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, fmt.Errorf("root is a %s, want a compound", tag.Type())
	}
	return root, nil
}

// saveBuffers holds the encode buffers of saveChunk; saves run on several
// goroutines, so each takes its own.
var saveBuffers = sync.Pool{New: func() any { return new([]byte) }}

// saveTries bounds saveChunk's restarts: a chunk whose block entities churn
// cannot spin the save, and one still disagreeing is left dirty for the next
// save.
const saveTries = 8

// saveLoaded writes the chunk for dirty generation gen unless gen or newer is
// already written. Holding the chunk's save lock across encode and write
// serialises racing unload saves, so an older one cannot land last with stale
// bytes. It reports whether it wrote.
func (w *World) saveLoaded(pos ChunkPos, l *loadedChunk, gen uint64) (bool, error) {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	if gen <= l.savedGen.Load() {
		return false, nil
	}
	if l.fresh {
		if err := w.saveGeneratedEntities(pos, l.chunk); err != nil {
			return false, err
		}
	}
	written, err := w.saveChunk(pos, l.chunk)
	if err != nil {
		return written, err
	}
	l.savedGen.Store(gen)
	return written, nil
}

// TakeGeneratedEntities transfers a loaded chunk's starting entities to its
// wake owner, which must carry or save them from then on.
func (w *World) TakeGeneratedEntities(pos ChunkPos) []nbt.Compound {
	w.mu.Lock()
	loaded := w.chunks[pos]
	if loaded != nil && len(loaded.chunk.generatedEntities) > 0 {
		tags := loaded.chunk.generatedEntities
		loaded.chunk.generatedEntities = nil
		w.mu.Unlock()
		return tags
	}
	w.mu.Unlock()
	if eg, ok := w.gen.(EntityGenerator); ok {
		return eg.GeneratedEntities(pos)
	}
	return nil
}

// saveGeneratedEntities writes the generator's entities for a chunk saved
// before the mob pass woke it, since a saved chunk is no longer fresh and
// nothing would place them again. A chunk with an entity entry is left to
// whoever wrote it.
func (w *World) saveGeneratedEntities(pos ChunkPos, c *Chunk) error {
	w.mu.RLock()
	generated := c.generatedEntities
	w.mu.RUnlock()
	if len(generated) == 0 {
		if eg, ok := w.gen.(EntityGenerator); ok {
			generated = eg.GeneratedEntities(pos)
		}
	}
	if len(generated) == 0 {
		return nil
	}
	if _, has, err := w.LoadChunkEntities(pos); err != nil {
		return err
	} else if !has {
		tags := make([]nbt.Tag, len(generated))
		for i, c := range generated {
			tags[i] = c
		}
		if err := w.SaveChunkEntities(pos, tags); err != nil {
			return err
		}
	}
	w.mu.Lock()
	c.generatedEntities = nil
	w.mu.Unlock()
	return nil
}

// saveChunk encodes the chunk and writes it in the dimension's sections (the
// nether and end store only their band, numbered from their own floors).
//
// Block entities are read in two phases because of the lock order (entity
// before world; see mu): pointers under the read lock, compounds under their
// own locks with the world lock down, since a tick holding an entity's lock
// marks its chunk dirty through the write lock and would deadlock against a
// read lock held across it. The section encode runs under the read lock, so a
// SetBlock or relight cannot change the chunk mid-encode. The second phase
// checks the entities are unchanged and restarts if not, so a chest broken
// between phases is not written as air carrying a chest; restarts are bounded
// (saveTries).
//
// It reports whether it wrote; a chunk whose stored data was unreadable is
// skipped without error.
func (w *World) saveChunk(pos ChunkPos, c *Chunk) (bool, error) {
	// LastUpdate is the game time at the save, which vanilla reads as the
	// chunk's last update.
	// vanilla: SerializableChunkData.copyOf (level.getGameTime())
	gameTime, _ := w.Time()
	c.lastUpdate = gameTime
	for range saveTries {
		ticks := w.ticksOfChunk(pos)
		w.mu.RLock()
		own := c.blockEntities()
		w.mu.RUnlock()
		live := encodeBlockEntities(own)

		w.mu.RLock()
		if !slices.Equal(c.blockEntities(), own) {
			w.mu.RUnlock()
			continue
		}
		root, err := encodeChunkNBTWith(c, live, ticks)
		if err != nil {
			w.mu.RUnlock()
			return false, fmt.Errorf("encode chunk (%d,%d): %w", pos.X, pos.Z, err)
		}
		root = w.clipChunkNBT(root)
		buf := saveBuffers.Get().(*[]byte)
		data, err := nbt.AppendEncode((*buf)[:0], "", root)
		w.mu.RUnlock()
		if err != nil {
			saveBuffers.Put(buf)
			return false, fmt.Errorf("serialize chunk (%d,%d): %w", pos.X, pos.Z, err)
		}
		if gate := w.chunkWriteGate.Load(); gate != nil {
			(*gate)()
		}
		rx, rz := regionCoords(pos)
		rf, err := w.region(rx, rz)
		if err != nil {
			saveBuffers.Put(buf)
			return false, err
		}
		err = rf.writeChunk(pos, data)
		w.releaseRegion(rf)
		*buf = data // keeps the grown capacity; writeChunk has copied or compressed data by now
		saveBuffers.Put(buf)
		return err == nil, skipUnreadable(err, "chunk", pos)
	}
	return false, fmt.Errorf("chunk (%d,%d) kept changing while it was saved", pos.X, pos.Z)
}

// skipUnreadable logs a refused write for an unreadable chunk and reports it
// done, so the caller drops its copy rather than retrying. Deliberately unlike
// vanilla, which overwrites such a chunk with a regenerated one.
func skipUnreadable(err error, what string, pos ChunkPos) error {
	if errors.Is(err, errUnreadable) {
		slog.Error("not saving a "+what+" whose stored data could not be read",
			"x", pos.X, "z", pos.Z, "error", err)
		return nil
	}
	return err
}

// region returns the open region file for (rx, rz), opening it on demand; the
// caller holds it until releaseRegion, so a concurrent eviction cannot close it
// underneath.
func (w *World) region(rx, rz int32) (*regionFile, error) {
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	return w.regions.get(rx, rz)
}

// releaseRegion gives back a handle from region.
func (w *World) releaseRegion(rf *regionFile) {
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	w.regions.release(rf)
}

// SetChunkGenerationGate calls gate before generating missing terrain, so
// tests can hold a generation backlog open without delaying world readers.
func (w *World) SetChunkGenerationGate(gate func(ChunkPos)) {
	if gate == nil {
		w.chunkGenerationGate.Store(nil)
	} else {
		w.chunkGenerationGate.Store(&gate)
	}
}

// SetChunkWriteGate makes every chunk save call gate after encoding, before it
// writes, with no lock held; nil removes it. It lets a test hold a save open.
func (w *World) SetChunkWriteGate(gate func()) {
	if gate == nil {
		w.chunkWriteGate.Store(nil)
		return
	}
	w.chunkWriteGate.Store(&gate)
}

// SetSaveFormat sets the chunk and entity records' compression
// (region-file-compression: deflate, lz4 or none) and per-write syncing
// (sync-chunk-writes). Call it before the first save: an open region file keeps
// the format it opened with.
func (w *World) SetSaveFormat(compression string, sync bool) error {
	format, err := parseSaveFormat(compression, sync)
	if err != nil {
		return err
	}
	w.regionMu.Lock()
	w.saveFormat = format
	w.regionMu.Unlock()
	return nil
}

// SeedDice reseeds the world's random source.
func (w *World) SeedDice(seed int64) {
	w.diceMu.Lock()
	defer w.diceMu.Unlock()
	w.dice = rand.New(rand.NewPCG(uint64(seed), 0))
}

// rollN returns a value in [0, n) from the world's random source.
func (w *World) rollN(n int) int {
	w.diceMu.Lock()
	defer w.diceMu.Unlock()
	return w.dice.IntN(n)
}

// relocateChunkRoot rewrites xPos/zPos to the slot read, before the decode keys
// block entities and filters scheduled ticks by them.
// vanilla: SerializableChunkData.read
func relocateChunkRoot(root nbt.Compound, pos ChunkPos) {
	x, _ := nbt.Get[nbt.Int](root, "xPos")
	z, _ := nbt.Get[nbt.Int](root, "zPos")
	if int32(x) == pos.X && int32(z) == pos.Z {
		return
	}
	slog.Error("chunk file is in the wrong location; relocating",
		"expected", pos, "got", ChunkPos{int32(x), int32(z)})
	root["xPos"] = nbt.Int(pos.X)
	root["zPos"] = nbt.Int(pos.Z)
}
