package server

import (
	"log/slog"
	"maps"

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

// The ender pearl beyond the shared throw (throwables.go): a flying pearl
// holds its chunks with a timed ticket, standing in for vanilla's TicketType.ender_pearl;
// the landing teleports the owner with 5.0 ender_pearl damage and resets
// their fall, the only wire sign of a break (vanilla's puff is
// client-side); and a pearl in flight at logout is saved in the player's
// ender_pearls list (its entity tag plus dimension) and respawned when the
// player returns.
// vanilla: ThrownEnderpearl.onHit, ServerPlayer.saveEnderPearls,
// PlayerList.remove

// enderPearlEndermiteChance is the odds a landing spawns an endermite.
// vanilla: ThrownEnderpearl.onHit
const enderPearlEndermiteChance = 0.05

// enderPearlLandingHurt is the damage a landing deals the owner.
// vanilla: ThrownEnderpearl.onHit's hurtServer
const enderPearlLandingHurt = 5.0

// pearlTicketRadius and pearlTicketTimeout are the ender_pearl ticket's
// reach (level 31: entity-ticking its chunk, block-ticking within one,
// loaded within two) and its life in ticks past each renewal.
// vanilla: ServerPlayer.placeEnderPearlTicket, TicketType.ENDER_PEARL
const (
	pearlTicketRadius  = 2
	pearlTicketTimeout = 40
)

// followPearlHold places the ender_pearl ticket at the pearl's chunk when it
// has changed chunk or the last placement is about to run out; the old
// ticket times out on its own. Only a pearl a player threw holds chunks.
// vanilla: ThrownEnderpearl.tick, ServerPlayer.registerAndUpdateEnderPearlTicket
func (s *Server) followPearlHold(it *thrownEntity) {
	if s.clientByUUID(it.ownerUUID) == nil {
		return
	}
	pos := world.ChunkPos{X: world.ChunkOf(it.throwable.X), Z: world.ChunkOf(it.throwable.Z)}
	now, _ := it.w.Time()
	if it.holding && it.holdWorld == it.w && it.holdPos == pos && now < it.holdRenew {
		return
	}
	it.holdWorld, it.holdPos, it.holding, it.holdRenew = it.w, pos, true, now+pearlTicketTimeout-1
	it.w.AddTimedTicket(pos, pearlTicketRadius, now+pearlTicketTimeout)
}

// pearlLanding is a pearl's landing, carried out of the locks: world, body,
// its start-of-tick position, the owner (nil if disconnected) and the
// players still tracking the pearl, who are told of its break.
type pearlLanding struct {
	w       *world.World
	id      int32
	it      *thrownEntity
	x, y, z float64
	owner   *client
	seenBy  []*client
	// endermite is the landing's 5% roll (ThrownEnderpearl.onHit), from the
	// pearl's own stream under the tick's locks.
	endermite bool
}

// landPearl lands a pearl: the owner teleports to its start-of-tick
// position keeping their look, takes 5.0 ender_pearl damage, has their fall
// reset and hears the teleport; then the pearl is removed with no entity
// event (vanilla's break is drawn client-side). A dead owner is not carried,
// nor a sleeping one in the pearl's dimension (isAllowedToTeleportOwner);
// an owner in another dimension crosses if alive. The teleport zeroes the
// owner's motion where vanilla's delta-relative TeleportTransition keeps
// it, a difference only the client's own velocity would show.
// vanilla: ThrownEnderpearl.onHit
func (s *Server) landPearl(land pearlLanding) {
	owner := land.owner
	if owner != nil && !pearlMayCarryOwner(owner, land.w) {
		owner = nil
	}
	if owner != nil {
		pos := owner.player.Position()
		s.spawnPearlEndermite(land, owner, pos)
		pos.X, pos.Y, pos.Z = land.x, land.y, land.z
		if owner.theWorld() == land.w {
			pearlCoolOwner(land, owner)
			owner.leaveVehicleForTeleport()
			owner.teleport(command.Move{Pos: pos})
			// The teleport is told to everyone watching at once, as /teleport does.
			owner.tp.mu.Lock()
			owner.sendMovement(owner.player.Position())
			owner.tp.mu.Unlock()
			s.pearlLandingTelling(land, owner)
		} else {
			// The crossing writes to the owner's connection, so it leaves the tick for
			// its own goroutine like every dimension change, with its hurt, fall reset
			// and sound.
			go s.carryPearlOwner(land, owner, pos)
		}
	}
	for _, c := range land.seenBy {
		c.send(protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{land.id}))
	}
}

// spawnPearlEndermite spawns a landing's endermite at the owner's position
// and look before they are carried. Peaceful, spawn_mobs off and
// spawn_monsters off refuse it. vanilla: ThrownEnderpearl.onHit
func (s *Server) spawnPearlEndermite(land pearlLanding, owner *client, pos player.Position) {
	if !land.endermite || !spawningMonsters(land.w) ||
		land.w.Difficulty() == rules.DifficultyPeaceful {
		return
	}
	s.spawnMobArriving(s.dimensionOfWorld(land.w), entity.TypeEndermite, pos.X, pos.Y, pos.Z, 0, nil,
		false, "", false, func(e *mobEntity) { e.mob.Yaw, e.mob.Pitch = pos.Yaw, pos.Pitch })
}

// carryPearlOwner moves an owner to the pearl's dimension and applies the
// landing there, off the tick.
func (s *Server) carryPearlOwner(land pearlLanding, owner *client, pos player.Position) {
	if hook := s.carryPearlHook; hook != nil {
		hook()
	}
	// The owner may die between the tick's check and this goroutine.
	dim := s.dimensionOfWorld(land.w)
	if !pearlMayCarryOwner(owner, land.w) || dim == owner.dimension() {
		return
	}
	// The cooldown is taken under the traveling flag, which holds it whole
	// until the arrival: a portal beside the landing then cannot take the
	// owner back on the crossing's first ticks.
	if !owner.traveling.CompareAndSwap(false, true) {
		return
	}
	defer owner.traveling.Store(false)
	pearlCoolOwner(land, owner)
	if err := owner.enterDimensionHeld(dim, command.Move{Pos: pos}, respawnNone, false); err != nil {
		slog.Warn("an ender pearl could not carry its owner across",
			"owner", owner.player.Name, "error", err)
		return
	}
	s.pearlLandingTelling(land, owner)
}

// pearlLandingTelling applies a landing to an owner in the pearl's
// dimension: fall reset, hurt and teleport sound. It runs on the tick for a
// same-dimension landing, on the crossing goroutine otherwise.
func (s *Server) pearlLandingTelling(land pearlLanding, owner *client) {
	if owner.theWorld() != land.w {
		return
	}
	owner.player.UpdateVitals(func(v *player.Vitals) {
		v.FallDistance = 0
		rules.ResetImpulse(v)
	})
	owner.hurt(enderPearlLandingHurt, rules.DamageTypeEnderPearl, nil)
	// vanilla: ThrownEnderpearl.playSound
	s.broadcastSoundID(land.w, registry.SoundEntityPlayerTeleport, protocol.SoundCategoryPlayer,
		land.x, land.y, land.z, 1, 1)
}

// pearlCoolOwner gives the owner a portal cooldown if the pearl is still on
// its own, before the teleport, so a portal beside the landing does not take
// them. vanilla: ThrownEnderpearl.onHit
func pearlCoolOwner(land pearlLanding, owner *client) {
	if land.it.portal.Cooldown > 0 {
		owner.portalCooldown.Store(playerPortalCooldownTicks)
	}
}

// pearlMayCarryOwner reports whether a landing may carry the owner: alive
// and awake for a same-dimension landing, alive for a crossing (what
// canUsePortal asks).
// vanilla: ThrownEnderpearl.isAllowedToTeleportOwner
func pearlMayCarryOwner(owner *client, w *world.World) bool {
	v := owner.player.Vitals()
	if owner.theWorld() == w {
		return !v.Dead && !owner.player.Sleep().Lying
	}
	return !v.Dead
}

// savedPearlTags is the player's ender_pearls list: each flying pearl's
// entity tag with its dimension. Only pearls are kept (eggs and snowballs
// just break), each with its own world, not the thrower's, so a pearl that
// crossed stays crossed on return.
// vanilla: ServerPlayer.saveEnderPearls
func (s *Server) savedPearlTags(owner uuid.UUID) []nbt.Tag {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	var pearls []nbt.Tag
	for _, it := range s.thrown {
		if it.throwable.Dead || it.throwable.Kind != entity.ThrownEnderPearl ||
			it.ownerUUID != owner {
			continue
		}
		pearls = append(pearls, s.pearlSavedTag(it))
	}
	return pearls
}

// pearlSavedTag is a pearl's entity tag plus its dimension, as saved per
// pearl. vanilla: ServerPlayer.saveEnderPearls
func (s *Server) pearlSavedTag(it *thrownEntity) *nbt.OrderedCompound {
	tag := thrownTag(it)
	tag.Set("ender_pearl_dimension", nbt.String(s.dimensionOfWorld(it.w).name))
	return tag
}

// pearlEntityID is the "id" field of a saved pearl's entity tag.
const pearlEntityID = "minecraft:ender_pearl"

// takePearlsForLeave removes the leaving player's pearls, returning the
// tags to save, and tells their watchers. Removing them before the save
// closes the window where a landing and the save could both carry the same
// pearl into the next session.
// vanilla: PlayerList.remove
func (s *Server) takePearlsForLeave(c *client) []nbt.Tag {
	s.arrowsMu.Lock()
	taken := s.thrown[:0]
	var leaving []*thrownEntity
	var saved []nbt.Tag
	for _, it := range s.thrown {
		if !it.throwable.Dead && it.throwable.Kind == entity.ThrownEnderPearl &&
			it.ownerUUID == c.player.UUID {
			leaving = append(leaving, it)
			saved = append(saved, s.pearlSavedTag(it))
			thrownTracker.unfile(it)
			continue
		}
		taken = append(taken, it)
	}
	s.thrown = taken
	s.arrowsMu.Unlock()

	// One removal per watcher still tracking the pearl (the seenBy a landing
	// tells), holds released alongside.
	takenFrom := make(map[*client][]int32)
	for _, it := range leaving {
		for id := range it.tracked.all() {
			if watcher := s.clientByUUID(id); watcher != nil {
				takenFrom[watcher] = append(takenFrom[watcher], it.throwable.ID)
			}
		}
	}
	for watcher, ids := range takenFrom {
		watcher.send(protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	return saved
}

// raisePlayerPearls respawns a returning player's saved pearls in their
// dimensions to finish their flights; one that is not a pearl, names an
// unknown dimension or lies outside the world is dropped with a log.
// vanilla: ServerPlayer.loadAndSpawnEnderPearls
func (s *Server) raisePlayerPearls(c *client, savedPearls []nbt.Compound) {
	for _, compound := range savedPearls {
		name, _ := nbt.Get[nbt.String](compound, "id")
		if name != pearlEntityID {
			slog.Warn("a saved ender pearl is no ender pearl, skipping",
				"id", string(name))
			continue
		}
		dimName, ok := nbt.Get[nbt.String](compound, "ender_pearl_dimension")
		if !ok {
			slog.Warn("a saved ender pearl names no dimension, skipping")
			continue
		}
		dim := s.dimensionByName(string(dimName))
		if dim.name != string(dimName) {
			slog.Warn("a saved ender pearl's dimension is not the server's, skipping",
				"dimension", string(dimName))
			continue
		}
		it := s.pearlOfSavedTag(c, dim.w, compound)
		if it == nil {
			continue
		}
		// A chunk file may hold the same pearl; whichever loads second is refused.
		if s.summonDuplicate(it.uuid) {
			slog.Warn("not loading a saved ender pearl whose UUID is already in the world", "uuid", it.uuid)
			continue
		}
		s.registerThrown(it)
	}
}

// pearlOfSavedTag builds a thrown body from a saved pearl tag, nil without
// a place or identity. The flight resumes at its saved place and speed,
// rolls from a fresh seed as for a loaded mob, the thrower exclusion as
// saved, and the owner taken from the returning player's new entity.
// vanilla: ThrowableItemProjectile.readAdditionalSaveData, Projectile.readAdditionalSaveData
func (s *Server) pearlOfSavedTag(c *client, w *world.World, compound nbt.Compound) *thrownEntity {
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		slog.Warn("a saved ender pearl has no place in it, skipping")
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		slog.Warn("a saved ender pearl has no identity in it, skipping")
		return nil
	}
	pearlItem, hasPearl := registry.ItemID(pearlEntityID)
	if !hasPearl {
		return nil
	}
	vx, vy, vz := entityMotionOfTag(compound)
	eid := s.nextEntity()
	t := entity.NewThrown(eid, entity.ThrownEnderPearl, x, y, z, vx, vy, vz,
		c.player.EntityID, s.entitySeed(eid))
	t.RestoreLeftOwner(nbt.BoolField(compound, "LeftOwner"))
	it := &thrownEntity{
		throwable: t,
		w:         w,
		uuid:      identity,
		stack:     protocol.ItemStack{ItemID: pearlItem, Count: 1},
		firstPass: true,
		ownerUUID: c.player.UUID,
		portal:    rules.EntityPortal{Cooldown: portalCooldownOf(compound)},
	}
	applyThrownRest(it, compound)
	return it
}

// storedPearlTags reads a player file's ender_pearls list, nil without one.
func storedPearlTags(root nbt.Compound) []nbt.Compound {
	list, ok := nbt.Get[nbt.List](root, "ender_pearls")
	if !ok {
		return nil
	}
	var pearls []nbt.Compound
	for _, item := range list.Items {
		if compound, ok := item.(nbt.Compound); ok {
			saved := maps.Clone(compound)
			pearls = append(pearls, saved)
		}
	}
	return pearls
}
