package server

import (
	"cmp"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The hopper's turn of the block entity tick (vanilla's pushItemsTick).
// Move rules are package inventory's (transfer.go), the cooldown the
// world's (world/hopper.go); this is locking and notification. Moves run
// under the hopper's lock, reached containers taking theirs in turn;
// viewers are told once every lock is released.

func oppositeFace(face int32) int32 {
	switch face {
	case protocol.FaceBottom:
		return protocol.FaceTop
	case protocol.FaceTop:
		return protocol.FaceBottom
	case protocol.FaceNorth:
		return protocol.FaceSouth
	case protocol.FaceSouth:
		return protocol.FaceNorth
	case protocol.FaceWest:
		return protocol.FaceEast
	default:
		return protocol.FaceWest
	}
}

func faceOffset(x, y, z int32, face int32) (int32, int32, int32) {
	offset := protocol.FaceOffset[face]
	return x + offset[0], y + offset[1], z + offset[2]
}

func faceStep(face int32) (int32, int32, int32) {
	offset := protocol.FaceOffset[face]
	return offset[0], offset[1], offset[2]
}

// blockFacing reads a block's facing property, down if it has none.
func blockFacing(w *world.World, x, y, z int32) int32 {
	id := w.BlockAt(x, y, z)
	if !registry.StateHasFacing(id) {
		return protocol.FaceBottom
	}
	// StateFacing's Direction.values order is the protocol face numbering.
	return int32(registry.StateFacing(id))
}

// containerBlockEntity is the second step of vanilla's getBlockContainer:
// the block entity at a position if its kind keeps slots. Not the ender
// chest (its contents are the opener's), nor the bell, comparator or signs.
func containerBlockEntity(w *world.World, x, y, z int32) *world.BlockEntity {
	be := w.BlockEntity(x, y, z)
	if be == nil {
		return nil
	}
	if world.ContainerSize(be.Kind) == 0 {
		return nil
	}
	return be
}

// containerTarget is what moves reach at a position: one container block
// entity, a vehicle, a double chest's halves (right first), or the composter's level
// view. Moves run under the target's locks (the composter has none, its
// state being the block); settle applies the target's world changes (the
// composter's block change, a jukebox's song) after the mover's lock, which
// must not carry them. vanilla: HopperBlockEntity.getContainerAt
type containerTarget interface {
	// use runs a move under the target's locks and reports success, a double chest's
	// right half first. UseLive, not Use: the target was found before locking,
	// and a container a blast removed since must be left alone.
	use(w *world.World, move containerMove) bool
	// entity is the target's block entity, nil for the composter (whose level
	// is the block state); the chained cooldown reads it as the source.
	entity() *world.BlockEntity
	// sync sends the target's viewers its slots; a composter has no window.
	sync(s *Server, w *world.World)
	// settle applies what a move changed about the target after the mover's
	// lock: a jukebox's song, a pot's or shelf's rendered items, a composter's
	// level.
	settle(s *Server, w *world.World)
}

// singleTarget is pointer-shaped so a single container needs no interface box.
type singleTarget struct{ be *world.BlockEntity }

// pairTarget keeps the double chest's right half first.
// vanilla: ChestBlock.getContainer
type pairTarget struct{ bes [2]*world.BlockEntity }

// containerMove carries a hopper move by value across target dispatch. Cold
// callers can supply fn; the hopper path needs no escaping callback or slot box.
type containerMove struct {
	self  blockEntitySlots
	pull  bool
	face  int32
	fn    func(inventory.Block)
	cache *world.HopperTargetCache
	// gate is the plugin's veto on the item moved; nil unless one listens.
	gate inventory.Gate
}

// applyContainerMove keeps concrete slot adapters unboxed on the hopper path.
func applyContainerMove[B inventory.Block](move containerMove, slots B, entity *world.BlockEntity) bool {
	if move.fn != nil {
		move.fn(slots)
		return false
	}
	var moved, intoEmpty bool
	if move.pull {
		moved, intoEmpty = inventory.SuckGated(slots, move.self, move.gate)
		if intoEmpty {
			move.self.be.HopperReceived(entity)
		}
	} else {
		moved, intoEmpty = inventory.EjectGated(move.self, slots, move.face, move.gate)
		if intoEmpty && entity != nil {
			entity.HopperReceived(move.self.be)
		}
	}
	return moved
}

func (t singleTarget) use(w *world.World, move containerMove) (moved bool) {
	t.be.UseLive(func(b *world.BlockEntity) {
		moved = applyContainerMove(move, containerSlots(b, w), b)
	})
	return moved
}

func (t *pairTarget) use(w *world.World, move containerMove) (moved bool) {
	t.bes[0].UseLive(func(right *world.BlockEntity) {
		t.bes[1].UseLive(func(left *world.BlockEntity) {
			moved = applyContainerMove(move, inventory.Compound{First: containerSlots(right, w), Second: containerSlots(left, w)}, right)
		})
	})
	return moved
}

func (t singleTarget) entity() *world.BlockEntity { return t.be }
func (t *pairTarget) entity() *world.BlockEntity  { return t.bes[0] }

func (t singleTarget) sync(s *Server, w *world.World) {
	s.syncContainerViewers(w, t.be.X, t.be.Y, t.be.Z)
}
func (t *pairTarget) sync(s *Server, w *world.World) {
	for _, be := range t.bes {
		singleTarget{be}.sync(s, w)
	}
}

// settle applies rendered container changes after the mover's lock is released.
func (t singleTarget) settle(s *Server, w *world.World) {
	be := t.be
	switch be.Kind {
	case world.KindJukebox:
		s.syncJukeboxRecord(w, be)
	case world.KindDecoratedPot, world.KindShelf:
		s.broadcastBlockEntity(w, be.X, be.Y, be.Z, be)
	case world.KindChiseledBookshelf:
		s.syncBookshelfState(w, be)
	}
}
func (t *pairTarget) settle(s *Server, w *world.World) {
	for _, be := range t.bes {
		singleTarget{be}.settle(s, w)
	}
}

// composterTarget is the composter's container, built over the block state
// like vanilla's WorldlyContainerHolder: level 8 holds one bone meal given
// up only downward, levels below 7 take one compostable only from the top,
// and level 7 (awaiting its ready tick) holds nothing. settle writes the
// changed level, since a block change runs the engine's wake, whose lock
// the mover's must not carry. vanilla: ComposterBlock.getContainer
type composterTarget struct {
	x, y, z int32
	// received is the one compostable a move put in through the top (a hopper
	// moves one item a turn); emptied is the bone meal a take removed.
	moves inventory.ComposterMoves
}

// use rereads the level so a move sees the current one; a block swapped in
// between lookups gets no view. vanilla: ComposterBlock.getContainer
func (t *composterTarget) use(w *world.World, move containerMove) bool {
	state := w.BlockAt(t.x, t.y, t.z)
	if registry.BlockOf(state) != registry.BlockComposter {
		return false
	}
	level, _ := registry.StateInt(state, registry.PropLevel)
	switch level {
	case 8:
		return applyContainerMove(move, t.moves.Output(protocol.ItemStack{ItemID: int32(registry.ItemBoneMeal), Count: 1}), nil)
	case 7:
		return applyContainerMove(move, inventory.ComposterFull{}, nil)
	default:
		return applyContainerMove(move, t.moves.Input(), nil)
	}
}

// entity is nil: the composter slots the block state holds alone.
func (t *composterTarget) entity() *world.BlockEntity { return nil }

// sync tells nothing; the composter opens no window.
func (t *composterTarget) sync(*Server, *world.World) {}

// settle writes the level the move changed and the fill's event.
func (t *composterTarget) settle(s *Server, w *world.World) {
	if t.moves.Received != nil {
		s.composterFed(w, t.x, t.y, t.z, *t.moves.Received)
	}
	if t.moves.Emptied {
		s.composterEmptied(w, t.x, t.y, t.z)
	}
}

// containerAt is the container target at a position: the block's, else a
// container vehicle in the block. vanilla: HopperBlockEntity.getContainerAt
func (s *Server) containerAt(w *world.World, x, y, z int32) containerTarget {
	return s.containerAtExcept(w, x, y, z, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, nil)
}

// containerAtExcept is containerAt excluding one cart, so a hopper cart
// never reads itself; the entity search uses the unit box centred on (cx, cy,
// cz), which a hopper cart sets to its own position.
func (s *Server) containerAtExcept(w *world.World, x, y, z int32, cx, cy, cz float64, self *minecartEntity) containerTarget {
	if target := blockContainerAt(w, x, y, z); target != nil {
		return target
	}
	return s.entityContainerAt(w, cx, cy, cz, self)
}

// containerBlocks are the blocks whose block entity keeps slots.
// vanilla: BlockState.hasBlockEntity ahead of getBlockEntity
var containerBlocks = registry.BlocksOf(func(b registry.Block) bool {
	kind, ok := containerEntityKindOf(b)
	return ok && world.ContainerSize(kind) > 0
})

// blockContainerAt is the block's container target, nil for none. The
// composter comes first, as vanilla reads WorldlyContainerHolder before any
// block entity. vanilla: HopperBlockEntity.getBlockContainer
func blockContainerAt(w *world.World, x, y, z int32) containerTarget {
	block := registry.BlockOf(w.BlockAt(x, y, z))
	if block == registry.BlockComposter {
		return &composterTarget{x: x, y: y, z: z}
	}
	if !containerBlocks[block] {
		return nil
	}
	be := containerBlockEntity(w, x, y, z)
	if be == nil {
		return nil
	}
	if !world.IsChestBlock(block) {
		return singleTarget{be}
	}
	if first, second, ok := w.ChestPair(x, y, z); ok {
		right := containerBlockEntity(w, first[0], first[1], first[2])
		left := containerBlockEntity(w, second[0], second[1], second[2])
		if right != nil && left != nil {
			return &pairTarget{bes: [2]*world.BlockEntity{right, left}}
		}
	}
	return singleTarget{be}
}

// composterFed composts the item a move put in the top: the player use's
// layer provider and level rise, with the matching fill event. A composter
// that filled meanwhile (another move, a player, a command) gives the item
// nothing and it comes back out the top, the hopper's slot already spent.
// vanilla: ComposterBlock.addItem, InputContainer.setChanged
func (s *Server) composterFed(w *world.World, x, y, z int32, fed protocol.ItemStack) {
	one := fed
	one.Count = min(one.Count, 1)
	state := w.BlockAt(x, y, z)
	ctx := s.blockInteractLootContext(w, x, y, z, state, protocol.ItemStack{}, rules.LootEntity{})
	after, inserted, used := rules.ComposterInsert(state, one, &ctx, s.levelRng)
	if !used || !inserted {
		s.spawnPoppedItem(w, x, y, z, one)
		return
	}
	rose := int32(0)
	if after != state {
		rose = 1
		s.setComposterLevel(w, x, y, z, after)
	}
	s.broadcastLevelEvent(w,
		rules.LevelEventComposterFill, x, y, z, rose)
}

// composterEmptied empties a full composter whose bone meal a take
// removed; one another taker already emptied settles as is.
// vanilla: ComposterBlock.empty
func (s *Server) composterEmptied(w *world.World, x, y, z int32) {
	if !rules.ComposterReady(w.BlockAt(x, y, z)) {
		return
	}
	s.setComposterLevel(w, x, y, z, registry.BlockComposter.DefaultState())
}

// setComposterLevel writes a composter's new level, tells everyone and posts
// the sourceless block_change; the wake books the ready tick for a rise to 7
// and updates comparators.
// vanilla: ComposterBlock.addLayer, empty (a hopper's or dropper's null entity)
func (s *Server) setComposterLevel(w *world.World, x, y, z int32, state uint16) {
	s.setBlockAndTell(w, x, y, z, state)
	s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{Affected: state})
}

// pourInto is inventory.AddItem plus the chained cooldown when into is an
// empty hopper; src is nil for an item entity, into nil for a target with no
// block entity (the composter).
func pourInto[B inventory.Block](src, into *world.BlockEntity, dest B, stack protocol.ItemStack, face int32, throughFace bool) protocol.ItemStack {
	left, intoEmpty := inventory.AddItem(dest, stack, face, throughFace)
	if intoEmpty && into != nil {
		into.HopperReceived(src)
	}
	return left
}

// tickHopper runs one hopper's turn. Viewer syncs and settles wait until
// after the hopper's lock: a sync takes that lock again, which would stall
// the tick, and a settle's composter block change takes the engine's lock.
func (s *Server) tickHopper(w *world.World, be *world.BlockEntity, age int64) {
	var gate *hopperGate
	if s.hopperListening() {
		var due bool
		be.Tick(func(b *world.BlockEntity) bool {
			// The commit owns the countdown on turns that may transfer items.
			due = b.Cooldown <= 1
			if !due {
				b.HopperReady(age)
			}
			return false
		})
		if !due {
			return
		}
		gate = s.askHopper(w, be)
	}
	var synced [1]*world.BlockEntity
	var turn hopperTurn
	// The list was read before locking, and a break since has already
	// spilled the hopper, so it must not move those items again.
	be.Tick(func(b *world.BlockEntity) bool {
		if !b.HopperReady(age) {
			return false
		}
		if !world.HopperEnabled(w, b.X, b.Y, b.Z) {
			return true
		}
		s.hopperStep(w, b, gate, &turn)
		if turn.moved || (gate != nil && gate.vetoed()) {
			b.Cooldown = world.HopperMoveInterval
			if !turn.moved {
				w.Touch(b.X, b.Y, b.Z)
			}
		}
		if turn.moved || turn.partial {
			synced[0] = b
		}
		return false
	})
	if !turn.moved && !turn.partial {
		return
	}
	sets := [2]containerTarget{turn.pushed, turn.pulled}
	if c := synced[0]; c != nil {
		s.syncContainerViewers(w, c.X, c.Y, c.Z)
	}
	for _, target := range sets {
		if target != nil {
			target.sync(s, w)
		}
	}
	// Container-side changes (a composter's level, a jukebox's song, a pot's or
	// shelf's items) apply after the hopper's lock.
	for _, target := range sets {
		if target != nil {
			target.settle(s, w)
		}
	}
	s.notifyItemsAbsorbed(w, turn.taken, turn.shrunk)
}

// hopperTurn is what one hopper turn did: moved reports a whole item moved,
// partial a partly taken item entity; the targets are synced after the lock.
type hopperTurn struct {
	moved, partial bool
	pushed, pulled containerTarget
	taken, shrunk  []*itemEntity
}

// hopperStep is one turn's moves under the hopper's lock, recorded in t; a
// probing gate makes it write nothing.
func (s *Server) hopperStep(w *world.World, b *world.BlockEntity, gate *hopperGate, t *hopperTurn) {
	self := containerSlots(b, w)
	if b.HopperTargets == nil {
		b.HopperTargets = &world.HopperTargets{}
	}
	if b.FilledSlots() != 0 {
		t.pushed = s.hopperEject(w, self, gate)
		t.moved = t.pushed != nil
	}
	if !slotsFull(b.Slots) {
		var suckMoved bool
		suckMoved, t.pulled, t.partial, t.taken, t.shrunk = s.hopperSuck(w, self, gate)
		t.moved = suckMoved || t.moved
	}
}

// hopperEject pushes one item out the spout (vanilla's ejectItems),
// returning the container it went into, nil if none, for the caller to sync
// after the hopper's lock.
func (s *Server) hopperEject(w *world.World, self blockEntitySlots, gate *hopperGate) containerTarget {
	b := self.be
	face := blockFacing(w, b.X, b.Y, b.Z)
	tx, ty, tz := faceOffset(b.X, b.Y, b.Z, face)
	cache := &b.HopperTargets.Push
	hopperRefresh(w, cache, tx, ty, tz)
	move := containerMove{self: self, face: oppositeFace(face)}
	var slot *moveGate
	if gate != nil {
		slot = &gate.push
	}
	target, moved, _ := s.hopperMove(w, cache, tx, ty, tz, move, slot)
	if !moved {
		return nil
	}
	return target
}

// hopperSuck is vanilla's suckInItems: from the container above if any,
// else item entities in the suck box (a full block above covers the mouth).
// It reports whether a whole item was taken, the container above it took
// from (synced by the caller after the locks), and whether a partly taken
// entity changed the slots without being a move (a partial take sets no
// cooldown). taken and shrunk are item entities whose viewers must be told
// after the lock: removed whole, or kept with their stack resent.
func (s *Server) hopperSuck(w *world.World, self blockEntitySlots, gate *hopperGate) (moved bool, pulled containerTarget, partial bool, taken, shrunk []*itemEntity) {
	b := self.be
	cache := &b.HopperTargets.Pull
	hopperRefresh(w, cache, b.X, b.Y+1, b.Z)
	move := containerMove{self: self, pull: true}
	var slot *moveGate
	if gate != nil {
		slot = &gate.pull
	}
	above, moved, skipped := s.hopperMove(w, cache, b.X, b.Y+1, b.Z, move, slot)
	if moved {
		return true, above, false, nil, nil
	}
	if skipped || above != nil {
		return false, nil, false, nil, nil
	}

	state := w.BlockAt(b.X, b.Y+1, b.Z)
	if world.ContainerMouthBlock(state) || world.HopperBlockedBy(state, registry.CollisionShapeFull(state)) {
		return false, nil, false, nil, nil
	}
	if !s.itemsOverColumn(w, b.X, b.Z) {
		return false, nil, false, nil, nil
	}
	x0, y0, z0, x1, y1, z1 := b.HopperSuckBox()
	for _, it := range s.itemEntitiesIn(w, x0, y0, z0, x1, y1, z1) {
		if it.item.Stack.Empty() {
			continue
		}
		if gate != nil {
			cancelled, decided := gate.pickup[it]
			if gate.probing {
				if !decided {
					gate.pending = it
					return false, nil, false, nil, nil
				}
				if !cancelled {
					return false, nil, false, nil, nil
				}
			}
			if cancelled {
				continue
			}
		}
		left := pourInto(nil, b, self, it.item.Stack, 0, false)
		if left.Empty() {
			it.item.Dead = true
			taken = append(taken, it)
			return true, nil, false, taken, shrunk
		}
		// vanilla: addItem(Container, ItemEntity) returns false for a remainder.
		it.item.Stack = left
		shrunk = append(shrunk, it)
		partial = true
	}
	return false, nil, partial, taken, shrunk
}

// notifyItemsAbsorbed tells item viewers of a hopper's suck after the
// hopper's lock: Remove Entities for whole items, Set Entity Metadata with
// the new stack for partial ones. vanilla: HopperBlockEntity.addItem
func (s *Server) notifyItemsAbsorbed(w *world.World, taken, shrunk []*itemEntity) {
	if len(taken) == 0 && len(shrunk) == 0 {
		return
	}
	var (
		removes []int32
		updates []itemMetadataUpdate
	)
	s.itemsMu.Lock()
	for _, it := range taken {
		removes = append(removes, it.item.ID)
		s.entityDestroyed(it.uuid)
	}
	for _, it := range shrunk {
		update := itemMetadataUpdate{it: it, metadata: itemMetadataPayload(it)}
		for id := range it.tracked.all() {
			update.watchers = append(update.watchers, id)
		}
		updates = append(updates, update)
	}
	s.itemsMu.Unlock()

	if len(removes) > 0 {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(removes))
	}
	s.sendItemMetadata(updates)
}

func itemChunkArea(minX, minZ, maxX, maxZ float64) chunkArea {
	return chunkArea{
		minX: world.ChunkOf(minX - entity.ItemHalfSize),
		minZ: world.ChunkOf(minZ - entity.ItemHalfSize),
		maxX: world.ChunkOf(maxX + entity.ItemHalfSize),
		maxZ: world.ChunkOf(maxZ + entity.ItemHalfSize),
	}
}

// itemEntitiesIn gathers w's live item entities whose boxes reach into the
// box under the items lock, in the order vanilla visits entity sections
// (lower X first, then Z, then Y) and by registration within a section.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
func (s *Server) itemEntitiesIn(w *world.World, minX, minY, minZ, maxX, maxY, maxZ float64) []*itemEntity {
	if minX >= maxX || minY >= maxY || minZ >= maxZ {
		return nil
	}
	area := itemChunkArea(minX, minZ, maxX, maxZ)
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	var found []*itemEntity
	for pos := range area.each {
		for it := range s.itemChunks.In(pos) {
			if it.w != w || it.item.Dead {
				continue
			}
			x0, y0, z0, x1, y1, z1 := it.item.BlastBox()
			if x1 <= minX || x0 >= maxX || y1 <= minY || y0 >= maxY || z1 <= minZ || z0 >= maxZ {
				continue
			}
			found = append(found, it)
		}
	}
	if len(found) > 1 {
		slices.SortFunc(found, func(a, b *itemEntity) int {
			ax, ay, az := sectionSortKey(a)
			bx, by, bz := sectionSortKey(b)
			return cmp.Or(cmp.Compare(ax, bx), cmp.Compare(az, bz), cmp.Compare(ay, by), cmp.Compare(a.order, b.order))
		})
	}
	return found
}

// sectionSortKey is an item's section coordinates in vanilla's visiting
// order: the section set is sorted by the packed long, whose Z and Y fields
// compare as unsigned, so negative sections come after positive ones.
func sectionSortKey(it *itemEntity) (x int32, y, z uint32) {
	fx, fy, fz := it.item.FeetPosition()
	return world.ChunkOf(fx), uint32(world.ChunkOf(fy)) & 0xFFFFF, uint32(world.ChunkOf(fz)) & 0x3FFFFF
}

// syncContainerViewers tells each viewer of the container at a position
// which slots changed since they were last told, in their window's
// numbering.
func (s *Server) syncContainerViewers(w *world.World, x, y, z int32) {
	// Read the entity and slots at the first viewer, so an unviewed container
	// needs neither a lookup nor a clone.
	var slots []protocol.ItemStack
	read := false
	s.containerViewers(w, x, y, z, nil, func(c *client) {
		window := c.openContainerWindow()
		if window == nil {
			return
		}
		if !read {
			be := w.BlockEntity(x, y, z)
			if be == nil {
				return
			}
			read = true
			be.Use(func(b *world.BlockEntity) {
				slots = slices.Clone(b.Slots)
			})
		}
		c.sendContainerChanges(window, window.slotBase(x, y, z), slots)
	})
}

// slotsEmpty and slotsFull read the hopper's own slots in place: a Slot call
// copies the 144-byte stack.
func slotsEmpty(slots []protocol.ItemStack) bool {
	for i := range slots {
		if slots[i].Count > 0 {
			return false
		}
	}
	return true
}

func slotsFull(slots []protocol.ItemStack) bool {
	for i := range slots {
		if slots[i].Count <= 0 || slots[i].Count < registry.StackMaxStackSizePtr(&slots[i]) {
			return false
		}
	}
	return true
}

// itemColumns records columns reached by items in the chunks already queried
// during one world's block-entity pass. Removals leave harmless positives for
// the exact box query, and arrivals add their columns immediately. Guarded by itemsMu.
type itemColumns struct {
	w       *world.World
	cols    map[[2]int32]struct{}
	scanned map[world.ChunkPos]struct{}
}

func (c *itemColumns) add(it *itemEntity) {
	if it.w != c.w || it.item.Dead {
		return
	}
	x0, _, z0, x1, _, z1 := it.item.BlastBox()
	for x := int32(math.Floor(x0)); x <= int32(math.Floor(x1)); x++ {
		for z := int32(math.Floor(z0)); z <= int32(math.Floor(z1)); z++ {
			c.cols[[2]int32{x, z}] = struct{}{}
		}
	}
}

// indexItemColumns starts a fresh pass for w.
func (s *Server) indexItemColumns(w *world.World) {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	idx := &s.itemColBuffer
	idx.w = w
	if idx.cols == nil {
		idx.cols = make(map[[2]int32]struct{})
		idx.scanned = make(map[world.ChunkPos]struct{})
	} else {
		clear(idx.cols)
		clear(idx.scanned)
	}
	s.itemCols = idx
}

func (s *Server) dropItemColumns() {
	s.itemsMu.Lock()
	s.itemCols = nil
	s.itemsMu.Unlock()
}

// itemsOverColumn is false only when the index says no item reaches the column.
func (s *Server) itemsOverColumn(w *world.World, x, z int32) bool {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	idx := s.itemCols
	if idx == nil || idx.w != w {
		return true
	}
	minX, minZ := float64(x), float64(z)
	area := itemChunkArea(minX, minZ, minX+1, minZ+1)
	for pos := range area.each {
		if _, ok := idx.scanned[pos]; ok {
			continue
		}
		idx.scanned[pos] = struct{}{}
		for it := range s.itemChunks.In(pos) {
			idx.add(it)
		}
	}
	_, ok := idx.cols[[2]int32{x, z}]
	return ok
}
