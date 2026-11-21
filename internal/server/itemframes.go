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

// The item frames: hung like a painting but on any face, the flat ones
// included, and outliving every end but a creative blow while its save says
// Fixed. It holds one item, turned through eight positions, which a
// comparator reads through the block behind it.

type frameEntity struct {
	entity.Filing
	id   int32
	uuid uuid.UUID
	w    *world.World
	// glow names the kind: minecraft:glow_item_frame rather than plain,
	// drawn lit and with its own five sounds and its own drop item.
	glow    bool
	dir     int32
	x, y, z int32

	// minX through maxZ bound the sliver the frame occupies in the cell
	// it hangs in; cells are the hanging cell and its support, the two a
	// change in might unhang it, indexed in the server's map so a block
	// change finds the frames it concerns.
	minX, minY, minZ, maxX, maxY, maxZ float64
	cells                              [][3]int32

	// item is the stack the frame holds, kept to one of whatever the use
	// filled it from; rotation is the position the item is drawn at, the
	// eight a use cycles through. fixed and invisible are the save's
	// flags: the first takes the frame out of the survives, use and drop
	// rules, the second the wire draws around. dropChance is the save's
	// probability the held item is laid when it comes out.
	item         protocol.ItemStack
	rotation     int32
	fixed        bool
	invulnerable bool
	invisible    bool
	dropChance   float32

	// checkInterval counts ticks toward the periodic survives poll.
	checkInterval int
	bodyTrack[struct{}]

	// tags are the scoreboard tags. Guarded by the frame lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// frameTracker tracks the frames, measured from the hanging cell's corner.
// A frame has no movement schedule.
var frameTracker = trackedKind[*frameEntity, struct{}]{
	rangeChunks: entity.ItemFrameTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.framesMu },
	list:        func(s *Server) *[]*frameEntity { return &s.frames },
	chunks:      func(s *Server) *entity.ChunkIndex[*frameEntity] { return &s.frameChunks },
	filing:      func(it *frameEntity) *entity.Filing { return &it.Filing },
	anchor:      func(it *frameEntity) (float64, float64) { return frameCentreX(it), frameCentreZ(it) },
	track:       func(f *frameEntity) *bodyTrack[struct{}] { return &f.bodyTrack },
	world:       func(f *frameEntity) *world.World { return f.w },
	id:          func(f *frameEntity) int32 { return f.id },
	dead:        func(*frameEntity) bool { return false },
	pose: func(f *frameEntity) bodyPose {
		return bodyPose{x: float64(f.x), y: float64(f.y), z: float64(f.z)}
	},
	tellPose: frameCentre,
	pairing:  framePairing,
}

// frameSliverOf copies the shape fields a projectile reads out of f. The
// frame lock is held.
func frameSliverOf(f *frameEntity) entitySliver {
	return entitySliver{
		id: f.id, typ: frameEntityType(f), fixed: f.fixed,
		minX: f.minX, minY: f.minY, minZ: f.minZ,
		maxX: f.maxX, maxY: f.maxY, maxZ: f.maxZ,
	}
}

// snapshotFramesLocked copies the frame list into slivers grouped by
// world. The frame lock is held.
func (s *Server) snapshotFramesLocked() map[*world.World][]entitySliver {
	byWorld := make(map[*world.World][]entitySliver)
	for _, f := range s.frames {
		byWorld[f.w] = append(byWorld[f.w], frameSliverOf(f))
	}
	return byWorld
}

// frameTell is what a change or an end reads after the frame lock, copied
// under it so a /tp or a use cannot be read halfway.
type frameTell struct {
	w            *world.World
	id           int32
	uuid         uuid.UUID
	glow         bool
	dir          int32
	custom       customName
	fixed        bool
	invulnerable bool
	silent       bool
	item         protocol.ItemStack
	dropChance   float32
	x, y, z      int32
	centreX      float64
	centreY      float64
	centreZ      float64
}

// frameTellOf copies the fields a tell reads out of f. The frame lock is
// held.
func frameTellOf(f *frameEntity) frameTell {
	return frameTell{
		w: f.w, id: f.id, uuid: f.uuid, glow: f.glow, dir: f.dir, custom: f.custom, fixed: f.fixed, silent: f.look.silent,
		item: f.item, dropChance: f.dropChance, x: f.x, y: f.y, z: f.z,
		centreX: frameCentreX(f), centreY: frameCentreY(f), centreZ: frameCentreZ(f),
	}
}

// frameCell is one block cell in the server's frame index.
type frameCell struct {
	w       *world.World
	x, y, z int32
}

// newFrame builds a frame hung at the cell given, with its sliver and
// cells.
func newFrame(id int32, identity uuid.UUID, w *world.World, x, y, z, dir int32, glow bool) *frameEntity {
	f := &frameEntity{id: id, uuid: identity, w: w, glow: glow, dropChance: 1}
	f.hangAt(x, y, z, dir)
	return f
}

// hangAt sets the frame's cell, direction, box and cells, leaving the
// server's index to the caller.
// vanilla: ItemFrame.setDirection
func (f *frameEntity) hangAt(x, y, z, dir int32) {
	f.x, f.y, f.z, f.dir = x, y, z, dir
	f.reshape()
	f.cells = [][3]int32{{x, y, z}, rules.ItemFrameSupportCell(x, y, z, dir)}
}

// setItem puts stack in the frame and resizes its box to it. The frame
// lock is held, or the frame is not yet registered.
// vanilla: ItemFrame.onItemChanged
func (f *frameEntity) setItem(stack protocol.ItemStack) {
	f.item = stack
	f.reshape()
}

// reshape sets the frame's box from its hanging cell, facing and item: the
// whole face while it holds a map (a stack with a map_id).
// vanilla: ItemFrame.hasFramedMap
func (f *frameEntity) reshape() {
	_, framedMap := f.item.MapIDOf()
	f.minX, f.minY, f.minZ, f.maxX, f.maxY, f.maxZ = rules.ItemFrameBox(f.x, f.y, f.z, f.dir, framedMap)
	f.Refile(entity.ChunkAt(frameCentreX(f), frameCentreZ(f)))
}

// frameSurvives reports whether a frame survives in the cell given: nothing
// solid in its sliver, a solid block behind it (a diode only behind a wall
// frame), and no painting or other frame across it. A fixed frame always
// survives. The cross check is a plain box intersect: two hanging bodies
// whose boxes meet face the same way, the pair canCoexist throws out.
// vanilla: ItemFrame.survives, HangingEntity.canCoexist
func frameSurvives(w *world.World, x, y, z, dir int32, fixed bool, paintings []*paintingEntity, frames []*frameEntity) bool {
	if fixed {
		return true
	}
	minX, minY, minZ, maxX, maxY, maxZ := rules.ItemFrameBox(x, y, z, dir, false)
	if w.BoxCollides(minX, minY, minZ, maxX, maxY, maxZ) {
		return false
	}
	support := rules.ItemFrameSupportCell(x, y, z, dir)
	state := w.BlockAt(support[0], support[1], support[2])
	onWall := protocol.FaceOffset[dir][1] == 0
	supported := registry.IsSolid(state) || isDiode(state) && onWall
	if !supported {
		return false
	}
	for _, p := range paintings {
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

// hangFrame hangs a frame in the cell given, registered and shown under the
// painting and frame locks so a placement sees every hanging body of the
// wall, and sent after them. ok is false when it does not survive there.
// The tell carries the placement sound's glow and centre.
// vanilla: HangingEntityItem.useOn
func (s *Server) hangFrame(w *world.World, x, y, z, dir int32, glow bool, stack protocol.ItemStack) (frameTell, bool) {
	id := s.nextEntity()
	// Read under s.mu, held through the registration.
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()

	s.paintingsMu.Lock()
	s.framesMu.Lock()
	f := newFrame(id, uuid.New(), w, x, y, z, dir, glow)
	// The stack's data loads before the survives check, so a Fixed frame
	// hangs anywhere. vanilla: EntityType.createDefaultStackConfig
	if tag := itemEntityTag(frameTag(f).Entries(), stack, frameEntityType(f)); tag != nil {
		if loaded := frameOfTag(s, w, tag); loaded != nil {
			loaded.id = id
			f = loaded
		}
	}
	if !frameSurvives(f.w, f.x, f.y, f.z, f.dir, f.fixed,
		s.paintingsOfWorldExceptLocked(w, nil), s.framesOfWorldExceptLocked(w, nil)) {
		s.framesMu.Unlock()
		s.paintingsMu.Unlock()
		s.mu.RUnlock()
		return frameTell{}, false
	}
	s.registerFrameLocked(f)
	watching := frameTracker.showLocked(f, watchers)
	tell := frameTellOf(f)
	shown := framePairing(f)
	s.framesMu.Unlock()
	s.paintingsMu.Unlock()
	s.mu.RUnlock()

	sendFrame(w, shown, watching)
	return tell, true
}

// summonFrame stands a /summon's item frame up at the position given: read
// through the same loader a saved frame's wake reads, hung in the cell the
// command position floors to, facing and holding what the tag said. A load
// that comes to nothing and a tag whose UUID is already a live body's are
// the two refusals, the notifier the caller's; a frame that hangs where no
// support holds it waits out the periodic survives poll.
// vanilla: SummonCommand.createEntity, ItemFrame.readAdditionalSaveData
func (s *Server) summonFrame(w *world.World, name string, x, y, z float64, tag nbt.Compound) (*frameEntity, error) {
	compound := summonTag(tag, name, x, y, z)
	compound["block_pos"] = nbt.IntArray{rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)}
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	w.Acquire(pos)
	f := frameOfTag(s, w, compound)
	w.Release(pos)
	if f == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(f.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	s.registerFrame(f)
	return f, nil
}

// registerFrame puts a woken or summoned frame into the world and shows it
// to every player in range.
func (s *Server) registerFrame(f *frameEntity) {
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()

	s.framesMu.Lock()
	s.registerFrameLocked(f)
	watching := frameTracker.showLocked(f, watchers)
	shown := framePairing(f)
	s.framesMu.Unlock()
	s.mu.RUnlock()

	sendFrame(f.w, shown, watching)
}

// registerFrameLocked adds f to the list and the cell index. The frame
// lock is held.
func (s *Server) registerFrameLocked(f *frameEntity) {
	s.frames = append(s.frames, f)
	frameTracker.file(s, f)
	s.addFrameCellsLocked(f)
}

// addFrameCellsLocked puts a frame's cells in the index. The frame lock is
// held.
func (s *Server) addFrameCellsLocked(f *frameEntity) {
	for _, cell := range f.cells {
		key := frameCell{w: f.w, x: cell[0], y: cell[1], z: cell[2]}
		s.frameCells[key] = append(s.frameCells[key], f)
	}
}

// rehangFrameLocked moves a frame to the cell and direction given, its
// index entries with it. Survival waits for the frame's next poll. The
// frame lock is held.
// vanilla: BlockAttachedEntity.tick
func (s *Server) rehangFrameLocked(f *frameEntity, x, y, z, dir int32) {
	s.removeFrameCellsLocked(f)
	f.hangAt(x, y, z, dir)
	s.addFrameCellsLocked(f)
}

// unlistFrameLocked takes a frame out of the list and the cell index, keeping
// its scores for a twin that carries its UUID on. The frame lock is held.
func (s *Server) unlistFrameLocked(f *frameEntity) {
	frameTracker.unfile(f)
	s.frames = slices.DeleteFunc(s.frames, func(o *frameEntity) bool { return o == f })
	s.removeFrameCellsLocked(f)
}

// removeFrameLocked unlists a destroyed frame and forgets its scores. The
// frame lock is held.
func (s *Server) removeFrameLocked(f *frameEntity) {
	s.unlistFrameLocked(f)
	s.entityDestroyed(f.uuid)
}

// removeFrameCellsLocked drops a frame's entries from the cell index. The
// frame lock is held.
func (s *Server) removeFrameCellsLocked(f *frameEntity) {
	for _, cell := range f.cells {
		key := frameCell{w: f.w, x: cell[0], y: cell[1], z: cell[2]}
		s.frameCells[key] = slices.DeleteFunc(s.frameCells[key],
			func(o *frameEntity) bool { return o == f })
		if len(s.frameCells[key]) == 0 {
			delete(s.frameCells, key)
		}
	}
}

// killFrame discards a frame with no break sound and no drop.
// vanilla: Entity.kill
func (s *Server) killFrame(f *frameEntity) {
	s.framesMu.Lock()
	s.removeFrameLocked(f)
	s.framesMu.Unlock()
	s.broadcastWorld(f.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{f.id}))
}

// teleportFrame rehangs a frame in the cell under the position given and
// marks it for the next tracking pass to tell its watchers the box centre.
// vanilla: Entity.teleportTo
func (s *Server) teleportFrame(f *frameEntity, to player.Position, by string) {
	x, y, z := int32(math.Floor(to.X)), int32(math.Floor(to.Y)), int32(math.Floor(to.Z))
	frameTracker.teleport(s, f, func() { s.rehangFrameLocked(f, x, y, z, f.dir) })
}

// frameTwinAt builds the frame that continues this one in another
// dimension, hanging in the cell under pos: the saved compound loaded
// again with a new ID and the same UUID. The caller holds the frames' lock
// or has taken the frame out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) frameTwinAt(f *frameEntity, to *dimension, pos player.Position) *frameEntity {
	tag := frameTag(f)
	tag.Set("block_pos", nbt.IntArray{rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)})
	return frameOfTag(s, to.w, tag.Entries())
}

// framesOfWorldExceptLocked collects the world's frames not in excluded
// (nil: every one). The frame lock is held.
func (s *Server) framesOfWorldExceptLocked(w *world.World, excluded map[*frameEntity]bool) []*frameEntity {
	frames := make([]*frameEntity, 0, len(s.frames))
	for _, f := range s.frames {
		if f.w == w && !excluded[f] {
			frames = append(frames, f)
		}
	}
	return frames
}

// frameByIDLocked finds the frame an entity ID names, nil for one gone.
// The frame lock is held.
func (s *Server) frameByIDLocked(w *world.World, id int32) *frameEntity {
	for _, f := range s.frames {
		if f.id == id && f.w == w {
			return f
		}
	}
	return nil
}

// frameByEntityID is frameByIDLocked for the packet paths, which do not
// hold the lock yet.
func (s *Server) frameByEntityID(w *world.World, id int32) *frameEntity {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	return s.frameByIDLocked(w, id)
}

// frameSees reports whether a client at (x, z) seeing chunkRadius chunks is
// in a frame's tracking range scaled by pct percent, for the map decorations
// it shows.
func frameSees(f *frameEntity, x, z float64, chunkRadius int32, pct int) bool {
	return inTrackingRange(x, z, float64(f.x), float64(f.z), frameTracker.rangeChunks, pct, chunkRadius)
}

// sendFrame sends each watcher the pairing built under the frame lock, so
// nothing here re-reads a frame a /tp may move.
func sendFrame(w *world.World, shown []outbound, watching []*client) {
	sendPacketsToAll(w, watching, shown)
}

// placeItemFrame handles a use of a frame item on a face, the flat ones
// included. handled is true for a frame item, which answers the use even
// when nothing hangs.
// vanilla: HangingEntityItem.useOn, ItemFrameItem.mayPlace
func (c *client) placeItemFrame(hand, x, y, z, face int32) (handled bool, err error) {
	c.useSwing = swingNone
	stack := c.heldItem(hand)
	var glow bool
	switch registry.Item(stack.ItemID) {
	case registry.ItemItemFrame:
	case registry.ItemGlowItemFrame:
		glow = true
	default:
		return false, nil
	}
	w := c.theWorld()
	off := protocol.FaceOffset[face]
	// vanilla: ItemFrameItem.mayPlace, Level.isInsideBuildHeight
	if hy := y + off[1]; hy < w.MinY() || hy >= w.MinY()+w.Height() {
		return true, nil
	}
	tell, hangs := c.srv.hangFrame(w, x+off[0], y+off[1], z+off[2], face, glow, stack)
	if !hangs {
		// Nothing survives where it would hang; the frame stays in hand.
		return true, nil
	}
	if !c.srv.hangingPlaced(c, w, tell.uuid, tell.id, x, y, z, face) {
		return true, nil
	}
	// The placer hears it too: unlike a block's, it is not predicted.
	// vanilla: ItemFrame.playPlacementSound
	if !tell.silent {
		c.srv.broadcastSoundID(w, soundFrame(tell.glow, framePlace), protocol.SoundCategoryNeutral,
			tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0)
	}
	c.gameEventAt(rules.GameEventEntityPlace, tell.centreX, tell.centreY, tell.centreZ, 0)
	c.useSwing = swingPredicted
	c.awardUsedHeld(hand)
	// The frame is spent outside creative. vanilla: ItemStack.consume
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	return true, nil
}

// useItemFrame handles an Interact on a frame: one of the held stack goes
// into an empty frame, and a filled one turns its item one of eight
// positions on. A fixed frame does nothing.
// vanilla: ItemFrame.interact
func (c *client) useItemFrame(target, hand int32) error {
	// A use past the interaction range is dropped before it is read.
	// vanilla: ServerGamePacketListenerImpl.handleInteract
	pos := c.player.Position()
	s := c.srv
	held := c.heldItem(hand)
	s.framesMu.Lock()
	f := s.frameByIDLocked(c.theWorld(), target)
	if f == nil {
		s.framesMu.Unlock()
		return nil
	}
	if !rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		f.minX, f.minY, f.minZ, f.maxX, f.maxY, f.maxZ) {
		s.framesMu.Unlock()
		return nil
	}
	var (
		sound    registry.Sound
		metadata []byte
		filled   bool
	)
	switch {
	case f.fixed, f.item.Empty() && held.Empty():
	case f.item.Empty():
		stored := held
		stored.Count = 1
		f.setItem(stored)
		sound = soundFrame(f.glow, frameAddItem)
		metadata = frameMetadataPayload(f, false)
		filled = true
	default:
		f.rotation = (f.rotation + 1) % 8
		sound = soundFrame(f.glow, frameRotateItem)
		metadata = frameMetadataPayload(f, false)
	}
	tell := frameTellOf(f)
	s.framesMu.Unlock()
	if sound == registry.SoundNone {
		return nil
	}
	c.useSwing = swingPredicted
	s.broadcastWorld(tell.w, protocol.IDPlayEntityMetadata, metadata)
	if !tell.silent {
		s.broadcastSoundID(tell.w, sound, protocol.SoundCategoryNeutral,
			tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0)
	}
	s.frameSignalChanged(tell)
	// An adventure hand pays too. vanilla: ItemStack.consume
	if filled && c.player.GameMode() != protocol.GameModeCreative {
		return c.consumeHeld(hand)
	}
	return nil
}

// attackItemFrame handles a swing at a frame, skipAttackInteraction's
// zero-damage hurt: a blow pops a held item, the next breaks the frame, a
// creative hand dropping nothing. A fixed frame refuses all but a creative
// hurt. It reports true for a frame in reach or not, the swing spent on it
// with none of a blow's side effects.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.skipAttackInteraction
func (c *client) attackItemFrame(target int32) bool {
	s := c.srv
	creative := c.player.GameMode() == protocol.GameModeCreative
	s.framesMu.Lock()
	f := s.frameByIDLocked(c.theWorld(), target)
	if f == nil {
		s.framesMu.Unlock()
		return false
	}
	// Out of reach the attack never runs.
	if !c.withinAttackRange(f.minX, f.minY, f.minZ, f.maxX, f.maxY, f.maxZ) {
		s.framesMu.Unlock()
		return true
	}
	// vanilla: BlockAttachedEntity.skipAttackInteraction
	if !c.mayInteractAt(f.x, f.z) {
		s.framesMu.Unlock()
		return true
	}
	if (f.fixed || f.invulnerable) && !creative {
		// The hurt refuses; the swing is spent all the same.
		s.framesMu.Unlock()
		return true
	}
	if s.entityDamageListened() {
		id := f.uuid
		s.framesMu.Unlock()
		cause := &damageCause{entityID: c.player.EntityID}
		if _, ok := s.askNonlivingDamage(c.theWorld(), id, meleeDamageType(c.heldItem(protocol.HandMain)), cause, 1); !ok {
			return true
		}
		s.framesMu.Lock()
		if f = s.frameByIDLocked(c.theWorld(), target); f == nil {
			s.framesMu.Unlock()
			return true
		}
	}
	if f.fixed || f.item.Empty() {
		// Nothing pops first: the blow breaks it.
		tell := frameTellOf(f)
		if s.removeFrameAsked(f, removedByEntity(c.player.UUID)) {
			s.breakFrameTail(tell, creative)
		}
		return true
	}
	held := f.item
	f.setItem(protocol.ItemStack{})
	metadata := frameMetadataPayload(f, false)
	tell := frameTellOf(f)
	s.framesMu.Unlock()
	s.popFrameItemTail(tell, metadata, held, creative)
	return true
}

// arrowFrameHit is a frame a shaft's sweep met. dropless is a creative
// shooter, mob a mob's, which mob_griefing binds; the caller spends or
// bounces arrow on the hurt's answer.
type arrowFrameHit struct {
	hitSound   registry.Sound
	w          *world.World
	frameID    int32
	damageType registry.DamageType
	dropless   bool
	mob        bool
	arrow      *arrowEntity
	// cause is the hurt's source for EntityDamageEvent, nil when not known.
	cause *damageCause
}

// arrowHitFrame lands an arrow's hurt on a frame like a blow: a filled one
// pops its item, an empty one breaks. With mob_griefing off a mob's shaft
// breaks none, though the pop runs ahead of that refusal. It reports
// whether the shaft is spent, a frame already gone included; a refused
// hurt is the caller's to bounce.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func (s *Server) arrowHitFrame(hit arrowFrameHit) bool {
	return s.hurtFrame(hit, false)
}

// hurtFrame shares the pop-or-break decision with typed command hurts.
// vanilla: ItemFrame.hurtServer
func (s *Server) hurtFrame(hit arrowFrameHit, explosion bool) bool {
	if s.entityDamageListened() {
		s.framesMu.Lock()
		f := s.frameByIDLocked(hit.w, hit.frameID)
		var id uuid.UUID
		if f != nil {
			id = f.uuid
		}
		s.framesMu.Unlock()
		if f == nil {
			return true
		}
		if _, ok := s.askNonlivingDamage(hit.w, id, hit.damageType, hit.cause, 1); !ok {
			return false
		}
	}
	s.framesMu.Lock()
	f := s.frameByIDLocked(hit.w, hit.frameID)
	if f == nil {
		s.framesMu.Unlock()
		return true
	}
	if (f.fixed || f.invulnerable) && !hit.dropless && !rules.InvulnerabilityBypassed(hit.damageType) {
		s.framesMu.Unlock()
		return false
	}
	if (explosion || f.fixed || f.item.Empty()) && hit.mob && !f.w.BoolRule(world.RuleMobGriefing) {
		s.framesMu.Unlock()
		return false
	}
	if explosion || f.fixed || f.item.Empty() {
		tell := frameTellOf(f)
		by := hangingRemover{cause: plugin.HangingBreakEntity}
		if hit.arrow != nil {
			by = removedByEntity(hit.arrow.ownerUUID)
		}
		if !s.removeFrameAsked(f, by) {
			return false
		}
		s.breakFrameTail(tell, hit.dropless)
		return true
	}
	held := f.item
	f.setItem(protocol.ItemStack{})
	metadata := frameMetadataPayload(f, false)
	tell := frameTellOf(f)
	s.framesMu.Unlock()
	s.popFrameItemTail(tell, metadata, held, hit.dropless)
	return true
}

// popFrameItemTail tells a popped frame: the emptied slot, the sound and
// the comparators' wake, which setItem runs before the drop rules are read,
// then the item laid in front of it.
// vanilla: ItemFrame.hurtServer, ItemFrame.dropItem
func (s *Server) popFrameItemTail(tell frameTell, metadata []byte, held protocol.ItemStack, dropless bool) {
	s.broadcastWorld(tell.w, protocol.IDPlayEntityMetadata, metadata)
	if !tell.silent {
		s.broadcastSoundID(tell.w, soundFrame(tell.glow, frameRemoveItem), protocol.SoundCategoryNeutral,
			tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0)
	}
	s.frameSignalChanged(tell)
	if !tell.w.BoolRule(world.RuleEntityDrops) || dropless {
		return
	}
	// vanilla: ItemFrame.dropItem
	if s.popRng.Float64() < float64(tell.dropChance) {
		s.spawnHangingDrop(tell.w, tell.dir, tell.centreX, tell.centreY, tell.centreZ, held)
	}
}

// breakFrameTail is a frame's end after the lock: the removal, the break
// sound whatever the drops come to, the comparators' wake, and the frame
// item and its held item laid unless entity_drops is off or dropless. A
// fixed frame has no wake and no drops.
// vanilla: ItemFrame.dropItem, BlockAttachedEntity.tick
func (s *Server) breakFrameTail(tell frameTell, dropless bool) {
	s.broadcastWorld(tell.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{tell.id}))
	if !tell.silent {
		s.broadcastSoundID(tell.w, soundFrame(tell.glow, frameBreak), protocol.SoundCategoryNeutral,
			tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0)
	}
	if tell.fixed {
		return
	}
	held := tell.item
	s.frameSignalChanged(tell)
	if !tell.w.BoolRule(world.RuleEntityDrops) || dropless {
		return
	}
	name := "minecraft:item_frame"
	if tell.glow {
		name = "minecraft:glow_item_frame"
	}
	id, ok := registry.ItemID(name)
	if !ok {
		return
	}
	s.spawnHangingDrop(tell.w, tell.dir, tell.centreX, tell.centreY, tell.centreZ,
		protocol.ItemStack{ItemID: id, Count: 1, CustomName: customNameText(tell.custom.tag())})
	if !held.Empty() {
		// vanilla: ItemFrame.dropItem
		if s.popRng.Float64() < float64(tell.dropChance) {
			s.spawnHangingDrop(tell.w, tell.dir, tell.centreX, tell.centreY, tell.centreZ, held)
		}
	}
}

// frameSignalChanged wakes the comparators reading the frame's cell
// through a conductor.
// vanilla: ItemFrame.setItem, Level.updateNeighbourForOutputSignal
func (s *Server) frameSignalChanged(tell frameTell) {
	s.physicsFor(tell.w).EntityOutputChanged(tell.x, tell.y, tell.z)
}

// pollFrames brings down, every 101 ticks of its life, a frame whose
// survives check fails.
// vanilla: BlockAttachedEntity.tick
func (s *Server) pollFrames() {
	s.lockTick(&s.paintingsMu)
	s.lockTick(&s.framesMu)
	var doomed []*frameEntity
	var tells []frameTell
	for _, f := range s.frames {
		f.checkInterval++
		if f.checkInterval <= 100 {
			continue
		}
		f.checkInterval = 0
		if !frameSurvives(f.w, f.x, f.y, f.z, f.dir, f.fixed,
			s.paintingsOfWorldExceptLocked(f.w, nil),
			s.framesOfWorldExceptLocked(f.w, map[*frameEntity]bool{f: true})) {
			doomed = append(doomed, f)
			tells = append(tells, frameTellOf(f))
		}
	}
	s.unlockTick(&s.framesMu)
	s.unlockTick(&s.paintingsMu)
	for i, f := range doomed {
		s.framesMu.Lock()
		if s.removeFrameAsked(f, hangingRemover{}) {
			s.breakFrameTail(tells[i], false)
		}
	}
}

// framePairing is what shows a frame: the spawn, then the state its
// metadata carries.
func framePairing(f *frameEntity) []outbound {
	return spawnAndMetadata(frameSpawnPayload(f), frameMetadataPayload(f, false))
}

// frameSpawnPayload is a frame's Spawn Entity: the direction in the data
// field and the box centre as the position; a flat frame turns its pitch
// rather than its yaw.
// vanilla: ItemFrame.getAddEntityPacket, ItemFrame.setDirection
func frameSpawnPayload(f *frameEntity) []byte {
	var b protocol.Buffer
	b.PutVarInt(f.id)
	b.PutUUID(f.uuid)
	b.PutVarInt(frameEntityType(f))
	b.PutF64(frameCentreX(f))
	b.PutF64(frameCentreY(f))
	b.PutF64(frameCentreZ(f))
	b.PutLpVec3(0, 0, 0) // velocity
	b.PutAngle(float32(framePitch(f.dir)))
	b.PutAngle(float32(frameYaw(f.dir)))
	b.PutAngle(0) // head yaw
	b.PutVarInt(f.dir)
	return b.Bytes()
}

// frameMetadataPayload is the metadata a frame is drawn with: the
// direction, the item and the rotation, sent even at their defaults, and
// the invisible flag when set.
// vanilla: ItemFrame.defineSynchedData
func frameMetadataPayload(f *frameEntity, all bool) []byte {
	var b protocol.Buffer
	b.PutVarInt(f.id)
	f.look.putSilent(&b, all && !f.look.silent)
	f.look.putNoGravity(&b, all && !f.look.noGravity)
	flags := f.look.glowFlags()
	if f.invisible {
		flags |= protocol.EntityFlagInvisible
	}
	if flags != 0 || all {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(flags)
	}
	b.PutMetadata(protocol.MetadataIndexHangingDirection, protocol.MetadataDirection)
	b.PutVarInt(f.dir)
	b.PutMetadata(protocol.MetadataIndexFrameItem, protocol.MetadataSlot)
	b.PutSlot(f.item)
	b.PutMetadata(protocol.MetadataIndexFrameRotation, protocol.MetadataVarInt)
	b.PutVarInt(f.rotation)
	f.custom.putMetadata(&b, customName{})
	b.EndMetadata()
	return b.Bytes()
}

// frameEntityType is the frame's entity type ID.
func frameEntityType(f *frameEntity) int32 {
	if f.glow {
		return protocol.EntityTypeGlowItemFrame
	}
	return protocol.EntityTypeItemFrame
}

// frameYaw and framePitch are a frame's body angles: a wall frame's yaw,
// and a flat one's ninety degrees of pitch, negative facing up (the angle
// byte wraps it to 270).
// vanilla: ItemFrame.setDirection
func frameYaw(dir int32) int32 {
	if protocol.FaceOffset[dir][1] == 0 {
		return painting2DOf(dir) * 90
	}
	return 0
}

func framePitch(dir int32) int32 {
	switch protocol.FaceOffset[dir][1] {
	case 1:
		return -90
	case -1:
		return 90
	default:
		return 0
	}
}

type frameSound uint8

const (
	framePlace frameSound = iota
	frameAddItem
	frameRotateItem
	frameRemoveItem
	frameBreak
)

var frameSounds = [2][5]registry.Sound{
	{registry.SoundEntityItemFramePlace, registry.SoundEntityItemFrameAddItem, registry.SoundEntityItemFrameRotateItem, registry.SoundEntityItemFrameRemoveItem, registry.SoundEntityItemFrameBreak},
	{registry.SoundEntityGlowItemFramePlace, registry.SoundEntityGlowItemFrameAddItem, registry.SoundEntityGlowItemFrameRotateItem, registry.SoundEntityGlowItemFrameRemoveItem, registry.SoundEntityGlowItemFrameBreak},
}

func soundFrame(glow bool, kind frameSound) registry.Sound {
	group := 0
	if glow {
		group = 1
	}
	return frameSounds[group][kind]
}

// frameCentreX, Y and Z are the box's middle, where its sounds play and
// its drops fall from.
func frameCentreX(f *frameEntity) float64 { return (f.minX + f.maxX) / 2 }
func frameCentreY(f *frameEntity) float64 { return (f.minY + f.maxY) / 2 }
func frameCentreZ(f *frameEntity) float64 { return (f.minZ + f.maxZ) / 2 }

func frameCentre(f *frameEntity) bodyPose {
	return bodyPose{x: frameCentreX(f), y: frameCentreY(f), z: frameCentreZ(f)}
}

// tickFrames runs the survives poll and settles who sees which frame, from
// where the players stood before the poll. A frozen world settles no frame.
func (s *Server) tickFrames() {
	watchers := s.trackingWatchers()
	s.pollFrames()
	frameTracker.frozenPass(s, watchers)
}

// frameAnalogOutputs answers the engine's comparator questions from the
// server's frame lists, one per dimension's physics.
type frameAnalogOutputs struct {
	s *Server
	w *world.World
}

// AnalogOutputAt reports the analog output of the frame hanging in the
// cell facing dir, and whether there is one: an empty frame is found but
// worth nothing. The index also holds a frame under its support cell, so
// both the cell and the direction are checked; getItemFrame picks only
// from a cell of exactly one.
// vanilla: ComparatorBlock.getItemFrame, ItemFrame.getAnalogOutput
func (o frameAnalogOutputs) AnalogOutputAt(dir, x, y, z int32) (int, bool) {
	o.s.framesMu.Lock()
	defer o.s.framesMu.Unlock()
	var one *frameEntity
	for _, f := range o.s.frameCells[frameCell{w: o.w, x: x, y: y, z: z}] {
		if f.dir != dir || f.x != x || f.y != y || f.z != z {
			continue
		}
		if one != nil {
			return 0, false // two frames in the cell read as none
		}
		one = f
	}
	if one == nil {
		return 0, false
	}
	// The held item's rotation plus one, zero with the frame empty.
	// vanilla: ItemFrame.getAnalogOutput
	if one.item.Empty() {
		return 0, true
	}
	return int(one.rotation) + 1, true
}
