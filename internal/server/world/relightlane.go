package world

import (
	"math/bits"
	"sync"
	"time"
)

type relightBox struct{ minX, minZ, maxX, maxZ int32 }

// RelightCompletion tracks asynchronous bookings sharing a result handler.
type RelightCompletion struct {
	sync.WaitGroup
	Handle func(*World, []LightChange)
}

type relightAnswer struct {
	then       func([]LightChange)
	completion *RelightCompletion
	bookings   int
}

func (a relightAnswer) answer(w *World, changed []LightChange) {
	if a.completion != nil {
		defer a.completion.Add(-a.bookings)
		if len(changed) != 0 {
			a.completion.Handle(w, changed)
		}
	} else {
		a.then(changed)
	}
}

type relightReq struct {
	relightBox
	first relightAnswer
	more  []relightAnswer
	next  int // next member of the merged group, or -1
}

func (r *relightReq) append(answer relightAnswer) {
	last := &r.first
	if len(r.more) != 0 {
		last = &r.more[len(r.more)-1]
	}
	if answer.completion != nil && answer.completion == last.completion {
		last.bookings += answer.bookings
		return
	}
	r.more = append(r.more, answer)
}

func (r *relightReq) answer(w *World, changed []LightChange) {
	runPoolTask(func() { r.first.answer(w, changed) })
	for _, answer := range r.more {
		runPoolTask(func() { answer.answer(w, nil) })
	}
	r.first, r.more = relightAnswer{}, nil
}

// relightLane queues relights after block changes. One drain task at a time
// takes everything queued and merges overlapping boxes, so a run of changes to
// the same ground (a lava flow's steps) is one wipe and refill under the write
// lock, holding one pool slot (which chunk generation would wait behind) rather
// than one each.
type relightLane struct {
	mu      sync.Mutex
	queue   []relightReq
	queued  map[relightBox]int
	running bool
	booked  uint64
	collect bool
}

// Give a burst of writes time to queue before taking a shadow that those
// writes would invalidate. Bound the wait while writes are still arriving.
const relightQuiet = 2 * time.Millisecond
const relightBatchLimit = 150 * time.Millisecond // three ticks at 20 TPS

// relightGroup is the merged box of one or more requests.
type relightGroup struct {
	relightBox
	first, last int // indexes into the batch
}

// maxRelightWipe caps a merged group's wipe at four single-column wipes, so a
// scattered burst cannot chain into a box whose relight holds the write lock
// past a tick.
const maxRelightWipe = 4 * (2*lightRadius + 1) * (2*lightRadius + 1)

// wipeArea is the columns the group's relight wipes: its box widened by the
// light radius.
func (b relightBox) wipeArea() int64 {
	return int64(b.maxX-b.minX+1+2*lightRadius) * int64(b.maxZ-b.minZ+1+2*lightRadius)
}

func (b relightBox) union(o relightBox) relightBox {
	return relightBox{
		minX: min(b.minX, o.minX), minZ: min(b.minZ, o.minZ),
		maxX: max(b.maxX, o.maxX), maxZ: max(b.maxZ, o.maxZ),
	}
}

// mergeRelights groups the requests, joining two when the joined wipe is no
// larger than the two apart and within maxRelightWipe, so merging never adds
// work. A box already over the cap still absorbs the boxes it contains, so a
// large request is neither split nor relit twice. Groups keep their first
// request's order.
func mergeRelights(batch []relightReq) []relightGroup {
	groups := make([]relightGroup, 0, len(batch))
	for j := range batch {
		batch[j].next = -1
		g := relightGroup{relightBox: batch[j].relightBox, first: j, last: j}
		at := len(groups)
		for i := 0; i < len(groups); i++ {
			o := groups[i]
			u := o.union(g.relightBox)
			area, a, b := u.wipeArea(), o.wipeArea(), g.wipeArea()
			if area > a+b || area > maxRelightWipe && area > max(a, b) {
				continue
			}
			// The grown group may now reach others: remove it and rescan from
			// the start.
			batch[o.last].next = g.first
			g.relightBox, g.first = u, o.first
			at = min(at, i)
			groups = append(groups[:i], groups[i+1:]...)
			i = -1
		}
		groups = append(groups, relightGroup{})
		copy(groups[at+1:], groups[at:])
		groups[at] = g
	}
	return groups
}

// RelightAreaAsync queues one light recompute for the column box plus the light
// radius and returns a channel receiving the changed light sections. One request for a
// batch's whole span (an explosion's crater) gives the same light as one per
// column, without re-wiping overlaps. A merged request receives the group's
// changed sections, or nil where another member got them, so each is told once.
func (w *World) RelightAreaAsync(minX, minZ, maxX, maxZ int32) <-chan []LightChange {
	done := make(chan []LightChange, 1)
	w.RelightAreaThen(minX, minZ, maxX, maxZ, func(changed []LightChange) { done <- changed })
	return done
}

// RelightAreaThen queues a recompute and calls then when it finishes. Only the
// first member of a merged group receives its changes. The other members get
// nil. The callback runs without world or lane locks and must not wait on the lane.
func (w *World) RelightAreaThen(minX, minZ, maxX, maxZ int32, then func([]LightChange)) {
	w.queueRelight(relightBox{minX, minZ, maxX, maxZ}, relightAnswer{then: then})
}

// RelightAreaTracked queues a recompute and tracks its completion, combining
// adjacent bookings for the same box and handler into one count.
func (w *World) RelightAreaTracked(minX, minZ, maxX, maxZ int32, completion *RelightCompletion) {
	completion.Add(1)
	w.queueRelight(relightBox{minX, minZ, maxX, maxZ}, relightAnswer{completion: completion, bookings: 1})
}

func (w *World) queueRelight(box relightBox, answer relightAnswer) {
	l := &w.relights
	l.mu.Lock()
	l.booked++
	if i, ok := l.queued[box]; ok {
		r := &l.queue[i]
		r.append(answer)
		l.mu.Unlock()
		return
	}
	if l.queued == nil {
		l.queued = make(map[relightBox]int)
	}
	l.queued[box] = len(l.queue)
	l.queue = append(l.queue, relightReq{relightBox: box, first: answer})
	l.collect = l.collect || box.minX&15 == 0 && box.minZ&15 == 0 && box.maxX == box.minX|15 && box.maxZ == box.minZ|15
	start := !l.running
	l.running = true
	l.mu.Unlock()
	if start {
		w.exec.submitChunk(w.drainRelights)
	}
}

// drainRelights relights the queue batch by batch until it is empty.
func (w *World) drainRelights() {
	l := &w.relights
	var batch []relightReq
	defer func() {
		if p := recover(); p != nil {
			// Answer every waiter nil and let the next request start a drain.
			l.mu.Lock()
			queued := l.queue
			l.queue = nil
			clear(l.queued)
			l.running = false
			l.collect = false
			l.mu.Unlock()
			for _, requests := range [...][]relightReq{batch, queued} {
				for i := range requests {
					r := &requests[i]
					if r.first.then != nil || r.first.completion != nil {
						r.answer(w, nil)
					}
				}
			}
			panic(p)
		}
	}()
	for {
		l.mu.Lock()
		if len(l.queue) == 0 {
			l.running = false
			l.mu.Unlock()
			return
		}
		collect := l.collect
		if collect {
			deadline := time.Now().Add(relightBatchLimit)
			for {
				booked := l.booked
				l.mu.Unlock()
				time.Sleep(relightQuiet)
				l.mu.Lock()
				if booked == l.booked || !time.Now().Before(deadline) {
					break
				}
			}
		}
		batch = l.queue
		l.queue = nil
		clear(l.queued)
		l.collect = false
		l.mu.Unlock()
		for _, g := range mergeRelights(batch) {
			var changed []LightChange
			if !collect || !w.settleDarkRelight(g.relightBox) {
				changed = w.relightArea(g.minX, g.minZ, g.maxX, g.maxZ)
			}
			for i := g.first; i != -1; i = batch[i].next {
				batch[i].answer(w, changed)
				changed = nil
			}
		}
	}
}

// A dark edit has nothing to propagate if neither its sections nor their
// neighbours contain light or emitters. Check whole sections so this still
// works after the per-cell marks overflow. Keep the check and clear atomic
// with block writes and other light installs.
func (w *World) settleDarkRelight(box relightBox) bool {
	minX, minZ := addClamped(box.minX, -lightRadius), addClamped(box.minZ, -lightRadius)
	maxX, maxZ := addClamped(box.maxX, lightRadius), addClamped(box.maxZ, lightRadius)
	w.mu.Lock()
	defer w.mu.Unlock()

	var bands uint32
	for x := minX >> 4; x <= maxX>>4; x++ {
		for z := minZ >> 4; z <= maxZ>>4; z++ {
			if loaded := w.chunks[ChunkPos{x, z}]; loaded != nil &&
				loaded.chunk.lightDirty != ([sectionSize]uint16{}) {
				bands |= loaded.chunk.lightBands
			}
		}
	}
	// The virtual sky above the top block section may be a source.
	if bands == 0 || bands&(1<<(lightSections-2)) != 0 {
		return false
	}
	bands = (bands | bands<<1 | bands>>1) &^ 1 // no propagation below the world
	for x := addClamped(minX, -1) >> 4; x <= addClamped(maxX, 1)>>4; x++ {
		for z := addClamped(minZ, -1) >> 4; z <= addClamped(maxZ, 1)>>4; z++ {
			loaded := w.chunks[ChunkPos{x, z}]
			if loaded == nil {
				continue
			}
			c := loaded.chunk
			if !c.lightCorrect {
				return false
			}
			for check := bands; check != 0; check &= check - 1 {
				i := bits.TrailingZeros32(check)
				if c.sections[i-1].emitting != 0 || !lightArrayEmpty(c.skyLight[i]) || !lightArrayEmpty(c.blockLight[i]) {
					return false
				}
			}
		}
	}
	w.lightDirtyIn(minX, minZ, maxX, maxZ, true)
	return true
}
