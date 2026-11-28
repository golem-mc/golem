package server

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The server runs the three dimensions a vanilla server runs. Each is one
// World of its own — its own generator off the shared level seed, its own
// region files in the vanilla layout, its own physics — and each player is in
// exactly one of them at a time. The overworld's world is also the one the
// clock, the weather, the difficulty, the seed and the spawn live in, the way
// the level.dat at the world's root is the overworld's; the nether and the end
// keep only their chunks, under dimensions/minecraft/<name>.

// dimension is one of the three worlds: its wire name and the ID of its type
// in the minecraft:dimension_type registry, the world itself, the physics of
// the blocks in it, and its mobs.
type dimension struct {
	name   string
	typeID int32
	// here is the worldScope.in of a command running in this dimension, built
	// once because every execute context asks for it.
	here func() *dimension
	gen  world.Generator
	w    *world.World
	// sleepStatus is the dimension's SleepStatus, guarded by Server.sleepMu.
	sleepStatus sleepCounts
	// plugin marks a plugin-created dimension, whose seed and spawn are its own.
	plugin bool
	// genSpec is the terrain a plugin world was asked for, kept for its
	// plugin_world.dat.
	genSpec world.PluginGenSpec
	// folder is the world folder a plugin world was opened over, and folderDim
	// the dimension of it that the world stores; empty for a world under the
	// server's own folder.
	folder, folderDim string
	physics           *world.Physics
	// fireSpread is the fire_spread_radius_around_player gate's view of the
	// players, read on the tick goroutine.
	fireSpread fireSpreadView
	// listCounter numbers the players as they join this dimension's player
	// list, a player who crosses in appended. It is the order a world-limited
	// selector resolves in.
	listCounter atomic.Uint64
	// mobs is the dimension's mob state: the entity.Mobs the tick advances,
	// the bookkeeping the server keeps around it, and the per-chunk record
	// of which mobs have been woken into the world. It is guarded by the
	// server's mobsMu, the way the item entities are by itemsMu.
	mobs *mobHerd
	// Mob loot scratch belongs to the tick goroutine. Ground and view share indices.
	mobLootGround []*itemEntity
	mobLootView   []entity.LootItem
	mobLootGrid   entity.BoxGrid
	// mobLootAreas are the chunks of the mobs that may take items; mobLootSeen
	// is gatherLootItems' visited set.
	mobLootAreas []lootArea
	mobLootSeen  map[world.ChunkPos]struct{}
	// spawnTuning is the plugin-set natural spawn limits and tick rates.
	spawnTuning entity.SpawnTuning
	// colliders belongs to each dimension/pass on the tick goroutine; grids
	// and per-mover exclusions must never be queried concurrently.
	colliders [collisionPassCount]mobCollisionWorld
	// pistons holds the other kinds' per-step grids, each guarded by its kind's lock.
	pistons bodyPistonGrids
	// navChanges is the cells whose collision shape changed since the last mob
	// tick, for the path recompute; navSpare is the emptied buffer the tick
	// swaps in. navChangesMu is a leaf lock, taken with nothing else.
	navChangesMu sync.Mutex
	navChanges   []entity.Waypoint
	navSpare     []entity.Waypoint
	// vehicles is the boats and minecarts the mob tick's cramming counts,
	// refilled each mob tick before the mob lock.
	// vehiclesCrammed is whether this tick's rule fills vehicles at all.
	vehicles        entity.PushCrowd
	vehiclesCrammed bool
	// touch is the plate and tripwire body snapshot shared within one pass.
	// Lock order: its mutex is taken under the engine lock and before the
	// entity locks; nothing takes it while holding an entity lock.
	touch touchWindow
	// sulfur is the bodies the potent sulfur geysers share within one tick.
	sulfur sulfurBodies
	// tickingBuf is the block entity tick's list, reused across ticks.
	tickingBuf []*world.BlockEntity
	// Chunk positions are owned by their respective passes on the tick goroutine.
	randomTickPositions []world.ChunkPos
	// randomTickKey is what randomTickPositions was gathered from; the sorted
	// list is reused while it holds.
	randomTickKey      randomTickKey
	spawningPositions  []world.ChunkPos
	spawnablePositions []entity.ChunkPos
	// spawnBodies is the natural spawning pass's obstruction list, reused across ticks.
	spawnBodies entity.SpawnBodies
	// phantoms is the dimension's insomnia spawner, ticked on the main loop;
	// nil outside the overworld.
	// vanilla: MinecraftServer.createLevels, overworldCustomSpawners
	phantoms *entity.PhantomSpawner
	// chests and chestOpen are what a cat's chest seat asks, guarded by mobsMu;
	// chestOpen is chests.open, made once.
	chests    chestWatch
	chestOpen func(x, y, z int32) bool
	// golemChests is what a copper golem's transport reaches, guarded by mobsMu.
	golemChests golemChests
	// compassProbe is the mob pass's lodestone scratch, guarded by mobsMu and
	// reused across mobs and ticks.
	compassProbe lodestoneProbe
	// drawTrades queues requests under mobsMu without retaining a tick collector.
	drawTrades func(*entity.Mob)
	tradeDraws []mobTradeDraw
	// cats is the dimension's stray cat spawner; nil outside the overworld.
	// vanilla: MinecraftServer.createLevels, overworldCustomSpawners
	cats *entity.CatSpawner
	// traders is the dimension's wandering trader spawner, whose saved
	// countdown is the server's traderData; nil outside the overworld.
	traders *entity.WanderingTraderSpawner
	// patrols is the unsaved overworld patrol countdown.
	patrols *entity.PatrolSpawner
	// siege is the unsaved overworld village siege.
	siege *entity.VillageSiege
	// border is the dimension's own world border.
	border *world.Border
	// raids is the dimension's raids, guarded by the server's raidsMu.
	raids *raid.Raids
	// dragonFight is the dimension's ender dragon fight, nil when its
	// dimension type has has_ender_dragon_fight false.
	// vanilla: ServerLevel.dragonFight
	dragonFight *dragonFight
	// villages indexes the dimension's village points of interest; it has
	// its own lock.
	villages *rules.VillageIndex
	// villageView is the villagers' read of villages, with the tick the
	// scans age by.
	villageView *villageView
}

// dimensionByName finds one of the server's dimensions by its wire name,
// which is what a saved player's Dimension field and a test hook both carry.
// It falls back to the overworld, which is also what an unknown dimension in
// a player file means: the player stands in the world's own dimension rather
// than nowhere.
func (s *Server) dimensionByName(name string) *dimension {
	for _, d := range s.allDims() {
		if d.name == name {
			return d
		}
	}
	return s.allDims()[0]
}

// dimensionOfWorld finds the dimension a world is, which is what broadcasts
// of block changes need: a change belongs to the world it happened in, and
// its audience is whoever is standing there.
func (s *Server) dimensionOfWorld(w *world.World) *dimension {
	for _, d := range s.allDims() {
		if d.w == w {
			return d
		}
	}
	return s.allDims()[0]
}

// dragonFightOf returns w's dragon fight, or nil when w is unknown or has none.
func (s *Server) dragonFightOf(w *world.World) *dragonFight {
	for _, d := range s.allDims() {
		if d.w == w {
			return d.dragonFight
		}
	}
	return nil
}

// makeDimensions builds the three worlds: the overworld first, because its
// construction settles the level seed and writes the level.dat the other two
// derive their generators from, then the nether and the end under its DIM
// folders. A flat overworld still gets a nether and an end of their own —
// there is no flat flavour of either, and the vanilla server's are what the
// generators make. A shared nether may replace the level seed for the nether,
// and generates its chunks once for every server sharing it.
func (s *Server) makeDimensions(overworldGen world.Generator, shared sharedNether) error {
	overworld := &dimension{
		name:     world.DimensionName,
		typeID:   registry.MustID("minecraft:dimension_type", world.DimensionType),
		gen:      overworldGen,
		w:        s.world,
		phantoms: &entity.PhantomSpawner{},
		cats:     &entity.CatSpawner{},
		patrols:  &entity.PatrolSpawner{},
		siege:    &entity.VillageSiege{},
		traders:  entity.NewWanderingTraderSpawner(),
	}
	seed := s.world.SeedValue()

	netherSeed, fixture := seed, (*world.GenerationFixture)(nil)
	if shared != nil {
		netherSeed, fixture = shared(seed, s.Config.GenerateStructures)
	}
	netherGen := world.NewNetherGenerator(netherSeed)
	if !s.Config.GenerateStructures {
		netherGen.DisableStructures()
	}
	if g, ok := vanillaStemGenerator(s.packStems, world.NetherName, func(gen nbt.Compound, shape world.DimensionShape) (world.Generator, error) {
		return world.StemGenerator(gen, shape, netherSeed, s.Config.GenerateStructures)
	}).(*world.NetherGenerator); ok {
		netherGen, fixture = g, nil
	}
	netherGen.SetStructureMobFinalizer(s.structureMobFinalizer(world.NetherName))
	nether := &dimension{
		name:   world.NetherName,
		typeID: registry.MustID("minecraft:dimension_type", world.NetherType),
		gen:    netherGen,
		w:      world.NewDimensionWorld(s.levelDir, true, netherGen),
	}
	if fixture != nil {
		nether.w.GenerateFrom(fixture)
	}

	endGen := world.NewEndGenerator(seed)
	if g, ok := vanillaStemGenerator(s.packStems, world.EndName, func(gen nbt.Compound, shape world.DimensionShape) (world.Generator, error) {
		return world.StemGenerator(gen, shape, seed, s.Config.GenerateStructures)
	}).(*world.EndGenerator); ok {
		endGen = g
	}
	end := &dimension{
		name:   world.EndName,
		typeID: registry.MustID("minecraft:dimension_type", world.EndType),
		gen:    endGen,
		w:      world.NewDimensionWorld(s.levelDir, false, endGen),
	}

	dims := append([]*dimension{overworld, nether, end}, s.packDimensions(seed)...)
	s.dimsList.Store(&dims)
	for i, d := range dims {
		if s.world.IsDebug() {
			d.w.SetDebug()
		}
		if err := s.initDimension(d, i > 0); err != nil {
			return err
		}
	}
	return nil
}

// allDims is the current dimension list, the overworld first. The slice is a
// snapshot that is never edited; it is empty until makeDimensions has run.
func (s *Server) allDims() []*dimension {
	if l := s.dimsList.Load(); l != nil {
		return *l
	}
	return nil
}

// addDimension appends d to the list. Callers run on the tick goroutine; the
// lock only orders them against each other.
func (s *Server) addDimension(d *dimension) {
	s.dimsMu.Lock()
	defer s.dimsMu.Unlock()
	list := append(slices.Clone(s.allDims()), d)
	s.dimsList.Store(&list)
}

// removeDimension drops d from the list.
func (s *Server) removeDimension(d *dimension) {
	s.dimsMu.Lock()
	defer s.dimsMu.Unlock()
	list := slices.DeleteFunc(slices.Clone(s.allDims()), func(o *dimension) bool { return o == d })
	s.dimsList.Store(&list)
}

// initDimension wires a built dimension to the server: its environment,
// physics, mobs, border, raids and village index, and the storage format of
// its region files, which is set before anything has been saved. followSpawn
// makes its SpawnPoint answer from the overworld's level.dat. A dimension
// other than the overworld reads the overworld's age, so a container change
// there is stamped with the tick the physics runs on rather than their own
// never-advancing age; a plugin dimension keeps its own.
func (s *Server) initDimension(d *dimension, followSpawn bool) error {
	if followSpawn {
		d.w.FollowSpawnOf(s.world)
	}
	if err := d.w.LoadEnvironment(); err != nil {
		return fmt.Errorf("dimension %s environment: %w", d.name, err)
	}
	d.here = func() *dimension { return d }
	d.w.SetChunkHooks(s.chunkHooksFor(d))
	d.physics = world.NewPhysics(d.w)
	d.physics.SetMaxChainedNeighborUpdates(s.Config.MaxChainedNeighborUpdates)
	d.physics.SetBlockHooks(physicsHooks{s: s, dim: d})
	d.physics.SetCopperChestViewers(copperChestViewers{s: s, w: d.w})
	d.physics.SetVineSpreadRule(func() bool { return d.w.BoolRule(world.RuleSpreadVines) })
	d.mobs = newMobHerd(d.w)
	d.mobs.lootExtra = s.mobLootExtra
	d.mobs.pistonPopulated = &s.pistonPopulated
	d.w.SetSaveFailureHook(s.sendLowDiskSpaceWarning)
	d.w.SetSignResolver(signResolver{s: s, w: d.w})
	d.w.SetBookResolver(bookResolver{s: s, w: d.w})
	d.border = d.w.LoadBorder(s.world)
	d.border.SetAbsoluteMax(int32(s.Config.MaxWorldSize))
	d.w.SetBorder(d.border)
	s.loadDimensionRaids(d)
	if d.w.HasEnderDragonFight() {
		d.dragonFight = s.loadDragonFight(d)
	}
	switch {
	case d.plugin:
		d.w.SetOwnLevel(s.world)
	case d.w != s.world:
		d.w.SetTimeSource(s.world)
	}
	return d.w.SetSaveFormat(s.Config.RegionFileCompression, s.Config.SyncChunkWrites)
}

// sharedNether gives the nether's seed and the fixture its chunks come from,
// for a level of levelSeed whose structures are on or off.
type sharedNether func(levelSeed int64, structures bool) (seed int64, fixture *world.GenerationFixture)

// dimension returns the dimension the player is in, which is set before the
// player joins the world and only changes through a dimension change.
func (c *client) dimension() *dimension {
	return c.dim.Load()
}

// theWorld returns the world of the dimension the player is in, which is what
// every player-specific business with blocks, chunks, physics, items, block
// entities, containers and lighting goes through: those all happen where the
// player is standing.
func (c *client) theWorld() *world.World {
	return c.dim.Load().w
}

// dimensionPayloads is the Login (play) packet's description of the world as
// far as the dimensions go: every dimension this server runs, by wire name.
func (s *Server) dimensionPayloads(b *protocol.Buffer) {
	b.PutVarInt(int32(len(s.allDims())))
	for _, d := range s.allDims() {
		b.PutString(d.name)
	}
}

// respawnPayload builds the Respawn packet for arriving in a dimension, which
// is Login (play)'s shape from the dimension type to the sea level, minus the
// connection fields and with which client data to keep at the end. isFlat is
// the overworld's answer, the world creation's for every dimension alike. The
// keep byte follows the vanilla server's own convention: a respawn after death
// keeps nothing, and a dimension change — a portal, a command's teleport —
// keeps everything, so the client carries its warm-up state, its abilities and
// the rest of what a living player has accumulated across the trip.
func respawnPayload(dim *dimension, w *world.World, v player.Vitals, kept byte, portalCooldown int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(dim.typeID)
	b.PutString(dim.name)
	putSpawnInfo(&b, w, v)
	b.PutVarInt(portalCooldown)
	b.PutVarInt(dim.w.SeaLevel())
	b.PutU8(kept)
	return b.Bytes()
}

// putSpawnInfo writes the fields Login (play) and Respawn share between the
// dimension name and the portal cooldown: obfuscated seed, game modes, the
// debug and superflat flags and the death location.
// vanilla: ServerPlayer.createCommonSpawnInfo
func putSpawnInfo(b *protocol.Buffer, w *world.World, v player.Vitals) {
	b.PutI64(rules.ObfuscateSeed(w.SeedValue()))
	b.PutVarInt(int32(v.GameMode))
	// Optional var int: 0 is absent, otherwise the mode plus one.
	if v.HasPreviousGameMode {
		b.PutVarInt(int32(v.PreviousGameMode) + 1)
	} else {
		b.PutVarInt(0)
	}
	b.PutBool(w.IsDebug()) // debug world
	b.PutBool(w.IsFlat())  // superflat world, the overworld's answer for every dimension
	b.PutBool(v.LastDeath.Set)
	if v.LastDeath.Set {
		b.PutString(v.LastDeath.Dimension)
		b.PutPosition(v.LastDeath.X, v.LastDeath.Y, v.LastDeath.Z)
	}
}

// standingAt is where a body stands at a spawn spot: the spot's column
// centre.
func standingAt(spot world.SpawnPos) player.Position {
	return player.Position{X: float64(spot.X) + 0.5, Y: float64(spot.Y), Z: float64(spot.Z) + 0.5}
}

// respawnKind is which respawn a crossing runs: none for a living one — a
// portal or a teleport — and a death's and the end credits' for the two a
// respawn request ends in, which differ in what crosses with the body: a
// death drops it for a fresh one, the credits keep everything it holds.
type respawnKind uint8

const (
	respawnNone respawnKind = iota
	respawnDeath
	respawnCredits
)

// enterDimension moves a live player to another dimension, and a dead one to
// the overworld of their respawn. It is the whole of a dimension change: the
// Respawn packet that swaps the client's world (the client unloads the old
// chunks itself — the docs say not to try), the standing-in-the-world
// sequence the swap leaves the client wanting — position, abilities, health,
// the inventory the Respawn took the client's view of, the clock, the
// weather of the dimension it lands in, and then the chunks — and the body
// being taken out of the dimension it left and put into the one it arrives
// in, for the others to see or stop seeing. move is where the player stands
// and turns on arrival.
//
// A death's respawn passes respawnDeath: the body that fell over disappears
// from whoever saw it fall rather than being repositioned, and no client
// data is kept across a death, the way it is across a living crossing. The
// destination has to be other than where the player is — a same-dimension
// respawn is the ordinary one in survival.go, and the docs warn against
// sending a Respawn for the dimension the player is already in while they
// live.
//
// A portal's travel passes asPassenger=true, the one crossing whose cart is
// carried with its rider, vanilla's Entity.handlePortal moving the vehicle's
// own passengers. Every other caller leaves it false, a command's teleport
// and a pearl dropping the seat, the way ServerPlayer.teleport removes the
// vehicle unless the transition is as a passenger.
//
// Only one change runs per player at a time: the traveling flag is the
// porter's, keeping a second crossing (a tick behind the first, a test hook
// racing one) from taking the player somewhere while their things are still
// being carried between platforms.
func (c *client) enterDimension(dim *dimension, move command.Move, respawn respawnKind, asPassenger bool) error {
	if dim == c.dimension() {
		return nil
	}
	if !c.traveling.CompareAndSwap(false, true) {
		return nil
	}
	defer c.traveling.Store(false)
	return c.enterDimensionHeld(dim, move, respawn, asPassenger)
}

// enterDimensionHeld is enterDimension with the traveling flag already held
// by the caller: a death's respawn and the end credits' take it before they
// reset the body, so a change already in flight refuses them with nothing
// half-done, and hold it through the same send-off.
func (c *client) enterDimensionHeld(dim *dimension, move command.Move, respawn respawnKind, asPassenger bool) error {
	old := c.dimension()
	// Plugin callbacks must run before the dimension switch holds the writer.
	tree := c.commandTreePacket(c.PermLevel())

	// A crossing leaves any camera behind: the view returns to the player,
	// whose new dimension the old camera's body is not in.
	c.clearCamera()

	// Whatever the player had open, their own grid included, goes back to
	// them - vanilla's Player.remove(CHANGED_DIMENSION) - and a use in progress
	// (meal, shield, bow draw) ends with nothing applied.
	// vanilla: ServerPlayer.teleport, LivingEntity.stopUsingItem
	if err := c.closeWindows(false, true, false); err != nil {
		c.log.Debug("closing the window of a travelling player failed", "error", err)
	}
	c.stopUsingItem()
	// A boat's or cart's seat is dropped here unless the crossing is as a
	// passenger: only a portal travels as one, vanilla's
	// Entity.handlePortal moving the vehicle's own passengers. A command's
	// teleport carries the vehicle itself, as a twin, and does not seat its
	// riders through this branch. A seat given back takes no step: a
	// teleport here would reach a client still in the old world, ahead of
	// the Respawn that swaps it.
	if asPassenger {
		if it := c.vehicle.Load(); it != nil {
			c.srv.moveBoatWithRiders(it, dim, move)
		} else {
			c.dismountBoat(false)
		}
		c.dismountCushion(false)
		if it := c.minecart.Load(); it != nil {
			c.srv.moveMinecartWithRider(it, dim, move.Pos)
		} else {
			c.dismountMinecart(false)
		}
	} else {
		c.leaveVehicleForTeleport()
	}
	// The dig the crossing interrupts comes off the books, its delayed
	// destroy with it - a delayed destroy's coordinates would name the old
	// dimension's block to the new world's ticks.
	c.resetDig()

	// The chunk sender has to be standing still for this: the chunks it
	// holds are the old dimension's, and they go back to the old world. It
	// is started again below, for the new one.
	c.stopChunkSender()
	c.forgetChunks(nil)

	// Out of the old dimension's view first, so that nobody sees the player
	// in two places: the body the others were looking at comes out, and the
	// tab list keeps its entry — the list is the whole server's.
	c.srv.broadcastWorldExcept(old.w, c, protocol.IDPlayRemoveEntities, removeEntity(c.player))

	left := point(c.player.Position())
	positionSync := c.placeAtMove(move)
	// The switch drains what was queued for the old world ahead of the
	// Respawn; from the store under mu it also drops the old world's late
	// packets (sendIn). The writer stays held through the send-off, so what a
	// tick queues for the body in the new world waits behind the send-off.
	release, err := c.switchDimension(dim)
	release = sync.OnceFunc(release)
	defer release()
	if err != nil {
		return err
	}
	c.srv.updateSleepingPlayerList()
	// A respawn replaces the watched body instead of carrying its watchers.
	// vanilla: PlayerList.respawn
	if respawn != respawnNone {
		c.srv.clearWatchersOf(c)
		// The fresh body is minted in the dimension it respawns into: a
		// tick between the death and the store then never sees a live body
		// in the world the Respawn is discarding. The dead body's own ticks
		// already cleared its portal count.
		c.resetRespawnBody(move.Pos, respawn)
	}
	// The Respawn clears the client's world, so every kind's record of it
	// goes. After the store: a tick re-checks the world under its lock, so
	// none can pair the old dimension's bodies again once this is done.
	c.srv.forgetObserver(c)
	// The waypoint pairings too, against the old world — whose players they
	// were — while the pass re-makes them in the new one.
	c.srv.forgetWaypoints(c, old.w)

	// The body is added to the dimension arrived at, at the end of its own
	// player list. vanilla: ServerLevel.addPlayer
	c.srv.appendToLevel(c)
	// vanilla: ServerPlayer.teleport's triggerDimensionChangeTriggers
	if respawn == respawnNone {
		c.changedDimensionAdvancements(old.name, dim.name)
		c.netherTravelAdvancements(old.name, dim.name, left)
	}

	// The Respawn packet carries the body over differently across the
	// three kinds of crossing: a living one keeps everything, a death
	// nothing, and the end credits the attributes alone.
	// vanilla: PlayerList.respawn's dataToKeep
	kept := byte(0x03)
	cooldown := c.portalCooldown.Load()
	if respawn != respawnNone {
		kept = 0
		if respawn == respawnCredits {
			kept = 0x01
		}
		cooldown = 0
		// The new body's abilities go out before PlayerList.respawn sends
		// anything.
		// vanilla: ServerPlayer.restoreFrom
		if err := c.sendAbilities(); err != nil {
			return err
		}
	}
	if err := c.writePacket(protocol.IDPlayRespawn, "respawn", respawnPayload(dim, c.srv.world, c.player.Vitals(), kept, cooldown)); err != nil {
		return err
	}
	// The Respawn clears the client's own copy of the player's state, so the
	// vanilla server sends it again behind the packet. The living crossing
	// and the two respawns are different vanilla paths and resend it in
	// different orders.
	// vanilla: ServerPlayer.teleport, PlayerList.respawn
	if respawn != respawnNone {
		// vanilla: PlayerList.respawn
		if err := c.writePacket(protocol.IDPlaySyncPosition, "synchronize player position", positionSync); err != nil {
			return err
		}
		if err := c.sendDifficulty(); err != nil {
			return err
		}
		if err := c.sendExperience(); err != nil {
			return err
		}
		if err := c.sendActiveEffects(); err != nil {
			return err
		}
		if err := c.sendPostEffects(); err != nil {
			return err
		}
		if err := c.sendInitializeBorder(); err != nil {
			return err
		}
		if err := c.sendUpdateTime(); err != nil {
			return err
		}
		if err := c.sendSpawnPosition(); err != nil {
			return err
		}
		if err := c.sendWeather(dim.w); err != nil {
			return err
		}
		// The death's new body starts from its defaults, the write of a
		// fresh ServerPlayer; the credits' keeps the whole one.
		// vanilla: ServerPlayer.<init>, ServerPlayer.restoreFrom
		if respawn == respawnDeath {
			c.player.ResetAttributes()
		}
		// The level info ends with GameEvent 13, ahead of the permission
		// level and the command tree that follows it.
		// vanilla: PlayerList.sendLevelInfo, sendPlayerPermissionLevel
		if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventStartWaitingPayload()); err != nil {
			return err
		}
		if err := c.sendOpLevel(); err != nil {
			return err
		}
		if err := c.writePacket(protocol.IDPlayCommands, "commands", tree); err != nil {
			return err
		}
	} else {
		// vanilla: ServerPlayer.teleport
		if err := c.sendDifficulty(); err != nil {
			return err
		}
		if err := c.sendOpLevel(); err != nil {
			return err
		}
		if err := c.writePacket(protocol.IDPlayCommands, "commands", tree); err != nil {
			return err
		}
		if err := c.writePacket(protocol.IDPlaySyncPosition, "synchronize player position", positionSync); err != nil {
			return err
		}
		if err := c.sendAbilities(); err != nil {
			return err
		}
		if err := c.sendInitializeBorder(); err != nil {
			return err
		}
		if err := c.sendUpdateTime(); err != nil {
			return err
		}
		if err := c.sendSpawnPosition(); err != nil {
			return err
		}
		// The sky, if it is doing anything in a dimension with weather.
		if err := c.sendWeather(dim.w); err != nil {
			return err
		}
		// Tell the client to expect a world, the way the join does: the client
		// waits for the chunks on its new loading screen rather than dropping
		// the player into the void.
		if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventStartWaitingPayload()); err != nil {
			return err
		}
	}
	if err := c.sendClientRules(old.w, dim.w); err != nil {
		return err
	}

	// The Respawn takes the client's view of the slots with the world, so
	// the whole inventory goes back in, then the held slot. A living
	// crossing sends the effects, the health and the experience behind them;
	// the respawns' attributes and health arrive from the first tick's
	// sync, here.
	// vanilla: ServerPlayer.teleport, PlayerList.respawn
	if err := c.writePacket(protocol.IDPlayContainerContent, "set container content", c.playerInventoryPayload()); err != nil {
		return err
	}
	if err := c.sendHeldItem(); err != nil {
		return err
	}
	if respawn != respawnNone {
		if err := c.introduceAttributes(); err != nil {
			return err
		}
		if err := c.writeHealth(); err != nil {
			return err
		}
	} else {
		if err := c.sendActiveEffects(); err != nil {
			return err
		}
		if err := c.sendPostEffects(); err != nil {
			return err
		}
		if err := c.writeHealth(); err != nil {
			return err
		}
		if err := c.sendExperience(); err != nil {
			return err
		}
	}

	if err := c.sendWorldEntities(false); err != nil {
		return err
	}
	if respawn == respawnDeath {
		// The anchor the fresh body came back to tells its charge, to this
		// body alone. The credits spend no charge, so no deplete.
		// vanilla: PlayerList.respawn
		if payload := c.anchorDepletePayload(); payload != nil {
			if err := c.writePacket(protocol.IDPlaySoundEffect, "respawn anchor deplete", payload); err != nil {
				return err
			}
		}
	}

	// The body goes in for the others now in the dimension with the player —
	// the tab list needs nothing, since it is the whole server's and already
	// has this player. Not for the player: the client has its own body from
	// the Respawn, and a Spawn Entity under its own ID would stand a second
	// one where it is.
	c.srv.broadcastBodyWorldExcept(dim.w, c, playerPairing(c.player, spawnPlayer(c.player)), true)
	// The player's own copy of the metadata stays outside the bundle: it
	// pairs no body.
	c.sendIn(dim.w, protocol.IDPlayEntityMetadata, playerMetadata(c.player, true))
	c.srv.recordBody(dim.w, c)
	// Where the others were last told this player is starts again from the
	// arrival, or their first step would arrive as a difference from the
	// place they left.
	c.trackMu.Lock()
	c.tracked = track(move.Pos)
	c.trackMu.Unlock()

	// A following spectator also fires a tree event, outside this writer hold.
	release()
	// Watchers follow a living crossing with their cameras cleared.
	// vanilla: Entity.teleportSpectators
	if respawn == respawnNone {
		c.srv.teleportSpectatorsWith(c, dim, command.Move{Pos: c.player.Position()})
	}

	c.startChunkSender()
	c.sendChunksAsync()
	c.pluginChangedWorld(old)

	c.log.Info("player entered dimension",
		"dimension", dim.name, "x", move.Pos.X, "y", move.Pos.Y, "z", move.Pos.Z, "respawn", respawn != respawnNone)
	return nil
}

// gameEventStartWaitingPayload builds Game Event 13, which puts the client on
// its loading screen until the chunks arrive rather than dropping the player
// into an empty world.
func gameEventStartWaitingPayload() []byte {
	var b protocol.Buffer
	b.PutU8(protocol.GameEventStartWaitingForChunks)
	b.PutF32(0)
	return b.Bytes()
}

// sendClientRules updates the three client-side game-rule flags whenever the
// destination world's value differs from the world the player just left.
// Respawn carries none of them, so vanilla leaves them unchanged across a
// dimension switch.
func (c *client) sendClientRules(old, dst *world.World) error {
	if v := dst.BoolRule(world.RuleImmediateRespawn); v != old.BoolRule(world.RuleImmediateRespawn) {
		val := float32(0)
		if v {
			val = 1
		}
		if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventPayload(protocol.GameEventImmediateRespawn, val)); err != nil {
			return err
		}
	}
	if v := dst.BoolRule(world.RuleLimitedCrafting); v != old.BoolRule(world.RuleLimitedCrafting) {
		val := float32(0)
		if v {
			val = 1
		}
		if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventPayload(protocol.GameEventLimitedCrafting, val)); err != nil {
			return err
		}
	}
	if v := dst.BoolRule(world.RuleReducedDebug); v != old.BoolRule(world.RuleReducedDebug) {
		status := protocol.EntityStatusReducedDebugOff
		if v {
			status = protocol.EntityStatusReducedDebugOn
		}
		if err := c.writePacket(protocol.IDPlayEntityEvent, "entity event", entityEventPayload(c.player.EntityID, uint8(status))); err != nil {
			return err
		}
	}
	return nil
}
