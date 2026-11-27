package server

import (
	"log/slog"
	"math"
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

// Server-side state for the falling blocks a gravity block leaves its
// place as, kept and shown like the primed TNT. The fall and landing
// themselves are entity.FallingBlock; the server holds the UUID, entity ID,
// dimension and per-observer tracking. A landing places its block through
// the same block-changed path every placed block does, and a block that
// cannot place goes out as an item. The falling lock sits beneath the
// player list's and alone of the entity ones, and sends run after the locks
// are down. There are no motion packets for a falling block — the vanilla
// type does not track its delta, the velocity riding the position packets.
// vanilla: Entity.spawnAtLocation

// fallingEntity is one falling block in a world. The fall itself is
// entity.FallingBlock; the server holds the dimension, UUID and tracking.
type fallingEntity struct {
	fb   *entity.FallingBlock
	w    *world.World
	uuid uuid.UUID

	bodyTrack[struct{}]

	// portal is the body's portal state, shared with every other body of
	// vanilla's Entity.
	portal rules.EntityPortal

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the falling-block lock, like the rest of
	// a block's own fields.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// fallingTracker tracks the falling blocks. The velocity rides the
// Teleport Entity: the type does not track its delta.
var fallingTracker = trackedKind[*fallingEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.fallings.built = false },
	pistonBit:        pistonFallings,

	rangeChunks: entity.FallingBlockTrackingRange,
	interval:    entity.FallingBlockUpdateInterval,
	lock:        func(s *Server) *sync.Mutex { return &s.fallingMu },
	list:        func(s *Server) *[]*fallingEntity { return &s.fallings },
	chunks:      func(s *Server) *entity.ChunkIndex[*fallingEntity] { return &s.fallingChunks },
	filing:      func(it *fallingEntity) *entity.Filing { return &it.fb.Filing },
	uuid:        func(it *fallingEntity) uuid.UUID { return it.uuid },
	track:       func(it *fallingEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *fallingEntity) *world.World { return it.w },
	id:          func(it *fallingEntity) int32 { return it.fb.ID },
	dead:        func(it *fallingEntity) bool { return it.fb.Dead },
	pose: func(it *fallingEntity) bodyPose {
		fb := it.fb
		return bodyPose{x: fb.X, y: fb.Y, z: fb.Z, vx: fb.VX, vy: fb.VY, vz: fb.VZ, onGround: fb.OnGround}
	},
	pairing: fallingPairing,
}

// fallingHurt is one landing's harm waiting for the bodies it covers: the
// damage and its type, the body that dealt it and where it stood, its name
// for the death message — the block's English name under vanilla's "Falling"
// prefix — and the hurt box the victims are gathered from.
type fallingHurt struct {
	w                *world.World
	id               int32
	x, y, z          float64
	name             string
	damage           float64
	damageType       registry.DamageType
	minX, minY, minZ float64
	maxX, maxY, maxZ float64
}

// hurtFallingVictims lands one fall's hurt on the living bodies its box
// covers, the no-creative-or-spectator still-alive rule. A player goes
// through the hurt pipeline, which refuses the modes that cannot be hurt and
// keeps the death message; a mob takes it as its own hurt, told about as
// vanilla's hurtServer tells one.
// vanilla: FallingBlockEntity.causeFallDamage
func (s *Server) hurtFallingVictims(h fallingHurt) {
	cause := &damageCause{entityID: h.id, x: h.x, y: h.y, z: h.z,
		killer: &rules.Killer{Name: protocol.Text{Text: h.name}}}

	s.mu.RLock()
	var clients []*client
	for _, c := range s.clients {
		if c.theWorld() == h.w {
			clients = append(clients, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range clients {
		minX, minY, minZ, maxX, maxY, maxZ := c.Box()
		if !boxHits(h.minX, h.minY, h.minZ, h.maxX, h.maxY, h.maxZ,
			minX, minY, minZ, maxX, maxY, maxZ) {
			continue
		}
		if landed, _ := c.hurt(h.damage, h.damageType, cause); landed {
			c.knockbackFromPoint(h.w, h.x, h.y, h.z, syncVelocityAfterTravel)
		}
	}

	s.lockTick(&s.mobsMu)
	var hits []*entity.Mob
	for _, m := range s.herdOf(h.w).herd {
		if m.Dead {
			continue
		}
		mobMinX, mobMinY, mobMinZ, mobMaxX, mobMaxY, mobMaxZ := m.BlastBox()
		if !boxHits(h.minX, h.minY, h.minZ, h.maxX, h.maxY, h.maxZ,
			mobMinX, mobMinY, mobMinZ, mobMaxX, mobMaxY, mobMaxZ) {
			continue
		}
		// vanilla: EntityTypes.FALLING_BLOCK, EntityDimensions.defaultEyeHeight
		from := entity.HitSource{X: h.x, Y: h.y, Z: h.z, Eye: h.y + float64(float32(0.98)*float32(0.85))}
		if m.HurtByFallFrom(h.damage, h.damageType, h.x, h.z, &from) {
			hits = append(hits, m)
		}
	}
	s.unlockTick(&s.mobsMu)

	// The telling of a mob's hurt, the event and the flinch a watching
	// client flashes; the health itself rides the tick's metadata.
	for _, m := range hits {
		s.broadcastWorld(h.w, protocol.IDPlayDamageEvent,
			damageEventPayload(m.ID, h.damageType, causeEntityID(cause), causeEntityID(cause)))
		s.broadcastWorld(h.w, protocol.IDPlayHurtAnimation,
			hurtAnimationPayload(m.ID, float32(math.Atan2(-(h.x-m.X), h.z-m.Z)*180/math.Pi)))
	}
}

// boxHits reports whether two boxes overlap, AABB.intersects' strict test.
func boxHits(aMinX, aMinY, aMinZ, aMaxX, aMaxY, aMaxZ, bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ float64) bool {
	return aMinX < bMaxX && aMaxX > bMinX &&
		aMinY < bMaxY && aMaxY > bMinY &&
		aMinZ < bMaxZ && aMaxZ > bMinZ
}

// tickFallingBlocks runs one game tick of every falling block: where
// everyone stands is read first with nothing held; physics and movements
// run under the falling lock alone; the block the landing set, the sends
// and the dropped item all run after.
func (s *Server) tickFallingBlocks() {
	s.lockTick(&s.fallingMu)
	none := len(s.fallings) == 0
	s.unlockTick(&s.fallingMu)
	if none {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	watchers := make(map[*world.World][]trackingWatcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		watchers[c.theWorld()] = append(watchers[c.theWorld()], trackingWatcher{c: c, x: pos.X, z: pos.Z, chunkRadius: c.chunkRadius()})
	}
	near := nearbyWorlds(watchers, func(w trackingWatcher) trackingWatcher { return w })
	defer releaseNearbyWorlds(near)

	var out trackSends
	removals := make(map[*world.World][]int32)

	// landing is a body whose tick ended it in a placed block; drop is
	// one ended in an item laid down; hurt is a landing's harm waiting for
	// the bodies it covers to be found. All three are collected under the
	// lock and run after it — the world's writes and the victim queries are
	// not made inside the tick's.
	type landing struct {
		w       *world.World
		x, y, z int32
		state   uint16
		silent  bool
	}
	type drop struct {
		w      *world.World
		ev     entity.FallingEvent
		silent bool
	}
	var (
		landings     []landing
		bodyLandings []bodyLanding
		drops        []drop
		hurts        []fallingHurt
		spawnBreaks  []landing
		ores         []cellSpot
		spawnCells   []entity.Waypoint
	)

	colliders := s.collidersByWorld(fallingCollisions)
	gate := s.newEntityGate()
	var held []*fallingEntity
	defer func() { fallingTracker.dormantPass(s, held) }()
	s.lockTick(&s.fallingMu)
	// The portal pass runs ahead of the tick, as for items: a body the
	// portal is sending across leaves the list here.
	type fallingPortalTravel struct {
		it   *fallingEntity
		kind rules.PortalKind
	}
	var travels []fallingPortalTravel
	var gates []gatewayTouch
	portalAlive := s.fallings[:0]
	for _, it := range s.fallings {
		if it.fb.Dead {
			fallingTracker.unfile(it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.fb.X, it.fb.Y, it.fb.Z, &it.portal, portalCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.fb.X, it.fb.Y, it.fb.Z, state)
		if !due {
			if state == rules.EndGatewayState {
				gatewayTouchAt(&gates, it.w, it.fb.X, it.fb.Y, it.fb.Z, gatewayTouch{
					mu: &s.fallingMu, portal: &it.portal, cooldown: portalCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveFalling(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			continue
		}
		fallingTracker.unfile(it)
		travels = append(travels, fallingPortalTravel{it: it, kind: kind})
	}
	s.fallings = portalAlive

	alive := s.fallings[:0]
	for _, it := range s.fallings {
		if it.fb.Dead {
			fallingTracker.unfile(it)
			continue // the landing happened before this tick
		}
		if !gate.ticks(it.w, it.fb.X, it.fb.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		events := it.fb.Step(colliders[it.w])
		if it.fb.LandedFall > 0 {
			it.fb.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.fb.X, y: it.fb.Y, z: it.fb.Z, halfWidth: entity.FallingHalfWidth})
		}
		spawnCells = it.fb.FrogspawnInside(it.w, spawnCells)
		for _, p := range spawnCells {
			spawnBreaks = append(spawnBreaks, landing{w: it.w, x: p.X, y: p.Y, z: p.Z})
		}

		if it.fb.Dead {
			// The tick ended the body: it is out of the world before
			// anything is told about it.
			for _, ev := range events {
				switch ev.Kind {
				case entity.FallingLanded:
					landings = append(landings, landing{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, state: ev.State, silent: it.look.silent})
				case entity.FallingDropped, entity.FallingBroken:
					drops = append(drops, drop{w: it.w, ev: ev, silent: it.look.silent})
				case entity.FallingSteppedOnRedstoneOre:
					ores = append(ores, cellSpot{it.w, ev.X, ev.Y, ev.Z})
				case entity.FallingHurt:
					minX, minY, minZ, maxX, maxY, maxZ := it.fb.HurtBox()
					name, _, _ := registry.BlockStateName(it.fb.BlockState)
					hurts = append(hurts, fallingHurt{
						w: it.w, id: it.fb.ID, x: it.fb.X, y: it.fb.Y, z: it.fb.Z,
						name: "Falling " + itemEnglishName(name), damage: ev.Damage, damageType: ev.DamageType,
						minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ,
					})
				}
			}
			removals[it.w] = append(removals[it.w], it.fb.ID)
			s.entityDestroyed(it.uuid)
			fallingTracker.unfile(it)
			continue
		}

		body := fallingTracker.body(it)
		mt := body.schedule(false)
		for _, i := range body.watchers(near[it.w]) {
			wh := watchers[it.w][i]
			c := wh.c
			if c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			if was, seen := body.settle(c, wh.x, wh.z, wh.chunkRadius, &out); seen && mt.gate {
				body.moveTold(was, c, mt, &out)
			}
		}
		if it.fb.Dead {
			fallingTracker.unfile(it)
		} else {
			alive = append(alive, it)
		}
	}
	s.fallings = alive
	s.unlockTick(&s.fallingMu)
	s.runBodyLandings(bodyLandings)

	for _, p := range spawnBreaks {
		if registry.BlockOf(p.w.BlockAt(p.x, p.y, p.z)) == registry.BlockFrogspawn {
			s.setBlockAndTell(p.w, p.x, p.y, p.z, registry.BlockAir.DefaultState())
		}
	}
	// The ore is lit before the hurts and landings.
	for _, o := range ores {
		s.lightRedstoneOre(o.w, o.x, o.y, o.z)
	}
	// The hurts land before the landings, vanilla's causeFallDamage running
	// inside the move that came to rest and before the tick sets its block.
	for _, h := range hurts {
		s.hurtFallingVictims(h)
	}

	// Landings next — vanilla's tick sets the block and tells its update
	// inside itself — the block set into the world, the neighbours of the
	// change asked (a torch above the new block breaks, water flows into
	// the air the fall left), every change told out with the column
	// relit. The landing's own level event goes out after the block
	// update, the order the vanilla tick's setBlock, onLand and discard
	// leave them in.
	for _, l := range landings {
		old := l.w.BlockAt(l.x, l.y, l.z)
		if !l.w.SetBlock(l.x, l.y, l.z, l.state) {
			slog.Warn("a falling block could not place itself in its own world", "x", l.x, "y", l.y, "z", l.z)
			continue
		}
		// A stair, fence or wall beside where the block landed reshapes, the
		// shape update the vanilla tick's setBlock runs.
		s.updateNeighborShapes(l.w, l.x, l.y, l.z)
		wake, effects, primes := s.physicsFor(l.w).NotifyChanged(l.x, l.y, l.z, old)
		changes := append([]world.BlockChange{{X: l.x, Y: l.y, Z: l.z, State: l.state}}, wake...)
		for _, e := range effects {
			s.tellEffect(l.w, e)
		}
		for _, t := range primes {
			s.primeTNT(l.w, t)
		}
		s.tellBlockChanges(l.w, changes)
		// vanilla: FallingBlockEntity.tick; the client draws the landed state
		// until it re-meshes the section, so the block does not blink out
		// between the body's removal and the rebuild.
		s.broadcastBlockChunkExcept(l.w, nil, l.x, l.z, protocol.IDPlayAddTransientBlock,
			blockUpdate(l.x, l.y, l.z, l.w.BlockAt(l.x, l.y, l.z)))
		// vanilla: AnvilBlock.onLand checks !entity.isSilent()
		if event, ok := registry.BlockOf(l.state).FallingLandEvent(); ok && !l.silent {
			s.broadcastLevelEvent(l.w, event, l.x, l.y, l.z, 0)
		}
	}

	// The bodies whose tick ended them go out of every view next: vanilla
	// discards the body from tracking the moment its tick is done with
	// it, before the item a refused landing drops.
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}

	// The breaks and drops: the block that could not be placed as an item
	// where the body stood, as vanilla's spawnAtLocation lays one, after
	// the level event its break plays.
	for _, d := range drops {
		if d.ev.Kind == entity.FallingBroken {
			if !d.silent {
				s.playFallingBroken(d.w, d.ev)
			}
		} else {
			s.spawnFallingDrop(d.w, d.ev)
		}
	}

	out.send()

	// Each body the portal pass caught arrives as a new falling block in
	// the dimension its portal goes to; a crossing with nowhere to
	// arrive puts the body back in the list, the cooldown spent.
	s.touchGateways(gates)
	for _, tr := range travels {
		s.finishFallingTravel(tr.it, tr.kind)
	}
}

// spawnFallingBlock stands a falling block in the world where the physics
// handed it over — the centre of the block's cell at its bottom — carrying
// the state it had. A body whose spawn carries the hurt numbers stands up
// hurting what it lands on; the anvil's own fall hurts entities.
// vanilla: FallingBlockEntity.fall
func (s *Server) spawnFallingBlock(w *world.World, f world.FallingSpawn) *fallingEntity {
	id := s.nextEntity()
	it := &fallingEntity{
		fb: entity.NewFallingBlock(id, float64(f.X)+0.5, float64(f.Y), float64(f.Z)+0.5,
			0, 0, 0, f.State, s.entitySeed(id)),
		uuid: uuid.New(),
		w:    w,
	}
	it.fb.StartX, it.fb.StartY, it.fb.StartZ = f.X, f.Y, f.Z
	// vanilla: BrushableBlock.tick calls disableDrop on the falling body.
	switch registry.BlockOf(f.State) {
	case registry.BlockSuspiciousSand, registry.BlockSuspiciousGravel:
		it.fb.CancelDrop = true
	}
	// vanilla: the block's own falling hook, fall()'s call on the body it
	// stands up. The speleothem tip carries its hurt numbers with it; an
	// anvil hurts for two a block up to forty, and its fall
	// is the one whose wear the landing rolls against.
	if f.HurtEntities {
		it.fb.SetHurtsEntities(f.FallHurtAmount, f.FallHurtMax)
	} else {
		switch registry.BlockOf(f.State) {
		case registry.BlockAnvil, registry.BlockChippedAnvil, registry.BlockDamagedAnvil:
			it.fb.SetHurtsEntities(2.0, 40)
		}
	}
	s.registerFalling(it)
	return it
}

// summonFallingBlock stands a /summon's falling block up at the position
// given: read through the same loader a saved fall's wake reads — the
// BlockState its tag named, sand where the tag named none — with the
// command's position the one it falls from and the tag's own motion riding
// along. A load that comes to nothing and a tag whose UUID is already a
// live body's are the two refusals, the notifier the caller's.
// vanilla: SummonCommand.createEntity
func (s *Server) summonFallingBlock(w *world.World, x, y, z float64, tag nbt.Compound) (*fallingEntity, error) {
	compound := summonTag(tag, fallingEntityID, x, y, z)
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	// The chunk the body falls in is loaded for it, the way every
	// summon's is.
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	it := fallingOfTag(s, w, compound)
	w.Release(pos)
	if it == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(it.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	it.fb.SetPos(x, y, z)
	s.registerFalling(it)
	return it, nil
}

// registerFalling puts a built falling block into the world and shows it to
// every player within the type's tracking range — the same both-locks
// moment registerItem explains. It is the one door every arrival into
// the world but a chunk's own wake walks: a chunk's saved bodies take the
// wake and arrive here, and the falls the physics engine hands over take
// it through spawnFallingBlock.
func (s *Server) registerFalling(it *fallingEntity) { fallingTracker.register(s, it) }

// fallingPairing is what shows a falling block: the spawn, then the cell
// the fall began in where fall() set one.
func fallingPairing(it *fallingEntity) []outbound {
	return spawnAndMetadata(fallingSpawnPayload(it), fallingMetadataPayload(it))
}

// fallingMetadataPayload builds the metadata carrying the glowing bit, the no-gravity flag
// and the fall's start, or nil at both defaults, which pairing leaves out.
func fallingMetadataPayload(it *fallingEntity) []byte {
	fb := it.fb
	flags := it.look.glowFlags()
	if fb.StartX == 0 && fb.StartY == 0 && fb.StartZ == 0 && !fb.NoGravity && it.custom == (customName{}) && flags == 0 && !it.look.silent {
		return nil
	}
	var b protocol.Buffer
	b.PutVarInt(fb.ID)
	it.look.putSilent(&b, false)
	if flags != 0 {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(flags)
	}
	if fb.NoGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(true)
	}
	if fb.StartX != 0 || fb.StartY != 0 || fb.StartZ != 0 {
		b.PutMetadata(protocol.MetadataIndexFallingBlockStartPos, protocol.MetadataBlockPos)
		b.PutPosition(fb.StartX, fb.StartY, fb.StartZ)
	}
	it.custom.putMetadata(&b, customName{})
	b.EndMetadata()
	return b.Bytes()
}

// fallingFields is every synched value of a falling block.
func fallingFields(it *fallingEntity) metaFields {
	fb := it.fb
	var m metaFields
	it.look.addSilentField(&m)
	m.add(protocol.MetadataIndexEntityFlags, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(it.look.glowFlags()) })
	m.add(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(fb.NoGravity) })
	m.add(protocol.MetadataIndexFallingBlockStartPos, protocol.MetadataBlockPos, func(b *protocol.Buffer) {
		b.PutPosition(fb.StartX, fb.StartY, fb.StartZ)
	})
	it.custom.addFields(&m)
	return m
}

// fallingSpawnPayload builds the Spawn Entity packet that puts a falling
// block in the world for a client: entity ID and UUID, the
// minecraft:falling_block type, position — the block state ID riding the
// packet's data field, which is how the client knows what it watches fall.
func fallingSpawnPayload(it *fallingEntity) []byte {
	return spawnEntityPayload(spawnEntity{
		id: it.fb.ID, uuid: it.uuid, typ: protocol.EntityTypeFallingBlock,
		x: it.fb.X, y: it.fb.Y, z: it.fb.Z,
		vx: it.fb.VX, vy: it.fb.VY, vz: it.fb.VZ,
		data: int32(it.fb.BlockState),
	})
}

// spawnFallingDrop lays the block a landing could not place down as an item
// where the body stood: the block's own item, one of it, pushed the little
// hop vanilla's ItemEntity constructor gives every drop.
func (s *Server) spawnFallingDrop(w *world.World, ev entity.FallingEvent) {
	if int(ev.State) >= registry.StateCount {
		return
	}
	item, ok := registry.BlockOf(ev.State).Item()
	if !ok {
		// A block without an item form of its own has nothing to drop;
		// the body is already gone, which is the whole of vanilla's
		// discard for one.
		return
	}
	it := s.newItemEntity(w, protocol.ItemStack{ItemID: int32(item), Count: 1},
		ev.FeetX, ev.FeetY, ev.FeetZ,
		s.popRng.Float64()*0.2-0.1, 0.2, s.popRng.Float64()*0.2-0.1,
		entity.BreakPickupDelay)
	// The falling blocks tick ahead of the items, but vanilla iterates one
	// entity list.
	it.skipStep = true
	s.registerFreshItem(it)
}

// playFallingBroken plays the level event the block's kind plays where its
// fall broke it: at the landing cell (anvil, speleothem), or with the state
// at the centre of the body for a block-break event (brushable).
// vanilla: Fallable.onBrokenAfterFall
func (s *Server) playFallingBroken(w *world.World, ev entity.FallingEvent) {
	if int(ev.State) >= registry.StateCount {
		return
	}
	event, ok := registry.BlockOf(ev.State).FallingBrokenEvent()
	if !ok {
		return
	}
	if event == protocol.WorldEventBlockBreak {
		// vanilla: BrushableBlock.onBrokenAfterFall
		centerY := ev.FeetY + entity.FallingHeight/2
		s.broadcastLevelEvent(w, event,
			int32(math.Floor(ev.FeetX)), int32(math.Floor(centerY)),
			int32(math.Floor(ev.FeetZ)), int32(ev.State))
		s.gameEvent(w, rules.GameEventBlockDestroy, ev.FeetX, centerY, ev.FeetZ, rules.GameEventContext{})
		return
	}
	s.broadcastLevelEvent(w, event, ev.X, ev.Y, ev.Z, 0)
}

// killFallingBlock discards a falling block the way the /kill command and
// vanilla's Entity.kill do: out of the world with nothing placed and
// nothing dropped.
func (s *Server) killFallingBlock(it *fallingEntity) {
	s.fallingMu.Lock()
	it.fb.Dead = true
	s.fallingMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.fb.ID}))
}

// teleportFallingBlock moves a falling block to the position given and stops
// its fall; the tick tells the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportFallingBlock(it *fallingEntity, to player.Position, by string) {
	fallingTracker.teleport(s, it, func() {
		it.fb.SetPos(to.X, to.Y, to.Z)
		it.fb.VX, it.fb.VY, it.fb.VZ = 0, 0, 0
		it.look.yaw, it.look.pitch = to.Yaw, to.Pitch
	})
	logTeleported("falling block", to, by)
}

// fallingTwinAt builds the falling block that continues this one's fall in
// another dimension, standing at pos: a new body, new ID but the same UUID,
// raised from the old one's entity tag, which keeps the block it carries,
// the times it keeps, the drop it gives up. The caller holds the falling
// blocks' lock or has taken the block out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) fallingTwinAt(it *fallingEntity, to *dimension, pos player.Position) *fallingEntity {
	twin := fallingOfTag(s, to.w, fallingTag(it).Entries())
	if twin == nil {
		return nil
	}
	twin.fb.SetPos(pos.X, pos.Y, pos.Z)
	return twin
}

// finishFallingTravel sends one falling block the portal pass caught across
// its portal (crossPortal).
func (s *Server) finishFallingTravel(it *fallingEntity, kind rules.PortalKind) {
	pos := player.Position{X: it.fb.X, Y: it.fb.Y, Z: it.fb.Z}
	s.crossPortal(it.w, it.fb.ID, pos, kind, func(to *dimension, spot world.SpawnPos, _ [2]int32) func() {
		twin := s.fallingTwinAt(it, to, standingAt(spot))
		if twin == nil {
			slog.Error("could not carry a falling block across a portal")
			return nil
		}
		return func() {
			s.registerFalling(twin)
			slog.Debug("falling block travelled through a portal",
				"to", to.name, "x", twin.fb.X, "y", twin.fb.Y, "z", twin.fb.Z)
		}
	}, func() {
		s.fallingMu.Lock()
		s.fallings = append(s.fallings, it)
		fallingTracker.file(s, it)
		s.fallingMu.Unlock()
	})
}
