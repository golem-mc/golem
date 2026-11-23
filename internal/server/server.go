package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/golem-mc/golem/internal/server/effect"
	"log/slog"
	mrand "math/rand"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/codeofconduct"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/management"
	"github.com/golem-mc/golem/internal/server/metrics"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/resourcepack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/tickrate"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Per-phase deadlines for handshake and login; configuration and play are
// kept alive by keep-alives instead.
const (
	handshakeTimeout = 10 * time.Second
	loginTimeout     = 30 * time.Second
)

// Server holds configuration and state for the Minecraft server.
type Server struct {
	// Online player slots are allocated and released under mu.
	freePlayerSlots      []int
	nextPlayerSlot       int
	playerSlotGeneration uint64
	// chatOrderMu orders session publication, join snapshots and player chat.
	// Take it before mu and before any recipient's chatMu; HTTP runs outside it.
	chatOrderMu    sync.Mutex
	chatKeys       chatsecurity.KeyProvider
	chatKeysCtx    context.Context
	chatKeysCancel context.CancelFunc
	// openContainerWindows may over-count clients removed with a window open,
	// but never under-counts: zero is only a shortcut for viewer discovery.
	openContainerWindows atomic.Int32

	// structures is the template cache /place and the structure blocks share.
	structures *world.TemplateStore
	// gameTests is the run a test instance block's Run started.
	gameTests gameTestTicker
	// templateFiles feeds structures the enabled packs.
	templateFiles *datapack.TemplateFiles
	Config        *config.Config
	// resourcePack is the pack every client is offered in configuration; nil
	// when resource-pack is unset or its id is not a UUID.
	resourcePack *resourcepack.Pack

	// conduct is the Code of Conduct per language; empty when
	// enable-code-of-conduct is off or the folder holds no texts.
	conduct codeofconduct.Texts
	favicon string // base64-encoded PNG prefixed with "data:image/png;base64,"

	// queryConn is the GS4 query listener's UDP socket, nil with enable-query
	// off. Shutdown's close of it ends ServeQuery.
	queryConn *net.UDPConn

	// queryMu guards queryChallenges, the query handshake tokens: one per remote
	// address, replaced by each handshake, valid for queryChallengeLife.
	queryMu         sync.Mutex
	queryChallenges *queryChallengeBook
	// world is the overworld, which also holds the clock, weather, difficulty,
	// seed and spawn (the root level.dat is the overworld's). The nether and
	// the end are in dims.
	world *world.World
	// sessionLock is the world folder's session.lock, held while open so a
	// second server cannot write its region files; released on Shutdown.
	sessionLock *world.SessionLock
	// shutdownOnce makes a later Shutdown, even a concurrent one, wait for the
	// first and stop nothing again.
	shutdownOnce sync.Once
	// dimsList is the overworld, nether and end in vanilla's order, then the
	// pack and plugin dimensions in creation order, each with its own
	// generator, physics and minecraft:dimension_type ID. It is replaced
	// whole under dimsMu, never edited, so a reader on any goroutine holds a
	// consistent snapshot (allDims).
	dimsList atomic.Pointer[[]*dimension]
	dimsMu   sync.Mutex
	players  *playerStore

	// storages is /data's command storages under data/<namespace>/, saved with
	// the worlds.
	storages *world.CommandStorages
	// entityData is the plugins' entity Data, under its own folder so /data never lists it.
	entityData *world.CommandStorages
	// bossBars is /bossbar's custom bars, saved in
	// data/minecraft/custom_boss_events.dat.
	bossBars *world.BossBars
	// witherBars are the withers' boss bars as their watchers were told them.
	witherBars witherBars
	// stopwatches is /stopwatch's state, saved in data/minecraft/stopwatches.dat.
	stopwatches *world.Stopwatches
	// traderData is the wandering trader spawner's countdown, saved in
	// data/minecraft/wandering_trader.dat.
	traderData *world.WanderingTraderData
	// scheduled is /schedule's queue, saved in
	// data/minecraft/scheduled_events.dat.
	scheduled *world.TimerQueue
	// funcs is the data packs' functions and tick tag at the last (re)load,
	// swapped whole so the game loop never sees one without the other.
	funcs atomic.Pointer[loadedFunctions]
	// loadPending marks the minecraft:load tag as not yet run.
	loadPending atomic.Bool
	// packReloadMu is held across a whole data pack (re)load; taken before
	// s.mu, never inside it.
	packReloadMu sync.Mutex
	// packTagsApplied is set once pack tags have been layered over the
	// process-wide tables, so a reload finding none resets them.
	packTagsApplied bool
	// packLootApplied is packTagsApplied for the loot tables and predicates.
	packLootApplied bool
	// packRecipesApplied is packTagsApplied for the recipes.
	packRecipesApplied bool
	// packRecipes is what the packs defined at the last load, which the
	// plugins' recipes are laid over; pluginRecipes is that layer.
	packRecipes    registry.PackRecipes
	pluginRecipeMu sync.Mutex
	pluginRecipes  pluginRecipeLayer
	// levelDir is the world folder resolved under the configured universe.
	levelDir string
	// packs is the world's data packs and which of them are enabled.
	packs *serverPacks
	// packStems is the packs' dimension files by name, read once at start for
	// makeDimensions.
	packStems map[string]nbt.Compound
	// scoreboard is /scoreboard's objectives and scores, saved in
	// data/minecraft/scoreboard.dat.
	scoreboard *scoreboard.Board
	// pluginScoreboards publishes immutable snapshots for tick and packet readers.
	pluginScoreboards atomic.Pointer[[]*scoreboard.Board]
	// destroyedMu guards destroyed, the UUIDs of non-mob bodies removed
	// since the tick's last flush. A leaf lock: it is taken under any kind's
	// lock.
	destroyedMu sync.Mutex
	destroyed   []goneMob
	// raidNotes are the raid stages of this tick, kept under raidsMu until the
	// listeners can be asked without it.
	raidNotes []raidNote

	// randomSequences is /random's named sequences, saved like the storages.
	randomSequences *world.RandomSequences

	// maps is the saved map data under data/minecraft/maps/; nothing new is
	// drawn on them.
	maps *world.Maps

	// privateKey and its DER-encoded public half are for online mode's
	// encrypted handshake; both nil offline.
	privateKey *rsa.PrivateKey
	publicKey  []byte

	// sessionServer overrides the hasJoined URL; set by tests before serving.
	sessionServer string
	// profileLookupServer overrides the name-lookup base URL; set by tests before serving.
	profileLookupServer string

	// loginTimeout overrides the login deadline when positive; set by tests
	// before serving.
	loginTimeout time.Duration

	// spinStacks is shared by all clients: tagged weapons can move between
	// players through drops and inventory transfers.
	spinStacks atomic.Uint64

	nextEntityID atomic.Int32

	// Keep-alive timings in nanoseconds, per server so tests can shorten them;
	// atomic so a test can do so once a player is in.
	keepAliveInterval atomic.Int64
	keepAliveTimeout  atomic.Int64
	// idleTimeout is the idle kick in minutes, off at zero or less;
	// /setidletimeout changes it live.
	idleTimeout atomic.Int32

	// autoSaveTicks is the auto-save interval in ticks, 0 for off; per server
	// so a test can shorten it, even while running.
	autoSaveTicks atomic.Int64
	// lastSaveTick is the ticksRun of the last auto-save; tick loop only.
	lastSaveTick uint64
	// emptyPause decides when the world stops ticking with no players; tick
	// loop only.
	emptyPause *tickrate.EmptyPause
	// savingOff is set by /save-off so the auto-save skips the levels until
	// /save-on. savingMu guards it and is held for a whole auto-save.
	// vanilla: ServerLevel.noSave
	savingMu sync.Mutex
	// bgSave counts the auto-saves still writing on their own goroutine.
	bgSave    saveGroup
	savingOff bool
	// asyncAutoSave makes a stepped test clock save off the tick too.
	asyncAutoSave atomic.Bool
	// entityWritesGated lets tests drive mob ticks without waiting on their disk gate.
	entityWritesGated atomic.Bool
	// timeSyncTicks is the time broadcast interval in ticks, 0 for never; per
	// server so a test can change it.
	timeSyncTicks uint64
	// lastTimeSync is the ticksRun of the last time broadcast; tick loop only.
	lastTimeSync uint64

	// stopReq is closed by RequestStop: Listen ends its listener on it and main
	// waits on it to shut down. stopOnce makes a second /stop a no-op.
	stopReq  chan struct{}
	stopOnce sync.Once
	// mgmt is the management server, nil unless enabled. mgmtBound is closed
	// once Listen has bound it at mgmtAddr.
	mgmt      *management.Server
	mgmtBound chan struct{}
	mgmtAddr  string
	// crashed is set when a tick panicked and the main loop stopped, or when
	// the watchdog fired.
	crashed atomic.Bool
	// nextTickNanos is the main loop's next scheduled tick start in unix
	// nanos on its loop clock, set before each tick runs; 0 while the loop
	// is not running. The watchdog measures a tick's overrun from it.
	// vanilla: MinecraftServer.nextTickTimeNanos
	nextTickNanos atomic.Int64
	// tickFault, test-only, is called at the top of every tick.
	tickFault atomic.Pointer[func()]
	// tickLockFault, test-only, is called by lockTick with the lock just
	// taken.
	tickLockFault atomic.Pointer[func(*sync.Mutex)]
	// tickLocks is the entity-kind locks the tick goroutine holds through
	// lockTick, innermost last; only that goroutine touches it.
	tickLocks []*sync.Mutex
	// pendingExploder is the removed body of the blast hurting a mob, for the
	// mob hurt gate; guarded by mobsMu.
	pendingExploder *removedBody
	// stopping is closed under mu when Shutdown begins, after which no
	// connection joins (beginJoin, addPlayer). joins counts connections past
	// beginJoin not yet registered or refused, which Shutdown waits out before
	// listing whom to disconnect.
	stopping chan struct{}
	joins    sync.WaitGroup
	// leaveMu is read-held by a departure and write-held by Shutdown to set
	// worldsClosed before the worlds close; a later departure sees it and
	// touches neither worlds nor player files.
	leaveMu      sync.RWMutex
	worldsClosed bool

	// ticksRun counts finished main-loop ticks; tests wait on it for a whole
	// tick to pass.
	ticksRun atomic.Uint64
	// gateways is the end gateways' cooldowns.
	gateways gatewayCooldowns
	// tickEnded is closed at each tick's end and replaced in the same step, so
	// every waiter wakes together (see awaitTickEnds). Nil until the loop
	// starts.
	tickEnded atomic.Pointer[chan struct{}]
	// metrics records how long each tick of the main loop took.
	metrics *metrics.Recorder
	// tickRate is the loop's ticking state: pace, the freeze gating the world's
	// passes, and /tick's sprint and steps.
	tickRate *tickrate.Manager

	// seed is the root of every random source below (dice.go): the clock live,
	// the test's name in a test. Each source is goroutine-safe except spawnRng,
	// dropRng and dispenseRng: the spawn pass owns its source, dropMu guards
	// the spill's, and only the tick loop dispenses.
	seed      int64
	spawnRng  *mrand.Rand // owned by the spawn pass; tests use it only with the loop held
	envRng    *mrand.Rand // the environment step: fire ignition, respiration
	lootRng   *mrand.Rand // loot table rolls
	brushes   brushBook   // what the blocks being brushed have taken
	vaults    vaultBook   // the vaults' compounds being read and written
	infestRng *mrand.Rand // the infested effect's rolls on players
	popRng    *mrand.Rand // where popped items and orbs land and fly
	levelRng  *mrand.Rand // the rest of vanilla's level random: xp, anvils, shots
	soundRng  *mrand.Rand // sound pitches and volumes

	// ominousLoot is the trial spawner loot rolled so far, ominousRolls the
	// roll count; tick goroutine only.
	ominousLoot  map[ominousLootKey][]protocol.ItemStack
	ominousRolls int

	dropMu      sync.Mutex
	dropRng     *rules.JavaRandom // a broken container's spill
	dispenseRng *rules.JavaRandom // dispensers and droppers

	// spawnDisabled turns the natural spawn pass off. Only the test harness
	// sets it, so tests not about spawning see nothing turn up unasked; a
	// test sets it while the loop runs, so it is atomic.
	spawnDisabled atomic.Bool

	// moveChecksOff turns off the moved-too-quickly and moved-wrongly checks.
	// Only the test harness sets it, since its tests place a body anywhere in
	// one move.
	moveChecksOff bool

	// Live properties read by connections and the tick loop while management
	// setters persist Config under settingsMu.
	maxPlayers           atomic.Int32
	motd                 atomic.Pointer[protocol.Text]
	spawnProtection      atomic.Int32
	acceptsTransfers     atomic.Bool
	hideOnlinePlayers    atomic.Bool
	enableStatus         atomic.Bool
	entityBroadcastRange atomic.Int32

	// allowFlight is the allow-flight setting.
	allowFlight atomic.Bool
	// enforceWhitelist is the enforce-whitelist setting.
	enforceWhitelist atomic.Bool
	// opLevel is op-permission-level, the level /op grants.
	opLevel atomic.Int32
	// viewDistance and simulationDistance are the view-distance and
	// simulation-distance settings in chunks; the management protocol
	// changes them live.
	viewDistance       atomic.Int32
	simulationDistance atomic.Int32

	// defaultGameMode is new players' mode: the gamemode key at start, then
	// /defaultgamemode. Not saved across restarts.
	defaultGameMode atomic.Int32
	// forceGameMode is the force-gamemode setting.
	forceGameMode atomic.Bool

	// Test hooks, nil otherwise. dispenseActHook runs between a dispense
	// visit's decision and act, where a container click lands;
	// dispenseDecideHook runs between the entity's lookup and its lock, where a
	// break or unload removes it.
	dispenseActHook    func()
	dispenseDecideHook func()

	// shelfSwapHook, test-only, runs between a shelf swap readying its block
	// entities and locking them, where a break or unload removes one.
	shelfSwapHook func()

	// beforeAckHook, test-only, runs just before a block interaction's
	// acknowledgement, where a handler's late change lands.
	beforeAckHook func()

	// carryPearlHook, test-only, runs at the start of a cross-dimension pearl
	// carry, between the landing's tick check and the goroutine's own.
	carryPearlHook func()

	// tickDone stops the main loop; shutdown closes it after its own save so
	// the two do not overlap.
	tickDone chan struct{}
	// tickExited is closed as the main loop returns, so stopTick can wait out
	// a tick in progress before the world closes under it.
	tickExited chan struct{}
	// tickRunning says whether the main loop runs, which decides whether play
	// reads get an idle deadline (see armReadDeadline). backstopMu guards it
	// with the deadlines: read loops arm under the read lock and stopTick
	// clears under the write lock, so no clear lands between a check and its
	// arming. It is taken before mu.
	backstopMu  sync.RWMutex
	tickRunning bool
	// tickClock and tickLog are the main loop's tick clock and its logger (the
	// backlog warning, a sprint's report); nil means wall clock and slog's
	// default. Tests swap them before the loop starts. tickClockMu guards
	// tickClock, which connections read for keep-alive time.
	tickClockMu sync.RWMutex
	tickClock   tickClock
	tickLog     *slog.Logger
	// keepAliveNow, when set, is the clock the keep-alive exchange runs by.
	keepAliveNow func() time.Time
	// lastPlayerInfo is the ticksRun count the latencies were last broadcast at.
	lastPlayerInfo uint64

	// spawnNow is the clock for date-bound spawn rolls (vanilla's
	// SpecialDates); nil is the wall clock.
	spawnNow func() time.Time

	mu sync.RWMutex
	// clients is everyone in the world, by player UUID. Connections are kept,
	// not just players, because what one player does must be written to the
	// others; dimensions only change who the others are.
	clients map[uuid.UUID]*client
	// deferWakes holds mailbox wakes until the tick ends.
	deferWakes atomic.Bool
	// wardenMu guards every client's wardenTracker; nothing is locked while it
	// is held.
	wardenMu sync.Mutex
	// configClients are connections in configuration, whose keep-alives the
	// tick runs; under configMu.
	configMu      sync.Mutex
	configClients map[*client]struct{}
	// joinCounter numbers connections as they join, under mu: the player-list
	// order command targets resolve in.
	joinCounter uint64

	// itemsMu guards items, the item entities in the worlds. The main loop
	// ticks them; anything else (a break, drop or death) only adds and
	// broadcasts the spawn, so this lock keeps a physics step from racing an
	// arrival. It is taken inside mu where both are needed, which is only
	// spawnItem and the tick's copy of the recipients.
	itemsMu    sync.Mutex
	items      []*itemEntity
	itemOrder  uint64
	itemChunks entity.ChunkIndex[*itemEntity]
	itemCols   *itemColumns
	// itemColBuffer retains storage between passes; itemCols is nil outside them.
	itemColBuffer itemColumns
	// deferItemSteps excludes arrivals during the entity passes ahead of
	// tickItems from that tick's item steps. Guarded by itemsMu.
	deferItemSteps bool

	// pickupSeam, test-only, opens the window between a pickup's claim on an
	// item and giving its stack, which a command's goroutine can win.
	pickupSeam func(itemID int32)

	// vitalsSeam, test-only, opens the window between a vitals tick reading a
	// body's bars and comparing them, which a death from another connection can
	// win.
	vitalsSeam func(name string)

	// relightSeam, test-only, reports each block-change batch's relight
	// bookings and changes, so a test can check them without the light pool's
	// timing.
	relightSeam func(booked int, changes []world.BlockChange)
	// relightsRun counts the relights relightAreaAsync submitted, skips
	// excluded; test-only, read through RelightsRun.
	relightsRun atomic.Int64
	// relightWG counts relight results not yet handed to the broadcast or the
	// flush queue, for WaitRelights.
	relightWG sync.WaitGroup
	// gatewayWG counts gateway moves running off the tick.
	gatewayWG sync.WaitGroup

	// chunkTakeSeam, test-only, opens the pause in a paced chunk take between
	// its client bookkeeping writes and its world work.
	chunkTakeSeam atomic.Pointer[func()]

	// chunkBeat, set only by a waiting test, is closed after any connection's
	// next chunk-sender pass.
	chunkBeat atomic.Pointer[chan struct{}]

	// chunkStopSeam, test-only, runs in a chunk sender's stop after its stop
	// channel closes, before the wait for the sender.
	chunkStopSeam func()
	// chunkSenderHold, test-only, holds every chunk sender started while set
	// until the channel closes, before its first pass.
	chunkSenderHold atomic.Pointer[chan struct{}]
	// chunkSenders counts the chunk senders ever started.
	chunkSenders atomic.Int64

	// chunkBatchSeam, test-only, opens the window between a chunk batch's start
	// packet and its first chunk.
	chunkBatchSeam func()

	// leaveSeam, test-only, opens the window between a departure's save and
	// its removal from the player list, where a save still seeing the player
	// must not overwrite the departure's file.
	leaveSeam func()

	// leaveWaitSeam, test-only, opens the window between a departure's
	// announcement and its wait for the released chunks' saves, where a player
	// arriving meanwhile must not be told of the departure.
	leaveWaitSeam func()

	// arrowRefusalSeam, test-only, opens the window between a refused hurt and
	// the tick's deflection, which a command's goroutine can win.
	arrowRefusalSeam func()

	// blastSpillSeam, test-only, runs after a blast reads a destroyed
	// container's slots and before giving them up, where a hopper's or
	// dispenser's turn must find the container gone.
	blastSpillSeam func(*world.BlockEntity)

	// playerTrackSeam, test-only, opens the window in the players' tracking
	// pass between its snapshot and settling, which a dimension change on
	// another goroutine can win. s.mu guards it, as the tick reads it each pass.
	playerTrackSeam func()

	// playerSendSeam, test-only, opens the window in the tracking pass between
	// deciding a pairing and sending it, which a step on another goroutine can
	// win. s.mu guards it, as the tick reads it each pass.
	playerSendSeam func()

	// playerJoinSeam, test-only, opens the window in a join between spawning a
	// body and finishing the newcomer's view, which the body's own connection
	// can win. s.mu guards it; the join reads it once per body.
	playerJoinSeam func()

	// playerJoinBodiesSeam, test-only, opens the window in a join after the tab
	// list and before spawning others' bodies, which a dimension arrival can
	// win. s.mu guards it; the join reads it once.
	playerJoinBodiesSeam func()

	// clickSeam, test-only, opens the window between a container click's block
	// entity lookup and its lock, which a blast removing the container can win.
	clickSeam func(*world.BlockEntity)

	// blockEntityTickSeam, test-only, runs before each entity's turn in the
	// block-entity pass, where a break may have removed it since the list was
	// read.
	blockEntityTickSeam func(*world.BlockEntity)

	// minecartCrossSeam, test-only, opens the window in a cart's crossing
	// after copying its riders and before carrying each, which a rider's own
	// crossing on another goroutine can win.
	minecartCrossSeam func()
	// minecartBoardSeam, test-only, opens the window between a crossing
	// twin's registration and the seating of its riders.
	minecartBoardSeam func()

	// tntCartBlastSeam, test-only, sees each TNT cart blast's radius.
	tntCartBlastSeam func(radius float64)

	// gameEventSeam, test-only, sees each game event posted.
	gameEventSeam atomic.Pointer[func(event string, x, y, z float64, ctx rules.GameEventContext)]

	// mobsMu guards the mobs, per dimension like the items: the main loop ticks
	// every herd under it, and anything else (spawn, hit, shearing) only reads
	// or adds. It is taken inside mu where both are needed, like itemsMu.
	mobsMu sync.Mutex
	// waypointPass counts the locator bar passes; tick loop only.
	waypointPass uint64
	// Mob tick scratch maps belong to the tick loop; no reader retains them
	// after tickMobs returns.
	mobTickKnown map[uuid.UUID]bool
	// mobTickForce reads the tick's UUID and loaded sets for every dimension
	// next tick (tick goroutine only).
	mobTickForce bool
	// mobInputs is the mob tick's inputs, reused every tick (tick goroutine only).
	mobInputs     mobTickInputs
	mobTickSends  []mobSend
	mobTickLoaded map[*world.World]map[world.ChunkPos]struct{}
	// tradeDraws reserves a merchant while a draw loads chunks without mobsMu.
	tradeDraws map[*entity.Mob]*tradeDrawState
	// raidsMu guards the dimensions' raids and raidState. It is taken
	// before mobsMu and s.mu, never while holding either.
	raidsMu   sync.Mutex
	raidState raidState

	// arrowsMu guards arrows, thrown projectiles (eggs, snowballs, potions) and
	// area effect clouds. The main loop ticks them under it; a bow, a throw or
	// a skeleton's shot only adds. It is taken inside mu and around mobsMu
	// (projectile ticks read mob bodies with the mob lock beneath), and by
	// nothing else holding another server lock.
	arrowsMu     sync.Mutex
	arrows       []*arrowEntity
	arrowChunks  entity.ChunkIndex[*arrowEntity]
	thrown       []*thrownEntity
	thrownChunks entity.ChunkIndex[*thrownEntity]
	clouds       []*cloudEntity
	cloudChunks  entity.ChunkIndex[*cloudEntity]

	// fangs are the evoker fangs of every dimension, under their own lock.
	fangs fangSet

	// bobbersMu guards bobbers, the cast fishing hooks (one per angler, found
	// by owner). The main loop steps them under it; a cast adds, a retrieval
	// removes, a use reads. It is taken around mobsMu like the projectile
	// pass's lock, by the jump's sends and observer bookkeeping removals, and
	// by nothing else holding another server lock.
	bobbersMu    sync.Mutex
	bobbers      []*bobberEntity
	bobberChunks entity.ChunkIndex[*bobberEntity]

	// orbsMu guards the experience orbs: the main loop ticks them under it; an
	// award, summon or death only adds. It is taken inside mu where both are
	// needed, like itemsMu.
	orbsMu    sync.Mutex
	orbs      []*orbEntity
	orbChunks entity.ChunkIndex[*orbEntity]

	// tntMu guards tnts, the primed TNT: the main loop ticks them under it; a
	// lit block or chain spawn only adds. It is taken inside mu where both are
	// needed, like itemsMu, and by nothing else holding another server lock.
	tntMu     sync.Mutex
	tnts      []*tntEntity
	tntChunks entity.ChunkIndex[*tntEntity]
	// tntBlasts indexes tnts by position while tickTNT runs its blasts, so
	// one blast reads the bodies near it and not the whole list.
	tntBlasts tntBlastIndex

	// boltMu guards bolts, the lightning bolts. It is taken alone.
	boltMu     sync.Mutex
	bolts      []*boltEntity
	boltChunks entity.ChunkIndex[*boltEntity]
	// boltTickEffects is scratch owned by the tick goroutine; records are cleared after applying.
	boltTickEffects boltEffects
	// lightningOdds is the denominator of a chunk's strike die; 0 is
	// vanilla's rules.LightningChunkOdds.
	lightningOdds int
	// trapOdds, when positive, replaces a strike's skeleton-trap chance.
	trapOdds float64

	// fireworksMu guards fireworks, rockets in flight: the main loop ticks them
	// under it; a launch only adds. It is taken inside mu where both are
	// needed, like itemsMu, and with the mob lock by the tick reading bodies a
	// flight may meet.
	fireworksMu    sync.Mutex
	fireworks      []*fireworkEntity
	fireworkChunks entity.ChunkIndex[*fireworkEntity]

	// boatsMu guards boats: the main loop ticks them under it; placing,
	// mounting, driving or striking one only adds or amends. It is taken inside
	// mu where both are needed, like itemsMu, and by nothing else holding
	// another server lock.
	boatsMu sync.Mutex
	// boatScratch holds each world's boat body storage between ticks; only
	// the boat tick touches it.
	boatScratch map[*world.World]*boatWorldBodies
	boats       []*boatEntity
	boatChunks  entity.ChunkIndex[*boatEntity]
	// cartPushScratch holds the cart tick's push inputs between ticks; only
	// the cart tick touches it.
	cartPushScratch *cartPushes
	// mannequinPushScratch is used only by the mannequin tick.
	mannequinPushScratch *mannequinPushes
	// crammers is the survival tick's pushable bodies, rebuilt on first need.
	crammers crammingCrowd

	// minecartsMu guards minecarts: the main loop ticks them under it; placing,
	// boarding, casting off or striking one only adds or amends. It is taken
	// inside mu and boatsMu where both are needed. The engine's detector-rail
	// answer takes it alone outside the engine's own lock.
	minecartsMu    sync.Mutex
	minecarts      []*minecartEntity
	minecartChunks entity.ChunkIndex[*minecartEntity]
	// vehicleMarks is each hull's and cart's transform at the last VehicleMoveEvent pass; tick goroutine only.
	vehicleMarks map[uuid.UUID]markState
	vehicleGen   uint32
	cartCols     *cartColumns
	// cartColBuffer retains storage between passes; cartCols is nil outside them.
	cartColBuffer cartColumns
	// pickupCands retains pickUpMobs' per-world storage between ticks.
	pickupCands map[*world.World]*pickupCands

	// fallingMu guards fallings, falling blocks in flight: the main loop ticks
	// them under it; the physics engine's hand-off only adds. It is taken
	// inside mu where both are needed, like itemsMu, and by nothing else
	// holding another server lock.
	fallingMu     sync.Mutex
	fallings      []*fallingEntity
	fallingChunks entity.ChunkIndex[*fallingEntity]

	// paintingsMu guards paintings. It is taken inside mu where both are
	// needed, like itemsMu, and before framesMu for a survival check.
	paintingsMu    sync.Mutex
	paintings      []*paintingEntity
	paintingChunks entity.ChunkIndex[*paintingEntity]

	// framesMu guards frames and frameCells, the frames indexed by the two
	// cells whose change might unhang them (their own and the support behind).
	// A use, wake or departure changes the list; a block change reads the
	// index. It is taken inside mu where both are needed, after paintingsMu
	// when a survives check asks both, and by nothing else holding another
	// server lock.
	framesMu    sync.Mutex
	frames      []*frameEntity
	frameChunks entity.ChunkIndex[*frameEntity]
	frameCells  map[frameCell][]*frameEntity

	// knotsMu guards knots, leash knots on fences. The main loop polls them
	// under it; a lead used on a fence, a use on the knot or a blow adds,
	// removes or breaks one. It is taken inside mu where both are needed, and
	// after mobsMu where both are (the leash-drop pass reads leashed mobs under
	// both).
	knotsMu    sync.Mutex
	knots      []*knotEntity
	knotChunks entity.ChunkIndex[*knotEntity]

	// armorStandsMu guards armorStands: the main loop ticks them under it; a
	// placement, summon or wake only adds. It is taken inside mu where both are
	// needed, like itemsMu, and by nothing else holding another server lock.
	armorStandsMu sync.Mutex
	armorStands   []*armorStandEntity
	standChunks   entity.ChunkIndex[*armorStandEntity]

	// endCrystalsMu guards endCrystals, the end crystals standing in the worlds.
	endCrystalsMu sync.Mutex
	endCrystals   []*endCrystalEntity
	crystalChunks entity.ChunkIndex[*endCrystalEntity]

	// mannequinsMu guards mannequins, the mannequins standing in the worlds.
	mannequinsMu    sync.Mutex
	mannequins      []*mannequinEntity
	mannequinChunks entity.ChunkIndex[*mannequinEntity]

	// eyesMu guards eyes, the eyes of ender in flight.
	eyesMu    sync.Mutex
	eyes      []*eyeEntity
	eyeChunks entity.ChunkIndex[*eyeEntity]

	// eyeTargets overrides the current world's structure locator in tests.
	eyeTargets entity.EyeTargets

	// fixturesMu guards fixtures: displays, interactions and markers.
	fixturesMu    sync.Mutex
	fixtures      []*fixtureEntity
	fixtureChunks entity.ChunkIndex[*fixtureEntity]
	// cushionsMade is set once any cushion is built and never cleared, so
	// a piston move skips the fixture scan on a server that never had one.
	cushionsMade atomic.Bool

	// ops is the operator list from ops.json, written by /op and /deop; it sets
	// each player's permission level and so their offered commands.
	ops *opsStore

	// bans is the player and IP ban lists (banned-players.json,
	// banned-ips.json), written by /ban, /ban-ip, /pardon and /pardon-ip and
	// read by login.
	bans *banStore

	// whitelist is whitelist.json and the white-list switch, set by /whitelist
	// and read by login.
	whitelist *whitelistStore

	// usercache is usercache.json, every joined player's name-UUID pair, for
	// resolving the names of players who have left.
	usercache *userCacheStore

	// configPath is where a changed setting rewrites server.properties.
	configPath string
	// settingsMu guards every runtime write to Config and every Config.Save.
	settingsMu sync.Mutex

	// sleepMu guards sleepStatus, vanilla's SleepStatus: the sleeper counts the
	// night skip and sleeping message read, recounted when a sleep begins or
	// ends or the overworld's players change. It is taken inside mu where both
	// are needed, like itemsMu.
	sleepMu sync.Mutex

	// bedMu makes claiming a bed one step: a sleep's occupied check and the
	// occupancy it sets run under it, so of two clicks racing for a free bed
	// only one passes (vanilla runs both on one thread). It is taken inside
	// lookMu, before the sleepMu the recount takes.
	bedMu sync.Mutex

	// portalHolds are the chunks portal crossings keep loaded for a while (in
	// place of vanilla's portal ticket), spent down by tickPortalHolds.
	// portalHoldsMu guards it, since a crossing holds its arrival chunk on the
	// carrying goroutine, not the tick loop.
	portalHoldsMu sync.Mutex
	// portalNotes are the portal events the tick's passes queued for the plugins.
	portalNotes portalNotes
	// portalTripsDecided counts player nether trips that finished, crossed or
	// refused; tests wait on it.
	portalTripsDecided atomic.Int64
	portalHolds        []portalHold

	// lidsMu guards lids, written by an open on a connection goroutine while
	// the tick counts entries down. Held around the map only, never across a
	// broadcast or another lock.
	lidsMu sync.Mutex

	// lids are containers whose lid block action vanilla re-sends while someone
	// looks in: ContainerOpenersCounter's recheck, five ticks after each send,
	// stored as ticks until the next. The tick recounts viewers per entry and
	// drops entries nobody is looking in.
	lids map[lidRecheck]int32

	// golemOpen is the copper golems' share of each container's openers.
	golemOpen golemOpeners

	// Keep plugin state after the hot fields to preserve their offsets.
	plugins         *pluginruntime.Runtime
	chunkEvents     chunkEventQueue
	pluginAdapter   *pluginHost
	pluginItemsOnce sync.Once
	pluginItemsVal  *pluginItems
	pluginCommands  pluginCommandRegistry
	// bareTrees caches pluginCommandTree per permission level while no plugin
	// has a command.
	bareTrees  [command.PermLevelOwners + 1]atomic.Pointer[command.Node]
	pluginHelp pluginHelp
}

// sleepCounts is vanilla's SleepStatus: a dimension's players and how many
// sleep, as of the last recount. It recounts when a sleep begins or ends,
// not every tick, so the night skip reads the share as of the last such
// change, against which the hundred-tick sleepers are counted.
type sleepCounts struct {
	active   int
	sleeping int
}

// New creates a Server from cfg. An unusable IconFile is logged and the
// server starts without an icon.
func New(cfg *config.Config, plugins ...plugin.Registration) (*Server, error) {
	return newServerWith(cfg, time.Now().UnixNano(), nil, plugins...)
}

// testLevelType and testNewGenerator let black-box tests build worlds on
// their own generator for a level-type, given the configured level-seed.
// Production leaves the generator nil, so every level-type reaches
// world.NewGenerator.
var (
	testLevelType    string
	testNewGenerator func(levelSeed string) world.Generator
)

// newServer is New with the root seed of the server's random sources given.
func newServer(cfg *config.Config, seed int64) (*Server, error) {
	return newServerWith(cfg, seed, nil)
}

// newServerWith is newServer with the nether's generation taken from shared
// when it is not nil.
func newServerWith(cfg *config.Config, seed int64, shared sharedNether, plugins ...plugin.Registration) (*Server, error) {
	// vanilla: DedicatedServer.initServer
	slog.Info("Starting minecraft server version " + protocol.GameVersion)
	// vanilla: Main.main, LevelStorageSource.LevelStorageAccess
	levelDir := cfg.LevelName
	if cfg.Universe != "" && cfg.Universe != "." {
		levelDir = filepath.Join(cfg.Universe, levelDir)
	}
	templateFiles := &datapack.TemplateFiles{WorldDir: levelDir}
	s := &Server{
		Config:          cfg,
		templateFiles:   templateFiles,
		players:         newPlayerStore(levelDir),
		levelDir:        levelDir,
		structures:      world.NewTemplateStore(templateFiles, dataVersion),
		clients:         make(map[uuid.UUID]*client),
		queryChallenges: newQueryChallengeBook(),
		storages:        world.NewCommandStorages(levelDir),
		entityData:      world.NewCommandStorages(filepath.Join(levelDir, "plugin_entity_data")),
		randomSequences: world.NewRandomSequences(levelDir),
		bossBars:        world.NewBossBars(levelDir, command.ComponentDecodes),
		stopwatches:     world.NewStopwatches(levelDir),
		traderData:      world.NewWanderingTraderData(levelDir),
		scheduled:       world.NewTimerQueue(levelDir),
		maps:            world.NewMaps(levelDir),
		timeSyncTicks:   defaultTimeSyncTicks,
		lids:            make(map[lidRecheck]int32),
		frameCells:      make(map[frameCell][]*frameEntity),
		stopReq:         make(chan struct{}),
		mgmtBound:       make(chan struct{}),
		stopping:        make(chan struct{}),
		metrics:         metrics.NewRecorder(tickInterval),
		tickRate:        tickrate.New(),
	}
	s.maxPlayers.Store(int32(cfg.MaxPlayers))
	s.spawnProtection.Store(int32(cfg.SpawnProtection))
	motd := protocol.Text{Text: cfg.MOTD}
	s.motd.Store(&motd)
	s.loadPlugins(plugins)
	initialized := false
	defer func() {
		if !initialized {
			s.disablePlugins()
		}
	}()
	s.chatKeysCtx, s.chatKeysCancel = context.WithCancel(context.Background())
	s.chatKeys = chatsecurity.NewServiceKeys(s.chatKeysCtx, nil)
	pack, warnings := resourcepack.Resolve(cfg.ResourcePack, cfg.ResourcePackID,
		cfg.ResourcePackSHA1, cfg.LegacyResourcePackHash, cfg.ResourcePackPrompt, cfg.RequireResourcePack)
	for _, w := range warnings {
		slog.Warn(w)
	}
	s.resourcePack = pack
	if cfg.EnableCodeOfConduct {
		dir := cfg.CodeOfConductDir
		if dir == "" {
			dir = codeofconduct.DefaultDir
		}
		texts, err := codeofconduct.Load(dir)
		if err != nil {
			return nil, err
		}
		s.conduct = texts
	}
	mgmt, err := newManagement(s, cfg)
	if err != nil {
		return nil, err
	}
	s.mgmt = mgmt
	s.keepAliveInterval.Store(int64(defaultKeepAliveInterval))
	s.keepAliveTimeout.Store(int64(defaultKeepAliveTimeout))
	s.idleTimeout.Store(int32(cfg.PlayerIdleTimeout))
	s.autoSaveTicks.Store(int64(max(cfg.AutoSaveTicks, 0)))
	s.emptyPause = tickrate.NewEmptyPause(cfg.PauseWhenEmptySeconds)
	// Take the session lock before anything reads the world, so a second server
	// on the folder stops here instead of racing for region files; a failed
	// build releases it. vanilla: LevelStorageSource.createAccess
	lock, err := world.Lock(levelDir)
	if err != nil {
		return nil, err
	}
	s.sessionLock = lock

	// The overworld first: it settles the seed and writes the level.dat the
	// other dimensions' generators read, all seeded alike as one vanilla world.
	// The Done line times only the level load, vanilla's span.
	// vanilla: DedicatedServer.initServer, loadLevel
	levelStart := time.Now()
	slog.Info("Preparing level \"" + cfg.LevelName + "\"")
	built := false
	defer func() {
		if !built {
			if err := s.sessionLock.Release(); err != nil {
				slog.Warn("could not release the world session lock", "error", err)
			}
		}
	}()
	if err := world.MigrateLayout(levelDir); err != nil {
		return nil, err
	}
	// vanilla: Main.main runs WorldUpgrader before the server starts
	if cfg.ForceUpgrade || cfg.RecreateRegionFiles {
		if err := world.UpgradeWorld(levelDir, world.UpgradeOptions{EraseCache: cfg.EraseCache, Recreate: cfg.RecreateRegionFiles}); err != nil {
			return nil, err
		}
	}
	// Read once the layout has moved a legacy data/scoreboard.dat into place.
	s.scoreboard = s.newScoreboard(levelDir)
	levelSeed := cfg.LevelSeed
	if cfg.Demo {
		// vanilla: WorldOptions.DEMO_OPTIONS; an existing world keeps its stored seed
		levelSeed = demoSeed
	}
	var gen world.Generator
	if override := s.earlyOverworldGenerator(levelSeed); override != nil {
		gen = override
	} else if testNewGenerator != nil && cfg.LevelType == testLevelType {
		// The test level-type also overrides the stored one, so a restarted test
		// world (whose world_gen_settings.dat names the stored type) keeps the test
		// ground.
		gen = testNewGenerator(levelSeed)
	} else {
		gen = world.NewGenerator(levelDir, cfg.LevelType, levelSeed, cfg.GeneratorSettings)
		if og, ok := gen.(*world.OverworldGenerator); ok && !cfg.GenerateStructures && !cfg.Demo {
			og.DisableStructures()
		}
	}
	// Only a generator that records bonus_chest in world_gen_settings.dat takes
	// the chest, so the file always agrees with the world.
	bonusChest := false
	if cfg.BonusChest {
		if g, ok := gen.(interface{ EnableBonusChest() }); ok {
			g.EnableBonusChest()
			bonusChest = true
		}
	}
	spawnStart := time.Now()
	loaded, err := world.NewWorld(levelDir, gen)
	if err != nil {
		return nil, err
	}
	s.world = loaded
	// vanilla: DedicatedServer.initServer (legacy announce-player-achievements)
	if v := cfg.LegacyAnnounceAchievements; v != "" {
		s.world.SetBoolRule(world.RuleShowAdvancementMsgs, strings.EqualFold(v, "true"))
	}
	if og, ok := gen.(*world.OverworldGenerator); ok {
		og.SetStructureMobFinalizer(s.structureMobFinalizer(world.DimensionName))
	}
	if err := s.openDataPacks(); err != nil {
		// vanilla: Main.main logs this at WARN with the exception
		slog.Warn("Failed to load datapacks, can't proceed with server load. You can either fix your datapacks or reset to vanilla with --safeMode", "error", err)
		return nil, err
	}
	if og, ok := gen.(*world.OverworldGenerator); ok {
		og.RefreshBiomeClimates()
		if world.PackWorldgenActive() {
			og.ForgetGenerated()
		}
	}
	s.world.ReloadClocks()
	debugSetup := s.world.IsDebug() && !s.world.Existed()
	if debugSetup {
		s.world.SetupDebugLevel()
	}
	if err := s.makeDimensions(gen, shared); err != nil {
		return nil, err
	}
	s.seedDice(seed)
	// Only a new world picks a spawn; an existing one keeps its level.dat's.
	// vanilla: MinecraftServer.prepareLevels
	if !s.world.Existed() {
		slog.Info("Selecting global world spawn...")
		if bonusChest {
			s.world.PlaceBonusChest(s.levelRng)
		}
	}
	forced := 0
	for _, d := range s.allDims() {
		forced += d.w.HoldForcedChunks()
	}
	slog.Info(fmt.Sprintf("Loading %d persistent chunks...", forced))
	slog.Info("Preparing spawn area: 100%")
	slog.Info(fmt.Sprintf("Time elapsed: %d ms", time.Since(spawnStart).Milliseconds()))

	favicon, err := loadFavicon(IconFile)
	if err != nil {
		slog.Warn("ignoring server icon", "error", err)
	} else if favicon != "" {
		s.favicon = favicon
		slog.Info("loaded server icon", "path", IconFile)
	}

	if cfg.OnlineMode {
		if err := s.generateKeyPair(); err != nil {
			return nil, err
		}
	} else {
		slog.Warn("online mode is off, players are not authenticated")
	}

	// server.properties' difficulty applies to a new world; an existing
	// level.dat keeps its own.
	s.world.SetDefaultHardcore(cfg.Hardcore)
	s.allowFlight.Store(cfg.AllowFlight)
	s.acceptsTransfers.Store(cfg.AcceptsTransfers)
	s.hideOnlinePlayers.Store(cfg.HideOnlinePlayers)
	s.enableStatus.Store(cfg.EnableStatus)
	s.entityBroadcastRange.Store(int32(cfg.EntityBroadcastRangePercentage))
	// vanilla: DedicatedServer.initServer sets GameType on every start;
	// Main.createNewWorldData names only a new world.
	demoWorld := cfg.Demo && !s.world.Existed()
	if demoWorld {
		// vanilla: MinecraftServer.DEMO_SETTINGS
		s.setDefaultGameMode(0)
	} else {
		s.setDefaultGameMode(configGameMode(cfg.GameMode))
	}
	if debugSetup {
		s.setDefaultGameMode(3)
	}
	if demoWorld {
		s.world.SetLevelName(demoLevelName)
	} else if !s.world.Existed() {
		// Unlike vanilla, which stores level-name verbatim, the folder's base
		// name, so a path (as tests pass) still names the world.
		s.world.SetLevelName(filepath.Base(cfg.LevelName))
	}
	s.forceGameMode.Store(cfg.ForceGameMode)
	if demoWorld {
		s.world.SetDefaultDifficulty(2)
	} else {
		s.world.SetDefaultDifficulty(configDifficulty(cfg.Difficulty))
	}
	// The op list is read on first use, not here, so a test can point the
	// server at a file it just wrote.
	s.ops = newOpsStore(opsFile)
	s.opLevel.Store(int32(cfg.OpPermissionLevel))
	s.viewDistance.Store(int32(cfg.ViewDistance))
	s.simulationDistance.Store(int32(cfg.SimulationDistance))
	s.enforceWhitelist.Store(cfg.EnforceWhitelist)
	// The ban lists, white-list and user cache are likewise read on first use.
	s.bans = newBanStore(bannedPlayersFile, bannedIPsFile)
	s.whitelist = newWhitelistStore(whitelistFile, cfg.WhiteList)
	s.usercache = newUserCacheStore(filepath.Join(cfg.Universe, usercacheFile))
	s.configPath = config.FileName
	slog.Info(fmt.Sprintf("Done (%.3fs)! For help, type \"help\"", time.Since(levelStart).Seconds()))
	built = true
	s.enablePlugins()
	initialized = true
	return s, nil
}

// generateKeyPair creates the RSA key pair used to negotiate encryption.
func (s *Server) generateKeyPair() error {
	// 1024 bits like vanilla: the key only protects the shared secret exchange,
	// and older clients may not accept more.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return fmt.Errorf("generate key pair: %w", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return fmt.Errorf("encode public key: %w", err)
	}
	s.privateKey, s.publicKey = key, der
	return nil
}

// Listen starts the TCP listener and accepts connections.
func (s *Server) Listen() error {
	addr := s.Config.Addr()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	if err := s.listenManagement(); err != nil {
		return err
	}
	slog.Info("server started", "addr", addr, "version", protocol.GameVersion, "protocol", protocol.Version)
	s.startTick()
	// RCON serves beside the game listener with enable-rcon on, closing on the
	// same stop request.
	go s.listenRcon()
	// The query port starts once the game listener has.
	// vanilla: DedicatedServer.initServer
	s.startQuery()
	s.mgmt.ServerStarted()

	// A stop request closes the listener under the accept loop, which reads
	// the closure as the stop, not a failed accept. The watch ends with the
	// listen, so a server stopping for its own reasons leaves no watcher.
	watching := make(chan struct{})
	defer close(watching)
	go func() {
		select {
		case <-watching:
		case <-s.stopReq:
			listener.Close()
		}
	}()

	// Accept errors are usually transient (out of file descriptors, say), so
	// retry with a growing delay rather than spin a core and flood the log.
	var delay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				select {
				case <-s.stopReq:
					return nil
				default:
					return fmt.Errorf("accept: %w", err)
				}
			}
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			slog.Error("accept failed, retrying", "error", err, "delay", delay)
			time.Sleep(delay)
			continue
		}
		delay = 0
		go s.HandleConnection(conn)
	}
}

// HandleConnection serves a single connection until it is closed.
func (s *Server) HandleConnection(conn net.Conn) {
	log := slog.With("remote", s.loggableAddr(conn.RemoteAddr()))
	c := &client{srv: s, conn: protocol.NewConn(conn), log: log, gone: make(chan struct{})}
	c.pendingAck.Store(-1)
	c.worldScope = worldScope{srv: s, in: c.dimension, by: c.Profile}
	c.conn.Trace = log
	defer c.conn.Close()

	if err := c.serve(); err != nil {
		// A client closing the connection is normal for status pings and quitting
		// players, not worth a warning.
		if isDisconnect(err) {
			c.log.Debug("connection closed", "state", c.conn.State, "error", err)
		} else {
			c.log.Warn("connection failed", "state", c.conn.State, "error", err)
		}
	}
	c.leave()
	close(c.gone)
}

// respawnPos is a player's spawn point with vanilla's companions: its
// dimension, the angles as the file stores them (yaw folded into
// [-180, 180), pitch clamped) and the forced flag (true for /spawnpoint,
// false for a bed). vanilla: ServerPlayer.setRespawnPosition
type respawnPos struct {
	pos    world.SpawnPos
	dim    string
	yaw    float32
	pitch  float32
	forced bool
}

// client is a connection and its state from handshake to play.
type client struct {
	// effectCause is what the caller names for the next effect change.
	effectCause    effect.Cause
	statusProtocol int // handshake protocol for the server-list callback
	// Immutable after registration; slot is reused only after removal.
	slot           int
	slotGeneration uint64
	srv            *Server
	conn           *protocol.Conn
	log            *slog.Logger
	worldScope

	profile player.Profile
	player  *player.Player

	lastHealth sentHealth

	pluginDisplayName       atomic.Pointer[protocol.Text]
	pluginCarried           pluginCarried
	pluginListName          atomic.Pointer[protocol.Text]
	pluginLocale            atomic.Pointer[string]
	pluginViewDistance      atomic.Int32
	pluginBrand             atomic.Pointer[string]
	pluginFirstPlayed       time.Time
	pluginConnectionMu      sync.Mutex
	pluginConnectionPhase   atomic.Int32
	pluginConnectionExposed atomic.Bool
	pluginConnecting        atomic.Bool
	pluginLoginBypass       uint8
	pluginDeparture         atomic.Pointer[pluginDeparture]
	pluginKickPending       atomic.Bool
	pluginQuitAt            atomic.Int64
	pluginMu                sync.Mutex
	pluginCookies           map[string][]pluginCookieRequest
	pluginPacks             map[uuid.UUID]bool
	pluginAttachments       []*pluginPermissionAttachment
	// pluginReady permits handles during the initial tree event, before mailbox release.
	pluginReady atomic.Bool

	// velocityDirty is an entity-tick shove awaiting the next travel step; tick loop only.
	velocityDirty bool
	// velocityFlushFirst sends the pending shove before travel ages it.
	velocityFlushFirst bool

	// transferred is set when the connection came in on the Transfer intent.
	transferred bool

	// blockUseConsumed is set by placeBlock when the block's own use answered
	// the click; connection goroutine only.
	blockUseConsumed bool
	// denyBlockUse and denyItemUse are a PlayerInteractEvent listener's DenyBlock and
	// DenyItem for the click placeBlock is handling.
	denyBlockUse, denyItemUse bool
	// defaultBlockUsed is set by placeBlock when that use was the block's
	// own, with no item, from the main hand, which default_block_use fires on.
	// vanilla: BlockBehaviour.useWithoutItem
	defaultBlockUsed bool
	// itemUsedOn is set by placeBlock when the click's item or block use was
	// consumed, which item_used_on_block fires on.
	// vanilla: InteractionResult.consumesAction
	itemUsedOn bool
	// useSwing belongs to the current interaction, on the connection goroutine.
	useSwing useSwing

	// dim is the player's dimension, one of the server's three. Atomic because
	// other goroutines read it (a broadcast picking recipients, a pickup
	// comparing worlds) and must never see a half-made change. It holds the
	// overworld until registration, which is also where a player without a
	// file joins.
	dim atomic.Pointer[dimension]

	// saved is the player's file as of login, written back with this server's
	// state laid over it. Set once before joining and only read afterwards, so
	// a shutdown save racing the connection's own cannot see it half changed.
	saved nbt.Compound

	// out queues the packets other players cause. Set once before the client
	// joins the player list, so that list's lock makes it safe to read from
	// other connections.
	out *mailbox

	// keepAlive is the client's keep-alive state, ticked by the main loop. Like
	// out it is set once before the player list join, so the tick can read it
	// under the list's lock.
	keepAlive *keepAliveTicker
	// configTicking is true while the tick may run the configuration
	// keep-alive; configTickMu makes the phase's end wait for such a tick.
	configTickMu  sync.Mutex
	configTicking bool
	// configPingOpen is true while a configuration ping may still be written.
	// configPingMu is held across that write and by the phase's end, so a ping
	// handed off before the end is written before it or not at all; one after
	// the switch to play would read as a different play packet.
	configPingMu   sync.Mutex
	configPingOpen bool

	// lastAction is the player's last action in Unix nanoseconds on the
	// spamTime clock; zero until joined, which the idle kick skips.
	// vanilla: ServerPlayer.lastActionTime
	lastAction atomic.Int64

	// rateTicks counts ticks since the rate limit's last check and rateAverage
	// is the running packets-per-second average; tick loop only. rateStarted is
	// set once the first tick took the baseline.
	rateTicks   int
	rateStarted bool
	rateAverage float64
	rateKicked  bool

	// configurationPacks is guarded by pluginConnectionMu, including its packet writes.
	configurationPacks resourcepack.Configuration

	// conductPending is set while configuration awaits Accept Code of Conduct;
	// connection goroutine only.
	conductPending bool

	// language is the last Client Information locale, which picks the Code of
	// Conduct text; en_us until one arrives.
	// vanilla: ClientInformation.createDefault
	language string

	// announced records that others were told of this arrival, so a connection
	// lost mid world-send does not announce a departure nobody saw arrive.
	announced bool

	// gone is closed once the connection has fully left (saved, off the list,
	// announced, chunks written). A second login under the name waits on it so
	// the newcomer registers only after the old one gave the name up.
	gone chan struct{}

	// replacement shares a duplicate-login kick decision with every newcomer
	// already waiting on it; cancellation leaves no departure to await.
	replacementMu sync.Mutex
	replacement   *replacementDecision

	// leaving marks a connection whose leave has begun removing its boat and
	// pearls. A save still seeing the player skips them: the snapshot would
	// lack the boat and pearls and could land after the departure's own save.
	leaving atomic.Bool

	// saveMu serialises a save of this player's file with the departure's
	// take-and-save, so a save cannot read the world after the takes and write
	// after the departure's file.
	saveMu sync.Mutex

	// disconnectReason is the message a disconnect showed the client, which
	// leave reports. Set by handlers and the tick's keep-alive timeout, read by
	// leave, hence atomic. Absent means the peer hung up, vanilla's
	// "Disconnected".
	disconnectReason atomic.Pointer[protocol.Text]

	// joinSeq is the connection's player-list position, assigned under s.mu on
	// join; a reconnect gets a fresh number, going last. Atomic: a respawn
	// writes it under s.mu while a selector sorts on it after releasing.
	joinSeq atomic.Uint64

	// dimSeq is the connection's position in its dimension's player list,
	// assigned on join or crossing in, the order of world-limited selectors.
	// Atomic: a crossing writes it while the join and selectors hold s.mu.
	// vanilla: ServerLevel.players
	dimSeq atomic.Uint64

	// loadTicks is how long the server waits for the client to finish loading
	// before the player counts as in the world: vanilla's sixty-tick timer from
	// join or respawn, cut short by Player Loaded. Until then the player cannot
	// be hurt (ServerPlayer.isInvulnerableTo) and handlers ignore what the
	// client sends (vanilla's hasClientLoaded checks), so nothing kills or
	// moves them behind "Loading terrain". Atomic: the read loop zeroes it, the
	// tick counts it down, handlers and hurt read it.
	loadTicks atomic.Int32

	// wardenTracker is the player's warden warning, guarded by Server.wardenMu.
	wardenTracker rules.WardenSpawnTracker

	// combatMu guards combat and lastClimbable; nothing is sent or locked while
	// it is held.
	combatMu sync.Mutex
	// combat is the recent hurts the death message is worded from.
	combat rules.CombatTracker
	// lastClimbable is the last climbable cell the body was in, valid until it
	// lands. vanilla: LivingEntity.lastClimbablePos
	lastClimbable    [3]int32
	hasLastClimbable bool
	// steps is the walk distance behind the player's step sounds and game
	// events, crystal its amethyst chime. Only the movement handler (and the
	// respawn, on the same goroutine) touches these and the two below.
	steps   rules.StepTracker
	crystal rules.CrystalStep
	// wasInWater is the water the last move read; waterSeen is false until
	// the first move after a join or respawn reads it, so that move makes no
	// splash. vanilla: Entity.wasTouchingWater, firstTick
	wasInWater, waterSeen bool

	// awaitingRespawn is the rest of vanilla's hasClientLoaded, its
	// waitingForRespawn flag: a dead player counts as not loaded until the
	// respawn restarts the grace, as
	// ServerGamePacketListenerImpl.markClientUnloadedAfterDeath and
	// restartClientLoadTimerAfterRespawn bracket each death. Death sets and
	// respawn clears it on their own goroutines, so it is atomic.
	awaitingRespawn atomic.Bool

	// deathTicks counts ticks a dead body has lain; bodyGone is set once past
	// corpseTicks and gone from watchers. The respawn clears both.
	// vanilla: LivingEntity.deathTime
	deathTicks atomic.Int32
	// deathKeeps is the last death's keep decision, taken by the respawn.
	deathKeeps atomic.Pointer[deathKeeps]
	bodyGone   atomic.Bool
	// holdsDropped is set once a dead player's chunk holds and simulation are
	// dropped, after the death effects; a rejoined dead player counts the
	// twenty ticks too. The respawn clears it.
	holdsDropped atomic.Bool

	// deadHealthPending holds a dead join's empty health bar until the first
	// chunk fill is out: the client does not wait for its chunk while dead, so
	// a bar sent at join would let it tick and fall through the ground. The
	// load timeout is no gate, as it can expire while chunks still generate.
	// Set by the join, sent by tickVitals after the fill; atomic.
	deadHealthPending atomic.Bool

	// chunkFillSent is set once a Chunk Batch Finished has gone out, when a
	// joining client has its chunk; it gates the dead join's bar. Atomic: the
	// chunk sender sets it, the tick reads it.
	chunkFillSent atomic.Bool

	// moveKeys holds the bits of the last Player Input, read by the cart tick
	// for a rider's nudge and by a player predicate's input. Atomic: the
	// reader writes it, the tick reads it. vanilla: ServerPlayer.lastClientInput
	moveKeys atomic.Uint32

	// wonGame keeps the end credits on: set by the first exit portal in the end
	// until the Client Command ending them respawns the player home. Meanwhile
	// portals do nothing, steps are refused and tracking leaves the body out.
	// Not saved: reconnecting goes straight home. Atomic: the portal tick
	// writes it, the reader and tracking pass read it.
	// vanilla: ServerPlayer.wonGame
	wonGame atomic.Bool

	// viewDistance is the client's render distance in chunks and skinParts its
	// skin layers, both reported in configuration. The render distance can
	// change mid-game, hence atomic: the read loop writes it while the chunk
	// sender reads it.
	viewDistance atomic.Int32
	skinParts    uint8
	leftHanded   bool

	// chatMode is the client's chat visibility: hidden may not chat, and player
	// chat reaches only full. Written by the connection's goroutine and read by
	// others' chat sends, so atomic.
	chatMode atomic.Int32

	// allowsListing is the Client Information flag allowing the player in the
	// server list sample; false until reported, read by status connections.
	// vanilla: ClientInformation.createDefault
	allowsListing atomic.Bool

	// chatSession is immutable; chatOrderMu guards publication and join reads.
	chatSession   *chatsecurity.Session
	chatDecoder   *chatsecurity.Decoder // connection goroutine only
	chatReady     bool                  // chatOrderMu; registered to queue chat behind the join snapshot
	chatRelayDone <-chan struct{}       // connection goroutine; last deferred relay before the next command

	// chatSpam and commandSpam are the chat and command spam counters
	// (chat.go), dropSpam the creative drop counter
	// (inventory_handlers.go); connection goroutine only.
	chatSpam    spamThrottler
	commandSpam spamThrottler
	dropSpam    spamThrottler

	// sentMapDecorations is the marker list last sent per map the client still
	// holds (in its slots or in frames in range); presence means the face went
	// out, and a differing list is resent. tickMaps only, on the tick loop.
	sentMapDecorations map[int32][]world.Decoration

	// mapTicks counts tickMaps calls, the clock spacing marker resends; only
	// tickMaps touches it.
	mapTicks int

	// mapSteps counts per held map, HoldingPlayer.step, picking the one-column
	// stripe each redraw scans; a map let go and held again restarts. Only
	// tickMaps touches it.
	mapSteps map[int32]uint32

	// mapDirty accumulates the pixels a held map's redraws changed since its
	// last colour patch, so one patch covers the whole rectangle. Tick
	// goroutine only (tickMaps and markMapDirty).
	mapDirty map[int32]mapRect

	// nextChatIndex is the per-recipient global index of Player Chat Message
	// packets sent to this client. Every connection's chat steps it, so chatMu
	// is held across taking the index and queueing the packet; reordering
	// between senders reads as a bad chat index to the client.
	// vanilla: ServerGamePacketListenerImpl.nextChatIndex
	nextChatIndex int32
	chatMu        sync.Mutex
	chatAcks      chatsecurity.Validator

	// kicked is set once the spam kick's Disconnect is on its way. Packets
	// already read when the counter crossed are neither broadcast nor counted,
	// so a burst is not relayed past the kick.
	kicked atomic.Bool

	// flying is whether the player toggled creative flight on, from Player
	// Abilities. It stops a creative flyer drawing as swimming in water, and is
	// saved so a player logging out mid-air returns flying. Atomic: the reader
	// and a shutdown save both read it.
	flying atomic.Bool

	// horizontalCollision is the last move packet's horizontal-collision
	// flag; a riptide spin that runs into a wall ends on it.
	// vanilla: Entity.horizontalCollision
	horizontalCollision atomic.Bool

	// bouncedAt is ticksRun+1 at the last position step that landed on a
	// bouncy block, 0 for none. The bounce clears onGround only once a server
	// tick has run after that step, so a rise handled in the same tick still
	// reads as off a ground.
	// vanilla: Entity.restituteMovementAfterCollisions
	bouncedAt atomic.Uint64

	// priorDY is the bits of the vertical movement the previous own step
	// reported, the delta movement vanilla's server player holds when the
	// next step lands.
	priorDY atomic.Uint64

	// fallFlying is whether the player is gliding on an elytra, opened by the
	// fall-flying player command. A firework's hand use rides on it; the reader
	// and the tick that ends it touch it, so atomic. Not saved: logging out
	// ends the glide, a joining body's wings closed until its client opens
	// them.
	fallFlying atomic.Bool

	// fallFlyTicks counts the ticks of the current glide; tick goroutine only.
	fallFlyTicks int

	// spin is the riptide spin in flight, under spinMu: the connection starts
	// it, the tick counts it down.
	spinMu sync.Mutex
	spin   spinSession

	// lookMu makes each appearance change and each sleep start or end (the pose
	// reads it) one step with the metadata it broadcasts, as handler and tick
	// both change them. Lock order: eatMu and bowMu, then lookMu, then s.bedMu
	// (a sleep's start claims its bed under it), then s.sleepMu (the sleeper
	// recount), then the player's own lock, the world's and s.mu.
	lookMu sync.Mutex
	// useItem is the item the raised hand's use began with, and useSpent
	// marks its stack used up before the hand drops (vanilla's useItem is
	// then empty and posts no vibration); both under lookMu.
	useItem  protocol.ItemStack
	useSpent bool

	// portalIn counts ticks in a nether portal and portalCooldown the ticks
	// before a portal works again, vanilla's cooldown that stops an arrival
	// bouncing straight back. The tick counts and the dimension change resets
	// them, hence atomic.
	portalIn       atomic.Int32
	portalCooldown atomic.Int32
	// endTravel is held by the end portal travel in flight.
	endTravel atomic.Bool

	// postMu guards postEffects, the client's post-processing effect ids in the
	// order they were added, and postSent, whether the client was last sent a
	// non-empty set.
	postMu      sync.Mutex
	postEffects []string
	postSent    bool

	// portalPrev and portalPrevWorld are the position and world at the last
	// portal tick, where the gateway sweep starts; tick loop only.
	portalPrev      player.Position
	portalPrevWorld *world.World
	// portalPrevStale is set by a teleport or respawn, which the sweep must
	// not cross. vanilla: Entity.clearMovementThisTick
	portalPrevStale atomic.Bool

	// takeXpDelay is vanilla's Player.takeXpDelay: ticks before the next orb
	// pickup, set to two by each orb and spent one a tick, so a knot of orbs
	// does not empty in one tick. The orb tick reads it and the pickup writes
	// it, hence atomic.
	takeXpDelay atomic.Int32

	// xpSeed is the file's XpSeed enchantment seed: 0 until restore sets the
	// file's, or a fresh roll if that is 0 or absent. Restore writes it and
	// every save reads it, hence atomic.
	xpSeed atomic.Int32

	// recipeBook is the player's recipe book (inventory.RecipeBook): sent whole
	// at join, changed by recipe book packets, written by saves. It is swapped,
	// never edited in place, so readers always see one whole book. The join's
	// restore sets it before the join reads it.
	recipeBook atomic.Pointer[inventory.RecipeBook]

	// stats is the player's statistics, set by the join's restore, nil before.
	stats atomic.Pointer[player.Stats]

	// advancements is the player's advancement progress, set by the join's
	// restore, nil before.
	advancements atomic.Pointer[advancement.Progress]
	// advancementAnnounceMu covers completion drains through reward and chat
	// dispatch, so command feedback cannot overtake a tick's announcement.
	advancementAnnounceMu sync.Mutex

	// scoreSampler is what read-only criteria objectives were last given; the
	// tick loop owns it, and scoreResample (a respawn) makes it start over.
	privateScoreSamplers map[*scoreboard.Board]*scoreboard.Sampler
	scoreSampler         scoreboard.Sampler
	scoreResample        atomic.Bool

	// A nil view uses the saved main board. Switching serializes the old
	// removal and new snapshot; each board locks against its live events.
	scoreboardView     atomic.Pointer[scoreboard.Board]
	scoreboardSwitchMu sync.Mutex

	// bookMu serialises recipe book swaps: handlers and the tick's unlock pass
	// both copy, change and swap a book, and a swap must not build on a book
	// changed underneath it.
	bookMu sync.Mutex

	// inventoryAdvancementMu guards the shared command/tick slot snapshot and
	// scratch through firing criteria. Taken before the player's lock.
	inventoryAdvancementMu sync.Mutex
	lastInventory          []protocol.ItemStack
	heldScratch            []int32
	changedScratch         []int32

	// traveling marks a dimension change in flight: one crossing per player at
	// a time, whoever asked (portal tick, respawn, hook), so a second cannot
	// pick the player up while the first sets them down.
	traveling atomic.Bool

	// tp is the teleport waiting on the client's accept.
	tp teleportWait

	// moveSeam, test-only, opens the window in a step between setting the
	// position and telling holders, where the pairing pass reads a stale
	// baseline. Guarded by tp.mu, which move holds over the whole step.
	moveSeam func()

	// trackMu guards tracked, seenBy and the sentFire, sentAir and shown
	// baselines below: the read loop sets them on a step and a respawn,
	// commands, portal travel and sleep wakes set tracked, and the tick
	// measures its environment deltas by sentFire and sentAir. Held over field
	// work and the tracking pass's sends but never while taking s.mu: a join
	// and the tick take s.mu first. The player's own lock may be taken under it.
	trackMu sync.Mutex

	// tracked is where others were last told this player is, so a step sends
	// the difference; regular steps wait for the movement schedule, while
	// teleports and oversized steps send at once. trackMu serialises its writers.
	tracked trackedPosition

	// movement is the per-player half of the generic tracker's movement
	// schedule; vanilla: EntityTypes.PLAYER.updateInterval(2).
	movement movementSchedule

	// seenBy are the clients holding this player's body, by UUID (vanilla's
	// ChunkMap$TrackedEntity.seenBy): move sends steps to them only, and the
	// tick settles them against tracking range. trackMu guards it; the join,
	// departures and a respawn also hold s.mu, always taking this second.
	seenBy map[uuid.UUID]*client

	// trackIdx is the client's index in this tick's present slice, written
	// only by the tick goroutine.
	trackIdx int

	// waypoints are the clients holding this player's locator-bar waypoint, by
	// online slot, the bar's counterpart to seenBy. The tick settles them against
	// the locator bar's ranges and sends each the waypoint packet; a departure,
	// respawn or crossing forgets them. trackMu guards them with seenBy.
	waypoints watcherSet[waypointLink]
	// waypointSeen is what the last waypoint pass read of this player; only
	// the tick loop touches it. waypointsForgot is set when the links were
	// dropped outside the pass, so the next one settles them again.
	waypointSeen waypointStamp
	// mobWaypoints are the mobs' waypoints on this player's bar, by mob
	// UUID; only the tick loop touches it.
	mobWaypoints    map[uuid.UUID]*mobWaypointLink
	waypointsForgot atomic.Bool

	// sentFire, sentAir and sentFrozen are what the player's own metadata last
	// said of burn, breath and freeze; the environment tick's deltas measure
	// against them so each change is sent once. The tick measures while join
	// and respawn reset, so trackMu guards them.
	sentFire     bool
	sentAir      int
	sentFrozen   int
	sentArrows   int
	sentStingers int
	// shown is the same for effect particles and extra hearts (effects.go);
	// effectsDirty is set by any effect change, from any goroutine.
	shown        bodyShown
	effectsDirty atomic.Bool

	// attackStrengthTicks is vanilla's Player.attackStrengthTicker, ticks since
	// the swing reset, scaling the next hit's damage. The tick increments it;
	// an attack, an arm swing or a main-hand item change resets it to 0.
	// Atomic: the tick winds it while the attack handlers read and reset it.
	attackStrengthTicks atomic.Int32

	// swing gates use swings on the arm swing in progress (useswing.go).
	swing swingState

	// lastAttackItem is the main hand's stack as last seen, as vanilla's
	// Player.tick keeps it: a different item resets the ticker above. Tick loop
	// only.
	lastAttackItem protocol.ItemStack

	// pendingAck is the highest block-interaction sequence still to
	// acknowledge, vanilla's ackBlockChangesUpTo: handlers record into it and
	// the tick's end sends one Acknowledge Block Change (see ackBlock). It
	// starts at -1, stored by HandleConnection before any read; anything at or
	// below records nothing. Atomic: the connection records, the main loop
	// flushes.
	pendingAck atomic.Int32

	// dig carries a survival dig between its packets and ticks: the block and
	// tick a START set, and the block a finished click left for the ticks (see
	// digState). The connection and the tick's survival pass both move it,
	// under digState's lock.
	dig digState
	// demo is the --demo script.
	demo demoState

	// equipmentDue is set by a pickup changing a visible slot; Set Equipment
	// goes out next tick, after the take and removal.
	// vanilla: LivingEntity.detectEquipmentUpdates runs before aiStep
	equipmentDue atomic.Bool

	// chunkTrigger is how the read loop asks the chunk sender for a send;
	// chunkMu orders the sender against a dimension change releasing its set.
	// loaded is the chunks held for the client (the send radius it knows plus
	// the world's load radius), sent the subset it received, pendingHolds the
	// load radius's queued takes for the sender to acquire a few at a time.
	// pendingSend is the paced send queue: held chunks in send radius not yet
	// sent, closest first, one batch per beat via sendChunkBatch. centre,
	// radius and load are what the sets were taken around, loadedWorld their
	// world; loaded is nil until the first send. These belong to the chunk
	// sender alone (plus forgetChunks once the sender is stopped under the
	// mutex), so they need no lock beyond chunkMu. chunkStop is the sender's
	// stop channel, closed via sync.OnceFunc, and chunkDone closes as it exits;
	// both stay set after a stop, so every stopper closes and waits.
	//
	// pacer holds the batch pacing driven by Chunk Batch Received. pacingMu
	// guards it, not chunkMu: the answers arrive on the read loop, which must
	// not wait on a generation the sender holds chunkMu across. generating
	// holds acquisitions for chunks not yet ready.
	// vanilla: PlayerChunkSender
	chunkTrigger atomic.Pointer[chan struct{}]
	chunkMu      sync.Mutex
	// chunkClosed, under chunkMu, is set once the connection or server is done
	// with the sender, ending restarts.
	chunkClosed bool
	chunkStop   func()
	chunkDone   chan struct{}

	// chunkBacklog is the generating and queued-hold count last seen by the
	// sender, chunkBooked whether it booked a view; ChunksPending reads both.
	chunkBacklog atomic.Int64
	chunkBooked  atomic.Bool

	loaded *chunkSet
	sent   *chunkSet
	// sentView mirrors sent, sentViewWorld its world, for goroutines sending a
	// chunk's changes to players who have it: they may not wait on chunkMu,
	// held across generation. sentViewMu guards both, with no lock taken under
	// it. vanilla: ChunkMap.isChunkTracked
	sentViewMu    sync.RWMutex
	sentView      *chunkSet
	sentViewWorld *world.World
	pendingHolds  []world.ChunkPos
	// pendingGeneration is the current view's unscheduled generation, closest first.
	pendingGeneration []world.ChunkPos
	pendingSend       []world.ChunkPos
	pendingPackets    map[world.ChunkPos]struct{}
	// wakeBeforeBatch holds the next batch's finish until the main loop has
	// ticked the chunks the join brought in.
	wakeBeforeBatch bool
	loadedWorld     *world.World
	center          world.ChunkPos
	radius          int32
	load            int32
	skipGen         bool

	generating map[world.ChunkPos]*world.ChunkRequest

	pacingMu sync.Mutex
	pacer    rules.ChunkPacer

	// tickSamples says the client asked for the F3 TPS chart samples, sent
	// while the player is an operator.
	tickSamples atomic.Bool

	// permLevel is the player's permission level, which picks the Commands
	// packet's commands. Written by /op, /deop and the management operator
	// methods on other goroutines, read by the connection; hence atomic.
	permLevel atomic.Int32

	// spawnPoint is the bed or /spawnpoint spawn, nil for the world spawn.
	// Written by the join's restore and the owning connection, read by respawn
	// and saves, hence atomic.
	spawnPoint atomic.Pointer[respawnPos]

	// camera is the entity a spectator's view follows, nil for their own eyes.
	// The command and Spectator Action set it; the survival tick snaps the body
	// to it and drops it when the holder sneaks or the camera dies; a game mode
	// change, teleport, death or dimension change clears it. Atomic: written by
	// the connection, read by the tick. vanilla: ServerPlayer.camera
	camera atomic.Pointer[cameraFollow]
	// sky is the time and weather a plugin shows this player, nil for the world's.
	sky atomic.Pointer[playerSky]
	// compassTarget is the spawn position a plugin sent this player, nil for
	// the world spawn.
	compassTarget atomic.Pointer[compassTarget]

	// abilities is what a plugin set on top of the game mode's abilities, nil
	// for none; hidden is the entity IDs this player is not shown, a
	// copy-on-write set, nil for none.
	abilities atomic.Pointer[pluginAbilities]
	hidden    atomic.Pointer[map[int32]struct{}]

	// container is the window state: the open container, the cursor item, the
	// click state ID both ends sync on, and the drag in progress. Written by
	// the reading goroutine (the only one opening, closing and clicking) and
	// the player's save path, both under the inventory lock. See containers.go.
	container containerSession

	// eatMu guards eat, the meal in progress: the connection starts and cancels
	// it, the main loop counts it down. See food.go.
	eatMu sync.Mutex
	eat   eatSession

	// brushMu guards brush, a brush hold in progress: the connection starts and
	// ends it, the main loop counts it. See brushing.go.
	brushMu sync.Mutex
	brush   brushSession

	// bowMu guards bow, the draw in progress: the connection starts and fires
	// it, the main loop counts its ticks. See bow.go.
	bowMu sync.Mutex
	bow   bowSession

	// shieldMu guards raise, the raised shield, and cooldowns, the player's item
	// cooldowns (an axe disabling the shield, an ender pearl's twenty ticks):
	// the connection starts them, the main loop counts them. Lock order: after
	// eatMu and bowMu, before lookMu. See shield.go.
	shieldMu  sync.Mutex
	raise     raiseSession
	cooldowns map[string]int32

	// spearMu guards spear, the kinetic-weapon charge. Lock order: after
	// shieldMu. See spearcharge.go.
	spearMu sync.Mutex
	spear   spearCharge

	// ownRng is the connection's dice, vanilla's LivingEntity random (unbreaking
	// wear, a thrown item's spread). Both the connection and main loop roll it,
	// so it is a newDice; ownMu guards the field. See durability.go.
	located locatedState

	ownMu  sync.Mutex
	ownRng *mrand.Rand

	// vehicle is the boat the player rides, if any: set by the mounting
	// Interact, cleared by dismount, leaving, death, a dimension change or the
	// boat breaking, from the connection and the main loop, hence atomic. The
	// boat's passenger list agrees with it under the boat lock.
	vehicle atomic.Pointer[boatEntity]

	// minecart is the cart the player rides, if any, the boat field's twin: set
	// by the mounting Interact, cleared by dismount, leaving, death, a
	// dimension change or the cart breaking. The cart's passenger list agrees
	// with it under the cart lock.
	minecart atomic.Pointer[minecartEntity]

	// mount is the mob the player sits on, if any; mobEntity.rider agrees with
	// it under the mob lock.
	mount atomic.Pointer[mobEntity]
	// cushion is the cushion the player sits on, if any; the cushion's
	// riders agree with it under the fixtures lock.
	cushion atomic.Pointer[fixtureEntity]
	// ridePrev is where the last tick found the player on a vehicle, for the
	// distance carried; tick only.
	ridePrev    player.Position
	ridePrevSet bool

	// vehicleOnJoin is the file's RootVehicle, the boat they left riding, held
	// for the tick after join that reseats them. pearlsOnJoin is the file's
	// ender_pearls, the pearls left flying, held for the tick that restores
	// them. Login writes and the tick takes them, on two goroutines, kept apart
	// by the pointer swaps.
	vehicleOnJoin atomic.Pointer[nbt.Compound]
	pearlsOnJoin  atomic.Pointer[[]nbt.Compound]
}

// creativeClient reports whether c is a creative player; nil-safe because
// the owner a projectile, thrown item or blast names may have left before
// its hurt lands. vanilla: DamageSource.isCreativePlayer
func creativeClient(c *client) bool {
	return c != nil && c.player.GameMode() == protocol.GameModeCreative
}

// handle reads the handshake and hands the connection to the state it asks for.
func (c *client) handle() error {
	if err := c.conn.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}

	// The first byte of a legacy ping is never a valid packet length. With
	// status off the bytes go down the normal pipeline and fail the handshake.
	// vanilla: ServerConnectionListener (LegacyQueryHandler only if repliesToStatus)
	if first, err := c.conn.Peek(1); err != nil {
		return fmt.Errorf("peek: %w", err)
	} else if first[0] == 0xFE && c.srv.enableStatus.Load() {
		return c.legacyPing()
	}

	packetID, data, err := c.conn.ReadPacket()
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if packetID != protocol.IDHandshake {
		return fmt.Errorf("expected handshake, got packet 0x%02X", packetID)
	}

	handshake, err := protocol.ReadHandshake(data)
	if err != nil {
		return err
	}
	c.log.Debug("handshake",
		"protocol", handshake.ProtocolVersion,
		"addr", handshake.ServerAddress,
		"port", handshake.ServerPort,
		"nextState", handshake.NextState)

	switch handshake.NextState {
	case protocol.NextStateStatus:
		if !c.srv.enableStatus.Load() {
			c.log.Debug("status request ignored (enable-status=false)")
			return nil
		}
		c.conn.State = protocol.StateStatus
		c.statusProtocol = int(handshake.ProtocolVersion)
		return c.status()

	case protocol.NextStateLogin:
		c.conn.State = protocol.StateLogin
		return c.loginAndPlay(handshake)

	case protocol.NextStateTransfer:
		// A transfer connection is a login from another server, refused unless
		// accepts-transfers allows it.
		// vanilla: ServerHandshakePacketListenerImpl.handleIntention
		c.conn.State = protocol.StateLogin
		if !c.srv.acceptsTransfers.Load() {
			return c.rejectLoginComponent(protocol.Text{
				Translate: "multiplayer.disconnect.transfers_disabled",
			})
		}
		c.transferred = true
		return c.loginAndPlay(handshake)

	default:
		return fmt.Errorf("unknown next state %d", handshake.NextState)
	}
}

// nextEntityID hands out the entity ID identifying a player in the world.
func (s *Server) nextEntity() int32 {
	return s.nextEntityID.Add(1)
}

// appendToLevel puts the player last in its dimension's player list (join
// or dimension change), the order world-limited selectors resolve in.
func (s *Server) appendToLevel(c *client) {
	d := c.dimension()
	c.dimSeq.Store(d.listCounter.Add(1))
}

// appendToPlayerList puts the player last in the player list, as a join or
// respawn does. Caller holds s.mu.
func (s *Server) appendToPlayerList(c *client) {
	s.joinCounter++
	c.joinSeq.Store(s.joinCounter)
}

// addPlayer registers a connected player and shows them to everyone, and
// returns the connections the newcomer must be shown in turn: only those in
// their dimension, since a body exists only where it stands. The tab list
// is server-wide and the join fills it from the connections.
//
// Registering and revealing are one step under the lock, so two players
// joining at once neither miss each other nor are told twice: the second
// finds the first in its returned list, and the first is told of the second
// here. Players in other dimensions get only the tab-list entry.
//
// A name already in the world is not refused: that connection is told to
// leave and this waits for it, so the newcomer registers only after the old
// one has saved, left the list and announced its departure. vanilla:
// ServerLoginPacketListenerImpl.verifyLoginAndFinishConnectionSetup,
// PlayerList.disconnectAllPlayersWithProfile
//
// Unlike vanilla, which kicks during login, the kick comes here after
// configuration, so a connection abandoned before this leaves the old body
// in place.
//
// The player's saved state is read under the same lock, before anyone sees
// them, so a quick reconnect loads the file the old connection already
// wrote: its slot is given up only after the departure saved.
func (s *Server) addPlayer(c *client) ([]*client, error) {
	if s.shuttingDown() {
		return nil, errShutdownRefusal
	}
	// Cheap refusal before draining a duplicate login; the check under the
	// lock below is the one that counts.
	// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
	if reason, refused := s.listRefusal(c.profile, c.remoteIP(), c.pluginLoginBypass); refused {
		return nil, &joinRefusal{reason: reason}
	}
	for {
		s.chatOrderMu.Lock()
		s.mu.Lock()
		old, taken := s.clients[c.player.UUID]
		if !taken {
			break
		}
		s.mu.Unlock()
		s.chatOrderMu.Unlock()

		// The lock is released while the old connection leaves, as its save and
		// removal need it. Waiting on gone keeps the two from overlapping; the loop
		// re-checks in case a third connection took the name.
		if !old.pluginDuplicateKick(protocol.Text{Translate: "multiplayer.disconnect.duplicate_login"}) {
			return nil, &joinRefusal{reason: protocol.Text{Translate: "multiplayer.disconnect.duplicate_login"}}
		}
		// A shutdown does not wait for the old connection's drain: Shutdown waits
		// for this join, and the old client may have stopped reading.
		select {
		case <-old.gone:
		case <-s.stopping:
			return nil, errShutdownRefusal
		}
	}
	defer s.chatOrderMu.Unlock()
	defer s.mu.Unlock()

	// Checked under the lock Shutdown closes stopping under, so a player
	// registered here is in the list Shutdown disconnects and saves.
	if s.shuttingDown() {
		return nil, errShutdownRefusal
	}

	// Rechecked under the lock: during the drain a ban or white-list removal
	// could land when no s.clients entry existed to kick.
	// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
	if reason, refused := s.listRefusal(c.profile, c.remoteIP(), c.pluginLoginBypass); refused {
		return nil, &joinRefusal{reason: reason}
	}

	// Capacity is rechecked under the lock: login checked without it, so a
	// slot free then may be taken by now.
	// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
	if len(s.clients) >= int(s.maxPlayers.Load()) && !s.ops.bypassesPlayerLimit(c.player.UUID) && !c.pluginRefusalBypassed(protocol.Text{Translate: "multiplayer.disconnect.server_full"}) {
		return nil, &joinRefusal{
			reason: protocol.Text{Translate: "multiplayer.disconnect.server_full"},
			text:   fmt.Sprintf("server is full (%d players)", len(s.clients)),
		}
	}

	s.assignPlayerSlot(c)
	c.restore()
	// A body joining dead has no corpse for the others to see.
	c.bodyGone.Store(c.player.Vitals().Dead)
	// The client already knows its invisibility and glow from its game mode and
	// effects, so the flag baseline starts there and a spectator's invisible
	// bit is no first-tick change. Particles are left to the tick, which a
	// joining body's restored effects need.
	c.trackMu.Lock()
	v := c.player.Vitals()
	effects := c.player.Effects()
	c.shown.invisible = rules.InvisibleToOthers(v.GameMode, effects)
	c.shown.glowing = rules.Glowing(effects)
	c.trackMu.Unlock()

	others := make([]*client, 0, len(s.clients))
	var sameDim []*client
	for _, other := range s.clients {
		others = append(others, other)
		if other.dimension() == c.dimension() {
			visible := bodyVisibleTo(c, other)
			other.showPlayer(c.player, c.theWorld(), visible)
			if visible {
				sameDim = append(sameDim, other)
			}
		} else {
			// The tab list is the whole server's; the body is not.
			other.send(protocol.IDPlayPlayerInfoUpdate, playerInfo(playerInfoEntry{player: c.player}))
		}
	}
	s.appendToPlayerList(c)
	s.appendToLevel(c)
	s.clients[c.player.UUID] = c
	// The mailbox holds session updates and chat until after the join snapshot.
	c.chatReady = true
	// From here this player's movement is worth passing on, as everyone else
	// now has a body to move.
	//
	// The record of bodies this exchange sent is made here under the same lock:
	// the join sends the newcomer the same-dimension half later, and a tick in
	// between would otherwise pair anyone not yet recorded twice.
	c.trackMu.Lock()
	c.tracked = track(c.player.Position())
	c.seenBy = make(map[uuid.UUID]*client)
	for _, other := range sameDim {
		c.seenBy[other.player.UUID] = other
	}
	c.trackMu.Unlock()
	return others, nil
}

// removePlayer unregisters a departed player and removes them for everyone.
// The entry is removed only if still this connection's, so a fast
// reconnect is not dropped by the old connection's cleanup.
func (s *Server) removePlayer(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients[c.player.UUID] != c {
		return
	}
	delete(s.clients, c.player.UUID)
	s.freePlayerSlots = append(s.freePlayerSlots, c.slot)
	for _, other := range s.clients {
		if other.dimension() == c.dimension() {
			other.hidePlayer(c.player)
		} else {
			other.send(protocol.IDPlayPlayerInfoRemove, playerInfoRemove(c.player))
		}
	}
}

// shutdownDrainTimeout bounds Shutdown's wait for a connection to finish
// disconnecting, so a client that stopped reading cannot hold the process.
const shutdownDrainTimeout = 3 * time.Second

// Shutdown disconnects everyone with the shutdown reason, each departure
// saving its player, then saves entities, worlds and server-wide data
// before the worlds close. vanilla: MinecraftServer.stopServer
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(s.shutdown)
}

func (s *Server) shutdown() {
	slog.Info("Stopping server") // vanilla: MinecraftServer.stopServer
	s.mgmt.ServerStopping()
	// Accept nothing more, on the game port or RCON, and let no accepted
	// connection join: one still logging in would enter closing worlds and
	// never be saved. vanilla: ServerConnectionListener.stop
	s.RequestStop()
	if s.chatKeysCancel != nil {
		s.chatKeysCancel()
	}
	s.mu.Lock()
	close(s.stopping)
	s.mu.Unlock()

	// Stop the main loop before saving so its auto-save cannot race this one:
	// both would write the same chunks, and though the region file mutex keeps
	// the bytes intact, "saved chunks" would be logged twice.
	s.stopTick()
	s.disablePlugins()
	command.StopJfrOnExit(filepath.Dir(s.configPath))

	// A relight still queued would install after the save and be lost.
	s.relightWG.Wait()

	// Stop query before the world, so nothing answers a stat mid-save.
	s.stopQuery()

	// A join admitted before stopping closed is registered (so listed below) or
	// refused once this returns.
	s.joins.Wait()
	s.mu.RLock()
	connected := s.clientsLocked()
	s.mu.RUnlock()

	// One deadline bounds both the senders' stop and the drain: a client that
	// stopped reading can hold either in Write for the whole write timeout, and
	// is closed outright once it passes.
	deadline := time.Now().Add(shutdownDrainTimeout)

	// Senders and departures give up chunks, and a released chunk removes its
	// entities only on a tick's pass, which no longer runs: hold each loaded
	// chunk until the worlds close so saveMobs still finds and writes it.
	for _, d := range s.allDims() {
		for _, pos := range d.w.LoadedChunkPositions() {
			d.w.HoldIfLoaded(pos)
		}
	}

	// Stop all chunk senders first, so the Disconnect is not queued behind a
	// fill and no paced take runs on a closing world.
	senders := make(map[*client]chan struct{}, len(connected))
	for _, c := range connected {
		done := make(chan struct{})
		senders[c] = done
		go func() {
			defer close(done)
			c.closeChunkSender()
		}()
	}
	for _, done := range senders {
		select {
		case <-done:
		case <-time.After(time.Until(deadline)):
		}
	}

	// Tell every connected player the server is closing, so the client shows
	// "Server closed" rather than a lost connection; its leave saves them once,
	// with their boat and pearls in flight, before entities and chunks are
	// written below. Waiting for the drain gets the Disconnect onto the wire
	// before the process closes the sockets. vanilla: PlayerList.removeAll
	for _, c := range connected {
		c.disconnectAfterDrain(protocol.Text{Translate: "multiplayer.disconnect.server_shutdown"})
	}
	var late []*client
	for _, c := range connected {
		select {
		case <-c.gone:
		case <-time.After(time.Until(deadline)):
			slog.Warn("closing a client that did not disconnect in time", "player", c.player.Name)
			c.conn.Close()
			late = append(late, c)
		}
	}
	// A closed socket fails all reads and writes, so a late connection's sender
	// and leave finish next; they get one more bounded wait, and a leave still
	// running is shut out of the worlds by worldsClosed.
	lateDeadline := time.Now().Add(shutdownDrainTimeout)
	for _, c := range late {
		select {
		case <-senders[c]:
		case <-time.After(time.Until(lateDeadline)):
		}
		select {
		case <-c.gone:
		case <-time.After(time.Until(lateDeadline)):
			slog.Warn("a client did not finish leaving in time and is not saved", "player", c.player.Name)
		}
	}
	// Waits for a departure still running, and turns away every later one.
	s.leaveMu.Lock()
	s.worldsClosed = true
	s.leaveMu.Unlock()

	// Entities before chunks: each mob is saved with its chunk, the entity file
	// written whole with that chunk's items, arrows, orbs and primed TNT, so a
	// restart finds them where they were.
	s.mgmt.SaveStarted()
	s.saveMobs()

	// Then every dimension's chunks, so broken blocks stay broken after a
	// restart.
	for _, d := range s.allDims() {
		d.w.Save()
		if err := d.w.Close(); err != nil {
			slog.Error("closing world", "err", err)
		}
	}

	// The /data storages and /random's named sequences go with the same save,
	// so they survive a restart.
	s.saveBorders()
	s.saveRaids()
	s.saveDragonFights()
	s.storages.Save()
	s.entityData.Save()
	// And the maps, whose id counter stops a restart reusing a map id.
	s.maps.Save()
	s.randomSequences.Save()
	// The stopwatches go with it too, so a /stopwatch survives the restart.
	s.stopwatches.Save()
	s.traderData.Save()
	s.scheduled.Save()
	s.bossBars.Save()
	s.saveScoreboard()
	s.mgmt.SaveCompleted()

	// Release the session lock once every dimension is closed, so another
	// server can open the folder.
	if s.sessionLock != nil {
		if err := s.sessionLock.Release(); err != nil {
			slog.Warn("could not release the world session lock", "error", err)
		}
	}
	// The management connections close last, so what the saves above send
	// them still goes out. vanilla: DedicatedServer.onServerExit
	s.mgmt.Close()
}

// PlayerCount returns the number of players currently in the world.
func (s *Server) PlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// onlineProfiles returns every player's profile in player-list order,
// /list's order.
func (s *Server) onlineProfiles() []player.Profile {
	clients := s.playerClients()
	profiles := make([]player.Profile, 0, len(clients))
	for _, c := range clients {
		profiles = append(profiles, c.player.Profile)
	}
	return profiles
}

// broadcast sends a packet to every player in every dimension, for
// everyone's business (chat, the clock, the tab list, the difficulty). The
// payload is shared, so it must not be changed afterwards.
func (s *Server) broadcast(packetID int32, data []byte) {
	s.broadcastExcept(nil, packetID, data)
}

// broadcastExcept sends a packet to every player but one, for what a player
// does to their own body: their client draws it itself and would fight the
// server over it.
func (s *Server) broadcastExcept(except *client, packetID int32, data []byte) {
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c != except {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()

	// Recipients are copied out so nothing done to a client (closing a lagging
	// connection, say) happens with the player list locked.
	sendToAll(nil, recipients, outbound{id: packetID, data: data})
}

// broadcastLevel sends a packet about w's level state (time, weather,
// difficulty) to the players it concerns: those in w for a plugin world, and
// for the vanilla dimensions, which share one level, those in any of them.
func (s *Server) broadcastLevel(w *world.World, packetID int32, data []byte) {
	if d := s.dimensionOfWorld(w); d != nil && d.plugin {
		s.broadcastWorld(w, packetID, data)
		return
	}
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if d := c.dimension(); d == nil || !d.plugin {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()
	sendToAll(nil, recipients, outbound{id: packetID, data: data})
}

// broadcastWorld sends a packet to every player in one dimension, for its
// own business (block changes, bodies, items). The payload is shared, so it
// must not be changed afterwards.
func (s *Server) broadcastWorld(w *world.World, packetID int32, data []byte) {
	s.broadcastWorldExcept(w, nil, packetID, data)
}

// broadcastWorldExcept sends a packet to every player in one dimension but
// one, for what that player does to their own body.
func (s *Server) broadcastWorldExcept(w *world.World, except *client, packetID int32, data []byte) {
	s.broadcastPacketsWorldExcept(w, except, []outbound{{id: packetID, data: data}})
}

// broadcastPacketsWorldExcept is broadcastWorldExcept for several packets,
// queued to each recipient in order.
func (s *Server) broadcastPacketsWorldExcept(w *world.World, except *client, packets []outbound) {
	s.broadcastBodyWorldExcept(w, except, packets, false)
}

// broadcastBodyWorldExcept is broadcastPacketsWorldExcept that, with
// onlyVisible, reaches only the viewers shown except's body.
func (s *Server) broadcastBodyWorldExcept(w *world.World, except *client, packets []outbound, onlyVisible bool) {
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c != except && c.theWorld() == w && (!onlyVisible || bodyVisibleTo(except, c)) {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()

	sendPacketsToAll(w, recipients, packets)
}

// broadcastNear sends a packet to every player in one dimension within
// radius blocks of (x, y, z), vanilla's PlayerList.broadcast earshot. The
// payload is shared, so it must not be changed afterwards.
func (s *Server) broadcastNear(w *world.World, x, y, z, radius float64, packetID int32, data []byte) {
	s.broadcastNearExcept(w, nil, x, y, z, radius, packetID, data)
}

// broadcastNearExcept is broadcastNear without one player, for a sound the
// actor needs on their own connection ahead of the packets that follow.
func (s *Server) broadcastNearExcept(w *world.World, except *client, x, y, z, radius float64, packetID int32, data []byte) {
	s.mu.RLock()
	var recipients []*client
	sq := radius * radius
	for _, c := range s.clients {
		if c == except || c.theWorld() != w {
			continue
		}
		p := c.player.Position()
		dx, dy, dz := p.X-x, p.Y-y, p.Z-z
		if dx*dx+dy*dy+dz*dz < sq {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()

	sendToAll(w, recipients, outbound{id: packetID, data: data})
}

// levelEventRange is a level or block event's reach in blocks, from the
// block's corner. vanilla: ServerLevel.levelEvent, ServerLevel.runBlockEvents
const levelEventRange = 64

// broadcastChunkExcept sends a packet about w's chunk at pos to every
// player but except who has the chunk; others would drop it.
// vanilla: ChunkHolder.broadcast (ChunkMap.getPlayers(pos, false))
func (s *Server) broadcastChunkExcept(w *world.World, except *client, pos world.ChunkPos, packetID int32, data []byte) {
	s.mu.RLock()
	var recipients []*client
	for _, c := range s.clients {
		if c != except && c.theWorld() == w && c.tracks(w, pos) {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()

	sendToAll(w, recipients, outbound{id: packetID, data: data})
}

// broadcastBlockUpdate sends the Block Update of (x, y, z) to the players
// who have its chunk.
func (s *Server) broadcastBlockUpdate(w *world.World, x, y, z int32, state uint16) {
	s.broadcastChunkExcept(w, nil, world.ChunkPos{X: x >> 4, Z: z >> 4}, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, state))
}

// broadcastBlockChunkExcept sends a packet about the block at (x, z) to the
// players but except who have its chunk.
func (s *Server) broadcastBlockChunkExcept(w *world.World, except *client, x, z int32, packetID int32, data []byte) {
	s.broadcastChunkExcept(w, except, world.ChunkPos{X: x >> 4, Z: z >> 4}, packetID, data)
}

// broadcastLevelEvent sends a World Event at (x, y, z) to the players within
// levelEventRange of it.
func (s *Server) broadcastLevelEvent(w *world.World, event, x, y, z, data int32) {
	s.broadcastLevelEventExcept(w, nil, event, x, y, z, data)
}

// broadcastLevelEventExcept is broadcastLevelEvent without the player whose
// own client plays the event.
func (s *Server) broadcastLevelEventExcept(w *world.World, except *client, event, x, y, z, data int32) {
	if event == protocol.WorldEventJukeboxPlay || event == protocol.WorldEventJukeboxStop {
		s.allaysHearJukebox(w, x, y, z, event == protocol.WorldEventJukeboxPlay)
	}
	s.broadcastNearExcept(w, except, float64(x), float64(y), float64(z), levelEventRange,
		protocol.IDPlayWorldEvent, worldEvent(event, x, y, z, data))
}

// broadcastBlockEvent sends a Block Action at (x, y, z) to the players within
// levelEventRange of it.
// vanilla: ServerLevel.runBlockEvents
func (s *Server) broadcastBlockEvent(w *world.World, x, y, z int32, data []byte) {
	s.broadcastNear(w, float64(x), float64(y), float64(z), levelEventRange, protocol.IDPlayBlockAction, data)
}

// broadcastSoundPacketExcept sends a built Sound Effect to players but
// except within its earshot, read back from the packet: an inline event's
// fixed range, or the volume's earshot, from the sound's position.
// vanilla: ServerLevel.playSeededSound, SoundEvent.getRange
func (s *Server) broadcastSoundPacketExcept(w *world.World, except *client, payload []byte) {
	r := protocol.NewReader(payload)
	fixed := float32(-1)
	if r.ReadVarInt() == 0 { // an inline event: its name, then its range
		r.ReadString(32767)
		if r.ReadBool() {
			fixed = r.ReadF32()
		}
	}
	r.ReadVarInt() // category
	x, y, z := float64(r.ReadI32())/8, float64(r.ReadI32())/8, float64(r.ReadI32())/8
	volume := r.ReadF32()
	if r.Err() != nil {
		// Not a packet soundEffectAt built: no earshot to keep it within.
		s.broadcastWorldExcept(w, except, protocol.IDPlaySoundEffect, payload)
		return
	}
	radius := soundRange(volume)
	if fixed >= 0 {
		radius = float64(fixed)
	}
	s.broadcastNearExcept(w, except, x, y, z, radius, protocol.IDPlaySoundEffect, payload)
}

// soundRange is vanilla PlayerList.broadcast's earshot: sixteen blocks, or
// volume times sixteen when the volume exceeds one.
func soundRange(volume float32) float64 {
	if volume > 1 {
		return 16 * float64(volume)
	}
	return 16
}

// soundHolderRange is SoundEvent.getRange for a resolved playback holder.
func soundHolderRange(sound protocol.SoundHolder, volume float32) float64 {
	if sound.Name != "" && sound.HasRange {
		return float64(sound.Range)
	}
	return soundRange(volume)
}

// broadcastVoice sends a mob voice packet to players in earshot of
// (x, y, z); nothing for an empty event or a silent body.
func (s *Server) broadcastVoice(w *world.World, kind entity.MobType, silent bool, x, y, z float64, sound protocol.SoundHolder, volume, pitch float32) {
	if sound.ID < 0 && sound.Name == "" || silent {
		return
	}
	s.broadcastNear(w, x, y, z, soundRange(volume), protocol.IDPlaySoundEffect,
		soundEffectHolderAt(sound, mobSoundCategory(kind), x, y, z, volume, pitch))
}

func (s *Server) broadcastVoiceID(w *world.World, kind entity.MobType, silent bool, x, y, z float64, sound registry.Sound, volume, pitch float32) {
	if sound == registry.SoundNone || silent {
		return
	}
	s.broadcastSoundID(w, sound, mobSoundCategory(kind), x, y, z, volume, pitch)
}

// pendingSound is a positioned sound effect, kept to play once a lock is let go.
type pendingSound struct {
	w             *world.World
	sound         protocol.SoundHolder
	category      int32
	x, y, z       float64
	volume, pitch float32
}

// broadcastSoundHolder plays a resolved registered or inline sound within earshot.
func (s *Server) broadcastSoundHolder(w *world.World, sound protocol.SoundHolder, category int32, x, y, z float64, volume, pitch float32) {
	s.broadcastNear(w, x, y, z, soundHolderRange(sound, volume), protocol.IDPlaySoundEffect,
		soundEffectHolderAt(sound, category, x, y, z, volume, pitch))
}

// broadcastSoundID plays a registered sound to players within earshot.
func (s *Server) broadcastSoundID(w *world.World, sound registry.Sound, category int32, x, y, z float64, volume, pitch float32) {
	s.broadcastNear(w, x, y, z, soundRange(volume), protocol.IDPlaySoundEffect,
		soundEffectIDAt(sound, category, x, y, z, volume, pitch))
}

// broadcastSoundIDExcept excludes the player whose client predicted the sound.
func (s *Server) broadcastSoundIDExcept(w *world.World, except *client, sound registry.Sound, category int32, x, y, z float64, volume, pitch float32) {
	s.broadcastNearExcept(w, except, x, y, z, soundRange(volume), protocol.IDPlaySoundEffect,
		soundEffectIDAt(sound, category, x, y, z, volume, pitch))
}

// playPending sends a sound kept until after a lock.
func (s *Server) playPending(snd pendingSound) {
	s.broadcastSoundHolder(snd.w, snd.sound, snd.category, snd.x, snd.y, snd.z, snd.volume, snd.pitch)
}

// broadcastEntitySoundID plays a registered sound following an entity.
func (s *Server) broadcastEntitySoundID(w *world.World, except *client, entityID int32, sound registry.Sound, category int32, x, y, z float64, volume, pitch float32) {
	s.broadcastNearExcept(w, except, x, y, z, soundRange(volume), protocol.IDPlayEntitySoundEffect,
		entitySoundEffectID(sound, category, entityID, volume, pitch))
}

// broadcastLight sends changed light sections to every player tracking the
// chunk. Interior chunks also need these arrays: a client can leave a newly
// occupied section dark after a block update, including while frozen.
// Unlike vanilla's border-only delivery, this does not rely on client relighting.
// The payload is built once per chunk, only if someone needs it.
// vanilla: ChunkHolder.broadcastChanges
func (s *Server) broadcastLight(w *world.World, changed []world.LightChange) {
	for _, ch := range changed {
		pos := ch.Pos
		s.mu.RLock()
		var recipients []*client
		for _, c := range s.clients {
			if c.theWorld() == w && c.tracks(w, pos) {
				recipients = append(recipients, c)
			}
		}
		s.mu.RUnlock()
		if len(recipients) == 0 {
			continue
		}
		payload := w.UpdateLightPacket(pos, ch.Sky, ch.Block)
		if payload == nil {
			continue
		}
		for _, c := range recipients {
			c.sendIn(w, protocol.IDPlayUpdateLight, payload)
		}
	}
}

// relightAsync submits a light recomputation to the worker pool and
// broadcasts the result, so block changes do not block the read loop while
// light spreads through a 31x31x384 box.
func (s *Server) relightAsync(w *world.World, x, z int32) {
	s.relightAreaAsync(w, x, z, x, z)
}

// relightAreaAsync submits one recomputation over the column box, for a
// batch of changes (an explosion's crater) as one task instead of one per
// column. The result goes out with the next block-change flush, ahead of that
// flush's block packets and once per chunk however many tasks finished. A
// result that finishes after a flush waits for the next, so its light can
// follow the block it belongs to; a stopped loop sends it at once.
// vanilla: ChunkHolder.broadcastChanges
func (s *Server) relightAreaAsync(w *world.World, minX, minZ, maxX, maxZ int32) {
	s.relightBoxesAsync(w, []changeBox{{minX, minZ, maxX, maxZ}})
}

// relightBoxesAsync books the boxes' relights together and collects their
// results on one goroutine, so a batch parks one waiter however many chunks it
// touches. A box needs no relight when its columns carry no light mark: the
// caller's write set the mark before this call, so an unmarked box was already
// relit, and a relight running now covers any mark it clears. A write that
// keeps its light (a fence arm, a stair shape) never marks.
func (s *Server) relightBoxesAsync(w *world.World, boxes []changeBox) {
	var chs []<-chan []world.LightChange
	for _, b := range boxes {
		if !w.LightMarkedIn(b.minX, b.minZ, b.maxX, b.maxZ) {
			continue
		}
		s.relightsRun.Add(1)
		chs = append(chs, w.RelightAreaAsync(b.minX, b.minZ, b.maxX, b.maxZ))
	}
	if len(chs) == 0 {
		return
	}
	s.relightWG.Go(func() {
		for _, ch := range chs {
			changed := <-ch
			if !s.loopRunning() {
				s.broadcastLight(w, changed)
				continue
			}
			w.MarkLightChanged(changed)
		}
	})
}

// physicsFor returns the world's block-update engine, to which block
// changes are handed.
func (s *Server) physicsFor(w *world.World) *world.Physics {
	return s.dimensionOfWorld(w).physics
}

// obstructed reports whether any body's box (player, mob, dying corpse,
// primed TNT, falling block, boat, minecart, end crystal, armor stand,
// mannequin) in w overlaps the block, which stops a placement into someone.
// state is the block to place, as the shape matters: a fence collides as a
// post plus arms, not a cube, so a body beside the post does not obstruct.
// Each list is read under its own lock alone; none of those locks nest here.
// vanilla: BlockItem.canPlace's isUnobstructed, EntityGetter
// .isUnobstructed over its getEntities walk; a spectator is dropped by that
// walk's NO_SPECTATORS selector, not by some missing blocksBuilding, which
// the other kinds here carry too.
func (s *Server) obstructed(w *world.World, x, y, z int32, state uint16) bool {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		if boxOverlapsPlaced(x, y, z, state,
			pos.X-d.Width/2, pos.Y, pos.Z-d.Width/2,
			pos.X+d.Width/2, pos.Y+d.Height, pos.Z+d.Width/2) {
			return true
		}
	}

	s.mobsMu.Lock()
	herd := s.herdOf(w)
	for _, m := range herd.herd {
		if mobOverlapsPlaced(x, y, z, state, m) {
			s.mobsMu.Unlock()
			return true
		}
	}
	// A corpse keeps its box for its twenty-tick death animation (vanilla's
	// deathTime), refusing placements until then.
	for _, corpse := range herd.dying {
		if mobOverlapsPlaced(x, y, z, state, corpse.ent.mob) {
			s.mobsMu.Unlock()
			return true
		}
	}
	s.mobsMu.Unlock()

	s.tntMu.Lock()
	for _, it := range s.tnts {
		if it.w != w || it.tnt.Dead {
			continue
		}
		minX, minZ := it.tnt.X-entity.TNTHalfWidth, it.tnt.Z-entity.TNTHalfWidth
		if boxOverlapsPlaced(x, y, z, state, minX, it.tnt.Y, minZ,
			it.tnt.X+entity.TNTHalfWidth, it.tnt.Y+entity.TNTHeight, it.tnt.Z+entity.TNTHalfWidth) {
			s.tntMu.Unlock()
			return true
		}
	}
	s.tntMu.Unlock()

	s.fallingMu.Lock()
	for _, it := range s.fallings {
		if it.w != w || it.fb.Dead {
			continue
		}
		minX, minZ := it.fb.X-entity.FallingHalfWidth, it.fb.Z-entity.FallingHalfWidth
		if boxOverlapsPlaced(x, y, z, state, minX, it.fb.Y, minZ,
			it.fb.X+entity.FallingHalfWidth, it.fb.Y+entity.FallingHeight, it.fb.Z+entity.FallingHalfWidth) {
			s.fallingMu.Unlock()
			return true
		}
	}
	s.fallingMu.Unlock()

	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w != w || it.boat.Dead {
			continue
		}
		minX, minZ := it.boat.X-entity.BoatHalfWidth, it.boat.Z-entity.BoatHalfWidth
		if boxOverlapsPlaced(x, y, z, state, minX, it.boat.Y, minZ,
			it.boat.X+entity.BoatHalfWidth, it.boat.Y+entity.BoatHeight, it.boat.Z+entity.BoatHalfWidth) {
			s.boatsMu.Unlock()
			return true
		}
	}
	s.boatsMu.Unlock()

	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.w != w || it.cart.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
		if boxOverlapsPlaced(x, y, z, state, minX, minY, minZ, maxX, maxY, maxZ) {
			s.minecartsMu.Unlock()
			return true
		}
	}
	s.minecartsMu.Unlock()

	s.endCrystalsMu.Lock()
	for _, ent := range s.endCrystals {
		if ent.w != w || ent.crystal.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := ent.crystal.BlastBox()
		if boxOverlapsPlaced(x, y, z, state, minX, minY, minZ, maxX, maxY, maxZ) {
			s.endCrystalsMu.Unlock()
			return true
		}
	}
	s.endCrystalsMu.Unlock()

	// A marker stand lets blocks through. vanilla: ArmorStand's
	// blocksBuilding = !isMarker()
	s.armorStandsMu.Lock()
	for _, ent := range s.armorStands {
		if ent.w != w || ent.stand.Dead || ent.stand.Marker {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := ent.stand.Box().Corners()
		if boxOverlapsPlaced(x, y, z, state, minX, minY, minZ, maxX, maxY, maxZ) {
			s.armorStandsMu.Unlock()
			return true
		}
	}
	s.armorStandsMu.Unlock()

	s.mannequinsMu.Lock()
	defer s.mannequinsMu.Unlock()
	for _, ent := range s.mannequins {
		if ent.w != w || ent.mannequin.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := ent.mannequin.BlastBox()
		if boxOverlapsPlaced(x, y, z, state, minX, minY, minZ, maxX, maxY, maxZ) {
			return true
		}
	}
	return false
}

// mobOverlapsPlaced tests one mob's box (the baby's if it is one) against
// the placed state's shape.
func mobOverlapsPlaced(x, y, z int32, state uint16, m *entity.Mob) bool {
	hw := m.BbWidth() / 2
	return boxOverlapsPlaced(x, y, z, state,
		m.X-hw, m.Y, m.Z-hw, m.X+hw, m.Y+m.BbHeight(), m.Z+hw)
}

// boxOverlapsPlaced tests one body's box against the placed state, behind
// the cell prefilter.
func boxOverlapsPlaced(x, y, z int32, state uint16, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	if !cellNearBox(x, y, z, minX, minY, minZ, maxX, maxY, maxZ) {
		return false
	}
	return blockOverlapsBody(x, y, z, state, minX, minY, minZ, maxX, maxY, maxZ)
}

// cellNearBox reports whether a body's box can reach the block at all, the
// prefilter before the shape query. The cell is padded on X and Z because
// OffsetType.XZ blocks (bamboo, dripstone) sit up to 4/16 outside it
// (world.collisionOffset), and on Y because fence, bars and wall shapes
// stand half a block taller than their cell.
func cellNearBox(x, y, z int32, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	const (
		xzPad = 4.0 / 16.0
		yPad  = 8.0 / 16.0
	)
	fx, fy, fz := float64(x), float64(y), float64(z)
	return minX < fx+1+xzPad && maxX > fx-xzPad &&
		minY < fy+1+yPad && maxY > fy &&
		minZ < fz+1+xzPad && maxZ > fz-xzPad
}

// blockOverlapsBody reports whether the block overlaps a body's box, given
// by two corners, using the world's collision shapes.
func blockOverlapsBody(x, y, z int32, state uint16, minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return world.StateCollides(state, x, y, z, minX, minY, minZ, maxX, maxY, maxZ)
}

// joinRefusal is addPlayer's error for a player who may no longer join;
// reason is shown to the client.
type joinRefusal struct {
	reason protocol.Text
	text   string
}

func (e *joinRefusal) Error() string {
	if e.text != "" {
		return e.text
	}
	return "join refused: " + e.reason.Plain()
}

// errShutdownRefusal refuses a join that arrives once Shutdown has begun.
var errShutdownRefusal = &joinRefusal{
	reason: protocol.Text{Translate: "multiplayer.disconnect.server_shutdown"},
	text:   "server is shutting down",
}

// shuttingDown reports whether Shutdown has begun; a server built without
// New never shuts down.
func (s *Server) shuttingDown() bool {
	select {
	case <-s.stopping:
		return true
	default:
		return false
	}
}

// beginJoin admits a connection into the play join, or returns false once
// Shutdown has begun. An admitted join must call endJoin after addPlayer
// registers or refuses it.
func (s *Server) beginJoin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown() {
		return false
	}
	s.joins.Add(1)
	return true
}

// endJoin ends a join beginJoin admitted.
func (s *Server) endJoin() { s.joins.Done() }

// clientsLocked copies the player list; s.mu is held by the caller.
func (s *Server) clientsLocked() []*client {
	out := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, c)
	}
	return out
}
