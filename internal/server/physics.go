package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// blockChanged runs what follows a server block change: the physics wake
// (vanilla setBlock flags 3: removal, onPlace, neighbour and shape updates),
// a fire-completed portal, a pumpkin-built golem, the wake's effects, and
// nether portal revalidation. oldState is the prior block; a wake on an
// unchanged block is a no-op, so callers knowing nothing changed skip it.
func (s *Server) blockChanged(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyChanged(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// blockPlaced is blockChanged for a command's placement: neighbours see the
// new block as the changed one. vanilla: ServerLevel.updateNeighboursOnBlockSet
func (s *Server) blockPlaced(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyPlaced(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// blockTemplatePlaced runs a template cell's removal reactions and on-place as
// it is written. vanilla: StructureTemplate.placeInWorld
func (s *Server) blockTemplatePlaced(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyTemplatePlaced(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// blockTemplateShaped runs a template cell's shape pass once all are placed.
func (s *Server) blockTemplateShaped(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyTemplateShapes(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// blockPlacedHooks is blockPlaced's share a block write owns: the placement
// hooks and the shape passes, before a block entity's tag loads.
func (s *Server) blockPlacedHooks(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyPlacedHooks(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// blockPlacedWake is blockPlaced's explicit neighbour update, run after
// blockPlacedHooks and the tag load.
func (s *Server) blockPlacedWake(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyPlacedWake(x, y, z, oldState)
	s.finishBlockWake(w, x, y, z, oldState, changes, effects, primes, false, false, false)
}

// updateNeighboursOnBlockSet wakes neighbours after a successful non-strict
// /setblock that left the block state unchanged. NotifyChanged skips that case.
// vanilla: ServerLevel.updateNeighboursOnBlockSet
func (s *Server) updateNeighboursOnBlockSet(w *world.World, x, y, z int32, oldState uint16) {
	changes, effects, primes := s.physicsFor(w).NotifyNeighbours(x, y, z, oldState)
	s.afterBlockWake(w, x, y, z, oldState, changes, effects, primes)
}

// afterBlockWake is what blockChanged runs after the physics wake, shared
// with the neighbour-only wake: fire portals, pumpkin golems, wake effects,
// broadcasts and the portal recheck. Hanging support is left to the entity
// poll (vanilla: BlockAttachedEntity.tick).
func (s *Server) afterBlockWake(w *world.World, x, y, z int32, oldState uint16,
	changes []world.BlockChange, effects []world.Effect, primes []world.TNTPrime) {
	s.finishBlockWake(w, x, y, z, oldState, changes, effects, primes, true, true, true)
}

func (s *Server) finishBlockWake(w *world.World, x, y, z int32, oldState uint16,
	changes []world.BlockChange, effects []world.Effect, primes []world.TNTPrime, onPlace, clients, shapes bool) {
	if onPlace && registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockFire {
		s.ignitePortalAt(w, x, y, z)
		s.emptyHivesBesideFire(w, x, y, z)
	}
	dim := s.dimensionOfWorld(w)
	if clients && !slices.Equal(world.CollisionBoxes(oldState), world.CollisionBoxes(w.BlockAt(x, y, z))) {
		dim.navChangesMu.Lock()
		dim.navChanges = append(dim.navChanges, entity.Waypoint{X: x, Y: y, Z: z})
		dim.navChangesMu.Unlock()
	}
	if v := dim.villages; v != nil {
		v.Changed(int(x), int(y), int(z), oldState, w.BlockAt(x, y, z))
	}
	if onPlace {
		s.trySpawnGolem(w, x, y, z, oldState)
	}
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	s.runWakePrimes(w, primes)
	s.tellBlockChanges(w, changes)
	if shapes {
		s.updatePortalNear(w, x, y, z)
	}
}

// runWakePrimes primes a wake's TNT. A prime that writes a block starts a
// wake of its own; its primes are queued and run from this loop, so a chain
// of any length uses constant stack.
func (s *Server) runWakePrimes(w *world.World, primes []world.TNTPrime) {
	for _, t := range primes {
		s.pendingPrimes = append(s.pendingPrimes, pendingPrime{w, t})
	}
	if s.drainingPrimes {
		return
	}
	s.drainingPrimes = true
	defer func() { s.drainingPrimes = false }()
	for len(s.pendingPrimes) > 0 {
		p := s.pendingPrimes[0]
		s.pendingPrimes = s.pendingPrimes[1:]
		s.primeTNT(p.w, p.t)
	}
}

type pendingPrime struct {
	w *world.World
	t world.TNTPrime
}

// physicsShapePass runs the engine's shape pass around (x, y, z) and tells the
// result.
func (s *Server) physicsShapePass(w *world.World, x, y, z int32) {
	changes, effects, primes := s.physicsFor(w).ShapeUpdateNeighbors(x, y, z)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}

// survivalShapePass is physicsShapePass for the neighbours that lose their
// support with the write.
func (s *Server) survivalShapePass(w *world.World, x, y, z int32) {
	changes, effects, primes := s.physicsFor(w).SurvivalShapeUpdates(x, y, z)
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}

// setBlockAndTell writes one block and tells everyone, like a flag-11
// setBlock: block update, occlusion light, neighbour shape update and wake.
// The update goes out at the tick's flush.
func (s *Server) setBlockAndTell(w *world.World, x, y, z int32, state uint16) {
	s.writeBlock(w, x, y, z, state, nil, writeLight|writeLightAfter|writeWake)
}

// setBlockAndTellNow is setBlockAndTell sending the update at once, for a
// caller whose block entity data must follow it on the wire and carries what
// the flush cannot rebuild from the stored tag (a brushable block's
// hit_direction).
func (s *Server) setBlockAndTellNow(w *world.World, x, y, z int32, state uint16) {
	s.writeBlock(w, x, y, z, state, nil, writeLight|writeLightAfter|writeWake|writeNow)
}

// tellEffect sends one engine effect (sound, piston Block Action, world
// event). A BreakDrop (level event 2001 from a shape-update destruction)
// also rolls and drops the block's loot. vanilla: Level.destroyBlock
func (s *Server) tellEffect(w *world.World, e world.Effect) {
	s.tellEffectFrom(w, nil, e)
}

// tellEffectFrom is tellEffect for an effect the player user's click made:
// user is the source of a GameEventByUser event and is left out of the sound
// of a hand use its client predicted (Effect.ExcludeUser), which would
// otherwise double it.
func (s *Server) tellEffectFrom(w *world.World, user *client, e world.Effect) {
	var except *client
	if e.ExcludeUser {
		except = user
	}
	if t := e.Trail; t != nil {
		s.sendParticlePayload(w, trailParticlePayload(t.X, t.Y, t.Z, t.TargetX, t.TargetY, t.TargetZ, t.Color, t.Duration, false, false), t.X, t.Y, t.Z)
	}
	if v := e.Vibration; v != nil {
		// The destination is a block position source (type 0).
		// vanilla: VibrationParticleOption.STREAM_CODEC, BlockPositionSource
		const id = protocol.ParticleVibration
		var data protocol.Buffer
		data.PutVarInt(0)
		data.PutPosition(v.DestX, v.DestY, v.DestZ)
		data.PutVarInt(int32(v.ArrivalTicks))
		s.sendParticlePayload(w, particleDataPayload(id, data.Bytes(), false, v.X, v.Y, v.Z, 0, 0, 0, 0, 1), v.X, v.Y, v.Z)
	}
	if b := e.Particles; b != nil {
		s.sendParticles(w, b.ID, b.X, b.Y, b.Z, b.SpreadX, b.SpreadY, b.SpreadZ, b.Speed, b.Count)
	}
	if e.GameEvent != registry.GameEventNone {
		var ctx rules.GameEventContext
		if e.GameEventByUser && user != nil {
			ctx = user.gameEventContext(0)
		} else if src := e.GameEventSource; src != nil {
			ctx = rules.GameEventContext{Player: src.Player, Mob: src.Mob, Sneaking: src.Sneaking}
		}
		ctx.Affected = e.GameEventAffected
		s.gameEvent(w, e.GameEvent, float64(e.X)+0.5, float64(e.Y)+0.5, float64(e.Z)+0.5, ctx)
		if e.GameEvent == registry.GameEventNoteBlockPlay {
			s.allaysHearNote(w, e.X, e.Y, e.Z) // vanilla: Allay.VibrationUser
		}
	}
	if v := e.ListenerEvent; v != nil {
		source := v.Player
		if v.Mob != uuid.Nil {
			source = v.Mob
		}
		s.offerWardenVibrations(w, v.Event, v.X, v.Y, v.Z, source, uuid.Nil, 0)
	}
	if e.CustomSound != "" || e.SoundID != registry.SoundNone {
		category := e.SoundCategory
		if category == protocol.SoundCategoryMaster {
			category = protocol.SoundCategoryBlock
		}
		x, y, z := float64(e.X)+0.5, float64(e.Y)+0.5, float64(e.Z)+0.5
		var payload []byte
		if e.SoundID != registry.SoundNone {
			payload = soundEffectIDAt(e.SoundID, category, x, y, z, e.Volume, e.Pitch)
		} else {
			// Custom heads use a direct holder even for a registered name.
			// vanilla: NoteBlock.triggerEvent
			payload = playSoundEffect(e.CustomSound, category, x, y, z, e.Volume, e.Pitch, rand.Int64())
		}
		s.broadcastNearExcept(w, except, x, y, z, soundRange(e.Volume), protocol.IDPlaySoundEffect, payload)

	}
	if e.Shriek != nil {
		s.tryShriek(w, e.Shriek.X, e.Shriek.Y, e.Shriek.Z, s.clientInWorld(w, e.Shriek.Player))
	}
	if e.ShriekResponse != nil {
		s.shriekRespond(w, *e.ShriekResponse)
	}
	if e.PistonPush != nil {
		s.pushByPiston(w, e.PistonPush)
	}
	if e.HasBlockAction {
		if e.BlockActionBlock == protocol.BlockBell && e.BlockAction == 1 {
			s.villagersHearBell(w, e.X, e.Y, e.Z)
			s.bellRung(w, e.X, e.Y, e.Z)
		}
		// vanilla: ServerLevel.runBlockEvents
		var b protocol.Buffer
		b.PutPosition(e.X, e.Y, e.Z)
		b.PutU8(e.BlockAction)
		b.PutU8(e.BlockActionData)
		b.PutVarInt(e.BlockActionBlock)
		s.broadcastBlockEvent(w, e.X, e.Y, e.Z, b.Bytes())
	}
	if e.LevelEvent != 0 {
		s.broadcastLevelEvent(w, int32(e.LevelEvent), e.X, e.Y, e.Z, e.LevelEventData)
	}
	if e.BreakDrop != 0 {
		s.rollBreakDrop(w, e)
	}
	if e.RemovedEntity != 0 {
		s.breakBlockEntity(w, e.X, e.Y, e.Z, e.RemovedEntity, true, func(_ *world.BlockEntity, spills []protocol.ItemStack) {
			s.spillContainer(w, e.X, e.Y, e.Z, spills)
		})
	}
}

// rollBreakDrop spawns the drops of the block e.BreakDrop that e broke. It is
// separate so that its loot closure's capture of e costs only an effect that
// breaks a block.
func (s *Server) rollBreakDrop(w *world.World, e world.Effect) {
	// The engine removed the block entity before the effect, so a shulker box's
	// slots and a skull's profile and name come from the effect's captured
	// compound. A piston-pushed block's table reads its partner from
	// the effect's column, not the live world.
	var components world.ItemComponents
	if e.BreakTag != nil {
		components = world.ImplicitComponentsOf(e.BreakTag)
	}
	ctx := s.breakLootContext(w, e.X, e.Y, e.Z, e.BreakDrop, nil, components)
	if e.BreakTag != nil {
		ctx.BlockEntity = func() nbt.Compound { return e.BreakTag }
	}
	ctx.BlockAt = func(dx, dy, dz int32) uint16 {
		if col := e.BreakColumn; col != nil && dx == 0 && dz == 0 && (dy == -1 || dy == 1) {
			return col[(dy+1)/2]
		}
		return w.BlockAt(e.X+dx, e.Y+dy, e.Z+dz)
	}
	for _, stack := range rules.RollLoot(rules.BlockLootTable(registry.BlockOf(e.BreakDrop)), &ctx, s.lootRng) {
		s.spawnBlockDrop(w, e.X, e.Y, e.Z, stack)
	}
}

// primeTNT spawns primed TNT (fuse 80) where redstone lit it, with the
// sound; false when tnt_explodes is off and nothing spawned.
func (s *Server) primeTNT(w *world.World, t world.TNTPrime) bool {
	return s.primeTNTFrom(w, t, 0, "", uuid.Nil)
}

// primeTNTFrom is primeTNT with an owner, which TntBlock.prime passes on
// from a projectile's owner.
func (s *Server) primeTNTFrom(w *world.World, t world.TNTPrime, ownerID int32, ownerName string, ownerUUID uuid.UUID) bool {
	// vanilla: TntBlock.prime
	if !w.TNTExplodes() {
		return false
	}
	if !t.Asked && s.plugins != nil && s.plugins.Has(pluginruntime.TNTPrime) {
		if cancelled := s.pluginTNTPrime(w, t, ownerUUID); cancelled {
			return false
		}
	}
	s.spawnTNT(w, float64(t.X)+0.5, float64(t.Y), float64(t.Z)+0.5,
		entity.TNTDefaultFuse, ownerID, ownerName, ownerUUID, true)
	s.gameEvent(w, rules.GameEventPrimeFuse, float64(t.X)+0.5, float64(t.Y)+0.5, float64(t.Z)+0.5,
		rules.GameEventContext{Player: ownerUUID})
	s.broadcastSoundID(w,
		registry.SoundEntityTntPrimed, protocol.SoundCategoryBlock,
		float64(t.X)+0.5, float64(t.Y), float64(t.Z)+0.5, 1.0, 1.0)
	return true
}

// tickPhysics ticks every dimension's physics. Falling blocks and primed TNT
// spawn after the block changes are sent, so clients see the block go
// first. Fires placed this tick are checked for portals before the changes
// go out: vanilla fills the portal within the placement, so a frame-lighting
// fire is sent as the portal.
func (s *Server) tickPhysics() {
	age, _ := s.world.Time()
	s.beginTouchWindows()
	defer s.endTouchWindows()
	gate := s.newEntityGate()
	for _, dim := range s.allDims() {
		s.refreshFireSpreadPlayers(dim)
		// A debug level runs no scheduled or random ticks.
		// vanilla: ServerLevel.tick, ServerChunkCache.tickChunks
		if dim.w.IsDebug() {
			continue
		}
		s.setPistonGrid(dim, true)
		dim.physics.SetTickFilter(gate.blockFilter(dim.w))
		changes, falls, primes, effects := dim.physics.Tick(age)
		for _, e := range effects {
			s.tellEffect(dim.w, e)
		}
		s.physicsResults(dim, changes, falls, primes, effects)
		s.shoveOnLids(dim)
		// Dispenses run after the block changes, once the redstone edge has
		// settled.
		for _, d := range dim.physics.TakeDispenses() {
			s.runDispense(dim.w, d)
		}
		// Command block runs go with the dispenses: the engine's pass is done, so
		// their commands wake nothing mid-tick, and their feedback goes out with
		// the tick's other sends.
		if runs := dim.physics.TakeCommandRuns(); len(runs) > 0 {
			for _, r := range runs {
				s.runCommandBlock(dim.w, r)
			}
			// A command block may have set the rule before the random ticks.
			dim.fireSpread.refreshRadius(dim.w)
		}
		for _, t := range dim.physics.TakeStructureTriggers() {
			s.runStructureTrigger(dim.w, t)
		}
		dim.physics.RecycleTickResults(changes, effects)
		// Random ticks run after scheduled ticks.
		s.tickRandomTicks(dim, age)
		s.setPistonGrid(dim, false)
	}
}

// setPistonGrid turns the dimension's piston grid on for a physics step or
// drops it after.
func (s *Server) setPistonGrid(dim *dimension, active bool) {
	setBodyPistonGrid(s, &s.mobsMu, &dim.mobs.pistonGrid, pistonMobs, active, func() bool {
		return len(dim.mobs.herd) > 0
	})
	setTrackedPistonGrid(s, &itemTracker, &dim.pistons.items, active)
	setTrackedPistonGrid(s, &orbTracker, &dim.pistons.orbs, active)
	setTrackedPistonGrid(s, &arrowTracker, &dim.pistons.arrows, active)
	setTrackedPistonGrid(s, &tntTracker, &dim.pistons.tnts, active)
	setTrackedPistonGrid(s, &fallingTracker, &dim.pistons.fallings, active)
	setTrackedPistonGrid(s, &boatTracker, &dim.pistons.boats, active)
	setTrackedPistonGrid(s, &standTracker, &dim.pistons.armorStands, active)
	setBodyPistonGrid(s, &s.fixturesMu, &dim.pistons.fixtures, pistonCushions, active, nil)
	setTrackedPistonGrid(s, &minecartTracker, &dim.pistons.minecarts, active)
	setTrackedPistonGrid(s, &endCrystalTracker, &dim.pistons.endCrystals, active)
}

// tickRandomTicks runs a random tick pass over the dimension's chunks within
// simulation distance of a player and its forced chunks, which tick with
// nobody online.
// vanilla: ServerChunkCache.tickChunks, ChunkMap.forEachBlockTickingChunk
func (s *Server) tickRandomTicks(dim *dimension, gameTime int64) {
	s.refreshRandomTickChunks(dim)
	s.randomTickChunks(dim, gameTime, dim.randomTickPositions)
}

// randomTickKey identifies the inputs of gatherRandomTickChunks.
type randomTickKey struct {
	valid                     bool
	chunkSet, forced, tickets uint64
	reach                     int32
	centres, scratch          []world.ChunkPos
	clients                   []*client
}

// refreshRandomTickChunks regathers and sorts dim's ticking chunks only when
// the key inputs differ from the last gather.
func (s *Server) refreshRandomTickChunks(dim *dimension) {
	k := &dim.randomTickKey
	s.mu.RLock()
	k.clients = k.clients[:0]
	for _, c := range s.clients {
		k.clients = append(k.clients, c)
	}
	s.mu.RUnlock()
	scratch := appendSimulationCentres(dim, k.clients, k.scratch[:0])
	// Map order is random; sort so the comparison is stable.
	world.SortChunkPositions(scratch)
	scratch = slices.Compact(scratch)
	k.scratch = scratch
	chunkSet, forced, tickets := dim.w.ChunkSetVersion(), dim.w.ForcedGeneration(), dim.w.TicketGeneration()
	reach := s.simulationDistance.Load()
	if k.valid && k.chunkSet == chunkSet && k.forced == forced && k.tickets == tickets &&
		k.reach == reach && slices.Equal(k.centres, scratch) {
		return
	}
	dim.randomTickPositions = gatherRandomTickChunks(dim, dim.randomTickPositions[:0], scratch, reach)
	world.SortChunkPositions(dim.randomTickPositions)
	k.valid, k.chunkSet, k.forced, k.tickets, k.reach = true, chunkSet, forced, tickets, reach
	k.centres = append(k.centres[:0], scratch...)
}

// randomTickSelection returns an independent slice for test hooks.
func (s *Server) randomTickSelection(dim *dimension) []world.ChunkPos {
	return gatherRandomTickChunks(dim, nil, s.simulationCentres(dim, nil), s.simulationDistance.Load())
}

// simulationCentres appends the chunk of each simulating player in dim.
func (s *Server) simulationCentres(dim *dimension, centres []world.ChunkPos) []world.ChunkPos {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	return appendSimulationCentres(dim, clients, centres)
}

func appendSimulationCentres(dim *dimension, clients []*client, centres []world.ChunkPos) []world.ChunkPos {
	for _, c := range clients {
		if c.theWorld() != dim.w || !c.simulates() {
			continue
		}
		pos := c.player.Position()
		centres = append(centres, world.ChunkPos{X: world.ChunkOf(pos.X), Z: world.ChunkOf(pos.Z)})
	}
	return centres
}

func gatherRandomTickChunks(dim *dimension, buf, centres []world.ChunkPos, reach int32) []world.ChunkPos {
	anyForced := dim.w.AnyChunkForced()
	anyTicketed := dim.w.AnyChunkTicketed()
	loaded := dim.w.AppendLoadedChunkPositions(buf)
	chunks := loaded[:0]
	for _, pos := range loaded {
		if chunkRandomTicks(centres, reach, pos) || anyForced && dim.w.ChunkForced(pos) ||
			anyTicketed && dim.w.ChunkTicketed(pos) {
			chunks = append(chunks, pos)
		}
	}
	return chunks
}

// chunkRandomTicks reports whether pos is within reach chunks (Chebyshev)
// of any centre. A dead player's body is gone and holds no
// ticket; the ring is a live player's simulation ticket.
// vanilla: DistanceManager.addPlayer
func chunkRandomTicks(centres []world.ChunkPos, reach int32, pos world.ChunkPos) bool {
	for _, c := range centres {
		if max(pos.X-c.X, c.X-pos.X, pos.Z-c.Z, c.Z-pos.Z) <= reach {
			return true
		}
	}
	return false
}

// randomTickChunks rolls random ticks over chunks and applies the results:
// effects, block changes and decay drops; no chunks does nothing. The
// caller gathers the chunks, so a hand-driven pass can narrow them.
func (s *Server) randomTickChunks(dim *dimension, gameTime int64, chunks []world.ChunkPos) {
	if len(chunks) == 0 {
		return
	}
	s.rollThunder(dim, chunks)
	changes, decays := dim.physics.RandomTick(gameTime, chunks, randomTickSpeed(dim.w), snowAccumulationHeight(dim.w))
	for _, e := range dim.physics.TakeRandomEffects() {
		s.tellEffect(dim.w, e)
	}
	s.physicsResults(dim, changes, nil, nil, nil)
	for _, d := range decays {
		s.decayDrops(dim, d)
	}
	for _, h := range dim.physics.TakeTurtleHatches() {
		s.hatchTurtles(dim, h)
	}
	for _, t := range dim.physics.TakePortalTicks() {
		s.portalSpawnsPiglin(dim, t)
	}
}

// randomTickSpeed is random_tick_speed clamped to 0 or more: random block
// picks per ticking section per tick.
func randomTickSpeed(w *world.World) int {
	speed := w.IntRule(world.RuleRandomTickSpeed)
	if speed < 0 {
		return 0
	}
	return int(speed)
}

// snowAccumulationHeight is max_snow_accumulation_height, the layers
// snowfall may stack to.
func snowAccumulationHeight(w *world.World) int {
	return int(w.IntRule(world.RuleMaxSnowHeight))
}

// decayDrops rolls a decayed leaf's loot with no tool, as an explosion
// does, and drops it there.
func (s *Server) decayDrops(dim *dimension, d world.Decay) {
	ctx := rules.LootContext{BlockState: d.State, HasBlock: true, Level: s.lootLevelAtBlock(dim.w, d.X, d.Y, d.Z)}
	for _, stack := range rules.RollLoot(rules.BlockLootTable(registry.BlockOf(d.State)), &ctx, s.lootRng) {
		s.spawnBlockDrop(dim.w, d.X, d.Y, d.Z, stack)
	}
}

// physicsResults applies an engine tick's results: fire portals, block
// change packets, portal revalidation, falling blocks, primed TNT and dried
// ghast ghastlings. Tests driving the engine by hand call it too.
func (s *Server) physicsResults(dim *dimension, changes []world.BlockChange, falls []world.FallingSpawn, primes []world.TNTPrime, effects []world.Effect) {
	for i := range changes {
		ch := &changes[i]
		if registry.BlockOf(ch.State) == registry.BlockFire &&
			s.ignitePortalAt(dim.w, ch.X, ch.Y, ch.Z) {
			ch.State = dim.w.BlockAt(ch.X, ch.Y, ch.Z)
		}
		s.emptyHivesBesideFire(dim.w, ch.X, ch.Y, ch.Z)
	}
	s.tellBlockChanges(dim.w, changes)
	// Every changed cell gets a flags-3 setBlock's shape update, so moved or
	// adjacent stairs, fences and walls reshape (the part the engine's own
	// update misses).
	for i := range changes {
		s.reconnectShapes(dim.w, changes[i].X, changes[i].Y, changes[i].Z)
	}
	for _, c := range dim.physics.TakeRideCells() {
		s.reconnectShapes(dim.w, c[0], c[1], c[2])
	}
	s.updatePortalsNear(dim.w, changes)
	// Bodies spawn after the block change clearing their cell, so the queue
	// goes out first.
	if len(falls)+len(primes) > 0 {
		s.flushBlockChanges(dim.w)
	}
	for _, f := range falls {
		s.spawnFallingBlock(dim.w, f)
	}
	for _, t := range primes {
		s.primeTNT(dim.w, t)

		slog.Debug("fire primed a tnt block", "x", t.X, "y", t.Y, "z", t.Z)
	}
	// Ghastlings last, after the block changes, so clients see the block go
	// first.
	tadpoles := dim.physics.TakeTadpoleHatches()
	if len(tadpoles) > 0 {
		s.flushBlockChanges(dim.w)
	}
	for _, h := range tadpoles {
		s.hatchTadpole(dim, h)
	}
	sniffers := dim.physics.TakeSnifferHatches()
	if len(sniffers) > 0 {
		s.flushBlockChanges(dim.w)
	}
	for _, h := range sniffers {
		s.hatchSniffer(dim, h)
	}
	ghastlings := dim.physics.TakeGhastlingSpawns()
	if len(ghastlings) > 0 {
		s.flushBlockChanges(dim.w)
	}
	for _, g := range ghastlings {
		s.spawnGhastling(dim, g)
	}
}

// tellBlockChanges queues block changes for players and relights each
// touched chunk once. Packets go out at the tick's flush with each cell's
// state then (see flushBlockChanges). A reshaped cell is queued for the mobs'
// path replan. Portal revalidation is the callers', since building a portal
// is made of changes that must not trigger it.
func (s *Server) tellBlockChanges(w *world.World, changes []world.BlockChange) {
	if len(changes) == 0 {
		return
	}
	reshaped := false
	for _, ch := range changes {
		if !ch.Silent {
			w.MarkChanged(ch.X, ch.Y, ch.Z)
			reshaped = reshaped || ch.Reshaped
		}
	}
	if reshaped {
		dim := s.dimensionOfWorld(w)
		dim.navChangesMu.Lock()
		for _, ch := range changes {
			if ch.Reshaped && !ch.Silent {
				dim.navChanges = append(dim.navChanges, entity.Waypoint{X: ch.X, Y: ch.Y, Z: ch.Z})
			}
		}
		dim.navChangesMu.Unlock()
	}
	if !s.loopRunning() {
		s.flushBlockChanges(w)
	}
	s.relightChanges(w, changes)
	for _, ch := range changes {
		s.pushEntitiesUp(w, ch)
	}
}

// loopRunning reports whether the main loop runs and will flush queued
// block changes; a stopped loop means a test driving the world by hand.
func (s *Server) loopRunning() bool {
	s.backstopMu.RLock()
	defer s.backstopMu.RUnlock()
	return s.tickRunning
}

// markChanged queues the cell's change for the tick's flush; a stopped loop
// has no flush, so it goes out at once.
func (s *Server) markChanged(w *world.World, x, y, z int32) {
	w.MarkChanged(x, y, z)
	if !s.loopRunning() {
		s.flushBlockChanges(w)
	}
}

// flushBlockChanges sends w's queued changes: first the Update Light of each
// chunk whose light was recomputed, then per chunk section a Block Update
// for one cell or Update Section Blocks for more, each cell read now. Every
// player with the chunk gets it, the one who changed the block included.
// vanilla: ChunkHolder.broadcastChanges
func (s *Server) flushBlockChanges(w *world.World) {
	s.broadcastLight(w, w.TakeLightChanged())
	var changes []world.BlockChange
	for _, sc := range w.TakeChanged() {
		for _, c := range sc.Cells {
			x, y, z := sc.Section[0]<<4|int32(c.X), sc.Section[2]<<4|int32(c.Y), sc.Section[1]<<4|int32(c.Z)
			changes = append(changes, world.BlockChange{X: x, Y: y, Z: z, State: w.BlockAt(x, y, z)})
		}
	}
	s.tellSectionBlocks(w, changes)
}

// flushAllBlockChanges is flushBlockChanges over every dimension.
func (s *Server) flushAllBlockChanges() {
	for _, dim := range s.allDims() {
		s.flushBlockChanges(dim.w)
	}
}

// bodyShove is a move of every body a region crosses: overlaps tests a body's
// box, and the body goes by (dx, dy, dz).
type bodyShove struct {
	overlaps   func(minX, minY, minZ, maxX, maxY, maxZ float64) bool
	dx, dy, dz float64
	// w is the world a shell's or lid's shove is in; unused by a block's.
	w *world.World
	// mobs adds the mobs to a shove that is not block growth (a lid's).
	mobs bool
	// ownBoat and ownCart are the vehicle a shell rides, which is in its own
	// root chain and so not shoved.
	// vanilla: Entity.isPassengerOfSameVehicle
	ownBoat *boatEntity
	ownCart *minecartEntity
}

// shoveOnLids moves the bodies resting in each lid that opened this tick;
// players move on the client.
// vanilla: ShulkerBoxBlockEntity.moveCollidedEntities
func (s *Server) shoveOnLids(dim *dimension) {
	for _, l := range dim.physics.TakeShulkerShoves() {
		slab, d := entity.ShulkerLidSlab(l.Facing, l.From, l.To, l.X, l.Y, l.Z)
		s.shoveBodies(dim.w, bodyShove{overlaps: slabOverlaps(slab), dx: d[0], dy: d[1], dz: d[2], w: dim.w, mobs: true}, false)
	}
}

// clipped returns the shell displacement allowed by the body's collision box.
func (sh bodyShove) clipped(bounds [6]float64) bodyShove {
	if sh.w != nil {
		sh.dx, sh.dy, sh.dz = entity.ClipDisplacement(sh.w, bounds, sh.dx, sh.dy, sh.dz)
	}
	return sh
}

// shoveBodiesBy is a shulker shell's shove: mobs move in the shulker's own
// step, and players move on the client.
func (s *Server) shoveBodiesBy(sh bodyShove) { s.shoveBodies(sh.w, sh, false) }

// slabOverlaps tests a body's box against a swept slab.
func slabOverlaps(b [6]float64) func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	return func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
		return maxX > b[0] && minX < b[3] && maxY > b[1] && minY < b[4] && maxZ > b[2] && minZ < b[5]
	}
}

// blockShove is a grown block raising what its new shape crosses.
func blockShove(ch world.BlockChange) bodyShove {
	return bodyShove{dy: ch.PushUp, overlaps: func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
		return blockOverlapsBody(ch.X, ch.Y, ch.Z, ch.State, minX, minY, minZ, maxX, maxY, maxZ)
	}}
}

// pushEntitiesUp is vanilla Block.pushEntitiesUp for a grown block: every
// body its new shape crosses is raised by the change's rise. Each kind is
// gathered under its own lock; none is held across the teleports.
func (s *Server) pushEntitiesUp(w *world.World, ch world.BlockChange) {
	if ch.PushUp == 0 {
		return
	}
	s.shoveBodies(w, blockShove(ch), true)
}

// shoveBodies moves the bodies sh crosses by its delta; only block growth
// moves players and mobs here.
func (s *Server) shoveBodies(w *world.World, sh bodyShove, blockGrowth bool) {
	// One collector per kind; a missing kind stays sunk in the grown block.
	collectors := []func(*world.World, bodyShove) []func(){
		s.collectItemsToPush,
		s.collectOrbsToPush,
		s.collectArrowsToPush,
		s.collectThrownToPush,
		s.collectTNTsToPush,
		s.collectFallingsToPush,
		s.collectBoatsToPush,
		s.collectMinecartsToPush,
		s.collectArmorStandsToPush,
	}
	if blockGrowth {
		collectors = append(collectors, s.collectPlayersToPush)
	}
	if blockGrowth || sh.mobs {
		collectors = append(collectors, s.collectMobsToPush)
	}
	var moves []func()
	for _, collect := range collectors {
		moves = append(moves, collect(w, sh)...)
	}
	for _, move := range moves {
		move()
	}
}

// collectPlayersToPush gathers w's non-spectator players the change
// crosses, under mu, returning their teleports.
func (s *Server) collectPlayersToPush(w *world.World, sh bodyShove) []func() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var moves []func()
	for _, c := range s.clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		if !sh.overlaps(pos.X-d.Width/2, pos.Y, pos.Z-d.Width/2,
			pos.X+d.Width/2, pos.Y+d.Height, pos.Z+d.Width/2) {
			continue
		}
		moves = append(moves, func() {
			pos := c.player.Position()
			pos.X, pos.Y, pos.Z = pos.X+sh.dx, pos.Y+sh.dy, pos.Z+sh.dz
			c.teleport(command.Move{Pos: pos})
		})
	}
	return moves
}

// collectMobsToPush gathers w's mobs the change crosses, under mobsMu,
// returning their teleports.
func (s *Server) collectMobsToPush(w *world.World, sh bodyShove) []func() {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var moves []func()
	herd := s.herdOf(w)
	for _, m := range herd.herd {
		hw := m.BbWidth() / 2
		if !sh.overlaps(m.X-hw, m.Y, m.Z-hw, m.X+hw, m.Y+m.BbHeight(), m.Z+hw) {
			continue
		}
		ent := herd.record(m)
		if ent == nil {
			continue
		}
		// Read the destination under the lock; teleport without it.
		moved := sh
		if sh.mobs {
			moved = sh.clipped([6]float64{m.X - hw, m.Y, m.Z - hw, m.X + hw, m.Y + m.BbHeight(), m.Z + hw})
		}
		to := player.Position{X: m.X + moved.dx, Y: m.Y + moved.dy, Z: m.Z + moved.dz, Yaw: m.Yaw, Pitch: m.Pitch}
		moves = append(moves, func() {
			s.teleportMob(ent, to, to.Yaw, to.Pitch, "push")
		})
	}
	return moves
}

// collectGrownLift returns a teleport for each live entity in list the
// grown change crosses. list is a pointer so the slice header is read under
// mu too, as ticks reassign the field. skip drops the dead and other
// worlds' bodies; box is the entity's box; move reads the destination under
// mu and returns the closure that sends it once mu is released.
func collectGrownLift[T any](
	mu *sync.Mutex,
	list *[]*T,
	sh bodyShove,
	skip func(*T) bool,
	box func(*T) (minX, minY, minZ, maxX, maxY, maxZ float64),
	move func(*T, bodyShove) func(),
) []func() {
	mu.Lock()
	defer mu.Unlock()
	var moves []func()
	for _, it := range *list {
		if skip(it) {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := box(it)
		if !sh.overlaps(minX, minY, minZ, maxX, maxY, maxZ) {
			continue
		}
		moves = append(moves, move(it, sh.clipped([6]float64{minX, minY, minZ, maxX, maxY, maxZ})))
	}
	return moves
}

// collectItemsToPush gathers w's item entities the change crosses, under
// itemsMu, returning their teleports.
func (s *Server) collectItemsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.itemsMu, &s.items, sh,
		func(it *itemEntity) bool { return it.item.Dead || it.w != w },
		func(it *itemEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.item.BlastBox() },
		func(it *itemEntity, sh bodyShove) func() {
			// The destination teleportItem takes is the box's feet.
			to := player.Position{X: it.item.X + sh.dx, Y: it.item.Y - entity.ItemHalfSize + sh.dy, Z: it.item.Z + sh.dz}
			return func() { s.teleportItem(it, to, "push") }
		})
}

// collectOrbsToPush gathers w's orbs the change crosses, under orbsMu,
// returning their teleports.
func (s *Server) collectOrbsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.orbsMu, &s.orbs, sh,
		func(it *orbEntity) bool { return it.orb.Dead || it.w != w },
		func(it *orbEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.orb.BlastBox() },
		func(it *orbEntity, sh bodyShove) func() {
			to := player.Position{X: it.orb.X + sh.dx, Y: it.orb.Y + sh.dy, Z: it.orb.Z + sh.dz}
			return func() { s.teleportOrb(it, to, "push") }
		})
}

// collectArrowsToPush gathers w's arrows (flying or stuck) the change
// crosses, under arrowsMu, returning their teleports.
func (s *Server) collectArrowsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.arrowsMu, &s.arrows, sh,
		func(it *arrowEntity) bool { return it.arrow.Dead || it.w != w },
		func(it *arrowEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.arrow.BlastBox() },
		func(it *arrowEntity, sh bodyShove) func() {
			to := player.Position{X: it.arrow.X + sh.dx, Y: it.arrow.Y + sh.dy, Z: it.arrow.Z + sh.dz}
			return func() { s.teleportArrow(it, to, "push") }
		})
}

// collectThrownToPush gathers thrown bodies; a shell skips noPhysics bullets.
// vanilla: Shulker.onPeekAmountChange, ShulkerBullet constructor
func (s *Server) collectThrownToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.arrowsMu, &s.thrown, sh,
		func(it *thrownEntity) bool {
			return it.throwable.Dead || it.w != w || sh.w != nil && it.throwable.Kind == entity.ThrownShulkerBullet
		},
		func(it *thrownEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.throwable.BlastBox() },
		func(it *thrownEntity, sh bodyShove) func() {
			x, y, z := it.throwable.X+sh.dx, it.throwable.Y+sh.dy, it.throwable.Z+sh.dz
			// A shell displaces the flight without stopping its velocity.
			return func() {
				thrownTracker.teleport(s, it, func() { it.throwable.SetPos(x, y, z) })
			}
		})
}

// collectTNTsToPush gathers w's primed TNT the change crosses, under tntMu,
// returning their teleports.
func (s *Server) collectTNTsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.tntMu, &s.tnts, sh,
		func(it *tntEntity) bool { return it.tnt.Dead || it.w != w },
		func(it *tntEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.tnt.BlastBox() },
		func(it *tntEntity, sh bodyShove) func() {
			to := player.Position{X: it.tnt.X + sh.dx, Y: it.tnt.Y + sh.dy, Z: it.tnt.Z + sh.dz}
			return func() { s.teleportTNT(it, to, "push") }
		})
}

// collectFallingsToPush gathers w's falling blocks the change crosses,
// under fallingMu, returning their teleports.
func (s *Server) collectFallingsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.fallingMu, &s.fallings, sh,
		func(it *fallingEntity) bool { return it.fb.Dead || it.w != w },
		func(it *fallingEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.fb.BlastBox() },
		func(it *fallingEntity, sh bodyShove) func() {
			to := player.Position{X: it.fb.X + sh.dx, Y: it.fb.Y + sh.dy, Z: it.fb.Z + sh.dz}
			return func() { s.teleportFallingBlock(it, to, "push") }
		})
}

// collectBoatsToPush gathers w's boats the change crosses, under boatsMu,
// returning their teleports.
func (s *Server) collectBoatsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.boatsMu, &s.boats, sh,
		func(it *boatEntity) bool { return it.boat.Dead || it.w != w || it == sh.ownBoat },
		func(it *boatEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.boat.BlastBox() },
		func(it *boatEntity, sh bodyShove) func() {
			to := player.Position{X: it.boat.X + sh.dx, Y: it.boat.Y + sh.dy, Z: it.boat.Z + sh.dz}
			yaw := it.boat.Yaw
			return func() { s.teleportBoat(it, to, yaw, "push") }
		})
}

// collectMinecartsToPush gathers carts under minecartsMu; their tick seats riders.
func (s *Server) collectMinecartsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.minecartsMu, &s.minecarts, sh,
		func(it *minecartEntity) bool { return it.cart.Dead || it.w != w || it == sh.ownCart },
		func(it *minecartEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return it.cart.BlastBox() },
		func(it *minecartEntity, sh bodyShove) func() {
			x, y, z := it.cart.X+sh.dx, it.cart.Y+sh.dy, it.cart.Z+sh.dz
			return func() {
				minecartTracker.teleport(s, it, func() { it.cart.SetPos(x, y, z) })
			}
		})
}

// collectArmorStandsToPush gathers w's armor stands the change crosses,
// under armorStandsMu, returning their teleports. A marker has no box.
func (s *Server) collectArmorStandsToPush(w *world.World, sh bodyShove) []func() {
	return collectGrownLift(&s.armorStandsMu, &s.armorStands, sh,
		func(ent *armorStandEntity) bool { return ent.stand.Dead || ent.w != w || ent.stand.Marker },
		func(ent *armorStandEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) { return ent.stand.BlastBox() },
		func(ent *armorStandEntity, sh bodyShove) func() {
			to := player.Position{X: ent.stand.X + sh.dx, Y: ent.stand.Y + sh.dy, Z: ent.stand.Z + sh.dz, Yaw: ent.stand.Yaw}
			return func() { s.teleportArmorStand(ent, to, "push", false) }
		})
}

// changeBox is one chunk's relight box, the bounds of its changes: it covers
// the union of per-column boxes and is larger when they scatter.
type changeBox struct{ minX, minZ, maxX, maxZ int32 }

// relightChanges books one relight per touched chunk over the bounds of its
// changes, in order of each chunk's first change so the order does not
// depend on map iteration.
func (s *Server) relightChanges(w *world.World, changes []world.BlockChange) {
	at := make(map[[2]int32]int, len(changes))
	var boxes []changeBox
	for _, ch := range changes {
		key := [2]int32{world.ChunkOf(float64(ch.X)), world.ChunkOf(float64(ch.Z))}
		if i, ok := at[key]; ok {
			b := &boxes[i]
			b.minX, b.minZ = min(b.minX, ch.X), min(b.minZ, ch.Z)
			b.maxX, b.maxZ = max(b.maxX, ch.X), max(b.maxZ, ch.Z)
			continue
		}
		at[key] = len(boxes)
		boxes = append(boxes, changeBox{minX: ch.X, minZ: ch.Z, maxX: ch.X, maxZ: ch.Z})
	}
	s.relightBoxesAsync(w, boxes)
	if s.relightSeam != nil {
		s.relightSeam(len(boxes), slices.Clone(changes))
	}
}

// tickRedstoneTouch runs entityInside for pressure plates, wooden buttons
// and tripwire, laying every entity's box over its cells.
// vanilla: BlockState.entityInside
func (s *Server) tickRedstoneTouch() {
	// Its own window, not one shared with tickPhysics: tickBolts can convert
	// mobs in between. The pass runs after the entity moves, so it reads where
	// every entity stands after this tick.
	// vanilla: Entity.applyEffectsFromBlocks
	s.beginTouchWindows()
	defer s.endTouchWindows()
	for _, dim := range s.allDims() {
		p := dim.physics
		if p.ToucherMissing() {
			p.SetToucher(redstoneToucher{s: s, dim: dim})
		}
		if p.RailCartsMissing() {
			p.SetRailCartSource(railCarts{s: s, dim: dim})
		}
		if p.SpreaderMissing() {
			p.SetFireSpreader(fireSpreadChecker{dim: dim})
		}
		if p.EntityOutputMissing() {
			p.SetEntityAnalogOutput(frameAnalogOutputs{s: s, w: dim.w})
		}
		if dim.w.LoadedTouchBlocks() == 0 {
			continue
		}
		var changes []world.BlockChange
		var effects []world.Effect
		var primes []world.TNTPrime
		// Lock order: the engine's lock, then the entity locks. The toucher takes
		// the window and entity locks again, so boxes are snapshotted and every
		// lock released before EntityInsideCells runs. Only the tick goroutine
		// refills the window, so the slice stays put until endTouchWindows.
		for _, b := range s.touchBodiesOf(dim) {
			x0, x1 := int32(math.Floor(b.x-b.w/2)), int32(math.Floor(b.x+b.w/2))
			y0, y1 := int32(math.Floor(b.y)), int32(math.Floor(b.y+b.h))
			z0, z1 := int32(math.Floor(b.z-b.w/2)), int32(math.Floor(b.z+b.w/2))
			if !dim.w.TouchBlocksIn(x0, y0, z0, x1, y1, z1) {
				continue
			}
			p.EntityInsideCells(x0, y0, z0, x1, y1, z1, b.y, b.onGround, &changes, &effects, &primes)
		}
		for _, e := range effects {
			s.tellEffect(dim.w, e)
		}
		s.tellBlockChanges(dim.w, changes)
		for _, t := range primes {
			s.primeTNT(dim.w, t)
		}
	}
}

// touchBody is an entity's box as plates see it: feet centre, width,
// height.
type touchBody struct {
	x, y, z, w, h float64
	living        bool
	onGround      bool
	// src names a player or mob body; zero for the rest.
	src world.EventSource
}

func boxBody(minX, minY, minZ, maxX, maxY, maxZ float64) touchBody {
	return touchBody{x: (minX + maxX) / 2, y: minY, z: (minZ + maxZ) / 2, w: maxX - minX, h: maxY - minY}
}

// touchWindow caches a dimension's touchBodies while open: entities do not
// move during tickPhysics nor tickRedstoneTouch, so one snapshot serves every
// plate and tripwire check of the pass.
type touchWindow struct {
	mu     sync.Mutex
	open   bool
	filled bool
	bodies []touchBody
}

func (t *touchWindow) begin() {
	t.mu.Lock()
	t.open, t.filled = true, false
	t.mu.Unlock()
}

// end closes the window, keeping the slice's capacity for the next one.
func (t *touchWindow) end() {
	t.mu.Lock()
	t.open, t.filled = false, false
	t.mu.Unlock()
}

func (s *Server) beginTouchWindows() {
	for _, dim := range s.allDims() {
		dim.touch.begin()
	}
}

func (s *Server) endTouchWindows() {
	for _, dim := range s.allDims() {
		dim.touch.end()
	}
}

// touchBodiesOf is dim's cached snapshot while its window is open, filled on
// first use, and a fresh touchBodies otherwise. The cached slice is reused by
// the next window, so only the tick goroutine, which opens and closes the
// windows, may keep it past the call; other readers use withTouchBodies.
func (s *Server) touchBodiesOf(dim *dimension) []touchBody {
	t := &dim.touch
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.open {
		return s.touchBodies(dim.w, nil)
	}
	s.fillTouchWindowLocked(dim)
	return t.bodies
}

// withTouchBodies runs fn on dim's bodies, holding the window's mutex while
// it is open so a refill cannot overwrite the slice under fn.
func (s *Server) withTouchBodies(dim *dimension, fn func([]touchBody)) {
	t := &dim.touch
	t.mu.Lock()
	if !t.open {
		t.mu.Unlock()
		fn(s.touchBodies(dim.w, nil))
		return
	}
	defer t.mu.Unlock()
	s.fillTouchWindowLocked(dim)
	fn(t.bodies)
}

func (s *Server) fillTouchWindowLocked(dim *dimension) {
	t := &dim.touch
	if !t.filled {
		t.bodies = s.touchBodies(dim.w, t.bodies[:0])
		t.filled = true
	}
}

// touchBodies snapshots the boxes of w's non-spectator players, mobs (not
// bats),
// items, orbs, arrows (flying or stuck), primed TNT, falling blocks, boats,
// minecarts, thrown projectiles, fishing hooks, fireworks, end crystals,
// armour stands and mannequins, each list under its own lock.
// It appends to bodies and returns the result.
// vanilla: EntitySelector.NO_SPECTATORS, AbstractArrow.tick
func (s *Server) touchBodies(w *world.World, bodies []touchBody) []touchBody {
	// Read under s.mu rather than copied out, so a window's refill allocates
	// nothing.
	s.mu.RLock()
	for _, c := range s.clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		bodies = append(bodies, touchBody{pos.X, pos.Y, pos.Z, d.Width, d.Height, true, pos.OnGround,
			world.EventSource{Player: c.player.UUID, Sneaking: c.player.Appearance().Sneaking}})
	}
	s.mu.RUnlock()
	lockedDo(&s.mobsMu, func() {
		dim := s.dimensionOfWorld(w)
		for _, m := range dim.mobs.herd {
			if m.Type == entity.TypeBat {
				// vanilla: Bat.isIgnoringBlockTriggers
				continue
			}
			b := boxBody(m.BlastBox())
			b.living, b.onGround = true, m.OnGround
			if me := dim.mobs.record(m); me != nil {
				b.src = world.EventSource{Mob: me.uuid}
			}
			bodies = append(bodies, b)
		}
	})
	lockedDo(&s.itemsMu, func() {
		for _, it := range s.items {
			if it.w == w && !it.item.Dead {
				b := boxBody(it.item.BlastBox())
				b.onGround = it.item.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.orbsMu, func() {
		for _, o := range s.orbs {
			if o.w == w && !o.orb.Dead {
				b := boxBody(o.orb.BlastBox())
				b.onGround = o.orb.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.arrowsMu, func() {
		for _, a := range s.arrows {
			if a.w == w && !a.arrow.Dead {
				bodies = append(bodies, boxBody(a.arrow.BlastBox()))
			}
		}
		for _, t := range s.thrown {
			if t.w == w && !t.throwable.Dead {
				bodies = append(bodies, boxBody(t.throwable.BlastBox()))
			}
		}
	})
	lockedDo(&s.bobbersMu, func() {
		for _, it := range s.bobbers {
			if it.w == w && !it.hook.Dead {
				b := boxBody(it.hook.BlastBox())
				b.onGround = it.hook.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.fireworksMu, func() {
		for _, it := range s.fireworks {
			if it.w == w && !it.rocket.Dead {
				bodies = append(bodies, boxBody(it.rocket.Box().Corners()))
			}
		}
	})
	lockedDo(&s.endCrystalsMu, func() {
		for _, it := range s.endCrystals {
			if it.w == w && !it.crystal.Dead {
				bodies = append(bodies, boxBody(it.crystal.BlastBox()))
			}
		}
	})
	lockedDo(&s.tntMu, func() {
		for _, it := range s.tnts {
			if it.w == w && !it.tnt.Dead {
				b := boxBody(it.tnt.BlastBox())
				b.onGround = it.tnt.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.fallingMu, func() {
		for _, it := range s.fallings {
			if it.w == w && !it.fb.Dead {
				b := boxBody(it.fb.BlastBox())
				b.onGround = it.fb.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.boatsMu, func() {
		for _, it := range s.boats {
			if it.w == w && !it.boat.Dead {
				b := boxBody(it.boat.BlastBox())
				b.onGround = it.boat.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.minecartsMu, func() {
		for _, it := range s.minecarts {
			if it.w == w && !it.cart.Dead {
				b := boxBody(it.cart.BlastBox())
				b.onGround = it.cart.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.armorStandsMu, func() {
		for _, ent := range s.armorStands {
			// vanilla: ArmorStand.isIgnoringBlockTriggers
			if ent.w == w && !ent.stand.Dead && !ent.stand.Marker {
				b := boxBody(ent.stand.BlastBox())
				b.living, b.onGround = true, ent.stand.OnGround
				bodies = append(bodies, b)
			}
		}
	})
	lockedDo(&s.mannequinsMu, func() {
		for _, ent := range s.mannequins {
			if ent.w == w {
				b := boxBody(ent.mannequin.BlastBox())
				b.living = true
				bodies = append(bodies, b)
			}
		}
	})
	return bodies
}

// fireSpreadChecker gates fire spread on fire_spread_radius_around_player:
// a non-spectator player in the dimension within that straight-line radius
// from the cell's corner to their feet.
// vanilla: ServerLevel.canSpreadFireAround
type fireSpreadChecker struct{ dim *dimension }

// fireSpreadView holds the rule and the feet of the dimension's non-spectator
// players as of the last refresh. Players do not move during the level tick,
// so one read per tick is what vanilla's per-call loop sees.
type fireSpreadView struct {
	// everywhere is a negative radius: fire ticks everywhere.
	everywhere bool
	r2         float64
	pts        []fireSpreadPoint
}

type fireSpreadPoint struct{ x, y, z float64 }

// refreshFireSpreadPlayers rereads the rule and the dimension's players.
func (s *Server) refreshFireSpreadPlayers(dim *dimension) {
	v := &dim.fireSpread
	v.refreshRadius(dim.w)
	v.pts = v.pts[:0]
	s.mu.RLock()
	for _, c := range s.clients {
		if c.theWorld() != dim.w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		v.pts = append(v.pts, fireSpreadPoint{pos.X, pos.Y, pos.Z})
	}
	s.mu.RUnlock()
}

// refreshRadius rereads fire_spread_radius_around_player.
func (v *fireSpreadView) refreshRadius(w *world.World) {
	radius := w.IntRule(world.RuleFireSpread)
	v.everywhere = radius < 0
	v.r2 = float64(radius) * float64(radius)
}

func (t fireSpreadChecker) FireCanSpread(x, y, z int32) bool {
	v := &t.dim.fireSpread
	if v.everywhere {
		return true
	}
	for _, p := range v.pts {
		dx, dy, dz := p.x-float64(x), p.y-float64(y), p.z-float64(z)
		if dx*dx+dy*dy+dz*dz < v.r2 {
			return true
		}
	}
	return false
}

// copperChestViewers answers the copper chest weathering gate from open
// windows: whether anyone has the container open (vanilla's
// ChestBlockEntity opener count is zero).
// vanilla: WeatheringCopperChestBlock.randomTick
type copperChestViewers struct {
	s *Server
	w *world.World
}

func (v copperChestViewers) ChestViewed(x, y, z int32) bool {
	return v.s.containerOpenerCount(v.w, x, y, z) > 0
}

// sittingCatSource answers the world's sitting-cat query from the mob herd.
type sittingCatSource struct {
	s   *Server
	dim *dimension
	// held is set when the caller already holds mobsMu.
	held bool
}

// SittingCatIn reports whether a cat in its sitting pose intersects box.
// vanilla: ChestBlock.isCatSittingOnChest
func (c sittingCatSource) SittingCatIn(box world.Box) bool {
	if !c.held {
		c.s.mobsMu.Lock()
		defer c.s.mobsMu.Unlock()
	}
	b := [6]float64{box.MinX, box.MinY, box.MinZ, box.MaxX, box.MaxY, box.MaxZ}
	area, indexed := smallChunkArea(&b)
	found := false
	c.dim.mobs.mobsInArea(area, indexed, func(m *entity.Mob) bool {
		if m.Type != entity.TypeCat || !m.Tamable.SittingPose {
			return true
		}
		x0, y0, z0, x1, y1, z1 := m.BlastBox()
		if x1 > box.MinX && x0 < box.MaxX && y1 > box.MinY && y0 < box.MaxY &&
			z1 > box.MinZ && z0 < box.MaxZ {
			found = true
			return false
		}
		return true
	})
	return found
}

// redstoneToucher answers the engine's plate and tripwire questions from
// the entity lists.
type redstoneToucher struct {
	s   *Server
	dim *dimension
}

func (t redstoneToucher) Touching(x, y, z int32) (living, all int) {
	box := world.TouchBox(x, y, z)
	t.s.withTouchBodies(t.dim, func(bodies []touchBody) {
		for _, b := range bodies {
			if t.pressesBody(box, b) {
				all++
				if b.living {
					living++
				}
			}
		}
	})
	return living, all
}

// Presser names the first body on the cell, as findFirst does on the entity
// list. vanilla: BasePressurePlateBlock.checkPressed
func (t redstoneToucher) Presser(x, y, z int32) (src world.EventSource) {
	box := world.TouchBox(x, y, z)
	t.s.withTouchBodies(t.dim, func(bodies []touchBody) {
		for _, b := range bodies {
			if t.pressesBody(box, b) {
				src = b.src
				return
			}
		}
	})
	return src
}

// Pressing counts bodies intersecting the wire's band, which decides the
// string's POWERED. vanilla: TripWireBlock.checkPressed
func (t redstoneToucher) Pressing(box world.Box) int {
	pressing := 0
	t.s.withTouchBodies(t.dim, func(bodies []touchBody) {
		for _, b := range bodies {
			if t.pressesBody(box, b) {
				pressing++
			}
		}
	})
	return pressing
}

// pressesBody reports whether the body's box intersects the box given.
func (t redstoneToucher) pressesBody(box world.Box, b touchBody) bool {
	return b.x+b.w/2 > box.MinX && b.x-b.w/2 < box.MaxX &&
		b.y+b.h > box.MinY && b.y < box.MaxY &&
		b.z+b.w/2 > box.MinZ && b.z-b.w/2 < box.MaxZ
}

// ArrowsIn counts the stuck arrows whose 0.5 by 0.5 box intersects the box.
func (t redstoneToucher) ArrowsIn(box world.Box) int {
	t.s.arrowsMu.Lock()
	defer t.s.arrowsMu.Unlock()
	const half = 0.25
	count := 0
	for _, a := range t.s.arrows {
		if a.w != t.dim.w || a.arrow.Dead {
			continue
		}
		ax, ay, az := a.arrow.X, a.arrow.Y, a.arrow.Z
		if ax+half > box.MinX && ax-half < box.MaxX &&
			ay+2*half > box.MinY && ay < box.MaxY &&
			az+half > box.MinZ && az-half < box.MaxZ {
			count++
		}
	}
	return count
}

type blockWritePolicy uint16

const (
	writeShape blockWritePolicy = 1 << iota
	writeEntity
	writeLight
	writeLightAfter
	writeWake
	writePlaced
	writeCrafter
	// writeNow tells the change at once instead of at the tick's flush, for a
	// caller whose own block entity data follows the update.
	writeNow
	// writeNeighborShapes is the shape pass of a flag-2 write: the rules
	// reshape and the engine's shape pass, no neighbour update.
	writeNeighborShapes
	// writePlacedHooks runs a placement's hooks and shape passes within the
	// write and leaves the explicit neighbour update to the caller.
	writePlacedHooks
	commandBlockWrite = writeShape | writeEntity | writeLight | writeWake
	strictBlockWrite  = writeEntity | writeLight
)

// Commands use flags 2|256, strict commands 2|816; strict omits shaping and wakes.
// Physics broadcasts before relighting; commands relight first. Preserve that
// order; a queued write's light goes ahead of its block at the flush.
// vanilla: Level.setBlock, ServerLevel.updateNeighboursOnBlockSet
func (s *Server) writeBlock(w *world.World, x, y, z int32, state uint16, defined map[string]string, policy blockWritePolicy) bool {
	return s.writeBlockWithUpdates(w, x, y, z, state, defined, policy, nil)
}

func (s *Server) writeBlockWithUpdates(w *world.World, x, y, z int32, state uint16, defined map[string]string, policy blockWritePolicy, updates *world.BlockUpdates) bool {
	if policy&writeShape != 0 {
		state = rules.WithDefinedProperties(rules.NeighborShapedState(w, s.levelRng, state, x, y, z), defined)
	}
	old, changed := w.SwapBlock(x, y, z, state)
	if !changed {
		return false
	}
	if policy&writeCrafter != 0 {
		s.physicsFor(w).ScheduleCrafter(x, y, z)
	}
	if updates != nil {
		s.replacePluginBlockEntity(w, x, y, z, old, state, updates.EntityEffects)
	} else if policy&writeEntity != 0 {
		s.replaceBlockEntity(w, x, y, z, old, w.BlockAt(x, y, z))
	}
	if policy&(writeLight|writeLightAfter) == writeLight {
		if !registry.SameLight(old, state) {
			// SwapBlock already marked this column. Book the whole chunk so
			// the lane can coalesce a burst of writes without rescanning light marks.
			s.bookRelight(w, changeBox{x &^ 15, z &^ 15, x | 15, z | 15})
		}
	}
	if policy&writeNow != 0 {
		s.broadcastBlockUpdate(w, x, y, z, state)
	} else if updates == nil || updates.Clients {
		s.markChanged(w, x, y, z)
	}
	if policy&(writeLight|writeLightAfter) == writeLight|writeLightAfter {
		s.relightAsync(w, x, z)
	}
	if updates != nil {
		s.applyBlockUpdates(w, x, y, z, old, state, *updates)
		return true
	}
	if policy&writePlacedHooks != 0 {
		s.updateNeighborShapes(w, x, y, z)
		s.blockPlacedHooks(w, x, y, z, old)
	} else if policy&(writeWake|writeNeighborShapes) == writeNeighborShapes {
		s.updateNeighborShapes(w, x, y, z)
		s.physicsShapePass(w, x, y, z)
	}
	if policy&writeWake != 0 {
		s.updateNeighborShapes(w, x, y, z)
		if policy&writePlaced != 0 {
			s.blockPlaced(w, x, y, z, old)
		} else {
			s.blockChanged(w, x, y, z, old)
		}
	}
	return true
}
