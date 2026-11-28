package server

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/world"
)

// cartBlock is a hopper cart's slots as a move's Block. The caller holds
// the cart lock, which is taken inside a source container's lock, never
// around one.
type cartBlock struct {
	it *minecartEntity
}

func (b cartBlock) Slot(slot int) protocol.ItemStack {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return protocol.ItemStack{}
	}
	return b.it.cargo.slots[slot]
}

func (b cartBlock) Peek(slot int) (itemID, count int32) {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return 0, 0
	}
	return b.it.cargo.slots[slot].ItemID, b.it.cargo.slots[slot].Count
}

func (b cartBlock) StackPtr(slot int) *protocol.ItemStack {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return nil
	}
	return &b.it.cargo.slots[slot]
}

// Filled scans the cargo: carts are rare and small.
func (b cartBlock) Filled() uint64 { return filledOf(b.it.cargo.slots) }

func (b cartBlock) AdjustCount(slot int, delta int32) {
	if slot < 0 || slot >= len(b.it.cargo.slots) {
		return
	}
	b.it.cargo.slots[slot].Count += delta
	b.it.cargo.version++
}

func (b cartBlock) Put(slot int, stack protocol.ItemStack) {
	cartCargo(b).Put(slot, stack)
}

func (b cartBlock) Stacks() []protocol.ItemStack { return b.it.cargo.slots }
func (b cartBlock) Len() int                     { return len(b.it.cargo.slots) }
func (b cartBlock) Kind() windowKind             { return inventory.KindHopper }

// hopperCartSuck is one tick's pull of an enabled hopper cart: one item from
// the container above, else one whole item entity from the hopper suck box
// or the cart's box widened 0.25. A cart has no cooldown and pushes nothing.
// vanilla: MinecartHopper.suckInItems, HopperBlockEntity.suckInItems
func (s *Server) hopperCartSuck(it *minecartEntity) {
	s.minecartsMu.Lock()
	if it.cart.Dead {
		s.minecartsMu.Unlock()
		return
	}
	w := it.w
	// The cart's level position is half a block above its feet.
	x, y, z := it.cart.X, it.cart.Y+0.5, it.cart.Z
	minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
	s.minecartsMu.Unlock()

	cart := cartBlock{it: it}
	above := s.containerAtExcept(w, int32(math.Floor(x)), int32(math.Floor(y+1)), int32(math.Floor(z)), x, y+1, z, it)
	if above != nil {
		moved := false
		above.use(w, containerMove{fn: func(src inventory.Block) {
			// A cart source already holds the cart lock.
			if _, fromCart := above.(cartTarget); !fromCart {
				s.minecartsMu.Lock()
				defer s.minecartsMu.Unlock()
			}
			moved, _ = inventory.Suck(src, cart)
		}})
		if moved {
			above.sync(s, w)
			above.settle(s, w)
			s.syncCartViewers(it)
			return
		}
	}

	var taken, shrunk []*itemEntity
	// try offers one item to the cart; true once a whole item went in.
	try := func(e *itemEntity) bool {
		if e.item.Stack.Empty() {
			return false
		}
		s.minecartsMu.Lock()
		left, _ := inventory.AddItem(cart, e.item.Stack, 0, false)
		s.minecartsMu.Unlock()
		if left.Empty() {
			e.item.Dead = true
			taken = append(taken, e)
			return true
		}
		if left.Count != e.item.Stack.Count {
			e.item.Stack = left
			shrunk = append(shrunk, e)
		}
		return false
	}
	// vanilla: Hopper.SUCK_AABB moved onto the cart's level position
	var mouth []*itemEntity
	whole := false
	if above == nil {
		mouth = s.itemEntitiesIn(w, x-0.5, y-0.5+11.0/16.0, z-0.5, x+0.5, y-0.5+2, z+0.5)
		if slices.ContainsFunc(mouth, try) {
			whole = true
		}
	}
	if !whole {
		for _, e := range s.itemEntitiesIn(w, minX-0.25, minY, minZ-0.25, maxX+0.25, maxY, maxZ+0.25) {
			// Only the mouth results can repeat: a box query returns each item once.
			if slices.Contains(mouth, e) {
				continue
			}
			if try(e) {
				break
			}
		}
	}
	if len(taken) > 0 || len(shrunk) > 0 {
		s.syncCartViewers(it)
	}
	s.notifyItemsAbsorbed(w, taken, shrunk)
}

// syncCartViewers tells every player viewing a cart which slots changed.
func (s *Server) syncCartViewers(it *minecartEntity) {
	s.syncVehicleViewers(func(w *openWindow) bool { return w.cart == it }, func() []protocol.ItemStack {
		s.minecartsMu.Lock()
		defer s.minecartsMu.Unlock()
		return slices.Clone(it.cargo.slots)
	})
}

// syncVehicleViewers sends cargo changes after the vehicle lock is released.
// vanilla: AbstractContainerMenu.broadcastChanges
func (s *Server) syncVehicleViewers(matches func(*openWindow) bool, slots func() []protocol.ItemStack) {
	if s.openContainerWindows.Load() == 0 {
		return
	}
	type viewer struct {
		c      *client
		window *openWindow
	}
	var buf [8]viewer
	recipients := buf[:0]
	s.mu.RLock()
	for _, c := range s.clients {
		window := c.openContainerWindow()
		if window != nil && matches(window) {
			recipients = append(recipients, viewer{c: c, window: window})
		}
	}
	s.mu.RUnlock()
	if len(recipients) == 0 {
		return
	}
	snapshot := slots()
	for _, r := range recipients {
		r.c.sendContainerChanges(r.window, 0, snapshot)
	}
}

// cartTarget is a container cart as a move's target. use takes the cart
// lock, which nests inside a block container's lock.
// vanilla: HopperBlockEntity.getEntityContainer
type cartTarget struct {
	s  *Server
	it *minecartEntity
}

func (t cartTarget) use(_ *world.World, move containerMove) bool {
	t.s.minecartsMu.Lock()
	defer t.s.minecartsMu.Unlock()
	if t.it.cart.Dead {
		return false
	}
	return cachedVehicleMove(move, cartBlock{it: t.it}, t.it.cargo.version)
}

// entity is nil: a cart has no block entity to chain a cooldown through.
func (t cartTarget) entity() *world.BlockEntity { return nil }

func (t cartTarget) sync(s *Server, _ *world.World) { s.syncCartViewers(t.it) }

// settle tells nothing; a cart has nothing a move changes beyond its slots.
func (t cartTarget) settle(*Server, *world.World) {}

// cartColumns buckets one world's container vehicles by every block column
// a hopper query box could meet them in during the block entity tick.
// Buckets keep each vehicle list's order.
// Guarded by minecartsMu.
type cartColumns struct {
	w     *world.World
	cols  map[[2]int32]vehicleBucket[minecartEntity]
	boats map[[2]int32]vehicleBucket[boatEntity]
	// spare* keep emptied overflow slices for the next index build.
	spareCols  [][]*minecartEntity
	spareBoats [][]*boatEntity
	// moreColKeys and moreBoatKeys list the buckets that took an overflow
	// slice, so a reset recycles those and clears the maps in bulk.
	moreColKeys  [][2]int32
	moreBoatKeys [][2]int32
}

// vehicleBucket holds a column's first vehicle inline so a lone vehicle
// costs no slice; more holds the rest in encounter order. The linear
// fallback sets only more, to the whole vehicle list.
type vehicleBucket[T any] struct {
	first *T
	more  []*T
}

// each calls f for the first vehicle if any, then the rest, in order.
func (b vehicleBucket[T]) each(f func(*T)) {
	if b.first != nil {
		f(b.first)
	}
	for _, it := range b.more {
		f(it)
	}
}

// resetBuckets empties m and keeps each overflow slice's storage in spare.
func resetBuckets[T any](m map[[2]int32]vehicleBucket[T], spare *[][]*T, moreKeys *[][2]int32) {
	for _, k := range *moreKeys {
		more := m[k].more
		clear(more)
		*spare = append(*spare, more[:0])
	}
	*moreKeys = (*moreKeys)[:0]
	clear(m)
}

func (c *cartColumns) reset() {
	resetBuckets(c.cols, &c.spareCols, &c.moreColKeys)
	resetBuckets(c.boats, &c.spareBoats, &c.moreBoatKeys)
}

func (c *cartColumns) add(it *minecartEntity) {
	if it.w != c.w || it.cart.Dead || it.cargo.slots == nil {
		return
	}
	minX, _, minZ, maxX, _, maxZ := it.cart.Box()
	addContainerColumns(c.cols, &c.spareCols, &c.moreColKeys, it, minX, minZ, maxX, maxZ)
}

func (c *cartColumns) addBoat(it *boatEntity) {
	if it.w != c.w || it.boat.Dead || it.cargo == nil {
		return
	}
	minX, _, minZ, maxX, _, maxZ := it.boat.Box()
	addContainerColumns(c.boats, &c.spareBoats, &c.moreBoatKeys, it, minX, minZ, maxX, maxZ)
}

// addContainerColumns includes every column whose unit query box can overlap the hull.
func addContainerColumns[T any](cols map[[2]int32]vehicleBucket[T], spare *[][]*T, moreKeys *[][2]int32, it *T, minX, minZ, maxX, maxZ float64) {
	for x := int32(math.Floor(minX - 0.5)); x <= int32(math.Floor(maxX+0.5)); x++ {
		for z := int32(math.Floor(minZ - 0.5)); z <= int32(math.Floor(maxZ+0.5)); z++ {
			k := [2]int32{x, z}
			b := cols[k]
			if b.first == nil {
				b.first = it
			} else {
				if b.more == nil {
					*moreKeys = append(*moreKeys, k)
				}
				if b.more == nil && len(*spare) > 0 {
					last := len(*spare) - 1
					b.more = (*spare)[last]
					(*spare)[last] = nil
					*spare = (*spare)[:last]
				}
				b.more = append(b.more, it)
			}
			cols[k] = b
		}
	}
}

// indexCartColumns builds the index for w; dropCartColumns ends it.
func (s *Server) indexCartColumns(w *world.World) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	idx := &s.cartColBuffer
	idx.w = w
	idx.reset()
	if idx.cols == nil {
		idx.cols = make(map[[2]int32]vehicleBucket[minecartEntity])
	}
	if idx.boats == nil {
		idx.boats = make(map[[2]int32]vehicleBucket[boatEntity])
	}
	for _, it := range s.minecarts {
		idx.add(it)
	}
	for _, it := range s.boats {
		idx.addBoat(it)
	}
	s.cartCols = idx
}

func (s *Server) dropCartColumns() {
	s.minecartsMu.Lock()
	s.cartCols = nil
	// Release the vehicles and world the last pass buffered.
	idx := &s.cartColBuffer
	idx.w = nil
	idx.reset()
	s.minecartsMu.Unlock()
}

// entityContainerAt picks a random living container vehicle whose box meets the
// unit box centred on (cx, cy, cz), never except; nil for none.
// vanilla: HopperBlockEntity.getEntityContainer
func (s *Server) entityContainerAt(w *world.World, cx, cy, cz float64, except *minecartEntity) containerTarget {
	// Boats are read under boatsMu then minecartsMu, the order the index
	// build uses, so dropCartColumns cannot recycle a bucket mid-traversal.
	// An empty or cart-only column needs minecartsMu alone.
	s.minecartsMu.Lock()
	locked := false
	for {
		carts, boats, indexed, empty := s.cartBucketsAt(w, cx, cz)
		if empty {
			s.minecartsMu.Unlock()
			if locked {
				s.boatsMu.Unlock()
			}
			return nil
		}
		if (!indexed || boats.first != nil) && !locked {
			s.minecartsMu.Unlock()
			s.boatsMu.Lock()
			s.minecartsMu.Lock()
			locked = true
			continue // the index may have changed while unlocked
		}
		var nearby [2]containerTarget
		found := nearby[:0]
		collectCart := func(it *minecartEntity) bool {
			if it == except || it.w != w || it.cart.Dead || it.cargo.slots == nil {
				return true
			}
			minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
			if boxMeets(minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz) {
				found = append(found, cartTarget{s: s, it: it})
			}
			return len(found) < 2
		}
		collectBoat := func(it *boatEntity) bool {
			if it.w != w || it.boat.Dead || it.cargo == nil {
				return true
			}
			minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
			if boxMeets(minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz) {
				found = append(found, boatTarget{s: s, it: it})
			}
			return len(found) < 2
		}
		ordered := true
		if !indexed {
			box := [6]float64{cx - 0.5, cy - 0.5, cz - 0.5, cx + 0.5, cy + 0.5, cz + 0.5}
			if area, ok := smallChunkArea(&box); ok {
				bodiesInChunks(&s.minecartChunks, nil, &area, collectCart)
				if len(found) < 2 {
					bodiesInChunks(&s.boatChunks, nil, &area, collectBoat)
				}
				ordered = len(found) >= 2
			}
			if ordered {
				// The same random draw must still select the same vehicle.
				found = found[:0]
				carts.more = s.minecarts
				boats.more = s.boats
			}
		}
		if ordered {
			carts.each(func(it *minecartEntity) { collectCart(it) })
			boats.each(func(it *boatEntity) { collectBoat(it) })
		}
		s.minecartsMu.Unlock()
		if locked {
			s.boatsMu.Unlock()
		}
		if len(found) == 0 {
			return nil
		}
		return found[s.levelRng.Intn(len(found))]
	}
}

// cartBucketsAt reads the index buckets for the column of (cx, cz); without an
// index for w it reports indexed false. empty means no vehicle can match.
// Caller holds minecartsMu.
func (s *Server) cartBucketsAt(w *world.World, cx, cz float64) (carts vehicleBucket[minecartEntity], boats vehicleBucket[boatEntity], indexed, empty bool) {
	idx := s.cartCols
	if idx == nil || idx.w != w {
		return carts, boats, false, false
	}
	if len(idx.cols) == 0 && len(idx.boats) == 0 {
		return carts, boats, true, true
	}
	key := [2]int32{int32(math.Floor(cx)), int32(math.Floor(cz))}
	carts = idx.cols[key]
	if len(idx.boats) > 0 {
		boats = idx.boats[key]
	}
	return carts, boats, true, carts.first == nil && boats.first == nil
}

// boxMeets tests strict overlap with the unit box centred on (cx, cy, cz).
func boxMeets(minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz float64) bool {
	return maxX > cx-0.5 && minX < cx+0.5 && maxY > cy-0.5 && minY < cy+0.5 && maxZ > cz-0.5 && minZ < cz+0.5
}

// filledOf is the mask of slots whose count is positive.
func filledOf(slots []protocol.ItemStack) uint64 {
	var m uint64
	for i := range min(len(slots), 64) {
		if slots[i].Count > 0 {
			m |= 1 << i
		}
	}
	return m
}
