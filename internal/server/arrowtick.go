package server

import (
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// tickArrows runs one game tick of every arrow, phased for the locks: the
// players, hulls and stands read first with nothing held; each arrow stepped
// against its world under both the arrow and mob locks, watchers settled
// against the tracking range of the arrow type, its hits, sticks and pickups
// collected; the effects applied after them.
func (s *Server) tickArrows() {
	in := newArrowTickInputs()
	s.readArrowTickInputs(&in)
	in.nearby = nearbyWorlds(in.watchers, func(w arrowWatcher) trackingWatcher {
		return trackingWatcher{c: w.c, x: w.pos.X, z: w.pos.Z, chunkRadius: w.chunkRadius}
	})
	defer releaseNearbyWorlds(in.nearby)
	var fx arrowEffects
	// removals is the arrows the tick itself ended, per world. It is kept
	// beside fx rather than in it: the collector's methods leak its
	// fields, which would carry the map to the heap.
	removals := make(map[*world.World][]int32)

	gate := s.newEntityGate()
	var held []*arrowEntity
	defer func() { arrowTracker.dormantPass(s, held) }()
	s.lockTick(&s.arrowsMu)
	s.lockTick(&s.mobsMu)
	flying := s.crossArrowPortalsLocked(&fx, gate)
	var hv arrowHangingView
	if flying {
		hv = newArrowHangingView()
		s.readArrowHanging(&hv)
	}
	alive := s.arrows[:0]
	for _, it := range s.arrows {
		if it.arrow.Dead {
			arrowTracker.unfile(it)
			continue // hit, picked up, or expired before this tick
		}
		if !gate.ticks(it.w, it.arrow.X, it.arrow.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		s.stepArrow(&fx, &in, &hv, it)
		// Expired in the ground or spent on a mob: vanilla's discard.
		if it.arrow.Dead {
			removals[it.w] = append(removals[it.w], it.arrow.ID)
			s.entityDestroyed(it.uuid)
			arrowTracker.unfile(it)
		} else {
			alive = append(alive, it)
		}
	}
	s.arrows = alive
	s.unlockTick(&s.mobsMu)
	s.unlockTick(&s.arrowsMu)

	fx.apply(s, removals)
	s.resolveHitAsks(fx.hitAsks)
	s.fireBlockHits(fx.blockHits)
}

// arrowWatcher is one player as the arrow tick sees them, read before any
// lock the tick takes.
type arrowWatcher struct {
	c           *client
	pos         player.Position
	dims        rules.PlayerDimensions
	chunkRadius int32
	creative    bool
}

// arrowTickInputs is what the arrow tick reads before the arrow lock.
type arrowTickInputs struct {
	// watchers is each world's players: the pickup and the sweep compare a
	// player's place with an arrow's.
	watchers map[*world.World][]arrowWatcher
	nearby   map[*world.World]*nearbyPlayers
	// The hulls of every world, read under the boat lock the tick's own
	// never holds: the boxes a flight line can reach, the frames' and the
	// mobs' own, and the vehicle each rider belongs to.
	boatsByWorld map[*world.World][]entity.ArrowTarget
	boatsByID    map[int32]bool
	boatRoots    map[int32]int32
	// standsByWorld is the stands' boxes, read under the stands lock the
	// tick's own never holds, the boats' way: each flight's sweep cuts them
	// to the slivers its line can reach.
	standsByWorld map[*world.World][]entity.ArrowTarget
	mannequins    mannequinTargetSnapshot
	// crystalsByWorld is the end crystals' boxes, read under their lock.
	crystalsByWorld map[*world.World][]entity.ArrowTarget
	// cushionsByWorld is the cushions' boxes, read under the fixtures lock.
	cushionsByWorld map[*world.World][]entity.ArrowTarget
	// worldTargets is each world's mob and player targets, built lazily by
	// the first arrow of the world; the arrow and mob locks are held for the
	// pass, so the herd cannot change under it.
	worldTargets map[*world.World]*arrowWorldTargets
	// spareByOwner maps a player on a team without friendly fire to the
	// entity IDs of every member of that team, itself included: the bodies
	// its shot passes. Nil while no such team has an online member.
	// vanilla: AbstractArrow.canHitEntity, Player.canHarmPlayer
	spareByOwner map[int32][]int32
}

// arrowWorldTargets is one world's bodies as targets, shared by its arrows.
type arrowWorldTargets struct {
	targets []entity.ArrowTarget // mobs in herd order, then players
	index   map[int32]int        // entity ID -> index in targets
	bodies  arrowBodies
}

// newArrowTickInputs makes the empty inputs of a tick. It is small enough to
// inline, so the watchers map stays in tickArrows' frame as long as no step
// leaks a map out of arrowTickInputs.
func newArrowTickInputs() arrowTickInputs {
	return arrowTickInputs{watchers: make(map[*world.World][]arrowWatcher)}
}

// readArrowTickInputs reads the players, the hulls and the stands into in,
// in that order, before the arrow lock.
func (s *Server) readArrowTickInputs(in *arrowTickInputs) {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		mode := c.player.GameMode()
		in.watchers[c.theWorld()] = append(in.watchers[c.theWorld()], arrowWatcher{
			c:           c,
			pos:         pos,
			dims:        c.dims(c.player.Appearance().Pose),
			chunkRadius: c.chunkRadius(),
			creative:    mode == protocol.GameModeCreative,
		})
	}
	in.boatsByWorld, in.boatsByID, in.boatRoots = s.boatSnapshots()
	in.standsByWorld = s.armorStandSnapshots()
	s.lockTick(&s.arrowsMu)
	flying := len(s.arrows) > 0
	s.unlockTick(&s.arrowsMu)
	if flying {
		in.mannequins = s.mannequinTargets()
		in.crystalsByWorld = s.endCrystalSnapshots()
		in.cushionsByWorld = s.cushionSnapshots()
		in.spareByOwner = s.teamSpares(clients)
	}
}

// teamSpares groups the online players by their team, keeping the teams
// with friendly fire off, as arrowTickInputs.spareByOwner.
func (s *Server) teamSpares(clients []*client) map[int32][]int32 {
	teams := s.scoreboard.MemberTeams()
	if teams == nil {
		return nil
	}
	members := make(map[string][]int32)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		if name, ok := teams[c.player.Name]; ok {
			members[name] = append(members[name], c.player.EntityID)
		}
	}
	var out map[int32][]int32
	for name, ids := range members {
		if t, ok := s.scoreboard.Team(name); !ok || t.FriendlyFire {
			continue
		}
		if out == nil {
			out = make(map[int32][]int32)
		}
		for _, id := range ids {
			out[id] = ids
		}
	}
	return out
}

// arrowPortalTravel is one arrow the portal pass is sending across.
type arrowPortalTravel struct {
	it   *arrowEntity
	kind rules.PortalKind
}

// crossArrowPortalsLocked runs the portal pass ahead of the tick, as for
// items: an arrow the portal is sending across leaves the list here, its
// arrival left to fx. It reports whether a retained arrow is not lodged in a
// block, the only kind whose step reads the hanging-entity view. The arrow
// lock is held.
func (s *Server) crossArrowPortalsLocked(fx *arrowEffects, gate entityGate) (flying bool) {
	portalAlive := s.arrows[:0]
	for _, it := range s.arrows {
		if it.arrow.Dead {
			arrowTracker.unfile(it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.arrow.X, it.arrow.Y, it.arrow.Z, &it.portal, projectileCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.arrow.X, it.arrow.Y, it.arrow.Z, state)
		if !due {
			a := it.arrow
			if state == rules.EndGatewayState {
				gatewayTouchAt(&fx.gateways, it.w, a.X, a.Y, a.Z, gatewayTouch{
					mu: &s.arrowsMu, portal: &it.portal, cooldown: projectileCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveArrow(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			flying = flying || !it.arrow.InGround
			continue
		}
		fx.travels = append(fx.travels, arrowPortalTravel{it: it, kind: kind})
		arrowTracker.unfile(it)
	}
	s.arrows = portalAlive
	return flying
}

// arrowHangingView is the frames, the paintings and the leash knots of each
// dimension, read once for the whole tick so the arrows do not each walk the
// world's lists: the per-arrow sweep cuts them down to the slivers its
// flight line can reach.
type arrowHangingView struct {
	frames    map[*world.World][]entitySliver
	frameByID map[*world.World]map[int32]*entitySliver
	paintings paintingSnapshot
	knots     map[*world.World][]entitySliver
	knotByID  map[*world.World]map[int32]bool
	// projectiles are the shulker bullets in flight, which a shaft breaks,
	// and the redirectable bodies, which it turns.
	projectiles map[*world.World][]entity.ArrowTarget
}

// newArrowHangingView makes the empty view, small enough to inline so its
// maps stay in tickArrows' frame.
func newArrowHangingView() arrowHangingView {
	return arrowHangingView{
		frames:    make(map[*world.World][]entitySliver),
		frameByID: make(map[*world.World]map[int32]*entitySliver),
		knots:     make(map[*world.World][]entitySliver),
		knotByID:  make(map[*world.World]map[int32]bool),
	}
}

// readArrowHanging reads the paintings and the frames, under the painting
// lock and the frame lock beneath it, then the knots under theirs, into hv.
// The arrow and mob locks are held.
func (s *Server) readArrowHanging(hv *arrowHangingView) {
	s.lockTick(&s.paintingsMu)
	hv.paintings = s.snapshotPaintingsLocked()
	s.lockTick(&s.framesMu)
	for _, f := range s.frames {
		sliver := frameSliverOf(f)
		hv.frames[f.w] = append(hv.frames[f.w], sliver)
		byID := hv.frameByID[f.w]
		if byID == nil {
			byID = make(map[int32]*entitySliver)
			hv.frameByID[f.w] = byID
		}
		byID[f.id] = &sliver
	}
	s.unlockTick(&s.framesMu)
	s.unlockTick(&s.paintingsMu)
	s.lockTick(&s.knotsMu)
	for _, k := range s.knots {
		sliver := knotSliverOf(k)
		hv.knots[k.w] = append(hv.knots[k.w], sliver)
		byID := hv.knotByID[k.w]
		if byID == nil {
			byID = make(map[int32]bool)
			hv.knotByID[k.w] = byID
		}
		byID[k.id] = true
	}
	s.unlockTick(&s.knotsMu)
	hv.projectiles = s.snapshotProjectileTargetsLocked()
}

// snapshotProjectileTargetsLocked is each world's shulker bullets and
// redirectable bodies in flight as targets. The arrow lock is held.
func (s *Server) snapshotProjectileTargetsLocked() map[*world.World][]entity.ArrowTarget {
	out := make(map[*world.World][]entity.ArrowTarget)
	for _, it := range s.thrown {
		t := it.throwable
		if t.Dead {
			continue
		}
		if t.Kind != entity.ThrownShulkerBullet && !t.Redirectable() {
			continue
		}
		minX, minY, minZ, maxX, maxY, maxZ := t.BlastBox()
		out[it.w] = append(out[it.w], entity.ArrowTarget{
			EntityID: t.ID, Type: t.Kind.EntityType(),
			MinX: minX, MinY: minY, MinZ: minZ, MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
		})
	}
	return out
}

// arrowBodies is the bodies of an arrow's dimension by the entity ID the
// hits and the owner's checks look them up by.
type arrowBodies struct {
	mobs    map[int32]*entity.Mob
	clients map[int32]*client
}

// stepArrow runs one arrow's tick, the arrow and mob locks held: its targets
// gathered, its step against the world, its watchers settled and its events
// turned into fx's kinds.
func (s *Server) stepArrow(fx *arrowEffects, in *arrowTickInputs, hv *arrowHangingView, it *arrowEntity) {
	var bodies arrowBodies
	dim := s.dimensionOfWorld(it.w)
	targets, owner := arrowTargets(it, dim, in, hv, &bodies)

	// pvp off: a shot a player owns passes every player by — no hit,
	// no ignite, no deflection — its own shooter's body included.
	// vanilla: AbstractArrow.canHitEntity
	byPlayer := shooterIsPlayer(it.arrow.OwnerID, bodies.clients)
	it.arrow.SparePlayers = !it.w.BoolRule(world.RulePvp) && byPlayer
	// A teammate under friendly fire off is passed the same way.
	it.arrow.SpareIDs = nil
	if byPlayer && !it.arrow.SparePlayers {
		it.arrow.SpareIDs = in.spareByOwner[it.arrow.OwnerID]
	}

	// A loyalty trident asking after a return its owner cannot take —
	// dead, or spectating — falls out of the trident's hands instead:
	// the stack lies where the body stands and the body goes, the
	// drop the tick after the locks lays.
	// vanilla: ThrownTrident.tick, isAcceptibleReturnOwner
	if it.arrow.Trident && it.arrow.Loyalty > 0 && it.arrow.DealtDamage() {
		if ownerClient := bodies.clients[it.arrow.OwnerID]; ownerClient != nil &&
			(ownerClient.player.Vitals().Dead || ownerClient.player.GameMode() == protocol.GameModeSpectator) {
			if it.arrow.Pickup == entity.PickupAllowed {
				fx.orphanDrops = append(fx.orphanDrops, it)
			}
			it.arrow.Dead = true
		}
	}

	it.queue(&fx.shots, it.w, it.arrow.X, it.arrow.Y, it.arrow.Z, it.ownerUUID)
	wasInGround := it.arrow.InGround
	it.piercingHit = it.arrow.PierceLevel > 0
	var before entity.Arrow
	var rollMark uint64
	asking := s.hitListening()
	if asking {
		before = *it.arrow
		rollMark = it.arrow.RandSrc.Mark()
	}
	// Only an enchanted weapon asks for loot conditions (hitDamage).
	it.arrow.BindLoot = nil
	if len(it.arrow.Weapon.Enchantments) > 0 {
		it.arrow.BindLoot = s.arrowLootBinder(it, &bodies)
	}
	events := it.arrow.Step(it.w, owner, targets)
	if asking {
		if ask, held := s.arrowHitAskOf(it, &bodies, before.HitAllowed, events); held {
			*it.arrow = before
			it.arrow.RandSrc.Rewind(rollMark)
			fx.hitAsks = append(fx.hitAsks, ask)
			return
		}
		it.arrow.HitAllowed = 0
	}
	if wasInGround != it.arrow.InGround {
		it.needsSync = true
	}
	for _, ev := range events {
		s.arrowEvent(fx, in, hv, &bodies, it, dim, ev)
		// vanilla: AbstractArrow.stepMoveAndHit, ProjectileDeflection.REVERSE
		if ev.Kind == entity.ArrowHitTarget || ev.Kind == entity.ArrowStuck || ev.Kind == entity.ArrowDeflected {
			it.needsSync = true
		}
	}
	fx.watchArrow(it, in.watchers[it.w], in.nearby[it.w], events)
}

// arrowTargets is what one arrow's flight may meet: the arrow's own
// dimension's mobs, read under the mob lock, and players, built from the
// places read before any of it, filled into bodies; then the frames, the
// paintings, the knots, the hulls and the stands whose boxes its swept box
// reaches. owner is the shooter's own body among them, the one the flight's
// first ticks cannot hit, nil when it is not in the dimension.
func arrowTargets(it *arrowEntity, dim *dimension, in *arrowTickInputs, hv *arrowHangingView, bodies *arrowBodies) (targets []entity.ArrowTarget, owner *entity.ArrowTarget) {
	wt := in.worldTargets[it.w]
	if wt == nil {
		wt = &arrowWorldTargets{
			index:  make(map[int32]int, len(dim.mobs.herd)),
			bodies: arrowBodies{mobs: make(map[int32]*entity.Mob, len(dim.mobs.herd)), clients: make(map[int32]*client)},
		}
		for _, m := range dim.mobs.herd {
			start := len(wt.targets)
			wt.index[m.ID] = start
			wt.targets = entity.MobProjectileTargets(wt.targets, m, mobEntityType(m.Type), in.boatRoots[m.ID])
			for _, target := range wt.targets[start:] {
				wt.bodies.mobs[target.EntityID] = m
			}
		}
		for _, w := range in.watchers[it.w] {
			target := w.c.projectileTarget(w.pos, projectileUnhittable(w.c))
			target.RootVehicle = in.boatRoots[w.c.player.EntityID]
			wt.index[w.c.player.EntityID] = len(wt.targets)
			wt.targets = append(wt.targets, target)
			wt.bodies.clients[w.c.player.EntityID] = w.c
		}
		for _, target := range in.mannequins.byWorld[it.w] {
			wt.index[target.EntityID] = len(wt.targets)
			wt.targets = append(wt.targets, target)
		}
		if in.worldTargets == nil {
			in.worldTargets = make(map[*world.World]*arrowWorldTargets)
		}
		in.worldTargets[it.w] = wt
	}
	*bodies = wt.bodies
	// Capped, so a sliver appended below copies instead of writing into the
	// shared list.
	targets = wt.targets[:len(wt.targets):len(wt.targets)]
	if i, ok := wt.index[it.arrow.OwnerID]; ok {
		owner = &targets[i]
	}
	// A lodged arrow's step never sweeps entities (vanilla:
	// AbstractArrow.tick, inGround branch), so it needs no slivers.
	if it.arrow.InGround {
		return targets, owner
	}
	// The frames and the paintings of the dimension whose slivers the
	// flight line can reach, their boxes what a shaft meets as
	// readily as a body's. The world's whole lists were read once
	// for the tick; this is the cut to the arrow's own swept box.
	boxMinX, boxMinY, boxMinZ, boxMaxX, boxMaxY, boxMaxZ := it.arrow.SweptBox()
	for _, f := range hv.frames[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{f.minX, f.minY, f.minZ}, [3]float64{f.maxX, f.maxY, f.maxZ}) {
			continue
		}
		targets = append(targets, entity.ArrowTarget{
			EntityID: f.id,
			Type:     f.typ,
			MinX:     f.minX, MinY: f.minY, MinZ: f.minZ,
			MaxX: f.maxX, MaxY: f.maxY, MaxZ: f.maxZ,
		})
	}
	for _, p := range hv.paintings.byWorld[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{p.minX, p.minY, p.minZ}, [3]float64{p.maxX, p.maxY, p.maxZ}) {
			continue
		}
		targets = append(targets, entity.ArrowTarget{
			EntityID: p.id,
			Type:     protocol.EntityTypePainting,
			MinX:     p.minX, MinY: p.minY, MinZ: p.minZ,
			MaxX: p.maxX, MaxY: p.maxY, MaxZ: p.maxZ,
		})
	}
	for _, k := range hv.knots[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{k.minX, k.minY, k.minZ}, [3]float64{k.maxX, k.maxY, k.maxZ}) {
			continue
		}
		targets = append(targets, entity.ArrowTarget{
			EntityID: k.id,
			Type:     protocol.EntityTypeLeashKnot,
			MinX:     k.minX, MinY: k.minY, MinZ: k.minZ,
			MaxX: k.maxX, MaxY: k.maxY, MaxZ: k.maxZ,
		})
	}
	// The hulls of the dimension the flight line can reach, what a
	// shaft meets as readily as a body's. The world's whole list was
	// read once for the tick; this is the cut to the arrow's own swept box.
	for _, bt := range in.boatsByWorld[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{bt.MinX, bt.MinY, bt.MinZ}, [3]float64{bt.MaxX, bt.MaxY, bt.MaxZ}) {
			continue
		}
		targets = append(targets, bt)
	}
	// The stands of the dimension the flight line can reach, whose
	// always-kills hurt breaks them where it lands.
	for _, st := range in.standsByWorld[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{st.MinX, st.MinY, st.MinZ}, [3]float64{st.MaxX, st.MaxY, st.MaxZ}) {
			continue
		}
		targets = append(targets, st)
	}
	// Any hurt blows a crystal up, so a shaft meets it as a body.
	// vanilla: Projectile.canHitEntity, EndCrystal.isPickable
	for _, cr := range in.crystalsByWorld[it.w] {
		if !rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{cr.MinX, cr.MinY, cr.MinZ}, [3]float64{cr.MaxX, cr.MaxY, cr.MaxZ}) {
			continue
		}
		targets = append(targets, cr)
	}
	for _, cu := range in.cushionsByWorld[it.w] {
		if rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{cu.MinX, cu.MinY, cu.MinZ}, [3]float64{cu.MaxX, cu.MaxY, cu.MaxZ}) {
			targets = append(targets, cu)
		}
	}
	for _, b := range hv.projectiles[it.w] {
		if rules.BoxesIntersect(
			[3]float64{boxMinX, boxMinY, boxMinZ}, [3]float64{boxMaxX, boxMaxY, boxMaxZ},
			[3]float64{b.MinX, b.MinY, b.MinZ}, [3]float64{b.MaxX, b.MaxY, b.MaxZ}) {
			targets = append(targets, b)
		}
	}
	return targets, owner
}

// watchArrow settles the watchers of an arrow that has stepped: its velocity,
// its pairings and removals, the metadata the tick turned, its movement on
// the schedule, and the pickup a watcher's touch claims. The arrow lock is
// held.
func (fx *arrowEffects) watchArrow(it *arrowEntity, watchers []arrowWatcher, near *nearbyPlayers, events []entity.ArrowEvent) {
	body := arrowTracker.body(it)
	mt := body.schedule(it.takeSync)

	// Velocity updates go to the watchers alone, including newly paired ones.
	// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
	var motion *outbound
	if it.motionDue(it.arrow.VX, it.arrow.VY, it.arrow.VZ, false) {
		motion = &outbound{id: protocol.IDPlaySetEntityMotion,
			data: entityVelocityPayload(it.arrow.ID, it.arrow.VX, it.arrow.VY, it.arrow.VZ)}
	}

	now := arrowMetadataStateOf(it)
	for _, i := range body.watchers(near) {
		w := watchers[i]
		if w.c.theWorld() != it.w {
			continue // crossed to another dimension since the snapshot
		}
		was, seen := body.settle(w.c, w.pos.X, w.pos.Z, w.chunkRadius, &fx.track)
		if !seen {
			continue
		}
		if motion != nil {
			fx.track.moves.add(w.c, *motion)
		}
		// The metadata a tick turned goes out the tick it turned in.
		if was.own != now {
			if slices.ContainsFunc(events, func(e entity.ArrowEvent) bool { return e.Kind == entity.ArrowEffectFaded }) {
				// vanilla: Arrow.tick, broadcastEntityEvent
				fx.metas.add(w.c, outbound{id: protocol.IDPlayEntityEvent, data: entityEventPayload(it.arrow.ID, protocol.EntityStatusArrowEffectPuff)})
			}
			if data := arrowMetadataDeltaPayload(it, was.own); data != nil {
				fx.metas.add(w.c, outbound{id: protocol.IDPlayEntityMetadata, data: data})
			}
			was.own = now
		}
		if mt.gate {
			body.moveTold(was, w.c, mt, &fx.track)
		}

		// Every watcher whose grown box touches a takeable arrow claims it;
		// the first claim run after the locks takes it.
		takeable := it.arrow.CanPickUp(w.creative)
		if it.arrow.Trident {
			// vanilla: ThrownTrident.playerTouch, tryPickup
			takeable = tridentCanTake(it.arrow, w.c.player.EntityID, w.creative)
		}
		if takeable && arrowWithinTouch(w.pos, w.dims, it.arrow) {
			fx.pickups = append(fx.pickups, arrowPickup{arrow: it, collector: w.c})
		}
	}
}

// arrowEvent turns one event of an arrow's step into the effects it leaves.
// The arrow and mob locks are held.
func (s *Server) arrowEvent(fx *arrowEffects, in *arrowTickInputs, hv *arrowHangingView, bodies *arrowBodies, it *arrowEntity, dim *dimension, ev entity.ArrowEvent) {
	switch ev.Kind {
	case entity.ArrowStuck:
		if s.hitListening() {
			fx.blockHits = append(fx.blockHits, arrowBlockHit{id: it.uuid, w: it.w, hit: ev.BlockHit, x: ev.X, y: ev.Y, z: ev.Z})
		}
		fx.landings = append(fx.landings, blockProjectileLanding(it.w, ev.BlockHit, it.ownerUUID))
		if it.arrow.Channeling > 0 || !it.arrow.Weapon.Empty() {
			fx.channelHits = append(fx.channelHits, channelBlockHit(it, ev.BlockHit))
		}
		// The hit-ground voice, the trident's own for its body.
		fx.sounds = append(fx.sounds, arrowHitSound(it.w, it.arrow, ev.HitSound))
		if isTargetBlock(ev.BlockHit.State) {
			fx.targetHits = append(fx.targetHits, targetBlockHit{w: it.w, block: ev.BlockHit, arrow: true, ownerID: it.arrow.OwnerID})
		}
		if isProjectileHitBlock(ev.BlockHit.State) {
			ownerID := it.arrow.OwnerID
			ownerName := ""
			ownerMode := int32(protocol.GameModeSurvival)
			if c := bodies.clients[ownerID]; c != nil {
				ownerName = c.player.Name
				ownerMode = c.player.GameMode()
			}
			pickupStack := it.arrow.PickupStack
			if it.arrow.Trident {
				pickupStack = it.arrow.Weapon // the thrown stack is also its pickup
			}
			fx.projectileHits = append(fx.projectileHits, projectileBlockHit{
				w: it.w, block: ev.BlockHit, fire: it.arrow.FireTicks > 0,
				entityType: it.entityType,
				ownerMob:   bodies.mobs[ownerID] != nil, ownerID: ownerID,
				ownerName: ownerName, ownerUUID: it.ownerUUID,
				ownerMode: ownerMode, pickupStack: pickupStack,
			})
		}
	case entity.ArrowDousedCauldron:
		// vanilla: Projectile.mayInteract — a mob's arrow needs mob_griefing
		if bodies.mobs[it.arrow.OwnerID] == nil || it.w.BoolRule(world.RuleMobGriefing) {
			fx.cauldronDouses = append(fx.cauldronDouses, arrowCauldron{it.w, int32(ev.X), int32(ev.Y), int32(ev.Z)})
		}
	case entity.ArrowMeltedSnow:
		fx.snowMelts = append(fx.snowMelts, projectileBlockHit{
			w: it.w, block: entity.BlockHit{X: int32(ev.X), Y: int32(ev.Y), Z: int32(ev.Z)},
			ownerMob: bodies.mobs[it.arrow.OwnerID] != nil, ownerID: it.arrow.OwnerID, ownerUUID: it.ownerUUID,
		})
	case entity.ArrowSnowflake:
		fx.flakes = append(fx.flakes, snowflakeSpot{it.w, ev.X, ev.Y, ev.Z})
	case entity.ArrowBubbleSurface:
		fx.bubbles = append(fx.bubbles, bubbleSurface{it.w, int32(ev.X), int32(ev.Y), int32(ev.Z)})
	case entity.ArrowSplashed:
		fx.splashes = append(fx.splashes, projectileSplash{w: it.w, x: ev.X, y: ev.Y, z: ev.Z,
			volume: ev.Volume, pitch: ev.Pitch, owner: it.ownerUUID})
	case entity.ArrowTridentReturn:
		// The loyalty's homecoming, played once at volume ten,
		// whose earshot is 160 blocks.
		fx.sounds = append(fx.sounds, pendingSound{w: it.w, sound: registry.SoundItemTridentReturn.Holder(),
			category: protocol.SoundCategoryNeutral, x: ev.X, y: ev.Y, z: ev.Z,
			volume: entity.TridentReturnVolume, pitch: 1.0})
	case entity.ArrowHitTarget:
		s.arrowHitTarget(fx, in, hv, bodies, it, dim, ev)
		fx.landings = append(fx.landings, projectileLanding{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, ownerUUID: it.ownerUUID})
	case entity.ArrowDeflected:
		if d, ok := deflectedBy(it.w, bodies.mobs[ev.Target.EntityID]); ok {
			fx.deflects = append(fx.deflects, d)
		}
	}
}

// arrowHitTarget sorts one body an arrow's flight met into the hurt it
// takes. A mob's hurt runs here under the mob lock, every other body's after
// the locks, the shaft staying in the list until that hurt answers whether
// it lands or bounces. The ignition a burning shaft grants runs ahead of the
// hurt and is put back when the hurt is refused; a body gone from the world
// between sweep and hit is deflected off, as a hurt that cannot land.
func (s *Server) arrowHitTarget(fx *arrowEffects, in *arrowTickInputs, hv *arrowHangingView, bodies *arrowBodies, it *arrowEntity, dim *dimension, ev entity.ArrowEvent) {
	if it.arrow.Channeling > 0 && ev.Target.Type != protocol.EntityTypeEnderman {
		target := ev.Target
		hit := tridentChannelHit{arrow: it, x: (target.MinX + target.MaxX) / 2, y: target.MinY, z: (target.MinZ + target.MaxZ) / 2, victim: target.Type, category: protocol.SoundCategoryNeutral}
		if m := bodies.mobs[target.EntityID]; m != nil {
			hit.x, hit.y, hit.z = m.X, m.Y, m.Z
			hit.silent = m.Silent
			hit.category = mobSoundCategory(m.Type)
		}
		if c := bodies.clients[target.EntityID]; c != nil {
			pos := c.player.Position()
			hit.x, hit.y, hit.z = pos.X, pos.Y, pos.Z
			hit.category = protocol.SoundCategoryPlayer
		}
		fx.channelHits = append(fx.channelHits, hit)
	}
	// The body turns, then refuses the shaft's hurt, which bounces it below.
	// vanilla: Projectile.onHit, AbstractHurtingProjectile.hurtServer
	if registry.EntityType(ev.Target.Type).InTag(registry.EntityTypeTagIDRedirectableProjectile) {
		fx.redirects = append(fx.redirects, s.redirectByHitLocked(it.w, ev.Target.EntityID, bodies, it.arrow.OwnerID, it.ownerUUID))
	}
	switch target := ev.Target; {
	case target.Type == protocol.EntityTypeMannequin:
		fx.ownerHurts = markOwnerHurt(fx.ownerHurts, bodies.clients, it.arrow.OwnerID, target.EntityID)
		fx.mannequinHits = append(fx.mannequinHits, arrowMannequinHit{ent: in.mannequins.byID[target.EntityID], arrow: it, event: ev, creative: shooterCreative(it.arrow.OwnerID, bodies.clients)})
	case bodies.mobs[target.EntityID] != nil:
		m := bodies.mobs[target.EntityID]
		fx.ownerHurts = markOwnerHurt(fx.ownerHurts, bodies.clients, it.arrow.OwnerID, m.ID)
		priorFire := m.FireTicks
		// vanilla: AbstractArrow.onHitEntity skips an enderman
		if ev.IgniteTicks > 0 && m.Type != entity.TypeEnderman {
			m.IgniteForTicksBy(int(ev.IgniteTicks), entity.CombustSource{By: pluginEntity{id: it.uuid}})
		}
		hit := s.arrowHurtMob(it, dim, m, ev, shooterIsPlayer(it.arrow.OwnerID, bodies.clients), shooterCreative(it.arrow.OwnerID, bodies.clients), shooterHitSource(it.arrow.OwnerID, bodies, m))
		// A projectile dodge can teleport the mob before the next arrow's query.
		wt := in.worldTargets[it.w]
		i := wt.index[m.ID]
		root := wt.targets[i].RootVehicle
		wt.targets[i] = entity.MobTarget(m.ID, mobEntityType(m.Type), m)
		wt.targets[i].RootVehicle = root
		if hit.victimID == 0 && !it.arrow.Trident {
			// A refused hurt puts back the burn the arrow lit.
			// vanilla: AbstractArrow.onHitEntity
			m.SetMetadataFireTicks(priorFire)
		}
		fx.mobHits = append(fx.mobHits, hit)
		if hit.victimID != 0 {
			fx.landChanneling(it)
		}
		if it.arrow.Trident && !hit.dodged {
			// The trident's hit is spent, not the body: the
			// deflection it bounces off with is queued for
			// the phase after the locks.
			fx.tridentBounces = append(fx.tridentBounces, it)
		}
	case bodies.clients[target.EntityID] != nil:
		fx.ownerHurts = markOwnerHurt(fx.ownerHurts, bodies.clients, it.arrow.OwnerID, target.EntityID)
		fx.playerHits = append(fx.playerHits, arrowPlayerHit{
			hitSound:    ev.HitSound,
			arrow:       it,
			w:           it.w,
			victim:      bodies.clients[target.EntityID],
			damage:      ev.Damage,
			igniteTicks: ev.IgniteTicks,
			effects:     arrowPostEffects(it),
			custom:      it.custom,
			x:           ev.X, y: ev.Y, z: ev.Z,
			vx: ev.VX, vz: ev.VZ,
		})
	case hv.frameByID[it.w][target.EntityID] != nil:
		// A frame takes the hurt a body does: its item pops, or
		// it breaks whole; a fixed one refuses every hurt but a
		// creative shooter's.
		// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
		f := hv.frameByID[it.w][target.EntityID]
		shooter := bodies.clients[it.arrow.OwnerID]
		hit := arrowFrameHit{
			hitSound: ev.HitSound,
			w:        it.w,
			frameID:  f.id,
			dropless: creativeClient(shooter),
			mob:      bodies.mobs[it.arrow.OwnerID] != nil,
			arrow:    it,
		}
		if f.fixed && !hit.dropless {
			if it.arrow.Trident {
				// A fixed frame the blow cannot take is one
				// the trident bounces off standing, the same
				// every hit it ever lands holds for it.
				fx.tridentBounces = append(fx.tridentBounces, it)
			} else {
				it.arrow.Bounce(it.w)
			}
			break
		}
		fx.frameHits = append(fx.frameHits, hit)
	case hv.knotByID[it.w][target.EntityID]:
		// A knot takes the shaft's hurt as a hanging body
		// does; a mob's shaft is refused where mob_griefing is off.
		// vanilla: BlockAttachedEntity.hurtServer
		fx.knotHits = append(fx.knotHits, arrowKnotHit{
			hitSound: ev.HitSound,
			w:        it.w,
			knotID:   target.EntityID,
			arrow:    it,
			mob:      bodies.mobs[it.arrow.OwnerID] != nil,
			creative: creativeClient(bodies.clients[it.arrow.OwnerID]),
		})
	case hv.paintings.byIDWorld[it.w][target.EntityID]:
		// A shaft takes a painting down at one blow, a mob's
		// only what mob_griefing lets it.
		// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
		shooter := bodies.clients[it.arrow.OwnerID]
		fx.paintingHits = append(fx.paintingHits, paintingHit{
			hitSound:   ev.HitSound,
			w:          it.w,
			paintingID: target.EntityID,
			dropless:   creativeClient(shooter),
			mob:        bodies.mobs[it.arrow.OwnerID] != nil,
			arrow:      it,
		})
	case in.boatsByID[target.EntityID]:
		// A hull takes the shaft's hurt as any body does, in the boat
		// lock's own phase after the locks, which answers whether it
		// lands or the hull is gone.
		// vanilla: VehicleEntity.hurtServer
		shooter := bodies.clients[it.arrow.OwnerID]
		fx.boatHits = append(fx.boatHits, boatProjectileBlow{
			hitSound: ev.HitSound,
			w:        it.w,
			boatID:   target.EntityID,
			damage:   ev.Damage,
			dropless: creativeClient(shooter),
			arrow:    it,
			ownerID:  it.arrow.OwnerID,
			fire:     it.arrow.FireTicks > 0,
			speedSq:  it.arrow.VX*it.arrow.VX + it.arrow.VY*it.arrow.VY + it.arrow.VZ*it.arrow.VZ,
		})
	case target.Type == protocol.EntityTypeShulkerBullet:
		// Any hurt destroys a bullet; it goes now, so a second shaft of the
		// tick passes through.
		// vanilla: ShulkerBullet.hurtServer
		if b := s.bulletByIDLocked(it.w, target.EntityID); b != nil {
			b.throwable.Dead = true
			fx.bulletHits = append(fx.bulletHits, arrowBulletHit{hitSound: ev.HitSound, w: it.w, arrow: it, id: b.throwable.ID,
				x: b.throwable.X, y: b.throwable.Y, z: b.throwable.Z})
		}
	case target.Type == protocol.EntityTypeEndCrystal:
		// vanilla: EndCrystal.hurtServer
		fx.crystalHits = append(fx.crystalHits, arrowCrystalHit{
			hitSound: ev.HitSound,
			w:        it.w, crystalID: target.EntityID,
			ownerID: it.arrow.OwnerID, ownerUUID: it.ownerUUID,
			arrow: it,
		})
	case target.Type == protocol.EntityTypeCushion:
		// vanilla: Cushion.hurtServer
		fx.cushionHits = append(fx.cushionHits, cushionProjectileHit{
			hitSound: ev.HitSound,
			w:        it.w, id: target.EntityID, arrow: it,
			shooter: bodies.clients[it.arrow.OwnerID],
			mob:     bodies.mobs[it.arrow.OwnerID] != nil,
		})
	case target.Type == protocol.EntityTypeArmorStand:
		// A stand takes an arrow's or a trident's hurt as the
		// always_kills_armor_stands tag's instant break.
		// vanilla: ArmorStand.hurtServer
		shooter := bodies.clients[it.arrow.OwnerID]
		fx.standHits = append(fx.standHits, arrowStandHit{
			hitSound: ev.HitSound,
			w:        it.w,
			standID:  target.EntityID,
			arrow:    it,
			dropless: creativeClient(shooter),
			mob:      bodies.mobs[it.arrow.OwnerID] != nil,
			noBuild:  shooter != nil && !rules.ModeMayBuild(shooter.player.GameMode()),
		})
	default:
		if it.arrow.Trident {
			fx.tridentBounces = append(fx.tridentBounces, it)
		} else {
			it.arrow.Bounce(it.w)
		}
	}
}

// arrowLootBinder lets a data-pack effect's loot conditions read a hit's
// bodies: the victim when it is a mob or player, the shooter as attacker, and
// the arrow itself as direct attacker. The arrow and mob locks are held.
// vanilla: Enchantment.damageContext
func (s *Server) arrowLootBinder(it *arrowEntity, bodies *arrowBodies) func(*rules.EnchContext, *entity.ArrowTarget) {
	book := s.herdOf(it.w).book
	mobLoot := func(m *entity.Mob) *rules.LootEntity {
		if ent := book[m]; ent != nil {
			return s.mobLootLocked(ent)
		}
		e := mobLootEntity(m)
		return &e
	}
	return func(ctx *rules.EnchContext, target *entity.ArrowTarget) {
		typed := func(t int32) func() *rules.LootEntity {
			return func() *rules.LootEntity { return &rules.LootEntity{Type: registry.EntityType(t).Name()} }
		}
		ctx.BodyOf = typed(ctx.Victim)
		if m := bodies.mobs[target.EntityID]; m != nil {
			ctx.BodyOf = func() *rules.LootEntity { return mobLoot(m) }
		} else if c := bodies.clients[target.EntityID]; c != nil {
			ctx.BodyOf = victimLoot(c)
		}
		if c := bodies.clients[it.arrow.OwnerID]; c != nil {
			ctx.AttackerOf = victimLoot(c)
		} else if m := bodies.mobs[it.arrow.OwnerID]; m != nil {
			ctx.AttackerOf = func() *rules.LootEntity { return mobLoot(m) }
		}
		ctx.DirectAttackerOf = func() *rules.LootEntity {
			snap := s.arrowSnapshot(it, nil)
			if snap == nil {
				return typed(ctx.DirectAttacker)()
			}
			// The arrow lock is held by this step and the snapshot's saved
			// compound would take it again. The snapshot is read-only and
			// local to this call, so a private lock stands in for it.
			snap.mu = new(sync.Mutex)
			return snapshotLoot(snap)()
		}
		ctx.LootWorld = func(loot *rules.LootContext) {
			s.fillBlowWorld(it.w, loot, [3]float64{it.arrow.X, it.arrow.Y, it.arrow.Z})
		}
	}
}
