package world

import "sync"

// relightReq is one RelightAreaAsync's column box and its answer channel.
type relightReq struct {
	minX, minZ, maxX, maxZ int32
	done                   chan []LightChange
}

// relightLane queues relights after block changes. One drain task at a time
// takes everything queued and merges overlapping boxes, so a run of changes to
// the same ground (a lava flow's steps) is one wipe and refill under the write
// lock, holding one pool slot (which chunk generation would wait behind) rather
// than one each.
type relightLane struct {
	mu      sync.Mutex
	queue   []relightReq
	running bool
}

// relightGroup is the merged box of one or more requests.
type relightGroup struct {
	minX, minZ, maxX, maxZ int32
	reqs                   []relightReq
}

// maxRelightWipe caps a merged group's wipe at four single-column wipes, so a
// scattered burst cannot chain into a box whose relight holds the write lock
// past a tick.
const maxRelightWipe = 4 * (2*lightRadius + 1) * (2*lightRadius + 1)

// wipeArea is the columns the group's relight wipes: its box widened by the
// light radius.
func (g relightGroup) wipeArea() int64 {
	return int64(g.maxX-g.minX+1+2*lightRadius) * int64(g.maxZ-g.minZ+1+2*lightRadius)
}

func (g relightGroup) union(o relightGroup) relightGroup {
	return relightGroup{
		minX: min(g.minX, o.minX), minZ: min(g.minZ, o.minZ),
		maxX: max(g.maxX, o.maxX), maxZ: max(g.maxZ, o.maxZ),
		reqs: append(g.reqs, o.reqs...),
	}
}

// mergeRelights groups the requests, joining two when the joined wipe is no
// larger than the two apart and within maxRelightWipe, so merging never adds
// work. A box already over the cap still absorbs the boxes it contains, so a
// large request is neither split nor relit twice. Groups keep their first
// request's order.
func mergeRelights(batch []relightReq) []relightGroup {
	groups := make([]relightGroup, 0, len(batch))
	for _, r := range batch {
		g := relightGroup{minX: r.minX, minZ: r.minZ, maxX: r.maxX, maxZ: r.maxZ, reqs: []relightReq{r}}
		at := len(groups)
		for i := 0; i < len(groups); i++ {
			u := groups[i].union(g)
			if u.wipeArea() > groups[i].wipeArea()+g.wipeArea() {
				continue
			}
			if u.wipeArea() > maxRelightWipe && u.wipeArea() > max(groups[i].wipeArea(), g.wipeArea()) {
				continue
			}
			// The grown group may now reach others: remove it and rescan from
			// the start.
			g = u
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
	l := &w.relights
	l.mu.Lock()
	l.queue = append(l.queue, relightReq{minX: minX, minZ: minZ, maxX: maxX, maxZ: maxZ, done: done})
	start := !l.running
	l.running = true
	l.mu.Unlock()
	if start {
		w.exec.submitChunk(w.drainRelights)
	}
	return done
}

// drainRelights relights the queue batch by batch until it is empty.
func (w *World) drainRelights() {
	l := &w.relights
	// groups[gi:] are taken but unanswered: only relightArea panics, before
	// its group is answered.
	var groups []relightGroup
	gi := 0
	defer func() {
		if p := recover(); p != nil {
			// Answer every waiter nil and let the next request start a drain.
			l.mu.Lock()
			queued := l.queue
			l.queue = nil
			l.running = false
			l.mu.Unlock()
			for _, g := range groups[gi:] {
				for _, r := range g.reqs {
					r.done <- nil
				}
			}
			for _, r := range queued {
				r.done <- nil
			}
			panic(p)
		}
	}()
	for {
		l.mu.Lock()
		batch := l.queue
		l.queue = nil
		if len(batch) == 0 {
			l.running = false
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		groups = mergeRelights(batch)
		for gi = range groups {
			g := groups[gi]
			changed := w.relightArea(g.minX, g.minZ, g.maxX, g.maxZ)
			for i, r := range g.reqs {
				if i == 0 {
					r.done <- changed
				} else {
					r.done <- nil
				}
			}
		}
		groups = nil
	}
}
