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

// Filled scans the cargo: carts are rare and small.
func (b cartBlock) Filled() uint64 { return filledOf(b.it.cargo.slots) }

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

	// vanilla: Hopper.SUCK_AABB moved onto the cart's level position
	var found []*itemEntity
	if above == nil {
		found = s.itemEntitiesIn(w, x-0.5, y-0.5+11.0/16.0, z-0.5, x+0.5, y-0.5+2, z+0.5)
	}
	for _, e := range s.itemEntitiesIn(w, minX-0.25, minY, minZ-0.25, maxX+0.25, maxY, maxZ+0.25) {
		if !slices.Contains(found, e) {
			found = append(found, e)
		}
	}
	var taken, shrunk []*itemEntity
	for _, e := range found {
		if e.item.Stack.Empty() {
			continue
		}
		s.minecartsMu.Lock()
		left, _ := inventory.AddItem(cart, e.item.Stack, 0, false)
		s.minecartsMu.Unlock()
		if left.Empty() {
			e.item.Dead = true
			taken = append(taken, e)
			break
		}
		if left.Count != e.item.Stack.Count {
			e.item.Stack = left
			shrunk = append(shrunk, e)
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
	cols  map[[2]int32]vehicleBucket[*minecartEntity]
	boats map[[2]int32]vehicleBucket[*boatEntity]
}

// vehicleBucket holds a column's first vehicle inline so a lone vehicle
// costs no slice; more holds the rest in encounter order.
type vehicleBucket[T any] struct {
	first T
	more  []T
	used  bool
}

// each calls f for the first vehicle, then the rest, in insertion order.
func (b vehicleBucket[T]) each(f func(T)) {
	if !b.used {
		return
	}
	f(b.first)
	for _, it := range b.more {
		f(it)
	}
}

func (c *cartColumns) add(it *minecartEntity) {
	if it.w != c.w || it.cart.Dead || it.cargo.slots == nil {
		return
	}
	minX, _, minZ, maxX, _, maxZ := it.cart.Box()
	addContainerColumns(c.cols, it, minX, minZ, maxX, maxZ)
}

func (c *cartColumns) addBoat(it *boatEntity) {
	if it.w != c.w || it.boat.Dead || it.cargo == nil {
		return
	}
	minX, _, minZ, maxX, _, maxZ := it.boat.Box()
	addContainerColumns(c.boats, it, minX, minZ, maxX, maxZ)
}

// addContainerColumns includes every column whose unit query box can overlap the hull.
func addContainerColumns[T any](cols map[[2]int32]vehicleBucket[T], it T, minX, minZ, maxX, maxZ float64) {
	for x := int32(math.Floor(minX - 0.5)); x <= int32(math.Floor(maxX+0.5)); x++ {
		for z := int32(math.Floor(minZ - 0.5)); z <= int32(math.Floor(maxZ+0.5)); z++ {
			k := [2]int32{x, z}
			b := cols[k]
			if b.used {
				b.more = append(b.more, it)
			} else {
				b.first, b.used = it, true
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
	if idx.cols == nil {
		idx.cols = make(map[[2]int32]vehicleBucket[*minecartEntity])
	} else {
		clear(idx.cols)
	}
	for _, it := range s.minecarts {
		idx.add(it)
	}
	if idx.boats == nil {
		idx.boats = make(map[[2]int32]vehicleBucket[*boatEntity])
	} else {
		clear(idx.boats)
	}
	for _, it := range s.boats {
		idx.addBoat(it)
	}
	s.cartCols = idx
}

func (s *Server) dropCartColumns() {
	s.minecartsMu.Lock()
	s.cartCols = nil
	// Release the vehicles and world the last pass buffered; published
	// overflow slices stay with their readers.
	idx := &s.cartColBuffer
	idx.w = nil
	clear(idx.cols)
	clear(idx.boats)
	s.minecartsMu.Unlock()
}

// entityContainerAt picks a random living container vehicle whose box meets the
// unit box centred on (cx, cy, cz), never except; nil for none.
// vanilla: HopperBlockEntity.getEntityContainer
func (s *Server) entityContainerAt(w *world.World, cx, cy, cz float64, except *minecartEntity) containerTarget {
	s.minecartsMu.Lock()
	carts := vehicleBucket[*minecartEntity]{}
	var boats vehicleBucket[*boatEntity]
	indexed := false
	if idx := s.cartCols; idx != nil && idx.w == w {
		if len(idx.cols) == 0 && len(idx.boats) == 0 {
			s.minecartsMu.Unlock()
			return nil
		}
		key := [2]int32{int32(math.Floor(cx)), int32(math.Floor(cz))}
		carts = idx.cols[key]
		boats = idx.boats[key]
		if !carts.used && !boats.used {
			s.minecartsMu.Unlock()
			return nil
		}
		indexed = true
	}
	var found []containerTarget
	visitCart := func(it *minecartEntity) {
		if it == except || it.w != w || it.cart.Dead || it.cargo.slots == nil {
			return
		}
		minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
		if boxMeets(minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz) {
			found = append(found, cartTarget{s: s, it: it})
		}
	}
	if indexed {
		carts.each(visitCart)
	} else {
		for _, it := range s.minecarts {
			visitCart(it)
		}
	}
	s.minecartsMu.Unlock()
	if !indexed || boats.used {
		s.boatsMu.Lock()
		visitBoat := func(it *boatEntity) {
			if it.w != w || it.boat.Dead || it.cargo == nil {
				return
			}
			minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
			if boxMeets(minX, minY, minZ, maxX, maxY, maxZ, cx, cy, cz) {
				found = append(found, boatTarget{s: s, it: it})
			}
		}
		if indexed {
			boats.each(visitBoat)
		} else {
			for _, it := range s.boats {
				visitBoat(it)
			}
		}
		s.boatsMu.Unlock()
	}
	if len(found) == 0 {
		return nil
	}
	return found[s.levelRng.Intn(len(found))]
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
