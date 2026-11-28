package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Chunk entity files for the non-mob kinds (items, arrows, orbs, primed TNT,
// falling blocks, paintings, frames, boats, minecarts, thrown bodies,
// clouds, rockets, eyes of ender), following the mobs: one wake per chunk
// per load under the mob lock, the file written whole from the live and
// carried entities, a written entity leaving with its chunk. Each kind's
// list is taken under its own lock alone, never above the mob lock.

// departingKind is which of the kinds a departure holds.
type departingKind uint8

const (
	departingItem departingKind = iota
	departingArrow
	departingOrb
	departingTNT
	departingFalling
	departingPainting
	departingFrame
	departingBoat
	departingMinecart
	departingArmorStand
	departingEndCrystal
	departingMannequin
	departingFixture
	departingThrown
	departingCloud
	departingFirework
	departingEye
)

// departure is a live entity its unloading chunk releases, with its save
// tag; exactly one kind field is set.
type departure struct {
	kind departingKind
	w    *world.World
	pos  world.ChunkPos
	tag  nbt.CompoundTag
	// boxed means the tag went into the unload write; only a boxed departure
	// whose chunk was written has left the world.
	boxed bool
	// riders are the mobs a departing boat or cart stepped off, nested under
	// its tag when they share its chunk's write.
	riders []*mobEntity

	item       *itemEntity
	arrow      *arrowEntity
	orb        *orbEntity
	tnt        *tntEntity
	falling    *fallingEntity
	painting   *paintingEntity
	frame      *frameEntity
	boat       *boatEntity
	minecart   *minecartEntity
	armorStand *armorStandEntity
	endCrystal *endCrystalEntity
	mannequin  *mannequinEntity
	fixture    *fixtureEntity
	thrown     *thrownEntity
	cloud      *cloudEntity
	firework   *fireworkEntity
	eye        *eyeEntity
}

// id is the entity ID for the removal packet.
func (d departure) id() int32 {
	switch d.kind {
	case departingItem:
		return d.item.item.ID
	case departingArrow:
		return d.arrow.arrow.ID
	case departingOrb:
		return d.orb.orb.ID
	case departingTNT:
		return d.tnt.tnt.ID
	case departingFalling:
		return d.falling.fb.ID
	case departingPainting:
		return d.painting.id
	case departingFrame:
		return d.frame.id
	case departingBoat:
		return d.boat.boat.ID
	case departingMinecart:
		return d.minecart.cart.ID
	case departingEndCrystal:
		return d.endCrystal.crystal.ID
	case departingMannequin:
		return d.mannequin.mannequin.ID
	case departingFixture:
		return d.fixture.id
	case departingThrown:
		return d.thrown.throwable.ID
	case departingCloud:
		return d.cloud.cloud.ID
	case departingFirework:
		return d.firework.rocket.ID
	case departingEye:
		return d.eye.eye.ID
	default:
		return d.armorStand.stand.ID
	}
}

// sweepDepartingEntities removes every live non-mob entity in an unloaded
// chunk from its list and returns it with its save tag. The caller writes
// the tags beside the departing mobs and drops each entity only once its
// write lands; a failed save readds it for the next tick, as for mobs.
// Each list lock is taken alone, in the tick phase where nothing else is
// held.
func (s *Server) sweepDepartingEntities() []departure {
	var out []departure

	s.lockTick(&s.itemsMu)
	s.setItemsLocked(sweepSaved(s.items, func(it *itemEntity) bool {
		if it.item.Dead {
			return false // a pickup or a kill consumed it; its own tick drops it
		}
		pos := world.ChunkPos{X: world.ChunkOf(it.item.X), Z: world.ChunkOf(it.item.Z)}
		if it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingItem, w: it.w, pos: pos,
			tag: itemTag(it), item: it,
		})
		return false
	}, itemTracker.unfile))
	s.unlockTick(&s.itemsMu)

	s.lockTick(&s.arrowsMu)
	s.arrows = sweepSaved(s.arrows, func(it *arrowEntity) bool {
		if it.arrow.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(it.arrow.X), Z: world.ChunkOf(it.arrow.Z)}
		if it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingArrow, w: it.w, pos: pos,
			tag: arrowTag(it), arrow: it,
		})
		return false
	}, arrowTracker.unfile)

	s.thrown = sweepSaved(s.thrown, func(it *thrownEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.throwable.X), Z: world.ChunkOf(it.throwable.Z)}
		if it.throwable.Dead || !thrownIsSaved(it) || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingThrown, w: it.w, pos: pos,
			tag: thrownTag(it), thrown: it,
		})
		return false
	}, thrownTracker.unfile)

	s.clouds = sweepSaved(s.clouds, func(it *cloudEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.cloud.X), Z: world.ChunkOf(it.cloud.Z)}
		if it.cloud.Dead || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingCloud, w: it.w, pos: pos,
			tag: cloudTag(it), cloud: it,
		})
		return false
	}, cloudTracker.unfile)
	s.unlockTick(&s.arrowsMu)

	s.lockTick(&s.fireworksMu)
	s.fireworks = sweepSaved(s.fireworks, func(it *fireworkEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.rocket.X), Z: world.ChunkOf(it.rocket.Z)}
		if it.rocket.Dead || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingFirework, w: it.w, pos: pos,
			tag: fireworkTag(it), firework: it,
		})
		return false
	}, fireworkTracker.unfile)
	s.unlockTick(&s.fireworksMu)

	s.lockTick(&s.eyesMu)
	s.eyes = sweepSaved(s.eyes, func(it *eyeEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.eye.X), Z: world.ChunkOf(it.eye.Z)}
		if it.eye.Dead || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingEye, w: it.w, pos: pos,
			tag: eyeTag(it), eye: it,
		})
		return false
	}, eyeTracker.unfile)
	s.unlockTick(&s.eyesMu)

	s.lockTick(&s.orbsMu)
	s.orbs = sweepSaved(s.orbs, func(it *orbEntity) bool {
		if it.orb.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(it.orb.X), Z: world.ChunkOf(it.orb.Z)}
		if it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingOrb, w: it.w, pos: pos,
			tag: orbTag(it), orb: it,
		})
		return false
	}, orbTracker.unfile)
	s.unlockTick(&s.orbsMu)

	s.lockTick(&s.tntMu)
	s.tnts = sweepSaved(s.tnts, func(it *tntEntity) bool {
		if it.tnt.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(it.tnt.X), Z: world.ChunkOf(it.tnt.Z)}
		if it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingTNT, w: it.w, pos: pos,
			tag: tntTag(it), tnt: it,
		})
		return false
	}, tntTracker.unfile)
	s.unlockTick(&s.tntMu)

	s.lockTick(&s.fallingMu)
	s.fallings = sweepSaved(s.fallings, func(it *fallingEntity) bool {
		if it.fb.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(it.fb.X), Z: world.ChunkOf(it.fb.Z)}
		if it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingFalling, w: it.w, pos: pos,
			tag: fallingTag(it), falling: it,
		})
		return false
	}, fallingTracker.unfile)
	s.unlockTick(&s.fallingMu)

	s.lockTick(&s.paintingsMu)
	s.paintings = sweepSaved(s.paintings, func(p *paintingEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(float64(p.x)), Z: world.ChunkOf(float64(p.z))}
		if p.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingPainting, w: p.w, pos: pos,
			tag: paintingTag(p), painting: p,
		})
		return false
	}, paintingTracker.unfile)
	s.unlockTick(&s.paintingsMu)

	s.lockTick(&s.framesMu)
	s.frames = sweepSaved(s.frames, func(f *frameEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(float64(f.x)), Z: world.ChunkOf(float64(f.z))}
		if f.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingFrame, w: f.w, pos: pos,
			tag: frameTag(f), frame: f,
		})
		s.removeFrameCellsLocked(f)
		return false
	}, frameTracker.unfile)
	s.unlockTick(&s.framesMu)

	s.lockTick(&s.boatsMu)
	s.boats = sweepSaved(s.boats, func(it *boatEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.boat.X), Z: world.ChunkOf(it.boat.Z)}
		// A dead boat stays until its own pass serves it, or its break and drop
		// would be lost.
		if it.boat.Dead || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingBoat, w: it.w, pos: pos,
			tag: boatTag(it), boat: it,
		})
		return false
	}, boatTracker.unfile)
	s.unlockTick(&s.boatsMu)

	s.lockTick(&s.minecartsMu)
	s.minecarts = sweepSaved(s.minecarts, func(it *minecartEntity) bool {
		pos := world.ChunkPos{X: world.ChunkOf(it.cart.X), Z: world.ChunkOf(it.cart.Z)}
		// A dead cart stays until the tick serves it, like a boat; a ridden one
		// stays with its rider, and a crossing's twin with the riders it seats.
		if it.cart.Dead || len(it.passengers) > 0 || it.boarding || it.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingMinecart, w: it.w, pos: pos,
			tag: minecartTag(it), minecart: it,
		})
		return false
	}, minecartTracker.unfile)
	s.unlockTick(&s.minecartsMu)

	s.lockTick(&s.armorStandsMu)
	s.armorStands = sweepSaved(s.armorStands, func(ent *armorStandEntity) bool {
		if ent.stand.Dead {
			// A break already removed it and ran its tail; nothing is left to serve.
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(ent.stand.X), Z: world.ChunkOf(ent.stand.Z)}
		if ent.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingArmorStand, w: ent.w, pos: pos,
			tag: armorStandTag(ent), armorStand: ent,
		})
		return false
	}, standTracker.unfile)
	s.unlockTick(&s.armorStandsMu)

	s.lockTick(&s.endCrystalsMu)
	s.endCrystals = sweepSaved(s.endCrystals, func(ent *endCrystalEntity) bool {
		if ent.crystal.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(ent.crystal.X), Z: world.ChunkOf(ent.crystal.Z)}
		if ent.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingEndCrystal, w: ent.w, pos: pos,
			tag: endCrystalTag(ent), endCrystal: ent,
		})
		return false
	}, endCrystalTracker.unfile)
	s.unlockTick(&s.endCrystalsMu)

	s.lockTick(&s.mannequinsMu)
	s.mannequins = sweepSaved(s.mannequins, func(ent *mannequinEntity) bool {
		if ent.mannequin.Dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(ent.mannequin.X), Z: world.ChunkOf(ent.mannequin.Z)}
		if ent.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingMannequin, w: ent.w, pos: pos,
			tag: mannequinTag(ent), mannequin: ent,
		})
		return false
	}, mannequinTracker.unfile)
	s.unlockTick(&s.mannequinsMu)

	s.lockTick(&s.fixturesMu)
	s.fixtures = sweepSaved(s.fixtures, func(ent *fixtureEntity) bool {
		if ent.dead {
			return false
		}
		pos := world.ChunkPos{X: world.ChunkOf(ent.x), Z: world.ChunkOf(ent.z)}
		if ent.w.ChunkLoaded(pos) {
			return true
		}
		out = append(out, departure{
			kind: departingFixture, w: ent.w, pos: pos,
			tag: fixtureTag(ent), fixture: ent,
		})
		return false
	}, fixtureTracker.unfile)
	s.unlockTick(&s.fixturesMu)

	return out
}

// liveEntityTags snapshots every live non-mob entity's save tag by world
// and chunk for the auto-save, each list under its own lock alone. An
// entity crossing a chunk border before the write is saved where seen and
// fixed by the next save.
func (s *Server) liveEntityTags() map[*world.World]map[world.ChunkPos][]nbt.Tag {
	tags := make(map[*world.World]map[world.ChunkPos][]nbt.Tag)

	s.itemsMu.Lock()
	collectSavedTags(tags, s.items, func(it *itemEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.item.Dead {
			return nil, 0, 0, nil
		}
		if _, ok := registry.ItemName(it.item.Stack.ItemID); !ok {
			// Never save an unnamed item as something else; it stays unsaved.
			slog.Error("could not name an item entity's stack for its save", "item", it.item.Stack.ItemID)
			return nil, 0, 0, nil
		}
		return it.w, it.item.X, it.item.Z, itemTag(it)
	})
	s.itemsMu.Unlock()

	s.arrowsMu.Lock()
	collectSavedTags(tags, s.arrows, func(it *arrowEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.arrow.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.arrow.X, it.arrow.Z, arrowTag(it)
	})
	collectSavedTags(tags, s.thrown, func(it *thrownEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.throwable.Dead || !thrownIsSaved(it) {
			return nil, 0, 0, nil
		}
		return it.w, it.throwable.X, it.throwable.Z, thrownTag(it)
	})
	collectSavedTags(tags, s.clouds, func(it *cloudEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.cloud.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.cloud.X, it.cloud.Z, cloudTag(it)
	})
	s.arrowsMu.Unlock()

	s.fireworksMu.Lock()
	collectSavedTags(tags, s.fireworks, func(it *fireworkEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.rocket.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.rocket.X, it.rocket.Z, fireworkTag(it)
	})
	s.fireworksMu.Unlock()

	s.eyesMu.Lock()
	collectSavedTags(tags, s.eyes, func(it *eyeEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.eye.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.eye.X, it.eye.Z, eyeTag(it)
	})
	s.eyesMu.Unlock()

	s.orbsMu.Lock()
	collectSavedTags(tags, s.orbs, func(it *orbEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.orb.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.orb.X, it.orb.Z, orbTag(it)
	})
	s.orbsMu.Unlock()

	s.tntMu.Lock()
	collectSavedTags(tags, s.tnts, func(it *tntEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.tnt.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.tnt.X, it.tnt.Z, tntTag(it)
	})
	s.tntMu.Unlock()

	s.fallingMu.Lock()
	collectSavedTags(tags, s.fallings, func(it *fallingEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.fb.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.fb.X, it.fb.Z, fallingTag(it)
	})
	s.fallingMu.Unlock()

	s.paintingsMu.Lock()
	collectSavedTags(tags, s.paintings, func(p *paintingEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		return p.w, float64(p.x), float64(p.z), paintingTag(p)
	})
	s.paintingsMu.Unlock()

	s.framesMu.Lock()
	collectSavedTags(tags, s.frames, func(f *frameEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		return f.w, float64(f.x), float64(f.z), frameTag(f)
	})
	s.framesMu.Unlock()

	s.boatsMu.Lock()
	collectSavedTags(tags, s.boats, func(it *boatEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.boat.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.boat.X, it.boat.Z, boatTag(it)
	})
	s.boatsMu.Unlock()

	s.minecartsMu.Lock()
	collectSavedTags(tags, s.minecarts, func(it *minecartEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if it.cart.Dead {
			return nil, 0, 0, nil
		}
		return it.w, it.cart.X, it.cart.Z, minecartTag(it)
	})
	s.minecartsMu.Unlock()

	s.armorStandsMu.Lock()
	collectSavedTags(tags, s.armorStands, func(ent *armorStandEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if ent.stand.Dead {
			return nil, 0, 0, nil
		}
		return ent.w, ent.stand.X, ent.stand.Z, armorStandTag(ent)
	})
	s.armorStandsMu.Unlock()

	s.endCrystalsMu.Lock()
	collectSavedTags(tags, s.endCrystals, func(ent *endCrystalEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if ent.crystal.Dead {
			return nil, 0, 0, nil
		}
		return ent.w, ent.crystal.X, ent.crystal.Z, endCrystalTag(ent)
	})
	s.endCrystalsMu.Unlock()

	s.mannequinsMu.Lock()
	collectSavedTags(tags, s.mannequins, func(ent *mannequinEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if ent.mannequin.Dead {
			return nil, 0, 0, nil
		}
		return ent.w, ent.mannequin.X, ent.mannequin.Z, mannequinTag(ent)
	})
	s.mannequinsMu.Unlock()

	s.fixturesMu.Lock()
	collectSavedTags(tags, s.fixtures, func(ent *fixtureEntity) (*world.World, float64, float64, nbt.CompoundTag) {
		if ent.dead {
			return nil, 0, 0, nil
		}
		return ent.w, ent.x, ent.z, fixtureTag(ent)
	})
	s.fixturesMu.Unlock()

	return tags
}

// liveEntityUUIDs collects every live non-mob entity's UUID. A wake skips
// saved entities in this set, so a failed save then reload never
// duplicates.
func (s *Server) liveEntityUUIDs() map[uuid.UUID]bool {
	known := make(map[uuid.UUID]bool)
	s.liveEntityUUIDsInto(known)
	return known
}

// liveEntityUUIDsInto adds every live non-mob UUID to known.
func (s *Server) liveEntityUUIDsInto(known map[uuid.UUID]bool) {
	s.scanLiveEntityUUIDs(&uuidScan{into: known})
}

// liveEntityUUID reports whether a live non-mob body has the UUID, asked of
// each kind's UUID index under its lock.
func (s *Server) liveEntityUUID(want uuid.UUID) bool {
	return hasLiveUUID(s, &itemTracker, want, func(it *itemEntity) bool { return !it.item.Dead }) ||
		hasLiveUUID(s, &arrowTracker, want, func(it *arrowEntity) bool { return !it.arrow.Dead }) ||
		hasLiveUUID(s, &thrownTracker, want, func(it *thrownEntity) bool { return !it.throwable.Dead && thrownIsSaved(it) }) ||
		hasLiveUUID(s, &cloudTracker, want, func(it *cloudEntity) bool { return !it.cloud.Dead }) ||
		hasLiveUUID(s, &fireworkTracker, want, func(it *fireworkEntity) bool { return !it.rocket.Dead }) ||
		hasLiveUUID(s, &eyeTracker, want, func(it *eyeEntity) bool { return !it.eye.Dead }) ||
		hasLiveUUID(s, &fangsTracker, want, func(it *fangsEntity) bool { return !it.fangs.Dead }) ||
		hasLiveUUID(s, &boltTracker, want, func(*boltEntity) bool { return true }) ||
		hasLiveUUID(s, &orbTracker, want, func(it *orbEntity) bool { return !it.orb.Dead }) ||
		hasLiveUUID(s, &tntTracker, want, func(it *tntEntity) bool { return !it.tnt.Dead }) ||
		hasLiveUUID(s, &fallingTracker, want, func(it *fallingEntity) bool { return !it.fb.Dead }) ||
		hasLiveUUID(s, &paintingTracker, want, func(*paintingEntity) bool { return true }) ||
		hasLiveUUID(s, &frameTracker, want, func(*frameEntity) bool { return true }) ||
		hasLiveUUID(s, &boatTracker, want, func(it *boatEntity) bool { return !it.boat.Dead }) ||
		hasLiveUUID(s, &minecartTracker, want, func(it *minecartEntity) bool { return !it.cart.Dead }) ||
		hasLiveUUID(s, &standTracker, want, func(it *armorStandEntity) bool { return !it.stand.Dead }) ||
		hasLiveUUID(s, &endCrystalTracker, want, func(it *endCrystalEntity) bool { return !it.crystal.Dead }) ||
		hasLiveUUID(s, &mannequinTracker, want, func(it *mannequinEntity) bool { return !it.mannequin.Dead }) ||
		hasLiveUUID(s, &fixtureTracker, want, func(it *fixtureEntity) bool { return !it.dead })
}

// hasLiveUUID reports whether the kind's UUID index holds a body with the
// UUID that is live.
func hasLiveUUID[B any, S any](s *Server, k *trackedKind[B, S], want uuid.UUID, live func(B) bool) bool {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	b, ok := k.chunks(s).ByUUID(want)
	return ok && live(b)
}

// scanLiveEntityUUIDs feeds every live non-mob UUID to sc, each kind under
// its own lock, until sc finds its UUID.
func (s *Server) scanLiveEntityUUIDs(sc *uuidScan) bool {
	return locked(&s.itemsMu, func() bool {
		return scanSavedUUIDs(s.items, func(it *itemEntity) (uuid.UUID, bool) { return it.uuid, !it.item.Dead }, sc)
	}) &&
		locked(&s.arrowsMu, func() bool {
			return scanSavedUUIDs(s.arrows, func(it *arrowEntity) (uuid.UUID, bool) { return it.uuid, !it.arrow.Dead }, sc)
		}) &&
		locked(&s.arrowsMu, func() bool {
			return scanSavedUUIDs(s.thrown, func(it *thrownEntity) (uuid.UUID, bool) { return it.uuid, !it.throwable.Dead && thrownIsSaved(it) }, sc)
		}) &&
		locked(&s.arrowsMu, func() bool {
			return scanSavedUUIDs(s.clouds, func(it *cloudEntity) (uuid.UUID, bool) { return it.uuid, !it.cloud.Dead }, sc)
		}) &&
		locked(&s.fireworksMu, func() bool {
			return scanSavedUUIDs(s.fireworks, func(it *fireworkEntity) (uuid.UUID, bool) { return it.uuid, !it.rocket.Dead }, sc)
		}) &&
		locked(&s.eyesMu, func() bool {
			return scanSavedUUIDs(s.eyes, func(it *eyeEntity) (uuid.UUID, bool) { return it.uuid, !it.eye.Dead }, sc)
		}) &&
		locked(&s.fangs.mu, func() bool {
			return scanSavedUUIDs(s.fangs.list, func(it *fangsEntity) (uuid.UUID, bool) { return it.uuid, !it.fangs.Dead }, sc)
		}) &&
		locked(&s.boltMu, func() bool {
			return scanSavedUUIDs(s.bolts, func(it *boltEntity) (uuid.UUID, bool) { return it.uuid, true }, sc)
		}) &&
		locked(&s.orbsMu, func() bool {
			return scanSavedUUIDs(s.orbs, func(it *orbEntity) (uuid.UUID, bool) { return it.uuid, !it.orb.Dead }, sc)
		}) &&
		locked(&s.tntMu, func() bool {
			return scanSavedUUIDs(s.tnts, func(it *tntEntity) (uuid.UUID, bool) { return it.uuid, !it.tnt.Dead }, sc)
		}) &&
		locked(&s.fallingMu, func() bool {
			return scanSavedUUIDs(s.fallings, func(it *fallingEntity) (uuid.UUID, bool) { return it.uuid, !it.fb.Dead }, sc)
		}) &&
		locked(&s.paintingsMu, func() bool {
			return scanSavedUUIDs(s.paintings, func(p *paintingEntity) (uuid.UUID, bool) { return p.uuid, true }, sc)
		}) &&
		locked(&s.framesMu, func() bool {
			return scanSavedUUIDs(s.frames, func(f *frameEntity) (uuid.UUID, bool) { return f.uuid, true }, sc)
		}) &&
		locked(&s.boatsMu, func() bool {
			return scanSavedUUIDs(s.boats, func(it *boatEntity) (uuid.UUID, bool) { return it.uuid, !it.boat.Dead }, sc)
		}) &&
		locked(&s.minecartsMu, func() bool {
			return scanSavedUUIDs(s.minecarts, func(it *minecartEntity) (uuid.UUID, bool) { return it.uuid, !it.cart.Dead }, sc)
		}) &&
		locked(&s.armorStandsMu, func() bool {
			return scanSavedUUIDs(s.armorStands, func(ent *armorStandEntity) (uuid.UUID, bool) { return ent.uuid, !ent.stand.Dead }, sc)
		}) &&
		locked(&s.endCrystalsMu, func() bool {
			return scanSavedUUIDs(s.endCrystals, func(ent *endCrystalEntity) (uuid.UUID, bool) { return ent.uuid, !ent.crystal.Dead }, sc)
		}) &&
		locked(&s.mannequinsMu, func() bool {
			return scanSavedUUIDs(s.mannequins, func(ent *mannequinEntity) (uuid.UUID, bool) { return ent.uuid, !ent.mannequin.Dead }, sc)
		}) &&
		locked(&s.fixturesMu, func() bool {
			return scanSavedUUIDs(s.fixtures, func(ent *fixtureEntity) (uuid.UUID, bool) { return ent.uuid, !ent.dead }, sc)
		})
}

// summonDuplicate reports whether a live body already has the UUID, which
// vanilla's tryAddFreshEntityWithPassengers refuses the summon for.
// vanilla: SummonCommand.createEntity
func (s *Server) summonDuplicate(identity uuid.UUID) bool {
	return s.liveEntityUUID(identity)
}

// readd returns an unwritten departure to its list, after a failed save or
// for a chunk whose herd never woke.
func (s *Server) readd(d departure) {
	switch d.kind {
	case departingItem:
		s.itemsMu.Lock()
		s.appendItemLocked(d.item)
		s.itemsMu.Unlock()
	case departingArrow:
		restoreSaved(s, &arrowTracker, d.arrow)
	case departingOrb:
		restoreSaved(s, &orbTracker, d.orb)
	case departingFalling:
		restoreSaved(s, &fallingTracker, d.falling)
	case departingPainting:
		s.paintingsMu.Lock()
		s.registerPaintingLocked(d.painting)
		s.paintingsMu.Unlock()
	case departingFrame:
		s.framesMu.Lock()
		s.registerFrameLocked(d.frame)
		s.framesMu.Unlock()
	case departingBoat:
		restoreSaved(s, &boatTracker, d.boat)
	case departingMinecart:
		restoreSaved(s, &minecartTracker, d.minecart)
	case departingArmorStand:
		restoreSaved(s, &standTracker, d.armorStand)
	case departingEndCrystal:
		restoreSaved(s, &endCrystalTracker, d.endCrystal)
	case departingMannequin:
		restoreSaved(s, &mannequinTracker, d.mannequin)
	case departingFixture:
		restoreSaved(s, &fixtureTracker, d.fixture)
	case departingThrown:
		restoreSaved(s, &thrownTracker, d.thrown)
	case departingCloud:
		restoreSaved(s, &cloudTracker, d.cloud)
	case departingFirework:
		restoreSaved(s, &fireworkTracker, d.firework)
	case departingEye:
		restoreSaved(s, &eyeTracker, d.eye)
	default:
		restoreSaved(s, &tntTracker, d.tnt)
	}
}

// wakeResult is what one chunk's wake loaded, each kind registered after
// the mob lock.
type wakeResult struct {
	mobs        []*mobEntity
	items       []*itemEntity
	arrows      []*arrowEntity
	orbs        []*orbEntity
	tnts        []*tntEntity
	fallings    []*fallingEntity
	paintings   []*paintingEntity
	frames      []*frameEntity
	boats       []*boatEntity
	minecarts   []*minecartEntity
	armorStands []*armorStandEntity
	endCrystals []*endCrystalEntity
	mannequins  []*mannequinEntity
	fixtures    []*fixtureEntity
	thrown      []*thrownEntity
	clouds      []*cloudEntity
	fireworks   []*fireworkEntity
	eyes        []*eyeEntity
	// teams are the bodies whose tag names a Team, joined once registered.
	teams []tagTeam
	// seats are the mobs a saved boat or cart nested, seated once both are
	// registered.
	seats []vehicleSeat
}

// vehicleSeat is a woken mob and the boat or cart it was saved riding; one
// of cart and boat is set.
type vehicleSeat struct {
	rider *mobEntity
	cart  *minecartEntity
	boat  *boatEntity
}

// add appends what another chunk's wake loaded, kind by kind.
func (r *wakeResult) add(o wakeResult) {
	r.mobs = append(r.mobs, o.mobs...)
	r.items = append(r.items, o.items...)
	r.arrows = append(r.arrows, o.arrows...)
	r.orbs = append(r.orbs, o.orbs...)
	r.tnts = append(r.tnts, o.tnts...)
	r.fallings = append(r.fallings, o.fallings...)
	r.paintings = append(r.paintings, o.paintings...)
	r.frames = append(r.frames, o.frames...)
	r.boats = append(r.boats, o.boats...)
	r.minecarts = append(r.minecarts, o.minecarts...)
	r.armorStands = append(r.armorStands, o.armorStands...)
	r.endCrystals = append(r.endCrystals, o.endCrystals...)
	r.mannequins = append(r.mannequins, o.mannequins...)
	r.fixtures = append(r.fixtures, o.fixtures...)
	r.thrown = append(r.thrown, o.thrown...)
	r.clouds = append(r.clouds, o.clouds...)
	r.fireworks = append(r.fireworks, o.fireworks...)
	r.eyes = append(r.eyes, o.eyes...)
	r.teams = append(r.teams, o.teams...)
	r.seats = append(r.seats, o.seats...)
}

// wasWritten reports whether d's chunk was in the landed unload write,
// meaning d has left rather than waiting for a retry.
func wasWritten(d departure, written map[*world.World]map[world.ChunkPos]struct{}) bool {
	chunks, ok := written[d.w]
	if !ok {
		return false
	}
	_, ok = chunks[d.pos]
	return ok
}

// removeDeparture removes a written departure from every view.
func (s *Server) removeDeparture(d departure) {
	s.broadcastWorld(d.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{d.id()}))
}

// settleDepartures ends the tick's leavers: written ones are gone; the rest
// (failed save, unwoken chunk, mobs left out) return to their lists, since
// writing over an unwoken chunk would lose entities the herd cannot
// simulate.
func (s *Server) settleDepartures(departures []departure, written map[*world.World]map[world.ChunkPos]struct{}) {
	for _, d := range departures {
		if d.boxed && wasWritten(d, written) {
			s.removeDeparture(d)
			continue
		}
		s.readd(d)
	}
}

// registerWake registers and shows a wake's entities. Arrow and TNT owners
// and the vexes' summoners are resolved after the mobs are added, so a
// skeleton and its saved arrow, or an evoker and its vex, find each other
// in whatever order they load; the sweep reaches a vex that woke first and
// is still waiting for its evoker.
func (s *Server) registerWake(woken wakeResult) {
	for _, ent := range woken.mobs {
		s.addMob(ent)
	}
	for _, ent := range woken.mobs {
		if ent.mob.Jockey == entity.JockeyZombieNautilus || ent.mob.Jockey == entity.JockeyStructureNautilus {
			s.resolveJockey(s.dimensionOfWorld(ent.w), ent)
		}
	}
	if len(woken.mobs) > 0 {
		s.relinkVexOwners()
	}
	for _, it := range woken.items {
		s.registerItem(it)
	}
	for _, it := range woken.arrows {
		it.arrow.OwnerID = s.resolveOwner(it.ownerUUID)
		s.registerArrow(it)
	}
	for _, it := range woken.orbs {
		s.registerOrb(it)
	}
	for _, it := range woken.tnts {
		it.tnt.OwnerID = s.resolveOwner(it.ownerUUID)
		s.registerTNT(it)
	}
	for _, it := range woken.fallings {
		s.registerFalling(it)
	}
	for _, p := range woken.paintings {
		s.registerPainting(p)
	}
	for _, f := range woken.frames {
		s.registerFrame(f)
	}
	for _, it := range woken.boats {
		s.registerBoat(it)
	}
	for _, it := range woken.minecarts {
		s.registerMinecart(it)
	}
	for _, seat := range woken.seats {
		if seat.cart != nil {
			s.mountMobOnMinecart(seat.rider, seat.cart, true)
		} else {
			s.mountMobOnBoat(seat.rider, seat.boat)
		}
	}
	for _, ent := range woken.armorStands {
		s.registerArmorStand(ent)
	}
	for _, ent := range woken.endCrystals {
		s.registerEndCrystal(ent)
	}
	for _, ent := range woken.mannequins {
		s.registerMannequin(ent)
	}
	for _, ent := range woken.fixtures {
		s.registerFixture(ent)
	}
	for _, it := range woken.thrown {
		it.throwable.OwnerID = s.resolveOwner(it.ownerUUID)
		s.registerThrown(it)
	}
	for _, it := range woken.clouds {
		s.registerCloud(it)
	}
	for _, it := range woken.fireworks {
		it.ownerID = s.resolveOwner(it.ownerUUID)
		it.rocket.OwnerID = it.ownerID
		s.registerFirework(it)
	}
	for _, ent := range woken.eyes {
		s.registerEye(ent)
	}
	for _, t := range woken.teams {
		s.joinTagTeam(t)
	}
}

// wakeArrivalChunk wakes a crossing's arrival chunk and registers its
// contents: never walked, it may hold saved entities the arrival must not
// overwrite, and waking lists it for its file so the arrival saves with it.
// An already woken chunk is left alone.
func (s *Server) wakeArrivalChunk(dim *dimension, spot world.SpawnPos) {
	pos := world.ChunkPos{X: world.ChunkOf(float64(spot.X)), Z: world.ChunkOf(float64(spot.Z))}
	if woken, fresh := s.wakeChunkOnce(dim, pos); fresh {
		s.registerWake(woken)
	}
}

// wakeChunkOnce wakes a chunk the first time asked, false after. Non-mob
// UUIDs are read before the mob lock, as their locks are never taken above
// it; the herd's UUIDs and woken records under it.
func (s *Server) wakeChunkOnce(dim *dimension, pos world.ChunkPos) (wakeResult, bool) {
	h := dim.mobs
	s.mobsMu.Lock()
	_, awake := h.woken[pos]
	s.mobsMu.Unlock()
	if awake {
		return wakeResult{}, false
	}
	known := s.liveEntityUUIDs()
	s.mobsMu.Lock()
	// RCON runs commands on its own goroutine, so another may have woken it.
	if _, ok := h.woken[pos]; ok {
		s.mobsMu.Unlock()
		return wakeResult{}, false
	}
	for _, ent := range h.book {
		known[ent.uuid] = true
	}
	h.woken[pos] = struct{}{}
	woken := h.wake(pos, s, known, false)
	s.mobsMu.Unlock()
	return woken, true
}

// resolveOwner returns a saved owner UUID's entity ID, players then mobs; 0
// for a nil UUID or an owner not in the world (e.g. its chunk not woken).
func (s *Server) resolveOwner(identity uuid.UUID) int32 {
	if owner := s.relatedByUUID(identity); owner != nil {
		return owner.EntityID()
	}
	return 0
}
