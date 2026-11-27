package server

import (
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The paintings, one of the two hanging kinds (the item frames are the
// other). A painting never moves on its own: it hangs while the wall behind
// it stands and no other hanging body runs across it.

// paintingCandidate is a variant with its size, read from the registry so a
// pack's override of a built-in hangs at the pack's size; ok is false for a
// variant without one.
func paintingCandidate(variant int32) (rules.PaintingCandidate, bool) {
	width, height, ok := registry.PaintingSize(variant)
	return rules.PaintingCandidate{Variant: variant, Width: width, Height: height}, ok
}

// paintingEntity is one painting hung in a world. x, y, z is the cell it
// hangs in, in front of the wall (vanilla's block_pos), and dir the way it
// faces in the packets' numbering. The box is its variant's sliver there.
type paintingEntity struct {
	entity.Filing
	id      int32
	uuid    uuid.UUID
	w       *world.World
	variant int32
	dir     int32
	x, y, z int32
	// width and height are the variant's size in blocks, read once at
	// creation: a pack entry's comes from the registry's data.
	width, height int32

	minX, minY, minZ, maxX, maxY, maxZ float64

	// checkInterval counts ticks toward the periodic survives poll.
	checkInterval int
	// invulnerable is the Invulnerable tag, which turns away every hurt but
	// a creative player's.
	invulnerable bool
	bodyTrack[struct{}]

	// tags are the scoreboard tags. Guarded by the painting lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// paintingTracker tracks the paintings, measured from the hanging cell's
// corner. A painting has no movement schedule.
var paintingTracker = trackedKind[*paintingEntity, struct{}]{
	rangeChunks: entity.PaintingTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.paintingsMu },
	list:        func(s *Server) *[]*paintingEntity { return &s.paintings },
	chunks:      func(s *Server) *entity.ChunkIndex[*paintingEntity] { return &s.paintingChunks },
	filing:      func(it *paintingEntity) *entity.Filing { return &it.Filing },
	uuid:        func(it *paintingEntity) uuid.UUID { return it.uuid },
	anchor:      func(it *paintingEntity) (float64, float64) { return paintingCentreX(it), paintingCentreZ(it) },
	track:       func(p *paintingEntity) *bodyTrack[struct{}] { return &p.bodyTrack },
	world:       func(p *paintingEntity) *world.World { return p.w },
	id:          func(p *paintingEntity) int32 { return p.id },
	dead:        func(*paintingEntity) bool { return false },
	// The client floors received positions to the anchor, then rebuilds the box.
	// vanilla: Painting.trackingPosition, BlockAttachedEntity.setPos
	pose: func(p *paintingEntity) bodyPose {
		return bodyPose{x: float64(p.x), y: float64(p.y), z: float64(p.z)}
	},
	pairing: paintingPairing,
}

// sendPainting sends each watcher the pairing built under the painting
// lock, so nothing here re-reads a painting a /tp may move.
func sendPainting(w *world.World, shown []outbound, watching []*client) {
	sendPacketsToAll(w, watching, shown)
}

// newPainting builds a painting hung at the cell given, with its box.
func newPainting(id int32, identity uuid.UUID, w *world.World, x, y, z, dir, variant, width, height int32) *paintingEntity {
	p := &paintingEntity{
		id: id, uuid: identity, w: w,
		variant: variant, dir: dir, x: x, y: y, z: z,
		width: width, height: height,
	}
	p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ = rules.PaintingBox(x, y, z, dir, width, height)
	return p
}

// isDiode reports the repeater and the comparator, the half-height blocks
// a hanging body accepts as its wall.
// vanilla: DiodeBlock.isDiode
func isDiode(state uint16) bool {
	block := registry.BlockOf(state)
	return block == registry.BlockRepeater || block == registry.BlockComparator
}

// paintingSurvives reports whether a painting of the size given survives
// in the cell given: nothing solid in its sliver, a solid wall behind every
// cell it covers, and none of others and frames across it. The cross check
// is a plain box intersect: the pair canCoexist throws out is the pair whose
// boxes meet the same wall the same way.
// vanilla: HangingEntity.survives, HangingEntity.canCoexist
func paintingSurvives(w *world.World, x, y, z, dir, width, height int32, others []*paintingEntity, frames []*frameEntity) bool {
	minX, minY, minZ, maxX, maxY, maxZ := rules.PaintingBox(x, y, z, dir, width, height)
	if w.BoxCollides(minX, minY, minZ, maxX, maxY, maxZ) {
		return false
	}
	for _, cell := range rules.PaintingSupportCells(x, y, z, dir, width, height) {
		if state := w.BlockAt(cell[0], cell[1], cell[2]); !registry.IsSolid(state) && !isDiode(state) {
			return false
		}
	}
	for _, p := range others {
		if rules.BoxesIntersect(
			[3]float64{minX, minY, minZ}, [3]float64{maxX, maxY, maxZ},
			[3]float64{p.minX, p.minY, p.minZ}, [3]float64{p.maxX, p.maxY, p.maxZ}) {
			return false
		}
	}
	for _, o := range frames {
		if rules.BoxesIntersect(
			[3]float64{minX, minY, minZ}, [3]float64{maxX, maxY, maxZ},
			[3]float64{o.minX, o.minY, o.minZ}, [3]float64{o.maxX, o.maxY, o.maxZ}) {
			return false
		}
	}
	return true
}

// hangPainting places a painting of create's roll among the placeable
// variants that survive the cell, rolled under the painting lock so two
// placements cannot take the same span of wall. ok is false when nothing
// fits.
// vanilla: HangingEntityItem.useOn, Painting.create
func (s *Server) hangPainting(w *world.World, x, y, z, dir int32, stack protocol.ItemStack) (paintingTell, bool) {
	return s.hangPaintingWhere(w, x, y, z, dir, stack,
		func(id int32, others []*paintingEntity, frames []*frameEntity) (rules.PaintingCandidate, bool) {
			dice := newDice(s.entitySeed(id))
			// The live tag, so a pack's minecraft:placeable counts.
			placeable := registry.PaintingVariantTagIDs("minecraft:placeable")
			candidates := make([]rules.PaintingCandidate, 0, len(placeable))
			for _, variant := range placeable {
				if c, ok := paintingCandidate(variant); ok {
					candidates = append(candidates, c)
				}
			}
			return rules.ChoosePainting(candidates,
				func(c rules.PaintingCandidate) bool {
					return paintingSurvives(w, x, y, z, dir, c.Width, c.Height, others, frames)
				},
				func(n int) int { return dice.Intn(n) })
		})
}

// hangPaintingVariant places the one variant a stack's painting/variant
// component names, in place of create's roll. ok is false when it does not
// fit.
// vanilla: HangingEntityItem.useOn, Painting.applyImplicitComponent
func (s *Server) hangPaintingVariant(w *world.World, x, y, z, dir int32, candidate rules.PaintingCandidate, stack protocol.ItemStack) (paintingTell, bool) {
	return s.hangPaintingWhere(w, x, y, z, dir, stack,
		func(_ int32, others []*paintingEntity, frames []*frameEntity) (rules.PaintingCandidate, bool) {
			if !paintingSurvives(w, x, y, z, dir, candidate.Width, candidate.Height, others, frames) {
				return rules.PaintingCandidate{}, false
			}
			return candidate, true
		})
}

// hangPaintingWhere hangs what choose picks, with the stack's entity data loaded, registered and shown under the
// locks and sent after them. choose is handed the new ID (create's seed),
// the world's paintings and its frames, and answers false when nothing
// fits. The tell carries the placement sound's centre.
func (s *Server) hangPaintingWhere(w *world.World, x, y, z, dir int32, stack protocol.ItemStack, choose func(id int32, others []*paintingEntity, frames []*frameEntity) (rules.PaintingCandidate, bool)) (paintingTell, bool) {
	id := s.nextEntity()
	// Read under s.mu, held through the registration.
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()

	s.paintingsMu.Lock()
	s.framesMu.Lock()
	others, frames := s.hangingNearCell(w, x, z)
	chosen, ok := choose(id, others, frames)
	if !ok {
		s.framesMu.Unlock()
		s.paintingsMu.Unlock()
		s.mu.RUnlock()
		return paintingTell{}, false
	}
	p := newPainting(id, uuid.New(), w, x, y, z, dir, chosen.Variant, chosen.Width, chosen.Height)
	if tag := itemEntityTag(paintingTag(p).Entries(), stack, protocol.EntityTypePainting); tag != nil {
		if loaded := paintingOfTag(s, w, tag); loaded != nil {
			loaded.id = id
			p = loaded
			others, frames := s.hangingNearCell(w, p.x, p.z)
			if !paintingSurvives(w, p.x, p.y, p.z, p.dir, p.width, p.height, others, frames) {
				s.framesMu.Unlock()
				s.paintingsMu.Unlock()
				s.mu.RUnlock()
				return paintingTell{}, false
			}
		}
	}
	s.registerPaintingLocked(p)
	watching := paintingTracker.showLocked(p, watchers)
	tell := paintingTellOf(p)
	shown := paintingPairing(p)
	s.framesMu.Unlock()
	s.paintingsMu.Unlock()
	s.mu.RUnlock()

	sendPainting(w, shown, watching)
	return tell, true
}

// summonPainting hangs a /summon's painting through a wake's loader, in the
// cell the position floors to, which the tag's block_pos never moves. A
// load that comes to nothing and a duplicate UUID are the refusals. One
// with no wall waits for the survives poll.
// vanilla: SummonCommand.createEntity, Painting.readAdditionalSaveData
func (s *Server) summonPainting(w *world.World, x, y, z float64, tag nbt.Compound) (*paintingEntity, error) {
	compound := summonTag(tag, paintingEntityID, x, y, z)
	compound["block_pos"] = nbt.IntArray{rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)}
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	p := paintingOfTag(s, w, compound)
	w.Release(pos)
	if p == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(p.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	s.registerPainting(p)
	return p, nil
}

// registerPainting puts a woken or summoned painting into the world and
// shows it to every player in range.
func (s *Server) registerPainting(p *paintingEntity) {
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()

	s.paintingsMu.Lock()
	s.registerPaintingLocked(p)
	watching := paintingTracker.showLocked(p, watchers)
	shown := paintingPairing(p)
	s.paintingsMu.Unlock()
	s.mu.RUnlock()

	sendPainting(p.w, shown, watching)
}

// registerPaintingLocked adds p to the list. The painting lock is held.
func (s *Server) registerPaintingLocked(p *paintingEntity) {
	s.paintings = append(s.paintings, p)
	paintingTracker.file(s, p)
}

// hangingNear collects the listed paintings and frames of w filed in the
// chunks covering the box widened by 2 blocks for paintings (a 4x4 painting's
// centre, the point it is filed under, is at most 2 blocks from any cell it
// covers) and by 1 for frames. Both
// kind locks are held. Only those whose box overlaps the box in x and z are
// returned. Bodies a listener still holds are not listed yet.
// vanilla: Level.hasEntities(EntityTypeTest.forClass(HangingEntity.class), ...)
func (s *Server) hangingNear(w *world.World, minX, minZ, maxX, maxZ float64) ([]*paintingEntity, []*frameEntity) {
	var paintings []*paintingEntity
	var frames []*frameEntity
	for pos := range s.paintingChunks.ChunksIn(entity.ChunkAt(minX-2, minZ-2), entity.ChunkAt(maxX+2, maxZ+2)) {
		for p := range s.paintingChunks.In(pos) {
			if p.w == w && p.maxX >= minX && p.minX <= maxX && p.maxZ >= minZ && p.minZ <= maxZ && !p.held && !paintingTracker.dead(p) {
				paintings = append(paintings, p)
			}
		}
	}
	for pos := range s.frameChunks.ChunksIn(entity.ChunkAt(minX-1, minZ-1), entity.ChunkAt(maxX+1, maxZ+1)) {
		for f := range s.frameChunks.In(pos) {
			if f.w == w && f.maxX >= minX && f.minX <= maxX && f.maxZ >= minZ && f.minZ <= maxZ && !f.held && !frameTracker.dead(f) {
				frames = append(frames, f)
			}
		}
	}
	return paintings, frames
}

// hangingNearCell is hangingNear around the cell and the widest painting.
func (s *Server) hangingNearCell(w *world.World, x, z int32) ([]*paintingEntity, []*frameEntity) {
	return s.hangingNear(w, float64(x-4), float64(z-4), float64(x+5), float64(z+5))
}

// placePainting handles a use of the painting item on a face. A stack's
// painting/variant component picks the variant; one that does not fit
// hangs nothing. handled is true for a painting item, which answers the use
// even when nothing hangs.
// vanilla: HangingEntityItem.useOn
func (c *client) placePainting(hand, x, y, z, face int32) (handled bool, err error) {
	c.useSwing = swingNone
	stack := c.heldItem(hand)
	if registry.Item(stack.ItemID) != registry.ItemPainting {
		return false, nil
	}
	// vanilla: HangingEntityItem.mayPlace
	if face == protocol.FaceTop || face == protocol.FaceBottom {
		c.useSwing = swingNone
		return true, nil
	}
	w := c.theWorld()
	off := protocol.FaceOffset[face]
	hx, hy, hz := x+off[0], y+off[1], z+off[2]
	var tell paintingTell
	var ok bool
	if variant, named := stack.PaintingVariantOf(); named {
		if candidate, sized := paintingCandidate(variant); sized {
			tell, ok = c.srv.hangPaintingVariant(w, hx, hy, hz, face, candidate, stack)
		} else {
			tell, ok = c.srv.hangPainting(w, hx, hy, hz, face, stack)
		}
	} else {
		tell, ok = c.srv.hangPainting(w, hx, hy, hz, face, stack)
	}
	if !ok {
		// Nothing fits where it would hang; the painting stays in hand.
		c.useSwing = swingNone
		return true, nil
	}
	if !c.srv.hangingPlaced(c, w, tell.uuid, tell.id, x, y, z, face) {
		return true, nil
	}
	// The placer hears it too: unlike a block's, it is not predicted.
	// vanilla: Painting.playPlacementSound
	if !tell.silent {
		c.srv.broadcastNear(w, tell.centreX, tell.centreY, tell.centreZ,
			soundRange(1.0), protocol.IDPlaySoundEffect,
			soundEffectIDAt(soundPaintingPlace, protocol.SoundCategoryNeutral,
				tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0))
	}
	c.gameEventAt(rules.GameEventEntityPlace, tell.centreX, tell.centreY, tell.centreZ, 0)
	c.useSwing = swingPredicted
	c.awardUsedHeld(hand)
	// The painting is spent outside creative. vanilla: ItemStack.consume
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	return true, nil
}

// attackPainting handles a swing at a painting: any hurt takes it down, and
// skipAttackInteraction returns before the blow's cooldown, feedback, wear
// or exhaustion. It reports true for a painting in reach or not, the swing
// spent on it. A creative hand lays no item.
// vanilla: BlockAttachedEntity.skipAttackInteraction, Painting.dropItem
func (c *client) attackPainting(target int32) bool {
	s := c.srv
	s.paintingsMu.Lock()
	p := s.paintingByIDLocked(c.theWorld(), target)
	if p == nil {
		s.paintingsMu.Unlock()
		return false
	}
	// Out of reach the attack never runs.
	creative := c.player.GameMode() == protocol.GameModeCreative
	inReach := c.withinAttackRange(p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ)
	// vanilla: BlockAttachedEntity.skipAttackInteraction
	if inReach && !c.mayInteractAt(p.x, p.z) {
		s.paintingsMu.Unlock()
		return true
	}
	// vanilla: Entity.isInvulnerableToBase
	refused := inReach && p.invulnerable && !creative
	var tell paintingTell
	if inReach && !refused {
		tell = paintingTellOf(p)
		if !s.removePaintingAsked(p, removedByEntity(c.player.UUID)) {
			return true
		}
	} else {
		s.paintingsMu.Unlock()
	}
	switch {
	case refused:
		// Player.attack plays the no-damage sound for a refused hurt.
		c.attackSound(soundAttackNoDamage)
	case inReach:
		s.breakPaintingTail(tell, creative)
	}
	return true
}

// paintingByIDLocked finds the painting an entity ID names, nil for one
// gone. The painting lock is held.
func (s *Server) paintingByIDLocked(w *world.World, id int32) *paintingEntity {
	for _, p := range s.paintings {
		if p.id == id && p.w == w {
			return p
		}
	}
	return nil
}

// unlistPaintingLocked takes a painting out of the list,
// keeping its scores for a twin that carries its UUID on. The painting lock is
// held.
func (s *Server) unlistPaintingLocked(p *paintingEntity) {
	paintingTracker.unfile(p)
	s.paintings = slices.DeleteFunc(s.paintings, func(o *paintingEntity) bool { return o == p })
}

// removePaintingLocked unlists a destroyed painting and forgets its scores.
// The painting lock is held.
func (s *Server) removePaintingLocked(p *paintingEntity) {
	s.unlistPaintingLocked(p)
	s.entityDestroyed(p.uuid)
}

// killPainting discards a painting with no break sound and no drop.
// vanilla: Entity.kill
func (s *Server) killPainting(p *paintingEntity) {
	s.paintingsMu.Lock()
	s.removePaintingLocked(p)
	s.paintingsMu.Unlock()
	s.broadcastWorld(p.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{p.id}))
}

// teleportPainting rehangs a painting in the cell under the position given
// and marks it for the next tracking pass to tell its watchers the box
// centre.
// vanilla: Entity.teleportTo
func (s *Server) teleportPainting(p *paintingEntity, to player.Position, by string) {
	x, y, z := int32(math.Floor(to.X)), int32(math.Floor(to.Y)), int32(math.Floor(to.Z))
	paintingTracker.teleport(s, p, func() {
		p.x, p.y, p.z = x, y, z
		p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ = rules.PaintingBox(x, y, z, p.dir, p.width, p.height)
		p.Refile(entity.ChunkAt(paintingCentreX(p), paintingCentreZ(p)))
	})
}

// paintingTwinAt builds the painting that continues this one in another
// dimension, hanging in the cell under pos: the saved compound loaded
// again with a new ID and the same UUID. The caller holds the paintings'
// lock or has taken the painting out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) paintingTwinAt(p *paintingEntity, to *dimension, pos player.Position) *paintingEntity {
	tag := paintingTag(p)
	// The loader validates block_pos against Pos before restoring the anchor.
	tag.Set("Pos", vec3Tag(pos.X, pos.Y, pos.Z))
	tag.Set("block_pos", nbt.IntArray{rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)})
	return paintingOfTag(s, to.w, tag.Entries())
}

// breakPaintingTail is a painting's end after the lock: the removal, then,
// with entity_drops on, the break sound and the item spawnHangingDrop
// throws. dropless is a creative blow or blast, whose dropItem lays none.
// vanilla: BlockAttachedEntity.kill, Painting.dropItem
func (s *Server) breakPaintingTail(tell paintingTell, dropless bool) {
	s.broadcastWorld(tell.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{tell.id}))
	if !tell.w.BoolRule(world.RuleEntityDrops) {
		return
	}
	if !tell.silent {
		s.broadcastNear(tell.w, tell.centreX, tell.centreY, tell.centreZ,
			soundRange(1.0), protocol.IDPlaySoundEffect,
			soundEffectIDAt(soundPaintingBreak, protocol.SoundCategoryNeutral,
				tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0))
	}
	if dropless {
		return
	}
	item := int32(registry.ItemPainting)
	s.spawnHangingDrop(tell.w, tell.dir, tell.centreX, tell.centreY, tell.centreZ,
		protocol.ItemStack{ItemID: item, Count: 1, CustomName: customNameText(tell.custom.tag())})
}

// paintingHit is a painting a projectile's sweep met. dropless is a
// creative shooter, the one an invulnerable painting lets through, mob a
// mob's projectile, which mob_griefing binds, and arrow the shaft whose hurt
// it is, nil for a thrown body.
type paintingHit struct {
	hitSound   registry.Sound
	damageType registry.DamageType
	w          *world.World
	paintingID int32
	dropless   bool
	mob        bool
	arrow      *arrowEntity
}

// hitPainting lands a projectile's hurt on a painting, which ends it like a
// blow; an invulnerable painting refuses all but a creative shooter's, and a
// mob's lands nothing with mob_griefing off. It reports whether a
// shaft is spent: a landed hurt, or a painting already gone. A refused hurt
// is the caller's to bounce.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func (s *Server) hitPainting(hit paintingHit) bool {
	s.paintingsMu.Lock()
	p := s.paintingByIDLocked(hit.w, hit.paintingID)
	if p == nil {
		// Taken down since the sweep: the shaft is spent all the same.
		s.paintingsMu.Unlock()
		return true
	}
	// vanilla: Entity.isInvulnerableToBase
	if (p.invulnerable && !hit.dropless && !rules.InvulnerabilityBypassed(hit.damageType)) || (hit.mob && !p.w.BoolRule(world.RuleMobGriefing)) {
		s.paintingsMu.Unlock()
		return false
	}
	tell := paintingTellOf(p)
	by := hangingRemover{cause: plugin.HangingBreakEntity}
	if hit.arrow != nil {
		by = removedByEntity(hit.arrow.ownerUUID)
	}
	if !s.removePaintingAsked(p, by) {
		return false
	}
	s.breakPaintingTail(tell, hit.dropless)
	return true
}

// paintingTell is what an end or a move reads after the painting lock,
// copied under it so a /tp cannot be read halfway.
type paintingTell struct {
	w       *world.World
	id      int32
	uuid    uuid.UUID
	dir     int32
	custom  customName
	silent  bool
	centreX float64
	centreY float64
	centreZ float64
}

// paintingTellOf copies the fields a tell reads out of p. The painting lock
// is held.
func paintingTellOf(p *paintingEntity) paintingTell {
	return paintingTell{
		w: p.w, id: p.id, uuid: p.uuid, dir: p.dir, custom: p.custom, silent: p.look.silent,
		centreX: paintingCentreX(p), centreY: paintingCentreY(p), centreZ: paintingCentreZ(p),
	}
}

// paintingSnapshot is a tick's paintings by world: the slivers a
// projectile's sweep cuts down, and the IDs a delayed hurt matches back.
type paintingSnapshot struct {
	byWorld   map[*world.World][]entitySliver
	byIDWorld map[*world.World]map[int32]bool
}

// snapshotPaintingsLocked copies the painting list into a paintingSnapshot.
// The painting lock is held.
func (s *Server) snapshotPaintingsLocked() paintingSnapshot {
	snap := paintingSnapshot{
		byWorld:   make(map[*world.World][]entitySliver),
		byIDWorld: make(map[*world.World]map[int32]bool),
	}
	for _, p := range s.paintings {
		sliver := entitySliver{
			id:   p.id,
			minX: p.minX, minY: p.minY, minZ: p.minZ,
			maxX: p.maxX, maxY: p.maxY, maxZ: p.maxZ,
		}
		snap.byWorld[p.w] = append(snap.byWorld[p.w], sliver)
		byID := snap.byIDWorld[p.w]
		if byID == nil {
			byID = make(map[int32]bool)
			snap.byIDWorld[p.w] = byID
		}
		byID[p.id] = true
	}
	return snap
}

// pollPaintings brings down, every 101 ticks of its life, a painting whose
// survives check fails. Block changes do not trigger this check.
// vanilla: BlockAttachedEntity.tick
func (s *Server) pollPaintings() {
	s.lockTick(&s.paintingsMu)
	s.lockTick(&s.framesMu)
	var doomed []*paintingEntity
	var tells []paintingTell
	for _, p := range s.paintings {
		p.checkInterval++
		if p.checkInterval <= 100 {
			continue
		}
		p.checkInterval = 0
		others, frames := s.hangingNear(p.w, p.minX, p.minZ, p.maxX, p.maxZ)
		others = slices.DeleteFunc(others, func(o *paintingEntity) bool { return o == p })
		if !paintingSurvives(p.w, p.x, p.y, p.z, p.dir, p.width, p.height, others, frames) {
			doomed = append(doomed, p)
			tells = append(tells, paintingTellOf(p))
		}
	}
	s.unlockTick(&s.framesMu)
	s.unlockTick(&s.paintingsMu)
	for i, p := range doomed {
		s.paintingsMu.Lock()
		if s.removePaintingAsked(p, hangingRemover{}) {
			s.breakPaintingTail(tells[i], false)
		}
	}
}

// paintingPairing is what shows a painting: the spawn, then the state its
// metadata carries.
func paintingPairing(p *paintingEntity) []outbound {
	return spawnAndMetadata(spawnPaintingPayload(p), paintingMetadataPayload(p))
}

// spawnPaintingPayload is a painting's Spawn Entity: the direction in the
// data field, and the hanging cell's corner rather than the box centre as
// the position; the client rebuilds the rest from the direction and size.
// vanilla: Painting.getAddEntityPacket
func spawnPaintingPayload(p *paintingEntity) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.id)
	b.PutUUID(p.uuid)
	b.PutVarInt(protocol.EntityTypePainting)
	b.PutF64(float64(p.x))
	b.PutF64(float64(p.y))
	b.PutF64(float64(p.z))
	b.PutLpVec3(0, 0, 0) // velocity
	b.PutAngle(0)        // pitch
	b.PutAngle(float32(painting2DOf(p.dir) * 90))
	b.PutAngle(0) // head yaw
	b.PutVarInt(p.dir)
	return b.Bytes()
}

// paintingMetadataPayload is the metadata a painting is drawn with: the
// direction, and the variant in ID-or-inline form, a registry ID plus one.
func paintingMetadataPayload(p *paintingEntity) []byte {
	return paintingMetadataTable(p, nil)
}

// paintingMetadataState is the typed baseline for a command's metadata delta.
type paintingMetadataState struct {
	dir, variant int32
	name         customName
	silent       bool
	flags        uint8
	noGravity    bool
}

func paintingMetadataStateOf(p *paintingEntity) paintingMetadataState {
	return paintingMetadataState{dir: p.dir, variant: p.variant, name: p.custom, silent: p.look.silent, flags: p.look.glowFlags(), noGravity: p.look.noGravity}
}

// paintingMetadataTable writes spawn fields (was == nil) or fields changed by a command.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func paintingMetadataTable(p *paintingEntity, was *paintingMetadataState) []byte {
	now := paintingMetadataStateOf(p)
	spawn := was == nil
	if spawn {
		was = &paintingMetadataState{}
	}
	var b protocol.Buffer
	b.PutVarInt(p.id)
	head := b.Len()
	if now.flags != was.flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(now.flags)
	}
	p.look.putSilent(&b, was.silent)
	p.look.putNoGravity(&b, was.noGravity)
	if spawn || now.dir != was.dir {
		b.PutMetadata(protocol.MetadataIndexHangingDirection, protocol.MetadataDirection)
		b.PutVarInt(now.dir)
	}
	if spawn || now.variant != was.variant {
		b.PutMetadata(protocol.MetadataIndexPaintingVariant, protocol.MetadataPaintingVariant)
		b.PutVarInt(now.variant + 1)
	}
	now.name.putMetadata(&b, was.name)
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// painting2DOf numbers a direction south 0, west 1, north 2, east 3: the
// saved facing, and the body's yaw in quarter turns.
// vanilla: Direction.get2DDataValue
func painting2DOf(dir int32) int32 {
	switch dir {
	case protocol.FaceSouth:
		return 0
	case protocol.FaceWest:
		return 1
	case protocol.FaceNorth:
		return 2
	default:
		return 3
	}
}

// directionOfPainting2D is painting2DOf's inverse, for a saved facing.
// vanilla: Direction.from2DDataValue
func directionOfPainting2D(twoD int32) int32 {
	switch twoD % 4 {
	case 0:
		return protocol.FaceSouth
	case 1:
		return protocol.FaceWest
	case 2:
		return protocol.FaceNorth
	default:
		return protocol.FaceEast
	}
}

// the two sounds of a painting's arrival and its end.
const (
	soundPaintingPlace = registry.SoundEntityPaintingPlace
	soundPaintingBreak = registry.SoundEntityPaintingBreak
)

// paintingCentreX, Y and Z are the box's middle, where its sounds play and
// its drop falls from.
func paintingCentreX(p *paintingEntity) float64 { return (p.minX + p.maxX) / 2 }
func paintingCentreY(p *paintingEntity) float64 { return (p.minY + p.maxY) / 2 }
func paintingCentreZ(p *paintingEntity) float64 { return (p.minZ + p.maxZ) / 2 }

// tickPaintings runs the survives poll, then settles who sees which
// painting; a frozen world runs the second half alone.
func (s *Server) tickPaintings() {
	s.pollPaintings()
	paintingTracker.frozenPass(s, s.trackingWatchers())
}
