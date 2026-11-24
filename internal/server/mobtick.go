package server

import (
	"github.com/golem-mc/golem/plugin"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// tickMobs runs one tick of every dimension's mobs in lock phases: players,
// leash holders, identities, leavers and hulls read with nothing held; each
// herd stepped under the mob lock alone, collecting effects; then the
// effects applied.
func (s *Server) tickMobs() {
	// Loot pickup goes before the advance, so the sweep's equipment diff tells
	// watchers what was picked up.
	s.tickRaiderBanners()
	s.tickMobLoot()
	in := s.resetMobTickInputs()
	// The raid tick ages the village scans; it takes a lock that comes before
	// the mob lock.
	villageTick := s.raidTick()
	for _, dim := range s.allDims() {
		if dim.villageView == nil {
			dim.villageView = &villageView{dim: dim}
		}
		dim.villageView.tick = villageTick
	}
	// Ahead of the inputs, so a knot a saved lead stands up is a holder
	// this tick.
	s.restoreMobLeashes()
	known := s.readMobTickInputs(in)
	fx := newMobEffects(s)
	fx.sends, s.mobTickSends = s.mobTickSends[:0], nil
	fx.departures = s.sweepDepartingEntities()
	s.stepOffDepartingVehicles(fx.departures)
	// Boat hulls are read before the mob lock, which a mob's move may not hold
	// while taking the boat lock.
	hulls := s.hullsByWorld()
	s.readMobVehicleCrowds(in)
	foxItems := s.foxItemView()
	raidCenters := s.lootRaidCenters()
	s.readHerdInputs(in)
	in.stands = s.armorStandRamTargets()

	s.lockTick(&s.mobsMu)
	for _, r := range in.treasures {
		r.m.SetDolphinTreasure(r.x, r.z, r.found)
	}
	s.addMobHolders(in)
	for _, dim := range s.allDims() {
		t := mobDimTick{s: s, dim: dim, h: dim.mobs, w: dim.w, known: known, prepared: in.prepared[dim.w], hulls: hulls, foxItems: foxItems, raidCenters: raidCenters[dim.w]}
		// navChangesMu is a leaf lock, so it may be taken under mobsMu.
		dim.navChangesMu.Lock()
		t.navChanges, dim.navChanges = dim.navChanges, dim.navSpare[:0]
		dim.navChangesMu.Unlock()
		t.step(&fx, in)
		dim.navSpare = t.navChanges[:0]
	}
	s.unlockTick(&s.mobsMu)

	fx.apply(s, in)
	s.recycleMobTickSends(fx.sends)
}

// recycleMobTickSends retains backing storage after mailboxes took sends by value.
func (s *Server) recycleMobTickSends(sends []mobSend) {
	clear(sends)
	s.mobTickSends = sends[:0]
}

// mobTickInputs is what the mob tick reads before the mob lock.
type mobTickInputs struct {
	// players is each world's player snapshots; inWorld their clients, index
	// for index.
	players map[*world.World][]entity.Player
	inWorld map[*world.World][]*client
	// byEntityID is the snapshots by entity ID, which is how a death finds its
	// killer.
	byEntityID map[int32]*entity.Player
	// clientByEntityID is their connections, for crediting a kill.
	clientByEntityID map[int32]*client
	// holders is every leash holder, handed to every world's herd.
	holders map[*world.World][]entity.LeashHolder
	// loadedBefore is each world's chunks loaded before the sweep: a chunk
	// counts as gone only if it was gone before the sweep and still is, so one
	// that unloads in between is left for the next tick rather than written
	// without the entities the sweep found in it.
	loadedBefore map[*world.World]map[world.ChunkPos]struct{}
	// prepared is the worlds whose loadedBefore and UUID set were read.
	prepared map[*world.World]bool
	// teams maps a member's name (a player's name, a mob's UUID) to its team.
	teams       map[string]string
	playerTeams map[uuid.UUID]string
	// teamRules maps a team's name to its collision rule.
	teamRules map[string]entity.CollisionRule
	// sulfurPower is neighbor power at unprimed explosive cubes, resolved
	// before the mob lock because the physics lock comes first.
	sulfurPower map[sulfurPowerCell]bool
	// treasures is the dolphins asking for a treasure structure, resolved
	// before the mob lock.
	treasures []dolphinTreasureInput
	// parrotOwners is the players owning a tamed parrot; only theirs get the
	// water and snow facts a landing needs. Nil when there is none.
	parrotOwners map[uuid.UUID]bool
	// holderSpans is every leash holder with its world, before the per-world
	// copies in holders.
	holderSpans []holderSpan
	// stands is each world's visible armor stands, a goat's ram targets.
	stands map[*world.World][]entity.StandTarget
}

// holderSpan is a leash holder with its world.
type holderSpan struct {
	holder entity.LeashHolder
	w      *world.World
}

// resetMobTickInputs empties the server's inputs for a new tick, keeping the
// maps and slices. Tick goroutine only. Nothing may hold a pointer into them
// past fx.apply: the snapshots and the maps are overwritten by the next tick.
func (s *Server) resetMobTickInputs() *mobTickInputs {
	in := &s.mobInputs
	if in.players == nil {
		in.players = make(map[*world.World][]entity.Player)
		in.inWorld = make(map[*world.World][]*client)
		in.byEntityID = make(map[int32]*entity.Player)
		in.playerTeams = make(map[uuid.UUID]string)
		in.clientByEntityID = make(map[int32]*client)
		in.holders = make(map[*world.World][]entity.LeashHolder, len(s.allDims()))
		in.sulfurPower = make(map[sulfurPowerCell]bool)
	}
	// Slices are truncated, not their map entries dropped, so each world's
	// capacity survives; contents are cleared so no snapshot keeps a stack alive.
	for w, ps := range in.players {
		clear(ps)
		in.players[w] = ps[:0]
	}
	for w, cs := range in.inWorld {
		clear(cs)
		in.inWorld[w] = cs[:0]
	}
	for w, hs := range in.holders {
		clear(hs)
		in.holders[w] = hs[:0]
	}
	clear(in.byEntityID)
	clear(in.playerTeams)
	clear(in.clientByEntityID)
	clear(in.sulfurPower)
	clear(in.treasures)
	in.treasures = in.treasures[:0]
	clear(in.holderSpans)
	in.holderSpans = in.holderSpans[:0]
	in.parrotOwners, in.teams, in.teamRules, in.loadedBefore, in.stands = nil, nil, nil, nil, nil
	return in
}

// readMobTickInputs reads players, leash holders and loaded chunks into in
// before the sweep and the mob lock, returning every live entity's UUID
// (read between the holders and the chunks).
func (s *Server) readMobTickInputs(in *mobTickInputs) (known map[uuid.UUID]bool) {
	// Read before the mob lock: positions and held stacks are under the player
	// locks.
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	in.parrotOwners = s.tamedParrotOwners()
	players := in.players
	in.teams = s.scoreboard.MemberTeams()
	in.teamRules = s.teamCollisionRules()
	// A mob whose holder went to another dimension keeps its lead (only death
	// or removal drops it), so every herd gets every holder, SameLevel marking
	// those that may pull.
	byEntityID, clientByEntityID, inWorld := in.byEntityID, in.clientByEntityID, in.inWorld
	for _, c := range clients {
		w := c.theWorld()
		pos := c.player.Position()
		_, invisible := c.player.Effect(invisibilityEffect)
		vel := c.player.Velocity()
		c.stepVelocity(w, pos.OnGround)
		vitals := c.player.Vitals()
		mode := c.player.GameMode()
		movement := c.player.KnownMovement()
		snapshot := entity.Player{Age: vitals.AgeTicks,
			X: pos.X, Y: pos.Y, Z: pos.Z,
			VX: vel.X, VY: vel.Y, VZ: vel.Z,
			KnownVX: movement.X, KnownVY: movement.Y, KnownVZ: movement.Z,
			Yaw: float64(pos.Yaw), Pitch: float64(pos.Pitch),
			UUID:          c.player.UUID,
			Team:          in.teams[c.player.Name],
			TeamCollision: in.teamRules[in.teams[c.player.Name]],
			HeldItem:      c.heldItem(protocol.HandMain),
			OffHandItem:   c.heldItem(protocol.HandOff),
			EntityID:      c.player.EntityID,
			// Huntable: alive and in a mode a monster may hunt.
			Huntable: !vitals.Dead &&
				mode != protocol.GameModeCreative &&
				mode != protocol.GameModeSpectator,
			// Pushable: alive and in a mode that can be pushed.
			Pushable: !vitals.Dead &&
				mode != protocol.GameModeSpectator,
			Passenger: c.isPassenger(),
			// Spectator keeps despawn distance from measuring against a spectator.
			Spectator: mode == protocol.GameModeSpectator,
			Creative:  mode == protocol.GameModeCreative,
			Dead:      vitals.Dead,
			// The facts the targeting conditions cut the hunt's reach by.
			Sneaking:   c.player.Appearance().Sneaking,
			Sprinting:  c.player.Appearance().Sprinting,
			Swimming:   c.player.Appearance().Swimming,
			Sleeping:   c.player.Sleep().Lying,
			SleepTimer: c.player.Sleep().Ticks,
			Invisible:  invisible,
			Worn:       c.wornArmour(),
			// The facts a witch picks its potion by.
			Health:   vitals.Health,
			Slowed:   hasEffect(c, effectSlowness),
			Poisoned: hasEffect(c, effectPoison),
			Weakened: hasEffect(c, effectWeakness),
			Hero:     hasEffect(c, effectHeroOfTheVillage),
		}
		dims := c.dims(c.player.Appearance().Pose)
		snapshot.Width, snapshot.Height, snapshot.EyeHeight = dims.Width, dims.Height, dims.EyeHeight
		if snapshot.Passenger {
			c.mobRiderSnapshot(&snapshot)
		}
		c.shoulderSnapshot(&snapshot, pos, in.parrotOwners[c.player.UUID])
		// Forget a blow older than lastHurtByMemory ticks. vanilla: LivingEntity.tick
		if vitals.LastHurtBy != 0 && vitals.AgeTicks-vitals.LastHurtByAge <= lastHurtByMemory {
			snapshot.HurtBy, snapshot.HurtByStamp = vitals.LastHurtBy, vitals.LastHurtByAge
		}
		snapshot.HurtNoWolfRetaliation = vitals.LastHurtNoWolfRetaliation
		snapshot.HurtMob, snapshot.HurtMobStamp = vitals.LastHurtMob, vitals.LastHurtMobAge
		if cap(players[w]) < len(clients) {
			// Each client contributes once; keeping capacity for all of them
			// makes the snapshot pointers stable for the rest of the tick.
			players[w] = slices.Grow(players[w], len(clients))
			inWorld[w] = slices.Grow(inWorld[w], len(clients))
		}
		players[w] = append(players[w], snapshot)
		// A dead or spectating player cannot hold a leash, which drops its mobs.
		// vanilla: Entity.canInteractWithLevel
		in.holderSpans = append(in.holderSpans, holderSpan{w: w, holder: playerLeashHolder(c)})
		in.playerTeams[c.player.UUID] = snapshot.Team
		byEntityID[c.player.EntityID] = &players[w][len(players[w])-1]
		clientByEntityID[c.player.EntityID] = c
		inWorld[w] = append(inWorld[w], c)
	}
	// Knots are read before the mob lock; each is alive while the knot list
	// holds it.
	s.lockTick(&s.knotsMu)
	for _, k := range s.knots {
		in.holderSpans = append(in.holderSpans, holderSpan{holder: knotHolderSnapshot(k), w: k.w})
	}
	s.unlockTick(&s.knotsMu)
	holders := in.holders
	for _, dim := range s.allDims() {
		for _, hs := range in.holderSpans {
			holder := hs.holder
			holder.SameLevel = hs.w == dim.w
			holders[dim.w] = append(holders[dim.w], holder)
		}
	}

	// The UUID set and the loaded sets are read only by a dimension that wakes
	// or lets chunks leave, so only such a dimension pays for them. The kind
	// locks that fill the set come before the mob lock, so the decision is
	// made here; a dimension that turns out to need them anyway waits a tick
	// (mobTickForce).
	force := s.mobTickForce
	s.mobTickForce = false
	if in.prepared == nil {
		in.prepared = make(map[*world.World]bool, len(s.allDims()))
	}
	clear(in.prepared)
	anyPrepared := false
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		h := dim.mobs
		if force || !h.wakeSetOK || h.wakeSet != dim.w.ChunkSetVersion() || len(h.wokenForWrite) > 0 {
			in.prepared[dim.w] = true
			anyPrepared = true
		}
	}
	s.unlockTick(&s.mobsMu)
	if !anyPrepared {
		return nil
	}
	if s.mobTickKnown == nil {
		s.mobTickKnown = make(map[uuid.UUID]bool)
	}
	known = s.mobTickKnown
	clear(known)
	s.liveEntityUUIDsInto(known)
	if s.mobTickLoaded == nil {
		s.mobTickLoaded = make(map[*world.World]map[world.ChunkPos]struct{}, len(s.allDims()))
	}
	in.loadedBefore = s.mobTickLoaded
	for _, dim := range s.allDims() {
		if !in.prepared[dim.w] {
			continue
		}
		set := in.loadedBefore[dim.w]
		if set == nil {
			set = make(map[world.ChunkPos]struct{})
			in.loadedBefore[dim.w] = set
		}
		clear(set)
		for _, pos := range dim.w.LoadedChunkPositions() {
			set[pos] = struct{}{}
		}
	}
	return known
}

// mobRiderSnapshot reads the immediate mount and root vehicle without nesting entity locks.
func (c *client) mobRiderSnapshot(p *entity.Player) {
	s := c.srv
	boat, cart := c.vehicle.Load(), c.minecart.Load()
	if mount := c.mount.Load(); mount != nil && boat == nil && cart == nil {
		s.mobsMu.Lock()
		m := mount.mob
		p.OnHappyGhast = m.Type == entity.TypeHappyGhast
		p.NautilusDriver = entity.IsNautilus(m.Type) && mount.rider == c && !m.Horse.Saddle.Empty()
		p.RideBox = entity.NewAABB(m.BlastBox())
		root := m.RootVehicle()
		p.RideRoot.X, p.RideRoot.Y, p.RideRoot.Z, p.RideRoot.Top = root.X, root.Y, root.Z, root.Y+root.BbHeight()
		if ent := s.herdOf(mount.w).book[root]; ent != nil {
			boat, cart = ent.vehicle, ent.cart
		}
		s.mobsMu.Unlock()
		if boat == nil && cart == nil {
			return
		}
	}

	var box entity.AABB
	var x, y, z, height float64
	switch {
	case boat != nil:
		s.boatsMu.Lock()
		p.BoatDriver = boat.driver() == c
		box = entity.NewAABB(boat.boat.Box())
		x, y, z, height = boat.boat.X, boat.boat.Y, boat.boat.Z, entity.BoatHeight
		s.boatsMu.Unlock()
	case cart != nil:
		s.minecartsMu.Lock()
		box = entity.NewAABB(cart.cart.Box())
		x, y, z, height = cart.cart.X, cart.cart.Y, cart.cart.Z, entity.MinecartHeight
		s.minecartsMu.Unlock()
	default:
		cushion := c.cushion.Load()
		if cushion == nil {
			return
		}
		s.fixturesMu.Lock()
		box = entity.NewAABB(entity.FixtureBox(cushion.kind, cushion.tag, cushion.x, cushion.y, cushion.z))
		x, y, z, height = cushion.x, cushion.y, cushion.z, entity.CushionHeight
		s.fixturesMu.Unlock()
	}
	if p.RideBox == (entity.AABB{}) {
		p.RideBox = box
	}
	p.RideRoot.X, p.RideRoot.Y, p.RideRoot.Z, p.RideRoot.Top = x, y, z, y+height
}

// stepOffDepartingVehicles empties the mob seats of boats and carts leaving
// with their chunk.
func (s *Server) stepOffDepartingVehicles(departures []departure) {
	// A vehicle leaving with its chunk drops its mob passengers before the
	// herd pass reads them: the vehicle is already a save tag and the mob
	// walks or saves where it sat. A player passenger keeps the chunk loaded,
	// so only mobs ride out; a player pointer is still cleared, just in case.
	vehicles := 0
	for _, d := range departures {
		if d.kind == departingBoat || d.kind == departingMinecart && len(d.minecart.mobs) > 0 {
			vehicles++
		}
	}
	if vehicles == 0 {
		return
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for i := range departures {
		d := &departures[i]
		switch d.kind {
		case departingMinecart:
			for _, ent := range d.minecart.mobs {
				ent.cart = nil
				ent.mob.RidesVehicle = false
			}
			d.riders = d.minecart.mobs
			d.minecart.mobs = nil
		case departingBoat:
			for _, p := range d.boat.passengers {
				if p.c != nil {
					p.c.vehicle.Store(nil)
					continue
				}
				p.ent.vehicle = nil
				p.ent.mob.RidesVehicle = false
				d.riders = append(d.riders, p.ent)
			}
			d.boat.passengers = nil
		}
	}
}

// mobDimTick is one dimension's step of the mob tick, under the mob lock.
// Effects and inputs are passed beside it, not held in it: escape analysis
// does not tell fields apart, so the pointers stored here would drag the
// tick's maps to the heap.
type mobDimTick struct {
	s   *Server
	dim *dimension
	h   *mobHerd
	w   *world.World
	// known is every live entity's UUID, so a wake refuses a duplicate: the
	// four kinds' share is read before the mob lock, the mobs' filled in by the
	// dimension passes.
	known map[uuid.UUID]bool
	// prepared is whether known and loadedBefore were read for this dimension,
	// which they are only when a wake or a departure is expected.
	prepared bool
	// SulfurCube.tickFuse precedes Entity.handlePortal and its cooldown decrement.
	sulfurPortalCooling map[*entity.Mob]bool
	// hulls and foxItems are boat hulls and the foxes' item view, read under
	// their own locks before the mob lock.
	hulls    map[*world.World][]entity.AABB
	foxItems map[*world.World][]entity.FoxItem
	// raidCenters snapshots active raids before the mob lock.
	raidCenters []raid.Pos
	// corpses are deaths after the body's own tickDeath (in its aiStep); they
	// start counting next tick, so removal is a full twenty ticks after the
	// death event. Others count from this tick via countCorpses' decrement.
	corpses []dyingMob
	// navChanges is the cells whose collision shape changed since the last
	// tick, replanning the paths they lie near.
	navChanges []entity.Waypoint
}

// step runs the dimension's phases in order.
func (t *mobDimTick) step(fx *mobEffects, in *mobTickInputs) {
	t.leaveUnloadedChunks(fx, in)
	t.wakeLoadedChunks(fx, in)
	t.markDormant(in)
	t.crossPortals(fx)
	dyingCount := len(t.h.dying)
	events := t.advance(fx, in)
	fx.tradeDraws = append(fx.tradeDraws, t.dim.tradeDraws...)
	clear(t.dim.tradeDraws)
	t.dim.tradeDraws = t.dim.tradeDraws[:0]
	t.mountGameEvents(fx, events)
	for _, ev := range events {
		t.event(fx, in, ev)
	}
	// The buffer is kept for the next tick; cleared so it holds no mob alive.
	clear(events)
	t.h.events = events[:0]
	t.noteMoves(fx)
	t.scheduleMovement(fx, in)
	t.countCorpses(fx, dyingCount)
}

// deferDepartures leaves a dimension whose sets were not read alone, asking
// the next tick to read them if something is in an unloaded chunk.
func (t *mobDimTick) deferDepartures(fx *mobEffects) {
	w := t.w
	var last world.ChunkPos
	haveLast := false
	for _, m := range t.h.herd {
		pos := herdChunk(m)
		if haveLast && pos == last {
			continue
		}
		last, haveLast = pos, true
		if !w.ChunkLoaded(pos) {
			t.s.mobTickForce = true
			return
		}
	}
	for _, d := range fx.departures {
		if d.w == w && !w.ChunkLoaded(d.pos) {
			t.s.mobTickForce = true
			return
		}
	}
}

// leaveUnloadedChunks queues snapshots of mobs and other unloaded leavers.
func (t *mobDimTick) leaveUnloadedChunks(fx *mobEffects, in *mobTickInputs) {
	s, h, w := t.s, t.h, t.w
	if !t.prepared {
		t.deferDepartures(fx)
		return
	}
	// Each chunk's answer is cached for the pass, so a chunk returning mid-pass
	// cannot be gone for only some of its mobs.
	unloadedSeen := make(map[world.ChunkPos]bool)
	loadedBefore := in.loadedBefore[w]
	// A herd is clustered, so the previous mob's chunk answers most asks.
	var lastPos world.ChunkPos
	var lastGone, haveLast bool
	unloaded := func(pos world.ChunkPos) bool {
		if haveLast && pos == lastPos {
			return lastGone
		}
		gone := false
		// Loaded before the sweep means present for the entire pass.
		if _, was := loadedBefore[pos]; !was {
			var ok bool
			if gone, ok = unloadedSeen[pos]; !ok {
				gone = !w.ChunkLoaded(pos)
				unloadedSeen[pos] = gone
			}
		}
		lastPos, lastGone, haveLast = pos, gone, true
		return gone
	}

	// A mob whose chunk unloaded leaves with it; a rider leaves with its root
	// vehicle's chunk, since vanilla never saves a passenger alone.
	// vanilla: PersistentEntitySectionManager.getPassengersAndSelf
	alive := h.herd[:0]
	gone := make(map[world.ChunkPos][]*entity.Mob)
	for _, m := range h.herd {
		pos := herdChunk(m)
		if !unloaded(pos) {
			alive = append(alive, m)
			continue
		}
		gone[pos] = append(gone[pos], m)
	}
	h.setHerd(alive)
	// A woken chunk left empty is written too, or the mobs that walked off it
	// come back as duplicates.
	for pos := range h.woken {
		if _, ok := gone[pos]; ok || !unloaded(pos) {
			continue
		}
		if h.listed[pos] {
			gone[pos] = nil
		}
	}
	// The four kinds' leavers join the same write, adding their chunks.
	goneTags := make(map[world.ChunkPos][]nbt.Tag)
	var steppedOff map[*mobEntity]uuid.UUID
	for i, d := range fx.departures {
		if d.w != w {
			continue
		}
		// A leaver whose chunk the herd never woke, or that left after the
		// snapshot, is not written; its tag waits out the tick.
		if _, ok := h.woken[d.pos]; !ok || !unloaded(d.pos) {
			continue
		}
		goneTags[d.pos] = append(goneTags[d.pos], d.tag)
		fx.departures[i].boxed = true
		for _, ent := range d.riders {
			if steppedOff == nil {
				steppedOff = make(map[*mobEntity]uuid.UUID)
			}
			steppedOff[ent], _ = entityUUIDOfTag(d.tag)
		}
		if _, ok := gone[d.pos]; !ok {
			gone[d.pos] = nil
		}
	}
	// An unwoken leaving chunk is read first so its saved mobs leave with it.
	var live map[uuid.UUID]bool
	for pos := range gone {
		if _, ok := h.woken[pos]; ok {
			continue
		}
		if live == nil {
			live = h.liveWith(t.known)
		}
		gone[pos] = append(gone[pos], h.wakeForWrite(pos, s, live)...)
	}
	written := make(map[world.ChunkPos]struct{}, len(gone))
	fx.written[w] = written
	for pos, mobs := range gone {
		h.boxMobs(pos, mobs, goneTags[pos], steppedOff)
		delete(h.woken, pos)
		delete(h.wokenForWrite, pos)
		// The immutable pending snapshot is the source for the next load, even
		// if disk I/O fails; the world retains it until a successful retry.
		delete(h.carried, pos)
		written[pos] = struct{}{}
		// Drop records before a reload can wake the queued snapshot's UUIDs.
		for _, m := range mobs {
			h.forget(m)
			fx.remove(w, m.ID)
		}
	}

	// Leavers written this pass leave the refusal set: a chunk coming straight
	// back, or a player rejoining within the tick, must wake them from the file
	// just queued, or they are refused as duplicates and lost.
	for _, d := range fx.departures {
		if _, ok := written[d.pos]; !ok || d.w != w || !d.boxed {
			continue
		}
		if identity, ok := entityUUIDOfTag(d.tag); ok {
			delete(t.known, identity)
		}
	}
}

// wakeLoadedChunks wakes entities of newly loaded chunks, registered after
// the mob lock is released.
func (t *mobDimTick) wakeLoadedChunks(fx *mobEffects, in *mobTickInputs) {
	s, h, w := t.s, t.h, t.w
	fx.loaded.teams = append(fx.loaded.teams, h.writeTeams...)
	h.writeTeams = nil
	// Nothing to wake while the loaded set and the write wakes are unchanged.
	version := w.ChunkSetVersion()
	if h.wakeSetOK && h.wakeSet == version && len(h.wokenForWrite) == 0 {
		return
	}
	positions := w.LoadedChunkPositions()
	due := positions[:0]
	for _, pos := range positions {
		if _, ok := h.woken[pos]; !ok || h.wokenForWrite[pos] {
			due = append(due, pos)
		}
	}
	if len(due) == 0 {
		h.wakeSet, h.wakeSetOK = version, true
		return
	}
	if !t.prepared {
		s.mobTickForce = true
		return
	}
	// Saved entities first; a chunk that never had a file generates a few
	// animals.
	for _, e := range h.book {
		t.known[e.uuid] = true
	}
	for _, pos := range due {
		var woken wakeResult
		if _, ok := h.woken[pos]; !ok {
			h.woken[pos] = struct{}{}
			woken = h.wake(pos, s, t.known, false)
		} else if h.wokenForWrite[pos] {
			delete(h.wokenForWrite, pos)
			carried := h.carried[pos]
			delete(h.carried, pos)
			woken = h.standUp(pos, carried, s, t.known, false, true)
		} else {
			continue
		}
		fx.loaded.add(woken)
	}
}

// crossPortals takes bodies whose portal crossing came due out of the herd,
// with their mob riders, to arrive after the mob lock is released.
func (t *mobDimTick) crossPortals(fx *mobEffects) {
	h, w := t.h, t.w
	// The portal pass runs before the advance, as for items.
	portalHerd := h.herd[:0]
	var crossing map[*entity.Mob]bool
	for _, m := range h.herd {
		ent := h.record(m)
		if ent == nil || m.Dormant || m.Type == entity.TypeEnderDragon || !m.CanUsePortal() {
			portalHerd = append(portalHerd, m)
			continue
		}
		if m.Type == entity.TypeSulfurCube && ent.portal.Cooldown > 0 {
			if t.sulfurPortalCooling == nil {
				t.sulfurPortalCooling = make(map[*entity.Mob]bool)
			}
			t.sulfurPortalCooling[m] = true
		}
		state := w.BlockAt(rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z))
		if m.Vehicle != nil && !m.Discarded() {
			// A mob's rider is not a body that can use a portal; it crosses
			// with its mount. vanilla: Entity.canUsePortal
			ent.portal.StepRider(state, portalCooldownTicks)
			portalHerd = append(portalHerd, m)
			continue
		}
		if m.RidesVehicle || m.Discarded() {
			// A boat's rider is no body to use a portal, its boat carrying
			// it; nor is one already gone. Air (state 0) only runs the
			// cooldown and decay. vanilla: Entity.canUsePortal
			state = 0
		}
		t.s.notePortalTouch(nil, ent.uuid, w, m.X, m.Y, m.Z, state)
		kind, due := ent.portal.Step(state, rules.PortalTransitionTime, portalCooldownTicks)
		if !due {
			if !m.RidesVehicle && !m.Dead {
				if state == rules.EndGatewayState {
					srv := t.s
					gatewayTouchAt(&fx.gateways, w, m.X, m.Y, m.Z, gatewayTouch{
						mu: &srv.mobsMu, portal: &ent.portal, cooldown: portalCooldownTicks,
						move: func(to player.Position) { srv.gatewayMoveMob(ent, to) },
					})
				}
			}
			portalHerd = append(portalHerd, m)
			continue
		}
		h.forget(m)
		fx.travels = append(fx.travels, mobPortalTravel{ent: ent, kind: kind, riders: h.takeRiders(m, &crossing)})
	}
	if len(crossing) > 0 {
		portalHerd = slices.DeleteFunc(portalHerd, func(m *entity.Mob) bool { return crossing[m] })
	}
	h.setHerd(portalHerd)
}

// takeRiders forgets m's mob riders, and theirs, marking each in crossing
// for the caller to take out of the herd; the rides stay linked, so a
// crossing with nowhere to arrive puts the stack back whole.
// vanilla: Entity.teleportCrossDimension teleports each passenger
func (h *mobHerd) takeRiders(m *entity.Mob, crossing *map[*entity.Mob]bool) []mobPortalTravel {
	var riders []mobPortalTravel
	for _, p := range m.Passengers {
		ent := h.record(p)
		if ent == nil {
			continue
		}
		if *crossing == nil {
			*crossing = make(map[*entity.Mob]bool)
		}
		(*crossing)[p] = true
		h.forget(p)
		riders = append(riders, mobPortalTravel{ent: ent, riders: h.takeRiders(p, crossing)})
	}
	return riders
}

// markDormant flags the mobs standing in chunks no player's simulation
// distance, forced chunk or plugin ticket entity-ticks.
func (t *mobDimTick) markDormant(in *mobTickInputs) {
	var centres []world.ChunkPos
	for _, c := range in.inWorld[t.w] {
		if c.simulates() {
			centres = append(centres, playerChunk(c.player.Position()))
		}
	}
	reach := t.s.simulationDistance.Load()
	for _, m := range t.h.herd {
		m.Dormant = !entityTicksIn(t.w, centres, reach, herdChunk(m))
	}
}

// advance steps the herd one tick, reads door changes, ink and landings
// off the bodies, and returns the tick's events.
func (t *mobDimTick) advance(fx *mobEffects, in *mobTickInputs) (events []entity.MobEvent) {
	s, h, w := t.s, t.h, t.w
	// The tick returns deaths and laid eggs; movement is scheduleMovement's.
	for _, m := range h.herd {
		switch m.Type {
		case entity.TypeSulfurCube:
			// vanilla: SulfurCube.primeWhenOnPoweredPosition
			if m.SulfurCanExplode() {
				m.SetSulfurPowered(in.sulfurPower[sulfurCell(w, m)])
			}
		case entity.TypeIllusioner:
			m.Effective = s.effectiveDifficultyAt(w, m.X, m.Z)
		case entity.TypeDolphin:
			m.DolphinItems = t.foxItems[w]
		case entity.TypeNautilus, entity.TypeZombieNautilus:
			m.NautilusIdentities = h
		case entity.TypeWarden:
			m.WardenIdentities = h
		case entity.TypeAllay, entity.TypeIronGolem, entity.TypeZombie, entity.TypeHusk, entity.TypeZombieVillager:
			m.Village = t.dim.villageView
		case entity.TypePanda:
			m.SetPandaItems(t.foxItems[w])
		case entity.TypeFox:
			m.Fox.Items = t.foxItems[w]
			m.Village = t.dim.villageView
		case entity.TypeCat:
			if t.dim.chestOpen == nil {
				t.dim.chests = chestWatch{w: w}
				t.dim.chestOpen = t.dim.chests.open
			}
			t.dim.chests.clients = in.inWorld[w]
			m.ChestOpen = t.dim.chestOpen
		case entity.TypeCopperGolem:
			t.dim.golemChests.s, t.dim.golemChests.w, t.dim.golemChests.dim, t.dim.golemChests.clients = s, w, t.dim, in.inWorld[w]
			m.Chests = &t.dim.golemChests
		case entity.TypeVillager:
			m.OfferUpdater = villagerOfferUpdater{}
			m.Village = t.dim.villageView
			if t.dim.drawTrades == nil {
				dim := t.dim
				dim.drawTrades = func(m *entity.Mob) {
					if s.tradeDraws[m] == nil {
						dim.tradeDraws = append(dim.tradeDraws, s.prepareTradeDraw(w, m))
					}
				}
			}
			m.DrawTrades = t.dim.drawTrades
		}
	}
	t.dim.villageView.updateOccupied(h.herd)
	gameTime, dayTime := w.Time()
	for m, ent := range h.book {
		m.SetMetadataGameTime(gameTime)
		m.DayTime = dayTime
		m.Team, m.TeamCollision = "", entity.CollideAlways
		if in.teams != nil {
			m.Team = m.TeamWithOwner(in.teams[ent.uuidText], func(id uuid.UUID) (*entity.Mob, string) {
				for _, dim := range s.allDims() {
					if owner := dim.mobs.byUUID[id]; owner != nil {
						return owner, in.teams[dim.mobs.book[owner].uuidText]
					}
				}
				return nil, in.playerTeams[id]
			})
			m.TeamCollision = in.teamRules[m.Team]
		}
		// vanilla: Vindicator.setCustomName
		if m.Type == entity.TypeVindicator && !m.Johnny && nameIsJohnny(ent.name) {
			m.MarkJohnny()
		}
		if m.IsCreaking() {
			m.CreakingOrphaned(creakingOrphaned(w, ent))
		}
	}
	ew := t.dim.colliders[mobCollisions].reset(w, t.hulls[w], h.herd)
	h.herd.BlocksChanged(ew, t.navChanges, &h.paths)
	var crowd *entity.PushCrowd
	if len(t.dim.vehicles.Bodies) > 0 {
		crowd = &t.dim.vehicles
	}
	var prevTargets map[*entity.Mob]int32
	if s.targetListened() {
		prevTargets = h.targetsOf()
	}
	events = h.herd.TickInto(h.events, ew, in.players[w], in.holders[w], crowd, in.stands[w], &h.paths)
	if prevTargets != nil {
		s.askTargetChanges(h, prevTargets)
	}
	// Read the body's pending effects once; application stays after the lock.
	// Ink includes hurts not told elsewhere (blast, fall, fire), and landings
	// feed the farmland trample (vanilla's fallOn).
	for m, ent := range h.book {
		// entity_damage for a mob hurt since the last tick, before any death
		// event; a hurt landed outside the tick is told up to one tick late.
		// vanilla: LivingEntity.actuallyHurt
		if m.TakeDamagedEvent() {
			fx.gameEvent(w, rules.GameEventEntityDamage, m.X, m.Y, m.Z, rules.GameEventContext{Mob: ent.uuid})
		}
		for _, op := range m.TakeDoorOps() {
			fx.doorOps = append(fx.doorOps, mobDoorOp{w: w, op: op})
		}
		for _, op := range m.TakeBedOps() {
			fx.bedOps = append(fx.bedOps, mobBedOp{w: w, op: op})
		}
		// LodestoneTrackerOf only reads Raw components.
		if len(m.MainHand.Raw) > 0 || len(m.OffHand.Raw) > 0 ||
			len(m.Worn[0].Raw) > 0 || len(m.Worn[1].Raw) > 0 || len(m.Worn[2].Raw) > 0 || len(m.Worn[3].Raw) > 0 ||
			len(m.Body.Raw) > 0 || len(m.Horse.Saddle.Raw) > 0 || len(m.Antenna.Raw) > 0 {
			tickMobCompasses(m, w, t.dim.name)
		}
		if cloud := takeInk(m); cloud != nil {
			fx.inks = append(fx.inks, mobInk{w: w, kind: m.Type, cloud: cloud})
		}
		if m.LandedFall > 0 {
			fx.tramples = append(fx.tramples, mobTrample{
				w: w,
				x: m.X, y: m.Y, z: m.Z,
				width:  m.BbWidth(),
				height: m.BbHeight(),
				fall:   m.LandedFall,
				kind:   m.Type,
				uuid:   t.h.MobUUID(m),
			})
			if m.Type != entity.TypeWarden {
				sx, sy, sz := rules.StandingBlock(w, m.X, m.Y, m.Z, m.BbWidth()/2)
				fx.gameEvent(w, rules.GameEventHitGround, m.X, m.Y, m.Z, rules.GameEventContext{Mob: ent.uuid, Affected: w.BlockAt(sx, sy, sz), Sneaking: m.Crouching})
			}
			m.LandedFall = 0
		} else if m.OnGround && !m.SteppingCarefully() && world.IsTurtleEgg(w.BlockAt(rules.BlockOf(m.X), rules.BlockOf(m.Y-0.2), rules.BlockOf(m.Z))) {
			// Standing on an egg rolls its stepOn every tick.
			fx.tramples = append(fx.tramples, mobTrample{w: w, x: m.X, y: m.Y, z: m.Z, kind: m.Type})
		}
	}
	return events
}

// scheduleMovement updates who sees each mob and queues its metadata,
// attribute, equipment and movement packets.
func (t *mobDimTick) scheduleMovement(fx *mobEffects, in *mobTickInputs) {
	h, w := t.h, t.w
	clients, players := in.inWorld[w], in.players[w]
	broadcastPct := int(t.s.entityBroadcastRange.Load())
	near := &nearbyPlayers{n: len(clients), view: func(i int) trackingWatcher {
		c, p := clients[i], players[i]
		return trackingWatcher{c: c, x: p.X, z: p.Z, chunkRadius: c.chunkRadius()}
	}}
	defer near.release()
	// Visibility is settled against tracking range; the movement tick comes on
	// the type's update interval or the tick after an impulse.
	for _, m := range h.herd {
		ent := h.record(m)
		if ent == nil {
			continue
		}
		body := mobTracker.body(ent)
		body.broadcastPct, body.broadcastRead = broadcastPct, true
		motion := mobMovement{seated: ent.seated()}
		mt := body.t.ride(body.t.stepDue(m.UpdateInterval(), m.Impulse), motion.seated)
		mt.teleported = ent.teleported
		m.Impulse = false
		// Equipment is read once and shared by every viewer.
		equippedOnce := false
		// The metadata state is read once too, on the first viewer in range.
		var now *mobMetadataState
		nowOnce := false
		deltas := mobMetadataDeltas{ent: ent}
		// The attributes a command changed since the last tick.
		var attrChanged []byte
		if a := m.Attributes(); a != nil {
			m.SyncSpeedModifiers(a)
			m.SyncEquipmentModifiers(a)
			if dirty := a.TakeDirty(); len(dirty) > 0 {
				attrChanged = attributesPayload(m.ID, dirty)
			}
		}
		for _, i := range body.watchers(near) {
			c := clients[i]
			if c.theWorld() != w {
				continue // crossed to another dimension since the snapshot
			}
			was, seen := mobSettle(&body, c, players[i].X, players[i].Z, c.chunkRadius(), &fx.sends)
			if seen {
				// Metadata changes (hunt flags, fire, health, effect particles) go out the
				// tick they change.
				if !nowOnce {
					now = ent.metadataState()
					nowOnce = true
				}
				if now != was.own.meta {
					old := was.own.meta
					was.own.meta = now
					deltas.now = now
					for _, p := range deltas.from(old) {
						fx.sends = append(fx.sends, mobSend{c: c, w: w, id: p.id, data: p.data})
					}
				}
				if attrChanged != nil {
					fx.sends = append(fx.sends, mobSend{c: c, w: w, id: protocol.IDPlayUpdateAttributes, data: attrChanged})
				}
				// Send equipment slots that differ from what this client was told, wear
				// included.
				if !equippedOnce {
					equippedOnce = true
					// Only writers invalidate the shared snapshot; each watcher still
					// compares against its own last send, including a fresh pairing.
					if ent.equipGen == 0 || ent.equipRead != m.EquipmentRevision() {
						ent.equipRead = m.EquipmentRevision()
						if ent.equipGen == 0 || !mobEquipmentSame(&ent.equipSeen, m) {
							ent.equipSeen = mobEquipmentNow(m)
							ent.equipGen++
						}
					}
				}
				if was.own.wornGen != ent.equipGen {
					equipped := &ent.equipSeen
					used := equipmentChanged(&was.own.worn, equipped)
					was.own.worn = *equipped
					was.own.wornGen = ent.equipGen
					if len(used) > 0 {
						fx.sends = append(fx.sends, mobSend{c: c, w: w, id: protocol.IDPlayEntityEquipment,
							data: equipmentEntriesPayload(m.ID, equipped, used)})
					}
				}
			}
			if seen && mt.gate {
				motion.move(&body, was, c, mt, &fx.sends)
			}
		}
		if mt.gate {
			ent.teleported = false
		}
	}
}

// countCorpses counts corpses down and removes expired ones, queueing the
// splits and death effects they leave.
func (t *mobDimTick) countCorpses(fx *mobEffects, dyingCount int) {
	h, w := t.h, t.w
	// An expired corpse is removed for its watchers with the death poof
	// vanilla broadcasts as the animation ends.
	stillDying := h.dying[:0]
	for i, body := range h.dying {
		// New corpses already spent their countdowns in Mob.tickInto.
		if i < dyingCount {
			body.ent.mob.TickTimers()
		}
		body.ent.mob.DeathTime++
		body.remains--
		if body.remains > 0 {
			stillDying = append(stillDying, body)
			continue
		}
		body.ent.dying = false
		// vanilla: LivingEntity.tickDeath
		fx.entityEvent(w, body.id, protocol.EntityStatusDeathPoof)
		fx.remove(w, body.id)
		// A corpse keeps its scores and team until it is removed.
		// vanilla: ServerLevel.EntityCallbacks.onDestroyed
		fx.scoreDrops = append(fx.scoreDrops, goneMob{body.ent.uuid, plugin.RemoveDeath})
		if body.ent.mob.CreakingTearingDown() {
			// vanilla: Creaking.tearDown
			fx.voiceID(w, body.ent.mob, registry.SoundEntityCreakingDeath, body.ent.mob.SoundVolume(), body.ent.mob.VoicePitch())
		}
		// vanilla: AbstractCubeMob.remove splits a dead cube
		for _, split := range body.ent.mob.Splits() {
			fx.splits = append(fx.splits, cubeSplit{w: w, from: body.ent, split: split})
		}
		// Read the dice, hull and place for death effects under the lock; they
		// fire outside it. vanilla: LivingEntity.remove's triggerOnDeathMobEffects
		m := body.ent.mob
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		fx.triggers = append(fx.triggers, mobEffectTrigger{
			w: w, body: m,
			box: entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ).Inflate(2),
			x:   m.X, y: m.Y, z: m.Z,
			dice: m.Rand, effects: m.AllEffects(),
			killed: true, height: maxY - minY,
		})
	}
	h.dying = append(stillDying, t.corpses...)
}

// addMobHolders offers every wandering trader and every mob that holds a
// lead to every herd as a leash holder. Called under the mob lock.
func (s *Server) addMobHolders(in *mobTickInputs) {
	// held is the IDs of the bodies some mob's lead is tied to.
	var held map[int32]bool
	for _, src := range s.allDims() {
		for _, m := range src.mobs.herd {
			if m.LeashHolderID != 0 {
				if held == nil {
					held = map[int32]bool{}
				}
				held[m.LeashHolderID] = true
			}
		}
	}
	for _, src := range s.allDims() {
		for _, m := range src.mobs.herd {
			trader := m.Type == entity.TypeWanderingTrader
			if !trader && !held[m.ID] {
				continue
			}
			h := entity.LeashHolder{
				EntityID: m.ID, X: m.X, Y: m.Y, Z: m.Z,
				Width: m.BbWidth(), Height: m.BbHeight(),
				Present: m.Alive(),
			}
			if trader {
				h.Trader, h.DespawnDelay = true, m.DespawnDelay
			}
			for _, dst := range s.allDims() {
				h.SameLevel = dst == src
				in.holders[dst.w] = append(in.holders[dst.w], h)
			}
		}
	}
}

// MobUUID resolves a mob identity under the herd lock.
func (h *mobHerd) MobUUID(m *entity.Mob) uuid.UUID {
	if ent := h.book[m]; ent != nil {
		return ent.uuid
	}
	return uuid.Nil
}

// teamCollisionRules is each team's collision rule, nil while there are no teams.
func (s *Server) teamCollisionRules() map[string]entity.CollisionRule {
	rules := s.scoreboard.TeamCollisions()
	if rules == nil {
		return nil
	}
	out := make(map[string]entity.CollisionRule, len(rules))
	for name, r := range rules {
		out[name] = entity.CollisionRule(r)
	}
	return out
}
