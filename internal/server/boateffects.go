package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// One of the two kinds of seat the tick hands a mob: x, y, z is where
// the seat has it; boat names the one being boarded, nil for a move or
// a leave alone. A leaving body carries the hull's place and world,
// frozen under the boat lock, so the mob lock's phase can work its
// dismount out beside it.
type boatMobSeat struct {
	ent        *mobEntity
	boat       *boatEntity
	w          *world.World
	x, y, z    float64
	bx, by, bz float64
	removed    bool
}
type boatPlayerSeat struct {
	c       *client
	x, y, z float64
}
type boatEffects struct {
	out                        trackSends
	metas                      sendList
	removals                   map[*world.World][]int32
	breaks                     []boatBreak
	carries, boards, offboards []boatMobSeat
	news                       []seatNews
	bounces                    []bodyBounce
	dismounts                  []boatPlayerSeat
	boardDrops, leashDrops     []mobLeashDrop
	// riders are the players aboard a boat a mob boarded.
	riders    []*client
	padCells  []boatPad
	splashes  []projectileSplash
	movements vehicleMovements
	burns     []burnSpot
	fizzles   []burnSpot
	paddles   []boatPaddle
	gateways  []gatewayTouch
	travels   []boatPortalTravel
	// flakes are the points hulls in powder snow may kick up a snowflake at.
	flakes []snowflakeSpot
	// mobShares are the mobs' shares of the hulls' pushes.
	mobShares []mobPushShare
}

// boatPaddle is one paddle stroke's sound to broadcast.
type boatPaddle struct {
	w     *world.World
	sound entity.PaddleSound
}

// boatPortalTravel is one hull with no player aboard the portal pass caught.
type boatPortalTravel struct {
	it   *boatEntity
	kind rules.PortalKind
}

func (fx *boatEffects) apply(s *Server) {
	fx.movements.apply(s)
	applyEach(s, fx.splashes, (*Server).splashProjectile)
	applyEach(s, fx.burns, (*Server).burnVehicle)
	applyEach(s, fx.fizzles, (*Server).fizzleVehicle)
	applyEach(s, fx.paddles, (*Server).playPaddle)
	s.applyMobShares(fx.mobShares)
	// The seats the tick leaves mobs in, written under the mob lock alone.
	if len(fx.carries) > 0 || len(fx.boards) > 0 || len(fx.offboards) > 0 {
		s.lockTick(&s.mobsMu)
		for _, seat := range fx.boards {
			newsIndex := -1
			for i := range fx.news {
				if fx.news[i].w == seat.w && fx.news[i].vehicle == seat.boat.boat.ID {
					newsIndex = i
					break
				}
			}
			// vanilla: Entity.startRiding leaves the old vehicle first.
			if old := seat.ent.mob.Vehicle; old != nil {
				entity.Dismount(old, seat.ent.mob)
				fx.news[newsIndex].event(rules.GameEventEntityDismount, old.X, old.Y, old.Z, rules.GameEventContext{Mob: seat.ent.uuid})
				fx.news = append(fx.news, seatNews{w: seat.w, vehicle: old.ID, ids: old.PassengerIDs()})
			}
			fx.news[newsIndex].event(rules.GameEventEntityMount, seat.bx, seat.by, seat.bz, rules.GameEventContext{Mob: seat.ent.uuid})
			seat.ent.vehicle = seat.boat
			seat.ent.mob.StartRiding()
			// A leashed mob boards free of its lead: Mob.startRiding
			// drops it where the body stood, before the seat takes it,
			// laying the item whatever entity_drops says.
			if holder := seat.ent.mob.RemoveLeash(); holder != 0 {
				m := seat.ent.mob
				fx.boardDrops = append(fx.boardDrops, mobLeashDrop{
					w: seat.w, mobID: m.ID, holder: holder, x: m.X, y: m.Y, z: m.Z,
				})
			}
			seat.ent.mob.SetPos(seat.x, seat.y, seat.z)
		}
		for _, seat := range fx.carries {
			seat.ent.mob.SetPos(seat.x, seat.y, seat.z)
		}
		for _, seat := range fx.offboards {
			seat.ent.vehicle = nil
			seat.ent.mob.RidesVehicle = false
			m := seat.ent.mob
			m.BoardingCooldown = boardingCooldown
			var x, y, z float64
			if seat.removed {
				x, y, z = entity.OffRemovedVehicle(seat.w, seat.x, seat.y, seat.z, seat.by, m.BbWidth(), m.BbHeight())
			} else {
				x, y, z = rules.BoatDismountSpot(seat.w, seat.bx, seat.by, seat.bz, m.Yaw,
					[]rules.DismountPose{{Dims: rules.PlayerDimensions{Width: m.BbWidth(), Height: m.BbHeight()}}})
			}
			m.SetPos(x, y, z)
		}
		s.unlockTick(&s.mobsMu)
	}

	// A body broken out of every view ahead of the drop it left behind it.
	for w, ids := range fx.removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	for _, p := range fx.padCells {
		// vanilla: LilyPadBlock.entityInside
		if old := p.w.BlockAt(p.x, p.y, p.z); s.destroyBlockContentsBy(p.w, p.x, p.y, p.z, old, "") {
			s.setBlockAndTell(p.w, p.x, p.y, p.z, world.RemovalLeaves(old))
		}
	}
	s.rollSnowflakes(fx.flakes)
	for _, drop := range fx.boardDrops {
		// The lead off a mob that boarded a boat is laid whatever
		// entity_drops says, vanilla's startRiding dropping it.
		s.finishLeashDrop(drop, true)
	}
	for _, drop := range fx.leashDrops {
		// Only a holder that died or left consults entity_drops; a snap
		// lays its lead whatever the rule says.
		s.finishLeashDrop(drop, drop.broke || drop.w.BoolRule(world.RuleEntityDrops))
	}
	s.sendSeatNews(fx.news)
	s.runBodyBounces(fx.bounces)
	fx.out.send()
	fx.metas.send()
	for _, d := range fx.dismounts {
		d.c.stepOffBoat(player.Position{X: d.x, Y: d.y, Z: d.z})
	}
	for _, br := range fx.breaks {
		s.breakBoat(br)
	}
	s.touchGateways(fx.gateways)
	startedRidingAdvancements(fx.riders)
	for _, tr := range fx.travels {
		s.finishBoatTravel(tr.it, tr.kind)
	}
}

// finishBoatTravel sends one hull the portal pass caught across its portal,
// cargo and mob riders with it.
func (s *Server) finishBoatTravel(it *boatEntity, kind rules.PortalKind) {
	var turn float32
	boatTracker.crossPortal(s, it, kind, func(twin *boatEntity) {
		s.registerBoat(twin)
		s.carryBoatMobs(it, twin, turn)
	}, func(to *world.World, at player.Position) (*boatEntity, bool) {
		twin := boatOfTag(s, to, boatTag(it).Entries())
		if twin == nil {
			return nil, false
		}
		twin.carriedPassengers = it.carriedPassengers
		turn = at.Yaw - twin.boat.Yaw
		twin.boat.SetPos(at.X, at.Y, at.Z)
		twin.boat.Yaw = at.Yaw
		twin.firstX, twin.firstY, twin.firstZ = at.X, at.Y, at.Z
		return twin, true
	}, nil)
}

// moveBoatWithRiders carries a hull and everyone aboard into another
// dimension, as moveMinecartWithRider does a cart: the hull is re-homed at
// the arrival still holding its players, and its mobs follow as twins. A
// second player aboard finds it already moved.
// vanilla: Entity.teleportCrossDimension
func (s *Server) moveBoatWithRiders(it *boatEntity, to *dimension, move command.Move) {
	pos := move.Pos
	var turn float32
	if move.RelYaw {
		turn = move.Offset.Yaw
	}
	var old *world.World
	var id int32
	moved := false
	lockedDo(&s.boatsMu, func() {
		old, id = it.w, it.boat.ID
		if old == to.w || it.boat.Dead {
			return
		}
		it.w = to.w
		it.boat.SetPos(pos.X, pos.Y, pos.Z)
		// The client takes a freshly seated rider's yaw from the hull.
		// vanilla: ClientPacketListener.handleSetEntityPassengersPacket
		it.boat.Yaw += turn
		boatTracker.invalidatePistonGrid(s, it)
		it.firstX, it.firstY, it.firstZ = pos.X, pos.Y, pos.Z
		it.tracked = nil
		moved = true
	})
	if !moved {
		return
	}
	s.broadcastWorld(old, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{id}))
	s.carryBoatMobs(it, it, turn)
}

// carryBoatMobs seats on to, in to's world, twins of the mobs aboard from
// (mobsAcross).
// vanilla: Entity.teleportCrossDimension re-seating its newPassengers
func (s *Server) carryBoatMobs(from, to *boatEntity, turn float32) {
	var mobs []*mobEntity
	lockedDo(&s.boatsMu, func() { mobs = from.takeMobs() })
	s.reseatBoatMobs(mobs, from, to, turn)
}

// takeMobs takes the mobs off it's seats for a crossing. The caller holds
// boatsMu.
func (it *boatEntity) takeMobs() (mobs []*mobEntity) {
	it.passengers = slices.DeleteFunc(it.passengers, func(p *boatPassenger) bool {
		if p.ent != nil {
			mobs = append(mobs, p.ent)
		}
		return p.ent != nil
	})
	return mobs
}

// reseatBoatMobs seats on to twins of mobs, taken off from (mobsAcross).
func (s *Server) reseatBoatMobs(mobs []*mobEntity, from, to *boatEntity, turn float32) {
	s.boatsMu.Lock()
	w, x, y, z := to.w, to.boat.X, to.boat.Y, to.boat.Z
	s.boatsMu.Unlock()
	aboard := func(ent *mobEntity) bool { return ent.vehicle == from }
	for _, ent := range s.mobsAcross(mobs, aboard, w, x, y, z, turn) {
		s.seatMobOnBoat(ent, to, false)
	}
}

// mobsAcross discards the mobs still aboard a crossing vehicle, each with
// whatever rides it, and raises their twins from their tags at (x, y, z) in
// w, turned by turn degrees, for the caller to seat. The caller holds no
// vehicle lock.
func (s *Server) mobsAcross(mobs []*mobEntity, aboard func(*mobEntity) bool, w *world.World, x, y, z float64, turn float32) []*mobEntity {
	if len(mobs) == 0 {
		return nil
	}
	tags := make([]nbt.Compound, 0, len(mobs))
	s.mobsMu.Lock()
	for _, ent := range mobs {
		if !aboard(ent) || !ent.mob.Alive() || ent.mob.Discarded() {
			continue
		}
		book := s.herdOf(ent.w).book
		tag := nbt.PlainCopy(mobTagIn(ent, book)).(nbt.Compound)
		tag["Pos"] = vec3Tag(x, y, z)
		for _, m := range appendRideTree(nil, ent, book) {
			m.vehicle, m.cart = nil, nil
			m.mob.RidesVehicle = false
			m.mob.Discard()
		}
		tags = append(tags, tag)
	}
	s.mobsMu.Unlock()
	twins := make([]*mobEntity, 0, len(tags))
	for _, tag := range tags {
		ent := s.raiseSavedMob(w, tag)
		if ent == nil {
			continue
		}
		s.mobsMu.Lock()
		ent.mob.Yaw += turn
		ent.mob.HeadYaw = ent.mob.Yaw
		s.mobsMu.Unlock()
		twins = append(twins, ent)
	}
	return twins
}

// playPaddle broadcasts a paddle stroke to everyone near, the rower included.
func (s *Server) playPaddle(p boatPaddle) {
	sound := registry.SoundEntityBoatPaddleLand
	if p.sound.Water {
		sound = registry.SoundEntityBoatPaddleWater
	}
	s.broadcastSoundID(p.w, sound, protocol.SoundCategoryNeutral, p.sound.X, p.sound.Y, p.sound.Z, 1, p.sound.Pitch)
}
