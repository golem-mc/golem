package server

import (
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The leash knots tied to fences, and the uses that move a mob between
// holders. The lead's own physics is the mob tick's (internal/server/entity/leash.go).

// The knot's box, its height above the fence's floor, its tracking range in
// chunks, and the survives poll's interval in ticks.
// vanilla: EntityTypes.LEASH_KNOT, LeashFenceKnotEntity.OFFSET_Y,
// BlockAttachedEntity.tick
const (
	knotWidth     = 0.375
	knotHeight    = 0.5
	knotStandingY = 0.375
	knotTracking  = 10
	knotPollTicks = 100
)

// The leash's own sounds, the names the sound registry carries: the tie a
// lead's use makes, the untie of one let go, the snap of one run out.
const (
	knotSoundTied   = registry.SoundItemLeadTied
	knotSoundUntied = registry.SoundItemLeadUntied
	knotSoundBreak  = registry.SoundItemLeadBreak
)

// knotSliverOf copies the box a projectile reads out of k. The knot lock is
// held.
func knotSliverOf(k *knotEntity) entitySliver {
	minX, minY, minZ, maxX, maxY, maxZ := knotBox(k)
	return entitySliver{id: k.id, minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ}
}

// knotEntity is one leash knot, at its fence's cell.
type knotEntity struct {
	id      int32
	uuid    uuid.UUID
	w       *world.World
	x, y, z int32

	// checkInterval counts ticks toward the periodic survives poll.
	checkInterval int
	// invulnerable is the Invulnerable tag, which turns away every hurt but
	// a creative player's.
	invulnerable bool

	bodyTrack[struct{}]

	// tags are the scoreboard tags. Guarded by the knot lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
}

// knotTracker tracks the knots, measured from the fence cell's corner. A
// knot has no movement schedule, and neither a join nor a frozen world
// shows one: the knot tick's pass does.
var knotTracker = trackedKind[*knotEntity, struct{}]{
	rangeChunks: knotTracking,
	lock:        func(s *Server) *sync.Mutex { return &s.knotsMu },
	list:        func(s *Server) *[]*knotEntity { return &s.knots },
	track:       func(k *knotEntity) *bodyTrack[struct{}] { return &k.bodyTrack },
	world:       func(k *knotEntity) *world.World { return k.w },
	id:          func(k *knotEntity) int32 { return k.id },
	dead:        func(*knotEntity) bool { return false },
	pose: func(k *knotEntity) bodyPose {
		return bodyPose{x: float64(k.x), y: float64(k.y), z: float64(k.z)}
	},
	tellPose: func(k *knotEntity) bodyPose { return bodyPose{x: knotX(k), y: knotY(k), z: knotZ(k)} },
	pairing:  knotPairing,
}

// knotHolderSnapshot is the holder a mob leashed to k is ticked against.
func knotHolderSnapshot(k *knotEntity) entity.LeashHolder {
	return entity.LeashHolder{
		EntityID: k.id,
		X:        float64(k.x) + 0.5,
		Y:        float64(k.y) + knotStandingY,
		Z:        float64(k.z) + 0.5,
		Width:    knotWidth, Height: knotHeight,
		Present: true, Knot: true,
	}
}

// playerLeashHolder is the holder snapshot of player c: their own place and
// box shape, Present while they may still hold a lead — alive, and no
// spectator's. The SameLevel flag is the caller's to set.
// vanilla: Entity.canInteractWithLevel
func playerLeashHolder(c *client) entity.LeashHolder {
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	return entity.LeashHolder{
		EntityID: c.player.EntityID,
		X:        pos.X, Y: pos.Y, Z: pos.Z,
		Width:  dims.Width,
		Height: dims.Height,
		Present: !c.player.Vitals().Dead &&
			c.player.GameMode() != protocol.GameModeSpectator,
	}
}

// playerHolderSnapshot is playerLeashHolder present whatever the player's
// mode, for a tie of their own.
func (c *client) playerHolderSnapshot() *entity.LeashHolder {
	h := playerLeashHolder(c)
	h.Present = true
	return &h
}

// knotAtCellLocked finds the knot tied to the fence block given, nil for
// none. The knot lock is held.
func (s *Server) knotAtCellLocked(w *world.World, x, y, z int32) *knotEntity {
	for _, k := range s.knots {
		if k.w == w && k.x == x && k.y == y && k.z == z {
			return k
		}
	}
	return nil
}

// knotByEntityID finds the knot an entity ID names, nil for one gone.
func (s *Server) knotByEntityID(w *world.World, id int32) *knotEntity {
	s.knotsMu.Lock()
	defer s.knotsMu.Unlock()
	for _, k := range s.knots {
		if k.id == id && k.w == w {
			return k
		}
	}
	return nil
}

// knotTell is what a change or an end reads after the knot lock, copied
// under it so a /tp cannot be read halfway.
type knotTell struct {
	w            *world.World
	id           int32
	x, y, z      float64
	invulnerable bool
}

// knotTellOf copies the fields a tell reads out of k. The knot lock is held.
func knotTellOf(k *knotEntity) knotTell {
	return knotTell{w: k.w, id: k.id, x: knotX(k), y: knotY(k), z: knotZ(k), invulnerable: k.invulnerable}
}

// box is the knot body's box, the shape a blow's reach check meets.
func (t knotTell) box() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := float64(knotWidth) / 2
	return t.x - half, t.y, t.z - half, t.x + half, t.y + knotHeight, t.z + half
}

// knotTellByEntityID finds the knot an entity ID names and copies the fields
// a tell reads out of it, false for one gone.
func (s *Server) knotTellByEntityID(w *world.World, id int32) (knotTell, bool) {
	s.knotsMu.Lock()
	defer s.knotsMu.Unlock()
	for _, k := range s.knots {
		if k.id == id && k.w == w {
			return knotTellOf(k), true
		}
	}
	return knotTell{}, false
}

// tieKnot stands a knot on the fence at the cell given, or takes the one
// there, and shows it to every player in range, again for one already
// shown. created is false for a knot found; the tell is for the caller's
// sound and links.
// vanilla: LeashFenceKnotEntity.getOrCreateKnot, createKnot
func (s *Server) tieKnot(w *world.World, x, y, z int32) (k *knotEntity, tell knotTell, created bool) {
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()
	s.mu.RUnlock()

	s.knotsMu.Lock()
	k = s.knotAtCellLocked(w, x, y, z)
	if k == nil {
		k = &knotEntity{
			id: s.nextEntity(), uuid: uuid.New(),
			w: w, x: x, y: y, z: z,
		}
		s.knots = append(s.knots, k)
		created = true
	}
	shown := knotPairing(k)
	tell = knotTellOf(k)
	// Picked under the lock, so the send cannot re-read a knot a /tp moved.
	watching := knotTracker.showLocked(k, watchers)
	s.knotsMu.Unlock()

	sendPacketsToAll(w, watching, shown)
	return k, tell, created
}

// knotPairing is a knot's Spawn Entity, at the fence cell's corner rather
// than the body's standing centre.
// vanilla: LeashFenceKnotEntity.getAddEntityPacket, ClientboundAddEntityPacket
func knotPairing(k *knotEntity) []outbound {
	var b protocol.Buffer
	b.PutVarInt(k.id)
	b.PutUUID(k.uuid)
	b.PutVarInt(protocol.EntityTypeLeashKnot)
	b.PutF64(float64(k.x))
	b.PutF64(float64(k.y))
	b.PutF64(float64(k.z))
	b.PutLpVec3(0, 0, 0) // velocity
	b.PutAngle(0)        // pitch
	b.PutAngle(0)        // yaw
	b.PutAngle(0)        // head yaw
	b.PutVarInt(0)
	return spawnAndMetadata(b.Bytes(), k.custom.metadataPayload(k.id))
}

// leashLinkPayload builds the Set Entity Link packet: the mob attached to
// the holder named, holding ID 0 for the lead let go.
func leashLinkPayload(attached, holding int32) []byte {
	var b protocol.Buffer
	b.PutI32(attached)
	b.PutI32(holding)
	return b.Bytes()
}

// knotSurvives reports whether the cell holds a block of the fences tag.
// vanilla: LeashFenceKnotEntity.survives
func knotSurvives(w *world.World, x, y, z int32) bool {
	return rules.IsFenceBlock(registry.BlockOf(w.BlockAt(x, y, z)))
}

// knotX, knotY, knotZ are a knot's entity position: the fence's centre, up
// the standing height.
func knotX(k *knotEntity) float64 { return float64(k.x) + 0.5 }
func knotY(k *knotEntity) float64 { return float64(k.y) + knotStandingY }
func knotZ(k *knotEntity) float64 { return float64(k.z) + 0.5 }

// knotBox is a knot's box.
// vanilla: LeashFenceKnotEntity.recalculateBoundingBox
func knotBox(k *knotEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	half := float64(knotWidth) / 2
	return knotX(k) - half, knotY(k), knotZ(k) - half,
		knotX(k) + half, knotY(k) + knotHeight, knotZ(k) + half
}

// destroyKnot takes a knot out of the registry; the callers send its
// removal and whatever its end is heard as.
func (s *Server) destroyKnot(k *knotEntity) {
	s.knotsMu.Lock()
	s.knots = slices.DeleteFunc(s.knots, func(o *knotEntity) bool { return o == k })
	s.knotsMu.Unlock()
	s.entityDestroyed(k.uuid)
}

// killKnot discards a knot with no untie sound. Each mob it held drops its
// lead on its own tick.
// vanilla: Entity.kill
func (s *Server) killKnot(k *knotEntity) {
	s.destroyKnot(k)
	s.broadcastWorld(k.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{k.id}))
}

// teleportKnot moves a knot to the cell under the position given and marks it
// for the next tracking pass to tell its watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportKnot(k *knotEntity, to player.Position, by string) {
	knotTracker.teleport(s, k, func() {
		k.x, k.y, k.z = int32(math.Floor(to.X)), int32(math.Floor(to.Y)), int32(math.Floor(to.Z))
	})
}

// knotTwinAt builds the knot that continues this one in another dimension,
// standing on the cell under pos: a new ID, the same UUID. The caller holds
// the knots' lock or has taken the knot out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) knotTwinAt(k *knotEntity, to *dimension, pos player.Position) *knotEntity {
	return &knotEntity{
		id: s.nextEntity(), uuid: k.uuid, w: to.w,
		x: rules.BlockOf(pos.X), y: rules.BlockOf(pos.Y), z: rules.BlockOf(pos.Z),
		invulnerable: k.invulnerable,
		tags:         nbt.NewStringSet(k.tags.Values()),
		custom:       k.custom,
	}
}

// registerKnot puts a built knot into the world and shows it to every
// player in range.
func (s *Server) registerKnot(k *knotEntity) {
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()
	s.mu.RUnlock()

	s.knotsMu.Lock()
	s.knots = append(s.knots, k)
	watching := knotTracker.showLocked(k, watchers)
	shown := knotPairing(k)
	s.knotsMu.Unlock()

	sendPacketsToAll(k.w, watching, shown)
}

// downKnot brings a knot down, a blow's or the survives poll's end, with
// the untie sound where it stood. Each mob it held drops its lead on its
// own tick.
// vanilla: BlockAttachedEntity.hurtServer, BlockAttachedEntity.tick
func (s *Server) downKnot(tell knotTell) {
	s.knotsMu.Lock()
	s.knots = slices.DeleteFunc(s.knots, func(o *knotEntity) bool {
		return o.id == tell.id && o.w == tell.w
	})
	s.knotsMu.Unlock()
	s.broadcastWorld(tell.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{tell.id}))
	s.broadcastSoundID(tell.w,
		knotSoundUntied, protocol.SoundCategoryNeutral, tell.x, tell.y, tell.z, 1.0, 1.0)

}

// knotHoldsNothing reports whether no live mob or boat of the knot's world
// is held by the knot any more. The mob and boat locks are taken in turn.
func (s *Server) knotHoldsNothing(k *knotEntity) bool {
	s.mobsMu.Lock()
	for _, ent := range s.herdOf(k.w).book {
		// A dead body holds no lead, so a corpse still in the herd the
		// tick before it says its death does not keep the knot up.
		if !ent.mob.Dead && ent.mob.LeashHolderID == k.id {
			s.mobsMu.Unlock()
			return false
		}
	}
	s.mobsMu.Unlock()
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == k.w && it.holder == k.id {
			return false
		}
	}
	return true
}

// discardEmptiedKnot takes a knot down, without a sound, once it holds no
// mob.
// vanilla: LeashFenceKnotEntity.notifyLeasheeRemoved
func (s *Server) discardEmptiedKnot(k *knotEntity) {
	if !s.knotHoldsNothing(k) {
		return
	}
	s.destroyKnot(k)
	s.broadcastWorld(k.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{k.id}))
}

// finishLeashDrop tells the end of one lead: a snap's sound at the holder,
// the lead laid at the mob if lay, the hold told gone unless the body is, and a knot left
// holding nothing taken down. lay carries the entity_drops rule a snap
// ignores.
// vanilla: Leashable.dropLeash
func (s *Server) finishLeashDrop(drop mobLeashDrop, lay bool) {
	if drop.broke {
		s.broadcastSoundID(drop.w,
			knotSoundBreak, protocol.SoundCategoryNeutral,
			drop.hx, drop.hy, drop.hz, 1.0, 1.0)

	}
	if lay {
		s.dropLeadItem(drop.w, drop.x, drop.y, drop.z)
	}
	if !drop.untracked {
		s.broadcastWorld(drop.w, protocol.IDPlaySetEntityLink, leashLinkPayload(drop.mobID, 0))
	}
	if drop.holder != 0 {
		if knot := s.knotByEntityID(drop.w, drop.holder); knot != nil {
			s.discardEmptiedKnot(knot)
		}
	}
}

// finishMobLeashDrop ends a lead the mob tick took off its mob. Only a
// holder that died or left consults entity_drops; a snap or a laid drop lays
// its lead whatever the rule says.
// vanilla: Leashable.tickLeash
func (s *Server) finishMobLeashDrop(drop mobLeashDrop) {
	s.finishLeashDrop(drop, drop.broke || drop.laid || drop.w.BoolRule(world.RuleEntityDrops))
}

// tieMobsTo moves each mob of the list that may be held onto the holder the
// snapshot gives, which a save names by ref, and reports which of them
// moved. The mob lock is held.
func tieMobsTo(mobs []*mobEntity, holder *entity.LeashHolder, ref leashRef) []*mobEntity {
	var moved []*mobEntity
	for _, ent := range mobs {
		if ent.mob.Dead || !entity.LeashableBy(ent.mob, holder) {
			continue
		}
		// One already held by the holder it is moving onto is moved
		// already; the link it carries stands.
		if ent.mob.LeashHolderID == holder.EntityID {
			continue
		}
		ent.tieLeash(holder.EntityID, ref)
		moved = append(moved, ent)
	}
	return moved
}

// mobsLeashedToLocked gathers the world's live mobs the holder holds whose
// box is within sixteen blocks every way of the centre given.
// vanilla: Leashable.leashableInArea
func (s *Server) mobsLeashedToLocked(w *world.World, holder int32, cx, cy, cz float64) []*mobEntity {
	var mobs []*mobEntity
	for _, ent := range s.herdOf(w).book {
		if ent.mob.LeashHolderID != holder || ent.mob.Dead {
			continue
		}
		halfW := 0.5 * ent.mob.BbWidth()
		halfH := 0.5 * ent.mob.BbHeight()
		if math.Abs(ent.mob.X-cx) > 16+halfW || math.Abs(ent.mob.Z-cz) > 16+halfW ||
			math.Abs(ent.mob.Y+halfH-cy) > 16+halfH {
			continue
		}
		mobs = append(mobs, ent)
	}
	return mobs
}

// tieMobsAllowed moves each live mob held by from near (cx, cy, cz) (except
// skip) onto to, asking PlayerLeashEntityEvent outside mobsMu when a listener
// is registered. found reports whether any candidate existed before the veto.
func (c *client) tieMobsAllowed(w *world.World, from int32, cx, cy, cz float64, to *entity.LeashHolder, ref leashRef, holderID uuid.UUID, skip *mobEntity) (moved []*mobEntity, found bool) {
	s := c.srv
	if !c.pluginListening(pluginruntime.PlayerLeashEntity) {
		s.mobsMu.Lock()
		led := s.mobsLeashedToLocked(w, from, cx, cy, cz)
		if skip != nil {
			led = slices.DeleteFunc(led, func(e *mobEntity) bool { return e == skip })
		}
		moved = tieMobsTo(led, to, ref)
		s.mobsMu.Unlock()
		return moved, len(moved) > 0
	}
	s.mobsMu.Lock()
	var cands []*mobEntity
	for _, ent := range s.mobsLeashedToLocked(w, from, cx, cy, cz) {
		if ent != skip && !ent.mob.Dead && ent.mob.LeashHolderID != to.EntityID && entity.LeashableBy(ent.mob, to) {
			cands = append(cands, ent)
		}
	}
	s.mobsMu.Unlock()
	if len(cands) == 0 {
		return nil, false
	}
	cands = slices.DeleteFunc(cands, func(ent *mobEntity) bool { return !c.leashAllowed(ent.uuid, holderID, false) })
	if len(cands) == 0 {
		return nil, true
	}
	s.mobsMu.Lock()
	for _, ent := range cands {
		if ent.mob.Dead || ent.mob.LeashHolderID != from || ent.mob.LeashHolderID == to.EntityID || !entity.LeashableBy(ent.mob, to) {
			continue
		}
		ent.tieLeash(to.EntityID, ref)
		moved = append(moved, ent)
	}
	s.mobsMu.Unlock()
	return moved, true
}

// tieBoatsAllowed is tieBoatsHeldTo with PlayerLeashEntityEvent asked outside
// boatsMu when a listener is registered. found reports whether any candidate
// existed before the veto.
func (c *client) tieBoatsAllowed(w *world.World, from int32, cx, cy, cz float64, to *entity.LeashHolder, ref leashRef, holderID uuid.UUID) (moved []int32, found bool) {
	s := c.srv
	if !c.pluginListening(pluginruntime.PlayerLeashEntity) {
		moved = s.tieBoatsHeldTo(w, from, cx, cy, cz, to, ref)
		return moved, len(moved) > 0
	}
	s.boatsMu.Lock()
	var cands []*boatEntity
	for _, it := range s.boats {
		if it.holder != to.EntityID && heldBoatWithin(it, w, from, cx, cy, cz, to) {
			cands = append(cands, it)
		}
	}
	s.boatsMu.Unlock()
	if len(cands) == 0 {
		return nil, false
	}
	cands = slices.DeleteFunc(cands, func(it *boatEntity) bool { return !c.leashAllowed(it.uuid, holderID, false) })
	if len(cands) == 0 {
		return nil, true
	}
	s.boatsMu.Lock()
	for _, it := range cands {
		if it.holder != to.EntityID && heldBoatWithin(it, w, from, cx, cy, cz, to) {
			it.tie(to.EntityID, ref)
			moved = append(moved, it.boat.ID)
		}
	}
	s.boatsMu.Unlock()
	return moved, true
}

// tieBoatsHeldTo moves the world's live boats the holder from holds, within
// sixteen blocks every way of the centre given, onto the holder to where it
// is within reach and does not hold them already; a save names to by ref.
// It returns the IDs of the boats moved.
// vanilla: Leashable.leashableInArea, canHaveALeashAttachedTo, setLeashedTo
func (s *Server) tieBoatsHeldTo(w *world.World, from int32, cx, cy, cz float64, to *entity.LeashHolder, ref leashRef) []int32 {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	var moved []int32
	for _, it := range s.boats {
		if it.holder != to.EntityID && heldBoatWithin(it, w, from, cx, cy, cz, to) {
			it.tie(to.EntityID, ref)
			moved = append(moved, it.boat.ID)
		}
	}
	return moved
}

// holdsBoatWithin reports whether tieBoatsHeldTo would find a boat to move.
func (s *Server) holdsBoatWithin(w *world.World, from int32, cx, cy, cz float64, to *entity.LeashHolder) bool {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	return slices.ContainsFunc(s.boats, func(it *boatEntity) bool {
		return heldBoatWithin(it, w, from, cx, cy, cz, to)
	})
}

// heldBoatWithin reports whether the live boat it of world w is held by from,
// crosses the 32-block cube centred on (cx, cy, cz), and is within the reach
// of to. The boat lock is held.
func heldBoatWithin(it *boatEntity, w *world.World, from int32, cx, cy, cz float64, to *entity.LeashHolder) bool {
	b := it.boat
	return it.w == w && it.holder == from && !b.Dead &&
		math.Abs(b.X-cx) <= 16+entity.BoatHalfWidth && math.Abs(b.Z-cz) <= 16+entity.BoatHalfWidth &&
		math.Abs(b.Y+0.5*entity.BoatHeight-cy) <= 16+0.5*entity.BoatHeight &&
		entity.LeashReaches(b.X, b.Y, b.Z, entity.BoatHeight, entity.LeashSnapDistance, to)
}

// useLeadOnFence handles a fence's use with any hand: the mobs and boats the
// player leads nearby move onto a knot tied there, no lead spent. handled is
// true when one moved; otherwise the click falls through.
// vanilla: FenceBlock.useWithoutItem, LeadItem.bindPlayerMobs
func (c *client) useLeadOnFence(hand, x, y, z int32) (handled bool, err error) {
	w := c.theWorld()
	if !knotSurvives(w, x, y, z) {
		return false, nil
	}
	s := c.srv

	// A knot is only stood up where one of the led mobs could move onto
	// it.
	// vanilla: Vec3.atCenterOf, Leashable.canHaveALeashAttachedTo
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	holder := entity.LeashHolder{
		X: float64(x) + 0.5, Y: float64(y) + knotStandingY, Z: float64(z) + 0.5,
		Width: knotWidth, Height: knotHeight, Present: true,
	}
	s.mobsMu.Lock()
	led := s.mobsLeashedToLocked(w, c.player.EntityID, cx, cy, cz)
	binding := make([]*mobEntity, 0, len(led))
	for _, ent := range led {
		if entity.LeashableBy(ent.mob, &holder) {
			binding = append(binding, ent)
		}
	}
	s.mobsMu.Unlock()
	if len(binding) == 0 && !s.holdsBoatWithin(w, c.player.EntityID, cx, cy, cz, &holder) {
		return false, nil
	}

	knot, tell, created := s.tieKnot(w, x, y, z)
	holder.EntityID = tell.id
	binding = slices.DeleteFunc(binding, func(ent *mobEntity) bool { return !c.leashAllowed(ent.uuid, knot.uuid, false) })
	s.mobsMu.Lock()
	// The hold is re-read under the lock the knot was stood up without: a
	// mob the player let go of in between is not moved, and a knot it
	// would leave holding nothing is not left standing.
	moved := make([]*mobEntity, 0, len(binding))
	for _, ent := range binding {
		if ent.mob.Dead || ent.mob.LeashHolderID != c.player.EntityID || !entity.LeashableBy(ent.mob, &holder) {
			continue
		}
		ent.tieLeash(holder.EntityID, leashRef{knot: [3]int32{x, y, z}, atKnot: true})
		moved = append(moved, ent)
	}
	s.mobsMu.Unlock()
	boats, _ := c.tieBoatsAllowed(w, c.player.EntityID, cx, cy, cz, &holder, leashRef{knot: [3]int32{x, y, z}, atKnot: true}, knot.uuid)
	if len(moved) == 0 && len(boats) == 0 {
		if created {
			s.discardEmptiedKnot(knot)
		}
		return false, nil
	}
	// The hold each body was moved onto is the knot's own, read off the
	// holder rather than the body's field the tick also writes.
	for _, ent := range moved {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink,
			leashLinkPayload(ent.mob.ID, holder.EntityID))
	}
	for _, id := range boats {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, holder.EntityID))
	}
	// The tie sound, said where the knot stands.
	// vanilla: LeashFenceKnotEntity.playPlacementSound
	s.broadcastSoundID(w,
		knotSoundTied, protocol.SoundCategoryNeutral, tell.x, tell.y, tell.z, 1.0, 1.0)

	// InteractionResult.SUCCESS_SERVER: the server swings the arm.
	c.useSwing = swingServer
	return true, nil
}

// useKnot handles an Interact on a knot: the mobs and boats the player leads
// nearby move onto it, or, with none and no sneak, the knot's own go back to
// the player. The client swings its own arm, so the server sends none.
// vanilla: LeashFenceKnotEntity.interact
func (c *client) useKnot(k *knotEntity, hand int32, sneaking bool) error {
	s := c.srv
	w := k.w
	// The knot's identity and standing place, copied under the lock: the
	// search and the sound below run outside it and a /tp may move the knot.
	s.knotsMu.Lock()
	tell := knotTellOf(k)
	ref := leashRef{knot: [3]int32{k.x, k.y, k.z}, atKnot: true}
	s.knotsMu.Unlock()
	holderKnot := entity.LeashHolder{
		EntityID: tell.id, X: tell.x, Y: tell.y, Z: tell.z,
		Width: knotWidth, Height: knotHeight, Present: true,
	}
	if c.heldItem(hand).ItemID == int32(registry.ItemShears) &&
		c.finishPlayerLeashShear(w, s.cutKnotLeashes(tell), tell.x, tell.y, tell.z, hand) {
		return nil
	}
	holderSelf := c.playerHolderSnapshot()

	// holding is the body the moved mobs and boats were tied to, one of
	// the two holders above, read off whichever search moved them rather
	// than the body's field the tick also writes.
	var moved []*mobEntity
	holding := holderKnot.EntityID
	var emptyKnot *knotEntity
	// Each search's box is centred on its own holder.
	// vanilla: Leashable.leashableInArea
	selfY := holderSelf.Y + 0.5*holderSelf.Height
	moved, hadMobs := c.tieMobsAllowed(w, c.player.EntityID, holderSelf.X, selfY, holderSelf.Z, &holderKnot, ref, k.uuid, nil)
	boats, hadBoats := c.tieBoatsAllowed(w, c.player.EntityID, holderSelf.X, selfY, holderSelf.Z, &holderKnot, ref, k.uuid)
	if len(moved) == 0 && len(boats) == 0 && !hadMobs && !hadBoats && !sneaking {
		// The take-back: nothing of the player's came, so the knot's own go
		// back to the player, whichever of them the player could hold.
		knotY := tell.y + 0.5*knotHeight
		moved, _ = c.tieMobsAllowed(w, tell.id, tell.x, knotY, tell.z, holderSelf, leashRef{holder: c.player.UUID}, c.player.UUID, nil)
		boats, _ = c.tieBoatsAllowed(w, tell.id, tell.x, knotY, tell.z, holderSelf, leashRef{holder: c.player.UUID}, c.player.UUID)
		holding = holderSelf.EntityID
		if len(moved) > 0 || len(boats) > 0 {
			emptyKnot = k
		}
	}

	if len(moved) == 0 && len(boats) == 0 {
		return nil
	}
	for _, ent := range moved {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink,
			leashLinkPayload(ent.mob.ID, holding))
	}
	for _, id := range boats {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, holding))
	}
	s.broadcastSoundID(w,
		knotSoundTied, protocol.SoundCategoryNeutral, tell.x, tell.y, tell.z, 1.0, 1.0)

	if emptyKnot != nil {
		s.discardEmptiedKnot(k)
	}
	c.useSwing = swingPredicted
	return nil
}

// tieLedOntoMob moves the mobs and boats the player leads near ent onto ent,
// spending nothing, and reports whether any moved.
// vanilla: Entity.interact, Leashable.leashableInArea
func (c *client) tieLedOntoMob(ent *mobEntity, hand int32) bool {
	s := c.srv
	w := ent.w
	s.mobsMu.Lock()
	m := ent.mob
	if m.Dead {
		s.mobsMu.Unlock()
		return false
	}
	holder := entity.LeashHolder{
		EntityID: m.ID, X: m.X, Y: m.Y, Z: m.Z,
		Width: m.BbWidth(), Height: m.BbHeight(), Present: true,
	}
	cx, cy, cz := m.X, m.Y+0.5*m.BbHeight(), m.Z
	s.mobsMu.Unlock()
	ref := leashRef{holder: ent.uuid}
	moved, _ := c.tieMobsAllowed(w, c.player.EntityID, cx, cy, cz, &holder, ref, ent.uuid, ent)
	for _, e := range moved {
		s.mobsMu.Lock()
		rides := e.mob.RidesVehicle
		s.mobsMu.Unlock()
		if rides {
			s.dismountMob(e)
			s.mobsMu.Lock()
			stillRides := e.mob.RidesVehicle
			s.mobsMu.Unlock()
			if stillRides {
				s.mobLeftItsVehicle(e)
			}
		}
	}
	boats, _ := c.tieBoatsAllowed(w, c.player.EntityID, cx, cy, cz, &holder, ref, ent.uuid)
	if len(moved) == 0 && len(boats) == 0 {
		return false
	}
	for _, e := range moved {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(e.mob.ID, holder.EntityID))
	}
	for _, id := range boats {
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, holder.EntityID))
	}
	c.gameEventAtCell(rules.GameEventEntityAction,
		int32(math.Floor(holder.X)), int32(math.Floor(holder.Y)), int32(math.Floor(holder.Z)))
	s.broadcastSoundID(w, knotSoundTied, protocol.SoundCategoryNeutral, holder.X, holder.Y, holder.Z, 1.0, 1.0)
	c.useSwing = swingServer
	return true
}

// bindLeadToMob handles a use on a mob ahead of its own mobInteract: one
// the player leads is untied, its lead dropped, and one held by nobody or
// a knot is tied to the player for a lead outside creative. handled is
// true when either ran. The hold is read and written in one mob-lock
// section, so a click cannot untie a mob the tick's leash step dropped.
// vanilla: Entity.interact, Leashable.dropLeash
func (c *client) bindLeadToMob(ent *mobEntity, held protocol.ItemStack, hand int32) (handled bool, err error) {
	s := c.srv
	item := registry.Item(held.ItemID)
	// The players' entity IDs, for the knot-versus-player test below: the
	// mob lock may not be taken while mu is.
	playerIDs := s.playerEntityIDs()
	holder := c.playerHolderSnapshot()

	if c.pluginListening(pluginruntime.PlayerLeashEntity) {
		s.mobsMu.Lock()
		untie := !ent.mob.Dead && ent.mob.LeashHolderID == c.player.EntityID
		tie := !untie && !ent.mob.Dead && item == registry.ItemLead && entity.Leashable(ent.mob.Type) &&
			!playerIDs[ent.mob.LeashHolderID] && entity.LeashableBy(ent.mob, holder)
		s.mobsMu.Unlock()
		if (untie || tie) && !c.leashAllowed(ent.uuid, c.player.UUID, untie) {
			return true, nil // the lead stays in hand
		}
	}

	s.mobsMu.Lock()
	if !ent.mob.Dead && ent.mob.LeashHolderID == c.player.EntityID {
		// The untie: the lead drops at the mob before the hold is told
		// gone, the untie said at the mob. Creative keeps its lead: no
		// item is laid.
		w := ent.w
		x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
		id := ent.mob.ID
		ent.mob.RemoveLeash()
		after := s.bodyInteraction(ent)
		s.mobsMu.Unlock()
		if c.player.GameMode() != protocol.GameModeCreative {
			s.dropLeadItem(w, x, y, z)
		}
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, 0))
		s.broadcastSoundID(w,
			knotSoundUntied, protocol.SoundCategoryNeutral, x, y, z, 1.0, 1.0)
		// The untie is SUCCESS.withoutItem: awarded for no item.
		c.interactedWithEntityAdvancements(after, 0)
		return true, nil
	}
	// The tie: only a mob held by nobody or by a knot takes a player's
	// lead — one another player leads is not there to be taken.
	if ent.mob.Dead || item != registry.ItemLead || !entity.Leashable(ent.mob.Type) ||
		playerIDs[ent.mob.LeashHolderID] || !entity.LeashableBy(ent.mob, holder) {
		s.mobsMu.Unlock()
		return false, nil
	}
	w := ent.w
	x, y, z := ent.mob.X, ent.mob.Y, ent.mob.Z
	id := ent.mob.ID
	// A knot's own lead drops before the new one is spent, its item laid
	// at the mob; vanilla's spawnAtLocation ignores entity_drops.
	leftKnot := ent.mob.LeashHolderID
	riding := ent.mob.RidesVehicle
	ent.tieLeash(holder.EntityID, leashRef{holder: c.player.UUID})
	after := s.bodyInteraction(ent)
	s.mobsMu.Unlock()
	if riding {
		// A tied body seated somewhere is set off it, vanilla's
		// setLeashedTo stopping the ride: a mob's back through the
		// unlink, a hull through the boat's own leave.
		s.dismountMob(ent)
		if ent.mob.RidesVehicle {
			s.mobLeftItsVehicle(ent)
		}
	}

	if leftKnot != 0 {
		s.dropLeadItem(w, x, y, z)
		s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, 0))
	}
	s.broadcastWorld(w, protocol.IDPlaySetEntityLink, leashLinkPayload(id, holder.EntityID))
	s.broadcastSoundID(w,
		knotSoundTied, protocol.SoundCategoryNeutral, x, y, z, 1.0, 1.0)

	// InteractionResult.SUCCESS_SERVER: the server swings the arm.
	c.useSwing = swingServer
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	if leftKnot != 0 {
		if knot := s.knotByEntityID(w, leftKnot); knot != nil {
			s.discardEmptiedKnot(knot)
		}
	}
	c.interactedWithEntityAdvancements(after, held.ItemID)
	return true, nil
}

// playerEntityIDs is every player's entity ID, to tell a player holder
// from a knot. The caller may not hold the mob lock.
func (s *Server) playerEntityIDs() map[int32]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make(map[int32]bool, len(s.clients))
	for _, c := range s.clients {
		ids[c.player.EntityID] = true
	}
	return ids
}

// dropLeadItem lays a lead at the feet given, with the hop the
// five-argument ItemEntity constructor gives every drop.
// vanilla: Entity.spawnAtLocation, ItemEntity
func (s *Server) dropLeadItem(w *world.World, x, y, z float64) {
	s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemLead), Count: 1},
		x, y, z,
		s.popRng.Float64()*0.2-0.1, 0.2, s.popRng.Float64()*0.2-0.1,
		entity.BreakPickupDelay)
}

// arrowKnotHit is a knot a shaft met; mob is a mob's shot, which
// mob_griefing binds, and creative a creative player's, which an
// invulnerable knot lets through.
type arrowKnotHit struct {
	hitSound registry.Sound
	w        *world.World
	knotID   int32
	arrow    *arrowEntity
	mob      bool
	creative bool
}

// arrowHitKnot brings a knot down for a shaft, after the tick's locks;
// an invulnerable knot refuses all but a creative shooter's, and a mob's is
// refused with mob_griefing off.
// vanilla: BlockAttachedEntity.hurtServer, Entity.isInvulnerableToBase
func (s *Server) arrowHitKnot(hit arrowKnotHit) bool {
	tell, ok := s.knotTellByEntityID(hit.w, hit.knotID)
	if !ok {
		return true
	}
	if (tell.invulnerable && !hit.creative) || (hit.mob && !hit.w.BoolRule(world.RuleMobGriefing)) {
		return false
	}
	s.downKnot(tell)
	return true
}

// attackKnot handles a swing at a knot: one blow in reach brings it down,
// unless the knot is invulnerable and the hand not creative.
// vanilla: BlockAttachedEntity.skipAttackInteraction, hurtServer
func (c *client) attackKnot(target int32) bool {
	s := c.srv
	tell, ok := s.knotTellByEntityID(c.theWorld(), target)
	if !ok {
		return false
	}
	// Out of reach the attack never runs.
	creative := c.player.GameMode() == protocol.GameModeCreative
	minX, minY, minZ, maxX, maxY, maxZ := tell.box()
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return true
	}
	// vanilla: BlockAttachedEntity.skipAttackInteraction
	if !c.mayInteractAt(int32(math.Floor(tell.x)), int32(math.Floor(tell.z))) {
		return true
	}
	// vanilla: Entity.isInvulnerableToBase
	if tell.invulnerable && !creative {
		c.attackSound(soundAttackNoDamage)
		return true
	}
	s.downKnot(tell)
	return true
}

// mobLeashDrop is one lead the mob tick took off a mob, read under the mob
// lock: where the mob and the holder stood, whether it broke, and the
// holder's ID, for a knot left holding nothing.
type mobLeashDrop struct {
	w          *world.World
	mobID      int32
	holder     int32
	x, y, z    float64
	broke      bool
	hx, hy, hz float64
	// laid lays the lead whatever entity_drops says. vanilla: Leashable.dropLeash
	laid bool
	// untracked skips the link release: watchers lost the body already.
	// vanilla: ChunkMap.sendToTrackingPlayers finds no tracked entity
	untracked bool
}

// tickKnots runs the survives poll, which brings a knot whose fence is gone
// down, and settles who sees which knot, under one hold of the knot lock.
func (s *Server) tickKnots() {
	watchers := s.trackingWatchers()
	near := nearbyTracking(watchers)
	defer near.release()

	var downed []knotTell
	var out trackSends
	s.lockTick(&s.knotsMu)
	alive := s.knots[:0]
	for _, k := range s.knots {
		// checkInterval++ == 100 fires on the 101st tick.
		// vanilla: BlockAttachedEntity.tick
		k.checkInterval++
		if k.checkInterval > knotPollTicks {
			k.checkInterval = 0
			if !knotSurvives(k.w, k.x, k.y, k.z) {
				downed = append(downed, knotTellOf(k))
				continue
			}
		}
		alive = append(alive, k)
	}
	s.knots = alive
	for _, k := range alive {
		body := knotTracker.body(k)
		mt := body.teleportTick()
		for _, i := range body.watchers(near) {
			wh := watchers[i]
			if wh.c.theWorld() != k.w {
				continue // a player of another dimension never sees it
			}
			if was, seen := body.settle(wh.c, wh.x, wh.z, wh.chunkRadius, &out); seen && mt.teleported {
				body.moveTold(was, wh.c, mt, &out)
			}
		}
	}
	s.unlockTick(&s.knotsMu)

	out.send()
	for _, tell := range downed {
		s.downKnot(tell)
	}
}

// leashDropped queues a lead coming off its mob.
func (t *mobDimTick) leashDropped(fx *mobEffects, ev entity.MobEvent) {
	w := t.w
	// The lead came off: the holder walked it past the lead's
	// reach, or died, or left the world, or a knot the lead was
	// tied to came down. The telling itself runs after the
	// locks, on vanilla's own order: the snap heard at the
	// holder where it broke, the lead laid at the mob, the hold
	// told to whoever watched it.
	// vanilla: Leashable.dropLeash, Leashable.tickLeash
	fx.leashDrops = append(fx.leashDrops, mobLeashDrop{
		w: w, mobID: ev.Mob.ID, holder: ev.HolderID,
		x: ev.MobX, y: ev.MobY, z: ev.MobZ,
		broke: ev.Broke, hx: ev.X, hy: ev.Y, hz: ev.Z,
		untracked: ev.Untracked,
	})
}

const shearsSnipSound = registry.SoundItemShearsSnip

// cutHeldLeashesLocked appends the cut of every lead the body id at the
// centre (cx, cy, cz) holds. The caller holds mobsMu.
func (s *Server) cutHeldLeashesLocked(w *world.World, id int32, cx, cy, cz float64, drops []mobLeashDrop) []mobLeashDrop {
	for _, ent := range s.mobsLeashedToLocked(w, id, cx, cy, cz) {
		if holder := ent.mob.RemoveLeash(); holder != 0 {
			drops = append(drops, mobLeashDrop{w: w, mobID: ent.mob.ID, holder: holder,
				x: ent.mob.X, y: ent.mob.Y, z: ent.mob.Z})
		}
	}
	return drops
}

// finishLeashShear lays each cut lead as an item per entity_drops, posts the
// shear event at (x, y, z) from ctx and plays the snip in category. It reports
// whether any lead was cut.
// vanilla: Entity.shearOffAllLeashConnections
func (s *Server) finishLeashShear(w *world.World, drops []mobLeashDrop, x, y, z float64, ctx rules.GameEventContext, category int32) bool {
	if len(drops) == 0 {
		return false
	}
	lay := w.BoolRule(world.RuleEntityDrops)
	for _, d := range drops {
		s.finishLeashDrop(d, lay)
	}
	s.gameEvent(w, rules.GameEventShear, x, y, z, ctx)
	s.broadcastSoundID(w, shearsSnipSound, category, x, y, z, 1, 1)
	return true
}

// dropAllLeashConnections drops every lead tied to the player, boats
// included, laying the lead items per entity_drops, and reports whether any
// was cut. The caller plays the lead-break sound.
// vanilla: Entity.dropAllLeashConnections
func (c *client) dropAllLeashConnections() bool {
	w := c.theWorld()
	h := playerLeashHolder(c)
	s := c.srv
	s.mobsMu.Lock()
	drops := s.cutHeldLeashesLocked(w, c.player.EntityID, h.X, h.Y+0.5*h.Height, h.Z, nil)
	s.mobsMu.Unlock()
	// boatsMu is taken with no other server lock held, so mobsMu waits.
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w == w && !it.boat.Dead && it.holder == c.player.EntityID {
			drops = append(drops, mobLeashDrop{w: w, mobID: it.boat.ID, holder: it.untie(),
				x: it.boat.X, y: it.boat.Y, z: it.boat.Z})
		}
	}
	s.boatsMu.Unlock()
	if len(drops) == 0 {
		return false
	}
	lay := w.BoolRule(world.RuleEntityDrops)
	for _, d := range drops {
		s.finishLeashDrop(d, lay)
	}
	s.gameEvent(w, rules.GameEventShear, h.X, h.Y, h.Z, rules.GameEventContext{})
	return true
}

// finishPlayerLeashShear is finishLeashShear in the player's voice, wearing
// the shears one point when a lead was cut.
// vanilla: Entity.interact
func (c *client) finishPlayerLeashShear(w *world.World, drops []mobLeashDrop, x, y, z float64, hand int32) bool {
	if !c.srv.finishLeashShear(w, drops, x, y, z, c.gameEventContext(0), protocol.SoundCategoryPlayer) {
		return false
	}
	c.wearHeld(hand, 1)
	c.useSwing = swingPredicted
	return true
}

// cutLeashesOfMob cuts a mob's own lead and every lead it holds, returning the
// drops and the mob's position; none if it is dead.
func (s *Server) cutLeashesOfMob(ent *mobEntity) (drops []mobLeashDrop, x, y, z float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := ent.mob
	if m.Dead {
		return nil, 0, 0, 0
	}
	if holder := m.RemoveLeash(); holder != 0 {
		drops = append(drops, mobLeashDrop{w: ent.w, mobID: m.ID, holder: holder, x: m.X, y: m.Y, z: m.Z})
	}
	drops = s.cutHeldLeashesLocked(ent.w, m.ID, m.X, m.Y+0.5*m.BbHeight(), m.Z, drops)
	return drops, m.X, m.Y, m.Z
}

// shearLeashesOfMob cuts a mob's leads with a player's shears. It reports
// whether any lead was cut.
// vanilla: Entity.interact
func (c *client) shearLeashesOfMob(ent *mobEntity, hand int32) bool {
	drops, x, y, z := c.srv.cutLeashesOfMob(ent)
	return c.finishPlayerLeashShear(ent.w, drops, x, y, z, hand)
}

// cutKnotLeashes cuts every lead the knot told by t holds.
// vanilla: LeashFenceKnotEntity.interact
func (s *Server) cutKnotLeashes(t knotTell) []mobLeashDrop {
	s.mobsMu.Lock()
	drops := s.cutHeldLeashesLocked(t.w, t.id, t.x, t.y+0.5*knotHeight, t.z, nil)
	s.mobsMu.Unlock()
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == t.w && it.holder == t.id {
			drops = append(drops, mobLeashDrop{w: t.w, mobID: it.boat.ID, holder: it.untie(),
				x: it.boat.X, y: it.boat.Y, z: it.boat.Z})
		}
	}
	return drops
}

// knotsIn returns the tells of the knots crossing the box.
func (s *Server) knotsIn(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) []knotTell {
	s.knotsMu.Lock()
	defer s.knotsMu.Unlock()
	var in []knotTell
	for _, k := range s.knots {
		if k.w != w {
			continue
		}
		t := knotTellOf(k)
		x0, y0, z0, x1, y1, z1 := t.box()
		if x0 < maxX && x1 > minX && y0 < maxY && y1 > minY && z0 < maxZ && z1 > minZ {
			in = append(in, t)
		}
	}
	return in
}
