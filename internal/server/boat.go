package server

import (
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Server-side state for the boats. The boat itself (the float, the rocking)
// is entity.Boat; the server holds the identity, the passengers and the
// tracking. The boat lock sits beneath the player list's and alone of the
// entity ones: it is never held while another of the server's locks is
// taken, and every read it guards is done in its own phase of the tick.

// boatEntity is one boat in a world. kind is the boat's item and entity type
// name. passengers is everyone aboard in mounting order, players by their
// connection and mobs by their entity, the list Set Passengers names; every
// one of them steps off a boat whose time has ended. cargo is a chest boat's
// 27 slots; a plain boat has none.
type boatEntity struct {
	boat *entity.Boat
	w    *world.World
	uuid uuid.UUID
	kind registry.EntityType

	bodyTrack[boatShown]

	// firstX, firstY, firstZ is where the hull stood at the start of the
	// tick, the base vanilla's handleMoveVehicle measures a driver's report
	// from: every report within one tick is checked against the tick's own
	// start, not against the report before it, so a client sending several
	// moves a tick cannot walk the hull away a legal step at a time.
	firstX, firstY, firstZ float64

	portal rules.EntityPortal

	// scoresSettled marks a dead hull whose scores the serving pass leaves
	// alone: /kill already reset them, or its UUID lives on in a twin or
	// with a logged-out rider. Guarded by the boat lock.
	scoresSettled bool

	passengers []*boatPassenger

	cargo        []protocol.ItemStack
	cargoVersion uint64 // slot writes, guarded by boatsMu
	spawnReason  uint8  // plugin.SpawnReason+1; 0 means SpawnDefault

	// lootTable is the table a chest boat's cargo is still to be rolled
	// from, with the seed the roll takes (0 for none); both are guarded by
	// the boat lock. The table is unpacked on the first open and on a break.
	lootTable string
	lootSeed  int64

	// holder is the entity ID of the body holding this hull's lead — a
	// player, a knot or a mob — 0 for an unleashed boat. The leash's own step drops
	// it for a holder gone or run out of reach, or an elastic stretch adds
	// its pull to the hull's velocity. leash names the holder as the save
	// does; set with holder 0, it is a lead read from a save still waiting
	// for its holder, leashWait the ticks it has waited. Change them
	// through tie and untie. Guarded by the boat lock.
	holder    int32
	leash     leashRef
	leashWait int

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the boat lock, like the rest of a boat's
	// own fields.
	tags nbt.StringSet

	// custom is the custom name, guarded by the boat lock.
	custom customName
	look   entityLook

	// carriedPassengers are nested rider compounds of unmodelled kinds,
	// written back under Passengers so they are not lost.
	carriedPassengers []nbt.Tag
}

// tie ties the hull's lead to the body id, which a save names by ref.
func (it *boatEntity) tie(id int32, ref leashRef) {
	it.holder, it.leash, it.leashWait = id, ref, 0
}

// untie takes the hull's lead off, a waiting one too, and returns the holder
// it had, 0 for none.
func (it *boatEntity) untie() int32 {
	holder := it.holder
	it.holder, it.leash, it.leashWait = 0, leashRef{}, 0
	return holder
}

// boatPassenger is one body aboard: a player by its connection, a mob by
// its entity. Exactly one of the two is set.
type boatPassenger struct {
	c   *client
	ent *mobEntity
}

// boatShown is the metadata a watcher was last told of a boat: the paddles,
// the rocking of a blow and the splash timer.
type boatShown struct {
	leftPaddle, rightPaddle bool
	hurtTime, hurtDir       int32
	hurtDamage              float64
	bubbleTime              int32
	// flags is the shared flags byte: glowing and on fire.
	flags  uint8
	silent bool
	frozen int32
}

func boatShownOf(it *boatEntity) boatShown {
	b := it.boat
	return boatShown{
		leftPaddle:  b.LeftPaddle,
		rightPaddle: b.RightPaddle,
		hurtTime:    b.HurtTime,
		hurtDir:     b.HurtDir,
		hurtDamage:  b.HurtDamage,
		bubbleTime:  int32(b.BubbleTime),
		flags:       it.look.flags(b.Burning),
		silent:      it.look.silent,
		frozen:      int32(b.TicksFrozen),
	}
}

// boatTracker tracks the boats; a watcher's own state is the metadata it was
// last told.
var boatTracker = trackedKind[*boatEntity, boatShown]{
	invalidatePiston: func(dim *dimension) { dim.pistons.boats.built = false },
	pistonBit:        pistonBoats,

	rangeChunks: entity.BoatTrackingRange,
	passengerRange: func(it *boatEntity) int {
		r := 0
		for _, p := range it.passengers {
			if p.c != nil {
				r = max(r, entity.PlayerTrackingRange)
			} else if p.ent != nil {
				r = max(r, mobStackRange(p.ent.mob))
			}
		}
		return r
	},
	interval: entity.BoatUpdateInterval,
	lock:     func(s *Server) *sync.Mutex { return &s.boatsMu },
	list:     func(s *Server) *[]*boatEntity { return &s.boats },
	chunks:   func(s *Server) *entity.ChunkIndex[*boatEntity] { return &s.boatChunks },
	filing:   func(it *boatEntity) *entity.Filing { return &it.boat.Filing },
	uuid:     func(it *boatEntity) uuid.UUID { return it.uuid },
	track:    func(it *boatEntity) *bodyTrack[boatShown] { return &it.bodyTrack },
	world:    func(it *boatEntity) *world.World { return it.w },
	id:       func(it *boatEntity) int32 { return it.boat.ID },
	dead:     func(it *boatEntity) bool { return it.boat.Dead },
	pose: func(it *boatEntity) bodyPose {
		b := it.boat
		return bodyPose{x: b.X, y: b.Y, z: b.Z, yaw: b.Yaw, onGround: b.OnGround}
	},
	portalDimensions: func(*boatEntity) (float64, float64) { return 2 * entity.BoatHalfWidth, entity.BoatHeight },
	portalCentered:   true,
	pairing:          boatPairing,
	own:              boatShownOf,
}

// driver is the first player aboard, the one vanilla's getControllingPassenger
// hands the boat to; nil for an all-mob boat, which nobody drives.
func (it *boatEntity) driver() *client {
	for _, p := range it.passengers {
		if p.c != nil {
			return p.c
		}
	}
	return nil
}

// seatAt is where the passenger at index of count aboard sits a hull
// standing at the place and yaw given, and what a mob among them adds its
// own offset by.
func (it *boatEntity) seatAt(count, index int, animal bool, x, y, z float64, yaw float32) (float64, float64, float64) {
	return entity.BoatSeat(it.kind, count, index, animal, x, y, z, yaw)
}

// seatOf is seatAt for a hull standing where the server keeps it.
func (it *boatEntity) seatOf(count, index int, animal bool) (float64, float64, float64) {
	return it.seatAt(count, index, animal, it.boat.X, it.boat.Y, it.boat.Z, it.boat.Yaw)
}

func (it *boatEntity) playerSeatAt(count, index int, x, y, z float64, yaw float32) (float64, float64, float64) {
	return entity.BoatPlayerSeat(it.kind, count, index, x, y, z, yaw)
}

func (it *boatEntity) playerSeatOf(count, index int) (float64, float64, float64) {
	return it.playerSeatAt(count, index, it.boat.X, it.boat.Y, it.boat.Z, it.boat.Yaw)
}

// playersAboard is the boat's player passengers, read under the boat lock.
func (it *boatEntity) playersAboard() []*client {
	var out []*client
	for _, p := range it.passengers {
		if p.c != nil {
			out = append(out, p.c)
		}
	}
	return out
}

// passengerIDs is everyone aboard as the Set Passengers packet names them,
// players and mobs in mounting order. The caller holds the boat lock.
func (it *boatEntity) passengerIDs() []int32 {
	ids := make([]int32, len(it.passengers))
	for i, p := range it.passengers {
		if p.c != nil {
			ids[i] = p.c.player.EntityID
		} else {
			ids[i] = p.ent.mob.ID
		}
	}
	return ids
}

// boatPad is a lily pad cell a hull floated into.
type boatPad struct {
	w       *world.World
	x, y, z int32
}

// tickBoats runs one game tick of every boat: where everyone stands is read
// first, with nothing held; the floats, the boardings and the movements run
// under the boat lock alone; the seats a boarding or a death leaves a mob
// in are written under the mob lock alone; the sends and the breaks run
// after the locks are down. A driven boat's own client runs it, so the
// driver is sent no movements — only the metadata it has not predicted.
func (s *Server) tickBoats() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	// The riders whose files carried them over: seated on the hull their
	// file stands back up, once their own join has finished. It runs even
	// with no hull standing, since the file's copy is what is raised here.
	// The mail a join sends is the loading screen's, and a pairing it drops
	// is a pairing nobody re-sends.
	for _, c := range clients {
		if !c.clientLoaded() {
			continue // still behind the loading screen; a later tick takes it
		}
		if saved := c.vehicleOnJoin.Load(); saved == nil || !c.savedVehicleChunkSent(*saved) {
			continue
		}
		// Under saveMu, so a save sees the file's copy or the seat, never
		// the gap between; a departure's save that took the copy leaves
		// nothing to raise.
		c.saveMu.Lock()
		if saved := c.vehicleOnJoin.Swap(nil); saved != nil {
			s.remountSavedVehicle(c, c.dimension(), *saved)
		}
		c.saveMu.Unlock()
	}

	gate := s.newEntityGate()
	var held []*boatEntity
	defer func() { boatTracker.dormantPass(s, held) }()
	// An empty list costs no more than the look at it; the dead a serving
	// still owes are boats too.
	s.lockTick(&s.boatsMu)
	if len(s.boats) == 0 {
		s.unlockTick(&s.boatsMu)
		return
	}
	// A frozen world steps only the hulls a player rides; a waiting lead
	// is the hull's own tick.
	// vanilla: TickRateManager.isEntityFrozen, Leashable.tickLeash
	runs := s.tickRate.Running()
	var boatWorlds []*world.World
	var waitHulls []*boatEntity
	var leashWaits []leashWait
	for _, it := range s.boats {
		if it.boat.Dead {
			continue
		}
		if it.holder == 0 && it.leash.set() && (runs || it.driver() != nil) {
			waitHulls = append(waitHulls, it)
			leashWaits = append(leashWaits, leashWait{w: it.w, ref: it.leash})
		}
		if !slices.Contains(boatWorlds, it.w) {
			boatWorlds = append(boatWorlds, it.w)
		}
	}
	s.unlockTick(&s.boatsMu)
	if len(leashWaits) > 0 {
		s.restoreBoatLeashes(waitHulls, leashWaits)
	}

	tick, pct := s.trackingTick(), s.entityBroadcastRange.Load()
	watchers := make(map[*world.World][]trackingWatcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		watchers[c.theWorld()] = append(watchers[c.theWorld()], newTrackingWatcher(c, tick, pct))
	}
	near := nearbyWorlds(watchers, func(w trackingWatcher) trackingWatcher { return w })
	defer releaseNearbyWorlds(near)

	// The leash holders each boat world is ticked against, read before the
	// boat lock: every player, the SameLevel flag marking the one whose
	// world this is — a holder that crossed away keeps the lead — and the
	// world's knots.
	leashHolders := make(map[*world.World]map[int32]entity.LeashHolder, len(boatWorlds))
	for _, w := range boatWorlds {
		leashHolders[w] = s.boatLeashHolders(clients, w)
	}

	// The mobs a hull's push might take and the pushable bodies its own move
	// stops at, read in one walk of each boat world's herd under the mob
	// lock alone, the answers frozen for this tick. It runs before the boat
	// lock, whose step may not take the mob one.
	teams := s.pushTeams()
	bodyIdx := s.boatBodies(clients, boatWorlds, teams)

	fx := boatEffects{removals: make(map[*world.World][]int32)}
	var padBuf []entity.LilyPadCell
	var flakeBuf []int32

	// The mobs this tick has already seated, shared by every hull's run:
	// one body cannot take two seats in the same tick.
	var boarded []*mobEntity

	// served takes a boat out of the world: the break's drop read, every
	// passenger stepped off — a player at the dismount spot beyond the
	// boat lock, a mob planned for the mob lock's phase — and the emptied
	// seat told to everyone.
	var unseat func(it *boatEntity)
	served := func(it *boatEntity) {
		boatTracker.unfile(it)
		fx.removals[it.w] = append(fx.removals[it.w], it.boat.ID)
		if !it.scoresSettled {
			s.entityDestroyed(it.uuid)
		}
		fx.breaks = append(fx.breaks, s.boatBreakOf(it))
		unseat(it)
	}
	// unseat steps every passenger off the hull and tells the emptied seat.
	unseat = func(it *boatEntity) {
		if len(it.passengers) > 0 {
			fx.news = append(fx.news, seatNews{w: it.w, vehicle: it.boat.ID, ids: nil})
		}
		for _, p := range it.passengers {
			ctx := rules.GameEventContext{}
			if p.c != nil {
				ctx = p.c.gameEventContext(0)
			} else {
				ctx.Mob = p.ent.uuid
			}
			fx.news[len(fx.news)-1].event(rules.GameEventEntityDismount, it.boat.X, it.boat.Y, it.boat.Z, ctx)
			if p.c != nil {
				pos := p.c.player.Position()
				var x, y, z float64
				if it.boat.Dead {
					// vanilla: LivingEntity.dismountVehicle
					d := p.c.dims(p.c.player.Appearance().Pose)
					x, y, z = entity.OffRemovedVehicle(it.w, pos.X, pos.Y, pos.Z, it.boat.Y, d.Width, d.Height)
				} else {
					x, y, z = rules.BoatDismountSpot(it.w, it.boat.X, it.boat.Y, it.boat.Z,
						pos.Yaw, rules.PlayerDismountPoses)
				}
				p.c.vehicle.Store(nil)
				fx.dismounts = append(fx.dismounts, boatPlayerSeat{c: p.c, x: x, y: y, z: z})
			} else {
				fx.offboards = append(fx.offboards, boatMobSeat{
					ent: p.ent, w: it.w, removed: it.boat.Dead,
					x: p.ent.mob.X, y: p.ent.mob.Y, z: p.ent.mob.Z,
					bx: it.boat.X, by: it.boat.Y, bz: it.boat.Z,
				})
			}
		}
		it.passengers = nil
	}

	s.lockTick(&s.boatsMu)
	for w, wb := range bodyIdx {
		wb.fileHulls(w)
	}
	alive := s.boats[:0]
	for _, it := range s.boats {
		if it.boat.Dead {
			// Broken since the last tick — a blow's end, a /kill, the
			// world's bottom — and gone with the drop its break leaves.
			served(it)
			continue
		}
		// A chunk that unloaded after this tick's sweep is the next tick's
		// to save. Stepping the hull now reads air for its ground and drops
		// it a tick's gravity before the save writes it.
		if pos := (world.ChunkPos{X: world.ChunkOf(it.boat.X), Z: world.ChunkOf(it.boat.Z)}); !it.w.ChunkLoaded(pos) || !gate.ticks(it.w, it.boat.X, it.boat.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		if !runs && it.driver() == nil {
			// The frozen world holds this hull where it stands, but its
			// tracking still settles.
			settleFrozenBoat(it, watchers[it.w], near[it.w], tick, &fx.out)
			alive = append(alive, it)
			continue
		}
		// An unridden hull floats and moves against the blocks and the
		// pushable bodies around it; a ridden one is its driver's to run.
		driver := it.driver()
		var bw entity.World = it.w
		if driver == nil {
			bw = boatBodyWorld(it.w, bodyIdx[it.w], it)
		}
		// The portal pass runs ahead of the float, as for items. A hull with
		// a player aboard counts but stays: the player's crossing carries it
		// (moveBoatWithRiders).
		state := it.w.BlockAt(rules.BlockOf(it.boat.X), rules.BlockOf(it.boat.Y), rules.BlockOf(it.boat.Z))
		s.notePortalTouch(nil, it.uuid, it.w, it.boat.X, it.boat.Y, it.boat.Z, state)
		if kind, due := it.portal.Step(state, rules.PortalTransitionTime, vehiclePortalCooldownTicks); due && driver == nil {
			boatTracker.unfile(it)
			fx.travels = append(fx.travels, boatPortalTravel{it: it, kind: kind})
			continue
		}
		sx, sy, sz := it.boat.X, it.boat.Y, it.boat.Z
		if driver != nil {
			// The driver moves the hull between ticks; sweep from this tick's baseline.
			sx, sy, sz = it.firstX, it.firstY, it.firstZ
		}
		from := entity.NewAABB(entity.BoatBoxAt(sx, sy, sz))
		it.boat.Step(bw, driver != nil)
		fx.movements.add(it.w, it.uuid, it.boat.Movement)
		known := player.Velocity{X: it.boat.VX, Z: it.boat.VZ}
		if driver != nil {
			known = driver.player.KnownMovement()
		}
		if it.boat.OnGround && !it.boat.Dead {
			if st, ok := groundStepOf(it.w, it.boat.X, it.boat.Y, it.boat.Z, entity.BoatHalfWidth, true); ok {
				fx.steps = append(fx.steps, st)
			}
		}
		if sp := it.boat.Splash; sp != nil {
			if driver != nil {
				sp.Ridden(known.X, known.Y, known.Z)
			}
			fx.splashes = append(fx.splashes, projectileSplash{w: it.w, x: sp.X, y: sp.Y, z: sp.Z, volume: sp.Volume, pitch: sp.Pitch})
		}
		if !it.look.silent {
			for _, ps := range it.boat.Paddles {
				fx.paddles = append(fx.paddles, boatPaddle{w: it.w, sound: ps})
			}
		}
		padBuf, flakeBuf = it.boat.InsideBlocks(it.w, sx, sy, sz, known.X, known.Z, padBuf, flakeBuf)
		for range it.boat.LavaHurts {
			fx.burns = append(fx.burns, burnSpot{it.w, it.boat.X, it.boat.Y, it.boat.Z})
		}
		fx.slides = appendSlides(fx.slides, honeySlide{it.w, it.boat.ID, it.boat.X, it.boat.Y, it.boat.Z}, it.boat.Slides)
		for range it.boat.Extinguishes {
			fx.fizzles = append(fx.fizzles, burnSpot{it.w, it.boat.X, it.boat.Y, it.boat.Z})
		}
		fx.addPutOut(it.w, it.boat)
		for _, c := range padBuf {
			fx.padCells = append(fx.padCells, boatPad{w: it.w, x: c.X, y: c.Y, z: c.Z})
		}
		for _, y := range flakeBuf {
			fx.flakes = append(fx.flakes, snowflakeSpot{it.w, it.boat.X, float64(y + 1), it.boat.Z})
		}
		if it.boat.EjectPassengers {
			it.boat.EjectPassengers = false
			unseat(it)
		}
		thrown := it.boat.Thrown
		it.boat.Thrown = entity.BoatThrowNone
		if it.boat.Dead {
			served(it)
			continue
		}
		if gx, gy, gz, ok := entity.EndGatewayInside(it.w, from, entity.NewAABB(it.boat.Box())); ok {
			fx.gateways = append(fx.gateways, gatewayTouch{
				w: it.w, x: gx, y: gy, z: gz, mu: &s.boatsMu,
				portal: &it.portal, cooldown: vehiclePortalCooldownTicks,
				move: func(to player.Position) { s.gatewayMoveBoat(it, to) },
			})
		}
		// The hull's own lead, one tick: a holder gone or run out of reach
		// drops it, a stretch within the band pulls the hull. The drop and
		// its break are told after the locks.
		if it.holder != 0 {
			holder, found := leashHolders[it.w][it.holder]
			if drop := boatLeashStep(it, holder, found); drop != nil {
				fx.leashDrops = append(fx.leashDrops, *drop)
			}
		}
		// The tick's own start, the base the driver's reports this tick are
		// checked against (see firstX).
		it.firstX, it.firstY, it.firstZ = it.boat.X, it.boat.Y, it.boat.Z

		// The mob passengers ride where the boat holds them.
		for i, p := range it.passengers {
			if p.ent != nil {
				x, y, z := it.seatOf(len(it.passengers), i, entity.IsAnimal(p.ent.mob.Type))
				fx.carries = append(fx.carries, boatMobSeat{ent: p.ent, x: x, y: y, z: z})
			}
		}
		// The push onto the boat: a live mob standing where the hull
		// sweeps — the boat's box swollen two tenths sideways and a
		// hundredth narrower upright — is taken aboard while a seat is
		// free and the bow is not under water. A mob this tick's earlier
		// hulls already took is kept out of the rest. What the mob-side
		// write of a boarding is belongs to the mob lock's phase below.
		// A boat whose first passenger is a player only pushes them.
		// vanilla: AbstractBoat.tick's push, Entity.canAddPassenger
		capacity := entity.BoatCapacity(it.kind)
		playerFirst := len(it.passengers) > 0 && it.passengers[0].c != nil
		if !playerFirst && bodyIdx[it.w] != nil && len(bodyIdx[it.w].cands) > 0 && it.boat.CanTakePassenger(len(it.passengers), capacity) {
			took := 0
			wb := bodyIdx[it.w]
			pick := entity.NewAABB(it.boat.X-entity.BoatHalfWidth-0.2, it.boat.Y+0.01, it.boat.Z-entity.BoatHalfWidth-0.2,
				it.boat.X+entity.BoatHalfWidth+0.2, it.boat.Y+entity.BoatHeight-0.01, it.boat.Z+entity.BoatHalfWidth+0.2)
			wb.candGrid.Query(pick, func(i int32) {
				if len(it.passengers) >= capacity {
					return
				}
				cand := wb.cands[i]
				if slices.Contains(boarded, cand.ent) {
					return // this tick gave it another hull's seat
				}
				if !entity.BoatPushTouches(cand.x, cand.y, cand.z, cand.width, cand.height,
					it.boat.X, it.boat.Y, it.boat.Z) {
					return
				}
				it.passengers = append(it.passengers, &boatPassenger{ent: cand.ent})
				boarded = append(boarded, cand.ent)
				took++
				x, y, z := it.seatOf(len(it.passengers), len(it.passengers)-1, entity.IsAnimal(cand.ent.mob.Type))
				fx.boards = append(fx.boards, boatMobSeat{ent: cand.ent, boat: it, w: it.w, x: x, y: y, z: z, bx: it.boat.X, by: it.boat.Y, bz: it.boat.Z})
			})
			if took > 0 {
				fx.riders = append(fx.riders, it.playersAboard()...)
				fx.news = append(fx.news, seatNews{
					w: it.w, vehicle: it.boat.ID, ids: it.passengerIDs(),
				})
			}
		}
		fx.push(it, bodyIdx[it.w], teams)

		body := boatTracker.body(it)
		// Taken before the schedule asks, so the flag never lingers.
		bounced := it.boat.TakeBounce()
		if bounced {
			fx.bounces = append(fx.bounces, bodyBounce{w: it.w, source: it.uuid, x: it.boat.X, y: it.boat.Y, z: it.boat.Z, halfWidth: entity.BoatHalfWidth})
		}
		mt := body.schedule(bounced)
		now := boatShownOf(it)
		still, prevTick := body.stillSince(tick)
		for _, i := range body.watchers(near[it.w]) {
			wh := watchers[it.w][i]
			c := wh.c
			if c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			// Read before the settle, so a watcher paired this tick is
			// measured against a zero record: it is sent every field off
			// zero, the shake direction's default among them, and its told
			// position is zeroed, so its next movement is a Teleport Entity.
			var was toldBody[boatShown]
			if p, ok := it.tracked.get(c); ok {
				was = *p
			}
			rec, seen := body.settleOrTold(wh, still, prevTick, &fx.out)
			if !seen {
				continue
			}
			// The throw is told the tick it lands, ahead of the metadata
			// that clears the spell. vanilla: AbstractBoat.tickBubbleColumn
			switch thrown {
			case entity.BoatThrowSink:
				fx.metas.add(c, outbound{id: protocol.IDPlayEntityEvent, data: entityEventPayload(it.boat.ID, protocol.EntityStatusBoatSink)})
			case entity.BoatThrowLaunch:
				fx.metas.add(c, outbound{id: protocol.IDPlayEntityEvent, data: entityEventPayload(it.boat.ID, protocol.EntityStatusBoatLaunch)})
			}
			// The metadata a tick spent goes out the tick it was spent in,
			// whatever the movement schedule says.
			if was.own != now {
				fx.metas.add(c, outbound{id: protocol.IDPlayEntityMetadata, data: boatDeltaMetadata(it.boat.ID, was.own, now)})
				was.own = now
				*rec = was
			}
			// The driver's own client runs its boat, so it is told only a
			// teleport, and always an absolute one: its told position is not
			// advanced by the boat's movements, so a teleport back onto it
			// would otherwise count as no change.
			// vanilla: ServerEntity.sendChanges
			if !mt.gate || c == driver && !mt.teleported {
				continue
			}
			tell := mt
			tell.resync = tell.resync || c == driver
			body.moveTold(rec, c, tell, &fx.out)
		}
		alive = append(alive, it)
	}
	s.boats = alive
	s.unlockTick(&s.boatsMu)

	fx.apply(s)
}

// boatLeashHolders reads the leash holders a world's boats are ticked
// against: every player on the server, with the SameLevel flag for this
// world, the world's knots and the mobs holding one of its boats. Read before the boat lock.
func (s *Server) boatLeashHolders(clients []*client, w *world.World) map[int32]entity.LeashHolder {
	holders := make(map[int32]entity.LeashHolder, len(clients))
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		h := playerLeashHolder(c)
		h.SameLevel = c.theWorld() == w
		holders[c.player.EntityID] = h
	}
	s.knotsMu.Lock()
	for _, k := range s.knots {
		if k.w == w {
			h := knotHolderSnapshot(k)
			h.SameLevel = true
			holders[k.id] = h
		}
	}
	s.knotsMu.Unlock()
	// A mob holding a boat's lead, looked up after the boat lock is released.
	var mobIDs []int32
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w == w && it.holder != 0 {
			if _, ok := holders[it.holder]; !ok {
				mobIDs = append(mobIDs, it.holder)
			}
		}
	}
	s.boatsMu.Unlock()
	if len(mobIDs) > 0 {
		s.mobsMu.Lock()
		for _, id := range mobIDs {
			if ent := s.mobByEntityIDLocked(w, id); ent != nil {
				m := ent.mob
				holders[id] = entity.LeashHolder{
					EntityID: id, X: m.X, Y: m.Y, Z: m.Z,
					Width: m.BbWidth(), Height: m.BbHeight(),
					Present: m.Alive(), SameLevel: true,
				}
			}
		}
		s.mobsMu.Unlock()
	}
	return holders
}

// leashLetGo tells a body's lead gone from holder without laying the item,
// and takes down a knot it leaves holding nothing. Called with no lock held.
// vanilla: Leashable.removeLeash
func (s *Server) leashLetGo(w *world.World, id, holder int32) {
	s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, 0))
	if knot := s.knotByEntityID(w, holder); knot != nil {
		s.discardEmptiedKnot(knot)
	}
}

// restoreBoatLeashes re-ties each waiting hull to the holder its save names:
// the knot on the cell, stood up there if none stands, or a player of the
// hull's world by UUID, or a mob of that world by UUID. A hull whose holder
// is not there waits, and lays its lead once the grace runs out. Called
// with no lock held.
// vanilla: Leashable.restoreLeashFromSave
func (s *Server) restoreBoatLeashes(hulls []*boatEntity, waits []leashWait) {
	knots := s.standLeashHolders(waits)
	var tied, dropped []leashTell
	s.boatsMu.Lock()
	for i, wt := range waits {
		it := hulls[i]
		if it.boat.Dead || it.holder != 0 || it.leash != wt.ref {
			continue // untied, re-tied or gone since the look
		}
		knots[i] = nil
		t := leashTell{w: it.w, id: it.boat.ID, holder: wt.holder, x: it.boat.X, y: it.boat.Y, z: it.boat.Z}
		if wt.holder != 0 {
			it.holder = wt.holder
			tied = append(tied, t)
			continue
		}
		// vanilla checks the body's tickCount > 100; a wait starts at its
		// load.
		if it.leashWait++; it.leashWait > entity.LeashRestoreTicks {
			it.untie()
			dropped = append(dropped, t)
		}
	}
	s.boatsMu.Unlock()
	s.tellLeashRestores(knots, tied, dropped)
}

// boatLeashStep works one tick of a tied hull's lead, vanilla's tickLeash
// for a Leashable boat: a holder gone drops the lead, one run out of reach
// snaps it — its break heard at the holder, its item laid at the hull — and
// a stretch within the elastic band pulls the hull. The caller holds the
// boat lock; the drop and the sound are the tick's, told after the locks.
// vanilla: Leashable.tickLeash
func boatLeashStep(it *boatEntity, holder entity.LeashHolder, found bool) *mobLeashDrop {
	drop, broke, _ := entity.LeashTickBody(it.boat.X, it.boat.Y, it.boat.Z,
		2*entity.BoatHalfWidth, entity.BoatHeight, it.boat.Yaw, entity.LeashElasticDistance, entity.LeashSnapDistance,
		&it.boat.VX, &it.boat.VY, &it.boat.VZ, holder, found)
	if !drop {
		return nil
	}
	id := it.untie()
	md := &mobLeashDrop{w: it.w, mobID: it.boat.ID, holder: id,
		x: it.boat.X, y: it.boat.Y, z: it.boat.Z}
	if broke {
		md.broke = true
		md.hx, md.hy, md.hz = holder.X, holder.Y, holder.Z
	}
	return md
}

// hullsByWorld reads every live hull's box, per world, under the boat lock
// alone, before the other entity passes take their own locks.
func (s *Server) hullsByWorld() map[*world.World][]entity.AABB {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	hulls := make(map[*world.World][]entity.AABB, len(s.allDims()))
	for _, it := range s.boats {
		if it.boat.Dead {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
		hulls[it.w] = append(hulls[it.w], entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ))
	}
	return hulls
}

// mobCandidate is one live mob a hull's boarding sweep might take: its place
// and box, frozen under the mob lock for the tick.
type mobCandidate struct {
	ent           *mobEntity
	x, y, z       float64
	width, height float64
}

// boatWorldBodies is one world's frozen boat inputs: the pushable bodies'
// boxes with the mob or client that owns each (the other nil), grids over
// the boxes and over the boarding candidates' boxes. Its storage is reused
// from tick to tick.
type boatWorldBodies struct {
	boxes   []entity.AABB
	grid    entity.BoxGrid
	mobs    []*mobEntity
	clients []*client
	// pushers is each box's push inputs.
	pushers  []boatPusher
	cands    []mobCandidate
	candGrid entity.BoxGrid
	// reach is each live hull's reach box and reachGrid indexes it: a mob
	// outside every one is nobody this tick's hulls stop at or take aboard.
	// hulls is the hull each reach box was filed for, and hullGrid files
	// each by its centre at the top of the hull loop (fileHulls).
	reach     []entity.AABB
	reachGrid entity.BoxGrid
	hulls     []*boatEntity
	hullGrid  entity.BoxGrid
	hullSlack float64
	// mobIdx and clientIdx are built on the first hull that carries a
	// passenger, from mobs and clients.
	mobIdx    map[*mobEntity]int32
	clientIdx map[*client]int32
	indexed   bool
	// bodies and skipBuf back the world boatBodyWorld returns, so a hull's
	// tick boxes nothing; one hull's step finishes before the next reuses them.
	bodies  entity.Bodies
	skipBuf [4]int32
}

func (wb *boatWorldBodies) reset() {
	wb.boxes, wb.mobs, wb.clients, wb.pushers, wb.cands = wb.boxes[:0], wb.mobs[:0], wb.clients[:0], wb.pushers[:0], wb.cands[:0]
	wb.grid.Reset()
	wb.candGrid.Reset()
	wb.reach = wb.reach[:0]
	clear(wb.hulls)
	wb.hulls = wb.hulls[:0]
	wb.reachGrid.Reset()
	wb.hullGrid.Reset()
	wb.hullSlack = 0
	wb.indexed = false
}

// boatReachMargin is how far past a hull's box, besides its speed, a body
// can matter to the hull's move or pick-up this tick; a deliberate bound, not
// vanilla's.
const boatReachMargin = 3.0

// inReach reports whether the box of a body at (x, y, z) meets some hull's
// reach box.
func (wb *boatWorldBodies) inReach(x, y, z, width, height float64) bool {
	half := width / 2
	box := entity.NewAABB(x-half, y, z-half, x+half, y+height, z+half)
	found := false
	wb.reachGrid.Query(box, func(i int32) {
		if !found && box.Crosses(wb.reach[i]) {
			found = true
		}
	})
	return found
}

// add files one pushable body; owner is a mob or a client, the other nil.
func (wb *boatWorldBodies) add(box entity.AABB, m *mobEntity, c *client, p boatPusher) {
	wb.grid.Add(int32(len(wb.boxes)), box)
	wb.boxes = append(wb.boxes, box)
	wb.mobs = append(wb.mobs, m)
	wb.clients = append(wb.clients, c)
	wb.pushers = append(wb.pushers, p)
}

// index builds the owner lookups once per tick.
func (wb *boatWorldBodies) index() {
	if wb.indexed {
		return
	}
	wb.indexed = true
	clear(wb.mobIdx)
	clear(wb.clientIdx)
	if wb.mobIdx == nil {
		wb.mobIdx = make(map[*mobEntity]int32)
		wb.clientIdx = make(map[*client]int32)
	}
	for i := range wb.boxes {
		if m := wb.mobs[i]; m != nil {
			wb.mobIdx[m] = int32(i)
		} else {
			wb.clientIdx[wb.clients[i]] = int32(i)
		}
	}
}

// boatBodies reads, in one walk of each boat world's herd under the mob
// lock alone, the pushable bodies an unridden hull's own move stops at — the
// isPushable bodies, so a dead, gone or climbing mob is nobody a hull stops
// at — and the live mobs a hull's boarding sweep might take, a body already
// aboard some boat, a dead one, or a kind the cannot_be_pushed_onto_boats
// tag keeps ashore being nobody it picks up. The tick's client snapshot
// supplies the players' push bodies, outside the lock. It runs before the
// boat lock, whose step may not take the mob one. The result is valid until
// the next call.
func (s *Server) boatBodies(clients []*client, worlds []*world.World, teams pushTeams) map[*world.World]*boatWorldBodies {
	if s.boatScratch == nil {
		s.boatScratch = make(map[*world.World]*boatWorldBodies)
	}
	out := make(map[*world.World]*boatWorldBodies, len(worlds))
	for _, w := range worlds {
		wb := s.boatScratch[w]
		if wb == nil {
			wb = &boatWorldBodies{hullGrid: entity.BoxGrid{Cell: boatHullCell}}
			s.boatScratch[w] = wb
		}
		wb.reset()
		out[w] = wb
	}
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if wb := out[it.w]; wb != nil && !it.boat.Dead {
			b := it.boat
			speed := math.Max(math.Abs(b.VX), math.Max(math.Abs(b.VY), math.Abs(b.VZ)))
			m := boatReachMargin + speed
			box := entity.NewAABB(b.X-m, b.Y-m, b.Z-m, b.X+m, b.Y+entity.BoatHeight+m, b.Z+m)
			wb.reachGrid.Add(int32(len(wb.reach)), box)
			wb.reach = append(wb.reach, box)
			wb.hulls = append(wb.hulls, it)
		}
	}
	s.boatsMu.Unlock()
	for _, c := range clients {
		if c.player == nil || c.player.GameMode() == protocol.GameModeSpectator ||
			c.player.Vitals().Dead {
			continue
		}
		wb := out[c.theWorld()]
		if wb == nil {
			continue
		}
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		half := dims.Width / 2
		team := teams.names[c.player.Name]
		wb.add(entity.NewAABB(pos.X-half, pos.Y, pos.Z-half,
			pos.X+half, pos.Y+dims.Height, pos.Z+half), nil, c,
			boatPusher{x: pos.X, minY: pos.Y, z: pos.Z, team: team, rule: teams.rules[team], pushes: s.tickRate.Running()})
	}
	s.mobsMu.Lock()
	for _, w := range worlds {
		wb := out[w]
		h := s.herdOf(w)
		h.mobsNearBoxes(wb.reach, func(m *entity.Mob) bool {
			if !wb.inReach(m.X, m.Y, m.Z, m.BbWidth(), m.BbHeight()) {
				return true
			}
			pushable := m.Pushable()
			boardable := !m.Dead && entity.BoatBoardable(m)
			if !pushable && !boardable {
				return true
			}
			ent := h.record(m)
			if ent == nil {
				return true
			}
			width, height := m.BbWidth(), m.BbHeight()
			half := width / 2
			if pushable {
				minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
				wb.add(entity.NewAABB(minX, minY, minZ, maxX, maxY, maxZ), ent, nil, boatPusher{
					x: m.X, minY: minY, z: m.Z, team: m.Team, rule: m.TeamCollision,
					pushes: m.PushesCarts(), takes: !ent.seated() && len(m.Passengers) == 0,
				})
			}
			if !boardable || ent.vehicle != nil || ent.cart != nil {
				return true
			}
			wb.candGrid.Add(int32(len(wb.cands)), entity.NewAABB(m.X-half, m.Y, m.Z-half, m.X+half, m.Y+height, m.Z+half))
			wb.cands = append(wb.cands, mobCandidate{ent: ent, x: m.X, y: m.Y, z: m.Z, width: width, height: height})
			return true
		})
	}
	s.mobsMu.Unlock()
	return out
}

// boatBodyWorld pairs a hull's world with the pushable bodies its own move
// stops at, its own passengers left out: a body the seat holds is the
// hull's to carry, not a wall (isPassengerOfSameVehicle). A world with
// none — or only the hull's own passengers — keeps its plain self.
func boatBodyWorld(w *world.World, wb *boatWorldBodies, it *boatEntity) entity.World {
	if wb == nil || len(wb.boxes) == 0 {
		return w
	}
	skip := wb.skipBuf[:0]
	if len(it.passengers) > 0 {
		wb.index()
	}
	for _, p := range it.passengers {
		if i, ok := wb.mobIdx[p.ent]; p.ent != nil && ok {
			skip = append(skip, i)
		}
		if i, ok := wb.clientIdx[p.c]; p.c != nil && ok {
			skip = append(skip, i)
		}
	}
	wb.bodies = entity.Bodies{World: w, Boxes: wb.boxes, Grid: &wb.grid, Skip: skip}
	return &wb.bodies
}

// boatBreak is what a broken boat leaves: its own item — the one whose name
// is the boat's kind, laid only where a destroy reached and the
// entity_drops rule allows — and the cargo a chest boat carried, which
// spills for every removal that destroys the hull.
type boatBreak struct {
	w        *world.World
	id       int32
	name     string
	x, y, z  float64
	withItem bool
	itemName *protocol.Text
	cargo    []protocol.ItemStack
	// leashHolder is the body that held the hull's lead when it broke, 0
	// for an unleash one; its lead is let go whatever the loot rule says.
	leashHolder int32
}

// boatBreakOf reads what a dead boat's break leaves, under the boat lock:
// the stacks lifted out, for the caller to lay after the locks are down.
// NoLoot suppresses the boat item alone; the cargo rides the break unless
// the hull was left carrying it away.
func (s *Server) boatBreakOf(it *boatEntity) boatBreak {
	b := boatBreak{
		w:           it.w,
		id:          it.boat.ID,
		name:        it.kind.Name(),
		x:           it.boat.X,
		y:           it.boat.Y,
		z:           it.boat.Z,
		withItem:    !it.boat.NoLoot,
		itemName:    customNameText(it.custom.tag()),
		leashHolder: it.untie(),
	}
	s.unpackBoatLoot(it, lootOpener{})
	b.cargo = it.cargo
	it.cargo = nil
	return b
}

// unpackBoatLoot rolls a chest boat's pending loot table into its empty
// cargo slots and clears the tag, returning the table it rolled ("" for
// none). A table this server holds no data for stays pending, so the tag
// survives a save. The caller holds the boat lock.
// vanilla: ContainerEntity.unpackChestVehicleLootTable
func (s *Server) unpackBoatLoot(it *boatEntity, opener lootOpener) string {
	table := it.lootTable
	if table == "" || it.cargo == nil {
		return ""
	}
	if !s.fillContainerLoot(it.w, rules.BlockOf(it.boat.X), rules.BlockOf(it.boat.Y), rules.BlockOf(it.boat.Z), table, it.lootSeed, it.cargo, opener) {
		return ""
	}
	it.cargoVersion++
	it.lootTable, it.lootSeed = "", 0
	return table
}

// syncBoatViewers tells every player viewing a boat which slots changed.
// vanilla: AbstractContainerMenu.broadcastChanges
func (s *Server) syncBoatViewers(it *boatEntity) {
	s.syncVehicleViewers(func(w *openWindow) bool { return w.boat == it }, func() []protocol.ItemStack {
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		return slices.Clone(it.cargo)
	})
}

// lootOpener is the player a container's pending table rolls for, read before
// the container's lock is taken; the zero value is nobody (a hopper's pull, a
// break, a comparator's read).
// vanilla: RandomizableContainer.unpackLootTable, ContainerEntity.unpackChestVehicleLootTable
type lootOpener struct {
	this rules.LootEntity
	luck float32
}

// lootOpener is the client as the player opening a container. Its saved
// compound, ride and pearls included, is read now: the roll runs under the
// container's lock, which ranks below the inventory lock the read takes.
func (c *client) lootOpener() lootOpener {
	this := c.LootEntity()
	tag := c.lootNBTRiding()()
	this.NBT = func() nbt.Compound { return tag }
	// The relations and live facts take locks that rank above the
	// container's, so they are read now too.
	this.Freeze()
	return lootOpener{this: this, luck: float32(c.luckAttribute())}
}

// fillContainerLoot rolls a pending table for a block container, minecart or
// chest boat at (x, y, z) for opener, its THIS and luck.
// vanilla: LootTable.fill
func (s *Server) fillContainerLoot(w *world.World, x, y, z int32, table string, seed int64, slots []protocol.ItemStack, opener lootOpener) bool {
	// The world answers the location checks the table makes around the
	// container's own place; a roll without it fails those.
	ctx := s.mapLootContext(w, x, y, z)
	ctx.BiomeAt = func(dx, dy, dz int32) string { return w.BiomeAt(x+dx, y+dy, z+dz) }
	ctx.This, ctx.Luck = opener.this, opener.luck
	ctx.Random = rules.SeededLootRandom(seed)
	return rules.FillLootReviewed(table, ctx, s.levelRng, slots, s.lootReview(w, table, x, y, z))
}

// lootFillFor is the world's container roll made for opener.
func (s *Server) lootFillFor(opener lootOpener) world.LootFill {
	return func(w *world.World, x, y, z int32, table string, seed int64, slots []protocol.ItemStack) bool {
		return s.fillContainerLoot(w, x, y, z, table, seed, slots, opener)
	}
}

// rollLoot is the roll a block entity hiding one item uses, made for opener
// with tool.
// vanilla: BrushableBlockEntity.unpackLootTable, VaultBlockEntity.Server.resolveItemsToEject
func (s *Server) rollLoot(w *world.World, x, y, z int32, table string, seed int64, opener lootOpener, tool protocol.ItemStack) ([]protocol.ItemStack, bool) {
	return s.rollLootWith(&rules.LootContext{Level: s.lootLevelAtBlock(w, x, y, z), Tool: rules.ToolOf(tool), This: opener.this, Luck: opener.luck}, table, seed)
}

// rollLootWith rolls table against ctx, returning false for an unknown table.
// vanilla: LootTable.getRandomItems
func (s *Server) rollLootWith(ctx *rules.LootContext, table string, seed int64) ([]protocol.ItemStack, bool) {
	data, known := registry.FindLootTable(table)
	if !known {
		return nil, false
	}
	if seed != 0 {
		ctx.Random = rules.SeededLootRandom(seed)
	}
	return rules.RollLootTable(table, data, ctx, s.levelRng), true
}

// breakBoat lays what a broken boat leaves where it broke — the cargo a
// chest boat carried spilled as a broken container's is, a lead the hull
// still carried let go, then the boat's own item at its feet. The
// entity_drops rule gates the item alone, the cargo spilling for any
// removal that destroys.
// vanilla: VehicleEntity.destroy, AbstractChestBoat.remove
func (s *Server) breakBoat(br boatBreak) {
	s.spillContainer(br.w, int32(math.Floor(br.x)), int32(math.Floor(br.y)), int32(math.Floor(br.z)), br.cargo)
	if br.leashHolder != 0 {
		s.dropLeadItem(br.w, br.x, br.y, br.z)
		s.broadcastWorld(br.w, protocol.IDPlaySetEntityLink, leashLinkPayload(br.id, 0))
		if knot := s.knotByEntityID(br.w, br.leashHolder); knot != nil {
			s.discardEmptiedKnot(knot)
		}
	}
	if br.withItem && br.w.BoolRule(world.RuleEntityDrops) {
		if id, ok := registry.ItemID(br.name); ok {
			s.spawnItemAtFeet(br.w, protocol.ItemStack{ItemID: id, Count: 1, CustomName: br.itemName}, br.x, br.y, br.z)
		}
	}
}

// spawnBoat stands a boat of the kind named in the world at the spot given,
// facing the yaw the placer stood at, and shows it to everyone in range.
// The entity kind is the item's own name: vanilla names every boat and
// raft's entity after its item, minecraft:oak_boat and its kin.
func (s *Server) spawnBoat(w *world.World, name string, x, y, z float64, yaw float32, custom customName) *boatEntity {
	if _, ok := registry.EntityTypeID(name); !ok {
		slog.Error("the entity registry has no boat for the item", "item", name)
		return nil
	}
	it := s.newBoat(w, name, x, y, z, yaw)
	it.custom = custom
	s.registerBoat(it)
	s.bodySpawned(it.uuid, plugin.SpawnDefault)
	return it
}

// newBoatFromItem builds a boat with its stack components applied.
// vanilla: BoatItem.getBoat, BoatDispenseItemBehavior.execute
func (s *Server) newBoatFromItem(w *world.World, name string, x, y, z float64, yaw float32, stack protocol.ItemStack) *boatEntity {
	it := s.newBoat(w, name, x, y, z, yaw)
	if tag := itemEntityTag(boatTag(it).Entries(), stack, it.entityTypeID()); tag != nil {
		applyBoatTag(it, tag)
	}
	// The placer sets yaw after the stack's data loads. vanilla: BoatItem.use
	it.boat.Yaw = yaw
	return it
}

func (s *Server) newBoat(w *world.World, name string, x, y, z float64, yaw float32) *boatEntity {
	typ, _ := registry.EntityTypeByName(name)
	it := &boatEntity{
		boat: entity.NewBoat(s.nextEntity(), x, y, z, yaw),
		uuid: uuid.New(),
		w:    w,
		kind: typ,
	}
	it.firstX, it.firstY, it.firstZ = x, y, z
	if entity.IsChestBoat(it.kind) {
		it.cargo = make([]protocol.ItemStack, entity.BoatCargoSlots)
	}
	return it
}

// summonBoat stands a /summon's boat of the kind named up at the position
// given: read through the same loader a saved hull's wake reads — the yaw
// and motion its tag named, the NoGravity flag, a chest boat's cargo — with
// the command's position the one it floats at. A load that comes to nothing
// and a tag whose UUID is already a live body's are the two refusals, the
// notifier the caller's.
// vanilla: SummonCommand.createEntity
func (s *Server) summonBoat(w *world.World, kind string, x, y, z float64, tag nbt.Compound, reason plugin.SpawnReason) (*boatEntity, error) {
	compound := summonTag(tag, kind, x, y, z)
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	it := boatOfTag(s, w, compound)
	w.Release(pos)
	if it == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(it.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	it.boat.SetPos(x, y, z)
	it.spawnReason = spawnReasonCode(reason)
	if err := s.registerSummonedVehicle(w, compound, vehicleSeat{boat: it}, reason); err != nil {
		return nil, err
	}
	return it, nil
}

// registerBoat puts a built boat into the world and shows it to every
// player within the type's tracking range.
func (s *Server) registerBoat(it *boatEntity) {
	boatTracker.register(s, it)
	s.boatsMu.Lock()
	s.minecartsMu.Lock()
	if s.cartCols != nil {
		s.cartCols.addBoat(it)
		if len(s.cartCols.boats) > 0 {
			s.cartColsEmptyWorld.Store(nil)
		}
	}
	s.minecartsMu.Unlock()
	s.boatsMu.Unlock()
}

// boatPairing is what shows a boat: the spawn, the metadata where it is not
// the fresh default, the lead's holder, and who rides it, all in one bundle.
// vanilla: ServerEntity.sendPairingData
func boatPairing(it *boatEntity) []outbound {
	packets := boatPairingPackets(it)
	if len(it.passengers) > 0 {
		packets = append(packets, boatPassengersPacket(it))
	}
	return bundle(packets)
}

// boatJoinPairing is what a join shows of a boat: boatPairing with the
// riders sent after the bundle, not in it.
func boatJoinPairing(it *boatEntity) []outbound {
	packets := bundle(boatPairingPackets(it))
	if len(it.passengers) > 0 {
		packets = append(packets, boatPassengersPacket(it))
	}
	return packets
}

func boatPairingPackets(it *boatEntity) []outbound {
	packets := pairing(outbound{id: protocol.IDPlaySpawnEntity, data: boatSpawnPayload(it)}, outbound{id: protocol.IDPlayEntityMetadata, data: boatSpawnMetadataPayload(it)})
	if it.holder != 0 {
		packets = append(packets, outbound{id: protocol.IDPlaySetEntityLink,
			data: leashLinkPayload(it.boat.ID, it.holder)})
	}
	return packets
}

func boatPassengersPacket(it *boatEntity) outbound {
	return outbound{id: protocol.IDPlaySetPassengers, data: setPassengersPayload(it.boat.ID, it.passengerIDs())}
}

// boatSpawnPayload builds the Spawn Entity packet that puts a boat in the
// world for a client: entity ID and UUID, the boat kind's own entity type,
// position, the motion it keeps and the yaw it faces. There is no
// type-specific data.
func boatSpawnPayload(it *boatEntity) []byte {
	b := it.boat
	return spawnEntityPayload(spawnEntity{
		id: b.ID, uuid: it.uuid, typ: it.entityTypeID(),
		x: b.X, y: b.Y, z: b.Z,
		vx: b.VX, vy: b.VY, vz: b.VZ,
		yaw: encodeAngle(b.Yaw),
	})
}

// entityType is the boat kind's name in the minecraft:entity_type
// registry, as the entity snapshot reads it.
func (it *boatEntity) entityType() string { return it.kind.Name() }

func (it *boatEntity) entityTypeID() int32 { return int32(it.kind) }

// boatSpawnMetadataPayload builds the Set Entity Metadata packet carrying
// what a boat is, at its spawn: the rocking of a body still recovering from
// a blow and the paddles a driver is turning, each left out at its fresh
// default — the pairing sends the non-default values alone.
func boatSpawnMetadataPayload(it *boatEntity) []byte {
	b := it.boat
	if b.HurtTime == 0 && b.HurtDir == protocol.BoatHurtDirDefault && b.HurtDamage == 0 &&
		!b.LeftPaddle && !b.RightPaddle && !b.NoGravity && b.BubbleTime == 0 && b.TicksFrozen == 0 && !b.Burning &&
		it.custom == (customName{}) && it.look.flags(false) == 0 && !it.look.silent {
		return nil
	}
	return boatMetadataFields(b, it.look.flags(b.Burning), it.look.flags(b.Burning) != 0, it.custom, it.look.silent, false, b.NoGravity, b.HurtTime != 0, b.HurtDir != protocol.BoatHurtDirDefault,
		b.HurtDamage != 0, b.LeftPaddle, b.RightPaddle, b.BubbleTime != 0, b.TicksFrozen != 0)
}

// boatFields is the synched value of a boat a /data write can change,
// defaults included.
func boatFields(it *boatEntity) metaFields {
	var m metaFields
	m.add(protocol.MetadataIndexEntityFlags, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(it.look.flags(it.boat.Burning)) })
	m.add(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(it.boat.NoGravity) })
	m.add(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt, func(b *protocol.Buffer) { b.PutVarInt(int32(it.boat.TicksFrozen)) })
	it.look.addSilentField(&m)
	it.custom.addFields(&m)
	return m
}

// boatDeltaMetadata builds the metadata update of what turned since was:
// the fields the client's record is behind on, a reset to the default among
// them — the rocking running out is news like the blow that started it.
func boatDeltaMetadata(id int32, was, now boatShown) []byte {
	if was == now {
		return nil
	}
	return boatMetadataFields(&entity.Boat{
		ID:         id,
		HurtTime:   now.hurtTime,
		HurtDir:    now.hurtDir,
		HurtDamage: now.hurtDamage,
		LeftPaddle: now.leftPaddle, RightPaddle: now.rightPaddle,
		BubbleTime: int(now.bubbleTime), TicksFrozen: int(now.frozen),
	}, now.flags, was.flags != now.flags, customName{}, now.silent, was.silent, false, was.hurtTime != now.hurtTime, was.hurtDir != now.hurtDir,
		was.hurtDamage != now.hurtDamage, was.leftPaddle != now.leftPaddle,
		was.rightPaddle != now.rightPaddle, was.bubbleTime != now.bubbleTime, was.frozen != now.frozen)
}

// boatMetadataFields lays the boat's metadata fields whose sends are asked
// for: the shared flags byte where asked for, a non-default custom name, the no-gravity flag a held-up hull
// carries, the vehicle's rocking — the ticks left, the direction count, the damage the blow laid, the
// multiplier the rocking runs at — the two paddles and the splash timer.
func boatMetadataFields(b *entity.Boat, flags uint8, tellFlags bool, custom customName, silent, wasSilent, noGravity, time, dir, damage, left, right, splash, frozen bool) []byte {
	var buf protocol.Buffer
	buf.PutVarInt(b.ID)
	if tellFlags {
		buf.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		buf.PutU8(flags)
	}
	entityLook{silent: silent}.putSilent(&buf, wasSilent)
	custom.putMetadata(&buf, customName{})
	if noGravity {
		buf.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		buf.PutBool(true)
	}
	if frozen {
		buf.PutMetadata(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt)
		buf.PutVarInt(int32(b.TicksFrozen))
	}
	if time {
		buf.PutMetadata(protocol.MetadataIndexVehicleShake, protocol.MetadataVarInt)
		buf.PutVarInt(b.HurtTime)
	}
	if dir {
		buf.PutMetadata(protocol.MetadataIndexVehicleShakeDirection, protocol.MetadataVarInt)
		buf.PutVarInt(b.HurtDir)
	}
	if damage {
		buf.PutMetadata(protocol.MetadataIndexVehicleShakeDamage, protocol.MetadataFloat)
		buf.PutF32(float32(b.HurtDamage))
	}
	if left {
		buf.PutMetadata(protocol.MetadataIndexBoatLeftPaddle, protocol.MetadataBoolean)
		buf.PutBool(b.LeftPaddle)
	}
	if right {
		buf.PutMetadata(protocol.MetadataIndexBoatRightPaddle, protocol.MetadataBoolean)
		buf.PutBool(b.RightPaddle)
	}
	if splash {
		buf.PutMetadata(protocol.MetadataIndexBoatBubbleTime, protocol.MetadataVarInt)
		buf.PutVarInt(int32(b.BubbleTime))
	}
	buf.EndMetadata()
	return buf.Bytes()
}

// setPassengersPayload builds the Set Passengers packet naming who rides a
// vehicle: the vehicle's ID, then the passengers' own.
func setPassengersPayload(vehicle int32, passengers []int32) []byte {
	var buf protocol.Buffer
	buf.PutVarInt(vehicle)
	buf.PutVarInt(int32(len(passengers)))
	for _, p := range passengers {
		buf.PutVarInt(p)
	}
	return buf.Bytes()
}

// handleUseBoat places a boat only if its final box fits after loading stack
// components; a rejected placement keeps the item.
// vanilla: BoatItem.use
func (c *client) handleUseBoat(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	name, boat := entity.HeldBoat(held)
	if !boat {
		return false
	}
	pos := c.player.Position()
	eyeY := pos.Y + c.dims(c.player.Appearance().Pose).EyeHeight
	w := c.theWorld()
	// An eye inside a pickable entity lays nothing.
	// vanilla: BoatItem.use's eye-in-entity check
	if c.srv.eyeInsidePickable(c, w, pos.X, eyeY, pos.Z) {
		return false
	}
	// vanilla: Item.getPlayerPOVHitResult
	reach := c.blockRange()
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	x, y, z, ok := rules.BoatSpot(w, pos.X, eyeY, pos.Z, dx, dy, dz, reach)
	if !ok {
		return false
	}
	it := c.srv.newBoatFromItem(w, name, x, y, z, pos.Yaw, held)
	// Entity data can replace Pos, so validate the loaded hull before spending
	// the item or registering it. Dispensers do not have this collision gate.
	// vanilla: BoatItem.use, BoatDispenseItemBehavior.execute
	b := it.boat
	if !rules.BoatFits(w, b.X, b.Y, b.Z) || c.srv.boatCrossesBody(w, b.X, b.Y, b.Z) {
		return false
	}
	// The item first, the boat after: a hand that cannot spend its item
	// lays nothing, and the count a test reads is spent before the spawn
	// the test reads next.
	if mode != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return false
		}
	}
	c.srv.registerBoat(it)
	c.srv.bodySpawned(it.uuid, plugin.SpawnDefault)
	c.gameEventAt(rules.GameEventEntityPlace, x, y, z, 0)
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	slog.Debug("boat laid", "kind", name, "x", x, "y", y, "z", z, "by", c.player.Name)
	return true
}

// eyeInsidePickable reports whether the point stands inside a pickable
// body's box — the players, the mobs, the items and the rest a boat item's
// use refuses to lay a boat over. layer, the player laying it, is not one
// of them. Each kind's list is read under its own lock alone.
// vanilla: BoatItem.use's EntitySelector.NO_SPECTATORS.and(Entity::isPickable)
func (s *Server) eyeInsidePickable(layer *client, w *world.World, x, y, z float64) bool {
	inside := func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
		return x >= minX && x <= maxX && y >= minY && y <= maxY && z >= minZ && z <= maxZ
	}

	s.mu.RLock()
	for _, other := range s.clients {
		if other == layer || other.player == nil || other.theWorld() != w ||
			other.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := other.player.Position()
		dims := other.dims(other.player.Appearance().Pose)
		half := dims.Width / 2
		if inside(pos.X-half, pos.Y, pos.Z-half, pos.X+half, pos.Y+dims.Height, pos.Z+half) {
			s.mu.RUnlock()
			return true
		}
	}
	s.mu.RUnlock()

	s.itemsMu.Lock()
	for _, it := range s.items {
		if !it.item.Dead && it.w == w && inside(it.item.BlastBox()) {
			s.itemsMu.Unlock()
			return true
		}
	}
	s.itemsMu.Unlock()

	s.arrowsMu.Lock()
	for _, it := range s.arrows {
		if !it.arrow.Dead && it.w == w && inside(it.arrow.BlastBox()) {
			s.arrowsMu.Unlock()
			return true
		}
	}
	s.arrowsMu.Unlock()

	s.arrowsMu.Lock()
	for _, it := range s.thrown {
		if !it.throwable.Dead && it.w == w && inside(it.throwable.BlastBox()) {
			s.arrowsMu.Unlock()
			return true
		}
	}
	s.arrowsMu.Unlock()

	s.orbsMu.Lock()
	for _, it := range s.orbs {
		if !it.orb.Dead && it.w == w && inside(it.orb.BlastBox()) {
			s.orbsMu.Unlock()
			return true
		}
	}
	s.orbsMu.Unlock()

	s.tntMu.Lock()
	for _, it := range s.tnts {
		if !it.tnt.Dead && it.w == w && inside(it.tnt.BlastBox()) {
			s.tntMu.Unlock()
			return true
		}
	}
	s.tntMu.Unlock()

	s.fallingMu.Lock()
	for _, it := range s.fallings {
		if !it.fb.Dead && it.w == w && inside(it.fb.BlastBox()) {
			s.fallingMu.Unlock()
			return true
		}
	}
	s.fallingMu.Unlock()

	s.boatsMu.Lock()
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == w && inside(it.boat.BlastBox()) {
			s.boatsMu.Unlock()
			return true
		}
	}
	s.boatsMu.Unlock()

	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(w).book {
		if !m.Dead && inside(m.BlastBox()) {
			return true
		}
	}
	return false
}

// boatCrossesBody reports whether a boat at the given position would cross
// a body, the entity half of use's noCollision that BoatFits's
// block check leaves out: an alive, non-spectator player or mob in the box
// refuses the laying — the layer's own body among them — and so does another
// hull. A body on a climbable is not pushable and does not count, except two:
// a flying player, whose onClimbable is off in flight, and a spider, whose
// own onClimbable is only its Climbing flag. The pickable kinds a boat passes
// through — the items, the arrows and the rest — do not count either.
// vanilla: Level.noCollision, AbstractBoat.canCollideWith, LivingEntity.isPushable
func (s *Server) boatCrossesBody(w *world.World, x, y, z float64) bool {
	crosses := func(bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ float64) bool {
		return entity.BoatSpotCrosses(x, y, z, bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ)
	}

	s.mu.RLock()
	for _, other := range s.clients {
		if other.player == nil || other.theWorld() != w ||
			other.player.GameMode() == protocol.GameModeSpectator || other.player.Vitals().Dead {
			continue
		}
		pos := other.player.Position()
		// A body on a climbable is not pushable, so it does not refuse the
		// boat — unless it is flying, which turns the player's own
		// onClimbable off. vanilla: Player.onClimbable, LivingEntity.isPushable
		if !other.flying.Load() && other.climbableAt(pos) {
			continue
		}
		dims := other.dims(other.player.Appearance().Pose)
		half := dims.Width / 2
		if crosses(pos.X-half, pos.Y, pos.Z-half, pos.X+half, pos.Y+dims.Height, pos.Z+half) {
			s.mu.RUnlock()
			return true
		}
	}
	s.mu.RUnlock()

	s.boatsMu.Lock()
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == w && crosses(it.boat.Box()) {
			s.boatsMu.Unlock()
			return true
		}
	}
	s.boatsMu.Unlock()

	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(w).book {
		if m.BlocksBoatSpot(w) && crosses(m.BlastBox()) {
			return true
		}
	}
	return false
}

// ridingBoat reports whether this player rides a boat: the guard the
// position packets' handlers ask, a body in a seat being where its vehicle
// keeps it.
func (c *client) ridingBoat() bool {
	return c.vehicle.Load() != nil
}

// handleMoveVehicle reads the driver's report of where their boat now
// stands and turns, and the server's answer to a report it cannot believe:
// one not made of real numbers ends the connection, and one that moved the
// boat too far or into the blocks is put back with a Move Vehicle of the
// server's own.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func (c *client) handleMoveVehicle(r *protocol.Reader) error {
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	onGround := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("move_vehicle", err)
	}
	// A NaN or non-finite value ends the connection before the loading
	// check drops the packet.
	// vanilla: PacketUtils.containsInvalidValues
	if containsInvalidValues(x, y, z, yaw, pitch) {
		return c.disconnect(fmt.Errorf("invalid vehicle movement from %s", c.player.Name),
			protocol.Text{Translate: "multiplayer.disconnect.invalid_vehicle_movement"})
	}
	// Dropped while loading, as every movement packet's handler drops.
	if !c.clientLoaded() {
		return nil
	}
	it := c.vehicle.Load()
	if it == nil {
		if ent := c.mount.Load(); ent != nil && ent.w == c.theWorld() {
			c.srv.moveHorse(c, ent, clampHorizontal(x), clampVertical(y), clampHorizontal(z),
				command.WrapDegrees(yaw), pitch, onGround)
		}
		return nil
	}
	if it.w != c.theWorld() {
		return nil // a stale drive: the seat was given up already
	}
	c.srv.moveBoat(c, it, clampHorizontal(x), clampVertical(y), clampHorizontal(z),
		command.WrapDegrees(yaw), onGround)
	return nil
}

// handlePaddleBoat reads the driver's paddle report into the boat's
// metadata state, which the tick sends out to every viewer; the driver's
// own client has drawn the paddles already.
// vanilla: ServerGamePacketListenerImpl.handlePaddleBoat
func (c *client) handlePaddleBoat(r *protocol.Reader) error {
	left, right := r.ReadBool(), r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("paddle_boat", err)
	}
	if !c.clientLoaded() {
		return nil
	}
	it := c.vehicle.Load()
	if it == nil {
		return nil
	}
	s := c.srv
	s.boatsMu.Lock()
	if it.driver() == c {
		it.boat.LeftPaddle, it.boat.RightPaddle = left, right
	}
	s.boatsMu.Unlock()
	return nil
}

// mobLeftItsBoat takes a mob out of whatever boat's passenger list it was
// left holding — a dead or despawned body's last ride, whose hull must not
// keep listing it, or a mob a lead just took off the seat — and sets it
// down beside the hull. Each lock is taken alone: the hull's list first, the
// mob's own vehicle field then, the emptied seat told to everyone after.
func (s *Server) mobLeftItsBoat(ent *mobEntity) {
	s.removeMobFromBoat(ent, true)
}

// removeMobFromBoat can retain a teleport destination while ending the seat.
func (s *Server) removeMobFromBoat(ent *mobEntity, reposition bool) {
	var (
		w          *world.World
		id         int32
		ids        []int32
		bx, by, bz float64
		found      bool
	)
	s.boatsMu.Lock()
	for _, it := range s.boats {
		for i, p := range it.passengers {
			if p.ent != ent {
				continue
			}
			it.passengers = slices.Delete(it.passengers, i, i+1)
			w, id, ids = it.w, it.boat.ID, it.passengerIDs()
			bx, by, bz = it.boat.X, it.boat.Y, it.boat.Z
			found = true
			break
		}
		if found {
			break
		}
	}
	s.boatsMu.Unlock()
	s.mobsMu.Lock()
	ent.vehicle = nil
	ent.mob.RidesVehicle = false
	ent.mob.BoardingCooldown = boardingCooldown
	if found && reposition {
		m := ent.mob
		x, y, z := rules.BoatDismountSpot(w, bx, by, bz, m.Yaw,
			[]rules.DismountPose{{Dims: rules.PlayerDimensions{Width: m.BbWidth(), Height: m.BbHeight()}}})
		m.SetPos(x, y, z)
	}
	s.mobsMu.Unlock()
	if !found {
		return
	}
	s.gameEvent(w, rules.GameEventEntityDismount, bx, by, bz, rules.GameEventContext{Mob: ent.uuid})
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
}

// mobTookOverBoat hands the boat seat a converted body held to the body that
// replaced it, vanilla's SINGLE conversion keeping the old body's seat: the
// hull's passenger list names the new body and its vehicle points at the boat,
// done before the caller shows the body so its first tick already reads it as
// seated. The hull and the seat IDs come back for the caller to tell everyone
// shown the hull once the body is in the world.
// vanilla: ConversionType.convert
func (s *Server) mobTookOverBoat(old, next *mobEntity) (*boatEntity, []int32) {
	var (
		it         *boatEntity
		ids        []int32
		bx, by, bz float64
	)
	s.boatsMu.Lock()
	for _, cand := range s.boats {
		for i, p := range cand.passengers {
			if p.ent != old {
				continue
			}
			// startRiding appends: the old body leaves the list and the
			// converted one takes the last seat, not the old index.
			cand.passengers = append(cand.passengers[:i], cand.passengers[i+1:]...)
			cand.passengers = append(cand.passengers, &boatPassenger{ent: next})
			it, ids = cand, cand.passengerIDs()
			bx, by, bz = cand.boat.X, cand.boat.Y, cand.boat.Z
			break
		}
		if it != nil {
			break
		}
	}
	s.boatsMu.Unlock()
	if it == nil {
		return nil, nil
	}
	s.mobsMu.Lock()
	next.vehicle = it
	next.mob.Carrier = it.uuid
	next.mob.StartRiding()
	s.mobsMu.Unlock()
	s.gameEvent(it.w, rules.GameEventEntityDismount, bx, by, bz, rules.GameEventContext{Mob: old.uuid})
	return it, ids
}

// boatByEntityID is the boat of a world an entity ID names, under the boat
// lock.
func (s *Server) boatByEntityID(w *world.World, id int32) *boatEntity {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.w == w && it.boat.ID == id && !it.boat.Dead {
			return it
		}
	}
	return nil
}

// useOnBoat answers the Interact a player made with a boat. A lead's own
// use comes first, vanilla's Entity.interact running ahead of the boat's:
// a hull the player already leads is untied, and a lead ties one held by
// nobody. AbstractBoat.interact then boards a plain click on a free seat,
// which a sneaking hand does not take; where it does not board,
// AbstractChestBoat.interact opens a chest boat's cargo and a plain boat
// answers nothing.
// vanilla: AbstractBoat.interact, Entity.interact, AbstractChestBoat.interact
func (c *client) useOnBoat(it *boatEntity, hand int32, sneaking bool) error {
	// vanilla's handleInteract verifies the body's box against the entity
	// interaction range and its buffer before the use is read at all: a
	// use past the reach is no use. The box is read under the boat lock,
	// which the tick's step holds while it moves the hull.
	pos := c.player.Position()
	c.srv.boatsMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
	c.srv.boatsMu.Unlock()
	if !rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		return nil
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		// A spectator uses nothing but the cargo of a MenuProvider, and a
		// cargo still holding its loot table opens nothing.
		// vanilla: Player.interactOn, AbstractChestBoat.createMenu
		if !entity.IsChestBoat(it.kind) {
			return nil
		}
		c.srv.boatsMu.Lock()
		unrolled := it.lootTable != ""
		c.srv.boatsMu.Unlock()
		if unrolled {
			return nil
		}
		return c.openBoatCargo(it)
	}
	if handled, err := c.leashBoat(it, hand); handled {
		return err
	}
	if !sneaking && c.srv.boatHasFreeSeat(it) {
		c.useSwing = swingPredicted
		c.srv.mountBoat(c, it, false)
		return nil
	}
	if entity.IsChestBoat(it.kind) {
		c.useSwing = swingPredicted
		return c.openBoatCargo(it)
	}
	return nil // a sneaking hand, or a full plain boat, mounts nothing
}

// boatHasFreeSeat reports whether a hull still has a seat for another rider,
// read under the boat lock as vanilla's canAddPassenger reads the passenger
// list.
func (s *Server) boatHasFreeSeat(it *boatEntity) bool {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	return it.boat.CanTakePassenger(len(it.passengers), entity.BoatCapacity(it.kind))
}

// leashBoat handles the leash branches of an Interact on a boat, vanilla's
// Entity.interact as AbstractBoat.interact runs it ahead of the boarding: a
// boat the player already leads comes untied — the lead dropping back as
// the item it is — and a lead ties a boat held by nobody. handled is true
// when either answered the use. The hold is read and written in one
// boat-lock section, so the click cannot race the hull's own leash step.
// vanilla: Entity.interact, Leashable.dropLeash
func (c *client) leashBoat(it *boatEntity, hand int32) (handled bool, err error) {
	s := c.srv
	held := c.heldItem(hand)
	item := registry.Item(held.ItemID)
	// The players' entity IDs, for the held-by-a-player test: the boat lock
	// may not be taken while mu is.
	playerIDs := s.playerEntityIDs()

	if item == registry.ItemShears {
		if drops, x, y, z := s.cutLeashesOfBoat(it); c.finishPlayerLeashShear(it.w, drops, x, y, z, hand) {
			return true, nil
		}
	}
	if c.pluginListening(pluginruntime.PlayerLeashEntity) {
		s.boatsMu.Lock()
		untie := !it.boat.Dead && it.holder == c.player.EntityID
		tie := !untie && !it.boat.Dead && item == registry.ItemLead && !playerIDs[it.holder]
		s.boatsMu.Unlock()
		if (untie || tie) && !c.leashAllowed(it.uuid, c.player.UUID, untie) {
			return true, nil
		}
	}
	s.boatsMu.Lock()
	if it.boat.Dead {
		s.boatsMu.Unlock()
		return false, nil
	}
	if it.holder == c.player.EntityID {
		c.useSwing = swingPredicted
		it.untie()
		s.boatsMu.Unlock()
		if c.player.GameMode() != protocol.GameModeCreative {
			s.dropLeadItem(it.w, it.boat.X, it.boat.Y, it.boat.Z)
		}
		s.broadcastWorld(it.w, protocol.IDPlaySetEntityLink, leashLinkPayload(it.boat.ID, 0))
		s.broadcastSoundID(it.w,
			knotSoundUntied, protocol.SoundCategoryNeutral, it.boat.X, it.boat.Y, it.boat.Z, 1.0, 1.0)

		return true, nil
	}
	if item != registry.ItemLead || playerIDs[it.holder] {
		s.boatsMu.Unlock()
		return false, nil
	}
	left := it.holder
	it.tie(c.player.EntityID, leashRef{holder: c.player.UUID})
	w, x, y, z, id := it.w, it.boat.X, it.boat.Y, it.boat.Z, it.boat.ID
	s.boatsMu.Unlock()

	if left != 0 {
		s.dropLeadItem(w, x, y, z)
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, 0))
	}
	s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, c.player.EntityID))
	s.broadcastSoundID(w,
		knotSoundTied, protocol.SoundCategoryNeutral, x, y, z, 1.0, 1.0)

	// InteractionResult.SUCCESS_SERVER: the server swings the arm.
	c.useSwing = swingServer
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	if left != 0 {
		if knot := s.knotByEntityID(w, left); knot != nil {
			s.discardEmptiedKnot(knot)
		}
	}
	return true, nil
}

// mountBoat seats a player on a boat, vanilla's startRiding: a seat free of
// the kind's capacity and a hull not under water take them, the passengers
// list grows by them, every viewer is told the boat's passengers, and the
// body moves to the seat the boat holds. The boarding player's own client
// takes it from there, switching to the boat's controls off the
// passengers packet alone. force is startRiding's: the capacity and the
// hull's water are not asked, as /ride mount does. It reports whether the
// rider was seated.
// vanilla: Entity.startRiding
func (s *Server) mountBoat(rider *client, it *boatEntity, force bool) bool {
	if !force && !rider.mountAllowed(it.uuid, true) {
		return false
	}
	// A rider switches over from a cart or another hull first, vanilla's
	// startRiding leaving the old vehicle before it seats them.
	if rider.minecart.Load() != nil {
		rider.dismountMinecart(false)
	}
	// A rider of a mob or a cushion leaves it only once this hull would take them.
	if rider.ridingMob() || rider.ridingCushion() {
		s.boatsMu.Lock()
		ok := !it.boat.Dead && !rider.player.Vitals().Dead &&
			(force || it.boat.CanTakePassenger(len(it.passengers), entity.BoatCapacity(it.kind)))
		s.boatsMu.Unlock()
		if !ok {
			return false
		}
		rider.dismountMob(false)
		rider.dismountCushion(false)
	}
	// A rider of another hull leaves it only once this one would take them.
	if old := rider.vehicle.Load(); old != nil {
		s.boatsMu.Lock()
		ok := old != it && !it.boat.Dead && !rider.player.Vitals().Dead &&
			(force || it.boat.CanTakePassenger(len(it.passengers), entity.BoatCapacity(it.kind)))
		s.boatsMu.Unlock()
		if !ok {
			return false
		}
		rider.dismountBoat(false)
	}
	s.boatsMu.Lock()
	if rider.vehicle.Load() != nil || rider.minecart.Load() != nil || rider.ridingCushion() ||
		rider.player.Vitals().Dead || it.boat.Dead ||
		(!force && !it.boat.CanTakePassenger(len(it.passengers), entity.BoatCapacity(it.kind))) {
		s.boatsMu.Unlock()
		return false
	}
	it.passengers = append(it.passengers, &boatPassenger{c: rider})
	rider.vehicle.Store(it)
	// Every player aboard moves to the seat the longer list holds: the
	// second passenger takes the stern, which walks the first to the bow.
	count := len(it.passengers)
	type seat struct {
		c       *client
		x, y, z float64
	}
	seats := make([]seat, 0, count)
	for i, p := range it.passengers {
		if p.c == nil {
			continue
		}
		x, y, z := it.playerSeatOf(count, i)
		seats = append(seats, seat{c: p.c, x: x, y: y, z: z})
	}
	vehicleID := it.boat.ID
	ids := it.passengerIDs()
	w := it.w
	bx, by, bz := it.boat.X, it.boat.Y, it.boat.Z
	s.boatsMu.Unlock()

	s.gameEvent(w, rules.GameEventEntityMount, bx, by, bz, rider.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers,
		setPassengersPayload(vehicleID, ids))
	// The seat is close by where they stood, but it is where the server
	// keeps them from now on: recorded like any step's, told of like any
	// step's, and carrying no survival of its own.
	riders := make([]*client, len(seats))
	for i, st := range seats {
		pos := st.c.player.Position()
		pos.X, pos.Y, pos.Z = st.x, st.y, st.z
		st.c.move(pos, false)
		riders[i] = st.c
	}
	startedRidingAdvancements(riders)
	return true
}

// stepOffBoat draws a rider out of a boat they have just left: the server's
// own step to the spot, the client's teleport that has to follow the Set
// Passengers taking it out of the seat, and the pose re-derived from the new
// spot. The sneak that begins a dismount is held through the step, so a rider
// on open ground is drawn crouched beside the hull, not standing; this server
// has no tick loop to run vanilla's per-tick pose decision. A dead body is
// left where the seat held it.
// vanilla: Entity.dismountTo, Player.updatePlayerPose
func (c *client) stepOffBoat(seat player.Position) {
	if c.player.Vitals().Dead {
		return
	}
	seat.Yaw, seat.Pitch = c.player.Position().Yaw, c.player.Position().Pitch
	seat.OnGround = false
	c.move(seat, true)
	c.dismountTo(seat)
	c.updateAppearance(func(*player.Appearance) {})
}

// dismountTo tells a player's own client where the server has stepped them
// off a seat, vanilla's Entity.dismountTo, which teleports through
// ServerPlayer.teleportTo. The Synchronize Player Position follows the Set
// Passengers that took them out of the seat: the client ignores a position
// applied while it still rides. The teleport keeps the client's look and
// velocity, the relative delta and rotation vanilla asks for; a dead body is
// left where the seat held it.
// vanilla: Entity.dismountTo, ServerPlayer.teleportTo
func (c *client) dismountTo(pos player.Position) {
	if !c.clientLoaded() || c.player.Vitals().Dead {
		return
	}
	c.teleport(command.Move{
		Pos:         pos,
		RelYaw:      true,
		RelPitch:    true,
		DeltaX:      true,
		DeltaY:      true,
		DeltaZ:      true,
		RotateDelta: true,
	})
}

// dismountBoat takes a player off the boat they ride, the sneak of vanilla's
// Player Input while it drives: the seat given back and every viewer told the
// passenger's off, then, when step is set, the body drawn out beside the hull
// with its own client told where. A dimension change removes the seat without
// the step — vanilla's ServerPlayer.changeDimension calls only removeVehicle,
// and a step teleport queued before its Respawn would reach a client still in
// the old world — and a leave removes it for a connection already closing.
// vanilla: AbstractBoat.getDismountLocationForPassenger
func (c *client) dismountBoat(step bool) {
	c.dismountMob(step)
	it := c.vehicle.Load()
	if it == nil {
		return
	}
	s := c.srv
	var ids []int32
	var vehicleID int32
	var broadcast bool
	var w *world.World
	var seat player.Position
	var bx, by, bz float64
	type rem struct {
		c       *client
		x, y, z float64
	}
	var rems []rem
	s.boatsMu.Lock()
	if i := slices.IndexFunc(it.passengers, func(p *boatPassenger) bool { return p.c == c }); i >= 0 {
		it.passengers = slices.Delete(it.passengers, i, i+1)
		c.vehicle.Store(nil)
		if it.driver() == nil {
			// Nobody's hand drives; the paddles keep nothing.
			it.boat.LeftPaddle, it.boat.RightPaddle = false, false
		}
		x, y, z := rules.BoatDismountSpot(it.w, it.boat.X, it.boat.Y, it.boat.Z,
			c.player.Position().Yaw, rules.PlayerDismountPoses)
		seat = player.Position{X: x, Y: y, Z: z}
		w = it.w
		bx, by, bz = it.boat.X, it.boat.Y, it.boat.Z
		vehicleID = it.boat.ID
		ids = it.passengerIDs()
		broadcast = true
		// Whoever is left moves to the seat the shorter list holds: a
		// sole passenger comes back to the hull's centre.
		count := len(it.passengers)
		for j, p := range it.passengers {
			if p.c == nil {
				continue
			}
			rx, ry, rz := it.playerSeatOf(count, j)
			rems = append(rems, rem{c: p.c, x: rx, y: ry, z: rz})
		}
	}
	s.boatsMu.Unlock()
	if broadcast {
		s.gameEvent(w, rules.GameEventEntityDismount, bx, by, bz, c.gameEventContext(0))
		s.broadcastWorld(w, protocol.IDPlaySetPassengers,
			setPassengersPayload(vehicleID, ids))
		if step {
			// Their own step off the boat, the landing the boat's top
			// makes of it.
			c.stepOffBoat(seat)
		}
		for _, r := range rems {
			pos := r.c.player.Position()
			pos.X, pos.Y, pos.Z = r.x, r.y, r.z
			r.c.move(pos, false)
		}
	}
}

// moveBoat puts a driven boat at the spot its driver reported, once the
// report has passed the checks every one of them goes through — a
// single-tick move past ten blocks, or a new collision, is refused with
// the boat's own standing sent back as a Move Vehicle
// — and carries every passenger with it: their bodies move to the seats
// the boat now holds, steps recorded and told of like any others but
// carrying no survival of their own — a boat's fall is the boat's, not its
// riders'.
func (s *Server) moveBoat(rider *client, it *boatEntity, x, y, z float64, yaw float32, onGround bool) {
	s.boatsMu.Lock()
	if it.boat.Dead || it.driver() != rider {
		s.boatsMu.Unlock()
		return
	}
	if it.boat.MovedTooFar(it.firstX, it.firstY, it.firstZ, x, y, z) {
		back := player.Position{X: it.boat.X, Y: it.boat.Y, Z: it.boat.Z,
			Yaw: it.boat.Yaw, Pitch: 0}
		backID := vehicleMovePayload(back)
		s.boatsMu.Unlock()
		slog.Warn("boat moved too quickly", "player", rider.player.Name,
			"x", x, "y", y, "z", z)
		rider.send(protocol.IDPlayMoveVehicle, backID)
		return
	}
	// vanilla: ServerGamePacketListenerImpl.isEntityCollidingWithAnythingNew
	boxAt := func(x, y, z float64) [6]float64 {
		x0, y0, z0, x1, y1, z1 := entity.BoatBoxAt(x, y, z)
		return [6]float64{x0, y0, z0, x1, y1, z1}
	}
	if it.w.CollidesAnew(boxAt(it.boat.X, it.boat.Y, it.boat.Z), boxAt(x, y, z)) {
		back := player.Position{X: it.boat.X, Y: it.boat.Y, Z: it.boat.Z,
			Yaw: it.boat.Yaw, Pitch: 0}
		backID := vehicleMovePayload(back)
		s.boatsMu.Unlock()
		slog.Warn("boat reported into the blocks", "player", rider.player.Name,
			"x", x, "y", y, "z", z)
		rider.send(protocol.IDPlayMoveVehicle, backID)
		return
	}
	fromX, fromY, fromZ := it.boat.X, it.boat.Y, it.boat.Z
	rider.noteMovement(x-it.boat.X, y-it.boat.Y, z-it.boat.Z)
	it.boat.SetPos(x, y, z)
	boatTracker.invalidatePistonGrid(s, it)
	it.boat.Yaw = yaw
	it.boat.OnGround = onGround
	movement := vehicleMovement{w: it.w, source: it.uuid, event: it.boat.RiddenMovement(it.w, fromX, fromY, fromZ)}
	type seat struct {
		c       *client
		x, y, z float64
	}
	var driverSeat seat
	var seats []seat
	for i, p := range it.passengers {
		if p.c == nil {
			continue
		}
		sx, sy, sz := it.playerSeatAt(len(it.passengers), i, x, y, z, yaw)
		if p.c == rider {
			driverSeat = seat{c: rider, x: sx, y: sy, z: sz}
			continue
		}
		seats = append(seats, seat{c: p.c, x: sx, y: sy, z: sz})
	}
	s.boatsMu.Unlock()
	s.emitVehicleMovement(movement)

	move := func(st seat) {
		pos := st.c.player.Position()
		pos.X, pos.Y, pos.Z = st.x, st.y, st.z
		pos.OnGround = onGround
		st.c.move(pos, false)
	}
	move(driverSeat)
	for _, st := range seats {
		move(st)
	}
}

// vehicleMovePayload builds the clientbound Move Vehicle that puts a boat
// back where the server keeps it: the hull's absolute position, yaw and
// pitch alone, with no entity ID and no on-ground flag.
// vanilla: ClientboundMoveVehiclePacket
func vehicleMovePayload(pos player.Position) []byte {
	var buf protocol.Buffer
	buf.PutF64(pos.X)
	buf.PutF64(pos.Y)
	buf.PutF64(pos.Z)
	buf.PutF32(pos.Yaw)
	buf.PutF32(pos.Pitch)
	return buf.Bytes()
}

// attackBoat is the Attack packet against a boat: the reach and the swing
// spent before anything lands, the blow the held item carries, and the
// rocking the boat's metadata carries out to every viewer. A survival
// blow's accumulated damage past forty destroys the boat, the tick's next
// pass taking it out of the world with the item its break leaves; a
// creative blow discards it with nothing left behind. A driver's own boat
// is no target of theirs.
// vanilla: VehicleEntity.hurtServer
func (c *client) attackBoat(target int32) {
	it := c.srv.boatByEntityID(c.theWorld(), target)
	if it == nil || c.vehicle.Load() == it {
		return
	}
	// The reach the attack verifies against, as against a mob. The box is
	// read under the boat lock, which the tick's step holds while it moves
	// the hull.
	pos := c.player.Position()
	c.srv.boatsMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
	snap := c.srv.boatSnapshot(it, nil)
	c.srv.boatsMu.Unlock()
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return
	}
	held := c.heldItem(protocol.HandMain)
	blow, ok := c.prepareAttack(held, it.entityTypeID(), false, snapshotLoot(snap))
	if !ok {
		return
	}

	s := c.srv
	landed, metadata := s.boatHurt(it, blow.damage, blow.context.DamageType, c.player.GameMode() == protocol.GameModeCreative, c.player.EntityID)

	if metadata != nil {
		s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, metadata)
	}
	if !landed {
		return
	}
	if entity.IsChestBoat(it.kind) && c.player.GameMode() != protocol.GameModeCreative {
		s.boatsMu.Lock()
		dead := it.boat.Dead
		s.boatsMu.Unlock()
		if dead {
			c.angerPiglinsOnChestVehicleBreak(c.theWorld())
		}
	}
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback)
	if extra > 0 {
		vx, vy, vz := rules.ExtraKnockbackPush(float64(pos.Yaw), extra)
		s.pushBoat(it, vx, vy, vz)
	}
	c.endSprintAfterKnockback(extra)
	if blow.feedback.Sweep {
		// The blow landed on a boat, and the sweep around it still
		// reaches the living bodies standing near it.
		c.sweepHurt(c.theWorld(), held, blow.baseDamage, it.boat.ID, minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.landedBlowFeedback(blow.feedback, it.boat.ID)
	c.finishMeleeItemEffects(held, blow.context, c.player.Vitals().FallDistance, false)
	// A non-living target wears nothing. vanilla: Player.itemAttackInteraction
	c.exhaust(rules.AttackExhaustion)
}

// pushBoat adds to a standing boat's motion; its tick carries the push out.
// vanilla: Entity.push
func (s *Server) pushBoat(it *boatEntity, vx, vy, vz float64) {
	s.boatsMu.Lock()
	if !it.boat.Dead {
		it.boat.Push(vx, vy, vz)
	}
	s.boatsMu.Unlock()
}

// boatShakeMetadata lays the rocking a blow wound, the three fields the
// body's own state carries: whatever they now hold, the values the
// clients' records were just brought to.
func boatShakeMetadata(b *entity.Boat) []byte {
	return boatMetadataFields(b, 0, false, customName{}, false, false, false, true, true, b.HurtDamage != 0, false, false, false, false)
}

// boatProjectileBlow is one boat a projectile's flight met: the world the
// hull stands in, the entity ID the hit is looked up by, the damage the
// blow carries, whether the shooter was a creative player whose blow
// discards the hull with nothing left behind, and the shaft when an arrow
// flew, so the caller can spend or fly it on once the hurt has answered.
type boatProjectileBlow struct {
	hitSound   registry.Sound
	damageType registry.DamageType
	w          *world.World
	boatID     int32
	damage     float64
	dropless   bool
	arrow      *arrowEntity
	// ownerID, fire and speedSq are the shooter, whether the shaft burns
	// and its squared speed, for the TNT cart's rules.
	ownerID int32
	fire    bool
	speedSq float64
}

// boatHurt applies a blow's hurt to a boat under the boat lock: the rocking
// wound, the damage added. It returns the metadata broadcast for a hull left
// standing — the rocking taken into every watching client's record, so the
// tick's own metadata pass does not send it twice — and whether the blow
// landed. The removal and the drop of a hull the blow broke are the boat
// tick's next pass.
// attacker is the hurting entity's ID, 0 for none.
// vanilla: VehicleEntity.hurtServer
func (s *Server) boatHurt(it *boatEntity, damage float64, damageType registry.DamageType, dropless bool, attacker int32) (landed bool, metadata []byte) {
	s.boatsMu.Lock()
	landed = it.boat.Hurt(damage, damageType, dropless)
	if landed && !it.boat.Dead {
		now := boatShownOf(it)
		for _, was := range it.tracked.all() {
			was.own.hurtTime, was.own.hurtDir, was.own.hurtDamage = now.hurtTime, now.hurtDir, now.hurtDamage
		}
		metadata = boatShakeMetadata(it.boat)
	}
	x, y, z := it.boat.X, it.boat.Y, it.boat.Z
	s.boatsMu.Unlock()
	if landed {
		s.gameEvent(it.w, rules.GameEventEntityDamage, x, y, z, s.attackerEventContext(attacker))
	}
	return landed, metadata
}

// boatProjectileHit applies a projectile's blow to the boat the entity ID
// names, the body of VehicleEntity.hurtServer a boat runs: the rocking
// wound, the damage added, and the metadata of a hull left standing told to
// every watcher. The removal and the drop of a hull the blow broke are the
// boat tick's next pass. It reports whether the blow landed.
// vanilla: VehicleEntity.hurtServer
func (s *Server) boatProjectileHit(blow boatProjectileBlow) bool {
	it := s.boatByEntityID(blow.w, blow.boatID)
	if it == nil {
		// Entity IDs are unique across kinds, so an ID boatSnapshots
		// handed out that no hull holds is a minecart's.
		return s.minecartProjectileHit(blow)
	}
	landed, metadata := s.boatHurt(it, blow.damage, blow.damageType, blow.dropless, blow.ownerID)
	if metadata != nil {
		s.broadcastWorld(blow.w, protocol.IDPlayEntityMetadata, metadata)
	}
	return landed
}

// boatSnapshots reads every standing boat's box and entity type under the
// boat lock, before a projectile tick takes its own locks: the boat lock is
// taken beneath none of them. Each world's hulls come back as the boxes a
// flight's swept line may meet, the blows are looked up by entity ID once
// the locks are down, and roots maps every body aboard a hull to that
// hull's ID — the vehicle group the owner-exclusion reads. The standing
// minecarts are merged in: projectiles meet both kinds as vehicles.
func (s *Server) boatSnapshots() (byWorld map[*world.World][]entity.ArrowTarget, ids map[int32]bool, roots map[int32]int32) {
	byWorld, ids, roots = s.boatHullSnapshots()
	cartWorlds, cartIDs, cartRoots := s.minecartSnapshots()
	if cartIDs == nil {
		return byWorld, ids, roots
	}
	if byWorld == nil {
		return cartWorlds, cartIDs, cartRoots
	}
	for w, ts := range cartWorlds {
		byWorld[w] = append(byWorld[w], ts...)
	}
	maps.Copy(ids, cartIDs)
	maps.Copy(roots, cartRoots)
	return byWorld, ids, roots
}

// boatHullSnapshots is boatSnapshots over the boats alone.
func (s *Server) boatHullSnapshots() (byWorld map[*world.World][]entity.ArrowTarget, ids map[int32]bool, roots map[int32]int32) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	if len(s.boats) == 0 {
		return nil, nil, nil
	}
	byWorld = make(map[*world.World][]entity.ArrowTarget)
	ids = make(map[int32]bool)
	roots = make(map[int32]int32)
	for _, it := range s.boats {
		if it.boat.Dead {
			continue
		}
		byWorld[it.w] = append(byWorld[it.w], entity.BoatTarget(it.boat.ID, it.entityTypeID(), it.boat))
		ids[it.boat.ID] = true
		for _, p := range it.passengers {
			if p.c != nil {
				roots[p.c.player.EntityID] = it.boat.ID
			} else if p.ent != nil {
				roots[p.ent.mob.ID] = it.boat.ID
			}
		}
	}
	return byWorld, ids, roots
}

// killBoat kills a boat: the end marked without reaching destroy, so the
// hull leaves no boat item, while the tick's next pass still spills the
// cargo a chest boat carried.
// vanilla: Entity.kill, AbstractChestBoat.remove
func (s *Server) killBoat(it *boatEntity) {
	s.boatsMu.Lock()
	if !it.boat.Dead {
		it.boat.Dead = true
		it.boat.NoLoot = true
		it.scoresSettled = true
		s.entityDestroyed(it.uuid)
	}
	s.boatsMu.Unlock()
}

// teleportBoat moves a boat to the position given and every passenger's body
// to the seat the boat now holds; the tick tells the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportBoat(it *boatEntity, to player.Position, yaw float32, by string) {
	type riderSeat struct {
		c       *client
		x, y, z float64
	}
	var riders []riderSeat
	boatTracker.teleport(s, it, func() {
		// A teleport during the block entity pass invalidates the column snapshot.
		s.minecartsMu.Lock()
		s.invalidateCartColumnsLocked()
		s.minecartsMu.Unlock()
		it.boat.SetPos(to.X, to.Y, to.Z)
		it.boat.Yaw, it.look.pitch = yaw, to.Pitch
		it.boat.VX, it.boat.VY, it.boat.VZ = 0, 0, 0
		it.firstX, it.firstY, it.firstZ = to.X, to.Y, to.Z
		for i, p := range it.passengers {
			if p.c != nil {
				x, y, z := it.playerSeatAt(len(it.passengers), i, to.X, to.Y, to.Z, yaw)
				riders = append(riders, riderSeat{c: p.c, x: x, y: y, z: z})
			}
		}
	})
	for _, r := range riders {
		pos := r.c.player.Position()
		pos.X, pos.Y, pos.Z = r.x, r.y, r.z
		r.c.move(pos, false)
	}
	logTeleported("boat", to, by)
}

// boatTwinAt raises a twin of a boat for a crossing: a fresh body of the
// same kind and identity, at rest at the position given, the cargo carried
// across. The passengers are not: their bodies belong to the clients that
// crossed, and the boat arrives empty.
func (s *Server) boatTwinAt(src *boatEntity, to *dimension, pos player.Position) *boatEntity {
	twin := &boatEntity{
		boat:  entity.NewBoat(s.nextEntity(), pos.X, pos.Y, pos.Z, 0),
		uuid:  src.uuid,
		w:     to.w,
		kind:  src.kind,
		cargo: src.cargo,

		lootTable: src.lootTable,
		lootSeed:  src.lootSeed,
		tags:      nbt.NewStringSet(src.tags.Values()),

		custom: src.custom,

		// The lead crosses as a saved one, re-tied on the far side.
		// vanilla: Entity.restoreFrom
		leash: src.leash,
	}
	twin.firstX, twin.firstY, twin.firstZ = pos.X, pos.Y, pos.Z
	if entity.IsChestBoat(src.kind) && twin.cargo == nil {
		twin.cargo = make([]protocol.ItemStack, entity.BoatCargoSlots)
	}
	return twin
}

// savedVehicle is the RootVehicle compound of a rider's file: Attach names
// the boat, minecart, mob or cushion they sit on, Entity is the root of the
// ride with its mob passengers nested, the player never among them. Nil on
// foot, or when the ride carries another player. With take, as on leaving, the
// root and every passenger are discarded, so the file's copy is the only one a
// rejoin can raise, and a container vehicle's cargo goes with it rather than
// spill twice. The vehicle and mob locks are taken one after the other, never
// together.
// vanilla: ServerPlayer.saveParentVehicle, PlayerList.remove
func (c *client) savedVehicle(take bool) nbt.Compound {
	s := c.srv
	boat := c.vehicle.Load()
	cart := c.minecart.Load()
	var root *mobEntity
	var attach uuid.UUID
	// A file's vehicle not yet raised is saved as the file had it, the seat
	// still empty; a departure takes it so the tick raises nothing.
	// vanilla: PlayerList.placeNewPlayer seats the rider before any save
	if pending := c.vehicleOnJoin.Load(); pending != nil {
		if take {
			c.vehicleOnJoin.Store(nil)
		}
		return *pending
	}
	if seat := c.cushion.Load(); seat != nil {
		return c.savedCushion(seat, take)
	}
	if boat != nil {
		attach = boat.uuid
	} else if cart != nil {
		attach = cart.uuid
	} else if ent := c.mount.Load(); ent != nil {
		s.mobsMu.Lock()
		attach, root = ent.uuid, ent
		book := s.herdOf(ent.w).book
		for root.mob.Vehicle != nil && book[root.mob.Vehicle] != nil {
			root = book[root.mob.Vehicle]
		}
		if boat = root.vehicle; boat != nil {
			root = nil
		} else if cart = root.cart; cart != nil {
			root = nil
		}
		s.mobsMu.Unlock()
	} else {
		return nil
	}

	var tag nbt.CompoundTag
	players := 0
	var mobs []*mobEntity
	var w *world.World
	if boat != nil {
		s.boatsMu.Lock()
		if boat.boat.Dead {
			s.boatsMu.Unlock()
			return nil
		}
		for _, p := range boat.passengers {
			if p.c != nil {
				players++
			} else if p.ent != nil {
				mobs = append(mobs, p.ent)
			}
		}
		tag, w = boatTag(boat), boat.w
		s.boatsMu.Unlock()
	} else if cart != nil {
		s.minecartsMu.Lock()
		if cart.cart.Dead {
			s.minecartsMu.Unlock()
			return nil
		}
		players = len(cart.passengers)
		mobs = append(mobs, cart.mobs...)
		tag, w = minecartTag(cart), cart.w
		s.minecartsMu.Unlock()
	} else {
		mobs, w = []*mobEntity{root}, root.w
	}

	s.mobsMu.Lock()
	if root != nil && root.mob.Discarded() {
		s.mobsMu.Unlock()
		return nil
	}
	book := s.herdOf(w).book
	var tree []*mobEntity
	for _, m := range mobs {
		tree = appendRideTree(tree, m, book)
	}
	for _, m := range tree {
		players += len(m.playersAboard())
	}
	if players != 1 {
		s.mobsMu.Unlock()
		return nil
	}
	if root != nil {
		tag = mobTagIn(root, book)
	} else if len(mobs) > 0 {
		// The carried riders boatTag or minecartTag wrote stay ahead of the mobs.
		carried, _ := nbt.Get[nbt.List](tag.Entries(), "Passengers")
		list := nbt.List{Elem: nbt.TypeCompound, Items: slices.Clone(carried.Items)}
		for _, m := range mobs {
			list.Items = append(list.Items, mobTagIn(m, book))
		}
		tag.Set("Passengers", list)
	}
	if take {
		for _, m := range tree {
			m.vehicle = nil
			m.cart = nil
			m.mob.RidesVehicle = false
			m.mob.Discard()
		}
	}
	s.mobsMu.Unlock()

	if take && boat != nil {
		s.boatsMu.Lock()
		boat.boat.Dead = true
		boat.boat.NoLoot = true
		boat.scoresSettled = true // vanilla: RemovalReason.UNLOADED_WITH_PLAYER
		boat.cargo = nil
		s.boatsMu.Unlock()
	} else if take && cart != nil {
		s.minecartsMu.Lock()
		cart.cart.Dead = true
		cart.cart.NoLoot = true
		cart.scoresSettled = true // vanilla: RemovalReason.UNLOADED_WITH_PLAYER
		cart.cargo = vehicleCargo{}
		cart.passengers = nil
		cart.mobs = nil
		s.minecartsMu.Unlock()
		c.minecart.Store(nil)
	}
	return nbt.Compound{"Attach": nbt.UUIDToIntArray(attach), "Entity": tag}
}

// appendRideTree appends a mob and its mob passengers, depth first. The
// caller holds the mob lock.
// vanilla: Entity.getSelfAndPassengers
func appendRideTree(tree []*mobEntity, ent *mobEntity, book map[*entity.Mob]*mobEntity) []*mobEntity {
	tree = append(tree, ent)
	for _, p := range ent.mob.Passengers {
		if pe, ok := book[p]; ok {
			tree = appendRideTree(tree, pe, book)
		}
	}
	return tree
}

// savedRideHolds reports whether an entity compound or one of its nested
// Passengers has the identity.
// vanilla: Entity.getIndirectPassengers
func savedRideHolds(compound nbt.Compound, identity uuid.UUID) bool {
	if id, ok := entityUUIDOfTag(compound); ok && id == identity {
		return true
	}
	list, _ := nbt.Get[nbt.List](compound, "Passengers")
	for _, item := range list.Items {
		if p, ok := nbt.CompoundView(item); ok && savedRideHolds(p, identity) {
			return true
		}
	}
	return false
}

// remountSavedVehicle seats a joining player back on the boat, minecart, mob or
// cushion their file's RootVehicle names in Attach, the root or any of its passengers. The
// root is found by identity — woken from its own chunk file beside the
// player, most turns, whatever chunk the wake of it lands in — and where the
// world holds no entity of the identity, the file's own copy is stood up with
// its mob passengers. Nothing is stood up when Attach names no entity of the
// ride: vanilla discards a root it cannot reattach to.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func (s *Server) remountSavedVehicle(c *client, dim *dimension, saved nbt.Compound) {
	entity, ok := nbt.Get[nbt.Compound](saved, "Entity")
	if !ok {
		return
	}
	attach, ok := nbt.UUIDField(saved, "Attach")
	if !ok || !savedRideHolds(entity, attach) {
		return
	}
	identity, ok := entityUUIDOfTag(entity)
	if !ok {
		return
	}
	if x, y, z, ok := entityPositionOfTag(entity); ok {
		// The root's own chunk wakes early for it: the entity it holds is
		// the one the pairing wants.
		s.wakeArrivalChunk(dim, world.SpawnPos{
			X: rules.BlockOf(x), Y: rules.BlockOf(y), Z: rules.BlockOf(z),
		})
	} else {
		return
	}
	id, _ := nbt.Get[nbt.String](entity, "id")
	if isCushionTag(id) {
		s.remountSavedCushion(c, dim.w, entity, identity)
		return
	}
	if isMobTag(id) {
		if s.raiseSavedMob(dim.w, entity) != nil {
			s.seatOnSavedMob(c, dim.w, attach)
		}
		return
	}
	if isMinecartTag(id) {
		s.remountSavedMinecart(c, dim.w, entity, identity, attach)
		return
	}
	s.boatsMu.Lock()
	var standing *boatEntity
	for _, it := range s.boats {
		if it.uuid == identity && !it.boat.Dead {
			standing = it
			break
		}
	}
	s.boatsMu.Unlock()
	if standing == nil {
		// The world holds no boat of the identity: the file's own copy
		// stands up with its mob passengers, the hull its chunk's file never
		// carried, and keeps its riders of other kinds to save again.
		if standing = boatOfTag(s, dim.w, entity); standing == nil {
			return
		}
		var woken wakeResult
		woken.collectTeam(entity)
		list, _ := nbt.Get[nbt.List](entity, "Passengers")
		var mobs []nbt.Compound
		for _, item := range list.Items {
			if p, ok := nbt.CompoundView(item); ok {
				if id, _ := nbt.Get[nbt.String](p, "id"); isMobTag(id) {
					mobs = append(mobs, p)
				} else {
					woken.collectTeams(p, nil)
					standing.carriedPassengers = append(standing.carriedPassengers, withoutTeam(p))
				}
			}
		}
		s.registerBoat(standing)
		for _, t := range woken.teams {
			s.joinTagTeam(t)
		}
		for _, p := range mobs {
			if ent := s.raiseSavedMob(dim.w, p); ent != nil {
				s.mountMobOnBoat(ent, standing)
			}
		}
	}
	if attach == identity {
		s.mountBoat(c, standing, false)
		return
	}
	s.seatOnSavedMob(c, dim.w, attach)
}

// savedVehicleChunkSent reports whether the rider was sent the chunk their
// file's RootVehicle stands in. The load timeout can run out before the
// chunk is even loaded, and a hull raised in a chunk nobody holds leaves
// with it on the next tick, dropping the rider; a sent chunk is one the
// rider holds. A root without a position passes, for the remount to refuse.
func (c *client) savedVehicleChunkSent(saved nbt.Compound) bool {
	root, ok := nbt.Get[nbt.Compound](saved, "Entity")
	if !ok {
		return true
	}
	x, _, z, ok := entityPositionOfTag(root)
	if !ok {
		return true
	}
	return c.tracks(c.dimension().w, world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)})
}

// sendBoatEntities shows a joining or arriving client every boat in its
// world within range, in boatJoinPairing's form.
func (c *client) sendBoatEntities() error {
	k := boatTracker
	k.pairing = boatJoinPairing
	return k.sendJoin(c)
}

// cutLeashesOfBoat cuts a boat's own lead and every lead it holds, returning
// the drops and the boat's position; none if it is gone.
// vanilla: Entity.shearOffAllLeashConnections
func (s *Server) cutLeashesOfBoat(it *boatEntity) (drops []mobLeashDrop, x, y, z float64) {
	s.boatsMu.Lock()
	if it.boat.Dead {
		s.boatsMu.Unlock()
		return nil, 0, 0, 0
	}
	w, id := it.w, it.boat.ID
	x, y, z = it.boat.X, it.boat.Y, it.boat.Z
	if it.holder != 0 {
		drops = append(drops, mobLeashDrop{w: w, mobID: id, holder: it.untie(), x: x, y: y, z: z})
	}
	s.boatsMu.Unlock()
	// boatsMu is taken with no other server lock held, so mobsMu waits.
	s.mobsMu.Lock()
	drops = s.cutHeldLeashesLocked(w, id, x, y+0.5*entity.BoatHeight, z, drops)
	s.mobsMu.Unlock()
	return drops, x, y, z
}

// boatsIn returns the live boats crossing the box.
func (s *Server) boatsIn(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) []*boatEntity {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	var in []*boatEntity
	for _, it := range s.boats {
		b := it.boat
		if it.w == w && !b.Dead &&
			b.X-entity.BoatHalfWidth < maxX && b.X+entity.BoatHalfWidth > minX &&
			b.Z-entity.BoatHalfWidth < maxZ && b.Z+entity.BoatHalfWidth > minZ &&
			b.Y < maxY && b.Y+entity.BoatHeight > minY {
			in = append(in, it)
		}
	}
	return in
}

// settleFrozenBoat settles the watchers of a hull a frozen world holds.
func settleFrozenBoat(it *boatEntity, watchers []trackingWatcher, near *nearbyPlayers, tick uint64, out *trackSends) {
	body := boatTracker.body(it)
	still, prevTick := body.stillSince(tick)
	for _, i := range body.watchers(near) {
		wh := watchers[i]
		if wh.c.theWorld() != it.w {
			continue // crossed to another dimension since the snapshot
		}
		body.settleOrTold(wh, still, prevTick, out)
	}
}
