package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// golemOpeners counts the copper golems holding each chest open: the golems'
// share of ContainerOpenersCounter, beside the players' windows. Its lock is
// a leaf, taken under the mob lock and by connection goroutines.
// vanilla: CopperGolem.hasContainerOpen
type golemOpeners struct {
	mu    sync.Mutex
	cells map[*entity.Mob][]lidRecheck
	count map[lidRecheck]int
}

// open books the golem as an opener of the cells.
func (g *golemOpeners) open(m *entity.Mob, cells []lidRecheck) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cells == nil {
		g.cells, g.count = map[*entity.Mob][]lidRecheck{}, map[lidRecheck]int{}
	}
	g.closeLocked(m)
	g.cells[m] = cells
	for _, c := range cells {
		g.count[c]++
	}
}

// close ends the golem's opening and returns the cells it held.
func (g *golemOpeners) close(m *entity.Mob) []lidRecheck {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closeLocked(m)
}

func (g *golemOpeners) closeLocked(m *entity.Mob) []lidRecheck {
	cells := g.cells[m]
	delete(g.cells, m)
	for _, c := range cells {
		if g.count[c]--; g.count[c] <= 0 {
			delete(g.count, c)
		}
	}
	return cells
}

// at is how many golems hold the cell's container open.
func (g *golemOpeners) at(w *world.World, x, y, z int32) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.count[lidRecheck{w: w, x: x, y: y, z: z}]
}

// chestLid is a golem's chest gaining or losing its lid opener, applied
// after the mob lock.
type chestLid struct {
	w       *world.World
	m       *entity.Mob
	x, y, z int32
	open    bool
}

// syncGolemChest sends the chest's viewers what the golem's move changed;
// it takes s.mu, so it runs after the mob lock.
func (s *Server) syncGolemChest(l chestLid) {
	if target := blockContainerAt(l.w, l.x, l.y, l.z); target != nil {
		target.sync(s, l.w)
	}
}

// applyChestLid books the golem's opening and tells the lid: the action, the
// open sound for the first opener, the close sound for the last leaving.
// vanilla: ContainerOpenersCounter.incrementOpeners, decrementOpeners
func (s *Server) applyChestLid(l chestLid) {
	var cells []lidRecheck
	if l.open {
		cells = []lidRecheck{{w: l.w, x: l.x, y: l.y, z: l.z}}
		if other, ok := golemChestPartner(l.w, l.x, l.y, l.z); ok {
			cells = append(cells, lidRecheck{w: l.w, x: other[0], y: other[1], z: other[2]})
		}
		s.golemOpen.open(l.m, cells)
	} else {
		cells = s.golemOpen.close(l.m)
	}
	for _, c := range cells {
		if !l.open {
			s.containerLidClosed(c.w, c.x, c.y, c.z)
			continue
		}
		s.containerLidOpened(c.w, c.x, c.y, c.z)
		if lid, ok := lidOf(c.w, c.x, c.y, c.z); ok && s.containerOpenerCount(c.w, c.x, c.y, c.z) == 1 {
			s.playContainerSound(c.w, lid.openSound, c.x, c.y, c.z)
		}
	}
}

func golemChestPartner(w *world.World, x, y, z int32) ([3]int32, bool) {
	first, second, ok := w.ChestPair(x, y, z)
	if !ok {
		return [3]int32{}, false
	}
	if first == [3]int32{x, y, z} {
		return second, true
	}
	return first, true
}

// golemChests is a dimension's chests as its copper golems reach them. The
// clients are the tick's, read before the mob lock, which s.mu may not be
// taken under.
type golemChests struct {
	s       *Server
	w       *world.World
	dim     *dimension
	clients []*client
	scratch []*world.BlockEntity // one chunk's candidates, reused across searches
}

var _ entity.ChestAccess = (*golemChests)(nil)

// ChestsIn lists the chests' cells in the box, walking the chunks around its
// centre in ChunkPos.rangeClosed order.
// vanilla: TransportItemsBetweenContainers.getTransportTarget
func (c *golemChests) ChestsIn(x0, y0, z0, x1, y1, z1 int32, into [][3]int32) [][3]int32 {
	cx, cz := ((x0+x1)>>1)>>4, ((z0+z1)>>1)>>4
	r := (x1-x0)/2/16 + 1
	for z := cz - r; z <= cz+r; z++ {
		for x := cx - r; x <= cx+r; x++ {
			c.scratch = c.w.AppendChunkBlockEntities(c.scratch[:0], world.ChunkPos{X: x, Z: z}, world.KindChest, world.KindTrappedChest)
			for _, be := range c.scratch {
				if be.X >= x0 && be.X <= x1 && be.Y >= y0 && be.Y <= y1 && be.Z >= z0 && be.Z <= z1 {
					into = append(into, [3]int32{be.X, be.Y, be.Z})
				}
			}
		}
	}
	clear(c.scratch)
	return into
}

// ChestBlocked runs under mobsMu, so the cat query must not take it again.
func (c *golemChests) ChestBlocked(x, y, z int32) bool {
	return c.w.ChestBlockedAtBy(x, y, z, sittingCatSource{s: c.s, dim: c.dim, held: true})
}

// ChestInUse counts players looking in and golems holding the chest open.
// vanilla: TransportItemsBetweenContainers.isAnotherMobInteractingWithTarget
func (c *golemChests) ChestInUse(x, y, z int32) bool {
	cells := [][3]int32{{x, y, z}}
	if other, ok := golemChestPartner(c.w, x, y, z); ok {
		cells = append(cells, other)
	}
	for _, cell := range cells {
		if c.s.golemOpen.at(c.w, cell[0], cell[1], cell[2]) > 0 || openersAmong(c.clients, c.w, cell[0], cell[1], cell[2]) > 0 {
			return true
		}
	}
	return false
}

// ChestLocked is isContainerLocked: an empty predicate is NO_LOCK.
func (c *golemChests) ChestLocked(x, y, z int32) (locked bool) {
	if be := c.w.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *world.BlockEntity) { locked = b.Lock != nil && len(b.Lock.Entries()) > 0 })
	}
	return locked
}

func (c *golemChests) ChestPartner(x, y, z int32) ([3]int32, bool) {
	return golemChestPartner(c.w, x, y, z)
}

// use runs fn on the chest's slots, a double chest as one container, under
// the block entities' locks.
func (c *golemChests) use(x, y, z int32, fn func(inventory.Block)) bool {
	target := blockContainerAt(c.w, x, y, z)
	if target == nil {
		return false
	}
	target.use(c.w, containerMove{fn: fn})
	return true
}

func (c *golemChests) ChestEmpty(x, y, z int32) (empty, ok bool) {
	ok = c.use(x, y, z, func(b inventory.Block) { empty = slotsEmpty(b.Stacks()) })
	return empty, ok
}

func (c *golemChests) ChestHolds(x, y, z int32, item protocol.ItemStack) (holds bool) {
	c.use(x, y, z, func(b inventory.Block) {
		for _, s := range b.Stacks() {
			if !s.Empty() && s.ItemID == item.ItemID {
				holds = true
				return
			}
		}
	})
	return holds
}

// ChestTake is pickupItemFromContainer.
func (c *golemChests) ChestTake(x, y, z int32) (taken protocol.ItemStack) {
	c.use(x, y, z, func(b inventory.Block) {
		for slot := range b.Len() {
			s := b.Slot(slot)
			if s.Empty() {
				continue
			}
			taken = s
			taken.Count = min(s.Count, 16)
			s.Count -= taken.Count
			if s.Empty() {
				s = protocol.ItemStack{}
			}
			b.Put(slot, s)
			return
		}
	})
	return taken
}

// ChestPut is addItemsToContainer, including the leftover count it leaves
// when a merge overfills.
func (c *golemChests) ChestPut(x, y, z int32, held protocol.ItemStack) protocol.ItemStack {
	c.use(x, y, z, func(b inventory.Block) {
		for slot := range b.Len() {
			s := b.Slot(slot)
			if s.Empty() {
				b.Put(slot, held)
				held = protocol.ItemStack{}
				return
			}
			room := registry.StackMaxStackSize(s) - s.Count
			if !s.Same(held) || room <= 0 {
				continue
			}
			s.Count += min(room, held.Count)
			held.Count -= room
			b.Put(slot, s)
			if held.Empty() {
				held = protocol.ItemStack{}
				return
			}
		}
	})
	return held
}

// copperGolemEvent turns a copper golem's statue or chest event into effects.
func (t *mobDimTick) copperGolemEvent(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobTurnedToStatue {
		t.turnedToStatue(fx, ev)
		return
	}
	if ev.Kind == entity.MobChestChanged {
		fx.chestSyncs = append(fx.chestSyncs, chestLid{w: t.w, m: ev.Mob, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z)})
		return
	}
	fx.chestLids = append(fx.chestLids, chestLid{w: t.w, m: ev.Mob, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), open: ev.Kind == entity.MobChestOpened})
}
