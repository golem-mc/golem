package server

import (
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/server/world"
)

// trackingRegion groups eight chunks per axis; membership conservatively covers
// the player's streamed square. Entity ranges and dimensions are checked later.
type trackingRegion struct{ x, z int32 }

type trackingBounds struct{ lo, hi trackingRegion }

type trackingSlot struct {
	index      int
	generation uint64
}

func slotOf(c *client) trackingSlot { return trackingSlot{c.slot, c.slotGeneration} }

type trackingIndex struct {
	generation uint64
	index      int // One-based; zero means absent.
}

func watcherBounds(w trackingWatcher) trackingBounds {
	x, z, r := world.ChunkOf(w.x), world.ChunkOf(w.z), w.chunkRadius
	return trackingBounds{trackingRegion{(x - r) >> 3, (z - r) >> 3}, trackingRegion{(x + r) >> 3, (z + r) >> 3}}
}

// trackingArea keeps player coverage between passes, without retaining clients.
// A pool gives each pass exclusive ownership.
type trackingArea struct {
	cells  map[trackingRegion][]trackingSlot
	bounds map[trackingSlot]trackingBounds
}

var trackingAreas = sync.Pool{New: func() any {
	return &trackingArea{cells: make(map[trackingRegion][]trackingSlot), bounds: make(map[trackingSlot]trackingBounds)}
}}

func (a *trackingArea) change(id trackingSlot, b trackingBounds, add bool) {
	for x := b.lo.x; x <= b.hi.x; x++ {
		for z := b.lo.z; z <= b.hi.z; z++ {
			cell := trackingRegion{x, z}
			if add {
				a.cells[cell] = append(a.cells[cell], id)
			} else {
				ids := a.cells[cell]
				i := slices.Index(ids, id)
				ids = slices.Delete(ids, i, i+1)
				if len(ids) == 0 {
					delete(a.cells, cell)
				} else {
					a.cells[cell] = ids
				}
			}
		}
	}
}

// nearbyPlayers maps coverage back into this pass's original watcher order.
// The area is populated lazily: ticks with no bodies need no index.
type nearbyPlayers struct {
	n      int
	view   func(int) trackingWatcher
	area   *trackingArea
	bySlot []trackingIndex
	all    []int
	buf    []int
}

func nearbyFor[T any](watchers []T, view func(T) trackingWatcher) *nearbyPlayers {
	return &nearbyPlayers{n: len(watchers), view: func(i int) trackingWatcher { return view(watchers[i]) }}
}

func nearbyTracking(watchers []trackingWatcher) *nearbyPlayers {
	return nearbyFor(watchers, func(w trackingWatcher) trackingWatcher { return w })
}

func nearbyWorlds[T any](watchers map[*world.World][]T, view func(T) trackingWatcher) map[*world.World]*nearbyPlayers {
	out := make(map[*world.World]*nearbyPlayers, len(watchers))
	for w, ws := range watchers {
		out[w] = nearbyFor(ws, view)
	}
	return out
}

func releaseNearbyWorlds(near map[*world.World]*nearbyPlayers) {
	for _, n := range near {
		n.release()
	}
}

func (n *nearbyPlayers) release() {
	if n != nil && n.area != nil {
		trackingAreas.Put(n.area)
		n.area = nil
	}
}

func (n *nearbyPlayers) init() {
	n.area = trackingAreas.Get().(*trackingArea)
	n.update()
}

func (n *nearbyPlayers) update() {
	slots := 0
	for i := range n.n {
		slots = max(slots, n.view(i).c.slot+1)
	}
	if slots > cap(n.bySlot) {
		n.bySlot = make([]trackingIndex, slots)
	} else {
		n.bySlot = n.bySlot[:slots]
		clear(n.bySlot)
	}
	for i := range n.n {
		w := n.view(i)
		id, bounds := slotOf(w.c), watcherBounds(w)
		n.bySlot[id.index] = trackingIndex{generation: id.generation, index: i + 1}
		if old, ok := n.area.bounds[id]; ok {
			if old == bounds {
				continue
			}
			n.area.change(id, old, false)
		}
		n.area.change(id, bounds, true)
		n.area.bounds[id] = bounds
	}
	for id, bounds := range n.area.bounds {
		if _, ok := n.index(id); !ok {
			n.area.change(id, bounds, false)
			delete(n.area.bounds, id)
		}
	}
}

// watchers includes old watchers even outside the area so range departures get
// their removal. Sorting preserves the pass's packet order; only rejected pairs
// can be omitted. Coverage uses view distance, so passenger or broadcast range
// changes cannot invalidate it. vanilla: ChunkMap.TrackedEntity.updatePlayer
func (v *trackedBody[B, S]) watchers(n *nearbyPlayers) []int {
	if n == nil {
		return nil
	}
	if n.all == nil {
		n.all = make([]int, n.n)
		for i := range n.all {
			n.all[i] = i
		}
	}
	// A short list is cheaper to walk than to index.
	if n.n <= 8 {
		return n.all
	}
	if n.area == nil {
		n.init()
	}
	n.buf = n.buf[:0]
	cell := trackingRegion{world.ChunkOf(v.p.x) >> 3, world.ChunkOf(v.p.z) >> 3}
	ids := n.area.cells[cell]
	if len(ids) == n.n {
		return n.all
	}
	for _, id := range ids {
		if i, ok := n.index(id); ok {
			n.buf = append(n.buf, i)
		}
	}
	for c := range v.t.tracked.clients() {
		if i, ok := n.index(slotOf(c)); ok {
			n.buf = append(n.buf, i)
		}
	}
	slices.Sort(n.buf)
	n.buf = slices.Compact(n.buf)
	return n.buf
}

func (n *nearbyPlayers) index(slot trackingSlot) (int, bool) {
	if slot.index < len(n.bySlot) {
		i := n.bySlot[slot.index]
		return i.index - 1, i.index != 0 && i.generation == slot.generation
	}
	return 0, false
}
