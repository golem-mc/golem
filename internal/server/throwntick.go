package server

import (
	"cmp"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// tickThrowables runs one game tick of every thrown body: the players and
// hulls read with no lock held, each body stepped under the arrow and mob
// locks collecting its effects, and the effects applied after them.
func (s *Server) tickThrowables() {
	in := thrownTickInputs{worlds: make(map[*world.World]*thrownWorld)}
	s.readThrownTickInputs(&in)
	for _, tw := range in.worlds {
		tw.nearby = nearbyFor(tw.watchers, func(w thrownWatcher) trackingWatcher {
			return trackingWatcher{c: w.c, x: w.pos.X, z: w.pos.Z, chunkRadius: w.chunkRadius}
		})
		defer tw.nearby.release()
	}
	var fx thrownEffects
	gate := s.newEntityGate()
	var held []*thrownEntity
	defer func() { thrownTracker.dormantPass(s, held) }()

	s.lockTick(&s.arrowsMu)
	s.lockTick(&s.mobsMu)
	s.crossThrownPortalsLocked(&fx, gate)
	alive := s.thrown[:0]
	for _, it := range s.thrown {
		if !it.throwable.Dead && !gate.ticks(it.w, it.throwable.X, it.throwable.Z) {
			alive = append(alive, it)
			held = append(held, it)
		} else if !it.throwable.Dead && s.stepThrown(&fx, &in, it) {
			alive = append(alive, it)
		} else {
			if it.throwable.Dead {
				s.entityDestroyed(it.uuid)
			}
			thrownTracker.unfile(it)
		}
	}
	s.thrown = alive
	s.unlockTick(&s.mobsMu)
	s.unlockTick(&s.arrowsMu)

	fx.apply(s)
	s.resolveHitAsks(fx.hitAsks)
	s.fireBlockHits(fx.blockHits)
}

// thrownWatcher is one player, read before any lock the tick takes.
type thrownWatcher struct {
	c           *client
	pos         player.Position
	chunkRadius int32
	unhittable  bool
}

// thrownWorld is one world's players and, built under the locks when a
// flight there first steps, its bodies by entity ID and its mob and player
// targets: no step adds or removes a body, so one build serves the tick.
type thrownWorld struct {
	watchers []thrownWatcher
	nearby   *nearbyPlayers
	dim      *dimension
	bodies   arrowBodies
	frames   map[int32]entitySliver
	// targets is the players' targets; index is each one's entry in it, by
	// entity ID. A body's mobs are gathered per body.
	targets []entity.ArrowTarget
	index   map[int32]int
}

// thrownTickInputs is what the thrown tick reads before and as it steps.
type thrownTickInputs struct {
	projectileTargetSnapshots
	clients []*client
	worlds  map[*world.World]*thrownWorld
	// byUUID is built on a flight's first owner lookup.
	byUUID map[uuid.UUID]*client
	// The hulls, read under the boat lock the tick never holds.
	boatsByWorld map[*world.World][]entity.ArrowTarget
	boatsByID    map[int32]bool
	boatRoots    map[int32]int32
	// mobsNear is the stepping body's mobs.
	mobsNear []*entity.Mob
	// targets is the stepping body's, built in buf.
	targets []entity.ArrowTarget
	buf     []entity.ArrowTarget
}

// readThrownTickInputs reads the players, raises the pearls a returning
// player's file carried, then reads the hulls.
func (s *Server) readThrownTickInputs(in *thrownTickInputs) {
	s.mu.RLock()
	in.clients = make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		in.clients = append(in.clients, c)
	}
	s.mu.RUnlock()

	for _, c := range in.clients {
		if c.player == nil {
			continue
		}
		tw := in.world(c.theWorld())
		if tw.watchers == nil {
			tw.watchers = make([]thrownWatcher, 0, len(in.clients))
		}
		tw.watchers = append(tw.watchers, thrownWatcher{c: c, pos: c.player.Position(),
			chunkRadius: c.chunkRadius(), unhittable: projectileUnhittable(c)})
	}
	// A player behind the loading screen has theirs raised on a later tick.
	for _, c := range in.clients {
		saved := c.pearlsOnJoin.Swap(nil)
		if saved == nil {
			continue
		}
		if c.clientLoaded() {
			s.raisePlayerPearls(c, *saved)
		} else {
			c.pearlsOnJoin.Store(saved)
		}
	}
	in.boatsByWorld, in.boatsByID, in.boatRoots = s.boatSnapshots()
	s.lockTick(&s.arrowsMu)
	flying := len(s.thrown) > 0
	s.unlockTick(&s.arrowsMu)
	if flying {
		in.projectileTargetSnapshots = s.readProjectileTargetSnapshots()
	}
}

func (in *thrownTickInputs) world(w *world.World) *thrownWorld {
	tw := in.worlds[w]
	if tw == nil {
		tw = &thrownWorld{}
		in.worlds[w] = tw
	}
	return tw
}

// stepWorld is w's thrownWorld with its bodies by ID built, the mob lock held.
func (in *thrownTickInputs) stepWorld(s *Server, w *world.World) *thrownWorld {
	tw := in.world(w)
	if tw.dim != nil {
		return tw
	}
	tw.dim = s.dimensionOfWorld(w)
	tw.bodies = arrowBodies{mobs: make(map[int32]*entity.Mob), clients: make(map[int32]*client, len(tw.watchers))}
	tw.index = make(map[int32]int, len(tw.watchers))
	tw.targets = make([]entity.ArrowTarget, 0, len(tw.watchers))
	for _, w := range tw.watchers {
		target := w.c.projectileTarget(w.pos, w.unhittable)
		target.RootVehicle = in.boatRoots[w.c.player.EntityID]
		tw.index[w.c.player.EntityID] = len(tw.targets)
		tw.targets = append(tw.targets, target)
		tw.bodies.clients[w.c.player.EntityID] = w.c
	}
	tw.frames = make(map[int32]entitySliver, len(in.frames[w]))
	for _, f := range in.frames[w] {
		tw.frames[f.id] = f
	}
	return tw
}

// clientByUUID is the online player of the identity given, nil for none.
func (in *thrownTickInputs) clientByUUID(id uuid.UUID) *client {
	if in.byUUID == nil {
		in.byUUID = make(map[uuid.UUID]*client, len(in.clients))
		for _, c := range in.clients {
			if c.player != nil {
				in.byUUID[c.player.UUID] = c
			}
		}
	}
	return in.byUUID[id]
}

// thrownPortalTravel is one thrown body the portal pass is sending across.
type thrownPortalTravel struct {
	it   *thrownEntity
	kind rules.PortalKind
}

// crossThrownPortalsLocked takes the bodies a portal is sending across out
// of the list ahead of the tick, their arrival left to fx.
func (s *Server) crossThrownPortalsLocked(fx *thrownEffects, gate entityGate) {
	portalAlive := s.thrown[:0]
	for _, it := range s.thrown {
		if it.throwable.Dead {
			thrownTracker.unfile(it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.throwable.X, it.throwable.Y, it.throwable.Z, &it.portal, projectileCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.throwable.X, it.throwable.Y, it.throwable.Z, state)
		if !due {
			t := it.throwable
			if state == rules.EndGatewayState {
				gatewayTouchAt(&fx.gateways, it.w, t.X, t.Y, t.Z, gatewayTouch{
					mu: &s.arrowsMu, portal: &it.portal, cooldown: projectileCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveThrown(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			continue
		}
		thrownTracker.unfile(it)
		fx.travels = append(fx.travels, thrownPortalTravel{it: it, kind: kind})
	}
	s.thrown = portalAlive
}

// stepThrown runs one body's tick into fx, the arrow and mob locks held, and
// reports whether the body flies on.
func (s *Server) stepThrown(fx *thrownEffects, in *thrownTickInputs, it *thrownEntity) bool {
	t := it.throwable
	// A dead owner's pearl vanishes by the gamerule; an offline owner's
	// flies on. vanilla: ThrownEnderpearl.tick
	if t.Kind == entity.ThrownEnderPearl {
		if owner := in.clientByUUID(it.ownerUUID); owner != nil && owner.player.Vitals().Dead &&
			it.w.BoolRule(world.RuleEnderPearlsVanishOnDeath) {
			return fx.vanish(it)
		}
	}
	// vanilla: AbstractHurtingProjectile.tick
	if (t.Kind.IsWindCharge() || t.Kind.IsFireball()) &&
		!it.w.ChunkLoadedAt(int32(math.Floor(t.X)), int32(math.Floor(t.Z))) {
		return fx.vanish(it)
	}
	// vanilla: ShulkerBullet.checkDespawn
	if t.Kind == entity.ThrownShulkerBullet && it.w.Difficulty() == rules.DifficultyPeaceful {
		return fx.vanish(it)
	}
	tw := in.stepWorld(s, it.w)
	// No discard for a removed owner: vanilla's getOwner never returns one,
	// so AbstractHurtingProjectile.tick's owner.isRemoved() never holds.
	// vanilla: EntityReference.getEntity
	in.targets = nil

	// A pearl carries its owner to vanilla's oldPosition, not the hit point.
	fromX, fromY, fromZ := t.X, t.Y, t.Z
	it.queue(&fx.shots, it.w, t.X, t.Y, t.Z, it.ownerUUID)
	var before entity.Thrown
	var rollMark uint64
	asking := s.hitListening()
	if asking {
		before = *t
		rollMark = t.RandSrc.Mark()
	}
	raw := t.StepGather(it.w, func(*entity.Thrown) (*entity.ArrowTarget, []entity.ArrowTarget) {
		owner := s.gatherTargets(in, it, tw)
		return owner, in.targets
	})
	if asking {
		if ask, held := s.thrownHitAskOf(it, &tw.bodies, before.HitAllowed, raw); held {
			*t = before
			t.RandSrc.Rewind(rollMark)
			fx.hitAsks = append(fx.hitAsks, ask)
			return true
		}
		t.HitAllowed = 0
	}
	events := fx.takeBubbles(it.w, raw)
	fx.watchThrown(it, tw.watchers, tw.nearby)
	// vanilla: AbstractWindCharge.tick
	if t.Kind.IsWindCharge() && !t.Dead && t.Y > float64(it.w.MinY()+it.w.Height())+windChargeCullAbove {
		t.Dead = true
	}
	for _, ev := range events {
		switch ev.Kind {
		case entity.ThrownBreak:
			if ev.Target.EntityID != 0 {
				fx.bodyLandings = append(fx.bodyLandings, projectileLanding{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, ownerUUID: it.ownerUUID})
				continue
			}
			fx.blockLandings = append(fx.blockLandings, thrownLanding{it.w, ev.BlockHit, it.ownerUUID})
			if asking && !ev.BlockHit.Border {
				fx.blockHits = append(fx.blockHits, arrowBlockHit{id: it.uuid, w: it.w, hit: ev.BlockHit, x: ev.X, y: ev.Y, z: ev.Z})
			}
			if !ev.BlockHit.Border && isTargetBlock(ev.BlockHit.State) {
				fx.targetHits = append(fx.targetHits, targetBlockHit{w: it.w, block: ev.BlockHit, ownerID: t.OwnerID})
			}
			if !ev.BlockHit.Border && isProjectileHitBlock(ev.BlockHit.State) {
				ownerMode := int32(protocol.GameModeSurvival)
				if c := tw.bodies.clients[t.OwnerID]; c != nil {
					ownerMode = c.player.GameMode()
				}
				fx.projectileHits = append(fx.projectileHits, projectileBlockHit{w: it.w, block: ev.BlockHit, fire: t.Kind.Burns(),
					entityType: t.Kind.EntityType(), ownerMob: tw.bodies.mobs[t.OwnerID] != nil,
					ownerID: t.OwnerID, ownerUUID: it.ownerUUID, ownerMode: ownerMode})
			}
		case entity.ThrownHitTarget:
			s.thrownHitTarget(fx, in, tw, it, ev)
			fx.bodyLandings = append(fx.bodyLandings, projectileLanding{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, ownerUUID: it.ownerUUID})
		case entity.ThrownDeflected:
			if d, ok := deflectedBy(it.w, tw.bodies.mobs[ev.Target.EntityID]); ok {
				fx.deflects = append(fx.deflects, d)
			}
		case entity.ThrownSplashed:
			fx.splashes = append(fx.splashes, projectileSplash{w: it.w, x: ev.X, y: ev.Y, z: ev.Z,
				volume: ev.Volume, pitch: ev.Pitch, silent: t.Silent, owner: it.ownerUUID})
		}
	}
	if !t.Dead {
		// The pearl's chunk hold moves after the locks, where generation costs.
		if t.Kind == entity.ThrownEnderPearl {
			fx.pearlHolds = append(fx.pearlHolds, it)
		}
		return true
	}
	s.endThrown(fx, in, tw, it, events, fromX, fromY, fromZ)
	return false
}

// gatherTargets sets in.targets to what a flight may meet: the mobs near it, players, paintings, frames, hulls, cushions, stands, crystals,
// shulker bullets and redirectable bodies. owner is the thrower's body
// among them, which the first ticks cannot hit; nil when not there.
func (s *Server) gatherTargets(in *thrownTickInputs, it *thrownEntity, tw *thrownWorld) (owner *entity.ArrowTarget) {
	t := it.throwable
	extra := len(tw.targets) + len(in.paintings.byWorld[it.w]) + len(in.frames[it.w]) + len(in.boatsByWorld[it.w]) + len(in.cushions[it.w]) + len(in.stands[it.w])
	if t.Kind.IsPotion() {
		extra += len(in.mannequins.byWorld[it.w])
	}
	if !t.Kind.IsWindCharge() {
		extra += len(in.crystals[it.w])
	}
	targets, ownerAt := in.mobTargets(slices.Grow(in.buf[:0], extra), s, it, tw)
	if ownerAt < 0 {
		if i, ok := tw.index[t.OwnerID]; ok {
			ownerAt = len(targets) + i
		}
	}
	targets = append(targets, tw.targets...)
	targets = in.appendHangings(targets, it.w)
	targets = append(targets, in.boatsByWorld[it.w]...)
	targets = append(targets, in.cushions[it.w]...)
	targets = append(targets, in.stands[it.w]...)
	if !t.Kind.IsWindCharge() {
		targets = append(targets, in.crystals[it.w]...)
	}
	for _, p := range in.projectiles[it.w] {
		if t.CanHitProjectile(p) {
			targets = append(targets, p)
		}
	}
	if t.Kind.IsPotion() {
		targets = append(targets, in.mannequins.byWorld[it.w]...)
	}
	in.buf, in.targets = targets, targets
	if ownerAt >= 0 {
		owner = &targets[ownerAt]
	}
	return owner
}

// mobTargets appends to targets the mobs the body may meet or splash, sorted
// by entity ID so the nearest-hit tie-break is stable, with tw.bodies.mobs
// holding them. ownerAt is the thrower's first entry in the result, -1 when
// it has none. The thrower and a bullet's target are always among them. Mobs
// refile under mobsMu, which is held, so the chunk index is current.
func (in *thrownTickInputs) mobTargets(targets []entity.ArrowTarget, s *Server, it *thrownEntity, tw *thrownWorld) (_ []entity.ArrowTarget, ownerAt int) {
	if len(tw.dim.mobs.herd) == 0 {
		return targets, -1
	}
	t := it.throwable
	box := t.MoveBox()
	// A bullet's steering moves it beyond the box. A splash reaches 4 blocks
	// out from the hit. vanilla: ThrownPotion.onHit inflate(4.0, 2.0, 4.0)
	grow := 0.0
	if t.Kind == entity.ThrownShulkerBullet {
		grow = 1
	} else if t.Kind.IsPotion() || t.Kind == entity.ThrownDragonFireball {
		grow = 5
	}
	box[0], box[1], box[2], box[3], box[4], box[5] = box[0]-grow, box[1]-grow, box[2]-grow, box[3]+grow, box[4]+grow, box[5]+grow
	// A mob is filed by its centre and found within smallChunkArea's padding
	// of the box, however wide its own box is.
	// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
	area, indexed := smallChunkArea(&box)
	// Vertically the query runs over 16-block sections, padded 4 below and
	// 0 above.
	minSection, maxSection := math.Floor((box[1]-4)/16), math.Floor(box[4]/16)
	in.mobsNear = in.mobsNear[:0]
	tw.dim.mobs.mobsInArea(area, indexed, func(m *entity.Mob) bool {
		if section := math.Floor(math.Floor(m.Y) / 16); section < minSection || section > maxSection {
			return true
		}
		// vanilla: EntitySection.getEntities tests the entity's own box
		minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
		if maxX < box[0] || minX > box[3] || maxZ < box[2] || minZ > box[5] || maxY < box[1] || minY > box[4] {
			return true
		}
		in.mobsNear = append(in.mobsNear, m)
		return true
	})
	// The thrower, and the mob a bullet homes on, wherever they are.
	for _, id := range [...]int32{t.OwnerID, t.BulletTargetID()} {
		if ent := s.mobByEntityIDLocked(it.w, id); ent != nil && !slices.Contains(in.mobsNear, ent.mob) {
			in.mobsNear = append(in.mobsNear, ent.mob)
		}
	}
	// While the thrower is excluded, its vehicle group's mobs wherever they
	// are: the moved box must clear every one of them.
	// vanilla: Projectile.isOutsideOwnerCollisionRange
	if root := in.boatRoots[t.OwnerID]; root != 0 && t.OwnerID != 0 && !t.LeftOwner() {
		for id, r := range in.boatRoots {
			if ent := s.mobByEntityIDLocked(it.w, id); r == root && ent != nil && !slices.Contains(in.mobsNear, ent.mob) {
				in.mobsNear = append(in.mobsNear, ent.mob)
			}
		}
	}
	// A dragon's parts reach beyond the chunks around its feet.
	for _, m := range tw.dim.mobs.dragonBodies {
		if !slices.Contains(in.mobsNear, m) {
			in.mobsNear = append(in.mobsNear, m)
		}
	}
	slices.SortFunc(in.mobsNear, func(a, b *entity.Mob) int { return cmp.Compare(a.ID, b.ID) })
	ownerAt = -1
	base := len(targets)
	for _, m := range in.mobsNear {
		start := len(targets)
		if m.ID == t.OwnerID && ownerAt < 0 {
			ownerAt = start - base
		}
		targets = entity.MobProjectileTargets(targets, m, mobEntityType(m.Type), in.boatRoots[m.ID])
		for _, target := range targets[start:] {
			tw.bodies.mobs[target.EntityID] = m
		}
	}
	return targets, ownerAt
}

// watchThrown settles a stepped body's watchers and, while it flies, tells
// them the first pass the tick after the throw and the motion and place on
// its own flight's schedule.
func (fx *thrownEffects) watchThrown(it *thrownEntity, watchers []thrownWatcher, near *nearbyPlayers) {
	t := it.throwable
	gate := t.TickCount%t.Kind.UpdateInterval() == 0
	told := !t.Dead && (it.firstPass || gate)
	if gate {
		it.teleported = false // the gate's own Teleport Entity tells it
	}
	body := thrownTracker.body(it)
	from := len(fx.tellTo)
	for _, i := range body.watchers(near) {
		w := watchers[i]
		if w.c.theWorld() != it.w {
			continue // crossed to another dimension since the snapshot
		}
		if _, seen := body.settle(w.c, w.pos.X, w.pos.Z, w.chunkRadius, &fx.track); seen && told {
			fx.tellTo = append(fx.tellTo, w.c)
		}
	}
	if len(fx.tellTo) > from {
		tell := thrownTell{from: from, to: len(fx.tellTo)}
		if it.firstPass {
			// The tracked entity's first pass: no distance, metadata again.
			tell.data[0], tell.data[1] = thrownZeroDeltaPayload(it), thrownMetadataPayload(it, false)
		}
		if gate {
			tell.data[2], tell.data[3] = entityVelocityPayload(t.ID, t.VX, t.VY, t.VZ), thrownTeleportPayload(t)
			if t.Kind.IsFireball() || t.Kind.IsWindCharge() {
				tell.power = projectilePowerPayload(t.ID, t.ProjectilePower())
			}
		}
		fx.tells = append(fx.tells, tell)
	}
	it.firstPass = false
}

// thrownHitTarget sorts a body a flight met into its hurt: a mob's runs here
// under the mob lock, every other's after the locks. Player.hurtServer
// refuses a 0-damage blow, so an egg or snowball only breaks on a player.
// vanilla: onHitEntity
func (s *Server) thrownHitTarget(fx *thrownEffects, in *thrownTickInputs, tw *thrownWorld, it *thrownEntity, ev entity.ThrownEvent) {
	t := it.throwable
	// The body turns, then refuses the hurt that follows.
	// vanilla: Projectile.onHit, AbstractHurtingProjectile.hurtServer
	if registry.EntityType(ev.Target.Type).InTag(registry.EntityTypeTagIDRedirectableProjectile) {
		fx.redirects = append(fx.redirects, s.redirectByHitLocked(it.w, ev.Target.EntityID, &tw.bodies, t.OwnerID, it.ownerUUID))
		return
	}
	if t.Kind == entity.ThrownDragonFireball {
		return
	}
	mobs, clients := tw.bodies.mobs, tw.bodies.clients
	ownerMob := mobs[t.OwnerID] != nil
	switch target := ev.Target; {
	case mobs[target.EntityID] != nil:
		s.thrownHurtMob(fx, tw, it, mobs[target.EntityID], ev)
	case clients[target.EntityID] != nil && t.Kind.IsFireball():
		fx.fireballs.playerHits = append(fx.fireballs.playerHits, fireballPlayerHit{
			victim: clients[target.EntityID], kind: t.Kind, ownerID: t.OwnerID,
			ownerLiving: ownerMob || clients[t.OwnerID] != nil, ownerMob: ownerMob,
			id: t.ID, uuid: it.uuid, custom: it.custom, x: ev.X, y: ev.Y, z: ev.Z, vx: t.VX, vz: t.VZ, heal: skullHealOf(tw, it),
		})
	case clients[target.EntityID] != nil && t.Kind == entity.ThrownLlamaSpit:
		fx.fireballs.spitHits = append(fx.fireballs.spitHits, bulletPlayerHit{
			victim: clients[target.EntityID], ownerID: t.OwnerID, ownerMob: ownerMob,
			id: t.ID, uuid: it.uuid, custom: it.custom, x: ev.X, y: ev.Y, z: ev.Z, vx: t.VX, vz: t.VZ,
		})
	case clients[target.EntityID] != nil && t.Kind == entity.ThrownShulkerBullet:
		fx.fireballs.bulletHits = append(fx.fireballs.bulletHits, bulletPlayerHit{
			victim: clients[target.EntityID], ownerID: t.OwnerID, ownerMob: ownerMob,
			id: t.ID, uuid: it.uuid, custom: it.custom, x: ev.X, y: ev.Y, z: ev.Z, vx: t.VX, vz: t.VZ,
		})
	case clients[target.EntityID] != nil && t.Kind.IsWindCharge():
		fx.ownerHurts = markOwnerHurt(fx.ownerHurts, clients, t.OwnerID, target.EntityID)
		fx.windPlayerHits = append(fx.windPlayerHits, windPlayerHit{
			victim: clients[target.EntityID], ownerID: t.OwnerID, ownerMob: ownerMob,
			id: t.ID, uuid: it.uuid, custom: it.custom, kind: t.Kind, x: ev.X, y: ev.Y, z: ev.Z, damage: ev.Damage, vx: t.VX, vz: t.VZ,
		})
	case target.Type == protocol.EntityTypeShulkerBullet:
		// Any hurt destroys a bullet, a snowball's none included.
		// vanilla: ShulkerBullet.hurtServer
		if b := s.bulletByIDLocked(it.w, target.EntityID); b != nil && b != it {
			b.throwable.Dead = true
			fx.fireballs.strikes = append(fx.fireballs.strikes, bulletStrike{w: it.w, id: b.throwable.ID,
				x: b.throwable.X, y: b.throwable.Y, z: b.throwable.Z})
		}
	case target.Type == protocol.EntityTypeEndCrystal:
		// vanilla: EndCrystal.hurtServer
		fx.crystalHits = append(fx.crystalHits, crystalProjectileHit{w: it.w, crystalID: target.EntityID,
			ownerID: t.OwnerID, ownerUUID: it.ownerUUID})
	case target.Type == protocol.EntityTypeCushion:
		// vanilla: Cushion.hurtServer
		fx.cushionHits = append(fx.cushionHits, cushionProjectileHit{w: it.w, id: target.EntityID,
			shooter: clients[t.OwnerID], mob: ownerMob})
	case target.Type == protocol.EntityTypeArmorStand:
		// Only an always_kills_armor_stands hurt breaks the stand; every
		// other body just ends its flight on it.
		// vanilla: ArmorStand.hurtServer, DamageSources.fireball, WitherSkull.onHitEntity
		var damageType registry.DamageType
		switch {
		case t.Kind.IsFireball(): // the dragon's returned above
			_, damageType = entity.FireballDamage(t.Kind, t.OwnerID != 0, ownerMob || clients[t.OwnerID] != nil)
		case t.Kind.IsWindCharge():
			damageType = rules.DamageTypeWindCharge
		}
		if !damageType.InTag(registry.DamageTypeTagIDAlwaysKillsArmorStands) {
			break
		}
		owner := clients[t.OwnerID]
		hit := thrownStandHit{w: it.w, standID: target.EntityID, damageType: damageType,
			dropless: creativeClient(owner), mob: ownerMob,
			noBuild: owner != nil && !rules.ModeMayBuild(owner.player.GameMode())}
		// Only a living owner makes the hurt wither_skull.
		if damageType == rules.DamageTypeWitherSkull {
			hit.heal = skullHealOf(tw, it)
		}
		fx.standHits = append(fx.standHits, hit)
	case in.paintings.byIDWorld[it.w][target.EntityID]:
		// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
		fx.paintingHits = append(fx.paintingHits, paintingHit{w: it.w, paintingID: target.EntityID,
			dropless: creativeClient(clients[t.OwnerID]), mob: ownerMob})
	case tw.frames[target.EntityID].id != 0:
		// A fixed frame refuses all but a creative thrower; the body breaks
		// on it either way. vanilla: ItemFrame.canHurtWhenFixed
		dropless := creativeClient(clients[t.OwnerID])
		if tw.frames[target.EntityID].fixed && !dropless {
			break
		}
		fx.frameHits = append(fx.frameHits, arrowFrameHit{w: it.w, frameID: target.EntityID, dropless: dropless, mob: ownerMob})
	case in.boatsByID[target.EntityID]:
		// A broken hull's removal and drop are the boat tick's next pass.
		// vanilla: VehicleEntity.hurtServer
		fx.boatHits = append(fx.boatHits, boatProjectileBlow{w: it.w, boatID: target.EntityID, damage: ev.Damage,
			dropless: creativeClient(clients[t.OwnerID])})
	}
}

// thrownHurtMob runs a thrown blow's hurt on a mob under the mob lock.
func (s *Server) thrownHurtMob(fx *thrownEffects, tw *thrownWorld, it *thrownEntity, m *entity.Mob, ev entity.ThrownEvent) {
	if m.Type == entity.TypeEnderDragon {
		m.AimDragonPart(int(ev.Target.EntityID - m.ID - 1))
		defer m.AimDragonPart(entity.DragonBody)
	}
	t := it.throwable
	clients := tw.bodies.clients
	player, creative := shooterIsPlayer(t.OwnerID, clients), shooterCreative(t.OwnerID, clients)
	from := shooterHitSource(t.OwnerID, &tw.bodies, m)
	damageType := rules.DamageTypeThrown
	var landed, softened bool
	oldVX, oldVY, oldVZ, oldImpulse := m.VX, m.VY, m.VZ, m.Impulse
	switch {
	case t.Kind.IsFireball():
		landed, softened, damageType = s.fireballHitMob(&fx.fireballs, tw.dim, it, m, player, creative,
			tw.bodies.mobs[t.OwnerID] != nil || clients[t.OwnerID] != nil, from)
	case t.Kind == entity.ThrownLlamaSpit:
		damageType = rules.DamageTypeSpit
		landed, softened = m.HurtByProjectile(entity.LlamaSpitDamage, damageType, t.VX, t.VZ, t.OwnerID, player, creative, from)
	case t.Kind == entity.ThrownShulkerBullet:
		damageType = rules.DamageTypeMobProjectile
		landed, softened = s.bulletHitMob(&fx.fireballs, tw.dim, it, m, player, creative, from)
	case t.Kind.IsWindCharge():
		// vanilla: AbstractWindCharge.onHitEntity, before the hurt
		fx.ownerHurts = markOwnerHurt(fx.ownerHurts, clients, t.OwnerID, m.ID)
		damageType = rules.DamageTypeWindCharge
		landed, softened = m.HurtByWindCharge(ev.Damage, t.VX, t.VZ, t.OwnerID, player, creative, from)
	default:
		landed, softened = m.HurtByThrown(ev.Damage, t.VX, t.VZ, t.OwnerID, player, creative, from)
	}
	if !landed {
		return
	}
	// vanilla: WitherSkull.onHitEntity heals the owner when the blow kills
	if t.Kind == entity.ThrownWitherSkull && !m.Alive() {
		fx.skullHeals = append(fx.skullHeals, skullHealOf(tw, it))
	}
	hit := thrownMobHitOf(tw.dim, it, m, damageType, softened, ev.Damage > 0)
	var pusher plugin.Entity = pluginEntity{host: s.pluginAdapter, id: it.uuid}
	if t.Kind.IsWindCharge() {
		// the charge bursts in this same tick, so it is gone when the event fires
		pusher = pluginRemoved{host: s.pluginAdapter, id: it.uuid, typ: registry.EntityType(t.Kind.EntityType()),
			at: plugin.Location{World: pluginWorld{srv: s, dim: tw.dim, host: s.pluginAdapter}, Pos: plugin.Vec3{X: t.X, Y: t.Y, Z: t.Z}}}
	}
	hit.kb = s.holdMobKnockbackBy(tw.dim.mobs.book[m], pusher, oldVX, oldVY, oldVZ, oldImpulse)
	fx.mobHits = append(fx.mobHits, hit)
}

// thrownMobHitOf is what a landed blow of the body on the mob tells. The
// health rides the telling, so the mobs' metadata pass does not repeat it.
func thrownMobHitOf(dim *dimension, it *thrownEntity, m *entity.Mob, damageType registry.DamageType, softened, damaged bool) thrownMobHit {
	return bodyMobHitOf(dim, it.w, it.throwable.OwnerID, it.throwable.ID, m, damageType, softened, damaged)
}

// bodyMobHitOf is thrownMobHitOf for any body (a thrown one or a cloud)
// with its owner's and its own entity ID.
func bodyMobHitOf(dim *dimension, w *world.World, ownerID, bodyID int32, m *entity.Mob, damageType registry.DamageType, softened, damaged bool) thrownMobHit {
	causeID := causeEntityID(&damageCause{entityID: ownerID})
	if ownerID == 0 {
		causeID = 0
	}
	hit := thrownMobHit{
		damageType: damageType, w: w, victimID: m.ID, kind: m.Type,
		sound: mobSound(m, true), volume: m.SoundVolume(), pitch: m.VoicePitch(), silent: m.Silent,
		causeID:  causeID,
		directID: causeEntityID(&damageCause{entityID: bodyID}),
		x:        m.X, y: m.Y, z: m.Z, vx: m.VX, vy: m.VY, vz: m.VZ,
		softened: softened,
	}
	if ent := dim.mobs.book[m]; ent != nil {
		hit.watchers = trackedWatchers(ent.tracked)
		if damaged {
			hit.health = ent.syncHealth()
		}
	}
	return hit
}

// endThrown collects the end of a body the step finished, by kind; events
// is the step's.
func (s *Server) endThrown(fx *thrownEffects, in *thrownTickInputs, tw *thrownWorld, it *thrownEntity,
	events []entity.ThrownEvent, fromX, fromY, fromZ float64) {
	t := it.throwable
	switch t.Kind {
	case entity.ThrownEnderPearl:
		// No break event: vanilla's pearl puffs client-side.
		land := pearlLanding{w: it.w, id: t.ID, it: it, x: fromX, y: fromY, z: fromZ,
			owner: in.clientByUUID(it.ownerUUID), endermite: t.Rand.Float32() < enderPearlEndermiteChance}
		// Only its watchers: one out of range was already told the removal.
		for _, watch := range tw.watchers {
			if _, told := it.tracked.get(watch.c); told {
				land.seenBy = append(land.seenBy, watch.c)
			}
		}
		fx.landings = append(fx.landings, land)
	case entity.ThrownDragonFireball, entity.ThrownSmallFireball, entity.ThrownLargeFireball, entity.ThrownWitherSkull, entity.ThrownShulkerBullet, entity.ThrownLlamaSpit:
		land := fireballLanding{tags: it.tags.Values(), silent: t.Silent, w: it.w, id: t.ID, kind: t.Kind,
			ownerID: t.OwnerID, ownerUUID: it.ownerUUID, uuid: it.uuid, custom: it.custom, ownerMob: tw.bodies.mobs[t.OwnerID] != nil,
			ownerPlayer: shooterIsPlayer(t.OwnerID, tw.bodies.clients),
			power:       t.ExplosionPower, dangerous: t.Dangerous, rng: t.Rand, x: t.X, y: t.Y, z: t.Z}
		for _, ev := range events {
			if ev.Kind == entity.ThrownBreak {
				land.block, land.onBlock = ev.BlockHit, true
			}
		}
		if t.Kind == entity.ThrownDragonFireball {
			land.cloudX, land.cloudY, land.cloudZ = t.X, t.Y, t.Z
			for _, target := range in.targets {
				if tw.bodies.mobs[target.EntityID] == nil && tw.bodies.clients[target.EntityID] == nil {
					continue
				}
				x, y, z := (target.MinX+target.MaxX)/2, target.MinY, (target.MinZ+target.MaxZ)/2
				if m := tw.bodies.mobs[target.EntityID]; m != nil {
					x, y, z = m.X, m.Y, m.Z
				}
				if target.MaxX <= t.X-4.5 || target.MinX >= t.X+4.5 || target.MaxY <= t.Y-2 || target.MinY >= t.Y+3 || target.MaxZ <= t.Z-4.5 || target.MinZ >= t.Z+4.5 {
					continue
				}
				if (x-t.X)*(x-t.X)+(y-t.Y)*(y-t.Y)+(z-t.Z)*(z-t.Z) < 16 {
					land.cloudX, land.cloudY, land.cloudZ = x, y, z
					break
				}
			}
		}
		fx.fireballs.landings = append(fx.fireballs.landings, land)
	case entity.ThrownWindCharge, entity.ThrownBreezeWindCharge:
		// vanilla: AbstractWindCharge.onHitBlock
		burst := windBurst{w: it.w, id: t.ID, uuid: it.uuid, custom: it.custom, ownerID: t.OwnerID, ownerUUID: it.ownerUUID, rng: t.Rand, x: t.X, y: t.Y, z: t.Z}
		burst.radius = windChargeRadius
		if t.Kind == entity.ThrownBreezeWindCharge {
			burst.radius, burst.breeze = 3, true
		}
		for _, ev := range events {
			burst.x, burst.y, burst.z = ev.X, ev.Y, ev.Z
			if ev.Kind == entity.ThrownBreak {
				dx, dy, dz := faceStep(ev.BlockHit.Face)
				burst.x += float64(dx) * windChargeFaceOffset
				burst.y += float64(dy) * windChargeFaceOffset
				burst.z += float64(dz) * windChargeFaceOffset
			}
		}
		fx.windBursts = append(fx.windBursts, burst)
	default:
		br := s.thrownBreakOf(in, tw, it, events)
		fx.breaks = append(fx.breaks, br)
	}
}

// thrownBreakOf is any other body's break; a potion's spray is read here,
// with the bodies it reaches at hand.
func (s *Server) thrownBreakOf(in *thrownTickInputs, tw *thrownWorld, it *thrownEntity, events []entity.ThrownEvent) thrownBreak {
	t := it.throwable
	br := thrownBreak{w: it.w, id: t.ID, uuid: it.uuid, kind: t.Kind, x: t.X, y: t.Y, z: t.Z, variant: it.variant, yaw: t.Yaw, silent: t.Silent, owner: it.ownerUUID}
	var struck entity.ArrowTarget
	for _, ev := range events {
		br.x, br.y, br.z = ev.X, ev.Y, ev.Z
		struck = ev.Target
		br.chicks = ev.Chicks
		if ev.Kind == entity.ThrownBreak {
			br.blockHit, br.onBlock = ev.BlockHit, ev.Target.EntityID == 0
		}
	}
	if br.kind.IsPotion() {
		br.potion = s.potionBreakOf(it, tw.dim, tw.bodies.mobs, tw.bodies.clients, in.targets, in.mannequins.byID)
		shl := br.potion
		shl.cloudX, shl.cloudY, shl.cloudZ = br.x, br.y, br.z
		// vanilla: ThrownLingeringPotion.onHitAsPotion
		if br.kind == entity.ThrownLingeringPotion && struck.EntityID != 0 {
			shl.cloudX, shl.cloudY, shl.cloudZ = (struck.MinX+struck.MaxX)/2, struck.MinY, (struck.MinZ+struck.MaxZ)/2
			if m := tw.bodies.mobs[struck.EntityID]; m != nil {
				shl.cloudX, shl.cloudY, shl.cloudZ = m.X, m.Y, m.Z
			} else if c := tw.bodies.clients[struck.EntityID]; c != nil {
				pos := c.player.Position()
				shl.cloudX, shl.cloudY, shl.cloudZ = pos.X, pos.Y, pos.Z
			}
		}
	}
	// Orbs leave along the face's normal, or against the flight off a body.
	// vanilla: ThrownExperienceBottle.onHit
	if br.kind == entity.ThrownExperienceBottle {
		br.xp = 3 + t.Rand.Intn(5) + t.Rand.Intn(5)
		br.dirX, br.dirY, br.dirZ = -t.VX, -t.VY, -t.VZ
		if br.onBlock {
			dx, dy, dz := faceStep(br.blockHit.Face)
			br.dirX, br.dirY, br.dirZ = float64(dx), float64(dy), float64(dz)
		}
	}
	return br
}
